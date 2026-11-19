package aitools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"windshift/internal/auth"
	"windshift/internal/database"
	"windshift/internal/models"
)

// This file adds MCP/AI tools for the P3M Front Door intake pack and the
// portfolio/programme/project creation endpoints, completing end-to-end P3M
// coverage: capture an idea at the front door, assess it (required fields per
// complexity), decide with a mandatory rationale (proceed creates the project
// pre-filled from the intake and records a passed front-door gate), then
// govern the resulting work through the existing stage-gate/RAID/budget tools.
// Access mirrors the HTTP surface: the global "p3m.view" permission gates
// every tool; writes additionally require the p3m.write token scope.

const p3mIntakeColumns = `id, title, source, description, problem_opportunity, strategic_alignment, intended_outcomes, indicative_cost, resource_requirements, risk_notes, change_impact_notes, business_group, work_type, complexity, status, decision, decided_by, decision_date, conditions, rationale, target_project_id, lifecycle_position, created_by, created_at, updated_at`

var aiP3MIntakeComplexities = map[string]bool{"low": true, "med": true, "high": true}
var aiP3MIntakeStatuses = map[string]bool{"captured": true, "assessing": true, "decided": true}
var aiP3MIntakeDecisions = map[string]bool{
	"proceed": true, "redirect": true, "defer": true, "decline": true, "return_info": true,
}

