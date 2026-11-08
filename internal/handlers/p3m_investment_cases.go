// Package handlers: P3M investment cases — the per-project prioritisation
// pipeline. An investment case carries a project's business case and
// risk/benefit/return scores through proposed -> under_review -> approved |
// rejected | deferred, so portfolios can be balanced on timing, strategic
// coverage, benefit, risk and return (the portfolio definition cycle).

package handlers

import (
	"context"
	"net/http"
	"slices"
	"time"

	"windshift/internal/models"
)

var p3mInvestmentCaseStatuses = []string{
	models.P3MICStatusProposed,
	models.P3MICStatusUnderReview,
	models.P3MICStatusApproved,
	models.P3MICStatusRejected,
	models.P3MICStatusDeferred,
}

func p3mValidInvestmentCaseStatus(s string) bool {
	return slices.Contains(p3mInvestmentCaseStatuses, s)
}

// p3mInvestmentCaseScoreOK reports whether a benefit/risk score is in the 1-5 scale.
func p3mInvestmentCaseScoreOK(v int) bool { return v >= 1 && v <= 5 }

const p3mInvestmentCaseColumns = `ic.id, ic.project_id, ic.status, ic.benefit_score, ic.risk_score, ic.roi_percent, ic.payback_months, ic.business_case, ic.page_id, ic.decided_by, ic.decision_date, ic.created_at, ic.updated_at`

// p3mInvestmentCaseListQuery joins the display fields (project title, resolved
// portfolio — a project may sit under a programme only — and decider name) used
// by list responses.
const p3mInvestmentCaseListQuery = `SELECT ` + p3mInvestmentCaseColumns + `,
	p.title AS project_title,
	COALESCE(p.portfolio_id, (SELECT portfolio_id FROM p3m_programmes WHERE id = p.programme_id)) AS portfolio_id,
	pf.title AS portfolio_title,
	COALESCE(u.first_name || ' ' || u.last_name, '') AS decided_by_name
	FROM p3m_investment_cases ic
	JOIN p3m_projects p ON p.id = ic.project_id
	LEFT JOIN p3m_portfolios pf ON pf.id = COALESCE(p.portfolio_id, (SELECT portfolio_id FROM p3m_programmes WHERE id = p.programme_id))
	LEFT JOIN users u ON u.id = ic.decided_by`

func scanP3MInvestmentCaseList(row interface{ Scan(...any) error }) (models.P3MInvestmentCase, error) {
	var c models.P3MInvestmentCase
	err := row.Scan(&c.ID, &c.ProjectID, &c.Status, &c.BenefitScore, &c.RiskScore,
		&c.ROIPercent, &c.PaybackMonths, &c.BusinessCase, &c.PageID, &c.DecidedBy, &c.DecisionDate,
		&c.CreatedAt, &c.UpdatedAt,
		&c.ProjectTitle, &c.PortfolioID, &c.PortfolioTitle, &c.DecidedByName)
	return c, err
}

// loadInvestmentCaseWithDisplay loads one case plus its joined display fields.
func (h *P3MHandler) loadInvestmentCaseWithDisplay(ctx context.Context, id int) (models.P3MInvestmentCase, error) {
	return scanP3MInvestmentCaseList(h.db.QueryRowContext(ctx, p3mInvestmentCaseListQuery+` WHERE ic.id = ?`, id))
}

