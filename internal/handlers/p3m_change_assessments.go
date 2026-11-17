package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"windshift/internal/models"
)

const p3mChangeAssessmentColumns = `id, entity_type, entity_id, impacted_groups, impact_by_characteristic, overall_rating, support_level, approach, deliverables, created_by, created_at, updated_at`

func scanP3MChangeAssessment(row interface{ Scan(...any) error }) (models.P3MChangeAssessment, error) {
	var a models.P3MChangeAssessment
	var impactedGroups, impactByCharacteristic, approach, deliverables sql.NullString
	var createdBy sql.NullInt64
	err := row.Scan(&a.ID, &a.EntityType, &a.EntityID, &impactedGroups, &impactByCharacteristic,
		&a.OverallRating, &a.SupportLevel, &approach, &deliverables, &createdBy, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return a, err
	}
	if impactedGroups.Valid {
		a.ImpactedGroups = &impactedGroups.String
	}
	if impactByCharacteristic.Valid {
		a.ImpactByCharacteristic = &impactByCharacteristic.String
	}
	if approach.Valid {
		a.Approach = &approach.String
	}
	if deliverables.Valid {
		a.Deliverables = &deliverables.String
	}
	if createdBy.Valid {
		v := int(createdBy.Int64)
		a.CreatedBy = &v
	}
	return a, nil
}

const p3mReadinessAssessmentColumns = `id, entity_type, entity_id, questions, answers, assessed_at, outcome, reassessment_of, created_by, created_at`

func scanP3MReadinessAssessment(row interface{ Scan(...any) error }) (models.P3MReadinessAssessment, error) {
	var a models.P3MReadinessAssessment
	var answers sql.NullString
	var reassessmentOf sql.NullInt64
	var createdBy sql.NullInt64
	err := row.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.Questions, &answers, &a.AssessedAt,
		&a.Outcome, &reassessmentOf, &createdBy, &a.CreatedAt)
	if err != nil {
		return a, err
	}
	if answers.Valid {
		a.Answers = &answers.String
	}
	if reassessmentOf.Valid {
		v := int(reassessmentOf.Int64)
		a.ReassessmentOf = &v
	}
	if createdBy.Valid {
		v := int(createdBy.Int64)
		a.CreatedBy = &v
	}
	return a, nil
}

// validJSONArray reports whether s (when non-nil and non-empty) parses as a
// JSON array. Empty/nil values pass: they mean "not provided".
func validJSONArray(s *string) bool {
	if s == nil || *s == "" {
		return true
	}
	var arr []json.RawMessage
	return json.Unmarshal([]byte(*s), &arr) == nil
}

// p3mChangeAssessmentInput is the create/update payload for a change impact
// assessment (CHG-002/003). JSON fields arrive as raw strings and are stored
// verbatim after array validation.
type p3mChangeAssessmentInput struct {
	ImpactedGroups         *string `json:"impacted_groups"`
	ImpactByCharacteristic *string `json:"impact_by_characteristic"`
	OverallRating          int     `json:"overall_rating"`
	SupportLevel           string  `json:"support_level"`
	Approach               *string `json:"approach"`
	Deliverables           *string `json:"deliverables"`
}

func (h *P3MHandler) validateP3MChangeAssessmentInput(w http.ResponseWriter, r *http.Request, in *p3mChangeAssessmentInput) bool {
	if !validJSONArray(in.ImpactedGroups) || !validJSONArray(in.Deliverables) {
		respondValidationError(w, r, "impacted_groups and deliverables must be JSON arrays")
		return false
	}
	if in.OverallRating < 1 || in.OverallRating > 5 {
		respondValidationError(w, r, "overall_rating must be between 1 and 5")
		return false
	}
	switch in.SupportLevel {
	case "none", "light", "full":
	default:
		respondValidationError(w, r, "support_level must be none, light or full")
		return false
	}
	if in.SupportLevel == "" {
		in.SupportLevel = "none"
	}
	return true
}

