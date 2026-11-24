package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// OHP portfolio report (RPT-001..RPT-009): report dimensions validation,
// RAG history, status reports, ELT decisions, change capacity, reference
// values and the curated portfolio report row. Report assembly lives at the
// bottom of this file.
// ---------------------------------------------------------------------------

// p3mValidateProjectReportFields validates the enumerated project report
// dimensions (RPT-017/018). The free-text dimensions (business owner group,
// responsible group, portfolio value) are drawn from the reference values
// register and are not enum-checked.
func p3mValidateProjectReportFields(deliveryComplexity, investmentType, priority *string) error {
	if deliveryComplexity != nil {
		switch *deliveryComplexity {
		case "low", "med", "high":
		default:
			return fmt.Errorf("delivery_complexity must be low, med or high")
		}
	}
	if investmentType != nil {
		switch *investmentType {
		case "mandatory", "maintenance", "tactical", "strategic", "innovation":
		default:
			return fmt.Errorf("investment_type must be one of mandatory, maintenance, tactical, strategic or innovation")
		}
	}
	if priority != nil {
		switch *priority {
		case "low", "med", "high":
		default:
			return fmt.Errorf("priority must be low, med or high")
		}
	}
	return nil
}

var p3mPeriodPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

func p3mValidPeriod(p string) bool { return p3mPeriodPattern.MatchString(p) }

// --- RAG history ------------------------------------------------------------

