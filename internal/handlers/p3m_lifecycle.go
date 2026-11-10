package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"windshift/internal/models"
)

// p3mStageGateTemplateColumns lists every p3m_stage_gate_templates column in
// scan order for scanP3MStageGateTemplate.
const p3mStageGateTemplateColumns = `id, name, from_stage_key, to_stage_key, mandatory, work_type, required_artifacts, description, enabled, created_at, updated_at`

var p3mStageKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// scanP3MStageGateTemplate reads one gate template row and parses its
// required_artifacts JSON into the typed checklist.
func scanP3MStageGateTemplate(row interface{ Scan(...any) error }) (models.P3MStageGateTemplate, error) {
	var t models.P3MStageGateTemplate
	var artifactsJSON string
	err := row.Scan(&t.ID, &t.Name, &t.FromStageKey, &t.ToStageKey, &t.Mandatory, &t.WorkType, &artifactsJSON, &t.Description, &t.Enabled, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return t, err
	}
	t.RequiredArtifacts = []models.P3MStageGateTemplateArtifact{}
	if artifactsJSON != "" && artifactsJSON != "null" {
		if err := json.Unmarshal([]byte(artifactsJSON), &t.RequiredArtifacts); err != nil {
			return t, fmt.Errorf("invalid required_artifacts JSON: %w", err)
		}
	}
	return t, nil
}

// loadP3MStageGateTemplate fetches one gate template by id.
func (h *P3MHandler) loadP3MStageGateTemplate(ctx context.Context, id int) (models.P3MStageGateTemplate, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+p3mStageGateTemplateColumns+` FROM p3m_stage_gate_templates WHERE id = ?`, id)
	return scanP3MStageGateTemplate(row)
}

// ListLifecycleConfig handles GET /api/p3m/config/lifecycle: the full stage
// list (including disabled stages), every gate template, and a per-work-type
// preview of the resulting path with its mandatory gates.
func (h *P3MHandler) ListLifecycleConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	stages, err := h.loadP3MLifecycleStages(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(ctx, `SELECT `+p3mStageGateTemplateColumns+` FROM p3m_stage_gate_templates ORDER BY id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	templates := make([]models.P3MStageGateTemplate, 0)
	for rows.Next() {
		t, err := scanP3MStageGateTemplate(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	previews := make([]models.P3MLifecyclePathPreview, 0, 6)
	workTypes := []string{"", models.P3MWorkTypeProgramme, models.P3MWorkTypeProject, models.P3MWorkTypeContinuousImprovement, models.P3MWorkTypeDigitalProduct, models.P3MWorkTypeOperational}
	for _, wt := range workTypes {
		var workType *string
		if wt != "" {
			workType = &wt
		}
		preview := models.P3MLifecyclePathPreview{WorkType: wt, StageKeys: []string{}, MandatoryGates: []string{}}
		for _, s := range stages {
			if s.Enabled {
				preview.StageKeys = append(preview.StageKeys, s.StageKey)
			}
		}
		gates, err := h.p3mMandatoryTemplatesForAll(ctx, workType)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		for _, g := range gates {
			from := "*"
			if g.FromStageKey != nil {
				from = *g.FromStageKey
			}
			preview.MandatoryGates = append(preview.MandatoryGates, from+"->"+g.ToStageKey)
		}
		previews = append(previews, preview)
	}

	respondJSONOK(w, models.P3MLifecycleConfig{Stages: stages, GateTemplates: templates, PathPreviews: previews})
}

// p3mMandatoryTemplatesForAll returns every enabled mandatory template that
// applies to workType (nil = the default path).
func (h *P3MHandler) p3mMandatoryTemplatesForAll(ctx context.Context, workType *string) ([]models.P3MStageGateTemplate, error) {
	query := `SELECT ` + p3mStageGateTemplateColumns + ` WHERE enabled = 1 AND mandatory = 1`
	args := []any{}
	if workType != nil {
		query += ` AND (work_type IS NULL OR work_type = ?)`
		args = append(args, *workType)
	} else {
		query += ` AND work_type IS NULL`
	}
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.P3MStageGateTemplate, 0)
	for rows.Next() {
		t, err := scanP3MStageGateTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateLifecycleStage handles POST /api/p3m/config/lifecycle/stages.
func (h *P3MHandler) CreateLifecycleStage(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		StageKey     string `json:"stage_key"`
		Name         string `json:"name"`
		Description  string `json:"description"`
		SortOrder    int    `json:"sort_order"`
		IsEntry      bool   `json:"is_entry"`
		IsExit       bool   `json:"is_exit"`
		GateRequired bool   `json:"gate_required"`
		Skippable    bool   `json:"skippable"`
		Enabled      *bool  `json:"enabled,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !p3mStageKeyPattern.MatchString(in.StageKey) {
		respondValidationError(w, r, "stage_key must be a lowercase slug (letters, digits, underscores)")
		return
	}
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return
	}
	var exists int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_lifecycle_stages WHERE stage_key = ?`, in.StageKey).Scan(&exists); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if exists > 0 {
		respondConflict(w, r, "stage_key already exists")
		return
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	// Invariant: after this insert at least one enabled entry stage must exist.
	if !in.IsEntry || !enabled {
		var entries int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_lifecycle_stages WHERE enabled = 1 AND is_entry = 1`).Scan(&entries); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if entries == 0 {
			respondConflict(w, r, "at least one enabled entry stage is required")
			return
		}
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_lifecycle_stages (stage_key, name, description, sort_order, is_entry, is_exit, gate_required, skippable, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.StageKey, in.Name, in.Description, in.SortOrder, boolToInt(in.IsEntry), boolToInt(in.IsExit), boolToInt(in.GateRequired), boolToInt(in.Skippable), boolToInt(enabled), now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	id64, _ := res.LastInsertId()
	stage := models.P3MLifecycleStage{ID: int(id64), StageKey: in.StageKey, Name: in.Name, Description: in.Description, SortOrder: in.SortOrder, IsEntry: in.IsEntry, IsExit: in.IsExit, GateRequired: in.GateRequired, Skippable: in.Skippable, Enabled: enabled, CreatedAt: now, UpdatedAt: now}
	h.p3mAudit(r, user, "p3m.lifecycle.stage.create", "p3m_lifecycle_stage", &stage.ID, stage.StageKey, map[string]any{
		"sort_order": in.SortOrder, "is_entry": in.IsEntry, "is_exit": in.IsExit, "enabled": enabled,
	})
	respondJSONCreated(w, stage)
}