func aiScanP3MIntake(row interface{ Scan(...any) error }) (models.P3MIntake, error) {
	var i models.P3MIntake
	err := row.Scan(&i.ID, &i.Title, &i.Source, &i.Description, &i.ProblemOpportunity, &i.StrategicAlignment,
		&i.IntendedOutcomes, &i.IndicativeCost, &i.ResourceRequirements, &i.RiskNotes, &i.ChangeImpactNotes,
		&i.BusinessGroup, &i.WorkType, &i.Complexity, &i.Status, &i.Decision, &i.DecidedBy, &i.DecisionDate,
		&i.Conditions, &i.Rationale, &i.TargetProjectID, &i.LifecyclePosition, &i.CreatedBy, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

func aiNullStringPtr(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func aiNullFloatPtr(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// aiP3MIntakeMissingRequiredFields returns the screen-configured required
// fields (for the intake's complexity) that are not yet completed on the
// record. Mirrors handlers.p3mIntakeMissingRequiredFields.
func aiP3MIntakeMissingRequiredFields(db database.Database, i models.P3MIntake) []string {
	rows, err := db.Query(`SELECT field_key FROM p3m_front_door_screens WHERE complexity = ? AND required = TRUE ORDER BY id`, i.Complexity)
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

// aiLoadEnabledLifecycleStages returns the enabled lifecycle stages in path
// order. Mirrors handlers.loadEnabledLifecycleStages.
func aiLoadEnabledLifecycleStages(ctx context.Context, db database.Database) ([]models.P3MLifecycleStage, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, stage_key, name, description, sort_order, is_entry, is_exit, gate_required, skippable, enabled FROM p3m_lifecycle_stages ORDER BY sort_order`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := make([]models.P3MLifecycleStage, 0)
	for rows.Next() {
		var s models.P3MLifecycleStage
		if err := rows.Scan(&s.ID, &s.StageKey, &s.Name, &s.Description, &s.SortOrder, &s.IsEntry, &s.IsExit, &s.GateRequired, &s.Skippable, &s.Enabled); err != nil {
			return nil, err
		}
		all = append(all, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]models.P3MLifecycleStage, 0, len(all))
	for _, s := range all {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out, nil
}

// aiResolveP3MParents resolves the portfolio/programme parents for a new
// project. Mirrors handlers.resolveProjectParents (error instead of HTTP).
func aiResolveP3MParents(ctx context.Context, db database.Database, fid, pid *int) (*int, *int, error) {
	if pid != nil {
		var pfID int
		err := db.QueryRowContext(ctx, `SELECT portfolio_id FROM p3m_programmes WHERE id = ?`, *pid).Scan(&pfID)
		if err != nil {
			return nil, nil, fmt.Errorf("programme %d not found", *pid)
		}
		if fid != nil && *fid != pfID {
			return nil, nil, fmt.Errorf("portfolio_id does not match the programme's portfolio")
		}
		return &pfID, pid, nil
	}
	if fid == nil {
		return nil, nil, fmt.Errorf("either portfolio_id or programme_id is required")
	}
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM p3m_portfolios WHERE id = ?)`, *fid).Scan(&exists)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, fmt.Errorf("portfolio %d not found", *fid)
	}
	return fid, nil, nil
}

// aiP3MIntakeCreateProject creates the project for a proceed decision:
// pre-filled from the intake at the lifecycle entry stage, with a passed
// front-door gate review carrying the decision conditions as its comments.
// Mirrors handlers.p3mIntakeCreateProject (error instead of HTTP).
func aiP3MIntakeCreateProject(ctx context.Context, db database.Database, userID int, intake models.P3MIntake, fid, pid *int, conditions string) (int, error) {
	fid2, pid2, err := aiResolveP3MParents(ctx, db, fid, pid)
	if err != nil {
		return 0, err
	}

	stages, err := aiLoadEnabledLifecycleStages(ctx, db)
	if err != nil || len(stages) == 0 {
		return 0, fmt.Errorf("no enabled lifecycle stages: %w", err)
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
	aiP3MApplyComputed(&p)
	res, err := db.ExecContext(ctx, `
		INSERT INTO p3m_projects (portfolio_id, programme_id, title, description, manager_id, work_type, current_stage, rag_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?)`,
		nullIntPtr(fid2), nullIntPtr(pid2), p.Title, p.Description, aiNullStringPtr(p.WorkType), p.CurrentStage, p.RagStatus, now, now)
	if err != nil {
		return 0, err
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
		if err := db.QueryRowContext(ctx, `
			SELECT id FROM p3m_stage_gate_templates
			WHERE from_stage_key = ? AND to_stage_key = ? AND enabled = TRUE
			  AND (work_type IS NULL OR work_type = ?)
			ORDER BY mandatory DESC, id LIMIT 1`,
			stages[entryPos].StageKey, toStage.StageKey, aiNullStringPtr(intake.WorkType)).Scan(&templateID); err == nil {
			// templateID may legitimately be NULL; keep it.
		} else if err != sql.ErrNoRows {
			return 0, fmt.Errorf("no matching gate template: %w", err)
		} else {
			templateID = nil // no matching template: ad-hoc gate
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO p3m_stage_gate_reviews (project_id, from_stage, to_stage, template_id, passed, reviewer_id, review_date, comments, created_at)
			VALUES (?, ?, ?, ?, TRUE, ?, ?, ?, ?)`,
			projectID, stages[entryPos].StageKey, toStage.StageKey, nullIntPtr(templateID), userID, now, comments, now); err != nil {
			return 0, err
		}
		if _, err := db.ExecContext(ctx, `UPDATE p3m_projects SET current_stage = ?, updated_at = ? WHERE id = ?`, toStage.StageKey, now, projectID); err != nil {
			return 0, err
		}
	}

	return projectID, nil
}

// aiP3MValidRag reports whether rag is one of the three P3M RAG states.
func aiP3MValidRag(rag string) bool {
	return rag == models.P3MRagGreen || rag == models.P3MRagAmber || rag == models.P3MRagRed
}

type listP3MIntakesArgs struct {
	Status   string `json:"status,omitempty" jsonschema:"Filter by pipeline status: captured, assessing or decided."`
	Decision string `json:"decision,omitempty" jsonschema:"Filter by recorded decision: proceed, redirect, defer, decline or return_info (the permanent decision record)."`
}

type getP3MIntakeArgs struct {
	ID int `json:"id" jsonschema:"The intake's numeric ID."`
}

type createP3MIntakeArgs struct {
	Title                string   `json:"title" jsonschema:"Short name of the idea or change request."`
	Source               string   `json:"source,omitempty" jsonschema:"standard (default) or exceptional_path for retrospective registration."`
	Description          string   `json:"description,omitempty"`
	ProblemOpportunity   string   `json:"problem_opportunity,omitempty"`
	StrategicAlignment   string   `json:"strategic_alignment,omitempty"`
	IntendedOutcomes     string   `json:"intended_outcomes,omitempty"`
	IndicativeCost       *float64 `json:"indicative_cost,omitempty" jsonschema:"Rough cost estimate; required before deciding when the complexity screen demands it."`
	ResourceRequirements string   `json:"resource_requirements,omitempty"`
	RiskNotes            string   `json:"risk_notes,omitempty"`
	ChangeImpactNotes    string   `json:"change_impact_notes,omitempty"`
	BusinessGroup        string   `json:"business_group,omitempty"`
	WorkType             *string  `json:"work_type,omitempty" jsonschema:"programme, project, continuous_improvement, digital_product or operational."`
	Complexity           string   `json:"complexity,omitempty" jsonschema:"low (default med), med or high; selects the required assessment fields."`
	Status               string   `json:"status,omitempty" jsonschema:"captured (default) or assessing."`
	LifecyclePosition    *string  `json:"lifecycle_position,omitempty" jsonschema:"For exceptional_path intakes: where in the lifecycle the work already sits."`
}

type updateP3MIntakeArgs struct {
	ID                   int      `json:"id" jsonschema:"The intake's numeric ID."`
	Title                *string  `json:"title,omitempty"`
	Source               *string  `json:"source,omitempty"`
	Description          *string  `json:"description,omitempty"`
	ProblemOpportunity   *string  `json:"problem_opportunity,omitempty"`
	StrategicAlignment   *string  `json:"strategic_alignment,omitempty"`
	IntendedOutcomes      *string  `json:"intended_outcomes,omitempty"`
	IndicativeCost       *float64 `json:"indicative_cost,omitempty"`
	ResourceRequirements *string  `json:"resource_requirements,omitempty"`
	RiskNotes            *string  `json:"risk_notes,omitempty"`
	ChangeImpactNotes    *string  `json:"change_impact_notes,omitempty"`
	BusinessGroup        *string  `json:"business_group,omitempty"`
	WorkType             *string  `json:"work_type,omitempty" jsonschema:"programme, project, continuous_improvement, digital_product or operational."`
	Complexity           *string  `json:"complexity,omitempty" jsonschema:"low, med or high; selects the required assessment fields."`
	Status               *string  `json:"status,omitempty" jsonschema:"captured or assessing (deciding is decide_p3m_intake)."`
	LifecyclePosition    *string  `json:"lifecycle_position,omitempty"`
}

type decideP3MIntakeArgs struct {
	ID              int     `json:"id" jsonschema:"The intake's numeric ID."`
	Decision        string  `json:"decision" jsonschema:"proceed, redirect, defer, decline or return_info (return_info reopens the intake for more information)."`
	Rationale       string  `json:"rationale" jsonschema:"Mandatory reason for the decision; stored as the permanent record."`
	Conditions      string  `json:"conditions,omitempty" jsonschema:"For proceed: conditions attached to the approval; recorded on the front-door gate review."`
	PortfolioID     *int    `json:"portfolio_id,omitempty" jsonschema:"For proceed without target_project_id: create the project under this portfolio (omit when programme_id is given)."`
	ProgrammeID     *int    `json:"programme_id,omitempty" jsonschema:"For proceed without target_project_id: create the project under this programme."`
	TargetProjectID *int    `json:"target_project_id,omitempty" jsonschema:"For proceed: link an existing project instead of creating one."`
}

type createP3MPortfolioArgs struct {
	Title              string  `json:"title" jsonschema:"Portfolio name."`
	Description        string  `json:"description,omitempty"`
	StrategicObjective string  `json:"strategic_objective,omitempty"`
	BudgetAllocated    float64 `json:"budget_allocated,omitempty"`
	RagStatus          string  `json:"rag_status,omitempty" jsonschema:"green (default), amber or red."`
	OwnerID            *int    `json:"owner_id,omitempty" jsonschema:"User ID of the portfolio owner."`
}

type createP3MProgrammeArgs struct {
	PortfolioID     int     `json:"portfolio_id" jsonschema:"Parent portfolio's numeric ID."`
	Title           string  `json:"title" jsonschema:"Programme name."`
	Description     string  `json:"description,omitempty"`
	ManagerID       *int    `json:"manager_id,omitempty" jsonschema:"User ID of the programme manager."`
	BudgetAllocated float64 `json:"budget_allocated,omitempty"`
	TargetStartDate string  `json:"target_start_date,omitempty" jsonschema:"YYYY-MM-DD or RFC 3339."`
	TargetEndDate   string  `json:"target_end_date,omitempty" jsonschema:"YYYY-MM-DD or RFC 3339."`
	RagStatus       string  `json:"rag_status,omitempty" jsonschema:"green (default), amber or red."`
}

type createP3MProjectArgs struct {
	Title                string   `json:"title" jsonschema:"Project name."`
	PortfolioID          *int     `json:"portfolio_id,omitempty" jsonschema:"Parent portfolio's numeric ID; give this or programme_id (programme wins)."`
	ProgrammeID          *int     `json:"programme_id,omitempty" jsonschema:"Parent programme's numeric ID."`
	Description          string   `json:"description,omitempty"`
	ManagerID            *int     `json:"manager_id,omitempty" jsonschema:"User ID of the project manager."`
	WorkType             *string  `json:"work_type,omitempty" jsonschema:"programme, project, continuous_improvement, digital_product or operational; selects matching gate templates."`
	CurrentStage         string   `json:"current_stage,omitempty" jsonschema:"Entry lifecycle stage key (default front_door); new projects must start at an entry stage."`
	BudgetApproved       float64  `json:"budget_approved,omitempty"`
	BudgetActual         float64  `json:"budget_actual,omitempty"`
	ProgressPercent      int      `json:"progress_percent,omitempty" jsonschema:"0-100."`
	StartDate            string   `json:"start_date,omitempty" jsonschema:"YYYY-MM-DD or RFC 3339."`
	TargetCompletionDate string   `json:"target_completion_date,omitempty" jsonschema:"YYYY-MM-DD or RFC 3339."`
}

func init() {
	Register(Default, Tool[listP3MIntakesArgs]{
		Name:        "list_p3m_intakes",
		Group:       CapabilityP3M,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List P3M Front Door intakes (the intake pipeline), newest first. Filter by status (captured, assessing, decided) and/or decision (proceed, redirect, defer, decline, return_info); the decided list is the permanent record of front-door decisions.",
		Scopes:      []string{auth.ScopeP3MRead},
		Run: func(_ context.Context, env *Env, args listP3MIntakesArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			var conds []string
			var qargs []any
			if args.Status != "" {
				if !aiP3MIntakeStatuses[args.Status] {
					return map[string]string{"error": "status must be one of: captured, assessing, decided"}, nil
				}
				conds = append(conds, "status = ?")
				qargs = append(qargs, args.Status)
			}
			if args.Decision != "" {
				if !aiP3MIntakeDecisions[args.Decision] {
					return map[string]string{"error": "decision must be one of: proceed, redirect, defer, decline, return_info"}, nil
				}
				conds = append(conds, "decision = ?")
				qargs = append(qargs, args.Decision)
			}
			query := `SELECT ` + p3mIntakeColumns + ` FROM p3m_intakes`
			if len(conds) > 0 {
				query += ` WHERE ` + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY created_at DESC, id DESC`

			rows, err := env.DB.Query(query, qargs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			out := make([]models.P3MIntake, 0)
			for rows.Next() {
				i, err := aiScanP3MIntake(rows)
				if err != nil {
					return nil, err
				}
				out = append(out, i)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	Register(Default, Tool[getP3MIntakeArgs]{
		Name:        "get_p3m_intake",
		Group:       CapabilityP3M,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get one P3M Front Door intake by ID, including its decision record (decision, rationale, conditions, decided_by, target project) when decided.",
		Scopes:      []string{auth.ScopeP3MRead},
		Run: func(_ context.Context, env *Env, args getP3MIntakeArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			i, err := aiScanP3MIntake(env.DB.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, args.ID))
			if err != nil {
				return map[string]string{"error": "intake not found"}, nil
			}
			return i, nil
		},
	})

	Register(Default, Tool[createP3MIntakeArgs]{
		Name:        "create_p3m_intake",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Capture a new idea at the P3M Front Door. New intakes start captured (or explicitly assessing); deciding them is done via decide_p3m_intake so the rationale stays mandatory.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args createP3MIntakeArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if strings.TrimSpace(args.Title) == "" {
				return map[string]string{"error": "title is required"}, nil
			}
			source := args.Source
			if source == "" {
				source = "standard"
			}
			complexity := args.Complexity
			if complexity == "" {
				complexity = "med"
			}
			if !aiP3MIntakeComplexities[complexity] {
				return map[string]string{"error": "complexity must be one of: low, med, high"}, nil
			}
			status := args.Status
			if status == "" {
				status = "captured"
			}
			if !aiP3MIntakeStatuses[status] || status == "decided" {
				return map[string]string{"error": "status must be captured or assessing on create"}, nil
			}

			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx, `
				INSERT INTO p3m_intakes (title, source, description, problem_opportunity, strategic_alignment, intended_outcomes, indicative_cost, resource_requirements, risk_notes, change_impact_notes, business_group, work_type, complexity, status, lifecycle_position, created_by, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				args.Title, source, args.Description, args.ProblemOpportunity, args.StrategicAlignment, args.IntendedOutcomes,
				aiNullFloatPtr(args.IndicativeCost), args.ResourceRequirements, args.RiskNotes, args.ChangeImpactNotes,
				args.BusinessGroup, aiNullStringPtr(args.WorkType), complexity, status, aiNullStringPtr(args.LifecyclePosition),
				env.UserID, now, now)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			i, err := aiScanP3MIntake(env.DB.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, int(id64)))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("p3m_intakes", i.ID, "create_p3m_intake", fmt.Sprintf("%s (complexity=%s status=%s)", i.Title, complexity, status))
			return i, nil
		},
	})

	Register(Default, Tool[updateP3MIntakeArgs]{
		Name:        "update_p3m_intake",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Update fields on a P3M Front Door intake (only the provided fields change). Status may move between captured and assessing; a decided intake reopens to assessing only when its decision was return_info. Deciding is exclusively decide_p3m_intake.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args updateP3MIntakeArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			old, err := aiScanP3MIntake(env.DB.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, args.ID))
			if err != nil {
				return map[string]string{"error": "intake not found"}, nil
			}

			next := old
			var changed []string
			setStr := func(key string, target *string, value *string) {
				if value == nil || *target == *value {
					return
				}
				*target = *value
				changed = append(changed, key)
			}
			setStr("title", &next.Title, args.Title)
			setStr("source", &next.Source, args.Source)
			setStr("description", &next.Description, args.Description)
			setStr("problem_opportunity", &next.ProblemOpportunity, args.ProblemOpportunity)
			setStr("strategic_alignment", &next.StrategicAlignment, args.StrategicAlignment)
			setStr("intended_outcomes", &next.IntendedOutcomes, args.IntendedOutcomes)
			setStr("resource_requirements", &next.ResourceRequirements, args.ResourceRequirements)
			setStr("risk_notes", &next.RiskNotes, args.RiskNotes)
			setStr("change_impact_notes", &next.ChangeImpactNotes, args.ChangeImpactNotes)
			setStr("business_group", &next.BusinessGroup, args.BusinessGroup)
			if args.IndicativeCost != nil && (old.IndicativeCost == nil || *old.IndicativeCost != *args.IndicativeCost) {
				next.IndicativeCost = args.IndicativeCost
				changed = append(changed, "indicative_cost")
			}
			if args.WorkType != nil && (old.WorkType == nil || *old.WorkType != *args.WorkType) {
				next.WorkType = args.WorkType
				changed = append(changed, "work_type")
			}
			if args.LifecyclePosition != nil && (old.LifecyclePosition == nil || *old.LifecyclePosition != *args.LifecyclePosition) {
				next.LifecyclePosition = args.LifecyclePosition
				changed = append(changed, "lifecycle_position")
			}
			if args.Complexity != nil && old.Complexity != *args.Complexity {
				if !aiP3MIntakeComplexities[*args.Complexity] {
					return map[string]string{"error": "complexity must be one of: low, med, high"}, nil
				}
				next.Complexity = *args.Complexity
				changed = append(changed, "complexity")
			}
			if args.Status != nil && old.Status != *args.Status {
				switch {
				case !aiP3MIntakeStatuses[*args.Status] || *args.Status == "decided":
					return map[string]string{"error": "status must be captured or assessing; use decide_p3m_intake to decide"}, nil
				case old.Status == "decided" && (old.Decision == nil || *old.Decision != "return_info"):
					return map[string]string{"error": "a decided intake can only reopen to assessing when its decision was return_info"}, nil
				default:
					next.Status = *args.Status
					changed = append(changed, "status")
				}
			}

			now := time.Now().UTC()
			if _, err := env.DB.ExecContext(ctx, `
				UPDATE p3m_intakes SET title = ?, source = ?, description = ?, problem_opportunity = ?, strategic_alignment = ?, intended_outcomes = ?, indicative_cost = ?, resource_requirements = ?, risk_notes = ?, change_impact_notes = ?, business_group = ?, work_type = ?, complexity = ?, status = ?, lifecycle_position = ?, updated_at = ? WHERE id = ?`,
				next.Title, next.Source, next.Description, next.ProblemOpportunity, next.StrategicAlignment, next.IntendedOutcomes,
				aiNullFloatPtr(next.IndicativeCost), next.ResourceRequirements, next.RiskNotes, next.ChangeImpactNotes,
				next.BusinessGroup, aiNullStringPtr(next.WorkType), next.Complexity, next.Status, aiNullStringPtr(next.LifecyclePosition), now, args.ID); err != nil {
				return nil, err
			}
			env.AuditWrite("p3m_intakes", next.ID, "update_p3m_intake", fmt.Sprintf("%s: changed %s", next.Title, strings.Join(changed, ", ")))
			updated, err := aiScanP3MIntake(env.DB.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, args.ID))
			if err != nil {
				return nil, err
			}
			return updated, nil
		},
	})

	Register(Default, Tool[decideP3MIntakeArgs]{
		Name:        "decide_p3m_intake",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskHigh,
		Description: "Record a Front Door decision on an intake. The rationale is mandatory and stored as the permanent record (the decided list prevents duplicate ideas). Every screen-required field for the intake's complexity must be completed first. proceed creates a project pre-filled from the intake at the lifecycle entry stage with a passed front-door gate carrying the conditions, or links target_project_id when given.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args decideP3MIntakeArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if !aiP3MIntakeDecisions[args.Decision] {
				return map[string]string{"error": "decision must be one of: proceed, redirect, defer, decline, return_info"}, nil
			}
			if strings.TrimSpace(args.Rationale) == "" {
				return map[string]string{"error": "rationale is required for every front door decision"}, nil
			}

			old, err := aiScanP3MIntake(env.DB.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, args.ID))
			if err != nil {
				return map[string]string{"error": "intake not found"}, nil
			}
			if old.Status == "decided" {
				return map[string]string{"error": "intake is already decided; reopen it via return_info to re-decide"}, nil
			}
			if missing := aiP3MIntakeMissingRequiredFields(env.DB, old); len(missing) > 0 {
				return map[string]string{"error": fmt.Sprintf("missing required assessment fields for %s complexity: %s", old.Complexity, strings.Join(missing, ", "))}, nil
			}

			now := time.Now().UTC()
			var targetProjectID *int
			if args.Decision == "proceed" {
				switch {
				case old.TargetProjectID != nil:
					targetProjectID = old.TargetProjectID // re-decision after return_info: the project already exists.
				case args.TargetProjectID != nil:
					var count int
					if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, *args.TargetProjectID).Scan(&count); err != nil || count == 0 {
						return map[string]string{"error": "target project not found"}, nil
					}
					targetProjectID = args.TargetProjectID // link an existing project.
				default:
					created, err := aiP3MIntakeCreateProject(ctx, env.DB, env.UserID, old, args.PortfolioID, args.ProgrammeID, args.Conditions)
					if err != nil {
						return map[string]string{"error": err.Error()}, nil
					}
					targetProjectID = &created
				}
			}

			if _, err := env.DB.ExecContext(ctx, `
				UPDATE p3m_intakes SET decision = ?, decided_by = ?, decision_date = ?, conditions = ?, rationale = ?, status = 'decided', target_project_id = ?, updated_at = ? WHERE id = ?`,
				args.Decision, env.UserID, now, args.Conditions, args.Rationale, nullIntPtr(targetProjectID), now, args.ID); err != nil {
				return nil, err
			}

			i, err := aiScanP3MIntake(env.DB.QueryRow(`SELECT `+p3mIntakeColumns+` FROM p3m_intakes WHERE id = ?`, args.ID))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("p3m_intakes", i.ID, "decide_p3m_intake", fmt.Sprintf("%s: %s (project=%v)", i.Title, args.Decision, targetProjectID))
			return i, nil
		},
	})

	Register(Default, Tool[createP3MPortfolioArgs]{
		Name:        "create_p3m_portfolio",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create a P3M portfolio (top of the portfolio→programme→project hierarchy).",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args createP3MPortfolioArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if strings.TrimSpace(args.Title) == "" {
				return map[string]string{"error": "title is required"}, nil
			}
			rag := args.RagStatus
			if rag == "" {
				rag = models.P3MRagGreen
			}
			if !aiP3MValidRag(rag) {
				return map[string]string{"error": "rag_status must be green, amber or red"}, nil
			}

			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx, `
				INSERT INTO p3m_portfolios (title, description, strategic_objective, budget_allocated, rag_status, owner_id, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				args.Title, args.Description, args.StrategicObjective, args.BudgetAllocated, rag, nullIntPtr(args.OwnerID), now, now)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			pf, err := aiScanP3MPortfolio(env.DB.QueryRow(`SELECT `+p3mPortfolioColumns+` FROM p3m_portfolios WHERE id = ?`, int(id64)))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("p3m_portfolios", pf.ID, "create_p3m_portfolio", args.Title)
			return pf, nil
		},
	})

	Register(Default, Tool[createP3MProgrammeArgs]{
		Name:        "create_p3m_programme",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create a P3M programme under an existing portfolio.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args createP3MProgrammeArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if strings.TrimSpace(args.Title) == "" {
				return map[string]string{"error": "title is required"}, nil
			}
			var exists bool
			if err := env.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM p3m_portfolios WHERE id = ?)`, args.PortfolioID).Scan(&exists); err != nil || !exists {
				return map[string]string{"error": "portfolio not found"}, nil
			}
			startDate, err := aiParsePlanDate(args.TargetStartDate)
			if err != nil {
				return map[string]string{"error": err.Error()}, nil
			}
			endDate, err := aiParsePlanDate(args.TargetEndDate)
			if err != nil {
				return map[string]string{"error": err.Error()}, nil
			}
			rag := args.RagStatus
			if rag == "" {
				rag = models.P3MRagGreen
			}
			if !aiP3MValidRag(rag) {
				return map[string]string{"error": "rag_status must be green, amber or red"}, nil
			}

			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx, `
				INSERT INTO p3m_programmes (portfolio_id, title, description, manager_id, budget_allocated, target_start_date, target_end_date, rag_status, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				args.PortfolioID, args.Title, args.Description, nullIntPtr(args.ManagerID), args.BudgetAllocated,
				nullTimePtr(startDate), nullTimePtr(endDate), rag, now, now)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			pg, err := aiScanP3MProgramme(env.DB.QueryRow(`SELECT `+p3mProgrammeColumns+` FROM p3m_programmes WHERE id = ?`, int(id64)))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("p3m_programmes", pg.ID, "create_p3m_programme", fmt.Sprintf("%s (portfolio %d)", args.Title, args.PortfolioID))
			return pg, nil
		},
	})

	Register(Default, Tool[createP3MProjectArgs]{
		Name:        "create_p3m_project",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create a P3M project under a portfolio or programme. New projects must start at an entry lifecycle stage (default front_door); pass the mandatory gates to advance them.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args createP3MProjectArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if strings.TrimSpace(args.Title) == "" {
				return map[string]string{"error": "title is required"}, nil
			}
			fid, pid, err := aiResolveP3MParents(ctx, env.DB, args.PortfolioID, args.ProgrammeID)
			if err != nil {
				return map[string]string{"error": err.Error()}, nil
			}

			stage := args.CurrentStage
			if stage == "" {
				stage = models.P3MLifecycleStageFrontDoor
			}
			stages, err := aiLoadEnabledLifecycleStages(ctx, env.DB)
			if err != nil {
				return nil, err
			}
			toPos := -1
			for i, s := range stages {
				if s.StageKey == stage {
					toPos = i
					break
				}
			}
			if toPos < 0 {
				keys := make([]string, 0, len(stages))
				for _, s := range stages {
					keys = append(keys, s.StageKey)
				}
				return map[string]string{"error": fmt.Sprintf("stage %q is not an enabled lifecycle stage (enabled: %s)", stage, strings.Join(keys, ", "))}, nil
			}
			if !stages[toPos].IsEntry {
				entryKeys := make([]string, 0)
				for _, s := range stages {
					if s.IsEntry {
						entryKeys = append(entryKeys, s.StageKey)
					}
				}
				return map[string]string{"error": fmt.Sprintf("new projects must start at an entry stage (%s); pass the mandatory gates to advance", strings.Join(entryKeys, ", "))}, nil
			}

			startDate, err := aiParsePlanDate(args.StartDate)
			if err != nil {
				return map[string]string{"error": err.Error()}, nil
			}
			endDate, err := aiParsePlanDate(args.TargetCompletionDate)
			if err != nil {
				return map[string]string{"error": err.Error()}, nil
			}

			p := models.P3MProject{
				PortfolioID: fid, ProgrammeID: pid, Title: args.Title, Description: args.Description, ManagerID: args.ManagerID,
				WorkType: args.WorkType, CurrentStage: stage, BudgetApproved: args.BudgetApproved, BudgetActual: args.BudgetActual,
				ProgressPercent: args.ProgressPercent, StartDate: startDate, TargetCompletionDate: endDate,
			}
			aiP3MApplyComputed(&p)

			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx, `
				INSERT INTO p3m_projects (portfolio_id, programme_id, title, description, manager_id, work_type, current_stage, budget_approved, budget_actual, progress_percent, start_date, target_completion_date, rag_status, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				nullIntPtr(p.PortfolioID), nullIntPtr(p.ProgrammeID), p.Title, p.Description, nullIntPtr(p.ManagerID),
				aiNullStringPtr(p.WorkType), p.CurrentStage, p.BudgetApproved, p.BudgetActual, p.ProgressPercent,
				nullTimePtr(p.StartDate), nullTimePtr(p.TargetCompletionDate), p.RagStatus, now, now)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			p.ID = int(id64)
			p.CreatedAt, p.UpdatedAt = now, now
			env.AuditWrite("p3m_projects", p.ID, "create_p3m_project", fmt.Sprintf("%s (stage=%s)", args.Title, stage))
			return p, nil
		},
	})
}