// getInvestmentCaseRow loads one case without display fields.
func (h *P3MHandler) getInvestmentCaseRow(ctx context.Context, id int) (models.P3MInvestmentCase, error) {
	var c models.P3MInvestmentCase
	err := h.db.QueryRowContext(ctx, `SELECT id, project_id, status, benefit_score, risk_score, roi_percent, payback_months, business_case, page_id, decided_by, decision_date, created_at, updated_at FROM p3m_investment_cases WHERE id = ?`, id).
		Scan(&c.ID, &c.ProjectID, &c.Status, &c.BenefitScore, &c.RiskScore,
			&c.ROIPercent, &c.PaybackMonths, &c.BusinessCase, &c.PageID, &c.DecidedBy, &c.DecisionDate,
			&c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// ListInvestmentCases handles GET /api/p3m/investment-cases?status=&portfolio_id=.
func (h *P3MHandler) ListInvestmentCases(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	query := p3mInvestmentCaseListQuery
	var conds []string
	var args []any
	q := r.URL.Query()
	if v := q.Get("status"); v != "" {
		if !p3mValidInvestmentCaseStatus(v) {
			respondValidationError(w, r, "status must be one of proposed, under_review, approved, rejected or deferred")
			return
		}
		conds = append(conds, "ic.status = ?")
		args = append(args, v)
	}
	if v := q.Get("portfolio_id"); v != "" {
		id, ok := parseQueryInt(v)
		if !ok {
			respondBadRequest(w, r, "invalid portfolio_id")
			return
		}
		conds = append(conds, `COALESCE(p.portfolio_id, (SELECT portfolio_id FROM p3m_programmes WHERE id = p.programme_id)) = ?`)
		args = append(args, id)
	}
	if len(conds) > 0 {
		query += " WHERE " + conds[0]
		for _, c := range conds[1:] {
			query += " AND " + c
		}
	}
	query += ` ORDER BY pf.title COLLATE NOCASE, p.title COLLATE NOCASE`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MInvestmentCase, 0)
	for rows.Next() {
		c, err := scanP3MInvestmentCaseList(rows)
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

// CreateInvestmentCase handles POST /api/p3m/investment-cases. A project has at
// most one case (UNIQUE project_id); a duplicate is rejected with 409. New cases
// start in the proposed stage; terminal statuses are decision-endpoint-only.
func (h *P3MHandler) CreateInvestmentCase(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		ProjectID     int      `json:"project_id"`
		BenefitScore  *int     `json:"benefit_score"`
		RiskScore     *int     `json:"risk_score"`
		ROIPercent    *float64 `json:"roi_percent"`
		PaybackMonths *int     `json:"payback_months"`
		BusinessCase  string   `json:"business_case"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.ProjectID == 0 {
		respondValidationError(w, r, "project_id is required")
		return
	}
	if _, err := h.getProjectRow(r.Context(), in.ProjectID); err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	benefit := 3
	if in.BenefitScore != nil {
		if !p3mInvestmentCaseScoreOK(*in.BenefitScore) {
			respondValidationError(w, r, "benefit_score must be between 1 and 5")
			return
		}
		benefit = *in.BenefitScore
	}
	risk := 3
	if in.RiskScore != nil {
		if !p3mInvestmentCaseScoreOK(*in.RiskScore) {
			respondValidationError(w, r, "risk_score must be between 1 and 5")
			return
		}
		risk = *in.RiskScore
	}

	var existingID int
	err := h.db.QueryRowContext(r.Context(), `SELECT id FROM p3m_investment_cases WHERE project_id = ?`, in.ProjectID).Scan(&existingID)
	if err == nil {
		respondConflict(w, r, "project already has an investment case")
		return
	}
	if !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_investment_cases (project_id, status, benefit_score, risk_score, roi_percent, payback_months, business_case, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ProjectID, models.P3MICStatusProposed, benefit, risk, in.ROIPercent, in.PaybackMonths, in.BusinessCase, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()

	c, err := h.loadInvestmentCaseWithDisplay(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, c)
}

// UpdateInvestmentCase handles PUT /api/p3m/investment-cases/{id}. Scores, ROI,
// payback and business case are mutable; status may only move between proposed
// and under_review — terminal statuses are set by the decision endpoint.
// Omitted fields keep their current values (partial update).
func (h *P3MHandler) UpdateInvestmentCase(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Status        string   `json:"status"`
		BenefitScore  *int     `json:"benefit_score"`
		RiskScore     *int     `json:"risk_score"`
		ROIPercent    *float64 `json:"roi_percent"`
		PaybackMonths *int     `json:"payback_months"`
		BusinessCase  string   `json:"business_case"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	existing, err := h.getInvestmentCaseRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_investment_case")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	benefit := existing.BenefitScore
	if in.BenefitScore != nil {
		if !p3mInvestmentCaseScoreOK(*in.BenefitScore) {
			respondValidationError(w, r, "benefit_score must be between 1 and 5")
			return
		}
		benefit = *in.BenefitScore
	}
	risk := existing.RiskScore
	if in.RiskScore != nil {
		if !p3mInvestmentCaseScoreOK(*in.RiskScore) {
			respondValidationError(w, r, "risk_score must be between 1 and 5")
			return
		}
		risk = *in.RiskScore
	}
	status := existing.Status
	if in.Status != "" {
		if in.Status != models.P3MICStatusProposed && in.Status != models.P3MICStatusUnderReview {
			respondValidationError(w, r, "status may only be set to proposed or under_review; use the decision endpoint for terminal statuses")
			return
		}
		status = in.Status
	}

	roi := existing.ROIPercent
	if in.ROIPercent != nil {
		roi = in.ROIPercent
	}
	payback := existing.PaybackMonths
	if in.PaybackMonths != nil {
		payback = in.PaybackMonths
	}
	businessCase := existing.BusinessCase
	if in.BusinessCase != "" {
		businessCase = in.BusinessCase
	}

	now := time.Now().UTC()
	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_investment_cases SET status = ?, benefit_score = ?, risk_score = ?, roi_percent = ?, payback_months = ?, business_case = ?, updated_at = ? WHERE id = ?`,
		status, benefit, risk, roi, payback, businessCase, now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}

	c, err := h.loadInvestmentCaseWithDisplay(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, c)
}

// DeleteInvestmentCase handles DELETE /api/p3m/investment-cases/{id}. The case
// is removed; the project itself is unaffected.
func (h *P3MHandler) DeleteInvestmentCase(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_investment_cases WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "p3m_investment_case")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DecideInvestmentCase handles POST /api/p3m/investment-cases/{id}/decision. The
// outcome (approved | rejected | deferred) is allowed from any status and the
// last decision wins; notes append to the business case as a dated line.
func (h *P3MHandler) DecideInvestmentCase(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Outcome string `json:"outcome"`
		Notes   string `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	switch in.Outcome {
	case models.P3MICStatusApproved, models.P3MICStatusRejected, models.P3MICStatusDeferred:
	default:
		respondValidationError(w, r, "outcome must be one of approved, rejected or deferred")
		return
	}

	c, err := h.getInvestmentCaseRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_investment_case")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	businessCase := c.BusinessCase
	if in.Notes != "" {
		if businessCase != "" {
			businessCase += "\n"
		}
		businessCase += "[" + today + "] " + in.Notes
	}

	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_investment_cases SET status = ?, decided_by = ?, decision_date = ?, business_case = ?, updated_at = ? WHERE id = ?`,
		in.Outcome, user.ID, today, businessCase, now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}

	out, err := h.loadInvestmentCaseWithDisplay(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}
