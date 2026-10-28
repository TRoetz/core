package handlers

import (
	"context"
	"net/http"
	"time"

	"windshift/internal/models"
)

// scanP3MStageGateReview reads one p3m_stage_gate_reviews row.
func scanP3MStageGateReview(row interface{ Scan(...any) error }) (models.P3MStageGateReview, error) {
	var g models.P3MStageGateReview
	err := row.Scan(&g.ID, &g.ProjectID, &g.FromStage, &g.ToStage, &g.Passed, &g.ReviewerID, &g.ReviewDate, &g.Comments, &g.CreatedAt)
	return g, err
}

const p3mStageGateColumns = `id, project_id, from_stage, to_stage, passed, reviewer_id, review_date, comments, created_at`

// loadP3MStageGateWithArtifacts fetches a gate review plus its artifact rows.
func (h *P3MHandler) loadP3MStageGateWithArtifacts(ctx context.Context, id int) (models.P3MStageGateReview, error) {
	g, err := h.getStageGateRow(ctx, id)
	if err != nil {
		return g, err
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, review_id, name, required, completed, link_or_note FROM p3m_gate_artifacts WHERE review_id = ? ORDER BY id`, id)
	if err != nil {
		return g, err
	}
	defer rows.Close()
	for rows.Next() {
		var a models.P3MGateArtifact
		if err := rows.Scan(&a.ID, &a.ReviewID, &a.Name, &a.Required, &a.Completed, &a.LinkOrNote); err != nil {
			return g, err
		}
		g.Artifacts = append(g.Artifacts, a)
	}
	return g, rows.Err()
}

func (h *P3MHandler) getStageGateRow(ctx context.Context, id int) (models.P3MStageGateReview, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+p3mStageGateColumns+` FROM p3m_stage_gate_reviews WHERE id = ?`, id)
	return scanP3MStageGateReview(row)
}

// ListStageGates handles GET /api/p3m/stage-gates?project_id=.
func (h *P3MHandler) ListStageGates(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	query := `SELECT ` + p3mStageGateColumns + ` FROM p3m_stage_gate_reviews`
	var args []any
	if pid, has := parseQueryInt(r.URL.Query().Get("project_id")); has {
		query += ` WHERE project_id = ?`
		args = append(args, pid)
	}
	query += ` ORDER BY id DESC`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	gates := make([]models.P3MStageGateReview, 0)
	for rows.Next() {
		g, err := scanP3MStageGateReview(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		gates = append(gates, g)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	for i := range gates {
		rows2, err := h.db.QueryContext(ctx, `SELECT id, review_id, name, required, completed, link_or_note FROM p3m_gate_artifacts WHERE review_id = ? ORDER BY id`, gates[i].ID)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		for rows2.Next() {
			var a models.P3MGateArtifact
			if err := rows2.Scan(&a.ID, &a.ReviewID, &a.Name, &a.Required, &a.Completed, &a.LinkOrNote); err != nil {
				rows2.Close()
				respondInternalError(w, r, err)
				return
			}
			gates[i].Artifacts = append(gates[i].Artifacts, a)
		}
		if err := rows2.Err(); err != nil {
			rows2.Close()
			respondInternalError(w, r, err)
			return
		}
		rows2.Close()
	}

	respondJSONOK(w, gates)
}

// CreateStageGate handles POST /api/p3m/stage-gates.
func (h *P3MHandler) CreateStageGate(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		ProjectID int `json:"project_id"`
		ToStage   string `json:"to_stage"`
		Comments  string `json:"comments"`
		Artifacts []struct {
			Name       string `json:"name"`
			Required   bool   `json:"required"`
			LinkOrNote string `json:"link_or_note"`
		} `json:"artifacts"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	project, err := h.getProjectRow(r.Context(), in.ProjectID)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if !p3mValidStage(in.ToStage) {
		respondValidationError(w, r, "to_stage must be one of initiation, planning, execution or closure")
		return
	}
	if in.ToStage == project.CurrentStage {
		respondValidationError(w, r, "to_stage must differ from the current stage")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_stage_gate_reviews (project_id, from_stage, to_stage, passed, reviewer_id, review_date, comments, created_at)
		VALUES (?, ?, ?, false, ?, NULL, ?, ?)`,
		in.ProjectID, project.CurrentStage, in.ToStage, user.ID, in.Comments, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	reviewID64, _ := res.LastInsertId()

	for _, a := range in.Artifacts {
		if a.Name == "" {
			continue
		}
		if _, err := h.db.ExecContext(r.Context(), `
			INSERT INTO p3m_gate_artifacts (review_id, name, required, completed, link_or_note, created_at)
			VALUES (?, ?, ?, false, ?, ?)`, int(reviewID64), a.Name, a.Required, a.LinkOrNote, now); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	gate, err := h.loadP3MStageGateWithArtifacts(r.Context(), int(reviewID64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, gate)
}

// DecideStageGate handles POST /api/p3m/stage-gates/{id}/decision. A passing
// decision requires every required artifact completed and the project still in
// the review's from_stage; it then advances the project to to_stage.
func (h *P3MHandler) DecideStageGate(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Passed bool `json:"passed"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	gate, err := h.loadP3MStageGateWithArtifacts(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_stage_gate")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	project, err := h.getProjectRow(r.Context(), gate.ProjectID)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	now := time.Now().UTC()
	if in.Passed {
		for _, a := range gate.Artifacts {
			if a.Required && !a.Completed {
				respondConflict(w, r, "required artifacts are not all completed")
				return
			}
		}
		if project.CurrentStage != gate.FromStage {
			respondConflict(w, r, "project stage changed since the gate was created")
			return
		}
		if _, err := h.db.ExecContext(r.Context(), `
			UPDATE p3m_projects SET current_stage = ?, updated_at = ? WHERE id = ?`,
			gate.ToStage, now, project.ID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_stage_gate_reviews SET passed = ?, review_date = ? WHERE id = ?`,
		in.Passed, now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}

	gate, err = h.loadP3MStageGateWithArtifacts(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, gate)
}

// UpdateGateArtifact handles PUT /api/p3m/gate-artifacts/{id}. Only the
// completion flag and the link/note are mutable.
func (h *P3MHandler) UpdateGateArtifact(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Completed  bool   `json:"completed"`
		LinkOrNote string `json:"link_or_note"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	res, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_gate_artifacts SET completed = ?, link_or_note = ? WHERE id = ?`,
		in.Completed, in.LinkOrNote, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "p3m_gate_artifact")
		return
	}

	var a models.P3MGateArtifact
	err = h.db.QueryRowContext(r.Context(), `SELECT id, review_id, name, required, completed, link_or_note FROM p3m_gate_artifacts WHERE id = ?`, id).
		Scan(&a.ID, &a.ReviewID, &a.Name, &a.Required, &a.Completed, &a.LinkOrNote)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, a)
}

// p3mValidRaciRole reports whether role is one of the four RACI roles.
func p3mValidRaciRole(role string) bool {
	switch role {
	case "responsible", "accountable", "consulted", "informed":
		return true
	default:
		return false
	}
}

// p3mEntityExists checks that a polymorphic tier reference points at a real row.
func (h *P3MHandler) p3mEntityExists(ctx context.Context, entityType string, entityID int) bool {
	switch entityType {
	case models.P3MEntityPortfolio:
		return h.portfolioExists(ctx, entityID)
	case models.P3MEntityProgramme:
		var n int
		if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_programmes WHERE id = ?`, entityID).Scan(&n); err != nil {
			return false
		}
		return n > 0
	case models.P3MEntityProject:
		var n int
		if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, entityID).Scan(&n); err != nil {
			return false
		}
		return n > 0
	default:
		return false
	}
}

// GetRaciMatrix handles GET /api/p3m/raci?entity_type=&entity_id=.
func (h *P3MHandler) GetRaciMatrix(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	entityType := r.URL.Query().Get("entity_type")
	entityID, hasEntityID := parseQueryInt(r.URL.Query().Get("entity_id"))
	if entityType == "" || !hasEntityID {
		respondBadRequest(w, r, "entity_type and entity_id are required")
		return
	}
	if !h.p3mEntityExists(r.Context(), entityType, entityID) {
		respondNotFound(w, r, "p3m_entity")
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `SELECT id, entity_type, entity_id, user_id, role FROM p3m_raci_assignments WHERE entity_type = ? AND entity_id = ? ORDER BY id`, entityType, entityID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MRaciAssignment, 0)
	for rows.Next() {
		var a models.P3MRaciAssignment
		if err := rows.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.UserID, &a.Role); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// UpsertRaciAssignment handles PUT /api/p3m/raci. The (entity_type, entity_id,
// user_id) triple is unique; a second write updates the role in place.
func (h *P3MHandler) UpsertRaciAssignment(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		EntityType string `json:"entity_type"`
		EntityID   int    `json:"entity_id"`
		UserID     int    `json:"user_id"`
		Role       string `json:"role"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mEntityExists(r.Context(), in.EntityType, in.EntityID) {
		respondNotFound(w, r, "p3m_entity")
		return
	}
	var userCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE id = ?`, in.UserID).Scan(&userCount); err != nil || userCount == 0 {
		respondNotFound(w, r, "user")
		return
	}
	if !p3mValidRaciRole(in.Role) {
		respondValidationError(w, r, "role must be one of responsible, accountable, consulted or informed")
		return
	}

	if _, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_raci_assignments (entity_type, entity_id, user_id, role)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(entity_type, entity_id, user_id) DO UPDATE SET role = excluded.role`,
		in.EntityType, in.EntityID, in.UserID, in.Role); err != nil {
		respondInternalError(w, r, err)
		return
	}

	var a models.P3MRaciAssignment
	err := h.db.QueryRowContext(r.Context(), `SELECT id, entity_type, entity_id, user_id, role FROM p3m_raci_assignments WHERE entity_type = ? AND entity_id = ? AND user_id = ?`,
		in.EntityType, in.EntityID, in.UserID).Scan(&a.ID, &a.EntityType, &a.EntityID, &a.UserID, &a.Role)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, a)
}

// DeleteRaciAssignment handles DELETE /api/p3m/raci/{id}.
func (h *P3MHandler) DeleteRaciAssignment(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_raci_assignments WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "p3m_raci_assignment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListAuthorizationThresholds handles GET /api/p3m/authorization-thresholds.
func (h *P3MHandler) ListAuthorizationThresholds(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id, level, threshold_amount FROM p3m_authorization_thresholds ORDER BY level`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MAuthorizationThreshold, 0)
	for rows.Next() {
		var t models.P3MAuthorizationThreshold
		if err := rows.Scan(&t.ID, &t.Level, &t.ThresholdAmount); err != nil {
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

// UpdateAuthorizationThreshold handles PUT /api/p3m/authorization-thresholds.
func (h *P3MHandler) UpdateAuthorizationThreshold(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		Level           string  `json:"level"`
		ThresholdAmount float64 `json:"threshold_amount"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	switch in.Level {
	case models.P3MEntityPortfolio, models.P3MEntityProgramme, models.P3MEntityProject:
	default:
		respondValidationError(w, r, "level must be one of portfolio, programme or project")
		return
	}
	if in.ThresholdAmount < 0 {
		respondValidationError(w, r, "threshold_amount must not be negative")
		return
	}

	now := time.Now().UTC()
	if _, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_authorization_thresholds (level, threshold_amount, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(level) DO UPDATE SET threshold_amount = excluded.threshold_amount, updated_at = excluded.updated_at`,
		in.Level, in.ThresholdAmount, now); err != nil {
		respondInternalError(w, r, err)
		return
	}

	var t models.P3MAuthorizationThreshold
	err := h.db.QueryRowContext(r.Context(), `SELECT id, level, threshold_amount FROM p3m_authorization_thresholds WHERE level = ?`, in.Level).
		Scan(&t.ID, &t.Level, &t.ThresholdAmount)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, t)
}

// p3mThresholdMap loads all authorization thresholds keyed by level.
func (h *P3MHandler) p3mThresholdMap(ctx context.Context) (map[string]float64, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT level, threshold_amount FROM p3m_authorization_thresholds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]float64)
	for rows.Next() {
		var level string
		var amount float64
		if err := rows.Scan(&level, &amount); err != nil {
			return nil, err
		}
		out[level] = amount
	}
	return out, rows.Err()
}