// UpdateLifecycleStage handles PUT /api/p3m/config/lifecycle/stages/{key}.
// The key itself is immutable; everything else may change. Disabling a stage
// that still hosts projects or would leave no entry stage is refused.
func (h *P3MHandler) UpdateLifecycleStage(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	var in struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		SortOrder    int    `json:"sort_order"`
		IsEntry      bool   `json:"is_entry"`
		IsExit       bool   `json:"is_exit"`
		GateRequired bool   `json:"gate_required"`
		Skippable    bool   `json:"skippable"`
		Enabled      *bool  `json:"enabled,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	var cur p3mStageRow
	err := h.db.QueryRowContext(r.Context(), `SELECT id, stage_key, name, description, sort_order, is_entry, is_exit, gate_required, skippable, enabled FROM p3m_lifecycle_stages WHERE stage_key = ?`, key).
		Scan(&cur.ID, &cur.StageKey, &cur.Name, &cur.Description, &cur.SortOrder, &cur.IsEntry, &cur.IsExit, &cur.GateRequired, &cur.Skippable, &cur.Enabled)
	if err != nil {
		respondNotFound(w, r, "p3m_lifecycle_stage")
		return
	}

	enabled := cur.Enabled == 1
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if !enabled && cur.Enabled == 1 {
		var hosted int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_projects WHERE current_stage = ?`, key).Scan(&hosted); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if hosted > 0 {
			respondConflict(w, r, fmt.Sprintf("%d projects are still in stage %q; move them first", hosted, key))
			return
		}
	}
	// Invariant: after this update at least one enabled entry stage must remain.
	if !(enabled && in.IsEntry) {
		var others int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_lifecycle_stages WHERE id != ? AND enabled = 1 AND is_entry = 1`, cur.ID).Scan(&others); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if others == 0 {
			respondConflict(w, r, "at least one enabled entry stage is required")
			return
		}
	}

	now := time.Now().UTC()
	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_lifecycle_stages SET name = ?, description = ?, sort_order = ?, is_entry = ?, is_exit = ?, gate_required = ?, skippable = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		in.Name, in.Description, in.SortOrder, boolToInt(in.IsEntry), boolToInt(in.IsExit), boolToInt(in.GateRequired), boolToInt(in.Skippable), boolToInt(enabled), now, cur.ID); err != nil {
		respondInternalError(w, r, err)
		return
	}

	stage := models.P3MLifecycleStage{ID: cur.ID, StageKey: key, Name: in.Name, Description: in.Description, SortOrder: in.SortOrder, IsEntry: in.IsEntry, IsExit: in.IsExit, GateRequired: in.GateRequired, Skippable: in.Skippable, Enabled: enabled, UpdatedAt: now}
	h.p3mAudit(r, user, "p3m.lifecycle.stage.update", "p3m_lifecycle_stage", &stage.ID, stage.StageKey, map[string]any{
		"sort_order": in.SortOrder, "is_entry": in.IsEntry, "is_exit": in.IsExit, "enabled": enabled,
	})
	respondJSONOK(w, stage)
}

// DeleteLifecycleStage handles DELETE /api/p3m/config/lifecycle/stages/{key}.
// Refused while projects sit at the stage or an enabled gate template
// references it.
func (h *P3MHandler) DeleteLifecycleStage(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")

	var id int
	err := h.db.QueryRowContext(r.Context(), `SELECT id FROM p3m_lifecycle_stages WHERE stage_key = ?`, key).Scan(&id)
	if err != nil {
		respondNotFound(w, r, "p3m_lifecycle_stage")
		return
	}
	var hosted int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_projects WHERE current_stage = ?`, key).Scan(&hosted); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if hosted > 0 {
		respondConflict(w, r, fmt.Sprintf("%d projects are still in stage %q; move them first", hosted, key))
		return
	}
	var referenced int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_stage_gate_templates WHERE enabled = 1 AND (from_stage_key = ? OR to_stage_key = ?)`, key, key).Scan(&referenced); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if referenced > 0 {
		respondConflict(w, r, "stage is referenced by enabled gate templates; disable or re-point them first")
		return
	}

	if _, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_lifecycle_stages WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.lifecycle.stage.delete", "p3m_lifecycle_stage", &id, key, map[string]any{})
	respondJSONOK(w, map[string]bool{"deleted": true})
}

// p3mValidateTemplateStages checks that both template stage keys name existing
// lifecycle stages (from may be nil). It writes a 400 on failure.
func (h *P3MHandler) p3mValidateTemplateStages(ctx context.Context, w http.ResponseWriter, r *http.Request, fromKey *string, toKey string) bool {
	stages, err := h.loadP3MLifecycleStages(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return false
	}
	all := make([]models.P3MLifecycleStage, len(stages))
	copy(all, stages)
	if p3mStagePosition(all, toKey) < 0 {
		respondValidationError(w, r, "to_stage_key "+toKey+" is not a lifecycle stage")
		return false
	}
	if fromKey != nil && p3mStagePosition(all, *fromKey) < 0 {
		respondValidationError(w, r, "from_stage_key "+*fromKey+" is not a lifecycle stage")
		return false
	}
	return true
}

// CreateGateTemplate handles POST /api/p3m/config/lifecycle/gate-templates.
func (h *P3MHandler) CreateGateTemplate(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Name              string                               `json:"name"`
		FromStageKey      *string                              `json:"from_stage_key,omitempty"`
		ToStageKey        string                               `json:"to_stage_key"`
		Mandatory         bool                                 `json:"mandatory"`
		WorkType          *string                              `json:"work_type,omitempty"`
		RequiredArtifacts []models.P3MStageGateTemplateArtifact `json:"required_artifacts"`
		Description       string                               `json:"description"`
		Enabled           *bool                                `json:"enabled,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Name == "" || in.ToStageKey == "" {
		respondValidationError(w, r, "name and to_stage_key are required")
		return
	}
	if !h.p3mValidateTemplateStages(r.Context(), w, r, in.FromStageKey, in.ToStageKey) {
		return
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	artifactsJSON, err := json.Marshal(in.RequiredArtifacts)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_stage_gate_templates (name, from_stage_key, to_stage_key, mandatory, work_type, required_artifacts, description, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, nullStringPtr(in.FromStageKey), in.ToStageKey, boolToInt(in.Mandatory), nullStringPtr(in.WorkType), string(artifactsJSON), in.Description, boolToInt(enabled), now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	tpl, err := h.loadP3MStageGateTemplate(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.lifecycle.template.create", "p3m_stage_gate_template", &tpl.ID, tpl.Name, map[string]any{
		"from_stage_key": nullStringPtr(tpl.FromStageKey), "to_stage_key": tpl.ToStageKey, "mandatory": tpl.Mandatory, "work_type": nullStringPtr(tpl.WorkType),
	})
	respondJSONCreated(w, tpl)
}

