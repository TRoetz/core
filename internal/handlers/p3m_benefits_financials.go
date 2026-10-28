package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"windshift/internal/models"
)

const p3mBenefitColumns = `id, project_id, programme_id, title, benefit_type, target_value, unit_of_measure, baseline_value, target_realization_date, owner_id, created_at`

func scanP3MBenefit(row interface{ Scan(...any) error }) (models.P3MBenefit, error) {
	var b models.P3MBenefit
	err := row.Scan(&b.ID, &b.ProjectID, &b.ProgrammeID, &b.Title, &b.BenefitType, &b.TargetValue, &b.UnitOfMeasure, &b.BaselineValue, &b.TargetRealizationDate, &b.OwnerID, &b.CreatedAt)
	return b, err
}

// p3mValidBenefitType reports whether benefit_type is one of the four kinds.
func p3mValidBenefitType(t string) bool {
	switch t {
	case "financial", "non_financial", "strategic", "compliance":
		return true
	default:
		return false
	}
}

// p3mValidateBenefitParent enforces the strict single-parent rule: exactly one
// of project/programme is set and it must reference an existing entity.
func (h *P3MHandler) p3mValidateBenefitParent(ctx context.Context, w http.ResponseWriter, r *http.Request, projectID, programmeID *int) bool {
	if (projectID == nil) == (programmeID == nil) {
		respondValidationError(w, r, "exactly one of project_id or programme_id is required")
		return false
	}
	var ok bool
	if projectID != nil {
		ok = h.p3mEntityExists(ctx, models.P3MEntityProject, *projectID)
		if !ok {
			respondNotFound(w, r, "p3m_project")
		}
	} else {
		var n int
		if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_programmes WHERE id = ?`, *programmeID).Scan(&n); err != nil || n == 0 {
			respondNotFound(w, r, "p3m_programme")
			ok = false
		} else {
			ok = true
		}
	}
	return ok
}

// p3mLatestRealizedValue returns the most recent realization value for a benefit.
func (h *P3MHandler) p3mLatestRealizedValue(ctx context.Context, benefitID int) (*float64, error) {
	var v *float64
	err := h.db.QueryRowContext(ctx, `SELECT realized_value FROM p3m_benefit_realizations WHERE benefit_id = ? ORDER BY recorded_date DESC, id DESC LIMIT 1`, benefitID).Scan(&v)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return v, nil
}

// ListBenefits handles GET /api/p3m/benefits?project_id=&programme_id=.
func (h *P3MHandler) ListBenefits(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	query := `SELECT ` + p3mBenefitColumns + ` FROM p3m_benefits`
	var conds []string
	var args []any
	if pid, has := parseQueryInt(r.URL.Query().Get("project_id")); has {
		conds = append(conds, "project_id = ?")
		args = append(args, pid)
	}
	if gid, has := parseQueryInt(r.URL.Query().Get("programme_id")); has {
		conds = append(conds, "programme_id = ?")
		args = append(args, gid)
	}
	if len(conds) > 0 {
		query += ` WHERE ` + joinPlaceholders(conds)
	}
	query += ` ORDER BY title`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MBenefit, 0)
	for rows.Next() {
		b, err := scanP3MBenefit(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		v, err := h.p3mLatestRealizedValue(ctx, b.ID)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		b.LatestRealizedValue = v
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateBenefit handles POST /api/p3m/benefits.
func (h *P3MHandler) CreateBenefit(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		ProjectID             *int      `json:"project_id,omitempty"`
		ProgrammeID           *int      `json:"programme_id,omitempty"`
		Title                 string    `json:"title"`
		BenefitType           string    `json:"benefit_type"`
		TargetValue           float64   `json:"target_value"`
		UnitOfMeasure         string    `json:"unit_of_measure"`
		BaselineValue         float64   `json:"baseline_value"`
		TargetRealizationDate time.Time `json:"target_realization_date"`
		OwnerID               int       `json:"owner_id"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	if !h.p3mValidateBenefitParent(r.Context(), w, r, in.ProjectID, in.ProgrammeID) {
		return
	}
	if !p3mValidBenefitType(in.BenefitType) {
		respondValidationError(w, r, "benefit_type must be one of financial, non_financial, strategic or compliance")
		return
	}
	var ownerCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE id = ?`, in.OwnerID).Scan(&ownerCount); err != nil || ownerCount == 0 {
		respondNotFound(w, r, "user")
		return
	}
	if in.TargetRealizationDate.IsZero() {
		respondValidationError(w, r, "target_realization_date is required")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_benefits (project_id, programme_id, title, benefit_type, target_value, unit_of_measure, baseline_value, target_realization_date, owner_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullInt(in.ProjectID), nullInt(in.ProgrammeID), in.Title, in.BenefitType, in.TargetValue, in.UnitOfMeasure, in.BaselineValue, in.TargetRealizationDate, in.OwnerID, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()

	b, err := h.getBenefitRow(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, b)
}

func (h *P3MHandler) getBenefitRow(ctx context.Context, id int) (models.P3MBenefit, error) {
	b, err := scanP3MBenefit(h.db.QueryRowContext(ctx, `SELECT `+p3mBenefitColumns+` FROM p3m_benefits WHERE id = ?`, id))
	if err != nil {
		return b, err
	}
	v, err := h.p3mLatestRealizedValue(ctx, id)
	if err != nil {
		return b, err
	}
	b.LatestRealizedValue = v
	return b, nil
}

// GetBenefit handles GET /api/p3m/benefits/{id}.
func (h *P3MHandler) GetBenefit(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	b, err := h.getBenefitRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_benefit")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, b)
}

// UpdateBenefit handles PUT /api/p3m/benefits/{id}. The body carries the full
// mutable state; the parent binding may move but must keep exactly one set.
func (h *P3MHandler) UpdateBenefit(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		ProjectID             *int      `json:"project_id,omitempty"`
		ProgrammeID           *int      `json:"programme_id,omitempty"`
		Title                 string    `json:"title"`
		BenefitType           string    `json:"benefit_type"`
		TargetValue           float64   `json:"target_value"`
		UnitOfMeasure         string    `json:"unit_of_measure"`
		BaselineValue         float64   `json:"baseline_value"`
		TargetRealizationDate time.Time `json:"target_realization_date"`
		OwnerID               int       `json:"owner_id"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	if !h.p3mValidateBenefitParent(r.Context(), w, r, in.ProjectID, in.ProgrammeID) {
		return
	}
	if !p3mValidBenefitType(in.BenefitType) {
		respondValidationError(w, r, "benefit_type must be one of financial, non_financial, strategic or compliance")
		return
	}
	var ownerCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE id = ?`, in.OwnerID).Scan(&ownerCount); err != nil || ownerCount == 0 {
		respondNotFound(w, r, "user")
		return
	}
	if in.TargetRealizationDate.IsZero() {
		respondValidationError(w, r, "target_realization_date is required")
		return
	}

	res, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_benefits SET project_id = ?, programme_id = ?, title = ?, benefit_type = ?, target_value = ?, unit_of_measure = ?, baseline_value = ?, target_realization_date = ?, owner_id = ? WHERE id = ?`,
		nullInt(in.ProjectID), nullInt(in.ProgrammeID), in.Title, in.BenefitType, in.TargetValue, in.UnitOfMeasure, in.BaselineValue, in.TargetRealizationDate, in.OwnerID, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "p3m_benefit")
		return
	}

	b, err := h.getBenefitRow(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, b)
}