// p3mRecordRagHistory appends one p3m_rag_history row when the entity's RAG
// differs from the latest recorded value (always records on first write).
// Failures never break the request - the mutation already committed.
func (h *P3MHandler) p3mRecordRagHistory(ctx context.Context, entityType string, entityID int, rag string, actor *models.User) {
	var last sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT rag_status FROM p3m_rag_history WHERE entity_type = ? AND entity_id = ? ORDER BY changed_at DESC, id DESC LIMIT 1`, entityType, entityID).Scan(&last)
	if err == nil && last.Valid && last.String == rag {
		return
	}
	var changedBy any
	if actor != nil {
		changedBy = actor.ID
	}
	_, _ = h.db.ExecContext(ctx, `INSERT INTO p3m_rag_history (entity_type, entity_id, rag_status, changed_by) VALUES (?, ?, ?, ?)`,
		entityType, entityID, rag, changedBy)
}

// GetProjectRagHistory handles GET /api/p3m/projects/{id}/rag-history: the
// project's RAG status changes, newest first.
func (h *P3MHandler) GetProjectRagHistory(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var exists int
	if err := h.db.QueryRowContext(r.Context(),`SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		respondNotFound(w, r, "p3m_project")
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT h.id, h.entity_type, h.entity_id, h.rag_status, h.changed_at, h.changed_by, u.username
		FROM p3m_rag_history h LEFT JOIN users u ON u.id = h.changed_by
		WHERE h.entity_type = 'project' AND h.entity_id = ?
		ORDER BY h.changed_at DESC, h.id DESC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	type ragHistoryRow struct {
		models.P3MRagHistory
		ChangedByUsername string `json:"changed_by_username,omitempty"`
	}
	out := make([]ragHistoryRow, 0)
	for rows.Next() {
		var row ragHistoryRow
		var changedBy sql.NullInt64
		if err := rows.Scan(&row.ID, &row.EntityType, &row.EntityID, &row.RagStatus, &row.ChangedAt, &changedBy, &row.ChangedByUsername); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if changedBy.Valid {
			v := int(changedBy.Int64)
			row.ChangedBy = &v
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// --- Project status reports (RPT-002) ----------------------------------------

const p3mStatusReportColumns = `id, project_id, period, issue, impact, next_steps, reported_by, reported_at`

func scanP3MStatusReport(row interface{ Scan(...any) error }) (models.P3MProjectStatusReport, error) {
	var s models.P3MProjectStatusReport
	var reportedBy sql.NullInt64
	err := row.Scan(&s.ID, &s.ProjectID, &s.Period, &s.Issue, &s.Impact, &s.NextSteps, &reportedBy, &s.ReportedAt)
	if err != nil {
		return s, err
	}
	if reportedBy.Valid {
		v := int(reportedBy.Int64)
		s.ReportedBy = &v
	}
	return s, nil
}

type p3mStatusReportInput struct {
	Period    string  `json:"period"`
	Issue     string  `json:"issue"`
	Impact    string  `json:"impact"`
	NextSteps string  `json:"next_steps"`
}

func (h *P3MHandler) p3mValidateStatusReportInput(w http.ResponseWriter, r *http.Request, in p3mStatusReportInput) bool {
	if !p3mValidPeriod(in.Period) {
		respondValidationError(w, r, "period must be YYYY-MM")
		return false
	}
	return true
}

// ListProjectStatusReports handles GET /api/p3m/projects/{id}/status-reports,
// newest first.
func (h *P3MHandler) ListProjectStatusReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var exists int
	if err := h.db.QueryRowContext(r.Context(),`SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		respondNotFound(w, r, "p3m_project")
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+p3mStatusReportColumns+` FROM p3m_project_status_reports WHERE project_id = ? ORDER BY period DESC, reported_at DESC, id DESC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MProjectStatusReport, 0)
	for rows.Next() {
		s, err := scanP3MStatusReport(rows)
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

// CreateProjectStatusReport handles POST /api/p3m/projects/{id}/status-reports.
// Create is an upsert per (project_id, period): re-reporting the same period
// replaces the row in place.
func (h *P3MHandler) CreateProjectStatusReport(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var exists int
	if err := h.db.QueryRowContext(r.Context(),`SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		respondNotFound(w, r, "p3m_project")
		return
	}
	var in p3mStatusReportInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidateStatusReportInput(w, r, in) {
		return
	}
	if _, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_project_status_reports (project_id, period, issue, impact, next_steps, reported_by, reported_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, period) DO UPDATE SET
			issue = excluded.issue,
			impact = excluded.impact,
			next_steps = excluded.next_steps,
			reported_by = excluded.reported_by,
			reported_at = excluded.reported_at`,
		id, in.Period, in.Issue, in.Impact, in.NextSteps, user.ID, time.Now().UTC()); err != nil {
		respondInternalError(w, r, err)
		return
	}
	s, err := scanP3MStatusReport(h.db.QueryRowContext(r.Context(), `SELECT `+p3mStatusReportColumns+` FROM p3m_project_status_reports WHERE project_id = ? AND period = ?`, id, in.Period))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.status_report.create", "p3m_project_status_report", &s.ID, fmt.Sprintf("project %d %s", id, in.Period), map[string]any{
		"project_id": id,
		"period":     in.Period,
		"issue":      in.Issue,
		"impact":     in.Impact,
		"next_steps": in.NextSteps,
	})
	respondJSONCreated(w, s)
}

// GetStatusReport handles GET /api/p3m/status-reports/{id}.
func (h *P3MHandler) GetStatusReport(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	s, err := scanP3MStatusReport(h.db.QueryRowContext(r.Context(), `SELECT `+p3mStatusReportColumns+` FROM p3m_project_status_reports WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project_status_report")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, s)
}

// UpdateStatusReport handles PUT /api/p3m/status-reports/{id}. The payload is
// a full record write (same shape as create); the owning project is fixed.
func (h *P3MHandler) UpdateStatusReport(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldS, err := scanP3MStatusReport(h.db.QueryRowContext(r.Context(), `SELECT `+p3mStatusReportColumns+` FROM p3m_project_status_reports WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project_status_report")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	var in p3mStatusReportInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidateStatusReportInput(w, r, in) {
		return
	}
	if in.Period != oldS.Period {
		var conflict int
		err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_project_status_reports WHERE project_id = ? AND period = ? AND id != ?`, oldS.ProjectID, in.Period, id).Scan(&conflict)
		if err == nil && conflict > 0 {
			respondConflict(w, r, fmt.Sprintf("project %d already has a status report for period %s", oldS.ProjectID, in.Period))
			return
		}
	}
	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_project_status_reports SET period = ?, issue = ?, impact = ?, next_steps = ?, reported_by = ?, reported_at = ? WHERE id = ?`,
		in.Period, in.Issue, in.Impact, in.NextSteps, user.ID, time.Now().UTC(), id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	s, err := scanP3MStatusReport(h.db.QueryRowContext(r.Context(), `SELECT `+p3mStatusReportColumns+` FROM p3m_project_status_reports WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.status_report.update", "p3m_project_status_report", &s.ID, fmt.Sprintf("project %d %s", s.ProjectID, s.Period), p3mAuditDiff(
		[3]any{"period", oldS.Period, in.Period},
		[3]any{"issue", oldS.Issue, in.Issue},
		[3]any{"impact", oldS.Impact, in.Impact},
		[3]any{"next_steps", oldS.NextSteps, in.NextSteps},
	))
	respondJSONOK(w, s)
}

// DeleteStatusReport handles DELETE /api/p3m/status-reports/{id}.
func (h *P3MHandler) DeleteStatusReport(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_project_status_reports WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_project_status_report")
		return
	}
	h.p3mAudit(r, user, "p3m.status_report.delete", "p3m_project_status_report", &id, "", map[string]any{"report_id": id})
	respondJSONOK(w, map[string]any{"deleted": true})
}

// --- ELT decisions (ELT-001/002/003) ------------------------------------------

const p3mELTDecisionColumns = `id, portfolio_id, project_id, title, why, decision_requested, conditions_until_approved, status, decided_by, decided_at, created_at, updated_at`

var p3mELTDecisionStatuses = map[string]bool{"pending": true, "approved": true, "rejected": true, "withdrawn": true}

func scanP3MELTDecision(row interface{ Scan(...any) error }) (models.P3MELTDecision, error) {
	var d models.P3MELTDecision
	var projectID sql.NullInt64
	var decidedBy sql.NullInt64
	var decidedAt sql.NullTime
	err := row.Scan(&d.ID, &d.PortfolioID, &projectID, &d.Title, &d.Why, &d.DecisionRequested, &d.ConditionsUntilApproved,
		&d.Status, &decidedBy, &decidedAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	if projectID.Valid {
		v := int(projectID.Int64)
		d.ProjectID = &v
	}
	if decidedBy.Valid {
		v := int(decidedBy.Int64)
		d.DecidedBy = &v
	}
	if decidedAt.Valid {
		v := decidedAt.Time
		d.DecidedAt = &v
	}
	return d, nil
}

type p3mELTDecisionInput struct {
	PortfolioID             int     `json:"portfolio_id"`
	ProjectID               *int    `json:"project_id"`
	Title                   string  `json:"title"`
	Why                     string  `json:"why"`
	DecisionRequested       string  `json:"decision_requested"`
	ConditionsUntilApproved string  `json:"conditions_until_approved"`
}

func (h *P3MHandler) p3mValidateELTDecisionInput(ctx context.Context, w http.ResponseWriter, r *http.Request, in p3mELTDecisionInput) bool {
	if in.PortfolioID == 0 {
		respondValidationError(w, r, "portfolio_id is required")
		return false
	}
	var pfCount int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_portfolios WHERE id = ?`, in.PortfolioID).Scan(&pfCount); err != nil || pfCount == 0 {
		respondNotFound(w, r, "p3m_portfolio")
		return false
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return false
	}
	if in.ProjectID != nil {
		var prCount int
		if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, *in.ProjectID).Scan(&prCount); err != nil || prCount == 0 {
			respondNotFound(w, r, "p3m_project")
			return false
		}
	}
	return true
}

// ListELTDecisions handles GET /api/p3m/elt-decisions?portfolio_id=.
func (h *P3MHandler) ListELTDecisions(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := `SELECT ` + p3mELTDecisionColumns + ` FROM p3m_elt_decisions`
	var args []any
	if v := r.URL.Query().Get("portfolio_id"); v != "" {
		id, ok := parseQueryInt(v)
		if !ok {
			respondBadRequest(w, r, "invalid portfolio_id")
			return
		}
		query += ` WHERE portfolio_id = ?`
		args = append(args, id)
	}
	query += ` ORDER BY CASE status WHEN 'pending' THEN 0 ELSE 1 END, updated_at DESC, id DESC`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MELTDecision, 0)
	for rows.Next() {
		d, err := scanP3MELTDecision(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateELTDecision handles POST /api/p3m/elt-decisions. New decisions start
// pending.
func (h *P3MHandler) CreateELTDecision(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in p3mELTDecisionInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidateELTDecisionInput(r.Context(), w, r, in) {
		return
	}
	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_elt_decisions (portfolio_id, project_id, title, why, decision_requested, conditions_until_approved, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
		in.PortfolioID, nullInt(in.ProjectID), in.Title, in.Why, in.DecisionRequested, in.ConditionsUntilApproved, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	d, err := scanP3MELTDecision(h.db.QueryRowContext(r.Context(), `SELECT `+p3mELTDecisionColumns+` FROM p3m_elt_decisions WHERE id = ?`, id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.elt_decision.create", "p3m_elt_decision", &d.ID, d.Title, map[string]any{
		"portfolio_id":     in.PortfolioID,
		"project_id":       nullInt(in.ProjectID),
		"decision_requested": in.DecisionRequested,
	})
	respondJSONCreated(w, d)
}

// GetELTDecision handles GET /api/p3m/elt-decisions/{id}.
func (h *P3MHandler) GetELTDecision(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	d, err := scanP3MELTDecision(h.db.QueryRowContext(r.Context(), `SELECT `+p3mELTDecisionColumns+` FROM p3m_elt_decisions WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_elt_decision")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, d)
}

// UpdateELTDecision handles PUT /api/p3m/elt-decisions/{id}. The payload is a
// full record write (same shape as create); status moves go through the
// decision endpoint.
func (h *P3MHandler) UpdateELTDecision(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldD, err := scanP3MELTDecision(h.db.QueryRowContext(r.Context(), `SELECT `+p3mELTDecisionColumns+` FROM p3m_elt_decisions WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_elt_decision")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	var in p3mELTDecisionInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidateELTDecisionInput(r.Context(), w, r, in) {
		return
	}
	now := time.Now().UTC()
	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_elt_decisions SET portfolio_id = ?, project_id = ?, title = ?, why = ?, decision_requested = ?, conditions_until_approved = ?, updated_at = ? WHERE id = ?`,
		in.PortfolioID, nullInt(in.ProjectID), in.Title, in.Why, in.DecisionRequested, in.ConditionsUntilApproved, now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	d, err := scanP3MELTDecision(h.db.QueryRowContext(r.Context(), `SELECT `+p3mELTDecisionColumns+` FROM p3m_elt_decisions WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.elt_decision.update", "p3m_elt_decision", &d.ID, d.Title, p3mAuditDiff(
		[3]any{"portfolio_id", oldD.PortfolioID, in.PortfolioID},
		[3]any{"project_id", nullInt(oldD.ProjectID), nullInt(in.ProjectID)},
		[3]any{"title", oldD.Title, in.Title},
		[3]any{"why", oldD.Why, in.Why},
		[3]any{"decision_requested", oldD.DecisionRequested, in.DecisionRequested},
		[3]any{"conditions_until_approved", oldD.ConditionsUntilApproved, in.ConditionsUntilApproved},
	))
	respondJSONOK(w, d)
}

// DeleteELTDecision handles DELETE /api/p3m/elt-decisions/{id}.
func (h *P3MHandler) DeleteELTDecision(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_elt_decisions WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_elt_decision")
		return
	}
	h.p3mAudit(r, user, "p3m.elt_decision.delete", "p3m_elt_decision", &id, "", map[string]any{"decision_id": id})
	respondJSONOK(w, map[string]any{"deleted": true})
}

// DecideELTDecision handles POST /api/p3m/elt-decisions/{id}/decision with
// body {status: approved|rejected}. Only pending decisions can be decided.
func (h *P3MHandler) DecideELTDecision(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Status != "approved" && in.Status != "rejected" {
		respondValidationError(w, r, "status must be approved or rejected")
		return
	}
	oldD, err := scanP3MELTDecision(h.db.QueryRowContext(r.Context(), `SELECT `+p3mELTDecisionColumns+` FROM p3m_elt_decisions WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_elt_decision")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if oldD.Status != "pending" {
		respondConflict(w, r, "only pending ELT decisions can be decided")
		return
	}
	now := time.Now().UTC()
	if _, err := h.db.ExecContext(r.Context(), `UPDATE p3m_elt_decisions SET status = ?, decided_by = ?, decided_at = ?, updated_at = ? WHERE id = ?`,
		in.Status, user.ID, now, now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	d, err := scanP3MELTDecision(h.db.QueryRowContext(r.Context(), `SELECT `+p3mELTDecisionColumns+` FROM p3m_elt_decisions WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.elt_decision.decide", "p3m_elt_decision", &d.ID, d.Title, map[string]any{
		"status": in.Status,
	})
	respondJSONOK(w, d)
}

// --- Change capacity (RPT-008) ------------------------------------------------

const p3mChangeCapacityColumns = `id, business_group, month, posture, notes`

var p3mChangeCapacityPostures = map[string]bool{"proceed": true, "caution": true, "pause": true, "reduce": true}

func scanP3MChangeCapacity(row interface{ Scan(...any) error }) (models.P3MChangeCapacity, error) {
	var c models.P3MChangeCapacity
	var notes sql.NullString
	err := row.Scan(&c.ID, &c.BusinessGroup, &c.Month, &c.Posture, &notes)
	if err != nil {
		return c, err
	}
	if notes.Valid {
		c.Notes = &notes.String
	}
	return c, nil
}

// ListChangeCapacity handles GET /api/p3m/change-capacity?from_month=&to_month=
// (both YYYY-MM, inclusive).
func (h *P3MHandler) ListChangeCapacity(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := `SELECT ` + p3mChangeCapacityColumns + ` FROM p3m_change_capacity`
	var conds []string
	var args []any
	q := r.URL.Query()
	if v := q.Get("from_month"); v != "" {
		if !p3mValidPeriod(v) {
			respondValidationError(w, r, "from_month must be YYYY-MM")
			return
		}
		conds = append(conds, "month >= ?")
		args = append(args, v)
	}
	if v := q.Get("to_month"); v != "" {
		if !p3mValidPeriod(v) {
			respondValidationError(w, r, "to_month must be YYYY-MM")
			return
		}
		conds = append(conds, "month <= ?")
		args = append(args, v)
	}
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY business_group, month`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MChangeCapacity, 0)
	for rows.Next() {
		c, err := scanP3MChangeCapacity(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// UpsertChangeCapacity handles PUT /api/p3m/change-capacity with body
// {rows: [{business_group, month, posture, notes}]}: an upsert per unique
// (business_group, month) pair.
func (h *P3MHandler) UpsertChangeCapacity(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Rows []struct {
			BusinessGroup string  `json:"business_group"`
			Month         string  `json:"month"`
			Posture       string  `json:"posture"`
			Notes         *string `json:"notes"`
		} `json:"rows"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if len(in.Rows) == 0 {
		respondValidationError(w, r, "rows is required and must not be empty")
		return
	}
	seen := make(map[string]bool, len(in.Rows))
	for i := range in.Rows {
		row := &in.Rows[i]
		if strings.TrimSpace(row.BusinessGroup) == "" {
			respondValidationError(w, r, "rows business_group must be a non-empty string")
			return
		}
		if !p3mValidPeriod(row.Month) {
			respondValidationError(w, r, "rows month must be YYYY-MM")
			return
		}
		if !p3mChangeCapacityPostures[row.Posture] {
			respondValidationError(w, r, "rows posture must be proceed, caution, pause or reduce")
			return
		}
		key := row.BusinessGroup + "\x00" + row.Month
		if seen[key] {
			respondValidationError(w, r, "duplicate rows entry for "+row.BusinessGroup+" "+row.Month)
			return
		}
		seen[key] = true
	}
	for i := range in.Rows {
		row := &in.Rows[i]
		if _, err := h.db.ExecContext(r.Context(), `
			INSERT INTO p3m_change_capacity (business_group, month, posture, notes)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(business_group, month) DO UPDATE SET
				posture = excluded.posture,
				notes = excluded.notes`,
			row.BusinessGroup, row.Month, row.Posture, row.Notes); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	h.p3mAudit(r, user, "p3m.change_capacity.upsert", "p3m_change_capacity", nil, fmt.Sprintf("%d cells", len(in.Rows)), map[string]any{
		"rows": len(in.Rows),
	})
	respondJSONOK(w, map[string]any{"updated": len(in.Rows)})
}

// DeleteChangeCapacity handles DELETE /api/p3m/change-capacity?business_group=&month=.
func (h *P3MHandler) DeleteChangeCapacity(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	group := r.URL.Query().Get("business_group")
	month := r.URL.Query().Get("month")
	if group == "" || month == "" {
		respondValidationError(w, r, "business_group and month query parameters are required")
		return
	}
	if !p3mValidPeriod(month) {
		respondValidationError(w, r, "month must be YYYY-MM")
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_change_capacity WHERE business_group = ? AND month = ?`, group, month)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_change_capacity")
		return
	}
	h.p3mAudit(r, user, "p3m.change_capacity.delete", "p3m_change_capacity", nil, group+" "+month, nil)
	respondJSONOK(w, map[string]any{"deleted": true})
}

// --- Reference values ---------------------------------------------------------

var p3mReferenceValueCategories = map[string]bool{"business_group": true, "theme": true, "cso": true, "benefit_category": true}

// ListReferenceValues handles GET /api/p3m/reference-values?category=.
func (h *P3MHandler) ListReferenceValues(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := `SELECT id, category, value FROM p3m_reference_values`
	var args []any
	if v := r.URL.Query().Get("category"); v != "" {
		if !p3mReferenceValueCategories[v] {
			respondValidationError(w, r, "category must be business_group, theme, cso or benefit_category")
			return
		}
		query += ` WHERE category = ?`
		args = append(args, v)
	}
	query += ` ORDER BY category, value`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MReferenceValue, 0)
	for rows.Next() {
		var v models.P3MReferenceValue
		if err := rows.Scan(&v.ID, &v.Category, &v.Value); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// ReplaceReferenceValues handles PUT /api/p3m/reference-values (admin): full
// replacement of one category's curated value list.
func (h *P3MHandler) ReplaceReferenceValues(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Category string   `json:"category"`
		Values   []string `json:"values"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !p3mReferenceValueCategories[in.Category] {
		respondValidationError(w, r, "category must be business_group, theme, cso or benefit_category")
		return
	}
	seen := make(map[string]bool, len(in.Values))
	for i := range in.Values {
		v := strings.TrimSpace(in.Values[i])
		if v == "" {
			respondValidationError(w, r, "values must be non-empty strings")
			return
		}
		if seen[v] {
			respondValidationError(w, r, "duplicate value "+v)
			return
		}
		in.Values[i] = v
		seen[v] = true
	}
	tx, err := h.db.Begin()
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM p3m_reference_values WHERE category = ?`, in.Category); err != nil {
		respondInternalError(w, r, err)
		return
	}
	for _, v := range in.Values {
		if _, err := tx.Exec(`INSERT INTO p3m_reference_values (category, value) VALUES (?, ?)`, in.Category, v); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.reference_values.replace", "p3m_reference_value", nil, in.Category, map[string]any{
		"category": in.Category,
		"count":    len(in.Values),
	})
	respondJSONOK(w, map[string]any{"replaced": len(in.Values)})
}

// --- Curated portfolio report (RPT-009) ----------------------------------------

const p3mCuratedReportColumns = `id, portfolio_id, period, executive_commentary, key_pressures, delivery_confidence_pct, confidence_lenses, programme_highlights, change_key_messages, published_by, published_at, created_at, updated_at`

// p3mCuratedProgrammeHighlight is one curated programme highlight row
// {programme_id, commentary}.
type p3mCuratedProgrammeHighlight struct {
	ProgrammeID int    `json:"programme_id"`
	Commentary  string `json:"commentary"`
}

// p3mCuratedReportView is the API shape of a curated portfolio report row
// with its JSON columns decoded.
type p3mCuratedReportView struct {
	ID                    int                          `json:"id"`
	PortfolioID           int                          `json:"portfolio_id"`
	Period                string                       `json:"period"`
	ExecutiveCommentary   string                       `json:"executive_commentary"`
	KeyPressures          *models.P3MReportKeyPressures `json:"key_pressures"`
	DeliveryConfidencePct *float64                     `json:"delivery_confidence_pct"`
	ConfidenceLenses      []models.P3MReportConfidenceLens `json:"confidence_lenses"`
	ProgrammeHighlights   []p3mCuratedProgrammeHighlight   `json:"programme_highlights"`
	ChangeKeyMessages     string                       `json:"change_key_messages"`
	PublishedBy           *int                         `json:"published_by,omitempty"`
	PublishedAt           *time.Time                   `json:"published_at,omitempty"`
	CreatedAt             time.Time                    `json:"created_at"`
	UpdatedAt             time.Time                    `json:"updated_at"`
}

func scanP3MPortfolioReportView(row interface{ Scan(...any) error }) (p3mCuratedReportView, error) {
	var v p3mCuratedReportView
	var keyPressures, lenses, highlights sql.NullString
	var confidence sql.NullFloat64
	var publishedBy sql.NullInt64
	var publishedAt sql.NullTime
	err := row.Scan(&v.ID, &v.PortfolioID, &v.Period, &v.ExecutiveCommentary, &keyPressures, &confidence,
		&lenses, &highlights, &v.ChangeKeyMessages, &publishedBy, &publishedAt, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return v, err
	}
	if keyPressures.Valid {
		var kp models.P3MReportKeyPressures
		if err := json.Unmarshal([]byte(keyPressures.String), &kp); err == nil {
			v.KeyPressures = &kp
		}
	}
	if confidence.Valid {
		v.DeliveryConfidencePct = &confidence.Float64
	}
	if lenses.Valid {
		var ls []models.P3MReportConfidenceLens
		if err := json.Unmarshal([]byte(lenses.String), &ls); err == nil {
			v.ConfidenceLenses = ls
		}
	}
	if highlights.Valid {
		var hs []p3mCuratedProgrammeHighlight
		if err := json.Unmarshal([]byte(highlights.String), &hs); err == nil {
			v.ProgrammeHighlights = hs
		}
	}
	if publishedBy.Valid {
		pb := int(publishedBy.Int64)
		v.PublishedBy = &pb
	}
	if publishedAt.Valid {
		pa := publishedAt.Time
		v.PublishedAt = &pa
	}
	if v.ConfidenceLenses == nil {
		v.ConfidenceLenses = []models.P3MReportConfidenceLens{}
	}
	if v.ProgrammeHighlights == nil {
		v.ProgrammeHighlights = []p3mCuratedProgrammeHighlight{}
	}
	return v, nil
}

// GetCuratedPortfolioReport handles GET /api/p3m/portfolios/{id}/
// portfolio-report/curated?period=YYYY-MM (404 when the row is absent).
func (h *P3MHandler) GetCuratedPortfolioReport(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		period = time.Now().UTC().Format("2006-01")
	}
	if !p3mValidPeriod(period) {
		respondValidationError(w, r, "period must be YYYY-MM")
		return
	}
	row := h.db.QueryRowContext(r.Context(), `SELECT `+p3mCuratedReportColumns+` FROM p3m_portfolio_reports WHERE portfolio_id = ? AND period = ?`, id, period)
	v, err := scanP3MPortfolioReportView(row)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_portfolio_report")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, v)
}

// UpsertCuratedPortfolioReport handles PUT /api/p3m/portfolios/{id}/
// portfolio-report/curated: upsert per (portfolio_id, period), stamping
// published_by/published_at.
func (h *P3MHandler) UpsertCuratedPortfolioReport(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var pfCount int
	if err := h.db.QueryRowContext(r.Context(),`SELECT COUNT(*) FROM p3m_portfolios WHERE id = ?`, id).Scan(&pfCount); err != nil || pfCount == 0 {
		respondNotFound(w, r, "p3m_portfolio")
		return
	}
	var in struct {
		Period                string                      `json:"period"`
		ExecutiveCommentary   string                      `json:"executive_commentary"`
		KeyPressures          *models.P3MReportKeyPressures `json:"key_pressures"`
		DeliveryConfidencePct *float64                    `json:"delivery_confidence_pct"`
		ConfidenceLenses      []models.P3MReportConfidenceLens `json:"confidence_lenses"`
		ProgrammeHighlights   []p3mCuratedProgrammeHighlight   `json:"programme_highlights"`
		ChangeKeyMessages     string                      `json:"change_key_messages"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !p3mValidPeriod(in.Period) {
		respondValidationError(w, r, "period must be YYYY-MM")
		return
	}
	if in.DeliveryConfidencePct != nil && (*in.DeliveryConfidencePct < 0 || *in.DeliveryConfidencePct > 100) {
		respondValidationError(w, r, "delivery_confidence_pct must be between 0 and 100")
		return
	}
	for i := range in.ConfidenceLenses {
		lens := &in.ConfidenceLenses[i]
		if strings.TrimSpace(lens.Key) == "" {
			respondValidationError(w, r, "confidence_lenses key must be a non-empty string")
			return
		}
		if lens.Rag != "green" && lens.Rag != "amber" && lens.Rag != "red" {
			respondValidationError(w, r, "confidence_lenses rag must be green, amber or red")
			return
		}
	}
	ctx := r.Context()
	for i := range in.ProgrammeHighlights {
		var pgCount int
		if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_programmes WHERE id = ?`, in.ProgrammeHighlights[i].ProgrammeID).Scan(&pgCount); err != nil || pgCount == 0 {
			respondNotFound(w, r, "p3m_programme")
			return
		}
	}

	var keyPressuresText, lensesText, highlightsText any
	if in.KeyPressures != nil {
		if b, err := json.Marshal(in.KeyPressures); err == nil {
			keyPressuresText = string(b)
		}
	}
	if in.ConfidenceLenses != nil {
		if b, err := json.Marshal(in.ConfidenceLenses); err == nil {
			lensesText = string(b)
		}
	}
	if in.ProgrammeHighlights != nil {
		if b, err := json.Marshal(in.ProgrammeHighlights); err == nil {
			highlightsText = string(b)
		}
	}
	now := time.Now().UTC()
	if _, err := h.db.ExecContext(ctx, `
		INSERT INTO p3m_portfolio_reports (portfolio_id, period, executive_commentary, key_pressures, delivery_confidence_pct, confidence_lenses, programme_highlights, change_key_messages, published_by, published_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(portfolio_id, period) DO UPDATE SET
			executive_commentary = excluded.executive_commentary,
			key_pressures = excluded.key_pressures,
			delivery_confidence_pct = excluded.delivery_confidence_pct,
			confidence_lenses = excluded.confidence_lenses,
			programme_highlights = excluded.programme_highlights,
			change_key_messages = excluded.change_key_messages,
			published_by = excluded.published_by,
			published_at = excluded.published_at,
			updated_at = excluded.updated_at`,
		id, in.Period, in.ExecutiveCommentary, keyPressuresText, in.DeliveryConfidencePct, lensesText, highlightsText, in.ChangeKeyMessages,
		user.ID, now, now, now); err != nil {
		respondInternalError(w, r, err)
		return
	}
	v, err := scanP3MPortfolioReportView(h.db.QueryRowContext(ctx, `SELECT `+p3mCuratedReportColumns+` FROM p3m_portfolio_reports WHERE portfolio_id = ? AND period = ?`, id, in.Period))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.portfolio_report.upsert", "p3m_portfolio_report", &v.ID, fmt.Sprintf("portfolio %d %s", id, in.Period), map[string]any{
		"portfolio_id":     id,
		"period":           in.Period,
		"delivery_confidence_pct": in.DeliveryConfidencePct,
	})
	respondJSONOK(w, v)
}

// ---------------------------------------------------------------------------
// OHP portfolio report — assembly (RPT-014/015)
// ---------------------------------------------------------------------------

// p3mReportInvestmentRef identifies one member of the investment set (a
// non-exit-stage project or any programme).
type p3mReportInvestmentRef struct {
	kind   string // project | programme
	id     int
	name   string
	stage  string // projects only
	onHold bool   // projects only
}

// p3mReportProjectRow is a raw p3m_projects row for report assembly.
type p3mReportProjectRow struct {
	ID                   int
	PortfolioID          *int
	ProgrammeID          *int
	Title                string
	ExternalRef          *string
	CurrentStage         string
	ProgressPercent      int
	TargetCompletionDate *time.Time
	RagStatus            string
	BusinessOwnerGroup   *string
	ResponsibleGroup     *string
	DeliveryComplexity   *string
	InvestmentType       *string
	Priority             *string
	PortfolioValue       *string
	IsCEKPI              bool
	OnHold               bool
	UpdatedAt            time.Time
}

func scanP3MReportProjectRow(row interface{ Scan(...any) error }) (p3mReportProjectRow, error) {
	var p p3mReportProjectRow
	err := row.Scan(&p.ID, &p.PortfolioID, &p.ProgrammeID, &p.Title, &p.ExternalRef, &p.CurrentStage, &p.ProgressPercent, &p.TargetCompletionDate, &p.RagStatus, &p.BusinessOwnerGroup, &p.ResponsibleGroup, &p.DeliveryComplexity, &p.InvestmentType, &p.PortfolioValue, &p.Priority, &p.IsCEKPI, &p.OnHold, &p.UpdatedAt)
	return p, err
}

// p3mReportProgrammeRow is a raw p3m_programmes row for report assembly.
type p3mReportProgrammeRow struct {
	ID                 int
	PortfolioID        int
	Title              string
	TargetEndDate      *time.Time
	RagStatus          string
	BusinessOwnerGroup *string
	ResponsibleGroup   *string
	UpdatedAt          time.Time
}

func scanP3MReportProgrammeRow(row interface{ Scan(...any) error }) (p3mReportProgrammeRow, error) {
	var p p3mReportProgrammeRow
	err := row.Scan(&p.ID, &p.PortfolioID, &p.Title, &p.TargetEndDate, &p.RagStatus, &p.BusinessOwnerGroup, &p.ResponsibleGroup, &p.UpdatedAt)
	return p, err
}

// p3mReportDisplayRag derives the display RAG for an investment: "tbd" when
// on hold or outside implement_deliver, otherwise the stored RAG. Programmes
// carry no stage / on-hold fields, so their display RAG is always "tbd".
func p3mReportDisplayRag(isProgramme, onHold bool, stage, rag string) string {
	if isProgramme || onHold || stage != "implement_deliver" {
		return "tbd"
	}
	return rag
}

// p3mReportRagTrendRank orders display RAGs for trend comparison.
var p3mReportRagTrendRank = map[string]int{"tbd": -1, "green": 0, "amber": 1, "red": 2}

var p3mReportImpactLabels = []string{"unknown", "minimal", "low", "moderate", "high", "severe"}

// p3mReportCategoryNames is the fixed display order of the tolerance
// categories on the red/amber detail slide.
var p3mReportCategoryNames = []struct{ Key, Name string }{
	{"schedule", "Time"},
	{"budget", "Cost"},
	{"scope", "Scope"},
	{"risk", "Risk"},
	{"benefits", "Benefits"},
	{"change", "Change"},
	{"resources", "Resources"},
}

// p3mISOWeekMonday parses an ISO week such as "2026-W45" into its Monday.
// time.Parse does not support ISO 8601 week dates ("2006-W01" is read as
// month "01"), so the Monday is derived from the calendar: January 4 always
// lies in ISO week 1 of its year, and December 28 in the last week.
func p3mISOWeekMonday(week string) (time.Time, bool) {
	yearPart, weekPart, ok := strings.Cut(week, "-W")
	if !ok || len(weekPart) != 2 {
		return time.Time{}, false
	}
	year, err := strconv.Atoi(yearPart)
	if err != nil {
		return time.Time{}, false
	}
	weekNo, err := strconv.Atoi(weekPart)
	if err != nil || weekNo < 1 {
		return time.Time{}, false
	}
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	// Go's Weekday counts Sunday as 0; the days back to the Monday of the
	// same ISO week are (weekday+6)%7.
	mondayOfWeek1 := jan4.AddDate(0, 0, -((int(jan4.Weekday())+6)%7))
	_, weeksInYear := time.Date(year, time.December, 28, 0, 0, 0, 0, time.UTC).ISOWeek()
	if weekNo > weeksInYear {
		return time.Time{}, false
	}
	return mondayOfWeek1.AddDate(0, 0, (weekNo-1)*7), true
}

// p3mISOWeeks enumerates the ISO weeks from start to end, inclusive.
func p3mISOWeeks(start, end string) []string {
	s, ok := p3mISOWeekMonday(start)
	if !ok {
		return nil
	}
	e, ok := p3mISOWeekMonday(end)
	if !ok || e.Before(s) {
		return nil
	}
	var out []string
	for t := s; !t.After(e); t = t.AddDate(0, 0, 7) {
		y, w := t.ISOWeek()
		out = append(out, fmt.Sprintf("%d-W%02d", y, w))
	}
	return out
}

// p3mReportMonths returns the six-month window as_of+1 … as_of+6.
func p3mReportMonths(asOf string) []string {
	yStr, mStr, _ := strings.Cut(asOf, "-")
	y, _ := strconv.Atoi(yStr)
	m, _ := strconv.Atoi(mStr)
	months := make([]string, 0, 6)
	for i := 1; i <= 6; i++ {
		months = append(months, time.Date(y, time.Month(m)+time.Month(i), 1, 0, 0, 0, 0, time.UTC).Format("2006-01"))
	}
	return months
}

// p3mReportProjects loads the portfolio tree's projects using the same
// portfolio resolution as the executive summary.
func (h *P3MHandler) p3mReportProjects(ctx context.Context, portfolioID int) ([]p3mReportProjectRow, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, portfolio_id, programme_id, title, external_ref, current_stage, progress_percent, target_completion_date, rag_status, business_owner_group, responsible_group, delivery_complexity, investment_type, portfolio_value, priority, is_ce_kpi, on_hold, updated_at
		FROM p3m_projects
		WHERE COALESCE(portfolio_id, (SELECT portfolio_id FROM p3m_programmes WHERE id = programme_id)) = ?
		ORDER BY title`, portfolioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]p3mReportProjectRow, 0)
	for rows.Next() {
		p, err := scanP3MReportProjectRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// p3mReportProgrammes loads the portfolio's programmes.
func (h *P3MHandler) p3mReportProgrammes(ctx context.Context, portfolioID int) ([]p3mReportProgrammeRow, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, portfolio_id, title, target_end_date, rag_status, business_owner_group, responsible_group, updated_at
		FROM p3m_programmes WHERE portfolio_id = ? ORDER BY title`, portfolioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]p3mReportProgrammeRow, 0)
	for rows.Next() {
		p, err := scanP3MReportProgrammeRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// p3mReportProgrammeItemStats returns per-project milestone/task done+total
// counts over the projects' linked workspaces. Milestones are items whose
// item type is "Milestone"; everything else is a task. An item is done when
// its status belongs to a completed status category
// (status_categories.is_completed) — the core module's authoritative
// "progress = 100" equivalent (items carry no progress column).
func (h *P3MHandler) p3mReportProgrammeItemStats(ctx context.Context, projectIDs []int) (map[int]models.P3MReportCounts, map[int]models.P3MReportCounts, error) {
	milestones := map[int]models.P3MReportCounts{}
	tasks := map[int]models.P3MReportCounts{}
	if len(projectIDs) == 0 {
		return milestones, tasks, nil
	}
	args := make([]any, len(projectIDs))
	for i, id := range projectIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT wpw.p3m_project_id,
		       CASE WHEN LOWER(COALESCE(it.name, '')) = 'milestone' THEN 1 ELSE 0 END,
		       COALESCE(sc.is_completed, FALSE)
		FROM workspace_p3m_projects wpw
		JOIN items i ON i.workspace_id = wpw.workspace_id
		LEFT JOIN item_types it ON it.id = i.item_type_id
		LEFT JOIN statuses s ON s.id = i.status_id
		LEFT JOIN status_categories sc ON sc.id = s.category_id
		WHERE wpw.p3m_project_id IN (`+questionPlaceholders(len(projectIDs))+`)`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			projectID int
			milestone int
			done      bool
		)
		if err := rows.Scan(&projectID, &milestone, &done); err != nil {
			return nil, nil, err
		}
		target := tasks
		if milestone == 1 {
			target = milestones
		}
		c := target[projectID]
		c.Total++
		if done {
			c.Done++
		}
		target[projectID] = c
	}
	return milestones, tasks, rows.Err()
}

// p3mReportRagTrend compares the current display-RAG rank with the rank of
// the newest history row at or before the month before as_of (fallback: the
// earliest row). Nil when the entity has no history rows.
func (h *P3MHandler) p3mReportRagTrend(ctx context.Context, entityType string, entityID int, displayRag, asOf string) *string {
	yStr, mStr, _ := strings.Cut(asOf, "-")
	y, _ := strconv.Atoi(yStr)
	m, _ := strconv.Atoi(mStr)
	cutoff := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC) // start of as_of
	rows, err := h.db.QueryContext(ctx, `SELECT rag_status, changed_at FROM p3m_rag_history WHERE entity_type = ? AND entity_id = ? ORDER BY changed_at DESC, id DESC`, entityType, entityID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type histRow struct{ rag string; at time.Time }
	var hist []histRow
	for rows.Next() {
		var hr histRow
		if err := rows.Scan(&hr.rag, &hr.at); err != nil {
			return nil
		}
		hist = append(hist, hr)
	}
	if len(hist) == 0 {
		return nil
	}
	past := hist[len(hist)-1]
	for _, hr := range hist {
		if hr.at.Before(cutoff) {
			past = hr
			break
		}
	}
	cur, okC := p3mReportRagTrendRank[displayRag]
	prev, okP := p3mReportRagTrendRank[past.rag]
	if !okC || !okP {
		return nil
	}
	var trend string
	switch {
	case cur < prev:
		trend = "improved"
	case cur > prev:
		trend = "worsened"
	default:
		trend = "unchanged"
	}
	return &trend
}

// buildP3MPortfolioReportDocument assembles the full OHP portfolio report for
// one portfolio as at asOf (YYYY-MM); closedSince (YYYY-MM-DD) filters the
// closed section when non-empty. Returns sql.ErrNoRows when the portfolio
// does not exist.
func (h *P3MHandler) buildP3MPortfolioReportDocument(ctx context.Context, portfolioID int, asOf, closedSince string) (models.P3MPortfolioReportDocument, error) {
	var doc models.P3MPortfolioReportDocument
	doc.AsOf = asOf
	doc.GeneratedAt = time.Now().UTC()

	var pfTitle string
	if err := h.db.QueryRowContext(ctx, `SELECT title FROM p3m_portfolios WHERE id = ?`, portfolioID).Scan(&pfTitle); err != nil {
		return doc, err
	}
	doc.Portfolio = models.P3MReportPortfolioRef{ID: portfolioID, Title: pfTitle}

	stages, err := h.loadEnabledLifecycleStages(ctx)
	if err != nil {
		return doc, err
	}
	stageName := map[string]string{}
	exitStages := map[string]bool{}
	for _, s := range stages {
		stageName[s.StageKey] = s.Name
		exitStages[s.StageKey] = s.IsExit
	}

	projects, err := h.p3mReportProjects(ctx, portfolioID)
	if err != nil {
		return doc, err
	}
	programmes, err := h.p3mReportProgrammes(ctx, portfolioID)
	if err != nil {
		return doc, err
	}
	projectByID := map[int]p3mReportProjectRow{}
	for _, p := range projects {
		projectByID[p.ID] = p
	}
	programmeByID := map[int]p3mReportProgrammeRow{}
	for _, p := range programmes {
		programmeByID[p.ID] = p
	}

	// Investment set: non-exit-stage projects + all programmes (programmes
	// carry no stage, so they never exit).
	investments := make([]p3mReportInvestmentRef, 0, len(projects)+len(programmes))
	for _, p := range projects {
		if !exitStages[p.CurrentStage] {
			investments = append(investments, p3mReportInvestmentRef{kind: "project", id: p.ID, name: p.Title, stage: p.CurrentStage, onHold: p.OnHold})
		}
	}
	for _, p := range programmes {
		investments = append(investments, p3mReportInvestmentRef{kind: "programme", id: p.ID, name: p.Title})
	}

	// Curated row for the period (may be absent).
	var curated *p3mCuratedReportView
	if v, err := scanP3MPortfolioReportView(h.db.QueryRowContext(ctx, `SELECT `+p3mCuratedReportColumns+` FROM p3m_portfolio_reports WHERE portfolio_id = ? AND period = ?`, portfolioID, asOf)); err == nil {
		curated = &v
	} else if !isNotFound(err) && err != sql.ErrNoRows {
		return doc, err
	}

	// --- 1-2 Position -------------------------------------------------------
	counts := models.P3MReportPositionCounts{
		ByStage:      map[string]int{},
		ByDisplayRag: map[string]int{"green": 0, "amber": 0, "red": 0, "tbd": 0},
	}
	for _, s := range stages {
		counts.ByStage[s.StageKey] = 0
	}
	counts.Programmes = len(programmes)
	counts.Projects = len(projects)
	pendingELT := map[int]bool{}
	{
		rows, err := h.db.QueryContext(ctx, `SELECT id, project_id FROM p3m_elt_decisions WHERE portfolio_id = ? AND status = 'pending'`, portfolioID)
		if err != nil {
			return doc, err
		}
		for rows.Next() {
			var id int
			var pid sql.NullInt64
			if err := rows.Scan(&id, &pid); err != nil {
				rows.Close()
				return doc, err
			}
			if pid.Valid {
				pendingELT[int(pid.Int64)] = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return doc, err
		}
	}
	for _, inv := range investments {
		var display string
		switch inv.kind {
		case "project":
			p := projectByID[inv.id]
			display = p3mReportDisplayRag(false, p.OnHold, p.CurrentStage, p.RagStatus)
			if _, known := counts.ByStage[p.CurrentStage]; known {
				counts.ByStage[p.CurrentStage]++
			}
		default:
			display = p3mReportDisplayRag(true, false, "", "")
		}
		counts.ByDisplayRag[display]++
		if inv.onHold {
			counts.OnHold++
		}
	}
	counts.ApprovedTotal = len(investments)
	doc.Position.Counts = counts
	if curated != nil {
		doc.Position.KeyPressures = curated.KeyPressures
	}
	doc.Position.ELTDecisionsRequired = []models.P3MReportELTDecisionRef{}
	{
		rows, err := h.db.QueryContext(ctx, `SELECT id, title, project_id FROM p3m_elt_decisions WHERE portfolio_id = ? AND status = 'pending' ORDER BY id`, portfolioID)
		if err != nil {
			return doc, err
		}
		for rows.Next() {
			var ref models.P3MReportELTDecisionRef
			var pid sql.NullInt64
			if err := rows.Scan(&ref.ID, &ref.Title, &pid); err != nil {
				rows.Close()
				return doc, err
			}
			if pid.Valid {
				if p, ok := projectByID[int(pid.Int64)]; ok {
					ref.ProjectRef = p.ExternalRef
					ref.ProjectTitle = &p.Title
				}
			}
			doc.Position.ELTDecisionsRequired = append(doc.Position.ELTDecisionsRequired, ref)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return doc, err
		}
	}

	// --- 3 Programme highlights ---------------------------------------------
	milestoneStats, taskStats, err := h.p3mReportProgrammeItemStats(ctx, func() []int {
		var ids []int
		for _, p := range projects {
			ids = append(ids, p.ID)
		}
		return ids
	}())
	if err != nil {
		return doc, err
	}
	projectsByProgramme := map[int][]p3mReportProjectRow{}
	for _, p := range projects {
		if p.ProgrammeID != nil {
			projectsByProgramme[*p.ProgrammeID] = append(projectsByProgramme[*p.ProgrammeID], p)
		}
	}
	highlightCommentary := map[int]string{}
	if curated != nil {
		for _, ph := range curated.ProgrammeHighlights {
			highlightCommentary[ph.ProgrammeID] = ph.Commentary
		}
	}
	doc.Highlights = make([]models.P3MReportProgrammeHighlight, 0, len(programmes))
	for _, pg := range programmes {
		m := milestoneStats
		t := taskStats
		mm, tt := models.P3MReportCounts{}, models.P3MReportCounts{}
		sum, n := 0, 0
		for _, p := range projectsByProgramme[pg.ID] {
			mm.Done += m[p.ID].Done
			mm.Total += m[p.ID].Total
			tt.Done += t[p.ID].Done
			tt.Total += t[p.ID].Total
			sum += p.ProgressPercent
			n++
		}
		progress := 0
		if n > 0 {
			progress = int((float64(sum) / float64(n) + 0.5))
		}
		doc.Highlights = append(doc.Highlights, models.P3MReportProgrammeHighlight{
			ProgrammeID:     pg.ID,
			Title:           pg.Title,
			Rag:             pg.RagStatus,
			ProgressPercent: progress,
			Milestones:      mm,
			Tasks:           tt,
			Commentary:      highlightCommentary[pg.ID],
		})
	}
	// --- 4 Key portfolio risks ------------------------------------------------
	{
		rows, err := h.db.QueryContext(ctx, `
			SELECT id, title, COALESCE(description, ''), COALESCE(mitigation_plan, ''), impact_score, probability_score
			FROM p3m_raid_items
			WHERE entity_type = 'portfolio' AND entity_id = ? AND item_type = 'risk' AND status IN ('open', 'mitigating', 'escalated')
			ORDER BY impact_score * probability_score DESC, id LIMIT 5`, portfolioID)
		if err != nil {
			return doc, err
		}
		doc.Risks = []models.P3MReportRisk{}
		for rows.Next() {
			var (
				risk            models.P3MReportRisk
				mitigation      string
				impact, prob    int
			)
			if err := rows.Scan(&risk.ID, &risk.Title, &risk.Description, &mitigation, &impact, &prob); err != nil {
				rows.Close()
				return doc, err
			}
			if mitigation != "" {
				risk.MitigationPlan = &mitigation
			}
			risk.RiskScore = float64(impact * prob)
			doc.Risks = append(doc.Risks, risk)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return doc, err
		}
	}

	// --- 5-6 Benefits -----------------------------------------------------------
	treeBenefits, err := h.p3mReportTreeBenefits(ctx, projects, programmes)
	if err != nil {
		return doc, err
	}
	doc.Benefits = buildP3MReportBenefits(treeBenefits, projectByID, exitStages)

	// --- 7 Dashboard ------------------------------------------------------------
	doc.Dashboard = h.buildP3MReportDashboard(ctx, portfolioID, curated, investments, projectByID, pendingELT, asOf, treeBenefits)

	// --- 8-11 Red/amber detail ----------------------------------------------------
	doc.AtRisk = h.buildP3MReportAtRisk(ctx, projects, asOf)

	// --- 12-14 Change dashboard + calendar ------------------------------------------
	doc.ChangeDashboard, doc.ChangeCalendar = h.buildP3MReportChangeSections(ctx, asOf, curated, projectByID, programmeByID, pfTitle)

	// --- 15-16 Ideate / proposals ---------------------------------------------------
	doc.Ideate, err = h.buildP3MReportIdeate(ctx, projectByID)
	if err != nil {
		return doc, err
	}
	doc.Proposals = buildP3MReportProposals(projects)

	// --- 17-18 Approved investments ---------------------------------------------------
	doc.Investments = h.buildP3MReportInvestments(ctx, investments, projectByID, programmeByID, stageName, asOf)

	// --- 19 Closed ----------------------------------------------------------------------
	doc.Closed, err = h.buildP3MReportClosed(ctx, projects, closedSince, exitStages)
	if err != nil {
		return doc, err
	}

	// --- ELT decision coversheets ---------------------------------------------------------
	doc.ELTDecisions = h.buildP3MReportELTCoversheets(ctx, portfolioID, projectByID, stageName)

	return doc, nil
}

// p3mReportBenefitRow is one benefit bound to the portfolio tree.
type p3mReportBenefitRow struct {
	ID                 int
	ProjectID          sql.NullInt64
	ProgrammeID        sql.NullInt64
	BenefitType        string
	StrategicObjective string
	BenefitCategory    string
	BusinessOwnerGroup string
}

func (h *P3MHandler) p3mReportTreeBenefits(ctx context.Context, projects []p3mReportProjectRow, programmes []p3mReportProgrammeRow) ([]p3mReportBenefitRow, error) {
	projectIDs := make([]int, 0, len(projects))
	for _, p := range projects {
		projectIDs = append(projectIDs, p.ID)
	}
	programmeIDs := make([]int, 0, len(programmes))
	for _, p := range programmes {
		programmeIDs = append(programmeIDs, p.ID)
	}
	if len(projectIDs) == 0 && len(programmeIDs) == 0 {
		return []p3mReportBenefitRow{}, nil
	}
	var clauses []string
	var args []any
	if len(projectIDs) > 0 {
		clauses = append(clauses, "b.project_id IN ("+questionPlaceholders(len(projectIDs))+")")
		for _, id := range projectIDs {
			args = append(args, id)
		}
	}
	if len(programmeIDs) > 0 {
		clauses = append(clauses, "b.programme_id IN ("+questionPlaceholders(len(programmeIDs))+")")
		for _, id := range programmeIDs {
			args = append(args, id)
		}
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT b.id, b.project_id, b.programme_id, b.benefit_type,
		       COALESCE(b.strategic_objective, ''), COALESCE(b.benefit_category, ''), COALESCE(b.business_owner_group, '')
		FROM p3m_benefits b WHERE `+strings.Join(clauses, " OR "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]p3mReportBenefitRow, 0)
	for rows.Next() {
		var b p3mReportBenefitRow
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.ProgrammeID, &b.BenefitType, &b.StrategicObjective, &b.BenefitCategory, &b.BusinessOwnerGroup); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func buildP3MReportBenefits(benefits []p3mReportBenefitRow, projectByID map[int]p3mReportProjectRow, exitStages map[string]bool) models.P3MReportBenefits {
	out := models.P3MReportBenefits{
		ByType: map[string]int{"financial": 0, "non_financial": 0, "strategic": 0, "compliance": 0, "unspecified": 0},
	}
	out.Total = len(benefits)
	owning := map[int]bool{}
	type csoRow struct {
		total, planning, implement, closed int
	}
	cso := map[string]*csoRow{}
	type ownerRow struct {
		categories map[string]int
		total      int
	}
	owners := map[string]*ownerRow{}
	for _, b := range benefits {
		out.ByType[b.BenefitType]++
		stage := ""
		switch {
		case b.ProjectID.Valid:
			if p, ok := projectByID[int(b.ProjectID.Int64)]; ok {
				stage = p.CurrentStage
			}
			owning[int(b.ProjectID.Int64)] = true
		case b.ProgrammeID.Valid:
			// Programmes carry no lifecycle stage, so programme-owned
			// benefits count in totals only, not in stage buckets.
			owning[int(b.ProgrammeID.Int64)] = true
		}
		csoKey := b.StrategicObjective
		if csoKey == "" {
			csoKey = "Unspecified"
		}
		row := cso[csoKey]
		if row == nil {
			row = &csoRow{}
			cso[csoKey] = row
		}
		row.total++
		switch {
		case stage == "define_planning":
			row.planning++
		case stage == "implement_deliver":
			row.implement++
		case exitStages[stage]:
			row.closed++
		}
		ownerKey := b.BusinessOwnerGroup
		if ownerKey == "" {
			ownerKey = "Unspecified"
		}
		o := owners[ownerKey]
		if o == nil {
			o = &ownerRow{categories: map[string]int{}}
			owners[ownerKey] = o
		}
		catKey := b.BenefitCategory
		if catKey == "" {
			catKey = "Unspecified"
		}
		o.categories[catKey]++
		o.total++
	}
	out.InvestmentCount = len(owning)
	out.ByCSO = make([]models.P3MReportBenefitCSORow, 0, len(cso))
	for k, v := range cso {
		out.ByCSO = append(out.ByCSO, models.P3MReportBenefitCSORow{CSO: k, Total: v.total, Planning: v.planning, Implement: v.implement, Closed: v.closed})
	}
	sort.Slice(out.ByCSO, func(i, j int) bool { return out.ByCSO[i].CSO < out.ByCSO[j].CSO })
	out.ByOwnerGroupAndCategory = make([]models.P3MReportBenefitOwnerRow, 0, len(owners))
	for k, v := range owners {
		out.ByOwnerGroupAndCategory = append(out.ByOwnerGroupAndCategory, models.P3MReportBenefitOwnerRow{OwnerGroup: k, Categories: v.categories, Total: v.total})
	}
	sort.Slice(out.ByOwnerGroupAndCategory, func(i, j int) bool { return out.ByOwnerGroupAndCategory[i].OwnerGroup < out.ByOwnerGroupAndCategory[j].OwnerGroup })
	return out
}

// buildP3MReportDashboard assembles slide 7: curated commentary/confidence,
// active RAG counts, pipeline by theme, change capacity, benefits by
// category and emerging demand.
func (h *P3MHandler) buildP3MReportDashboard(ctx context.Context, portfolioID int, curated *p3mCuratedReportView, investments []p3mReportInvestmentRef, projectByID map[int]p3mReportProjectRow, pendingELT map[int]bool, asOf string, benefits []p3mReportBenefitRow) models.P3MReportDashboard {
	var dash models.P3MReportDashboard
	dash.ConfidenceLenses = []models.P3MReportConfidenceLens{}
	dash.ActiveByRag = map[string]int{"green": 0, "amber": 0, "red": 0}
	dash.ChangeCapacity = []models.P3MReportCapacityRow{}
	dash.BenefitsByCategory = []models.P3MReportBenefitCategoryRow{}
	dash.EmergingDemand = []models.P3MReportEmergingDemandRow{}
	if curated != nil {
		ec := curated.ExecutiveCommentary
		dash.ExecutiveCommentary = &ec
		dash.ConfidenceLenses = curated.ConfidenceLenses
	}
	if dash.ConfidenceLenses == nil {
		dash.ConfidenceLenses = []models.P3MReportConfidenceLens{}
	}

	// Delivery confidence trend: last six non-null values, period ascending.
	{
		rows, err := h.db.QueryContext(ctx, `SELECT delivery_confidence_pct FROM p3m_portfolio_reports WHERE portfolio_id = ? AND delivery_confidence_pct IS NOT NULL ORDER BY period DESC LIMIT 6`, portfolioID)
		if err == nil {
			var trend []float64
			for rows.Next() {
				var value float64
				if err := rows.Scan(&value); err == nil {
					trend = append(trend, value)
				}
			}
			rows.Close()
			if len(trend) > 0 {
				for i, j := 0, len(trend)-1; i < j; i, j = i+1, j-1 {
					trend[i], trend[j] = trend[j], trend[i]
				}
				dash.DeliveryConfidence.Trend = trend
				last := trend[len(trend)-1]
				dash.DeliveryConfidence.Current = &last
			}
		}
	}

	// Active investments by RAG (tbd excluded).
	for _, inv := range investments {
		var display string
		if inv.kind == "project" {
			p := projectByID[inv.id]
			display = p3mReportDisplayRag(false, p.OnHold, p.CurrentStage, p.RagStatus)
		} else {
			display = p3mReportDisplayRag(true, false, "", "")
		}
		if _, ok := dash.ActiveByRag[display]; ok {
			dash.ActiveByRag[display]++
		}
	}

	// Pipeline by theme: theme = portfolio_value (projects); programmes have
	// no theme field and count under "Unspecified".
	{
		type themeRow struct {
			planning, awaitingELT, active, closed int
		}
		themes := map[string]*themeRow{}
		for _, inv := range investments {
			theme := "Unspecified"
			if inv.kind == "project" {
				if p := projectByID[inv.id]; p.PortfolioValue != nil && *p.PortfolioValue != "" {
					theme = *p.PortfolioValue
				}
			}
			row := themes[theme]
			if row == nil {
				row = &themeRow{}
				themes[theme] = row
			}
			if inv.kind == "project" {
				p := projectByID[inv.id]
				switch {
				case p.OnHold:
					// on-hold investments appear in none of the four buckets
				case pendingELT[p.ID]:
					row.awaitingELT++
				case p.CurrentStage == "define_planning":
					row.planning++
				case p.CurrentStage == "implement_deliver":
					row.active++
				}
			}
		}
		totalByTheme := map[string]int{}
		for _, inv := range investments {
			theme := "Unspecified"
			if inv.kind == "project" {
				if p := projectByID[inv.id]; p.PortfolioValue != nil && *p.PortfolioValue != "" {
					theme = *p.PortfolioValue
				}
			}
			totalByTheme[theme]++
		}
		dash.PipelineByTheme = make([]models.P3MReportThemeRow, 0, len(themes))
		for theme, row := range themes {
			dash.PipelineByTheme = append(dash.PipelineByTheme, models.P3MReportThemeRow{
				Theme: theme, Planning: row.planning, AwaitingELT: row.awaitingELT, Active: row.active, Closed: row.closed, Total: totalByTheme[theme],
			})
		}
		sort.Slice(dash.PipelineByTheme, func(i, j int) bool { return dash.PipelineByTheme[i].Theme < dash.PipelineByTheme[j].Theme })
	}

	// Change capacity: postures over the six-month window.
	months := p3mReportMonths(asOf)
	{
		args := make([]any, len(months))
		for i, m := range months {
			args[i] = m
		}
		rows, err := h.db.QueryContext(ctx, `SELECT business_group, month, posture FROM p3m_change_capacity WHERE month IN (`+questionPlaceholders(len(months))+`)`, args...)
		if err == nil {
			groups := map[string]map[string]any{}
			for rows.Next() {
				var group, month, posture string
				if err := rows.Scan(&group, &month, &posture); err == nil {
					if groups[group] == nil {
						groups[group] = map[string]any{}
					}
					groups[group][month] = posture
				}
			}
			rows.Close()
			dash.ChangeCapacity = make([]models.P3MReportCapacityRow, 0, len(groups))
			for group, postures := range groups {
				dash.ChangeCapacity = append(dash.ChangeCapacity, models.P3MReportCapacityRow{Group: group, Postures: postures})
			}
			sort.Slice(dash.ChangeCapacity, func(i, j int) bool { return dash.ChangeCapacity[i].Group < dash.ChangeCapacity[j].Group })
		}
	}

	// Benefits by category.
	{
		type catRow struct {
			financial, nonFinancial, unspecified, total int
		}
		cats := map[string]*catRow{}
		for _, b := range benefits {
			key := b.BenefitCategory
			if key == "" {
				key = "Unspecified"
			}
			row := cats[key]
			if row == nil {
				row = &catRow{}
				cats[key] = row
			}
			switch b.BenefitType {
			case "financial":
				row.financial++
			case "non_financial":
				row.nonFinancial++
			case "unspecified":
				row.unspecified++
			}
			row.total++
		}
		for key, row := range cats {
			dash.BenefitsByCategory = append(dash.BenefitsByCategory, models.P3MReportBenefitCategoryRow{
				Category: key, Financial: row.financial, NonFinancial: row.nonFinancial, Unspecified: row.unspecified, Total: row.total,
			})
		}
		sort.Slice(dash.BenefitsByCategory, func(i, j int) bool { return dash.BenefitsByCategory[i].Category < dash.BenefitsByCategory[j].Category })
	}

	// Emerging demand: captured/assessing intakes by business group plus
	// proposal-stage projects by business owner group.
	{
		ideate := map[string]int{}
		rows, err := h.db.QueryContext(ctx, `SELECT COALESCE(NULLIF(business_group, ''), 'Unspecified') FROM p3m_intakes WHERE status IN ('captured', 'assessing')`)
		if err == nil {
			for rows.Next() {
				var group string
				if err := rows.Scan(&group); err == nil {
					ideate[group]++
				}
			}
			rows.Close()
		}
		proposal := map[string]int{}
		for _, p := range projectByID {
			if p.CurrentStage == "proposal" {
				group := "Unspecified"
				if p.BusinessOwnerGroup != nil && *p.BusinessOwnerGroup != "" {
					group = *p.BusinessOwnerGroup
				}
				proposal[group]++
			}
		}
		groups := map[string]bool{}
		for g := range ideate {
			groups[g] = true
		}
		for g := range proposal {
			groups[g] = true
		}
		dash.EmergingDemand = make([]models.P3MReportEmergingDemandRow, 0, len(groups))
		for g := range groups {
			dash.EmergingDemand = append(dash.EmergingDemand, models.P3MReportEmergingDemandRow{Group: g, Ideate: ideate[g], Proposal: proposal[g]})
		}
		sort.Slice(dash.EmergingDemand, func(i, j int) bool { return dash.EmergingDemand[i].Group < dash.EmergingDemand[j].Group })
	}
	return dash
}

// buildP3MReportAtRisk assembles slides 8-11: delivery projects whose
// display RAG is red or amber, with duration in status, live tolerance
// category RAGs and the newest status report.
func (h *P3MHandler) buildP3MReportAtRisk(ctx context.Context, projects []p3mReportProjectRow, asOf string) []models.P3MReportAtRiskProject {
	out := make([]models.P3MReportAtRiskProject, 0)
	if len(projects) == 0 {
		return out
	}
	// Latest RAG-history row per project matching its current status.
	ids := make([]int, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	type histRow struct {
		entityID  int
		ragStatus string
		changedAt time.Time
	}
	var hist []histRow
	rows, err := h.db.QueryContext(ctx, `SELECT entity_id, rag_status, changed_at FROM p3m_rag_history WHERE entity_type = 'project' AND entity_id IN (`+questionPlaceholders(len(ids))+`) ORDER BY changed_at DESC, id DESC`, args...)
	if err == nil {
		for rows.Next() {
			var hr histRow
			if err := rows.Scan(&hr.entityID, &hr.ragStatus, &hr.changedAt); err == nil {
				hist = append(hist, hr)
			}
		}
		rows.Close()
	}
	newestMatching := map[int]time.Time{}
	for _, hr := range hist {
		if _, seen := newestMatching[hr.entityID]; seen {
			continue
		}
		for _, p := range projects {
			if p.ID == hr.entityID && p.RagStatus == hr.ragStatus {
				newestMatching[p.ID] = hr.changedAt
				break
			}
		}
	}

	// Newest status report per project.
	type srRow struct {
		projectID int
		period    string
		issue     string
		impact    string
		nextSteps string
	}
	var srs []srRow
	srows, err := h.db.QueryContext(ctx, `SELECT project_id, period, issue, impact, next_steps FROM p3m_project_status_reports WHERE project_id IN (`+questionPlaceholders(len(ids))+`) ORDER BY period DESC, reported_at DESC, id DESC`, args...)
	if err == nil {
		for srows.Next() {
			var sr srRow
			if err := srows.Scan(&sr.projectID, &sr.period, &sr.issue, &sr.impact, &sr.nextSteps); err == nil {
				srs = append(srs, sr)
			}
		}
		srows.Close()
	}
	newestSR := map[int]srRow{}
	for _, sr := range srs {
		if _, seen := newestSR[sr.projectID]; !seen {
			newestSR[sr.projectID] = sr
		}
	}

	yStr, mStr, _ := strings.Cut(asOf, "-")
	asOfYear, _ := strconv.Atoi(yStr)
	asOfMonth, _ := strconv.Atoi(mStr)
	for _, p := range projects {
		display := p3mReportDisplayRag(false, p.OnHold, p.CurrentStage, p.RagStatus)
		if display != "red" && display != "amber" {
			continue
		}
		duration := 1
		if changed, ok := newestMatching[p.ID]; ok {
			diff := (asOfYear*12+asOfMonth) - (changed.Year()*12+int(changed.Month()))
			if diff > 1 {
				duration = diff
			}
		}
		row := models.P3MReportAtRiskProject{
			ID:                     p.ID,
			Title:                  p.Title,
			DisplayRag:             display,
			DurationInStatusMonths: duration,
			TolerancesAtRisk:       []models.P3MReportToleranceAtRisk{},
			BusinessOwnerGroup:     p.BusinessOwnerGroup,
			ResponsibleGroup:       p.ResponsibleGroup,
		}
		tol := h.p3mToleranceFor(ctx, "project", p.ID)
		if rags, err := h.p3mProjectCategoryRags(ctx, p.ID, tol); err == nil {
			for _, cn := range p3mReportCategoryNames {
				if rags[cn.Key] != "green" {
					row.TolerancesAtRisk = append(row.TolerancesAtRisk, models.P3MReportToleranceAtRisk{Category: cn.Key, Name: cn.Name, Rag: rags[cn.Key]})
				}
			}
		}
		if sr, ok := newestSR[p.ID]; ok {
			srCopy := models.P3MReportStatusReport{Period: sr.period, Issue: sr.issue, Impact: sr.impact, NextSteps: sr.nextSteps}
			row.StatusReport = &srCopy
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayRag != out[j].DisplayRag {
			return out[i].DisplayRag == "red"
		}
		if out[i].DurationInStatusMonths != out[j].DurationInStatusMonths {
			return out[i].DurationInStatusMonths > out[j].DurationInStatusMonths
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// p3mReportCalendarEntry is a raw calendar row for the change sections.
type p3mReportCalendarEntry struct {
	ID          int
	Activity    string
	StartWeek   *string
	EndWeek     *string
	ImpactLevel int
	Category    *string
	EntityType  *string
	EntityID    *int
}

// p3mReportChangeEntryActive returns the window months (from the six-month
// window) in which the entry is active, plus its effective impact per month
// (max of group-impact levels, falling back to the entry level).
func p3mReportChangeEntryActive(entry p3mReportCalendarEntry, window []string, impacts []models.P3MReportCalendarGroupImpact) (map[string]int, bool) {
	byMonth := map[string]int{}
	dated := entry.StartWeek != nil && entry.EndWeek != nil
	if !dated {
		return byMonth, false
	}
	weeks := p3mISOWeeks(*entry.StartWeek, *entry.EndWeek)
	if weeks == nil {
		return byMonth, false
	}
	windowSet := map[string]bool{}
	for _, m := range window {
		windowSet[m] = true
	}
	for _, week := range weeks {
		monday, ok := p3mISOWeekMonday(week)
		if !ok {
			continue
		}
		month := monday.Format("2006-01")
		if !windowSet[month] {
			continue
		}
		effective := entry.ImpactLevel
		for _, g := range impacts {
			if g.ImpactLevel > effective {
				effective = g.ImpactLevel
			}
		}
		if effective > byMonth[month] {
			byMonth[month] = effective
		}
	}
	return byMonth, len(byMonth) > 0
}

// buildP3MReportChangeSections assembles slides 12-14: the change dashboard
// (known initiatives, active by month, medium/high entries) and the change
// calendar (dated + undated entries).
func (h *P3MHandler) buildP3MReportChangeSections(ctx context.Context, asOf string, curated *p3mCuratedReportView, projectByID map[int]p3mReportProjectRow, programmeByID map[int]p3mReportProgrammeRow, pfTitle string) (models.P3MReportChangeDashboard, models.P3MReportChangeCalendar) {
	var dash models.P3MReportChangeDashboard
	var cal models.P3MReportChangeCalendar
	dash.Months = p3mReportMonths(asOf)
	dash.ActiveByMonth = []models.P3MReportChangeMonthRow{}
	dash.MediumHigh = []models.P3MReportMediumHighEntry{}
	cal.Months = dash.Months
	cal.Dated = []models.P3MReportCalendarDatedEntry{}
	cal.Undated = []models.P3MReportCalendarUndatedEntry{}
	if curated != nil {
		dash.KeyMessages = curated.ChangeKeyMessages
	}

	rows, err := h.db.QueryContext(ctx, `SELECT id, activity, start_week, end_week, impact_level, category, entity_type, entity_id FROM p3m_change_calendar_entries ORDER BY id`)
	if err != nil {
		return dash, cal
	}
	var entries []p3mReportCalendarEntry
	for rows.Next() {
		var e p3mReportCalendarEntry
		if err := rows.Scan(&e.ID, &e.Activity, &e.StartWeek, &e.EndWeek, &e.ImpactLevel, &e.Category, &e.EntityType, &e.EntityID); err == nil {
			entries = append(entries, e)
		}
	}
	rows.Close()

	entryIDs := make([]int, 0, len(entries))
	for _, e := range entries {
		entryIDs = append(entryIDs, e.ID)
	}
	impactsByID := map[int][]models.P3MReportCalendarGroupImpact{}
	if len(entryIDs) > 0 {
		args := make([]any, len(entryIDs))
		for i, id := range entryIDs {
			args[i] = id
		}
		irows, err := h.db.QueryContext(ctx, `SELECT entry_id, business_group, impact_level FROM p3m_change_calendar_group_impacts WHERE entry_id IN (`+questionPlaceholders(len(entryIDs))+`) ORDER BY business_group`, args...)
		if err == nil {
			for irows.Next() {
				var entryID int
				var g models.P3MReportCalendarGroupImpact
				if err := irows.Scan(&entryID, &g.BusinessGroup, &g.ImpactLevel); err == nil {
					impactsByID[entryID] = append(impactsByID[entryID], g)
				}
			}
			irows.Close()
		}
	}
	dash.KnownInitiatives = len(entries)

	// Active by month over the window.
	for _, month := range dash.Months {
		row := models.P3MReportChangeMonthRow{
			Month:    month,
			ByImpact: map[string]int{},
		}
		for _, label := range p3mReportImpactLabels {
			row.ByImpact[label] = 0
		}
		for _, e := range entries {
			active, _ := p3mReportChangeEntryActive(e, []string{month}, impactsByID[e.ID])
			if impact, ok := active[month]; ok {
				row.Active++
				row.ByImpact[p3mReportImpactLabels[impact]]++
			}
		}
		dash.ActiveByMonth = append(dash.ActiveByMonth, row)
	}

	// Medium/high: effective impact >= 4 in any window month.
	for _, e := range entries {
		active, _ := p3mReportChangeEntryActive(e, dash.Months, impactsByID[e.ID])
		monthsGE4 := []string{}
		for _, m := range dash.Months {
			if impact, ok := active[m]; ok && impact >= 4 {
				monthsGE4 = append(monthsGE4, m)
			}
		}
		if len(monthsGE4) == 0 {
			continue
		}
		entryRow := models.P3MReportMediumHighEntry{EntryID: e.ID, Activity: e.Activity, Groups: []models.P3MReportMediumHighGroup{}}
		children := impactsByID[e.ID]
		if len(children) == 0 {
			if e.ImpactLevel >= 4 {
				entryRow.Groups = append(entryRow.Groups, models.P3MReportMediumHighGroup{Group: "ALL", ImpactLevel: e.ImpactLevel, Months: monthsGE4})
			}
		} else {
			for _, g := range children {
				if g.ImpactLevel >= 4 {
					entryRow.Groups = append(entryRow.Groups, models.P3MReportMediumHighGroup{Group: g.BusinessGroup, ImpactLevel: g.ImpactLevel, Months: monthsGE4})
				}
			}
		}
		if len(entryRow.Groups) > 0 {
			dash.MediumHigh = append(dash.MediumHigh, entryRow)
		}
	}
	sort.Slice(dash.MediumHigh, func(i, j int) bool { return dash.MediumHigh[i].EntryID < dash.MediumHigh[j].EntryID })

	// Calendar lists.
	resolveEntity := func(e p3mReportCalendarEntry) *models.P3MReportCalendarEntity {
		if e.EntityType == nil || e.EntityID == nil {
			return nil
		}
		var title string
		switch *e.EntityType {
		case "project":
			if p, ok := projectByID[*e.EntityID]; ok {
				title = p.Title
			}
		case "programme":
			if p, ok := programmeByID[*e.EntityID]; ok {
				title = p.Title
			}
		case "portfolio":
			title = pfTitle
		default:
			return nil
		}
		if title == "" {
			return nil
		}
		entityType := *e.EntityType
		entityID := *e.EntityID
		return &models.P3MReportCalendarEntity{Type: entityType, ID: entityID, Title: title}
	}
	type datedKey struct {
		start string
		row   models.P3MReportCalendarDatedEntry
	}
	var dated []datedKey
	for _, e := range entries {
		impacts := impactsByID[e.ID]
		if impacts == nil {
			impacts = []models.P3MReportCalendarGroupImpact{}
		}
		switch {
		case e.StartWeek != nil && e.EndWeek != nil:
			active, _ := p3mReportChangeEntryActive(e, dash.Months, impacts)
			months := make([]string, 0, len(active))
			for _, m := range dash.Months {
				if _, ok := active[m]; ok {
					months = append(months, m)
				}
			}
			dated = append(dated, datedKey{
				start: *e.StartWeek,
				row: models.P3MReportCalendarDatedEntry{
					Activity:     e.Activity,
					Category:     e.Category,
					GroupImpacts: impacts,
					Months:       months,
					Entity:       resolveEntity(e),
				},
			})
		case e.StartWeek == nil && e.EndWeek == nil:
			cal.Undated = append(cal.Undated, models.P3MReportCalendarUndatedEntry{
				Activity:     e.Activity,
				Category:     e.Category,
				GroupImpacts: impacts,
			})
		}
	}
	sort.Slice(dated, func(i, j int) bool {
		if dated[i].start != dated[j].start {
			return dated[i].start < dated[j].start
		}
		return dated[i].row.Activity < dated[j].row.Activity
	})
	for _, k := range dated {
		cal.Dated = append(cal.Dated, k.row)
	}
	sort.Slice(cal.Undated, func(i, j int) bool { return cal.Undated[i].Activity < cal.Undated[j].Activity })
	return dash, cal
}

// buildP3MReportIdeate assembles the ideate table: captured/assessing intakes
// with their front-door completeness and linked target-project reference.
func (h *P3MHandler) buildP3MReportIdeate(ctx context.Context, projectByID map[int]p3mReportProjectRow) ([]models.P3MReportIdeateRow, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE status IN ('captured', 'assessing') ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.P3MReportIdeateRow, 0)
	for rows.Next() {
		intake, err := scanP3MIntake(rows)
		if err != nil {
			return nil, err
		}
		row := models.P3MReportIdeateRow{
			Title:            intake.Title,
			BusinessGroup:    intake.BusinessGroup,
			ResponsibleGroup: intake.ResponsibleGroup,
		}
		if intake.TargetProjectID != nil {
			if p, ok := projectByID[*intake.TargetProjectID]; ok && p.ExternalRef != nil {
				ref := *p.ExternalRef
				row.Ref = ref
			}
		}
		row.FrontDoorCompleted = len(h.p3mIntakeMissingRequiredFields(intake)) == 0
		out = append(out, row)
	}
	return out, rows.Err()
}

// buildP3MReportProposals assembles the proposal table: proposal-stage
// projects.
func buildP3MReportProposals(projects []p3mReportProjectRow) []models.P3MReportProposalRow {
	out := make([]models.P3MReportProposalRow, 0)
	for _, p := range projects {
		if p.CurrentStage != "proposal" {
			continue
		}
		out = append(out, models.P3MReportProposalRow{
			Title:              p.Title,
			Ref:                p.ExternalRef,
			BusinessOwnerGroup: p.BusinessOwnerGroup,
			ResponsibleGroup:   p.ResponsibleGroup,
			Complexity:         p.DeliveryComplexity,
			Priority:           p.Priority,
		})
	}
	return out
}

// buildP3MReportInvestments assembles the approved-investments table: the
// investment set, projects before programmes, each ordered by name, with the
// RAG trend against the newest history row before the current period.
func (h *P3MHandler) buildP3MReportInvestments(ctx context.Context, investments []p3mReportInvestmentRef, projectByID map[int]p3mReportProjectRow, programmeByID map[int]p3mReportProgrammeRow, stageName map[string]string, asOf string) []models.P3MReportInvestment {
	out := make([]models.P3MReportInvestment, 0, len(investments))
	for _, inv := range investments {
		switch inv.kind {
		case "project":
			p := projectByID[inv.id]
			row := models.P3MReportInvestment{
				Kind:               "project",
				ID:                 p.ID,
				Name:               p.Title,
				Ref:                p.ExternalRef,
				ResponsibleGroup:   p.ResponsibleGroup,
				BusinessOwnerGroup: p.BusinessOwnerGroup,
				Complexity:         p.DeliveryComplexity,
				Phase:              stageName[p.CurrentStage],
				DisplayRag:         p3mReportDisplayRag(false, p.OnHold, p.CurrentStage, p.RagStatus),
				OnHold:             p.OnHold,
				InvestmentType:     p.InvestmentType,
				Theme:              p.PortfolioValue,
				IsCEKPI:            p.IsCEKPI,
			}
			if p.TargetCompletionDate != nil {
				end := p.TargetCompletionDate.Format("2006-01")
				row.ExpectedEnd = &end
			}
			row.RagTrend = h.p3mReportRagTrend(ctx, "project", p.ID, row.DisplayRag, asOf)
			out = append(out, row)
		default:
			p := programmeByID[inv.id]
			row := models.P3MReportInvestment{
				Kind:               "programme",
				ID:                 p.ID,
				Name:               p.Title,
				ResponsibleGroup:   p.ResponsibleGroup,
				BusinessOwnerGroup: p.BusinessOwnerGroup,
				Phase:              "",
				DisplayRag:         p3mReportDisplayRag(true, false, "", ""),
			}
			if p.TargetEndDate != nil {
				end := p.TargetEndDate.Format("2006-01")
				row.ExpectedEnd = &end
			}
			row.RagTrend = h.p3mReportRagTrend(ctx, "programme", p.ID, row.DisplayRag, asOf)
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "project"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// buildP3MReportClosed assembles the closed-since table: exit-stage projects
// (programmes carry no stage and therefore never close) whose closure date —
// the newest passing gate into an exit stage, else the row update time — is
// at or after closedSince when that filter is set.
func (h *P3MHandler) buildP3MReportClosed(ctx context.Context, projects []p3mReportProjectRow, closedSince string, exitStages map[string]bool) ([]models.P3MReportClosedRow, error) {
	var since time.Time
	hasSince := false
	if closedSince != "" {
		t, err := time.Parse("2006-01-02", closedSince)
		if err != nil {
			return nil, err
		}
		since = t.UTC()
		hasSince = true
	}
	exitProjects := make([]p3mReportProjectRow, 0)
	for _, p := range projects {
		if exitStages[p.CurrentStage] {
			exitProjects = append(exitProjects, p)
		}
	}
	if len(exitProjects) == 0 {
		return []models.P3MReportClosedRow{}, nil
	}
	ids := make([]int, 0, len(exitProjects))
	for _, p := range exitProjects {
		ids = append(ids, p.ID)
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	gateClosedAt := map[int]time.Time{}
	if len(exitStages) > 0 {
		exitKeys := make([]any, 0, len(exitStages))
		for k := range exitStages {
			exitKeys = append(exitKeys, k)
		}
		ph := questionPlaceholders(len(ids))
		grows, err := h.db.QueryContext(ctx, `SELECT project_id, MAX(review_date) FROM p3m_stage_gate_reviews WHERE project_id IN (`+ph+`) AND to_stage IN (`+questionPlaceholders(len(exitKeys))+`) GROUP BY project_id`, append(args, exitKeys...)...)
		if err == nil {
			for grows.Next() {
				var projectID int
				var at time.Time
				if err := grows.Scan(&projectID, &at); err == nil {
					gateClosedAt[projectID] = at
				}
			}
			grows.Close()
		}
	}
	closureSeen := map[int]bool{}
	crows, err := h.db.QueryContext(ctx, `SELECT project_id FROM p3m_closure_records WHERE project_id IN (`+questionPlaceholders(len(ids))+`)`, args...)
	if err == nil {
		for crows.Next() {
			var projectID int
			if err := crows.Scan(&projectID); err == nil {
				closureSeen[projectID] = true
			}
		}
		crows.Close()
	}
	out := make([]models.P3MReportClosedRow, 0)
	for _, p := range exitProjects {
		closedAt := p.UpdatedAt
		if at, ok := gateClosedAt[p.ID]; ok {
			closedAt = at
		}
		if hasSince && closedAt.Before(since) {
			continue
		}
		out = append(out, models.P3MReportClosedRow{
			Kind:               "project",
			Name:               p.Title,
			Ref:                p.ExternalRef,
			ResponsibleGroup:   p.ResponsibleGroup,
			BusinessOwnerGroup: p.BusinessOwnerGroup,
			EndReportReceived:  closureSeen[p.ID],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// buildP3MReportELTCoversheets assembles the ELT decision coversheet section:
// all decisions of the portfolio in any status, pending first.
func (h *P3MHandler) buildP3MReportELTCoversheets(ctx context.Context, portfolioID int, projectByID map[int]p3mReportProjectRow, stageName map[string]string) []models.P3MReportELTDecision {
	out := []models.P3MReportELTDecision{}
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, project_id, title, why, decision_requested, conditions_until_approved, status
		FROM p3m_elt_decisions WHERE portfolio_id = ?
		ORDER BY CASE WHEN status = 'pending' THEN 0 ELSE 1 END, id`, portfolioID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var (
			d   models.P3MReportELTDecision
			pid sql.NullInt64
		)
		if err := rows.Scan(&d.ID, &pid, &d.Title, &d.Why, &d.DecisionRequested, &d.ConditionsUntilApproved, &d.Status); err != nil {
			return out
		}
		if pid.Valid {
			if p, ok := projectByID[int(pid.Int64)]; ok {
				d.ProjectRef = p.ExternalRef
				d.ProjectTitle = &p.Title
				phase := stageName[p.CurrentStage]
				d.ProjectPhase = &phase
				d.BusinessOwnerGroup = p.BusinessOwnerGroup
				d.ResponsibleGroup = p.ResponsibleGroup
			}
		}
		out = append(out, d)
	}
	return out
}

// GetPortfolioReport handles GET /api/p3m/portfolios/{id}/portfolio-report.
// Query params: as_of=YYYY-MM (default: current UTC month),
// closed_since=YYYY-MM-DD (optional filter for the closed section).
func (h *P3MHandler) GetPortfolioReport(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	asOf := r.URL.Query().Get("as_of")
	if asOf == "" {
		asOf = time.Now().UTC().Format("2006-01")
	}
	if !p3mValidPeriod(asOf) {
		respondValidationError(w, r, "as_of must be YYYY-MM")
		return
	}
	closedSince := r.URL.Query().Get("closed_since")
	if closedSince != "" {
		if _, err := time.Parse("2006-01-02", closedSince); err != nil {
			respondValidationError(w, r, "closed_since must be YYYY-MM-DD")
			return
		}
	}
	doc, err := h.buildP3MPortfolioReportDocument(r.Context(), id, asOf, closedSince)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_portfolio")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, doc)
}

// GetPortfolioReportMarkdown handles
// GET /api/p3m/portfolios/{id}/portfolio-report/markdown: the same document
// rendered as Markdown (text/markdown).
func (h *P3MHandler) GetPortfolioReportMarkdown(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	asOf := r.URL.Query().Get("as_of")
	if asOf == "" {
		asOf = time.Now().UTC().Format("2006-01")
	}
	if !p3mValidPeriod(asOf) {
		respondValidationError(w, r, "as_of must be YYYY-MM")
		return
	}
	closedSince := r.URL.Query().Get("closed_since")
	if closedSince != "" {
		if _, err := time.Parse("2006-01-02", closedSince); err != nil {
			respondValidationError(w, r, "closed_since must be YYYY-MM-DD")
			return
		}
	}
	doc, err := h.buildP3MPortfolioReportDocument(r.Context(), id, asOf, closedSince)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_portfolio")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(renderP3MPortfolioReportMarkdown(doc)))
}

// renderP3MPortfolioReportMarkdown renders the assembled report document as
// Markdown: 14 numbered sections in the reference report's slide order.
// Empty sections render a "No …" line and are never omitted.
func renderP3MPortfolioReportMarkdown(doc models.P3MPortfolioReportDocument) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — Portfolio Report — as at %s\n\n", mdEscape(doc.Portfolio.Title), doc.AsOf)

	// --- 1 Portfolio Position ---------------------------------------------------
	b.WriteString("## 1 Portfolio Position\n\n")
	c := doc.Position.Counts
	delivery := c.ByDisplayRag["green"] + c.ByDisplayRag["amber"] + c.ByDisplayRag["red"]
	planning := c.ByStage["define_planning"]
	fmt.Fprintf(&b, "The approved portfolio includes %d investments: %d in delivery, %d in planning, %d on hold. Of the %d in delivery, %d report Green, %d Amber and %d Red.\n\n",
		c.ApprovedTotal, delivery, planning, c.OnHold, delivery, c.ByDisplayRag["green"], c.ByDisplayRag["amber"], c.ByDisplayRag["red"])
	if kp := doc.Position.KeyPressures; kp != nil {
		b.WriteString("**Key pressures**\n\n")
		fmt.Fprintf(&b, "- Delivery outlook: %s\n", mdEscape(kp.DeliveryOutlook))
		fmt.Fprintf(&b, "- Amber/red projects: %s\n", mdEscape(kp.AmberRedProjects))
		fmt.Fprintf(&b, "- Business readiness for change: %s\n\n", mdEscape(kp.BusinessReadinessChange))
	}
	b.WriteString("| Stage | Count | Display RAG | Count |\n|---|---|---|---|\n")
	for _, stage := range sortedKeys(c.ByStage) {
		fmt.Fprintf(&b, "| %s | %d | | |\n", mdEscape(stage), c.ByStage[stage])
	}
	for _, rag := range []string{"green", "amber", "red", "tbd"} {
		fmt.Fprintf(&b, "| | | %s | %d |\n", rag, c.ByDisplayRag[rag])
	}
	b.WriteString("\n")
	if len(doc.Position.ELTDecisionsRequired) == 0 {
		b.WriteString("No ELT decisions required.\n")
	} else {
		b.WriteString("**ELT decisions required**\n\n")
		for _, d := range doc.Position.ELTDecisionsRequired {
			line := fmt.Sprintf("- %s", mdEscape(d.Title))
			if d.ProjectRef != nil && *d.ProjectRef != "" {
				line += " (" + mdEscape(*d.ProjectRef)
				if d.ProjectTitle != nil {
					line += " — " + mdEscape(*d.ProjectTitle)
				}
				line += ")"
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n")

	// --- 2 Programme Highlights ----------------------------------------------------
	b.WriteString("## 2 Programme Highlights\n\n")
	if len(doc.Highlights) == 0 {
		b.WriteString("No programmes.\n")
	} else {
		b.WriteString("| Programme | RAG | Progress | Milestones | Tasks | Commentary |\n|---|---|---|---|---|---|\n")
		for _, hl := range doc.Highlights {
			fmt.Fprintf(&b, "| %s | %s | %d%% | %d/%d | %d/%d | %s |\n",
				mdEscape(hl.Title), hl.Rag, hl.ProgressPercent,
				hl.Milestones.Done, hl.Milestones.Total, hl.Tasks.Done, hl.Tasks.Total, mdEscape(hl.Commentary))
		}
	}
	b.WriteString("\n")

	// --- 3 Key Portfolio Risks -------------------------------------------------------
	b.WriteString("## 3 Key Portfolio Risks\n\n")
	if len(doc.Risks) == 0 {
		b.WriteString("No open portfolio risks.\n")
	} else {
		b.WriteString("| # | Risk | Score | Mitigation |\n|---|---|---|---|\n")
		for i, r := range doc.Risks {
			mitigation := "—"
			if r.MitigationPlan != nil {
				mitigation = mdEscape(*r.MitigationPlan)
			}
			fmt.Fprintf(&b, "| %d | %s | %g | %s |\n", i+1, mdEscape(r.Title), r.RiskScore, mitigation)
		}
	}
	b.WriteString("\n")

	// --- 4 Benefits by Type --------------------------------------------------------------
	b.WriteString("## 4 Benefits by Type\n\n")
	if doc.Benefits.Total == 0 {
		b.WriteString("No benefits recorded.\n")
	} else {
		fmt.Fprintf(&b, "%d benefits across %d investments.\n\n", doc.Benefits.Total, doc.Benefits.InvestmentCount)
		b.WriteString("| Type | Count |\n|---|---|\n")
		for _, t := range []string{"financial", "non_financial", "strategic", "compliance", "unspecified"} {
			fmt.Fprintf(&b, "| %s | %d |\n", t, doc.Benefits.ByType[t])
		}
	}
	b.WriteString("\n")

	// --- 5 Benefits by CSO / Owner Group ----------------------------------------------------
	b.WriteString("## 5 Benefits by CSO / Owner Group\n\n")
	if len(doc.Benefits.ByCSO) == 0 {
		b.WriteString("No benefits by strategic objective.\n")
	} else {
		b.WriteString("**By strategic objective (CSO)**\n\n")
		b.WriteString("| CSO | Total | Planning | Implement | Closed |\n|---|---|---|---|---|\n")
		for _, r := range doc.Benefits.ByCSO {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n", mdEscape(r.CSO), r.Total, r.Planning, r.Implement, r.Closed)
		}
		b.WriteString("\n**By owner group × category**\n\n")
		b.WriteString("| Owner group | Categories | Total |\n|---|---|---|\n")
		for _, r := range doc.Benefits.ByOwnerGroupAndCategory {
			cats := make([]string, 0, len(r.Categories))
			for k, v := range r.Categories {
				cats = append(cats, fmt.Sprintf("%s: %d", k, v))
			}
			fmt.Fprintf(&b, "| %s | %s | %d |\n", mdEscape(r.OwnerGroup), mdEscape(strings.Join(cats, ", ")), r.Total)
		}
	}
	b.WriteString("\n")

	// --- 6 Portfolio Dashboard -----------------------------------------------------------------
	d := doc.Dashboard
	b.WriteString("## 6 Portfolio Dashboard\n\n")
	if d.ExecutiveCommentary != nil {
		b.WriteString(mdEscape(*d.ExecutiveCommentary) + "\n\n")
	}
	if d.DeliveryConfidence.Current != nil {
		trend := make([]string, 0, len(d.DeliveryConfidence.Trend))
		for _, t := range d.DeliveryConfidence.Trend {
			trend = append(trend, fmt.Sprintf("%g", t))
		}
		fmt.Fprintf(&b, "Delivery confidence: %g%% (trend: %s)\n\n", *d.DeliveryConfidence.Current, strings.Join(trend, ", "))
	}
	if len(d.ConfidenceLenses) == 0 {
		b.WriteString("No confidence lenses recorded.\n")
	} else {
		b.WriteString("| Lens | Position | RAG |\n|---|---|---|\n")
		for _, l := range d.ConfidenceLenses {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", mdEscape(l.Key), mdEscape(l.Position), l.Rag)
		}
		b.WriteString("\n")
	}
	b.WriteString("**Active investments by RAG**\n\n")
	for _, rag := range []string{"green", "amber", "red"} {
		fmt.Fprintf(&b, "- %s: %d\n", rag, d.ActiveByRag[rag])
	}
	b.WriteString("\n**Pipeline by theme**\n\n")
	if len(d.PipelineByTheme) == 0 {
		b.WriteString("No investments in the pipeline.\n")
	} else {
		b.WriteString("| Theme | Planning | Awaiting ELT | Active | Closed | Total |\n|---|---|---|---|---|---|\n")
		for _, t := range d.PipelineByTheme {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d |\n", mdEscape(t.Theme), t.Planning, t.AwaitingELT, t.Active, t.Closed, t.Total)
		}
	}
	b.WriteString("\n**Change capacity (next 6 months)**\n\n")
	if len(d.ChangeCapacity) == 0 {
		b.WriteString("No capacity postures recorded.\n")
	} else {
		months := p3mReportMonths(doc.AsOf)
		header := "| Group |"
		sep := "|---|"
		for _, m := range months {
			header += " " + m + " |"
			sep += "---|"
		}
		b.WriteString(header + "\n" + sep + "\n")
		for _, row := range d.ChangeCapacity {
			line := "| " + mdEscape(row.Group) + " |"
			for _, m := range months {
				v, _ := row.Postures[m].(string)
				line += " " + v + " |"
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n**Benefits by category**\n\n")
	if len(d.BenefitsByCategory) == 0 {
		b.WriteString("No benefits by category.\n")
	} else {
		b.WriteString("| Category | Financial | Non-financial | Unspecified | Total |\n|---|---|---|---|---|\n")
		for _, r := range d.BenefitsByCategory {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n", mdEscape(r.Category), r.Financial, r.NonFinancial, r.Unspecified, r.Total)
		}
	}
	b.WriteString("\n**Emerging demand**\n\n")
	if len(d.EmergingDemand) == 0 {
		b.WriteString("No emerging demand.\n")
	} else {
		b.WriteString("| Group | Ideate | Proposal |\n|---|---|---|\n")
		for _, r := range d.EmergingDemand {
			fmt.Fprintf(&b, "| %s | %d | %d |\n", mdEscape(r.Group), r.Ideate, r.Proposal)
		}
	}
	b.WriteString("\n")

	// --- 7 Red/Amber Delivery Detail -------------------------------------------------------------
	b.WriteString("## 7 Red/Amber Delivery Detail\n\n")
	if len(doc.AtRisk) == 0 {
		b.WriteString("No red or amber delivery projects.\n")
	} else {
		for _, p := range doc.AtRisk {
			tols := make([]string, 0, len(p.TolerancesAtRisk))
			for _, t := range p.TolerancesAtRisk {
				tols = append(tols, fmt.Sprintf("%s=%s", t.Name, t.Rag))
			}
			owner := "—"
			if p.BusinessOwnerGroup != nil && *p.BusinessOwnerGroup != "" {
				owner = *p.BusinessOwnerGroup
			}
			deliverer := "—"
			if p.ResponsibleGroup != nil && *p.ResponsibleGroup != "" {
				deliverer = *p.ResponsibleGroup
			}
			fmt.Fprintf(&b, "### %s — %s · %d months in status · tolerances: %s · owner: %s · deliverer: %s\n\n",
				mdEscape(p.Title), strings.ToUpper(p.DisplayRag), p.DurationInStatusMonths,
				mdEscape(strings.Join(tols, ", ")), mdEscape(owner), mdEscape(deliverer))
			if sr := p.StatusReport; sr != nil {
				fmt.Fprintf(&b, "- **Issue:** %s\n", mdEscape(sr.Issue))
				fmt.Fprintf(&b, "- **Impact:** %s\n", mdEscape(sr.Impact))
				fmt.Fprintf(&b, "- **Next steps:** %s\n", mdEscape(sr.NextSteps))
			} else {
				b.WriteString("- **Issue:** No status report recorded.\n")
			}
			b.WriteString("\n")
		}
	}

	// --- 8 Change Dashboard --------------------------------------------------------------------------
	cd := doc.ChangeDashboard
	b.WriteString("## 8 Change Dashboard\n\n")
	fmt.Fprintf(&b, "%d known change initiatives over %s.\n\n", cd.KnownInitiatives, strings.Join(cd.Months, ", "))
	if cd.KeyMessages != "" {
		b.WriteString(mdEscape(cd.KeyMessages) + "\n\n")
	}
	if len(cd.ActiveByMonth) == 0 {
		b.WriteString("No active changes in the window.\n")
	} else {
		b.WriteString("| Month | Active | Unknown | Minimal | Low | Moderate | High | Severe |\n|---|---|---|---|---|---|---|---|\n")
		for _, m := range cd.ActiveByMonth {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d | %d |\n",
				m.Month, m.Active, m.ByImpact["unknown"], m.ByImpact["minimal"], m.ByImpact["low"], m.ByImpact["moderate"], m.ByImpact["high"], m.ByImpact["severe"])
		}
	}
	b.WriteString("\n**Medium/high impact entries**\n\n")
	if len(cd.MediumHigh) == 0 {
		b.WriteString("No medium or high impact changes in the window.\n")
	} else {
		for _, e := range cd.MediumHigh {
			groups := make([]string, 0, len(e.Groups))
			for _, g := range e.Groups {
				groups = append(groups, fmt.Sprintf("%s (impact %d, %s)", g.Group, g.ImpactLevel, strings.Join(g.Months, ", ")))
			}
			fmt.Fprintf(&b, "- %s — %s\n", mdEscape(e.Activity), mdEscape(strings.Join(groups, "; ")))
		}
	}
	b.WriteString("\n")

	// --- 9 Change Calendar -------------------------------------------------------------------------------
	cc := doc.ChangeCalendar
	b.WriteString("## 9 Change Calendar\n\n")
	if len(cc.Dated) == 0 {
		b.WriteString("No dated change calendar entries.\n")
	} else {
		b.WriteString("| Activity | Category | Group impacts | Months | Entity |\n|---|---|---|---|---|\n")
		for _, e := range cc.Dated {
			category := ""
			if e.Category != nil {
				category = *e.Category
			}
			groups := make([]string, 0, len(e.GroupImpacts))
			for _, g := range e.GroupImpacts {
				groups = append(groups, fmt.Sprintf("%s: %d", g.BusinessGroup, g.ImpactLevel))
			}
			entity := "—"
			if e.Entity != nil {
				entity = e.Entity.Type + " " + mdEscape(e.Entity.Title)
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				mdEscape(e.Activity), mdEscape(category), mdEscape(strings.Join(groups, ", ")), strings.Join(e.Months, ", "), entity)
		}
	}
	b.WriteString("\n**Undated**\n\n")
	if len(cc.Undated) == 0 {
		b.WriteString("No undated change calendar entries.\n")
	} else {
		for _, e := range cc.Undated {
			b.WriteString("- " + mdEscape(e.Activity))
			if e.Category != nil {
				b.WriteString(" (" + mdEscape(*e.Category) + ")")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	// --- 10 Ideate ---------------------------------------------------------------------------------------------------
	b.WriteString("## 10 Ideate\n\n")
	if len(doc.Ideate) == 0 {
		b.WriteString("No intakes in the front door.\n")
	} else {
		b.WriteString("| Title | Ref | Business group | Responsible group | Front door completed |\n|---|---|---|---|---|\n")
		for _, r := range doc.Ideate {
			rg := "—"
			if r.ResponsibleGroup != nil && *r.ResponsibleGroup != "" {
				rg = *r.ResponsibleGroup
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %v |\n", mdEscape(r.Title), mdEscape(r.Ref), mdEscape(r.BusinessGroup), mdEscape(rg), r.FrontDoorCompleted)
		}
	}
	b.WriteString("\n")

	// --- 11 Proposals --------------------------------------------------------------------------------------------------------
	b.WriteString("## 11 Proposals\n\n")
	if len(doc.Proposals) == 0 {
		b.WriteString("No projects at proposal stage.\n")
	} else {
		b.WriteString("| Title | Ref | Owner group | Responsible group | Complexity | Priority |\n|---|---|---|---|---|---|\n")
		for _, r := range doc.Proposals {
			ref, bog, rg, cx, pr := "", "", "", "", ""
			if r.Ref != nil {
				ref = *r.Ref
			}
			if r.BusinessOwnerGroup != nil {
				bog = *r.BusinessOwnerGroup
			}
			if r.ResponsibleGroup != nil {
				rg = *r.ResponsibleGroup
			}
			if r.Complexity != nil {
				cx = *r.Complexity
			}
			if r.Priority != nil {
				pr = *r.Priority
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", mdEscape(r.Title), mdEscape(ref), mdEscape(bog), mdEscape(rg), mdEscape(cx), mdEscape(pr))
		}
	}
	b.WriteString("\n")

	// --- 12 Approved Investments ------------------------------------------------------------------------------------------------------
	b.WriteString("## 12 Approved Investments\n\n")
	if len(doc.Investments) == 0 {
		b.WriteString("No approved investments.\n")
	} else {
		b.WriteString("| Kind | Name | Ref | Phase | RAG | On hold | Expected end | Type | Theme | CE KPI | Trend |\n|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, r := range doc.Investments {
			ref, it, th, trend := "", "", "", ""
			if r.Ref != nil {
				ref = *r.Ref
			}
			if r.InvestmentType != nil {
				it = *r.InvestmentType
			}
			if r.Theme != nil {
				th = *r.Theme
			}
			if r.RagTrend != nil {
				trend = *r.RagTrend
			}
			end := ""
			if r.ExpectedEnd != nil {
				end = *r.ExpectedEnd
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %v | %s | %s | %s | %v | %s |\n",
				r.Kind, mdEscape(r.Name), mdEscape(ref), mdEscape(r.Phase), r.DisplayRag, r.OnHold, end, mdEscape(it), mdEscape(th), r.IsCEKPI, trend)
		}
	}
	b.WriteString("\n")

	// --- 13 Closed -------------------------------------------------------------------------------------------------------------------------------
	b.WriteString("## 13 Closed\n\n")
	if len(doc.Closed) == 0 {
		b.WriteString("No closed investments.\n")
	} else {
		b.WriteString("| Kind | Name | Ref | Owner group | Deliverer | End report received |\n|---|---|---|---|---|---|\n")
		for _, r := range doc.Closed {
			ref, bog, rg := "", "", ""
			if r.Ref != nil {
				ref = *r.Ref
			}
			if r.BusinessOwnerGroup != nil {
				bog = *r.BusinessOwnerGroup
			}
			if r.ResponsibleGroup != nil {
				rg = *r.ResponsibleGroup
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %v |\n", r.Kind, mdEscape(r.Name), mdEscape(ref), mdEscape(bog), mdEscape(rg), r.EndReportReceived)
		}
	}
	b.WriteString("\n")

	// --- 14 ELT Decision Coversheets ------------------------------------------------------------------------------------------------------------------
	b.WriteString("## 14 ELT Decision Coversheets\n\n")
	if len(doc.ELTDecisions) == 0 {
		b.WriteString("No ELT decision coversheets.\n")
	} else {
		for _, d := range doc.ELTDecisions {
			b.WriteString("### " + mdEscape(d.Title) + "\n\n")
			fmt.Fprintf(&b, "- **Status:** %s\n", d.Status)
			if d.ProjectTitle != nil {
				line := "- **Project:** " + mdEscape(*d.ProjectTitle)
				if d.ProjectRef != nil && *d.ProjectRef != "" {
					line += " (" + mdEscape(*d.ProjectRef) + ")"
				}
				if d.ProjectPhase != nil {
					line += " — " + mdEscape(*d.ProjectPhase)
				}
				b.WriteString(line + "\n")
			}
			if d.BusinessOwnerGroup != nil && *d.BusinessOwnerGroup != "" {
				fmt.Fprintf(&b, "- **Owner group:** %s\n", mdEscape(*d.BusinessOwnerGroup))
			}
			if d.ResponsibleGroup != nil && *d.ResponsibleGroup != "" {
				fmt.Fprintf(&b, "- **Responsible group:** %s\n", mdEscape(*d.ResponsibleGroup))
			}
			fmt.Fprintf(&b, "- **Why:** %s\n", mdEscape(d.Why))
			fmt.Fprintf(&b, "- **Decision requested:** %s\n", mdEscape(d.DecisionRequested))
			if d.ConditionsUntilApproved != "" {
				fmt.Fprintf(&b, "- **Conditions until approved:** %s\n", mdEscape(d.ConditionsUntilApproved))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// mdEscape escapes pipe characters so free text survives Markdown tables.
func mdEscape(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// sortedKeys returns the string keys of a map in ascending order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}


