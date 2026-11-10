package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"windshift/internal/models"
)

// Front Door intake pack (FD-001..005, FD-008, FD-010/011): the single entry
// point for new work into the P3M lifecycle. Intakes are captured once and a
// "proceed" decision creates/links a project pre-filled from the record, so
// downstream stages never re-enter the idea's details.

const p3mIntakeColumns = `id, title, source, description, problem_opportunity, strategic_alignment, intended_outcomes, indicative_cost, resource_requirements, risk_notes, change_impact_notes, business_group, work_type, complexity, status, decision, decided_by, decision_date, conditions, rationale, target_project_id, lifecycle_position, created_by, created_at, updated_at`

var p3mIntakeComplexities = map[string]bool{"low": true, "med": true, "high": true}
var p3mIntakeStatuses = map[string]bool{"captured": true, "assessing": true, "decided": true}
var p3mIntakeDecisions = map[string]bool{
	"proceed": true, "redirect": true, "defer": true, "decline": true, "return_info": true,
}

// p3mIntakeFieldKeys are the field keys addressable in front-door screen
// configuration (p3m_front_door_screens).
var p3mIntakeFieldKeys = map[string]bool{
	"title": true, "source": true, "description": true, "problem_opportunity": true,
	"strategic_alignment": true, "intended_outcomes": true, "indicative_cost": true,
	"resource_requirements": true, "risk_notes": true, "change_impact_notes": true,
	"business_group": true, "work_type": true,
}

func scanP3MIntake(row interface{ Scan(...any) error }) (models.P3MIntake, error) {
	var i models.P3MIntake
	err := row.Scan(&i.ID, &i.Title, &i.Source, &i.Description, &i.ProblemOpportunity, &i.StrategicAlignment,
		&i.IntendedOutcomes, &i.IndicativeCost, &i.ResourceRequirements, &i.RiskNotes, &i.ChangeImpactNotes,
		&i.BusinessGroup, &i.WorkType, &i.Complexity, &i.Status, &i.Decision, &i.DecidedBy, &i.DecisionDate,
		&i.Conditions, &i.Rationale, &i.TargetProjectID, &i.LifecyclePosition, &i.CreatedBy, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

// p3mIntakeMissingRequiredFields returns the screen-configured required fields
// (for the intake's complexity) that are not yet completed on the record.
func (h *P3MHandler) p3mIntakeMissingRequiredFields(i models.P3MIntake) []string {
	rows, err := h.db.Query(`SELECT field_key FROM p3m_front_door_screens WHERE complexity = ? AND required = 1 ORDER BY id`, i.Complexity)
	if err != nil {
		return nil
	}
	defer rows.Close()

	present := map[string]bool{
		"title":                 strings.TrimSpace(i.Title) != "",
		"source":                i.Source != "",
		"description":           strings.TrimSpace(i.Description) != "",
		"problem_opportunity":   strings.TrimSpace(i.ProblemOpportunity) != "",
		"strategic_alignment":   strings.TrimSpace(i.StrategicAlignment) != "",
		"intended_outcomes":     strings.TrimSpace(i.IntendedOutcomes) != "",
		"indicative_cost":       i.IndicativeCost != nil,
		"resource_requirements": strings.TrimSpace(i.ResourceRequirements) != "",
		"risk_notes":            strings.TrimSpace(i.RiskNotes) != "",
		"change_impact_notes":   strings.TrimSpace(i.ChangeImpactNotes) != "",
		"business_group":        strings.TrimSpace(i.BusinessGroup) != "",
		"work_type":             i.WorkType != nil && *i.WorkType != "",
	}

	var missing []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil
		}
		if !present[key] {
			missing = append(missing, key)
		}
	}
	return missing
}