// DeleteBenefit handles DELETE /api/p3m/benefits/{id}. Realizations cascade.
func (h *P3MHandler) DeleteBenefit(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_benefits WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "p3m_benefit")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListBenefitRealizations handles GET /api/p3m/benefits/{id}/realizations.
func (h *P3MHandler) ListBenefitRealizations(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var count int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_benefits WHERE id = ?`, id).Scan(&count); err != nil || count == 0 {
		respondNotFound(w, r, "p3m_benefit")
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `SELECT id, benefit_id, recorded_date, realized_value, notes, recorded_by, created_at FROM p3m_benefit_realizations WHERE benefit_id = ? ORDER BY recorded_date ASC, id ASC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MBenefitRealization, 0)
	for rows.Next() {
		var z models.P3MBenefitRealization
		if err := rows.Scan(&z.ID, &z.BenefitID, &z.RecordedDate, &z.RealizedValue, &z.Notes, &z.RecordedBy, &z.CreatedAt); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, z)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateBenefitRealization handles POST /api/p3m/benefits/{id}/realizations.
func (h *P3MHandler) CreateBenefitRealization(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		RecordedDate  time.Time `json:"recorded_date"`
		RealizedValue float64   `json:"realized_value"`
		Notes         string    `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	var count int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_benefits WHERE id = ?`, id).Scan(&count); err != nil || count == 0 {
		respondNotFound(w, r, "p3m_benefit")
		return
	}
	if in.RecordedDate.IsZero() {
		respondValidationError(w, r, "recorded_date is required")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_benefit_realizations (benefit_id, recorded_date, realized_value, notes, recorded_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, in.RecordedDate, in.RealizedValue, in.Notes, user.ID, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	zid64, _ := res.LastInsertId()

	var z models.P3MBenefitRealization
	err = h.db.QueryRowContext(r.Context(), `SELECT id, benefit_id, recorded_date, realized_value, notes, recorded_by, created_at FROM p3m_benefit_realizations WHERE id = ?`, int(zid64)).
		Scan(&z.ID, &z.BenefitID, &z.RecordedDate, &z.RealizedValue, &z.Notes, &z.RecordedBy, &z.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, z)
}

// DeleteBenefitRealization handles DELETE /api/p3m/benefits/{id}/realizations/{rid}.
func (h *P3MHandler) DeleteBenefitRealization(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	benefitID, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	realizationID, ok := requireIDParam(w, r, "rid")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_benefit_realizations WHERE id = ? AND benefit_id = ?`, realizationID, benefitID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "p3m_benefit_realization")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateProjectBaseline handles POST /api/p3m/projects/{id}/baselines. The
// snapshot is a JSON capture of the project's key fields at this moment and is
// never mutated afterwards.
func (h *P3MHandler) CreateProjectBaseline(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Label string `json:"label"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Label == "" {
		in.Label = "Baseline"
	}

	p, err := h.getProjectRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	now := time.Now().UTC()
	snapshot := map[string]any{
		"title":                 p.Title,
		"description":           p.Description,
		"budget_approved":       p.BudgetApproved,
		"start_date":            nullTimePtr(p.StartDate),
		"target_completion_date": nullTimePtr(p.TargetCompletionDate),
		"progress_percent":      p.ProgressPercent,
		"captured_at":           now.Format(time.RFC3339),
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_baselines (project_id, label, snapshot, created_by, created_at)
		VALUES (?, ?, ?, ?, ?)`, id, in.Label, string(snapshotJSON), user.ID, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var b models.P3MBaseline
	var snapshotText string
	bid64, _ := res.LastInsertId()
	err = h.db.QueryRowContext(r.Context(), `SELECT id, project_id, label, snapshot, created_by, created_at FROM p3m_baselines WHERE id = ?`, int(bid64)).
		Scan(&b.ID, &b.ProjectID, &b.Label, &snapshotText, &b.CreatedBy, &b.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	b.Snapshot = map[string]any{}
	if err := json.Unmarshal([]byte(snapshotText), &b.Snapshot); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, b)
}

// ListProjectBaselines handles GET /api/p3m/projects/{id}/baselines.
func (h *P3MHandler) ListProjectBaselines(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var count int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, id).Scan(&count); err != nil || count == 0 {
		respondNotFound(w, r, "p3m_project")
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `SELECT id, project_id, label, snapshot, created_by, created_at FROM p3m_baselines WHERE project_id = ? ORDER BY id DESC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MBaseline, 0)
	for rows.Next() {
		var b models.P3MBaseline
		var snapshotText string
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Label, &snapshotText, &b.CreatedBy, &b.CreatedAt); err != nil {
			respondInternalError(w, r, err)
			return
		}
		b.Snapshot = map[string]any{}
		if err := json.Unmarshal([]byte(snapshotText), &b.Snapshot); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

const p3mTrancheColumns = `id, entity_type, entity_id, title, amount, expenditure_type, planned_date, status, created_at, updated_at`

func scanP3MTranche(row interface{ Scan(...any) error }) (models.P3MBudgetTranche, error) {
	var t models.P3MBudgetTranche
	err := row.Scan(&t.ID, &t.EntityType, &t.EntityID, &t.Title, &t.Amount, &t.ExpenditureType, &t.PlannedDate, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// p3mValidTrancheField validates the enum fields shared by tranche writes.
func p3mValidateTrancheFields(w http.ResponseWriter, r *http.Request, expenditureType, status string) bool {
	switch expenditureType {
	case "capex", "opex":
	default:
		respondValidationError(w, r, "expenditure_type must be capex or opex")
		return false
	}
	if status == "" {
		status = "proposed"
	}
	switch status {
	case "proposed", "approved", "released":
	default:
		respondValidationError(w, r, "status must be one of proposed, approved or released")
		return false
	}
	return true
}

// ListTranches handles GET /api/p3m/tranches?entity_type=&entity_id=.
func (h *P3MHandler) ListTranches(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	query := `SELECT ` + p3mTrancheColumns + ` FROM p3m_budget_tranches`
	var conds []string
	var args []any
	entityType := r.URL.Query().Get("entity_type")
	if entityType != "" {
		conds = append(conds, "entity_type = ?")
		args = append(args, entityType)
	}
	if entityID, has := parseQueryInt(r.URL.Query().Get("entity_id")); has && entityType != "" {
		if !h.p3mEntityExists(r.Context(), entityType, entityID) {
			respondNotFound(w, r, "p3m_entity")
			return
		}
		conds = append(conds, "entity_id = ?")
		args = append(args, entityID)
	}
	if len(conds) > 0 {
		query += ` WHERE ` + joinPlaceholders(conds)
	}
	query += ` ORDER BY planned_date ASC, id ASC`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MBudgetTranche, 0)
	for rows.Next() {
		t, err := scanP3MTranche(rows)
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

// CreateTranche handles POST /api/p3m/tranches.
func (h *P3MHandler) CreateTranche(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		EntityType      string    `json:"entity_type"`
		EntityID        int       `json:"entity_id"`
		Title           string    `json:"title"`
		Amount          float64   `json:"amount"`
		ExpenditureType string    `json:"expenditure_type"`
		PlannedDate     time.Time `json:"planned_date"`
		Status          string    `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	if !h.p3mEntityExists(r.Context(), in.EntityType, in.EntityID) {
		respondNotFound(w, r, "p3m_entity")
		return
	}
	if !p3mValidateTrancheFields(w, r, in.ExpenditureType, in.Status) {
		return
	}
	if in.PlannedDate.IsZero() {
		respondValidationError(w, r, "planned_date is required")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_budget_tranches (entity_type, entity_id, title, amount, expenditure_type, planned_date, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.EntityType, in.EntityID, in.Title, in.Amount, in.ExpenditureType, in.PlannedDate, in.Status, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()

	t, err := scanP3MTranche(h.db.QueryRowContext(r.Context(), `SELECT `+p3mTrancheColumns+` FROM p3m_budget_tranches WHERE id = ?`, int(id64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, t)
}

// UpdateTranche handles PUT /api/p3m/tranches/{id}. The body carries the full
// mutable state of the tranche.
func (h *P3MHandler) UpdateTranche(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		EntityType      string    `json:"entity_type"`
		EntityID        int       `json:"entity_id"`
		Title           string    `json:"title"`
		Amount          float64   `json:"amount"`
		ExpenditureType string    `json:"expenditure_type"`
		PlannedDate     time.Time `json:"planned_date"`
		Status          string    `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	if !h.p3mEntityExists(r.Context(), in.EntityType, in.EntityID) {
		respondNotFound(w, r, "p3m_entity")
		return
	}
	if !p3mValidateTrancheFields(w, r, in.ExpenditureType, in.Status) {
		return
	}
	if in.PlannedDate.IsZero() {
		respondValidationError(w, r, "planned_date is required")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_budget_tranches SET entity_type = ?, entity_id = ?, title = ?, amount = ?, expenditure_type = ?, planned_date = ?, status = ?, updated_at = ? WHERE id = ?`,
		in.EntityType, in.EntityID, in.Title, in.Amount, in.ExpenditureType, in.PlannedDate, in.Status, now, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "p3m_tranche")
		return
	}

	t, err := scanP3MTranche(h.db.QueryRowContext(r.Context(), `SELECT `+p3mTrancheColumns+` FROM p3m_budget_tranches WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, t)
}

// DeleteTranche handles DELETE /api/p3m/tranches/{id}.
func (h *P3MHandler) DeleteTranche(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_budget_tranches WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "p3m_tranche")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetForecast handles GET /api/p3m/forecast?from=YYYY-MM-DD&to=YYYY-MM-DD.
// Approved and released tranches are aggregated per calendar month; the range
// defaults to the next twelve months starting at the current one.
func (h *P3MHandler) GetForecast(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	toExclusive := from.AddDate(0, 12, 0)

	if s := r.URL.Query().Get("from"); s != "" {
		parsed, err := time.ParseInLocation("2006-01-02", s, time.UTC)
		if err != nil {
			respondBadRequest(w, r, "invalid date")
			return
		}
		from = parsed
	}
	if s := r.URL.Query().Get("to"); s != "" {
		parsed, err := time.ParseInLocation("2006-01-02", s, time.UTC)
		if err != nil {
			respondBadRequest(w, r, "invalid date")
			return
		}
		toExclusive = parsed.AddDate(0, 0, 1) // to is inclusive in the query string
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT title, amount, expenditure_type, planned_date FROM p3m_budget_tranches
		WHERE status IN ('approved', 'released') AND planned_date >= ? AND planned_date < ?`,
		from.Format("2006-01-02"), toExclusive.Format("2006-01-02"))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	type monthTotals struct {
		capex float64
		opex  float64
	}
	totals := make(map[string]*monthTotals)
	for rows.Next() {
		var title string
		var amount float64
		var expenditureType string
		var plannedDate time.Time
		if err := rows.Scan(&title, &amount, &expenditureType, &plannedDate); err != nil {
			respondInternalError(w, r, err)
			return
		}
		month := plannedDate.Format("2006-01")
		mt := totals[month]
		if mt == nil {
			mt = &monthTotals{}
			totals[month] = mt
		}
		switch expenditureType {
		case "capex":
			mt.capex += amount
		default:
			mt.opex += amount
		}
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	out := make([]models.P3MForecastPoint, 0, len(totals))
	for month, mt := range totals {
		out = append(out, models.P3MForecastPoint{Month: month, CapexTotal: mt.capex, OpexTotal: mt.opex})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Month < out[j].Month })
	respondJSONOK(w, out)
}