// UpdateGateTemplate handles PUT /api/p3m/config/lifecycle/gate-templates/{id}.
func (h *P3MHandler) UpdateGateTemplate(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name              string                               `json:"name"`
		FromStageKey      *string                              `json:"from_stage_key,omitempty"`
		ToStageKey        string                               `json:"to_stage_key"`
		Mandatory         bool                                 `json:"mandatory"`
		WorkType          *string                              `json:"work_type,omitempty"`
		RequiredArtifacts []models.P3MStageGateTemplateArtifact `json:"required_artifacts"`
		Description       string                               `json:"description"`
		Enabled           *bool                                `json:"enabled,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Name == "" || in.ToStageKey == "" {
		respondValidationError(w, r, "name and to_stage_key are required")
		return
	}
	var curID int
	err := h.db.QueryRowContext(r.Context(), `SELECT id FROM p3m_stage_gate_templates WHERE id = ?`, id).Scan(&curID)
	if err != nil {
		respondNotFound(w, r, "p3m_stage_gate_template")
		return
	}
	if !h.p3mValidateTemplateStages(r.Context(), w, r, in.FromStageKey, in.ToStageKey) {
		return
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	artifactsJSON, err := json.Marshal(in.RequiredArtifacts)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	now := time.Now().UTC()
	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_stage_gate_templates SET name = ?, from_stage_key = ?, to_stage_key = ?, mandatory = ?, work_type = ?, required_artifacts = ?, description = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		in.Name, nullStringPtr(in.FromStageKey), in.ToStageKey, boolToInt(in.Mandatory), nullStringPtr(in.WorkType), string(artifactsJSON), in.Description, boolToInt(enabled), now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}

	tpl, err := h.loadP3MStageGateTemplate(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.lifecycle.template.update", "p3m_stage_gate_template", &tpl.ID, tpl.Name, map[string]any{
		"from_stage_key": nullStringPtr(tpl.FromStageKey), "to_stage_key": tpl.ToStageKey, "mandatory": tpl.Mandatory, "enabled": enabled,
	})
	respondJSONOK(w, tpl)
}

// DeleteGateTemplate handles DELETE /api/p3m/config/lifecycle/gate-templates/{id}.
// Reviews that referenced the template keep their own artifact checklists; the
// reference is nulled by the schema.
func (h *P3MHandler) DeleteGateTemplate(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var name string
	err := h.db.QueryRowContext(r.Context(), `SELECT name FROM p3m_stage_gate_templates WHERE id = ?`, id).Scan(&name)
	if err != nil {
		respondNotFound(w, r, "p3m_stage_gate_template")
		return
	}
	if _, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_stage_gate_templates WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.lifecycle.template.delete", "p3m_stage_gate_template", &id, name, map[string]any{})
	respondJSONOK(w, map[string]bool{"deleted": true})
}

// GetProjectGates handles GET /api/p3m/projects/{id}/gates: the project's
// current stage plus every enabled gate template's status for it.
func (h *P3MHandler) GetProjectGates(w http.ResponseWriter, r *http.Request) {
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

	stages, err := h.loadEnabledLifecycleStages(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, `SELECT `+p3mStageGateTemplateColumns+` FROM p3m_stage_gate_templates WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	templates := make([]models.P3MStageGateTemplate, 0)
	for rows.Next() {
		t, err := scanP3MStageGateTemplate(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	skipped, err := h.p3mSkippedStages(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	passedRows, err := h.db.QueryContext(ctx, `SELECT from_stage, to_stage FROM p3m_stage_gate_reviews WHERE project_id = ? AND passed = 1`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer passedRows.Close()

	passed := make(map[string][]string) // to_stage -> from stages
	for passedRows.Next() {
		var f, t string
		if err := passedRows.Scan(&f, &t); err != nil {
			respondInternalError(w, r, err)
			return
		}
		passed[t] = append(passed[t], f)
	}
	if err := passedRows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	openRows, err := h.db.QueryContext(ctx, `SELECT to_stage FROM p3m_stage_gate_reviews WHERE project_id = ? AND passed = 0`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer openRows.Close()

	openTargets := make(map[string]bool)
	for openRows.Next() {
		var t string
		if err := openRows.Scan(&t); err != nil {
			respondInternalError(w, r, err)
			return
		}
		openTargets[t] = true
	}
	if err := openRows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	gates := make([]models.P3MProjectGateStatus, 0, len(templates))
	for _, t := range templates {
		status := "not_started"
		switch {
		case skipped[t.ToStageKey]:
			status = "skipped"
		default:
			for _, f := range passed[t.ToStageKey] {
				if t.FromStageKey == nil || *t.FromStageKey == f {
					status = "passed"
					break
				}
			}
			if status == "not_started" && openTargets[t.ToStageKey] {
				status = "in_progress"
			}
		}
		gates = append(gates, models.P3MProjectGateStatus{Template: &t, Status: status})
	}

	// Order by the target stage's position in the enabled path.
	for i := range gates {
		for j := i + 1; j < len(gates); j++ {
			if p3mStagePosition(stages, gates[j].Template.ToStageKey) < p3mStagePosition(stages, gates[i].Template.ToStageKey) {
				gates[i], gates[j] = gates[j], gates[i]
			}
		}
	}

	respondJSONOK(w, models.P3MProjectGates{ProjectID: id, CurrentStage: project.CurrentStage, Gates: gates})
}

// RecordStageSkip handles POST /api/p3m/projects/{id}/stage-skips. The stage
// must be enabled and skippable; one skip per stage per project.
func (h *P3MHandler) RecordStageSkip(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		StageKey string `json:"stage_key"`
		Reason   string `json:"reason"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Reason == "" {
		respondValidationError(w, r, "reason is required")
		return
	}

	project, err := h.getProjectRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	var stage p3mStageRow
	err = h.db.QueryRowContext(r.Context(), `SELECT id, stage_key, name, description, sort_order, is_entry, is_exit, gate_required, skippable, enabled FROM p3m_lifecycle_stages WHERE stage_key = ?`, in.StageKey).
		Scan(&stage.ID, &stage.StageKey, &stage.Name, &stage.Description, &stage.SortOrder, &stage.IsEntry, &stage.IsExit, &stage.GateRequired, &stage.Skippable, &stage.Enabled)
	if err != nil {
		respondNotFound(w, r, "p3m_lifecycle_stage")
		return
	}
	if stage.Enabled != 1 || stage.Skippable != 1 {
		respondConflict(w, r, "stage "+in.StageKey+" is not an enabled skippable stage")
		return
	}
	var existing int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_project_stage_skips WHERE project_id = ? AND stage_key = ?`, id, in.StageKey).Scan(&existing); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if existing > 0 {
		respondConflict(w, r, "stage "+in.StageKey+" is already skipped for this project")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_project_stage_skips (project_id, stage_key, reason, decided_by, decided_at) VALUES (?, ?, ?, ?, ?)`, id, in.StageKey, in.Reason, user.ID, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	skip := models.P3MProjectStageSkip{ID: int(id64), ProjectID: id, StageKey: in.StageKey, Reason: in.Reason, DecidedBy: user.ID, DecidedAt: now}
	h.p3mAudit(r, user, "p3m.stage_skip.record", "p3m_project_stage_skip", &skip.ID, project.Title+" "+in.StageKey, map[string]any{"reason": in.Reason})
	respondJSONCreated(w, skip)
}

// ListStageSkips handles GET /api/p3m/projects/{id}/stage-skips.
func (h *P3MHandler) ListStageSkips(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id, project_id, stage_key, reason, decided_by, decided_at FROM p3m_project_stage_skips WHERE project_id = ? ORDER BY decided_at`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	skips := make([]models.P3MProjectStageSkip, 0)
	for rows.Next() {
		var s models.P3MProjectStageSkip
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.StageKey, &s.Reason, &s.DecidedBy, &s.DecidedAt); err != nil {
			respondInternalError(w, r, err)
			return
		}
		skips = append(skips, s)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, skips)
}