// ListIntakes handles GET /api/p3m/front-door (pipeline board), optionally
// filtered by ?status=.
func (h *P3MHandler) ListIntakes(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	query := `SELECT ` + p3mIntakeColumns + ` FROM p3m_intakes`
	var args []any
	if status := r.URL.Query().Get("status"); status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC, id DESC`

	rows, err := h.db.Query(query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MIntake, 0)
	for rows.Next() {
		i, err := scanP3MIntake(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// GetIntake handles GET /api/p3m/front-door/{id}.
func (h *P3MHandler) GetIntake(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	row := h.db.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, id)
	i, err := scanP3MIntake(row)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_intake")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, i)
}

// CreateIntake handles POST /api/p3m/front-door. New records start captured
// (or explicitly assessing); the decision is recorded via the decision
// endpoint so rationale stays mandatory.
func (h *P3MHandler) CreateIntake(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Title                string   `json:"title"`
		Source               string   `json:"source"`
		Description          string   `json:"description"`
		ProblemOpportunity   string   `json:"problem_opportunity"`
		StrategicAlignment   string   `json:"strategic_alignment"`
		IntendedOutcomes     string   `json:"intended_outcomes"`
		IndicativeCost       *float64 `json:"indicative_cost,omitempty"`
		ResourceRequirements string   `json:"resource_requirements"`
		RiskNotes            string   `json:"risk_notes"`
		ChangeImpactNotes    string   `json:"change_impact_notes"`
		BusinessGroup        string   `json:"business_group"`
		WorkType             *string  `json:"work_type,omitempty"`
		Complexity           string   `json:"complexity"`
		Status               string   `json:"status"`
		LifecyclePosition    *string  `json:"lifecycle_position,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	if in.Source == "" {
		in.Source = "standard"
	}
	if in.Complexity == "" {
		in.Complexity = "med"
	}
	if !p3mIntakeComplexities[in.Complexity] {
		respondValidationError(w, r, "complexity must be one of: low, med, high")
		return
	}
	if in.Status == "" {
		in.Status = "captured"
	}
	if !p3mIntakeStatuses[in.Status] || in.Status == "decided" {
		respondValidationError(w, r, "status must be captured or assessing on create")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_intakes (title, source, description, problem_opportunity, strategic_alignment, intended_outcomes, indicative_cost, resource_requirements, risk_notes, change_impact_notes, business_group, work_type, complexity, status, lifecycle_position, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Title, in.Source, in.Description, in.ProblemOpportunity, in.StrategicAlignment, in.IntendedOutcomes,
		nullFloatPtr(in.IndicativeCost), in.ResourceRequirements, in.RiskNotes, in.ChangeImpactNotes,
		in.BusinessGroup, nullStringPtr(in.WorkType), in.Complexity, in.Status, nullStringPtr(in.LifecyclePosition),
		user.ID, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	i, err := scanP3MIntake(h.db.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.intake.create", "p3m_intake", &i.ID, i.Title, map[string]any{
		"source":     in.Source,
		"complexity": in.Complexity,
		"status":     in.Status,
		"work_type":  nullStringPtr(in.WorkType),
	})
	respondJSONCreated(w, i)
}