// ListChangeAssessments handles GET /api/p3m/projects/{id}/change-assessments.
// The schema allows one active assessment per entity, so the list holds at
// most one row; clients treat an empty array as "not yet assessed".
func (h *P3MHandler) ListChangeAssessments(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+p3mChangeAssessmentColumns+` FROM p3m_change_assessments WHERE entity_type = 'project' AND entity_id = ? ORDER BY id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MChangeAssessment, 0)
	for rows.Next() {
		a, err := scanP3MChangeAssessment(rows)
		if err != nil {
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

// CreateChangeAssessment handles POST /api/p3m/projects/{id}/change-assessments.
// Because the assessment is unique per entity this is an upsert: a second
// submission replaces the previous one (the impact picture is a snapshot of
// the current change, not an append-only log).
func (h *P3MHandler) CreateChangeAssessment(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in p3mChangeAssessmentInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.validateP3MChangeAssessmentInput(w, r, &in) {
		return
	}

	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_change_assessments (entity_type, entity_id, impacted_groups, impact_by_characteristic, overall_rating, support_level, approach, deliverables, created_by)
		VALUES ('project', ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(entity_type, entity_id) DO UPDATE SET
			impacted_groups = excluded.impacted_groups,
			impact_by_characteristic = excluded.impact_by_characteristic,
			overall_rating = excluded.overall_rating,
			support_level = excluded.support_level,
			approach = excluded.approach,
			deliverables = excluded.deliverables,
			created_by = excluded.created_by`, id, in.ImpactedGroups, in.ImpactByCharacteristic, in.OverallRating, in.SupportLevel, in.Approach, in.Deliverables, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rowID, _ := res.LastInsertId()
	a, err := scanP3MChangeAssessment(h.db.QueryRowContext(r.Context(), `SELECT `+p3mChangeAssessmentColumns+` FROM p3m_change_assessments WHERE entity_type = 'project' AND entity_id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_change_assessment_saved", "p3m_change_assessment", &a.ID, "")
	_ = rowID
	respondJSONCreated(w, a)
}

// UpdateChangeAssessment handles PUT /api/p3m/change-assessments/{id}.
func (h *P3MHandler) UpdateChangeAssessment(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in p3mChangeAssessmentInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.validateP3MChangeAssessmentInput(w, r, &in) {
		return
	}

	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_change_assessments SET impacted_groups = ?, impact_by_characteristic = ?, overall_rating = ?, support_level = ?, approach = ?, deliverables = ? WHERE id = ?`,
		in.ImpactedGroups, in.ImpactByCharacteristic, in.OverallRating, in.SupportLevel, in.Approach, in.Deliverables, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_change_assessment")
		return
	}
	a, err := scanP3MChangeAssessment(h.db.QueryRowContext(r.Context(), `SELECT `+p3mChangeAssessmentColumns+` FROM p3m_change_assessments WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_change_assessment_updated", "p3m_change_assessment", &id, "")
	respondJSONOK(w, a)
}

// p3mReadinessAssessmentInput is the create payload for a readiness
// assessment (CHG-006). Questions is the checklist JSON; Answers may be
// omitted on creation and filled in via PUT.
type p3mReadinessAssessmentInput struct {
	Questions      string  `json:"questions"`
	Answers        *string `json:"answers"`
	Outcome        string  `json:"outcome"`
	ReassessmentOf *int    `json:"reassessment_of"`
}

func (h *P3MHandler) validateP3MReadinessAssessmentInput(w http.ResponseWriter, r *http.Request, in p3mReadinessAssessmentInput) bool {
	var questions []map[string]any
	if err := json.Unmarshal([]byte(in.Questions), &questions); err != nil || len(questions) == 0 {
		respondValidationError(w, r, "questions must be a non-empty JSON array of {id, text, required}")
		return false
	}
	switch in.Outcome {
	case "go", "no_go", "reassess":
	default:
		respondValidationError(w, r, "outcome must be go, no_go or reassess")
		return false
	}
	if !validJSONArray(in.Answers) {
		respondValidationError(w, r, "answers must be a JSON array of {question_id, answer, gap_action}")
		return false
	}
	return true
}

// ListReadinessAssessments handles GET /api/p3m/projects/{id}/readiness-assessments.
// Returns the full history newest-first; the first row is the gate for entry
// to implement_deliver.
func (h *P3MHandler) ListReadinessAssessments(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+p3mReadinessAssessmentColumns+` FROM p3m_readiness_assessments WHERE entity_type = 'project' AND entity_id = ? ORDER BY assessed_at DESC, id DESC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MReadinessAssessment, 0)
	for rows.Next() {
		a, err := scanP3MReadinessAssessment(rows)
		if err != nil {
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

// CreateReadinessAssessment handles POST /api/p3m/projects/{id}/readiness-assessments.
// When reassessment_of is omitted it links to the previous latest assessment,
// keeping the go/no_go/reassess chain intact for audit.
func (h *P3MHandler) CreateReadinessAssessment(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in p3mReadinessAssessmentInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.validateP3MReadinessAssessmentInput(w, r, in) {
		return
	}

	ctx := r.Context()
	reassessmentOf := in.ReassessmentOf
	if reassessmentOf == nil {
		var prevID sql.NullInt64
		err := h.db.QueryRowContext(ctx, `SELECT id FROM p3m_readiness_assessments WHERE entity_type = 'project' AND entity_id = ? ORDER BY assessed_at DESC, id DESC LIMIT 1`, id).Scan(&prevID)
		if err == nil && prevID.Valid {
			v := int(prevID.Int64)
			reassessmentOf = &v
		} else if !isNotFound(err) {
			respondInternalError(w, r, err)
			return
		}
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(ctx, `INSERT INTO p3m_readiness_assessments (entity_type, entity_id, questions, answers, assessed_at, outcome, reassessment_of, created_by) VALUES ('project', ?, ?, ?, ?, ?, ?, ?)`,
		id, in.Questions, in.Answers, now, in.Outcome, reassessmentOf, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rowID, _ := res.LastInsertId()
	a, err := scanP3MReadinessAssessment(h.db.QueryRowContext(ctx, `SELECT `+p3mReadinessAssessmentColumns+` FROM p3m_readiness_assessments WHERE id = ?`, rowID))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_readiness_assessment_created", "p3m_readiness_assessment", &a.ID, in.Outcome)
	respondJSONCreated(w, a)
}

// UpdateReadinessAssessment handles PUT /api/p3m/readiness-assessments/{id}.
// Used to record answers and move the outcome (e.g. reassess -> go once gap
// actions close).
func (h *P3MHandler) UpdateReadinessAssessment(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Answers *string `json:"answers"`
		Outcome string  `json:"outcome"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	switch in.Outcome {
	case "go", "no_go", "reassess":
	default:
		respondValidationError(w, r, "outcome must be go, no_go or reassess")
		return
	}
	if !validJSONArray(in.Answers) {
		respondValidationError(w, r, "answers must be a JSON array of {question_id, answer, gap_action}")
		return
	}

	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_readiness_assessments SET answers = ?, outcome = ? WHERE id = ?`, in.Answers, in.Outcome, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_readiness_assessment")
		return
	}
	a, err := scanP3MReadinessAssessment(h.db.QueryRowContext(r.Context(), `SELECT `+p3mReadinessAssessmentColumns+` FROM p3m_readiness_assessments WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_readiness_assessment_updated", "p3m_readiness_assessment", &id, in.Outcome)
	respondJSONOK(w, a)
}
