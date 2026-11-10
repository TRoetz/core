package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"windshift/internal/models"
)

const p3mReportSnapshotColumns = `id, entity_type, entity_id, period, overall_rag, category_rags, suggested_category_rags, rag_override_reasons, commentary, escalations, metrics, captured_by, captured_at`

func scanP3MReportSnapshot(row interface{ Scan(...any) error }) (models.P3MReportSnapshot, error) {
	var s models.P3MReportSnapshot
	var categoryRags, suggested, reasons, commentary, escalations, metrics sql.NullString
	var capturedBy sql.NullInt64
	err := row.Scan(&s.ID, &s.EntityType, &s.EntityID, &s.Period, &s.OverallRAG, &categoryRags,
		&suggested, &reasons, &commentary, &escalations, &metrics, &capturedBy, &s.CapturedAt)
	if err != nil {
		return s, err
	}
	if categoryRags.Valid {
		s.CategoryRags = &categoryRags.String
	}
	if suggested.Valid {
		s.SuggestedCategoryRags = &suggested.String
	}
	if reasons.Valid {
		s.RAGOverrideReasons = &reasons.String
	}
	if commentary.Valid {
		s.Commentary = &commentary.String
	}
	if escalations.Valid {
		s.Escalations = &escalations.String
	}
	if metrics.Valid {
		s.Metrics = &metrics.String
	}
	if capturedBy.Valid {
		v := int(capturedBy.Int64)
		s.CapturedBy = &v
	}
	return s, nil
}

const p3mToleranceProfileColumns = `id, scope, entity_id, schedule_days, budget_pct, risk_score, updated_at`

func scanP3MToleranceProfile(row interface{ Scan(...any) error }) (models.P3MToleranceProfile, error) {
	var t models.P3MToleranceProfile
	err := row.Scan(&t.ID, &t.Scope, &t.EntityID, &t.ScheduleDays, &t.BudgetPct, &t.RiskScore, &t.UpdatedAt)
	return t, err
}

// p3mDefaultTolerance is used when no profile exists for the scope (RPT-007).
func p3mDefaultTolerance() models.P3MToleranceProfile {
	return models.P3MToleranceProfile{Scope: "project", ScheduleDays: 14, BudgetPct: 10.0, RiskScore: 3}
}

// p3mToleranceFor loads the tolerance profile for a scope/entity or returns
// the defaults when none is configured.
func (h *P3MHandler) p3mToleranceFor(ctx context.Context, scope string, entityID int) models.P3MToleranceProfile {
	t, err := scanP3MToleranceProfile(h.db.QueryRowContext(ctx, `SELECT `+p3mToleranceProfileColumns+` FROM p3m_tolerance_profiles WHERE scope = ? AND entity_id = ?`, scope, entityID))
	if err != nil {
		return p3mDefaultTolerance()
	}
	return t
}

var p3mRAGRank = map[string]int{"green": 0, "amber": 1, "red": 2}

func p3mWorseRag(a, b string) string {
	if p3mRAGRank[b] > p3mRAGRank[a] {
		return b
	}
	return a
}