// p3mApplyProjectBudget persists a new approved budget on a project and
// recomputes its RAG status from the updated figures.
func (h *P3MHandler) p3mApplyProjectBudget(ctx context.Context, projectID int, requestedAmount float64) error {
	p, err := h.getProjectRow(ctx, projectID)
	if err != nil {
		return err
	}
	p.BudgetApproved = requestedAmount
	p3mApplyComputed(&p)
	now := time.Now().UTC()
	_, err = h.db.ExecContext(ctx, `
		UPDATE p3m_projects SET budget_approved = ?, rag_status = ?, updated_at = ? WHERE id = ?`,
		requestedAmount, p.RagStatus, now, projectID)
	return err
}

// CreateBudgetApproval handles POST /api/p3m/budget-approvals. The increase is
// routed by the tier authorization thresholds; increases within the project's
// own threshold are approved and applied immediately.
func (h *P3MHandler) CreateBudgetApproval(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		ProjectID       int     `json:"project_id"`
		RequestAmount   float64 `json:"requested_amount"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	project, err := h.getProjectRow(r.Context(), in.ProjectID)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if in.RequestAmount <= project.BudgetApproved {
		respondValidationError(w, r, "requested_amount must exceed the current approved budget")
		return
	}

	thresholds, err := h.p3mThresholdMap(r.Context())
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	delta := in.RequestAmount - project.BudgetApproved
	routedToLevel := models.P3MEntityPortfolio
	switch {
	case delta <= thresholds[models.P3MEntityProject]:
		routedToLevel = models.P3MEntityProject
	case delta <= thresholds[models.P3MEntityProgramme]:
		routedToLevel = models.P3MEntityProgramme
	}

	status := "pending"
	var decidedBy *int
	decisionNotes := ""
	if routedToLevel == models.P3MEntityProject {
		status = "approved"
		decidedBy = &user.ID
		decisionNotes = "auto-approved within project threshold"
		if err := h.p3mApplyProjectBudget(r.Context(), in.ProjectID, in.RequestAmount); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_budget_approvals (project_id, current_budget, requested_amount, routed_to_level, status, requested_by, decided_by, decision_notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ProjectID, project.BudgetApproved, in.RequestAmount, routedToLevel, status, user.ID, nullInt(decidedBy), decisionNotes, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var a models.P3MBudgetApproval
	id64, _ := res.LastInsertId()
	err = h.db.QueryRowContext(r.Context(), `SELECT id, project_id, current_budget, requested_amount, routed_to_level, status, requested_by, decided_by, decision_notes, created_at, updated_at FROM p3m_budget_approvals WHERE id = ?`, int(id64)).
		Scan(&a.ID, &a.ProjectID, &a.CurrentBudget, &a.RequestAmount, &a.RoutedToLevel, &a.Status, &a.RequestedBy, &a.DecidedBy, &a.DecisionNotes, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, a)
}

// ListBudgetApprovals handles GET /api/p3m/budget-approvals?status=&project_id=.
func (h *P3MHandler) ListBudgetApprovals(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	query := `SELECT id, project_id, current_budget, requested_amount, routed_to_level, status, requested_by, decided_by, decision_notes, created_at, updated_at FROM p3m_budget_approvals`
	var conds []string
	var args []any
	if s := r.URL.Query().Get("status"); s != "" {
		conds = append(conds, "status = ?")
		args = append(args, s)
	}
	if pid, has := parseQueryInt(r.URL.Query().Get("project_id")); has {
		conds = append(conds, "project_id = ?")
		args = append(args, pid)
	}
	if len(conds) > 0 {
		query += ` WHERE ` + joinPlaceholders(conds)
	}
	query += ` ORDER BY id DESC`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MBudgetApproval, 0)
	for rows.Next() {
		var a models.P3MBudgetApproval
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.CurrentBudget, &a.RequestAmount, &a.RoutedToLevel, &a.Status, &a.RequestedBy, &a.DecidedBy, &a.DecisionNotes, &a.CreatedAt, &a.UpdatedAt); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// DecideBudgetApproval handles POST /api/p3m/budget-approvals/{id}/decision.
// Approval applies the requested amount to the project budget; rejection leaves
// the current budget in force. Only pending approvals can be decided.
func (h *P3MHandler) DecideBudgetApproval(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Passed bool   `json:"passed"`
		Notes  string `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	var a models.P3MBudgetApproval
	err := h.db.QueryRowContext(r.Context(), `SELECT id, project_id, current_budget, requested_amount, routed_to_level, status, requested_by, decided_by, decision_notes, created_at, updated_at FROM p3m_budget_approvals WHERE id = ?`, id).
		Scan(&a.ID, &a.ProjectID, &a.CurrentBudget, &a.RequestAmount, &a.RoutedToLevel, &a.Status, &a.RequestedBy, &a.DecidedBy, &a.DecisionNotes, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_budget_approval")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if a.Status != "pending" {
		respondConflict(w, r, "approval already decided")
		return
	}

	status := "rejected"
	if in.Passed {
		if err := h.p3mApplyProjectBudget(r.Context(), a.ProjectID, a.RequestAmount); err != nil {
			respondInternalError(w, r, err)
			return
		}
		status = "approved"
	}

	now := time.Now().UTC()
	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_budget_approvals SET status = ?, decided_by = ?, decision_notes = ?, updated_at = ? WHERE id = ?`,
		status, user.ID, in.Notes, now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}

	err = h.db.QueryRowContext(r.Context(), `SELECT id, project_id, current_budget, requested_amount, routed_to_level, status, requested_by, decided_by, decision_notes, created_at, updated_at FROM p3m_budget_approvals WHERE id = ?`, id).
		Scan(&a.ID, &a.ProjectID, &a.CurrentBudget, &a.RequestAmount, &a.RoutedToLevel, &a.Status, &a.RequestedBy, &a.DecidedBy, &a.DecisionNotes, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, a)
}
