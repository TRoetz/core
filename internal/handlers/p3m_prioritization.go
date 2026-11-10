package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// Portfolio prioritisation scoring criteria + investment case scores
// (PO-003/004/008). Score changes are audit-emitted so the score history is
// reconstructable from the audit log.
// ---------------------------------------------------------------------------

const p3mScoringCriterionColumns = `id, name, weight, direction, description, enabled, created_at, updated_at`

func scanP3MScoringCriterion(row interface{ Scan(...any) error }) (models.P3MScoringCriterion, error) {
	var c models.P3MScoringCriterion
	err := row.Scan(&c.ID, &c.Name, &c.Weight, &c.Direction, &c.Description, &c.Enabled, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func p3mValidScoringDirection(d string) bool {
	switch d {
	case "higher_better", "lower_better":
		return true
	default:
		return false
	}
}

// ListScoringCriteria handles GET /api/p3m/scoring-criteria.
func (h *P3MHandler) ListScoringCriteria(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+p3mScoringCriterionColumns+` FROM p3m_scoring_criteria ORDER BY name`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MScoringCriterion, 0)
	for rows.Next() {
		c, err := scanP3MScoringCriterion(rows)
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

// CreateScoringCriterion handles POST /api/p3m/scoring-criteria. Names are
// unique; a duplicate returns 409.
func (h *P3MHandler) CreateScoringCriterion(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	var in struct {
		Name        string  `json:"name"`
		Weight      float64 `json:"weight"`
		Direction   string  `json:"direction"`
		Description *string `json:"description"`
		Enabled     *bool   `json:"enabled"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return
	}
	if in.Weight < 0 {
		respondValidationError(w, r, "weight must be >= 0")
		return
	}
	if !p3mValidScoringDirection(in.Direction) {
		respondValidationError(w, r, "direction must be higher_better or lower_better")
		return
	}

	var existing int
	if err := h.db.QueryRowContext(ctx, `SELECT id FROM p3m_scoring_criteria WHERE name = ?`, in.Name).Scan(&existing); err == nil {
		respondConflict(w, r, "A scoring criterion with this name already exists")
		return
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO p3m_scoring_criteria (name, weight, direction, description, enabled) VALUES (?, ?, ?, ?, ?)`,
		in.Name, in.Weight, in.Direction, in.Description, enabled)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	c, err := scanP3MScoringCriterion(h.db.QueryRowContext(ctx, `SELECT `+p3mScoringCriterionColumns+` FROM p3m_scoring_criteria WHERE id = ?`, int(id64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_scoring_criterion_created", "p3m_scoring_criterion", &c.ID, in.Name)
	respondJSONCreated(w, c)
}

// UpdateScoringCriterion handles PUT /api/p3m/scoring-criteria/{id}.
func (h *P3MHandler) UpdateScoringCriterion(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name        string  `json:"name"`
		Weight      float64 `json:"weight"`
		Direction   string  `json:"direction"`
		Description *string `json:"description"`
		Enabled     *bool   `json:"enabled"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return
	}
	if in.Weight < 0 {
		respondValidationError(w, r, "weight must be >= 0")
		return
	}
	if !p3mValidScoringDirection(in.Direction) {
		respondValidationError(w, r, "direction must be higher_better or lower_better")
		return
	}

	var dup int
	if err := h.db.QueryRowContext(ctx, `SELECT id FROM p3m_scoring_criteria WHERE name = ? AND id != ?`, in.Name, id).Scan(&dup); err == nil {
		respondConflict(w, r, "A scoring criterion with this name already exists")
		return
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	res, err := h.db.ExecContext(ctx, `UPDATE p3m_scoring_criteria SET name = ?, weight = ?, direction = ?, description = ?, enabled = ? WHERE id = ?`,
		in.Name, in.Weight, in.Direction, in.Description, enabled, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_scoring_criterion")
		return
	}
	c, err := scanP3MScoringCriterion(h.db.QueryRowContext(ctx, `SELECT `+p3mScoringCriterionColumns+` FROM p3m_scoring_criteria WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_scoring_criterion_updated", "p3m_scoring_criterion", &id, in.Name)
	respondJSONOK(w, c)
}

// DeleteScoringCriterion handles DELETE /api/p3m/scoring-criteria/{id}. The
// investment case scores referencing it are removed by the FK cascade.
func (h *P3MHandler) DeleteScoringCriterion(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_scoring_criteria WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_scoring_criterion")
		return
	}
	logAudit(h.db, r, user, "p3m_scoring_criterion_deleted", "p3m_scoring_criterion", &id, "")
	respondJSONOK(w, map[string]any{"deleted": true})
}

// UpsertInvestmentCaseScore handles POST /api/p3m/investment-cases/{ic_id}/scores.
// A score is unique per (investment case, criterion); re-scoring updates the
// existing row and emits an audit event carrying the new value (PO-008).
func (h *P3MHandler) UpsertInvestmentCaseScore(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	icID, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		CriterionID int     `json:"criterion_id"`
		Score       float64 `json:"score"`
		Rationale   *string `json:"rationale"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.CriterionID <= 0 {
		respondValidationError(w, r, "criterion_id is required")
		return
	}
	if in.Score < 1 || in.Score > 5 {
		respondValidationError(w, r, "score must be between 1 and 5")
		return
	}

	var icExists int
	if err := h.db.QueryRowContext(ctx, `SELECT id FROM p3m_investment_cases WHERE id = ?`, icID).Scan(&icExists); err != nil {
		respondNotFound(w, r, "p3m_investment_case")
		return
	}
	var critName string
	if err := h.db.QueryRowContext(ctx, `SELECT name FROM p3m_scoring_criteria WHERE id = ?`, in.CriterionID).Scan(&critName); err != nil {
		respondNotFound(w, r, "p3m_scoring_criterion")
		return
	}

	if _, err := h.db.ExecContext(ctx, `
		INSERT INTO p3m_investment_case_scores (ic_id, criterion_id, score, rationale, scored_by)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(ic_id, criterion_id) DO UPDATE SET
			score = excluded.score,
			rationale = excluded.rationale,
			scored_by = excluded.scored_by`, icID, in.CriterionID, in.Score, in.Rationale, user.ID); err != nil {
		respondInternalError(w, r, err)
		return
	}

	var s models.P3MInvestmentCaseScore
	var rationale sql.NullString
	var scoredBy sql.NullInt64
	err := h.db.QueryRowContext(ctx, `SELECT id, ic_id, criterion_id, score, rationale, scored_by, created_at, updated_at FROM p3m_investment_case_scores WHERE ic_id = ? AND criterion_id = ?`, icID, in.CriterionID).
		Scan(&s.ID, &s.ICID, &s.CriterionID, &s.Score, &rationale, &scoredBy, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if rationale.Valid {
		s.Rationale = &rationale.String
	}
	if scoredBy.Valid {
		v := int(scoredBy.Int64)
		s.ScoredBy = &v
	}

	logAudit(h.db, r, user, "p3m_investment_case_score_upserted", "p3m_investment_case_score", &s.ID, critName+"="+formatScore(in.Score))
	respondJSONCreated(w, s)
}

// ListInvestmentCaseScores handles GET /api/p3m/investment-cases/{ic_id}/scores.
// Each row is joined with its criterion so clients can render the scorecard.
func (h *P3MHandler) ListInvestmentCaseScores(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	icID, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var icExists int
	if err := h.db.QueryRowContext(ctx, `SELECT id FROM p3m_investment_cases WHERE id = ?`, icID).Scan(&icExists); err != nil {
		respondNotFound(w, r, "p3m_investment_case")
		return
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT s.id, s.ic_id, s.criterion_id, s.score, s.rationale, s.scored_by, s.created_at, s.updated_at,
		       c.name, c.weight, c.direction
		FROM p3m_investment_case_scores s
		JOIN p3m_scoring_criteria c ON c.id = s.criterion_id
		WHERE s.ic_id = ?
		ORDER BY c.name`, icID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var s models.P3MInvestmentCaseScore
		var rationale sql.NullString
		var scoredBy sql.NullInt64
		var critName string
		var weight float64
		var direction string
		if err := rows.Scan(&s.ID, &s.ICID, &s.CriterionID, &s.Score, &rationale, &scoredBy, &s.CreatedAt, &s.UpdatedAt, &critName, &weight, &direction); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if rationale.Valid {
			s.Rationale = &rationale.String
		}
		if scoredBy.Valid {
			v := int(scoredBy.Int64)
			s.ScoredBy = &v
		}
		out = append(out, map[string]any{
			"score":        s,
			"criterion_id": s.CriterionID,
			"criterion":    critName,
			"weight":       weight,
			"direction":    direction,
		})
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// GetInvestmentCaseScoreHistory handles GET /api/p3m/investment-cases/{ic_id}/score-history.
// Reconstructs the score timeline from the audit events emitted by
// UpsertInvestmentCaseScore (PO-008): one entry per recorded change, oldest first.
// The value is parsed from the event's resource name ("criterion=score"); when it
// cannot be parsed only the raw string is returned.
func (h *P3MHandler) GetInvestmentCaseScoreHistory(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	icID, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var icExists int
	if err := h.db.QueryRowContext(ctx, `SELECT id FROM p3m_investment_cases WHERE id = ?`, icID).Scan(&icExists); err != nil {
		respondNotFound(w, r, "p3m_investment_case")
		return
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT al.timestamp, COALESCE(al.username, ''), COALESCE(al.resource_name, '')
		FROM audit_logs al
		JOIN p3m_investment_case_scores s ON s.id = al.resource_id
		WHERE al.action_type = 'p3m_investment_case_score_upserted'
		  AND al.resource_type = 'p3m_investment_case_score'
		  AND s.ic_id = ?
		ORDER BY al.timestamp ASC, al.id ASC`, icID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var at time.Time
		var username, resourceName string
		if err := rows.Scan(&at, &username, &resourceName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		entry := map[string]any{
			"at":   at.Format(time.RFC3339),
			"user": username,
			"raw":  resourceName,
		}
		if i := strings.LastIndex(resourceName, "="); i > 0 {
			entry["criterion"] = resourceName[:i]
			if v, perr := strconv.ParseFloat(resourceName[i+1:], 64); perr == nil {
				entry["score"] = v
			}
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// p3mCompositeScore computes the weighted composite for one investment case:
// sum(weight * effective score) / sum(weight) over its enabled criteria that
// have a recorded score. lower_better criteria are inverted on the 1-5 scale.
func (h *P3MHandler) p3mCompositeScore(ctx context.Context, icID int) (*float64, int, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT s.score, c.weight, c.direction
		FROM p3m_investment_case_scores s
		JOIN p3m_scoring_criteria c ON c.id = s.criterion_id
		WHERE s.ic_id = ? AND c.enabled = 1`, icID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	totalWeight := 0.0
	weighted := 0.0
	count := 0
	for rows.Next() {
		var score, weight float64
		var direction string
		if err := rows.Scan(&score, &weight, &direction); err != nil {
			return nil, 0, err
		}
		eff := score
		if direction == "lower_better" {
			eff = 6 - score
		}
		totalWeight += weight
		weighted += weight * eff
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if count == 0 || totalWeight <= 0 {
		return nil, 0, nil
	}
	composite := weighted / totalWeight
	return &composite, count, nil
}

// GetRankedInvestmentCases handles GET /api/p3m/investment-cases/ranked. With
// no filter it ranks every investment case in the module; portfolio_id or
// programme_id restricts the ranking to that tier (PO-004). Cases without any
// recorded score rank last with a null composite.
func (h *P3MHandler) GetRankedInvestmentCases(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	var projectFilter string
	var args []any
	switch {
	case r.URL.Query().Get("portfolio_id") != "":
		id, okID := parseQueryInt(r.URL.Query().Get("portfolio_id"))
		if !okID {
			respondValidationError(w, r, "portfolio_id must be an integer")
			return
		}
		projectFilter = `pp.portfolio_id = ?`
		args = append(args, id)
	case r.URL.Query().Get("programme_id") != "":
		id, okID := parseQueryInt(r.URL.Query().Get("programme_id"))
		if !okID {
			respondValidationError(w, r, "programme_id must be an integer")
			return
		}
		projectFilter = `pp.programme_id = ?`
		args = append(args, id)
	default:
		projectFilter = `1=1`
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT ic.id, ic.project_id, ic.status, pp.title
		FROM p3m_investment_cases ic
		JOIN p3m_projects pp ON pp.id = ic.project_id
		WHERE `+projectFilter+`
		ORDER BY ic.id`, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	type p3mRankedIC struct {
		icID        int
		projectID   int
		status      string
		title       string
		composite   *float64
		scoredCount int
	}
	ranked := make([]p3mRankedIC, 0)
	for rows.Next() {
		var item p3mRankedIC
		if err := rows.Scan(&item.icID, &item.projectID, &item.status, &item.title); err != nil {
			respondInternalError(w, r, err)
			return
		}
		item.composite, item.scoredCount, _ = h.p3mCompositeScore(ctx, item.icID)
		ranked = append(ranked, item)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && p3mRankBefore(ranked[j], ranked[j-1]); j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}

	out := make([]map[string]any, 0, len(ranked))
	for i, item := range ranked {
		out = append(out, map[string]any{
			"rank":            i + 1,
			"ic_id":           item.icID,
			"project_id":      item.projectID,
			"title":           item.title,
			"status":          item.status,
			"composite_score": round2(item.composite),
			"scored_criteria": item.scoredCount,
		})
	}
	respondJSONOK(w, map[string]any{
		"ranking": out,
	})
}

// p3mRankBefore reports whether a should sort before b: scored composites
// descending first, then unscored cases by title.
func p3mRankBefore(a, b struct {
	icID        int
	projectID   int
	status      string
	title       string
	composite   *float64
	scoredCount int
}) bool {
	switch {
	case a.composite != nil && b.composite == nil:
		return true
	case a.composite == nil && b.composite != nil:
		return false
	case a.composite != nil && b.composite != nil:
		if *a.composite != *b.composite {
			return *a.composite > *b.composite
		}
		return a.title < b.title
	default:
		return a.title < b.title
	}
}

// GetPortfolioRoadmap handles GET /api/p3m/portfolios/{id}/roadmap: the dated
// projects under a portfolio in start-date order (PO-005).
func (h *P3MHandler) GetPortfolioRoadmap(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, title, programme_id, work_type, start_date, target_completion_date, progress_percent, rag_status
		FROM p3m_projects
		WHERE portfolio_id = ? AND (start_date IS NOT NULL OR target_completion_date IS NOT NULL)
		ORDER BY COALESCE(start_date, '9999-12-31'), id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var pid int
		var title string
		var programmeID sql.NullInt64
		var workType sql.NullString
		var start, target sql.NullTime
		var progress int
		var rag string
		if err := rows.Scan(&pid, &title, &programmeID, &workType, &start, &target, &progress, &rag); err != nil {
			respondInternalError(w, r, err)
			return
		}
		entry := map[string]any{
			"id":                 pid,
			"title":              title,
			"progress_percent":   progress,
			"rag_status":         rag,
		}
		if programmeID.Valid {
			entry["programme_id"] = int(programmeID.Int64)
		}
		if workType.Valid && workType.String != "" {
			entry["work_type"] = workType.String
		}
		if start.Valid {
			entry["start_date"] = start.Time.UTC().Format("2006-01-02")
		}
		if target.Valid {
			entry["target_completion_date"] = target.Time.UTC().Format("2006-01-02")
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, map[string]any{
		"portfolio_id": id,
		"projects":     out,
	})
}

// round2 rounds a pointer to two decimals (nil stays nil).
func round2(v *float64) any {
	if v == nil {
		return nil
	}
	return float64(int(*v*100+0.5)) / 100
}

// formatScore renders a score for audit detail without trailing zeros.
func formatScore(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