// p3mProjectCategoryRags computes the six tolerance-based category RAGs for a
// single project (RPT-007). Every category starts green and only escalates on
// concrete evidence: schedule slip vs baseline, budget overrun vs tolerance,
// open risk scores, latest change assessment rating, overdue benefit
// measurements, and high-impact changes active this week.
func (h *P3MHandler) p3mProjectCategoryRags(ctx context.Context, projectID int, tol models.P3MToleranceProfile) (map[string]string, error) {
	rags := map[string]string{"schedule": "green", "budget": "green", "scope": "green", "risk": "green", "benefits": "green", "change": "green"}

	// schedule: slip of the current target completion date against the latest
	// baseline's captured date.
	var snapText sql.NullString
	if err := h.db.QueryRowContext(ctx, `SELECT snapshot FROM p3m_baselines WHERE project_id = ? ORDER BY id DESC LIMIT 1`, projectID).Scan(&snapText); err == nil && snapText.Valid {
		var snap map[string]any
		if json.Unmarshal([]byte(snapText.String), &snap) == nil {
			if baseStr, ok := snap["target_completion_date"].(string); ok && baseStr != "" && baseStr != "null" {
				baseDate, perr := time.Parse(time.RFC3339, baseStr)
				if perr != nil {
					baseDate, _ = time.Parse("2006-01-02", baseStr)
				}
				var cur sql.NullString
				if err := h.db.QueryRowContext(ctx, `SELECT target_completion_date FROM p3m_projects WHERE id = ?`, projectID).Scan(&cur); err == nil && cur.Valid {
					curDate, cerr := time.Parse("2006-01-02", cur.String)
					if cerr != nil {
						curDate, _ = time.Parse(time.RFC3339, cur.String)
					}
					slipDays := int(curDate.Sub(baseDate).Hours() / 24)
					switch {
					case slipDays > 2*tol.ScheduleDays:
						rags["schedule"] = "red"
					case slipDays > tol.ScheduleDays:
						rags["schedule"] = "amber"
					}
				}
			}
		}
	}

	// budget: actual spend overrun against the approved budget.
	var approved, actual float64
	if err := h.db.QueryRowContext(ctx, `SELECT budget_approved, budget_actual FROM p3m_projects WHERE id = ?`, projectID).Scan(&approved, &actual); err == nil && approved > 0 {
		pct := (actual - approved) / approved * 100
		switch {
		case pct > 2*tol.BudgetPct:
			rags["budget"] = "red"
		case pct > tol.BudgetPct:
			rags["budget"] = "amber"
		}
	}

	// risk: open/mitigating/escalated risks against the tolerance score.
	rows, err := h.db.QueryContext(ctx, `SELECT impact_score FROM p3m_raid_items WHERE entity_type = 'project' AND entity_id = ? AND item_type = 'risk' AND status IN ('open', 'mitigating', 'escalated')`, projectID)
	if err != nil {
		return rags, err
	}
	maxImpact := 0
	atOrAbove := 0
	for rows.Next() {
		var score int
		if err := rows.Scan(&score); err != nil {
			rows.Close()
			return rags, err
		}
		if score > maxImpact {
			maxImpact = score
		}
		if score >= tol.RiskScore {
			atOrAbove++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return rags, err
	}
	rows.Close()
	switch {
	case maxImpact == 5 || atOrAbove >= 3:
		rags["risk"] = "red"
	case maxImpact >= tol.RiskScore:
		rags["risk"] = "amber"
	}

	// scope: latest change impact assessment rating.
	var rating sql.NullInt64
	if err := h.db.QueryRowContext(ctx, `SELECT overall_rating FROM p3m_change_assessments WHERE entity_type = 'project' AND entity_id = ? ORDER BY updated_at DESC LIMIT 1`, projectID).Scan(&rating); err == nil && rating.Valid {
		switch int(rating.Int64) {
		case 5:
			rags["scope"] = "red"
		case 4:
			rags["scope"] = "amber"
		}
	}

	// benefits: overdue measurement points on the project's benefits.
	today := time.Now().UTC().Format("2006-01-02")
	var overdue int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_benefit_measurements m JOIN p3m_benefits b ON b.id = m.benefit_id WHERE b.project_id = ? AND m.completed_at IS NULL AND m.due_date < ?`, projectID, today).Scan(&overdue); err == nil {
		switch {
		case overdue > 2:
			rags["benefits"] = "red"
		case overdue >= 1:
			rags["benefits"] = "amber"
		}
	}

	// change: high-impact calendar entries active this week.
	label := isoWeekString(time.Now().UTC())
	var maxImpactLevel sql.NullInt64
	if err := h.db.QueryRowContext(ctx, `SELECT MAX(impact_level) FROM p3m_change_calendar_entries WHERE entity_type = 'project' AND entity_id = ? AND start_week <= ? AND end_week >= ?`, projectID, label, label).Scan(&maxImpactLevel); err == nil && maxImpactLevel.Valid {
		switch int(maxImpactLevel.Int64) {
		case 5:
			rags["change"] = "red"
		case 4:
			rags["change"] = "amber"
		}
	}

	return rags, nil
}

// p3mEntityCategoryRags resolves the suggested category RAGs for any entity.
// Projects compute directly; programmes and portfolios aggregate their child
// projects with worst-RAG-wins (a portfolio uses its own tolerance profile).
func (h *P3MHandler) p3mEntityCategoryRags(ctx context.Context, entityType string, entityID int) (map[string]string, models.P3MToleranceProfile, error) {
	if entityType == "project" {
		tol := h.p3mToleranceFor(ctx, "project", entityID)
		rags, err := h.p3mProjectCategoryRags(ctx, entityID, tol)
		return rags, tol, err
	}

	var scope string
	switch entityType {
	case "programme":
		scope = "portfolio" // programmes inherit the parent portfolio's profile when present
	default:
		scope = "portfolio"
	}
	tol := p3mDefaultTolerance()
	if entityType == "portfolio" {
		tol = h.p3mToleranceFor(ctx, scope, entityID)
	} else if entityType == "programme" {
		var portfolioID sql.NullInt64
		if err := h.db.QueryRowContext(ctx, `SELECT portfolio_id FROM p3m_programmes WHERE id = ?`, entityID).Scan(&portfolioID); err == nil && portfolioID.Valid {
			tol = h.p3mToleranceFor(ctx, scope, int(portfolioID.Int64))
		}
	}

	var projectIDs []int
	query := `SELECT id FROM p3m_projects WHERE portfolio_id = ?`
	args := []any{entityID}
	if entityType == "programme" {
		query = `SELECT id FROM p3m_projects WHERE programme_id = ?`
	}
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, tol, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, tol, err
		}
		projectIDs = append(projectIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, tol, err
	}

	rags := map[string]string{"schedule": "green", "budget": "green", "scope": "green", "risk": "green", "benefits": "green", "change": "green"}
	for _, pid := range projectIDs {
		child, err := h.p3mProjectCategoryRags(ctx, pid, tol)
		if err != nil {
			return nil, tol, err
		}
		for cat, v := range child {
			rags[cat] = p3mWorseRag(rags[cat], v)
		}
	}
	return rags, tol, nil
}

// p3mSnapshotMetrics captures the point-in-time metrics stored with a report
// snapshot (RPT-005).
func (h *P3MHandler) p3mSnapshotMetrics(ctx context.Context, entityType string, entityID int) map[string]any {
	if entityType == "project" {
		var approved, actual float64
		var progress int
		var rag string
		out := map[string]any{}
		if err := h.db.QueryRowContext(ctx, `SELECT budget_approved, budget_actual, progress_percent, rag_status FROM p3m_projects WHERE id = ?`, entityID).Scan(&approved, &actual, &progress, &rag); err == nil {
			out["budget_approved"] = approved
			out["budget_actual"] = actual
			out["progress_percent"] = progress
			out["rag_status"] = rag
		}
		var openRaid int
		if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_raid_items WHERE entity_type = 'project' AND entity_id = ? AND status != 'closed'`, entityID).Scan(&openRaid); err == nil {
			out["open_raid_count"] = openRaid
		}
		return out
	}

	var projectCount int
	query := `SELECT COUNT(*) FROM p3m_projects WHERE portfolio_id = ?`
	if entityType == "programme" {
		query = `SELECT COUNT(*) FROM p3m_projects WHERE programme_id = ?`
	}
	if err := h.db.QueryRowContext(ctx, query, entityID).Scan(&projectCount); err != nil {
		return map[string]any{}
	}
	out := map[string]any{"project_count": projectCount}
	var green, amber, red int
	countQuery := `SELECT rag_status, COUNT(*) FROM p3m_projects WHERE portfolio_id = ? GROUP BY rag_status`
	if entityType == "programme" {
		countQuery = `SELECT rag_status, COUNT(*) FROM p3m_projects WHERE programme_id = ? GROUP BY rag_status`
	}
	rows, err := h.db.QueryContext(ctx, countQuery, entityID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			break
		}
		switch s {
		case "green":
			green = n
		case "amber":
			amber = n
		case "red":
			red = n
		}
	}
	out["projects_green"] = green
	out["projects_amber"] = amber
	out["projects_red"] = red
	return out
}

