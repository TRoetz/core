package handlers

import (
	"net/http"
	"time"

	"windshift/internal/models"
)

const p3mCloseOutColumns = `id, project_id, final_budget, actual_to_date, outstanding_commitments, ongoing_obligations, finance_reviewer_id, status, notes, closed_at, created_at, updated_at`

func scanP3MCloseOut(row interface{ Scan(...any) error }) (models.P3MFinancialCloseOut, error) {
	var c models.P3MFinancialCloseOut
	err := row.Scan(&c.ID, &c.ProjectID, &c.FinalBudget, &c.ActualToDate, &c.OutstandingCommitments, &c.OngoingObligations,
		&c.FinanceReviewerID, &c.Status, &c.Notes, &c.ClosedAt, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// p3mCloseOutStatus normalises and validates the close-out status transition.
// Validation (open -> validated) requires a finance reviewer and stamps
// closed_at; reopening clears it. Mirrors the small explicit decision payload
// of stage gates rather than a full-record write.
func p3mCloseOutStatus(w http.ResponseWriter, r *http.Request, requested string, reviewerID *int, previousClosedAt *time.Time) (string, *time.Time, bool) {
	status := requested
	if status == "" {
		status = "open"
	}
	switch status {
	case "open":
		return "open", nil, true
	case "validated":
		if reviewerID == nil {
			respondValidationError(w, r, "finance_reviewer_id is required to validate the close-out")
			return "", nil, false
		}
		closedAt := previousClosedAt
		if closedAt == nil {
			now := time.Now().UTC()
			closedAt = &now
		}
		return "validated", closedAt, true
	default:
		respondValidationError(w, r, "status must be open or validated")
		return "", nil, false
	}
}

// GetCloseOut handles GET /api/p3m/projects/{id}/close-out. It returns the
// project's financial close-out record (FIN-010) or 404 when none exists yet.
func (h *P3MHandler) GetCloseOut(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	if _, err := h.getProjectRow(ctx, id); err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	c, err := scanP3MCloseOut(h.db.QueryRowContext(ctx, `SELECT `+p3mCloseOutColumns+` FROM p3m_financial_close_outs WHERE project_id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_close_out")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, c)
}

// CreateCloseOut handles POST /api/p3m/projects/{id}/close-out. It creates the
// project's single close-out record; status defaults to open and a validated
// creation stamps closed_at immediately.
func (h *P3MHandler) CreateCloseOut(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
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

	var in struct {
		FinalBudget            float64 `json:"final_budget"`
		ActualToDate           float64 `json:"actual_to_date"`
		OutstandingCommitments float64 `json:"outstanding_commitments"`
		OngoingObligations     string  `json:"ongoing_obligations"`
		FinanceReviewerID      *int    `json:"finance_reviewer_id,omitempty"`
		Status                 string  `json:"status"`
		Notes                  string  `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	status, closedAt, ok := p3mCloseOutStatus(w, r, in.Status, in.FinanceReviewerID, nil)
	if !ok {
		return
	}

	var existing int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_financial_close_outs WHERE project_id = ?`, id).Scan(&existing); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if existing > 0 {
		respondConflict(w, r, "close-out already exists for this project; use PUT to update it")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(ctx, `
		INSERT INTO p3m_financial_close_outs (project_id, final_budget, actual_to_date, outstanding_commitments, ongoing_obligations, finance_reviewer_id, status, notes, closed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.FinalBudget, in.ActualToDate, in.OutstandingCommitments, in.OngoingObligations, nullInt(in.FinanceReviewerID), status, in.Notes, nullTimePtr(closedAt), now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	cid64, _ := res.LastInsertId()

	c, err := scanP3MCloseOut(h.db.QueryRowContext(ctx, `SELECT `+p3mCloseOutColumns+` FROM p3m_financial_close_outs WHERE id = ?`, int(cid64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.close_out.create", "p3m_project", &id, project.Title, map[string]any{
		"final_budget":            in.FinalBudget,
		"actual_to_date":          in.ActualToDate,
		"outstanding_commitments": in.OutstandingCommitments,
		"status":                  status,
		"finance_reviewer_id":     nullInt(in.FinanceReviewerID),
	})
	respondJSONCreated(w, c)
}

// UpdateCloseOut handles PUT /api/p3m/projects/{id}/close-out. The body carries
// the full mutable state of the record; moving status to validated is the
// finance sign-off step (mirrors a stage-gate decision) and stamps closed_at,
// while reopening clears it.
func (h *P3MHandler) UpdateCloseOut(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
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
	oldC, err := scanP3MCloseOut(h.db.QueryRowContext(ctx, `SELECT `+p3mCloseOutColumns+` FROM p3m_financial_close_outs WHERE project_id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_close_out")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		FinalBudget            float64 `json:"final_budget"`
		ActualToDate           float64 `json:"actual_to_date"`
		OutstandingCommitments float64 `json:"outstanding_commitments"`
		OngoingObligations     string  `json:"ongoing_obligations"`
		FinanceReviewerID      *int    `json:"finance_reviewer_id,omitempty"`
		Status                 string  `json:"status"`
		Notes                  string  `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	status, closedAt, ok := p3mCloseOutStatus(w, r, in.Status, in.FinanceReviewerID, oldC.ClosedAt)
	if !ok {
		return
	}

	now := time.Now().UTC()
	_, err = h.db.ExecContext(ctx, `
		UPDATE p3m_financial_close_outs SET final_budget = ?, actual_to_date = ?, outstanding_commitments = ?, ongoing_obligations = ?, finance_reviewer_id = ?, status = ?, notes = ?, closed_at = ?, updated_at = ? WHERE id = ?`,
		in.FinalBudget, in.ActualToDate, in.OutstandingCommitments, in.OngoingObligations, nullInt(in.FinanceReviewerID), status, in.Notes, nullTimePtr(closedAt), now, oldC.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	c, err := scanP3MCloseOut(h.db.QueryRowContext(ctx, `SELECT `+p3mCloseOutColumns+` FROM p3m_financial_close_outs WHERE id = ?`, oldC.ID))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.close_out.update", "p3m_project", &id, project.Title, p3mAuditDiff(
		[3]any{"final_budget", oldC.FinalBudget, in.FinalBudget},
		[3]any{"actual_to_date", oldC.ActualToDate, in.ActualToDate},
		[3]any{"outstanding_commitments", oldC.OutstandingCommitments, in.OutstandingCommitments},
		[3]any{"ongoing_obligations", oldC.OngoingObligations, in.OngoingObligations},
		[3]any{"finance_reviewer_id", nullInt(oldC.FinanceReviewerID), nullInt(in.FinanceReviewerID)},
		[3]any{"status", oldC.Status, status},
		[3]any{"notes", oldC.Notes, in.Notes},
	))
	respondJSONOK(w, c)
}