// UpdateIntake handles PUT /api/p3m/front-door/{id}. Pointer fields are
// partial updates. Status may move between captured and assessing; a decided
// intake reopens to assessing only when its decision was return_info (the
// return-for-information loop). Deciding is exclusively the decision endpoint.
func (h *P3MHandler) UpdateIntake(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	old, err := scanP3MIntake(h.db.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_intake")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Title                *string  `json:"title,omitempty"`
		Source               *string  `json:"source,omitempty"`
		Description          *string  `json:"description,omitempty"`
		ProblemOpportunity   *string  `json:"problem_opportunity,omitempty"`
		StrategicAlignment   *string  `json:"strategic_alignment,omitempty"`
		IntendedOutcomes     *string  `json:"intended_outcomes,omitempty"`
		IndicativeCost       *float64 `json:"indicative_cost"`
		ResourceRequirements *string  `json:"resource_requirements,omitempty"`
		RiskNotes            *string  `json:"risk_notes,omitempty"`
		ChangeImpactNotes    *string  `json:"change_impact_notes,omitempty"`
		BusinessGroup        *string  `json:"business_group,omitempty"`
		WorkType             *string  `json:"work_type"`
		Complexity           *string  `json:"complexity,omitempty"`
		Status               *string  `json:"status,omitempty"`
		LifecyclePosition    *string  `json:"lifecycle_position"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	next := old
	diff := map[string]any{}
	setStr := func(key string, target *string, value *string) {
		if value == nil {
			return
		}
		if *target != *value {
			diff[key] = map[string]string{"old": *target, "new": *value}
			*target = *value
		}
	}
	setStr("title", &next.Title, in.Title)
	setStr("source", &next.Source, in.Source)
	setStr("description", &next.Description, in.Description)
	setStr("problem_opportunity", &next.ProblemOpportunity, in.ProblemOpportunity)
	setStr("strategic_alignment", &next.StrategicAlignment, in.StrategicAlignment)
	setStr("intended_outcomes", &next.IntendedOutcomes, in.IntendedOutcomes)
	setStr("resource_requirements", &next.ResourceRequirements, in.ResourceRequirements)
	setStr("risk_notes", &next.RiskNotes, in.RiskNotes)
	setStr("change_impact_notes", &next.ChangeImpactNotes, in.ChangeImpactNotes)
	setStr("business_group", &next.BusinessGroup, in.BusinessGroup)
	if in.IndicativeCost != nil && (old.IndicativeCost == nil || *old.IndicativeCost != *in.IndicativeCost) {
		diff["indicative_cost"] = map[string]any{"old": old.IndicativeCost, "new": *in.IndicativeCost}
		next.IndicativeCost = in.IndicativeCost
	}
	if in.WorkType != nil && (old.WorkType == nil || *old.WorkType != *in.WorkType) {
		diff["work_type"] = map[string]any{"old": nullStringPtr(old.WorkType), "new": *in.WorkType}
		next.WorkType = in.WorkType
	}
	if in.LifecyclePosition != nil && (old.LifecyclePosition == nil || *old.LifecyclePosition != *in.LifecyclePosition) {
		diff["lifecycle_position"] = map[string]any{"old": nullStringPtr(old.LifecyclePosition), "new": *in.LifecyclePosition}
		next.LifecyclePosition = in.LifecyclePosition
	}
	if in.Complexity != nil && old.Complexity != *in.Complexity {
		if !p3mIntakeComplexities[*in.Complexity] {
			respondValidationError(w, r, "complexity must be one of: low, med, high")
			return
		}
		diff["complexity"] = map[string]string{"old": old.Complexity, "new": *in.Complexity}
		next.Complexity = *in.Complexity
	}
	if in.Status != nil && old.Status != *in.Status {
		switch {
		case !p3mIntakeStatuses[*in.Status] || *in.Status == "decided":
			respondValidationError(w, r, "status must be captured or assessing; use the decision endpoint to decide")
			return
		case old.Status == "decided" && !(old.Decision != nil && *old.Decision == "return_info"):
			respondConflict(w, r, "a decided intake can only reopen to assessing when its decision was return_info")
			return
		default:
			diff["status"] = map[string]string{"old": old.Status, "new": *in.Status}
			next.Status = *in.Status
		}
	}

	now := time.Now().UTC()
	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_intakes SET title = ?, source = ?, description = ?, problem_opportunity = ?, strategic_alignment = ?, intended_outcomes = ?, indicative_cost = ?, resource_requirements = ?, risk_notes = ?, change_impact_notes = ?, business_group = ?, work_type = ?, complexity = ?, status = ?, lifecycle_position = ?, updated_at = ? WHERE id = ?`,
		next.Title, next.Source, next.Description, next.ProblemOpportunity, next.StrategicAlignment, next.IntendedOutcomes,
		nullFloatPtr(next.IndicativeCost), next.ResourceRequirements, next.RiskNotes, next.ChangeImpactNotes,
		next.BusinessGroup, nullStringPtr(next.WorkType), next.Complexity, next.Status, nullStringPtr(next.LifecyclePosition), now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.intake.update", "p3m_intake", &next.ID, next.Title, diff)
	updated, err := scanP3MIntake(h.db.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, updated)
}

// DecideIntake handles POST /api/p3m/front-door/{id}/decision. The rationale is
// mandatory (FD-008). Before deciding, every screen-configured required field
// for the intake's complexity must be completed (FD-003/005). A "proceed"
// decision creates a project pre-filled from the intake at the lifecycle entry
// stage and records a passed front-door gate carrying the conditions, then
// advances the project to the next enabled stage (capture-once, FD-010).
func (h *P3MHandler) DecideIntake(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Decision        string `json:"decision"`
		Rationale       string `json:"rationale"`
		Conditions      string `json:"conditions"`
		PortfolioID     *int   `json:"portfolio_id,omitempty"`
		ProgrammeID     *int   `json:"programme_id,omitempty"`
		TargetProjectID *int   `json:"target_project_id,omitempty"` // link an existing project instead of creating one (FD-010)
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !p3mIntakeDecisions[in.Decision] {
		respondValidationError(w, r, "decision must be one of: proceed, redirect, defer, decline, return_info")
		return
	}
	if strings.TrimSpace(in.Rationale) == "" {
		respondValidationError(w, r, "rationale is required for every front door decision")
		return
	}

	old, err := scanP3MIntake(h.db.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_intake")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if old.Status == "decided" {
		respondConflict(w, r, "intake is already decided; reopen it via return_info to re-decide")
		return
	}

	if missing := h.p3mIntakeMissingRequiredFields(old); len(missing) > 0 {
		respondValidationError(w, r, fmt.Sprintf("missing required assessment fields for %s complexity: %s", old.Complexity, strings.Join(missing, ", ")))
		return
	}

	now := time.Now().UTC()
	var targetProjectID *int
	if in.Decision == "proceed" {
		switch {
		case old.TargetProjectID != nil:
			targetProjectID = old.TargetProjectID // re-decision after return_info: the project already exists.
		case in.TargetProjectID != nil:
			if _, err := h.getProjectRow(r.Context(), *in.TargetProjectID); err != nil {
				respondNotFound(w, r, "target project not found")
				return
			}
			targetProjectID = in.TargetProjectID // link an existing project (FD-010).
		default:
			created := h.p3mIntakeCreateProject(r, w, user, old, in.PortfolioID, in.ProgrammeID, in.Conditions)
			if created == 0 {
				return // handler wrote its own error response
			}
			targetProjectID = &created
		}
	}

	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_intakes SET decision = ?, decided_by = ?, decision_date = ?, conditions = ?, rationale = ?, status = 'decided', target_project_id = ?, updated_at = ? WHERE id = ?`,
		in.Decision, user.ID, now, in.Conditions, in.Rationale, nullInt(targetProjectID), now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}

	i, err := scanP3MIntake(h.db.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAudit(r, user, "p3m.intake.decide", "p3m_intake", &i.ID, i.Title, map[string]any{
		"decision":          in.Decision,
		"conditions":        in.Conditions,
		"target_project_id": nullInt(targetProjectID),
	})
	respondJSONOK(w, i)
}

// p3mIntakeCreateProject creates the project for a proceed decision: pre-filled
// from the intake at the lifecycle entry stage, with a passed front-door gate
// review carrying the decision conditions as its comments. Returns 0 when an
// error response was already written (or no project could be created).
func (h *P3MHandler) p3mIntakeCreateProject(r *http.Request, w http.ResponseWriter, user *models.User, intake models.P3MIntake, fid, pid *int, conditions string) int {
	ctx := r.Context()
	fid2, pid2, ok := h.resolveProjectParents(ctx, w, r, fid, pid)
	if !ok {
		return 0
	}

	stages, err := h.loadEnabledLifecycleStages(ctx)
	if err != nil || len(stages) == 0 {
		respondInternalError(w, r, fmt.Errorf("no enabled lifecycle stages: %w", err))
		return 0
	}
	entryPos := -1
	for i, s := range stages {
		if s.IsEntry {
			entryPos = i
			break
		}
	}
	if entryPos < 0 {
		entryPos = 0
	}

	now := time.Now().UTC()
	p := models.P3MProject{Title: intake.Title, Description: intake.Description, WorkType: intake.WorkType, CurrentStage: stages[entryPos].StageKey}
	p3mApplyComputed(&p)
	res, err := h.db.ExecContext(ctx, `
		INSERT INTO p3m_projects (portfolio_id, programme_id, title, description, manager_id, work_type, current_stage, rag_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?)`,
		nullInt(fid2), nullInt(pid2), p.Title, p.Description, nullStringPtr(p.WorkType), p.CurrentStage, p.RagStatus, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return 0
	}
	projectID64, _ := res.LastInsertId()
	projectID := int(projectID64)

	// Record the front-door gate as passed (the intake record is the
	// classification/assessment artifact - capture-once) and advance to the
	// next enabled stage when one exists.
	if entryPos+1 < len(stages) {
		toStage := stages[entryPos+1]
		comments := "Front Door decision: proceed"
		if strings.TrimSpace(conditions) != "" {
			comments += "\nConditions: " + conditions
		}
		var templateID *int
		if err := h.db.QueryRowContext(ctx, `
			SELECT id FROM p3m_stage_gate_templates
			WHERE from_stage_key = ? AND to_stage_key = ? AND enabled = 1
			  AND (work_type IS NULL OR work_type = ?)
			ORDER BY mandatory DESC, id LIMIT 1`,
			stages[entryPos].StageKey, toStage.StageKey, nullStringPtr(intake.WorkType)).Scan(&templateID); err == nil {
			// templateID may legitimately be NULL; keep it.
		} else if !isNotFound(err) {
			templateID = nil // no matching template: ad-hoc gate
		}
		if _, err := h.db.ExecContext(ctx, `
			INSERT INTO p3m_stage_gate_reviews (project_id, from_stage, to_stage, template_id, passed, reviewer_id, review_date, comments, created_at)
			VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?)`,
			projectID, stages[entryPos].StageKey, toStage.StageKey, nullInt(templateID), user.ID, now, comments, now); err != nil {
			respondInternalError(w, r, err)
			return 0
		}
		if _, err := h.db.ExecContext(ctx, `UPDATE p3m_projects SET current_stage = ?, updated_at = ? WHERE id = ?`, toStage.StageKey, now, projectID); err != nil {
			respondInternalError(w, r, err)
			return 0
		}
	}

	h.p3mAudit(r, user, "p3m.project.create", "p3m_project", &projectID, intake.Title, map[string]any{
		"source":            "front_door_intake",
		"intake_id":         intake.ID,
		"portfolio_id":      nullInt(fid2),
		"programme_id":      nullInt(pid2),
		"current_stage":     stages[entryPos].StageKey,
		"work_type":         nullStringPtr(intake.WorkType),
	})
	return projectID
}

// GetFrontDoorScreens handles GET /api/p3m/front-door/screens: the per-complexity
// required-field configuration (FD-003/005).
func (h *P3MHandler) GetFrontDoorScreens(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.Query(`SELECT id, complexity, field_key, required FROM p3m_front_door_screens ORDER BY CASE complexity WHEN 'low' THEN 0 WHEN 'med' THEN 1 ELSE 2 END, id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MFrontDoorScreen, 0)
	for rows.Next() {
		var s models.P3MFrontDoorScreen
		if err := rows.Scan(&s.ID, &s.Complexity, &s.FieldKey, &s.Required); err != nil {
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

// UpdateFrontDoorScreens handles PUT /api/p3m/front-door/screens: full
// replacement of the per-complexity required-field configuration.
func (h *P3MHandler) UpdateFrontDoorScreens(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Screens []struct {
			Complexity string `json:"complexity"`
			FieldKey   string `json:"field_key"`
			Required   bool   `json:"required"`
		} `json:"screens"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	seen := map[string]bool{}
	for _, s := range in.Screens {
		if !p3mIntakeComplexities[s.Complexity] {
			respondValidationError(w, r, "complexity must be one of: low, med, high")
			return
		}
		if !p3mIntakeFieldKeys[s.FieldKey] {
			respondValidationError(w, r, fmt.Sprintf("unknown field_key %q", s.FieldKey))
			return
		}
		key := s.Complexity + "/" + s.FieldKey
		if seen[key] {
			respondValidationError(w, r, "duplicate screen entry for "+key)
			return
		}
		seen[key] = true
	}

	tx, err := h.db.Begin()
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM p3m_front_door_screens`); err != nil {
		respondInternalError(w, r, err)
		return
	}
	for _, s := range in.Screens {
		required := 0
		if s.Required {
			required = 1
		}
		if _, err := tx.Exec(`INSERT INTO p3m_front_door_screens (complexity, field_key, required) VALUES (?, ?, ?)`, s.Complexity, s.FieldKey, required); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	h.p3mAudit(r, user, "p3m.front_door_screens.update", "p3m_front_door_screen", nil, fmt.Sprintf("%d screen rules", len(in.Screens)), map[string]any{
		"screen_count": len(in.Screens),
	})
	screens := make([]models.P3MFrontDoorScreen, 0)
	for _, s := range in.Screens {
		screens = append(screens, models.P3MFrontDoorScreen{Complexity: s.Complexity, FieldKey: s.FieldKey, Required: s.Required})
	}
	respondJSONOK(w, screens)
}