// CreateReportSnapshot handles POST /api/p3m/report-snapshots?entity_type=&entity_id=.
// The body carries the manual RAGs and commentary; suggested category RAGs and
// metrics are computed server-side at capture time (RPT-005/007). Categories
// where the manual choice differs from the suggestion record an override reason.
func (h *P3MHandler) CreateReportSnapshot(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	entityType := r.URL.Query().Get("entity_type")
	entityIDStr := r.URL.Query().Get("entity_id")
	switch entityType {
	case "project", "programme", "portfolio":
	default:
		respondValidationError(w, r, "entity_type must be project, programme or portfolio")
		return
	}
	entityID, ok := parseQueryInt(entityIDStr)
	if !ok || entityID <= 0 {
		respondValidationError(w, r, "entity_id is required")
		return
	}

	var in struct {
		Period             string          `json:"period"`
		OverallRAG         string          `json:"overall_rag"`
		CategoryRags       json.RawMessage `json:"category_rags"`
		RAGOverrideReasons map[string]string `json:"rag_override_reasons"`
		Commentary         *string         `json:"commentary"`
		Escalations        *string         `json:"escalations"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Period == "" {
		respondValidationError(w, r, "period is required (e.g. 2026-11)")
		return
	}
	switch in.OverallRAG {
	case "green", "amber", "red":
	default:
		respondValidationError(w, r, "overall_rag must be green, amber or red")
		return
	}

	suggested, _, err := h.p3mEntityCategoryRags(ctx, entityType, entityID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	suggestedJSON, _ := json.Marshal(suggested)

	var manual map[string]string
	categoryRagsStr := ""
	if len(in.CategoryRags) > 0 && string(in.CategoryRags) != "null" {
		if err := json.Unmarshal(in.CategoryRags, &manual); err != nil {
			respondValidationError(w, r, "category_rags must be a JSON object of category -> green|amber|red")
			return
		}
		for _, v := range manual {
			if _, known := p3mRAGRank[v]; !known {
				respondValidationError(w, r, "category_rags values must be green, amber or red")
				return
			}
		}
		b, _ := json.Marshal(manual)
		categoryRagsStr = string(b)
	}

	reasons := map[string]string{}
	for cat, sug := range suggested {
		if m, okM := manual[cat]; okM && m != sug {
			if reason, provided := in.RAGOverrideReasons[cat]; provided && reason != "" {
				reasons[cat] = reason
			} else {
				reasons[cat] = "manual override"
			}
		}
	}
	reasonsJSON, _ := json.Marshal(reasons)

	metricsJSON, _ := json.Marshal(h.p3mSnapshotMetrics(ctx, entityType, entityID))

	var categoryRagsPtr *string
	if categoryRagsStr != "" {
		categoryRagsPtr = &categoryRagsStr
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO p3m_report_snapshots (entity_type, entity_id, period, overall_rag, category_rags, suggested_category_rags, rag_override_reasons, commentary, escalations, metrics, captured_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entityType, entityID, in.Period, in.OverallRAG, categoryRagsPtr, string(suggestedJSON), string(reasonsJSON), in.Commentary, in.Escalations, string(metricsJSON), user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	s, err := scanP3MReportSnapshot(h.db.QueryRowContext(ctx, `SELECT `+p3mReportSnapshotColumns+` FROM p3m_report_snapshots WHERE id = ?`, int(id64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_report_snapshot_created", "p3m_report_snapshot", &s.ID, in.Period)
	respondJSONCreated(w, s)
}

// ListReportSnapshots handles GET /api/p3m/report-snapshots. Optional filters:
// entity_type and entity_id; newest first, capped at 100 rows.
func (h *P3MHandler) ListReportSnapshots(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	query := `SELECT ` + p3mReportSnapshotColumns + ` FROM p3m_report_snapshots WHERE 1=1`
	args := []any{}
	q := r.URL.Query()
	if v := q.Get("entity_type"); v != "" {
		query += ` AND entity_type = ?`
		args = append(args, v)
	}
	if v := q.Get("entity_id"); v != "" {
		id, okID := parseQueryInt(v)
		if !okID {
			respondValidationError(w, r, "entity_id must be an integer")
			return
		}
		query += ` AND entity_id = ?`
		args = append(args, id)
	}
	query += ` ORDER BY captured_at DESC LIMIT 100`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MReportSnapshot, 0)
	for rows.Next() {
		s, err := scanP3MReportSnapshot(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// GetReportSnapshot handles GET /api/p3m/report-snapshots/{id}.
func (h *P3MHandler) GetReportSnapshot(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	s, err := scanP3MReportSnapshot(h.db.QueryRowContext(r.Context(), `SELECT `+p3mReportSnapshotColumns+` FROM p3m_report_snapshots WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_report_snapshot")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, s)
}

// SuggestCategoryRags handles GET /api/p3m/report-snapshots/suggest. It returns
// the live tolerance-computed category RAGs (RPT-007) plus the tolerance
// profile in force, so clients can pre-fill a snapshot form and show why each
// category is where it is.
func (h *P3MHandler) SuggestCategoryRags(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	entityType := r.URL.Query().Get("entity_type")
	switch entityType {
	case "project", "programme", "portfolio":
	default:
		respondValidationError(w, r, "entity_type must be project, programme or portfolio")
		return
	}
	entityID, ok := parseQueryInt(r.URL.Query().Get("entity_id"))
	if !ok || entityID <= 0 {
		respondValidationError(w, r, "entity_id is required")
		return
	}

	suggested, tol, err := h.p3mEntityCategoryRags(ctx, entityType, entityID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, map[string]any{
		"suggested": suggested,
		"tolerance": map[string]any{
			"schedule_days": tol.ScheduleDays,
			"budget_pct":    tol.BudgetPct,
			"risk_score":    tol.RiskScore,
		},
	})
}

// p3mToleranceProfileInput is the create/update payload for a tolerance profile.
type p3mToleranceProfileInput struct {
	Scope        string  `json:"scope"`
	EntityID     int     `json:"entity_id"`
	ScheduleDays int     `json:"schedule_days"`
	BudgetPct    float64 `json:"budget_pct"`
	RiskScore    int     `json:"risk_score"`
}

func (h *P3MHandler) validateP3MToleranceProfileInput(w http.ResponseWriter, r *http.Request, in p3mToleranceProfileInput) bool {
	switch in.Scope {
	case "portfolio", "project":
	default:
		respondValidationError(w, r, "scope must be portfolio or project")
		return false
	}
	if in.EntityID <= 0 {
		respondValidationError(w, r, "entity_id is required")
		return false
	}
	if in.ScheduleDays < 0 {
		respondValidationError(w, r, "schedule_days must be >= 0")
		return false
	}
	if in.BudgetPct < 0 {
		respondValidationError(w, r, "budget_pct must be >= 0")
		return false
	}
	if in.RiskScore < 1 || in.RiskScore > 5 {
		respondValidationError(w, r, "risk_score must be between 1 and 5")
		return false
	}
	return true
}

// ListToleranceProfiles handles GET /api/p3m/tolerance-profiles. Optional
// filters: scope and entity_id.
func (h *P3MHandler) ListToleranceProfiles(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	query := `SELECT ` + p3mToleranceProfileColumns + ` FROM p3m_tolerance_profiles WHERE 1=1`
	args := []any{}
	q := r.URL.Query()
	if v := q.Get("scope"); v != "" {
		query += ` AND scope = ?`
		args = append(args, v)
	}
	if v := q.Get("entity_id"); v != "" {
		id, okID := parseQueryInt(v)
		if !okID {
			respondValidationError(w, r, "entity_id must be an integer")
			return
		}
		query += ` AND entity_id = ?`
		args = append(args, id)
	}
	query += ` ORDER BY scope, entity_id`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MToleranceProfile, 0)
	for rows.Next() {
		t, err := scanP3MToleranceProfile(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateToleranceProfile handles POST /api/p3m/tolerance-profiles. A profile is
// unique per (scope, entity_id), so a second create for the same pair updates
// it in place.
func (h *P3MHandler) CreateToleranceProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in p3mToleranceProfileInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.validateP3MToleranceProfileInput(w, r, in) {
		return
	}

	ctx := r.Context()
	if _, err := h.db.ExecContext(ctx, `
		INSERT INTO p3m_tolerance_profiles (scope, entity_id, schedule_days, budget_pct, risk_score)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(scope, entity_id) DO UPDATE SET
			schedule_days = excluded.schedule_days,
			budget_pct = excluded.budget_pct,
			risk_score = excluded.risk_score`, in.Scope, in.EntityID, in.ScheduleDays, in.BudgetPct, in.RiskScore); err != nil {
		respondInternalError(w, r, err)
		return
	}
	t, err := scanP3MToleranceProfile(h.db.QueryRowContext(ctx, `SELECT `+p3mToleranceProfileColumns+` FROM p3m_tolerance_profiles WHERE scope = ? AND entity_id = ?`, in.Scope, in.EntityID))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_tolerance_profile_saved", "p3m_tolerance_profile", &t.ID, in.Scope)
	respondJSONCreated(w, t)
}

// UpdateToleranceProfile handles PUT /api/p3m/tolerance-profiles/{id}.
func (h *P3MHandler) UpdateToleranceProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in p3mToleranceProfileInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.validateP3MToleranceProfileInput(w, r, in) {
		return
	}

	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_tolerance_profiles SET scope = ?, entity_id = ?, schedule_days = ?, budget_pct = ?, risk_score = ? WHERE id = ?`,
		in.Scope, in.EntityID, in.ScheduleDays, in.BudgetPct, in.RiskScore, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_tolerance_profile")
		return
	}
	t, err := scanP3MToleranceProfile(h.db.QueryRowContext(r.Context(), `SELECT `+p3mToleranceProfileColumns+` FROM p3m_tolerance_profiles WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_tolerance_profile_updated", "p3m_tolerance_profile", &id, in.Scope)
	respondJSONOK(w, t)
}

// DeleteToleranceProfile handles DELETE /api/p3m/tolerance-profiles/{id}. The
// scope then falls back to the default tolerances.
func (h *P3MHandler) DeleteToleranceProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_tolerance_profiles WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_tolerance_profile")
		return
	}
	logAudit(h.db, r, user, "p3m_tolerance_profile_deleted", "p3m_tolerance_profile", &id, "")
	respondJSONOK(w, map[string]any{"deleted": true})
}
