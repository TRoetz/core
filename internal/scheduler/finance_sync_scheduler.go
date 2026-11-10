package scheduler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"windshift/internal/blobstore"
	"windshift/internal/config"
	"windshift/internal/database"
	"windshift/internal/emailutil"
	"windshift/internal/logger"
	"windshift/internal/models"
	"windshift/internal/repository"
	"windshift/internal/smtp"
)

const (
	financeSyncTickInterval = 5 * time.Minute
	financeSyncStaleAfter   = 30 * time.Minute
	financeSyncMaxBytes     = 64 << 20 // 64 MiB per file
	financeSyncErrorCap     = 200      // error_summary entries kept (most recent)
)

// financeSyncRequiredColumns is the minimum CSV header set for financial-system
// exports (§4.2). Extra columns are ignored and preserved in raw_row.
var financeSyncRequiredColumns = []string{"initiative_ref", "period", "line_type", "amount"}

// FinanceSyncOptions carries deployment-level settings a single sync run needs.
type FinanceSyncOptions struct {
	PathPrefix   string // allowlist for file_path sources; empty disables file_path
	NotifyEmails string // comma-separated failure-alert recipients
	SMTPSender   *smtp.NotificationSMTPSender
}

// StartFinanceSyncRun creates a 'running' run row and returns its ID. The caller
// must finalize it with FinishFinanceSyncRun (or the stale-run reaper will).
func StartFinanceSyncRun(db database.Database, configID int, trigger string) (int64, error) {
	res, err := db.Exec(`INSERT INTO p3m_finance_sync_runs (config_id, trigger, started_at, status) VALUES (?, ?, ?, 'running')`, configID, trigger, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("create finance sync run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("finance sync run id: %w", err)
	}
	return id, nil
}

// FinishFinanceSyncRun fetches the configured source and runs the import
// pipeline for an existing run row. The run row is always finalized; a non-nil
// return means fatal failure.
func FinishFinanceSyncRun(ctx context.Context, db database.Database, store *blobstore.Store, opts FinanceSyncOptions, cfg models.P3MFinanceSyncConfig, runID int64) error {
	data, err := fetchFinanceSyncFile(ctx, store, opts.PathPrefix, cfg.SourceType, cfg.SourceRef)
	if err != nil {
		finalizeFinanceSyncRun(db, runID, "failed", &cfg, nil, 0, 0, 0, 0, []financeSyncRowError{{Row: 0, Reason: err.Error()}})
		slog.Warn("p3m finance sync: fetch failed", "run_id", runID, "config_id", cfg.ID, "error", err)
		return err
	}
	return ImportFinanceSyncData(ctx, db, opts, cfg, runID, data)
}

// ImportFinanceSyncData runs the import pipeline for in-memory file content:
// hash no-op check → parse → match → insert (single transaction) → finalize.
// Used by the ad-hoc upload endpoint; the run row must already exist with
// trigger='upload'. The run row is always finalized; a non-nil return means
// fatal failure.
func ImportFinanceSyncData(ctx context.Context, db database.Database, opts FinanceSyncOptions, cfg models.P3MFinanceSyncConfig, runID int64, data []byte) error {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	var total, matched, unmatched, failed int
	rowErrs := make([]financeSyncRowError, 0)

	fatal := func(err error) error {
		finalizeFinanceSyncRun(db, runID, "failed", &cfg, nil, total, matched, unmatched, failed, append(rowErrs, financeSyncRowError{Row: 0, Reason: err.Error()}))
		slog.Warn("p3m finance sync: run failed", "run_id", runID, "config_id", cfg.ID, "error", err)
		return err
	}

	lastHash := ""
	if cfg.LastFileHash != nil {
		lastHash = *cfg.LastFileHash
	}
	if lastHash == hash {
		slog.Info("p3m finance sync: file unchanged, skipping import", "run_id", runID, "config_id", cfg.ID)
		finalizeFinanceSyncRun(db, runID, "success", &cfg, &hash, 0, 0, 0, 0, nil)
		return nil
	}

	rows, parseErrs, err := parseFinanceSyncCSV(data, cfg.DefaultCurrency)
	rowErrs = append(rowErrs, parseErrs...)
	if err != nil {
		return fatal(err)
	}

	idx, err := loadFinanceProjectIndex(ctx, db)
	if err != nil {
		return fatal(fmt.Errorf("load project index: %w", err))
	}
	rejected, err := loadFinanceRejections(ctx, db)
	if err != nil {
		return fatal(fmt.Errorf("load rejections: %w", err))
	}

	now := time.Now().UTC()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fatal(fmt.Errorf("begin import transaction: %w", err))
	}
	for _, row := range rows {
		for _, period := range row.periods {
			total++
			status, projectID := "unmatched", (*int)(nil)
			key := row.ref + "|" + period + "|" + row.lineType
			if rejected[key] {
				status = "rejected"
			} else if pid, ok := idx.byRef[normalizeFinanceRef(row.ref)]; ok {
				status, projectID = "matched", &pid
			} else if cfg.MatchMode == "ref_then_title" && row.title != "" {
				if cands := idx.byTitle[NormalizeFinanceTitle(row.title)]; len(cands) == 1 {
					pid := cands[0]
					status, projectID = "matched", &pid
				} else if len(cands) > 1 {
					rowErrs = append(rowErrs, financeSyncRowError{Row: row.rowNum, Reason: fmt.Sprintf("ambiguous title %q matches projects %v; assign or reject in the review queue", row.title, cands)})
				}
			}
			switch status {
			case "matched":
				matched++
			case "unmatched":
				unmatched++
			}
			if _, err := tx.Exec(`INSERT INTO p3m_finance_actuals (config_id, run_id, project_id, external_ref, cost_center, work_order, period, line_type, amount, currency, description, match_status, raw_row, imported_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				cfg.ID, runID, finNullInt(projectID), row.ref, finNullStr(row.costCenter), finNullStr(row.workOrder), period, row.lineType, row.amount, row.currency, finNullStr(row.description), status, &row.raw, now); err != nil {
				failed++
				rowErrs = append(rowErrs, financeSyncRowError{Row: row.rowNum, Reason: fmt.Sprintf("insert failed for %s/%s/%s: %v", row.ref, period, row.lineType, err)})
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fatal(fmt.Errorf("commit import transaction: %w", err))
	}

	status := "success"
	if unmatched > 0 || failed > 0 {
		status = "partial"
	}
	finalizeFinanceSyncRun(db, runID, status, &cfg, &hash, total, matched, unmatched, failed, rowErrs)

	logger.LogAudit(db, logger.AuditEvent{
		UserID:         0,
		Username:       "system",
		Source:         logger.AuditSourceSystem,
		ActionType:     "p3m.finance_sync.run",
		ResourceType:   "p3m_finance_sync_config",
		ResourceID:     &cfg.ID,
		ResourceName:   cfg.Name,
		Details:        map[string]any{"run_id": runID, "status": status, "rows_total": total, "rows_matched": matched, "rows_unmatched": unmatched, "rows_failed": failed, "file_hash": hash},
		Success:        status == "success",
	})

	if (status == "failed" || status == "partial") && opts.SMTPSender != nil {
		sendFinanceSyncAlert(ctx, opts, cfg, runID, status, total, matched, unmatched, failed)
	}
	return nil
}

// fetchFinanceSyncFile reads the configured source and returns its content.
// file_path sources must live under pathPrefix; s3 sources are read through the
// blobstore client. The no-op hash is computed by ImportFinanceSyncData.
func fetchFinanceSyncFile(ctx context.Context, store *blobstore.Store, pathPrefix, sourceType, sourceRef string) ([]byte, error) {
	var data []byte
	switch sourceType {
	case "file_path":
		prefix := strings.TrimRight(pathPrefix, "/")
		if prefix == "" {
			return nil, errors.New("file_path sources are disabled (FINANCE_SYNC_PATH_PREFIX not set)")
		}
		cleaned := filepath.Clean(sourceRef)
		if !filepath.IsAbs(cleaned) || cleaned != sourceRef {
			return nil, fmt.Errorf("source_ref must be an absolute path: %q", sourceRef)
		}
		if cleaned != prefix && !strings.HasPrefix(cleaned, prefix+"/") {
			return nil, fmt.Errorf("path %q is outside the allowed prefix %q", cleaned, prefix)
		}
		f, err := os.Open(cleaned) // #nosec G304 -- validated against allowlist above
		if err != nil {
			return nil, fmt.Errorf("open finance export: %w", err)
		}
		defer f.Close()
		data, err = io.ReadAll(io.LimitReader(f, financeSyncMaxBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read finance export: %w", err)
		}
	case "s3":
		obj, err := store.Open(ctx, sourceRef)
		if err != nil {
			return nil, fmt.Errorf("open s3 object %q: %w", sourceRef, err)
		}
		defer obj.Close()
		data, err = io.ReadAll(io.LimitReader(obj, financeSyncMaxBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read s3 object %q: %w", sourceRef, err)
		}
	default:
		return nil, fmt.Errorf("unknown source_type %q", sourceType)
	}
	if int64(len(data)) > financeSyncMaxBytes {
		return nil, fmt.Errorf("finance export exceeds %d bytes", financeSyncMaxBytes)
	}
	return data, nil
}

// finalizeFinanceSyncRun writes the terminal run row and updates the config's
// last-run bookkeeping. Failures here are logged only: the import itself already
// committed (or did not start), so a bookkeeping error must not mask it.
func finalizeFinanceSyncRun(db database.Database, runID int64, status string, cfg *models.P3MFinanceSyncConfig, fileHash *string, total, matched, unmatched, failed int, rowErrs []financeSyncRowError) {
	now := time.Now().UTC()
	summary := ""
	if len(rowErrs) > 0 {
		all := rowErrs
		if len(all) > financeSyncErrorCap {
			all = all[len(all)-financeSyncErrorCap:]
		}
		if b, err := json.Marshal(all); err == nil {
			summary = string(b)
		}
	}
	var summaryPtr *string
	if summary != "" {
		summaryPtr = &summary
	}
	if _, err := db.Exec(`UPDATE p3m_finance_sync_runs SET completed_at = ?, status = ?, file_hash = ?, rows_total = ?, rows_matched = ?, rows_unmatched = ?, rows_failed = ?, error_summary = ? WHERE id = ?`,
		now, status, finNullStr(fileHash), total, matched, unmatched, failed, finNullStr(summaryPtr), runID); err != nil {
		slog.Warn("p3m finance sync: finalize run", "run_id", runID, "error", err)
	}
	if _, err := db.Exec(`UPDATE p3m_finance_sync_configs SET last_run_at = ?, last_run_status = ?, last_file_hash = COALESCE(?, last_file_hash), updated_at = ? WHERE id = ?`,
		now, status, finNullStr(fileHash), now, cfg.ID); err != nil {
		slog.Warn("p3m finance sync: update config bookkeeping", "config_id", cfg.ID, "error", err)
	}
}

// sendFinanceSyncAlert emails the failure-alert distribution list on failed or
// partial runs (spec §4.5). Recipients come from FINANCE_SYNC_NOTIFY_EMAILS.
func sendFinanceSyncAlert(ctx context.Context, opts FinanceSyncOptions, cfg models.P3MFinanceSyncConfig, runID int64, status string, total, matched, unmatched, failed int) {
	if strings.TrimSpace(opts.NotifyEmails) == "" {
		return
	}
	data := map[string]any{
		"ConfigName":    cfg.Name,
		"RunID":         runID,
		"Status":        status,
		"RowsTotal":     total,
		"RowsMatched":   matched,
		"RowsUnmatched": unmatched,
		"RowsFailed":    failed,
	}
	for _, addr := range strings.Split(opts.NotifyEmails, ",") {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if err := opts.SMTPSender.SendTransactionalContext(ctx, addr, emailutil.TemplateFinanceSyncFailure, data); err != nil {
			slog.Warn("p3m finance sync: failure alert", "to", addr, "error", err)
		}
	}
}

// ---------------------------------------------------------------------------
// CSV parsing
// ---------------------------------------------------------------------------

type financeSyncRowError struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

type parsedFinanceRow struct {
	rowNum      int
	ref         string
	title       string
	costCenter  *string
	workOrder   *string
	lineType    string
	amount      float64 // per period (quarter rows are split evenly)
	currency    string
	description *string
	raw         string
	periods     []string
}

// parseFinanceSyncCSV parses a financial-system export. It is deliberately
// defensive: no formula injection (values stored as text/numbers, never evaluated),
// delimiter sniffing, and per-row errors instead of aborting the whole file.
func parseFinanceSyncCSV(data []byte, defaultCurrency string) ([]parsedFinanceRow, []financeSyncRowError, error) {
	firstLine := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		firstLine = data[:i]
	}
	delim := sniffFinanceDelimiter(string(firstLine))

	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = delim
	r.FieldsPerRecord = -1 // extra columns are ignored (preserved in raw_row)
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read CSV header: %w", err)
	}
	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, req := range financeSyncRequiredColumns {
		if _, ok := colIdx[req]; !ok {
			return nil, nil, fmt.Errorf("CSV is missing required column %q", req)
		}
	}

	get := func(rec []string, name string) (string, bool) {
		i, ok := colIdx[name]
		if !ok || i >= len(rec) {
			return "", false
		}
		return strings.TrimSpace(rec[i]), true
	}
	strOrNull := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}

	rows := make([]parsedFinanceRow, 0, 64)
	errs := make([]financeSyncRowError, 0)
	rowNum := 1
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		rowNum++
		// Self-describing replay format: the header plus the raw cells, so a
		// stored row can be re-matched or audited without the original file.
		rawJSON, _ := json.Marshal(map[string]any{"header": header, "row": rec})
		if err != nil {
			errs = append(errs, financeSyncRowError{Row: rowNum, Reason: fmt.Sprintf("parse error: %v", err)})
			continue
		}
		ref, _ := get(rec, "initiative_ref")
		if ref == "" {
			errs = append(errs, financeSyncRowError{Row: rowNum, Reason: "missing initiative_ref"})
			continue
		}
		periodRaw, ok := get(rec, "period")
		if !ok || periodRaw == "" {
			errs = append(errs, financeSyncRowError{Row: rowNum, Reason: "missing period"})
			continue
		}
		lineType := strings.ToLower(mustGet(rec, colIdx, "line_type"))
		switch lineType {
		case "actual", "commitment", "forecast":
		default:
			errs = append(errs, financeSyncRowError{Row: rowNum, Reason: fmt.Sprintf("invalid line_type %q (want actual, commitment or forecast)", lineType)})
			continue
		}
		amountStr := mustGet(rec, colIdx, "amount")
		if amountStr == "" {
			errs = append(errs, financeSyncRowError{Row: rowNum, Reason: "missing amount"})
			continue
		}
		amount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			errs = append(errs, financeSyncRowError{Row: rowNum, Reason: fmt.Sprintf("invalid amount %q", amountStr)})
			continue
		}
		currency := mustGet(rec, colIdx, "currency")
		if currency == "" {
			currency = defaultCurrency
		}
		title := mustGet(rec, colIdx, "project_title")
		cc := mustGet(rec, colIdx, "cost_center")
		wo := mustGet(rec, colIdx, "work_order")
		desc := mustGet(rec, colIdx, "description")

		periods, err := expandFinancePeriod(periodRaw)
		if err != nil {
			errs = append(errs, financeSyncRowError{Row: rowNum, Reason: err.Error()})
			continue
		}

		rows = append(rows, parsedFinanceRow{
			rowNum:      rowNum,
			ref:         ref,
			title:       title,
			costCenter:  strOrNull(cc),
			workOrder:   strOrNull(wo),
			lineType:    lineType,
			amount:      amount / float64(len(periods)),
			currency:    currency,
			description: strOrNull(desc),
			raw:         string(rawJSON),
			periods:     periods,
		})
	}
	return rows, errs, nil
}

func mustGet(rec []string, colIdx map[string]int, name string) string {
	i, ok := colIdx[name]
	if !ok || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}

// expandFinancePeriod normalizes a CSV period into canonical YYYY-MM months. A
// quarter (YYYY-Qn or YYYYQN) expands to its three months; the caller splits
// the amount evenly across them — an interim convention for finance exports
// that report quarterly.
func expandFinancePeriod(raw string) ([]string, error) {
	raw = strings.TrimSpace(strings.ToUpper(raw))
	// Both quarter forms end with the quarter digit: YYYY-Qn (7 chars) and
	// YYYYQN (6 chars). Checked before months because YYYY-Qn also starts
	// with a dash.
	if (len(raw) == 7 && raw[4] == '-' && raw[5] == 'Q') || (len(raw) == 6 && raw[4] == 'Q') {
		q := raw[len(raw)-1:]
		if q < "1" || q > "4" {
			return nil, fmt.Errorf("invalid quarter in period %q", raw)
		}
		y := raw[:4]
		for _, c := range y {
			if c < '0' || c > '9' {
				return nil, fmt.Errorf("invalid year in period %q", raw)
			}
		}
		start := (int(q[0]-'1') * 3) + 1
		out := make([]string, 3)
		for i := range out {
			out[i] = fmt.Sprintf("%s-%02d", y, start+i)
		}
		return out, nil
	}
	if len(raw) == 7 && raw[4] == '-' {
		m := raw[5:7]
		if m < "01" || m > "12" {
			return nil, fmt.Errorf("invalid month in period %q", raw)
		}
		return []string{strings.ToLower(raw)}, nil
	}
	return nil, fmt.Errorf("invalid period %q (want YYYY-MM or YYYY-Qn)", raw)
}

// sniffFinanceDelimiter picks the most frequent candidate delimiter in the header
// row. Mirrors csvimport.DetectDelimiter for in-memory content.
func sniffFinanceDelimiter(firstLine string) rune {
	best, bestCount := ',', strings.Count(firstLine, ",")
	for _, c := range []rune{';', '\t', '|'} {
		if n := strings.Count(firstLine, string(c)); n > bestCount {
			best, bestCount = c, n
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// Matching
// ---------------------------------------------------------------------------

type financeProjectIndex struct {
	byRef   map[string]int    // normalized external_ref → project ID
	byTitle map[string][]int  // normalized title → project IDs
}

func loadFinanceProjectIndex(ctx context.Context, db database.Database) (financeProjectIndex, error) {
	idx := financeProjectIndex{byRef: make(map[string]int), byTitle: make(map[string][]int)}
	rows, err := db.QueryContext(ctx, `SELECT id, title, external_ref FROM p3m_projects`)
	if err != nil {
		return idx, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var title string
		var ref *string
		if err := rows.Scan(&id, &title, &ref); err != nil {
			return idx, err
		}
		if ref != nil {
			if key := normalizeFinanceRef(*ref); key != "" {
				idx.byRef[key] = id
			}
		}
		if key := NormalizeFinanceTitle(title); key != "" {
			idx.byTitle[key] = append(idx.byTitle[key], id)
		}
	}
	return idx, rows.Err()
}

// loadFinanceRejections returns the set of (ref|period|line_type) keys that a
// user has rejected in the review queue. Rejected rows are imported with
// match_status='rejected' and never re-surface as unmatched.
func loadFinanceRejections(ctx context.Context, db database.Database) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT external_ref || '|' || period || '|' || line_type FROM p3m_finance_sync_rejections`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		set[key] = true
	}
	return set, rows.Err()
}

func normalizeFinanceRef(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// NormalizeFinanceTitle lowercases and collapses whitespace/punctuation runs so
// "Data & Archive  Storage" matches "data archive storage". Shared by the sync
// engine (import-time matching) and the review queue (candidate suggestions).
func NormalizeFinanceTitle(s string) string {
	var b strings.Builder
	prevSpace := true // leading punctuation/space is skipped
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		isWord := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isWord {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func finNullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func finNullStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// ---------------------------------------------------------------------------
// Scheduler (tick-based, mirrors the other windshift schedulers)
// ---------------------------------------------------------------------------

// FinanceSyncScheduler runs enabled P3M finance sync configs on their schedules.
type FinanceSyncScheduler struct {
	db         database.Database
	store      *blobstore.Store
	cfg        config.FinanceSyncConfig
	smtpSender *smtp.NotificationSMTPSender
	runRepo    *repository.SchedulerRunRepository

	timer   *time.Timer
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	running bool
}

func NewFinanceSyncScheduler(db database.Database, store *blobstore.Store, cfg config.FinanceSyncConfig, smtpSender *smtp.NotificationSMTPSender) *FinanceSyncScheduler {
	return &FinanceSyncScheduler{db: db, store: store, cfg: cfg, smtpSender: smtpSender, runRepo: repository.NewSchedulerRunRepository(db)}
}

func (s *FinanceSyncScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	s.timer = time.NewTimer(financeSyncTickInterval)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.wg.Add(1)
	go s.loop(ctx, s.timer)
}

func (s *FinanceSyncScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.timer.Stop()
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *FinanceSyncScheduler) loop(ctx context.Context, timer *time.Timer) {
	defer s.wg.Done()
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			if _, err := s.runTick(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("p3m finance sync tick", "error", err)
			}
			timer.Reset(financeSyncTickInterval)
		case <-ctx.Done():
			return
		}
	}
}

func (s *FinanceSyncScheduler) runTick(ctx context.Context) (items int, runErr error) {
	start := time.Now()
	defer recordSchedulerRun(s.runRepo, "finance_sync", start, &items, &runErr, time.Now)
	if !s.cfg.Enabled {
		return 0, nil
	}
	s.reclaimStaleRuns()
	configs, err := s.listEnabledConfigs(ctx)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	for _, cfgRow := range configs {
		if cfgRow.LastRunAt != nil && now.Sub(*cfgRow.LastRunAt) < time.Duration(cfgRow.ScheduleMinutes)*time.Minute {
			continue
		}
		runID, err := StartFinanceSyncRun(s.db, cfgRow.ID, "schedule")
		if err != nil {
			slog.Warn("p3m finance sync: start scheduled run", "config_id", cfgRow.ID, "error", err)
			continue
		}
		items++
		if err := FinishFinanceSyncRun(ctx, s.db, s.store, s.options(), cfgRow, runID); err != nil && ctx.Err() == nil {
			slog.Warn("p3m finance sync: scheduled run failed", "config_id", cfgRow.ID, "run_id", runID, "error", err)
		}
	}
	return items, nil
}

func (s *FinanceSyncScheduler) options() FinanceSyncOptions {
	return FinanceSyncOptions{PathPrefix: s.cfg.PathPrefix, NotifyEmails: s.cfg.NotifyEmails, SMTPSender: s.smtpSender}
}

// reclaimStaleRuns marks runs left 'running' by a crash or restart as failed so
// they do not block the config's next scheduled tick.
func (s *FinanceSyncScheduler) reclaimStaleRuns() {
	cutoff := time.Now().UTC().Add(-financeSyncStaleAfter)
	res, err := s.db.Exec(`UPDATE p3m_finance_sync_runs SET status = 'failed', completed_at = ?, error_summary = ? WHERE status = 'running' AND started_at < ?`,
		time.Now().UTC(), `[{"row":0,"reason":"reclaimed stale run (scheduler restarted mid-import)"}]`, cutoff)
	if err != nil {
		slog.Warn("p3m finance sync: reclaim stale runs", "error", err)
		return
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		slog.Info("p3m finance sync: reclaimed stale runs", "count", n)
	}
}

func (s *FinanceSyncScheduler) listEnabledConfigs(ctx context.Context) ([]models.P3MFinanceSyncConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, enabled, source_type, source_ref, schedule_minutes, period_format, default_currency, match_mode, last_run_at, last_run_status, last_file_hash, created_by, updated_by, created_at, updated_at FROM p3m_finance_sync_configs WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.P3MFinanceSyncConfig
	for rows.Next() {
		var c models.P3MFinanceSyncConfig
		if err := rows.Scan(&c.ID, &c.Name, &c.Enabled, &c.SourceType, &c.SourceRef, &c.ScheduleMinutes, &c.PeriodFormat, &c.DefaultCurrency, &c.MatchMode, &c.LastRunAt, &c.LastRunStatus, &c.LastFileHash, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
