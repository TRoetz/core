package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"windshift/internal/models"
	"windshift/internal/restapi"
	"windshift/internal/scheduler"
)

// ---------------------------------------------------------------------------
// Financial system sync (FIN-007): config CRUD, run trigger/upload, review queue.
// The engine lives in internal/scheduler; these handlers expose it over REST and
// add the operator-facing reconciliation views.
// ---------------------------------------------------------------------------

const p3mFinanceSyncConfigColumns = `id, name, enabled, source_type, source_ref, schedule_minutes, period_format, default_currency, match_mode, last_run_at, last_run_status, last_file_hash, created_by, updated_by, created_at, updated_at`

func scanP3MFinanceSyncConfig(row interface{ Scan(...any) error }) (models.P3MFinanceSyncConfig, error) {
	var c models.P3MFinanceSyncConfig
	err := row.Scan(&c.ID, &c.Name, &c.Enabled, &c.SourceType, &c.SourceRef, &c.ScheduleMinutes, &c.PeriodFormat, &c.DefaultCurrency, &c.MatchMode,
		&c.LastRunAt, &c.LastRunStatus, &c.LastFileHash, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

const p3mFinanceSyncRunColumns = `id, config_id, trigger, started_at, completed_at, status, file_hash, rows_total, rows_matched, rows_unmatched, rows_failed, error_summary`

func scanP3MFinanceSyncRun(row interface{ Scan(...any) error }) (models.P3MFinanceSyncRun, error) {
	var run models.P3MFinanceSyncRun
	err := row.Scan(&run.ID, &run.ConfigID, &run.Trigger, &run.StartedAt, &run.CompletedAt, &run.Status, &run.FileHash,
		&run.RowsTotal, &run.RowsMatched, &run.RowsUnmatched, &run.RowsFailed, &run.ErrorSummary)
	return run, err
}

const p3mFinanceActualColumns = `id, config_id, run_id, project_id, external_ref, cost_center, work_order, period, line_type, amount, currency, description, match_status, raw_row, imported_at`

func scanP3MFinanceActual(row interface{ Scan(...any) error }) (models.P3MFinanceActual, error) {
	var a models.P3MFinanceActual
	err := row.Scan(&a.ID, &a.ConfigID, &a.RunID, &a.ProjectID, &a.ExternalRef, &a.CostCenter, &a.WorkOrder, &a.Period, &a.LineType,
		&a.Amount, &a.Currency, &a.Description, &a.MatchStatus, &a.RawRow, &a.ImportedAt)
	return a, err
}

// p3mFinanceRawTitle extracts the project_title cell from a self-describing
// raw_row ({"header": [...], "row": [...]}) so unmatched rows can be matched by
// title without re-reading the original file.
func p3mFinanceRawTitle(raw *string) string {
	if raw == nil || *raw == "" {
		return ""
	}
	var obj struct {
		Header []string `json:"header"`
		Row    []string `json:"row"`
	}
	if err := json.Unmarshal([]byte(*raw), &obj); err != nil {
		return ""
	}
	for i, h := range obj.Header {
		if strings.EqualFold(strings.TrimSpace(h), "project_title") && i < len(obj.Row) {
			return strings.TrimSpace(obj.Row[i])
		}
	}
	return ""
}

// p3mFinanceSyncAvailable reports whether the sync engine was wired at startup.
func (h *P3MHandler) p3mFinanceSyncAvailable(w http.ResponseWriter, r *http.Request) bool {
	if h.financeStore == nil {
		respondError(w, r, restapi.NewAPIError(http.StatusServiceUnavailable, "finance_sync_unavailable", "financial sync is not configured on this server"))
		return false
	}
	return true
}

// ListFinanceSyncConfigs handles GET /api/p3m/finance-sync/configs.
func (h *P3MHandler) ListFinanceSyncConfigs(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	rows, err := h.db.QueryContext(ctx, `SELECT `+p3mFinanceSyncConfigColumns+` FROM p3m_finance_sync_configs ORDER BY id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	configs := make([]models.P3MFinanceSyncConfig, 0)
	for rows.Next() {
		c, err := scanP3MFinanceSyncConfig(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		configs = append(configs, c)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, configs)
}

// p3mFinanceSyncConfigInput is the create/update payload for a sync config.
type p3mFinanceSyncConfigInput struct {
	Name            string `json:"name"`
	Enabled         *bool  `json:"enabled,omitempty"`
	SourceType      string `json:"source_type"`
	SourceRef       string `json:"source_ref"`
	ScheduleMinutes int    `json:"schedule_minutes"`
	PeriodFormat    string `json:"period_format"`
	DefaultCurrency string `json:"default_currency"`
	MatchMode       string `json:"match_mode"`
}

// p3mFinanceSyncValidateConfig checks the shared config invariants and applies
// defaults. It writes a 400 on failure and returns false.
func (h *P3MHandler) p3mFinanceSyncValidateConfig(w http.ResponseWriter, r *http.Request, in *p3mFinanceSyncConfigInput, isCreate bool) bool {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return false
	}
	switch in.SourceType {
	case "file_path":
		ref := strings.TrimSpace(in.SourceRef)
		if ref == "" || !strings.HasPrefix(ref, "/") {
			respondValidationError(w, r, "source_ref must be an absolute path for file_path sources")
			return false
		}
		in.SourceRef = ref
	case "s3":
		ref := strings.TrimSpace(in.SourceRef)
		if !strings.HasPrefix(ref, "s3://") {
			respondValidationError(w, r, "source_ref must start with s3:// for s3 sources")
			return false
		}
		in.SourceRef = ref
	default:
		respondValidationError(w, r, "source_type must be file_path or s3")
		return false
	}
	if in.ScheduleMinutes <= 0 {
		in.ScheduleMinutes = 60
	}
	if in.ScheduleMinutes < 5 || in.ScheduleMinutes > 1440 {
		respondValidationError(w, r, "schedule_minutes must be between 5 and 1440")
		return false
	}
	if strings.TrimSpace(in.PeriodFormat) == "" {
		in.PeriodFormat = "yyyy-mm"
	}
	if strings.TrimSpace(in.DefaultCurrency) == "" {
		in.DefaultCurrency = "NZD"
	}
	switch in.MatchMode {
	case "", "ref_only", "ref_then_title":
		if in.MatchMode == "" {
			in.MatchMode = "ref_only"
		}
	default:
		respondValidationError(w, r, "match_mode must be ref_only or ref_then_title")
		return false
	}
	return true
}

// CreateFinanceSyncConfig handles POST /api/p3m/finance-sync/configs (admin).
func (h *P3MHandler) CreateFinanceSyncConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in p3mFinanceSyncConfigInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mFinanceSyncValidateConfig(w, r, &in, true) {
		return
	}
	ctx := r.Context()
	now := time.Now().UTC()
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	res, err := h.db.ExecContext(ctx, `
		INSERT INTO p3m_finance_sync_configs (name, enabled, source_type, source_ref, schedule_minutes, period_format, default_currency, match_mode, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, boolToInt(enabled), in.SourceType, in.SourceRef, in.ScheduleMinutes, in.PeriodFormat, in.DefaultCurrency, in.MatchMode, user.ID, user.ID, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	c, err := scanP3MFinanceSyncConfig(h.db.QueryRowContext(ctx, `SELECT `+p3mFinanceSyncConfigColumns+` FROM p3m_finance_sync_configs WHERE id = ?`, int(id64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.finance_sync.config_create", "p3m_finance_sync_config", &c.ID, c.Name, map[string]any{
		"source_type": in.SourceType, "schedule_minutes": in.ScheduleMinutes, "match_mode": in.MatchMode,
	})
	respondJSONCreated(w, c)
}

// UpdateFinanceSyncConfig handles PUT /api/p3m/finance-sync/configs/{id} (admin).
func (h *P3MHandler) UpdateFinanceSyncConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	current, err := scanP3MFinanceSyncConfig(h.db.QueryRowContext(ctx, `SELECT `+p3mFinanceSyncConfigColumns+` FROM p3m_finance_sync_configs WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_finance_sync_config")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	var in struct {
		Name            *string `json:"name,omitempty"`
		Enabled         *bool   `json:"enabled,omitempty"`
		SourceType      *string `json:"source_type,omitempty"`
		SourceRef       *string `json:"source_ref,omitempty"`
		ScheduleMinutes *int    `json:"schedule_minutes,omitempty"`
		PeriodFormat    *string `json:"period_format,omitempty"`
		DefaultCurrency *string `json:"default_currency,omitempty"`
		MatchMode       *string `json:"match_mode,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	name := current.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	sourceType := current.SourceType
	if in.SourceType != nil {
		sourceType = *in.SourceType
	}
	sourceRef := current.SourceRef
	if in.SourceRef != nil {
		sourceRef = *in.SourceRef
	}
	scheduleMinutes := current.ScheduleMinutes
	if in.ScheduleMinutes != nil {
		scheduleMinutes = *in.ScheduleMinutes
	}
	matchMode := current.MatchMode
	if in.MatchMode != nil {
		matchMode = *in.MatchMode
	}

	// Validate the merged shape through the shared validator.
	merged := p3mFinanceSyncConfigInput{
		Name: name, SourceType: sourceType, SourceRef: sourceRef,
		ScheduleMinutes: scheduleMinutes, PeriodFormat: current.PeriodFormat,
		DefaultCurrency: current.DefaultCurrency, MatchMode: matchMode,
	}
	if !h.p3mFinanceSyncValidateConfig(w, r, &merged, false) {
		return
	}

	now := time.Now().UTC()
	enabled := current.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	periodFormat := merged.PeriodFormat
	defaultCurrency := merged.DefaultCurrency
	if _, err := h.db.ExecContext(ctx, `
		UPDATE p3m_finance_sync_configs SET name = ?, enabled = ?, source_type = ?, source_ref = ?, schedule_minutes = ?, period_format = ?, default_currency = ?, match_mode = ?, updated_by = ?, updated_at = ? WHERE id = ?`,
		name, boolToInt(enabled), sourceType, sourceRef, scheduleMinutes, periodFormat, defaultCurrency, matchMode, user.ID, now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	c, err := scanP3MFinanceSyncConfig(h.db.QueryRowContext(ctx, `SELECT `+p3mFinanceSyncConfigColumns+` FROM p3m_finance_sync_configs WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.finance_sync.config_update", "p3m_finance_sync_config", &c.ID, c.Name, p3mAuditDiff(
		[3]any{"name", current.Name, name},
		[3]any{"enabled", current.Enabled, enabled},
		[3]any{"source_type", current.SourceType, sourceType},
		[3]any{"source_ref", current.SourceRef, sourceRef},
		[3]any{"schedule_minutes", current.ScheduleMinutes, scheduleMinutes},
		[3]any{"match_mode", current.MatchMode, matchMode},
	))
	respondJSONOK(w, c)
}

// DeleteFinanceSyncConfig handles DELETE /api/p3m/finance-sync/configs/{id} (admin).
func (h *P3MHandler) DeleteFinanceSyncConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	current, err := scanP3MFinanceSyncConfig(h.db.QueryRowContext(ctx, `SELECT `+p3mFinanceSyncConfigColumns+` FROM p3m_finance_sync_configs WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_finance_sync_config")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM p3m_finance_sync_configs WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.finance_sync.config_delete", "p3m_finance_sync_config", &id, current.Name, nil)
	w.WriteHeader(http.StatusNoContent)
}

// RunFinanceSyncNow handles POST /api/p3m/finance-sync/configs/{id}/run. It
// creates a run row and executes the import in the background; the response
// carries the run ID for polling GET /runs/{id}.
func (h *P3MHandler) RunFinanceSyncNow(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	if !h.p3mFinanceSyncAvailable(w, r) {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	cfgRow, err := scanP3MFinanceSyncConfig(h.db.QueryRowContext(ctx, `SELECT `+p3mFinanceSyncConfigColumns+` FROM p3m_finance_sync_configs WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_finance_sync_config")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	runID64, err := scheduler.StartFinanceSyncRun(h.db, cfgRow.ID, "manual")
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.finance_sync.run_manual", "p3m_finance_sync_config", &cfgRow.ID, cfgRow.Name, map[string]any{"run_id": int(runID64)})
	go func() {
		runCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if err := scheduler.FinishFinanceSyncRun(runCtx, h.db, h.financeStore, h.financeOpts, cfgRow, runID64); err != nil {
			slog.Warn("p3m finance sync: manual run failed", "run_id", runID64, "config_id", cfgRow.ID, "error", err)
		}
	}()
	respondJSONOK(w, map[string]any{"run_id": int(runID64), "status": "running"})
}

// UploadFinanceSyncCSV handles POST /api/p3m/finance-sync/configs/{id}/upload.
// The CSV is staged in the blob store and imported synchronously; the response
// carries the run ID with final counts.
func (h *P3MHandler) UploadFinanceSyncCSV(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	if !h.p3mFinanceSyncAvailable(w, r) {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	cfgRow, err := scanP3MFinanceSyncConfig(h.db.QueryRowContext(ctx, `SELECT `+p3mFinanceSyncConfigColumns+` FROM p3m_finance_sync_configs WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_finance_sync_config")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	const maxUpload = 64 << 20 // 64 MiB, matching the engine's file cap
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1024)
	file, header, err := r.FormFile("file")
	if err != nil {
		respondValidationError(w, r, "multipart field 'file' is required (max 64 MiB)")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		respondBadRequest(w, r, fmt.Sprintf("read upload: %v", err))
		return
	}

	storedPath := fmt.Sprintf("p3m/finance-sync/uploads/%d/%d%s", cfgRow.ID, time.Now().Unix(), extensionOf(header.Filename))
	if err := h.financeStore.Put(ctx, storedPath, bytes.NewReader(data), int64(len(data)), "text/csv"); err != nil {
		slog.Warn("p3m finance sync: stage upload failed", "config_id", cfgRow.ID, "error", err)
		respondInternalError(w, r, fmt.Errorf("stage upload: %w", err))
		return
	}

	runID64, err := scheduler.StartFinanceSyncRun(h.db, cfgRow.ID, "upload")
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	importErr := scheduler.ImportFinanceSyncData(ctx, h.db, h.financeOpts, cfgRow, runID64, data)
	h.p3mAudit(r, user, "p3m.finance_sync.upload", "p3m_finance_sync_config", &cfgRow.ID, cfgRow.Name, map[string]any{
		"run_id": int(runID64), "file": header.Filename, "bytes": len(data), "stored_path": storedPath,
	})
	if importErr != nil {
		respondBadRequest(w, r, fmt.Sprintf("import failed: %v", importErr))
		return
	}
	run, err := scanP3MFinanceSyncRun(h.db.QueryRowContext(ctx, `SELECT `+p3mFinanceSyncRunColumns+` FROM p3m_finance_sync_runs WHERE id = ?`, int(runID64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, map[string]any{"run": run, "stored_path": storedPath})
}

// extensionOf returns the file extension (with dot) or ".csv" when absent.
func extensionOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 && i < len(name)-1 {
		return name[i:]
	}
	return ".csv"
}

// ListFinanceSyncRuns handles GET /api/p3m/finance-sync/runs?config_id=&status=.
func (h *P3MHandler) ListFinanceSyncRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := `SELECT ` + p3mFinanceSyncRunColumns + ` FROM p3m_finance_sync_runs WHERE 1=1`
	args := make([]any, 0, 3)
	if v := strings.TrimSpace(r.URL.Query().Get("config_id")); v != "" {
		id, ok := parseQueryInt(v)
		if !ok {
			respondValidationError(w, r, "config_id must be an integer")
			return
		}
		query += ` AND config_id = ?`
		args = append(args, id)
	}
	if v := strings.TrimSpace(r.URL.Query().Get("status")); v != "" {
		query += ` AND status = ?`
		args = append(args, v)
	}
	limit := 50
	if v, ok := parseQueryInt(r.URL.Query().Get("limit")); ok && v > 0 {
		limit = v
	}
	if limit > 200 {
		limit = 200
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	runs := make([]models.P3MFinanceSyncRun, 0)
	for rows.Next() {
		run, err := scanP3MFinanceSyncRun(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, runs)
}

// p3mFinanceSyncRunErrors is the decoded shape of a run's error_summary.
type p3mFinanceSyncRunError struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// GetFinanceSyncRun handles GET /api/p3m/finance-sync/runs/{id}. The raw
// error_summary JSON is also decoded into a structured errors array.
func (h *P3MHandler) GetFinanceSyncRun(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	run, err := scanP3MFinanceSyncRun(h.db.QueryRowContext(ctx, `SELECT `+p3mFinanceSyncRunColumns+` FROM p3m_finance_sync_runs WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_finance_sync_run")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	var errs []p3mFinanceSyncRunError
	if run.ErrorSummary != nil && *run.ErrorSummary != "" {
		_ = json.Unmarshal([]byte(*run.ErrorSummary), &errs)
	}
	respondJSONOK(w, map[string]any{"run": run, "errors": errs})
}

// ListUnmatchedFinanceRows handles GET /api/p3m/finance-sync/unmatched. It is
// the review queue: unmatched fact rows with candidate projects (by normalized
// title) for assign/reject actions.
func (h *P3MHandler) ListUnmatchedFinanceRows(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := `SELECT a.` + p3mFinanceActualColumns + ` FROM p3m_finance_actuals a WHERE a.match_status = 'unmatched'`
	args := make([]any, 0, 2)
	if v := strings.TrimSpace(r.URL.Query().Get("config_id")); v != "" {
		id, ok := parseQueryInt(v)
		if !ok {
			respondValidationError(w, r, "config_id must be an integer")
			return
		}
		query += ` AND a.config_id = ?`
		args = append(args, id)
	}
	limit := 200
	if v, ok := parseQueryInt(r.URL.Query().Get("limit")); ok && v > 0 {
		limit = v
	}
	if limit > 1000 {
		limit = 1000
	}
	query += ` ORDER BY a.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	facts := make([]models.P3MFinanceActual, 0)
	for rows.Next() {
		a, err := scanP3MFinanceActual(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		facts = append(facts, a)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	// Candidate lookup: one project index per request, keyed by normalized title.
	type projRef struct {
		id    int
		title string
		ref   *string
	}
	index := make(map[string][]projRef)
	prows, err := h.db.QueryContext(ctx, `SELECT id, title, external_ref FROM p3m_projects`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for prows.Next() {
		var p projRef
		if err := prows.Scan(&p.id, &p.title, &p.ref); err != nil {
			prows.Close()
			respondInternalError(w, r, err)
			return
		}
		if key := scheduler.NormalizeFinanceTitle(p.title); key != "" {
			index[key] = append(index[key], p)
		}
	}
	if err := prows.Err(); err != nil {
		prows.Close()
		respondInternalError(w, r, err)
		return
	}
	prows.Close()

	for i := range facts {
		title := p3mFinanceRawTitle(facts[i].RawRow)
		if title == "" {
			continue
		}
		cands := index[scheduler.NormalizeFinanceTitle(title)]
		for _, c := range cands {
			facts[i].Candidates = append(facts[i].Candidates, models.P3MFinanceMatchCandidate{ID: c.id, Title: c.title, Ref: c.ref})
		}
	}
	respondJSONOK(w, facts)
}

// AssignUnmatchedFinanceRow handles POST /api/p3m/finance-sync/unmatched/{fact_id}/assign (admin).
// It matches the fact row to a project and optionally persists an external_ref on
// the project so future runs match by reference.
func (h *P3MHandler) AssignUnmatchedFinanceRow(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	factID, ok := requireIDParam(w, r, "fact_id")
	if !ok {
		return
	}
	ctx := r.Context()
	fact, err := scanP3MFinanceActual(h.db.QueryRowContext(ctx, `SELECT `+p3mFinanceActualColumns+` FROM p3m_finance_actuals WHERE id = ?`, factID))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_finance_actual")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if fact.MatchStatus != "unmatched" {
		respondConflict(w, r, "row is no longer unmatched (already matched or rejected)")
		return
	}

	var in struct {
		ProjectID   int     `json:"project_id"`
		ExternalRef *string `json:"external_ref,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.ProjectID <= 0 {
		respondValidationError(w, r, "project_id is required")
		return
	}

	var projectTitle string
	if err := h.db.QueryRowContext(ctx, `SELECT title FROM p3m_projects WHERE id = ?`, in.ProjectID).Scan(&projectTitle); err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	ref := fact.ExternalRef
	if in.ExternalRef != nil && strings.TrimSpace(*in.ExternalRef) != "" {
		ref = strings.TrimSpace(*in.ExternalRef)
	}

	now := time.Now().UTC()
	if ref != "" {
		var existing int
		if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_projects WHERE external_ref = ? AND id <> ?`, ref, in.ProjectID).Scan(&existing); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if existing > 0 {
			respondConflict(w, r, fmt.Sprintf("external_ref %q is already used by another project", ref))
			return
		}
		if _, err := h.db.ExecContext(ctx, `UPDATE p3m_projects SET external_ref = ?, updated_at = ? WHERE id = ?`, ref, now, in.ProjectID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	res, err := h.db.ExecContext(ctx, `UPDATE p3m_finance_actuals SET project_id = ?, match_status = 'matched' WHERE id = ? AND match_status = 'unmatched'`, in.ProjectID, factID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondConflict(w, r, "row is no longer unmatched")
		return
	}

	h.p3mAudit(r, user, "p3m.finance_sync.assign", "p3m_finance_actual", &factID, fact.ExternalRef+" "+fact.Period+" "+fact.LineType, map[string]any{
		"project_id": in.ProjectID, "project_title": projectTitle, "external_ref": ref,
	})
	respondJSONOK(w, map[string]any{"ok": true})
}

// RejectUnmatchedFinanceRow handles POST /api/p3m/finance-sync/unmatched/{fact_id}/reject (admin).
// It records the dismissal so later runs store such rows as rejected instead of
// re-queueing them.
func (h *P3MHandler) RejectUnmatchedFinanceRow(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	factID, ok := requireIDParam(w, r, "fact_id")
	if !ok {
		return
	}
	ctx := r.Context()
	fact, err := scanP3MFinanceActual(h.db.QueryRowContext(ctx, `SELECT `+p3mFinanceActualColumns+` FROM p3m_finance_actuals WHERE id = ?`, factID))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_finance_actual")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if fact.MatchStatus != "unmatched" {
		respondConflict(w, r, "row is no longer unmatched (already matched or rejected)")
		return
	}

	var in struct {
		Reason string `json:"reason"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		respondValidationError(w, r, "reason is required")
		return
	}

	var existing int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_finance_sync_rejections WHERE external_ref = ? AND period = ? AND line_type = ?`, fact.ExternalRef, fact.Period, fact.LineType).Scan(&existing); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if existing == 0 {
		if _, err := h.db.ExecContext(ctx, `INSERT INTO p3m_finance_sync_rejections (external_ref, period, line_type, reason, rejected_by) VALUES (?, ?, ?, ?, ?)`, fact.ExternalRef, fact.Period, fact.LineType, reason, user.ID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	} else if _, err := h.db.ExecContext(ctx, `UPDATE p3m_finance_sync_rejections SET reason = ?, rejected_by = ? WHERE external_ref = ? AND period = ? AND line_type = ?`, reason, user.ID, fact.ExternalRef, fact.Period, fact.LineType); err != nil {
		respondInternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(ctx, `UPDATE p3m_finance_actuals SET match_status = 'rejected' WHERE id = ? AND match_status = 'unmatched'`, factID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondConflict(w, r, "row is no longer unmatched")
		return
	}

	h.p3mAudit(r, user, "p3m.finance_sync.reject", "p3m_finance_actual", &factID, fact.ExternalRef+" "+fact.Period+" "+fact.LineType, map[string]any{"reason": reason})
	respondJSONOK(w, map[string]any{"ok": true})
}

// p3mFinanceEffectiveActualsQuery returns the SELECT for effective (latest-run)
// matched actual rows plus its WHERE fragment builder. The effective value for a
// (project, period, line_type) key comes from the latest successful run that
// matched it; earlier runs stay in the table for audit.
func p3mFinanceEffectiveActualsQuery() string {
	return `SELECT a.` + p3mFinanceActualColumns + ` FROM p3m_finance_actuals a JOIN p3m_finance_sync_runs r ON r.id = a.run_id WHERE a.match_status = 'matched' AND r.status IN ('success','partial')`
}

const p3mFinanceEffectiveSubquery = ` AND a.run_id = (SELECT MAX(a2.run_id) FROM p3m_finance_actuals a2 JOIN p3m_finance_sync_runs r2 ON r2.id = a2.run_id WHERE a2.project_id = a.project_id AND a2.period = a.period AND a2.line_type = a.line_type AND a2.match_status = 'matched' AND r2.status IN ('success','partial'))`

// ListEffectiveFinanceActuals handles GET /api/p3m/finance-sync/actuals with
// optional project_id, period_from, period_to and line_type filters.
func (h *P3MHandler) ListEffectiveFinanceActuals(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := p3mFinanceEffectiveActualsQuery() + p3mFinanceEffectiveSubquery
	args := make([]any, 0, 5)
	q := r.URL.Query()
	if v := strings.TrimSpace(q.Get("project_id")); v != "" {
		id, ok := parseQueryInt(v)
		if !ok {
			respondValidationError(w, r, "project_id must be an integer")
			return
		}
		query += ` AND a.project_id = ?`
		args = append(args, id)
	}
	for param, op := range map[string]string{"period_from": ">=", "period_to": "<="} {
		if v := strings.TrimSpace(q.Get(param)); v != "" {
			query += fmt.Sprintf(` AND a.period %s ?`, op)
			args = append(args, v)
		}
	}
	if v := strings.TrimSpace(q.Get("line_type")); v != "" {
		query += ` AND a.line_type = ?`
		args = append(args, v)
	}
	query += ` ORDER BY a.project_id, a.period, a.line_type LIMIT 1000`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	facts := make([]models.P3MFinanceActual, 0)
	for rows.Next() {
		a, err := scanP3MFinanceActual(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		facts = append(facts, a)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, facts)
}

// GetProjectFinanceReconciliation handles GET /api/p3m/projects/{id}/finance-reconciliation.
// It compares the project's approved budget against synced actuals per period and
// flags divergence when cumulative spend exceeds budget plus contingency.
func (h *P3MHandler) GetProjectFinanceReconciliation(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	project, err := h.getProjectRow(ctx, id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(ctx, p3mFinanceEffectiveActualsQuery()+p3mFinanceEffectiveSubquery+` AND a.project_id = ? ORDER BY a.period, a.line_type`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	type periodRow struct {
		Period           string  `json:"period"`
		Actual           float64 `json:"actual"`
		Commitment       float64 `json:"commitment"`
		Forecast         float64 `json:"forecast"`
		CumulativeActual float64 `json:"cumulative_actual"`
		VarianceVsBudget float64 `json:"variance_vs_budget"`
		Divergent        bool    `json:"divergent"`
	}
	periods := make([]periodRow, 0)
	indexByPeriod := make(map[string]int)
	var lastSyncedPeriod string
	for rows.Next() {
		var (
			aID         int
			configID    int
			runID       *int
			projectID   *int
			externalRef string
			costCenter  *string
			workOrder   *string
			period      string
			lineType    string
			amount      float64
			currency    string
			description *string
			matchStatus string
			rawRow      *string
			importedAt  time.Time
		)
		if err := rows.Scan(&aID, &configID, &runID, &projectID, &externalRef, &costCenter, &workOrder, &period, &lineType, &amount, &currency, &description, &matchStatus, &rawRow, &importedAt); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if period > lastSyncedPeriod {
			lastSyncedPeriod = period
		}
		i, exists := indexByPeriod[period]
		if !exists {
			periods = append(periods, periodRow{Period: period})
			indexByPeriod[period] = len(periods) - 1
			i = len(periods) - 1
		}
		switch lineType {
		case "actual":
			periods[i].Actual += amount
		case "commitment":
			periods[i].Commitment += amount
		case "forecast":
			periods[i].Forecast += amount
		}
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	budget := project.BudgetApproved
	tolerance := budget + project.Contingency
	cumulative := 0.0
	for i := range periods {
		cumulative += periods[i].Actual
		periods[i].CumulativeActual = cumulative
		periods[i].VarianceVsBudget = budget - cumulative
		periods[i].Divergent = cumulative > tolerance
	}

	respondJSONOK(w, map[string]any{
		"project_id":         id,
		"budget_approved":    budget,
		"contingency":        project.Contingency,
		"last_synced_period": lastSyncedPeriod,
		"periods":            periods,
	})
}

// GetFinanceSyncSummary handles GET /api/p3m/finance-sync/summary: sync health per
// config (stale when older than twice the schedule), unmatched count and synced
// actual spend by portfolio for the pulse widget.
func (h *P3MHandler) GetFinanceSyncSummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	configs := make([]map[string]any, 0)
	now := time.Now().UTC()
	crows, err := h.db.QueryContext(ctx, `SELECT `+p3mFinanceSyncConfigColumns+` FROM p3m_finance_sync_configs ORDER BY id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for crows.Next() {
		c, err := scanP3MFinanceSyncConfig(crows)
		if err != nil {
			crows.Close()
			respondInternalError(w, r, err)
			return
		}
		var ageMinutes int64 = -1
		stale := false
		if c.LastRunAt != nil {
			ageMinutes = int64(now.Sub(*c.LastRunAt).Minutes())
			stale = c.Enabled && now.Sub(*c.LastRunAt) > 2*time.Duration(c.ScheduleMinutes)*time.Minute
		} else if c.Enabled {
			stale = true
		}
		configs = append(configs, map[string]any{
			"id":              c.ID,
			"name":            c.Name,
			"enabled":         c.Enabled,
			"last_run_at":     nullTimePtr(c.LastRunAt),
			"last_run_status": nullStringPtr(c.LastRunStatus),
			"age_minutes":     ageMinutes,
			"stale":           stale,
		})
	}
	if err := crows.Err(); err != nil {
		crows.Close()
		respondInternalError(w, r, err)
		return
	}
	crows.Close()

	var unmatched int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_finance_actuals WHERE match_status = 'unmatched'`).Scan(&unmatched); err != nil {
		respondInternalError(w, r, err)
		return
	}

	syncedByPortfolio := make(map[int]float64)
	srows, err := h.db.QueryContext(ctx, `
		SELECT COALESCE(p.portfolio_id, pg.portfolio_id) AS fid, SUM(a.amount)
		FROM p3m_finance_actuals a
		JOIN p3m_projects p ON p.id = a.project_id
		LEFT JOIN p3m_programmes pg ON pg.id = p.programme_id
		JOIN p3m_finance_sync_runs r ON r.id = a.run_id
		WHERE a.match_status = 'matched' AND a.line_type = 'actual' AND r.status IN ('success','partial')
		  AND COALESCE(p.portfolio_id, pg.portfolio_id) IS NOT NULL
		GROUP BY fid`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for srows.Next() {
		var (
			fid int
			sum float64
		)
		if err := srows.Scan(&fid, &sum); err != nil {
			srows.Close()
			respondInternalError(w, r, err)
			return
		}
		syncedByPortfolio[fid] += sum
	}
	if err := srows.Err(); err != nil {
		srows.Close()
		respondInternalError(w, r, err)
		return
	}
	srows.Close()

	respondJSONOK(w, map[string]any{
		"configs":           configs,
		"unmatched_count":   unmatched,
		"synced_by_portfolio": syncedByPortfolio,
	})
}

// p3mRollupSyncedBlock returns the "synced" sub-block for a project rollup: total
// synced actual/commitment/forecast and the last synced period. It is nil when no
// matched rows exist so the key stays absent from the response.
func (h *P3MHandler) p3mRollupSyncedBlock(ctx context.Context, projectID int) (map[string]any, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT a.line_type, SUM(a.amount), MAX(a.period)
		FROM p3m_finance_actuals a JOIN p3m_finance_sync_runs r ON r.id = a.run_id
		WHERE a.project_id = ? AND a.match_status = 'matched' AND r.status IN ('success','partial')`+p3mFinanceEffectiveSubquery+`
		GROUP BY a.line_type`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]any)
	var lastPeriod string
	for rows.Next() {
		var (
			lineType string
			sum      float64
			period   string
		)
		if err := rows.Scan(&lineType, &sum, &period); err != nil {
			return nil, err
		}
		if period > lastPeriod {
			lastPeriod = period
		}
		switch lineType {
		case "actual":
			out["actual_total"] = sum
		case "commitment":
			out["commitment_total"] = sum
		case "forecast":
			out["forecast_total"] = sum
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	out["last_synced_period"] = lastPeriod
	return out, nil
}
