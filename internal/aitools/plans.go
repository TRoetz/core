package aitools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"windshift/internal/auth"
	"windshift/internal/cql"
	"windshift/internal/database"
	"windshift/internal/models"
	"windshift/internal/repository"
)

// This file adds MCP/AI tools for the Plans module — the cross-workspace
// planning container that groups workspaces, P3M entities, and SMS
// stakeholders into delivery plans. Access mirrors the HTTP surface: the
// global "plans.view" permission gates every tool (reads and writes alike),
// and the plan itself is the unit of access rather than any single workspace.

const planColumns = `id, name, description, status, color, owner_id, start_date, end_date, created_at, updated_at`

var planStatuses = map[string]bool{
	models.PlanStatusDraft:     true,
	models.PlanStatusActive:    true,
	models.PlanStatusCompleted: true,
	models.PlanStatusArchived:  true,
}

// aiPlanSourceCondition replicates handlers.PlanHandler.planSourceCondition:
// the WHERE fragment selecting every item that belongs to a plan (items in
// linked workspaces OR matching any of the plan's named CQL filters). The
// returned SQL references only aliases from repository.ItemListFilterFromClause().
func aiPlanSourceCondition(ctx context.Context, db database.Database, planID int) (string, []any, error) {
	var wsIDs []int
	rows, err := db.QueryContext(ctx, `SELECT workspace_id FROM plan_workspaces WHERE plan_id = ?`, planID)
	if err != nil {
		return "", nil, fmt.Errorf("load plan workspaces: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return "", nil, err
		}
		wsIDs = append(wsIDs, id)
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}

	var filterFragments []string
	var filterArgs []any
	fRows, err := db.QueryContext(ctx, `SELECT cql_query FROM plan_filters WHERE plan_id = ? ORDER BY id`, planID)
	if err != nil {
		return "", nil, fmt.Errorf("load plan filters: %w", err)
	}
	defer fRows.Close()
	var queries []string
	for fRows.Next() {
		var q string
		if err := fRows.Scan(&q); err != nil {
			return "", nil, err
		}
		queries = append(queries, q)
	}
	if err := fRows.Err(); err != nil {
		return "", nil, err
	}

	if len(queries) > 0 {
		wsMap, err := repository.NewWorkspaceRepository(db).BuildWorkspaceMap()
		if err != nil {
			return "", nil, fmt.Errorf("build workspace map: %w", err)
		}
		cfMap, err := repository.NewItemRepository(db).GetCQLCustomFieldMap()
		if err != nil {
			return "", nil, fmt.Errorf("load custom field map: %w", err)
		}
		evaluator := cql.NewEvaluator(wsMap, cfMap, db.GetDriverName())
		for _, q := range queries {
			frag, args, err := evaluator.EvaluateToSQL(q)
			if err != nil {
				return "", nil, fmt.Errorf("evaluate plan filter %q: %w", q, err)
			}
			filterFragments = append(filterFragments, frag)
			filterArgs = append(filterArgs, args...)
		}
	}

	var parts []string
	var args []any
	if len(wsIDs) > 0 {
		ph := make([]string, len(wsIDs))
		for i, id := range wsIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		parts = append(parts, "i.workspace_id IN ("+strings.Join(ph, ",")+")")
	}
	for _, frag := range filterFragments {
		if strings.TrimSpace(frag) == "" {
			continue
		}
		parts = append(parts, "("+frag+")")
		args = append(args, filterArgs...)
	}
	if len(parts) == 0 {
		return "1=0", nil, nil
	}
	return strings.Join(parts, " OR "), args, nil
}

// aiPlanItemFromClause extends the shared CQL alias set with teams and users
// so any valid filter fragment resolves in every plan query.
var aiPlanItemFromClause = repository.ItemListFilterFromClause() + `
	LEFT JOIN teams tm ON i.team_id = tm.id
	LEFT JOIN users u ON i.assignee_id = u.id`

const aiPlanItemSelect = `
	i.id,
	w.key || '-' || CAST(i.workspace_item_number AS TEXT),
	i.title,
	COALESCE(it.name, ''),
	i.status_id,
	COALESCE(st.name, ''),
	COALESCE(sc.builtin_key, ''),
	sc.is_completed,
	i.workspace_id,
	w.key,
	w.name,
	i.parent_id,
	i.team_id,
	COALESCE(tm.name, ''),
	i.assignee_id,
	COALESCE(u.first_name || ' ' || u.last_name, ''),
	COALESCE(u.avatar_url, ''),
	i.start_date, i.end_date, i.due_date,
	i.story_points, i.estimate_minutes`

func scanPlanItemRow(row interface{ Scan(...any) error }) (models.PlanItem, error) {
	var it models.PlanItem
	var completed bool
	var parentID sql.NullInt64
	var teamID sql.NullInt64
	var assigneeID sql.NullInt64
	var startDate sql.NullTime
	var endDate sql.NullTime
	var dueDate sql.NullTime
	var storyPoints sql.NullFloat64
	var estimateMin sql.NullInt64
	err := row.Scan(
		&it.ID, &it.Key, &it.Title, &it.ItemTypeName,
		&it.StatusID, &it.StatusName, &it.StatusCategory, &completed,
		&it.WorkspaceID, &it.WorkspaceKey, &it.WorkspaceName,
		&parentID, &teamID, &it.TeamName,
		&assigneeID, &it.AssigneeName, &it.AssigneeAvatarURL,
		&startDate, &endDate, &dueDate,
		&storyPoints, &estimateMin,
	)
	if err != nil {
		return it, err
	}
	it.IsCompleted = completed
	if parentID.Valid {
		it.ParentID = new(int(parentID.Int64))
	}
	if teamID.Valid {
		it.TeamID = new(int(teamID.Int64))
	}
	if assigneeID.Valid {
		it.AssigneeID = new(int(assigneeID.Int64))
	}
	if startDate.Valid {
		it.StartDate = new(startDate.Time)
	}
	if endDate.Valid {
		it.EndDate = new(endDate.Time)
	}
	if dueDate.Valid {
		it.DueDate = new(dueDate.Time)
	}
	if storyPoints.Valid {
		it.StoryPoints = new(float64(storyPoints.Float64))
	}
	if estimateMin.Valid {
		it.EstimateMinutes = new(int(estimateMin.Int64))
	}
	return it, nil
}

func scanPlanRow(row interface{ Scan(...any) error }) (models.Plan, error) {
	var p models.Plan
	var ownerID sql.NullInt64
	var startDate, endDate sql.NullTime
	var createdAt, updatedAt time.Time
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color,
		&ownerID, &startDate, &endDate, &createdAt, &updatedAt)
	if err != nil {
		return p, err
	}
	if ownerID.Valid {
		v := int(ownerID.Int64)
		p.OwnerID = &v
	}
	if startDate.Valid {
		v := startDate.Time
		p.StartDate = &v
	}
	if endDate.Valid {
		v := endDate.Time
		p.EndDate = &v
	}
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	return p, nil
}

func getPlanRow(db database.Database, id int) (*models.Plan, error) {
	row := db.QueryRow(`SELECT `+planColumns+` FROM plans WHERE id = ?`, id)
	p, err := scanPlanRow(row)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func scanPlanWithCounts(row interface{ Scan(...any) error }, wsCount, p3mCount, shCount *int) (models.Plan, error) {
	var p models.Plan
	var ownerID sql.NullInt64
	var startDate, endDate sql.NullTime
	var createdAt, updatedAt time.Time
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color,
		&ownerID, &startDate, &endDate, &createdAt, &updatedAt, wsCount, p3mCount, shCount)
	if err != nil {
		return p, err
	}
	if ownerID.Valid {
		v := int(ownerID.Int64)
		p.OwnerID = &v
	}
	if startDate.Valid {
		v := startDate.Time
		p.StartDate = &v
	}
	if endDate.Valid {
		v := endDate.Time
		p.EndDate = &v
	}
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	return p, nil
}

// aiParsePlanDate accepts either "YYYY-MM-DD" or RFC 3339 and returns a UTC
// time, or nil when empty.
func aiParsePlanDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			utc := t.UTC()
			return &utc, nil
		}
	}
	return nil, fmt.Errorf("invalid date %q (want YYYY-MM-DD or RFC 3339)", s)
}

func aiRequirePlansView(env *Env) (bool, error) {
	ok, err := env.PermService.HasGlobalPermission(env.UserID, models.PermissionPlansView)
	return ok, err
}

// ----------------------------------------------------------------------------
// list_plans
// ----------------------------------------------------------------------------

type listPlansArgs struct {
	Status string `json:"status,omitempty" jsonschema:"Filter by status: draft, active, completed, or archived."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Max plans to return (default 50, max 200)."`
	Offset int    `json:"offset,omitempty" jsonschema:"Offset for pagination."`
}

func init() {
	Register(Default, Tool[listPlansArgs]{
		Name:        "list_plans",
		Group:       CapabilityPlans,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List delivery plans (cross-workspace planning containers) with their linked workspace, P3M entity, and stakeholder counts.",
		Scopes:      []string{auth.ScopePlansRead},
		Run: func(ctx context.Context, env *Env, args listPlansArgs) (any, error) {
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			limit := args.Limit
			if limit <= 0 {
				limit = 50
			}
			if limit > 200 {
				limit = 200
			}
			where := "1=1"
			var queryArgs []any
			if args.Status != "" {
				where = "status = ?"
				queryArgs = append(queryArgs, args.Status)
			}
			rows, err := env.DB.QueryContext(ctx, `
				SELECT `+planColumns+`,
					(SELECT COUNT(*) FROM plan_workspaces pw WHERE pw.plan_id = plans.id),
					(SELECT COUNT(*) FROM plan_p3m_entities pe WHERE pe.plan_id = plans.id),
					(SELECT COUNT(*) FROM plan_stakeholders ps WHERE ps.plan_id = plans.id)
				FROM plans
				WHERE `+where+`
				ORDER BY name COLLATE NOCASE
				LIMIT ? OFFSET ?`, append(queryArgs, limit, args.Offset)...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			plans := make([]models.Plan, 0)
			for rows.Next() {
				var wsCount, p3mCount, shCount int
				p, err := scanPlanWithCounts(rows, &wsCount, &p3mCount, &shCount)
				if err != nil {
					return nil, err
				}
				p.WorkspaceCount = wsCount
				p.P3MEntityCount = p3mCount
				p.StakeholderCount = shCount
				plans = append(plans, p)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return plans, nil
		},
	})

	// ------------------------------------------------------------------------
	// get_plan
	// ------------------------------------------------------------------------
	Register(Default, Tool[getPlanArgs]{
		Name:        "get_plan",
		Group:       CapabilityPlans,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get a delivery plan with its linked workspaces, P3M entities, SMS stakeholders, and CQL filters.",
		Scopes:      []string{auth.ScopePlansRead},
		Run: func(ctx context.Context, env *Env, args getPlanArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			p, err := getPlanRow(env.DB, args.PlanID)
			if err != nil {
				return map[string]string{"error": "plan not found"}, nil
			}
			out := models.PlanDetail{Plan: *p, Workspaces: []models.PlanWorkspace{}, P3MEntities: []models.PlanP3MEntity{}, Stakeholders: []models.PlanStakeholder{}}

			wsRows, err := env.DB.QueryContext(ctx, `
				SELECT pw.plan_id, pw.workspace_id, pw.created_at,
					w.name, w.key,
					COALESCE(i.c, 0)
				FROM plan_workspaces pw
				JOIN workspaces w ON w.id = pw.workspace_id
				LEFT JOIN (SELECT workspace_id, COUNT(*) AS c FROM items GROUP BY workspace_id) i
					ON i.workspace_id = pw.workspace_id
				WHERE pw.plan_id = ?
				ORDER BY w.name COLLATE NOCASE`, args.PlanID)
			if err != nil {
				return nil, err
			}
			for wsRows.Next() {
				var l models.PlanWorkspace
				var createdAt time.Time
				if err := wsRows.Scan(&l.PlanID, &l.WorkspaceID, &createdAt, &l.WorkspaceName, &l.WorkspaceKey, &l.ItemCount); err != nil {
					wsRows.Close()
					return nil, err
				}
				l.CreatedAt = createdAt
				out.Workspaces = append(out.Workspaces, l)
			}
			if err := wsRows.Err(); err != nil {
				wsRows.Close()
				return nil, err
			}
			wsRows.Close()

			p3mRows, err := env.DB.QueryContext(ctx, `
				SELECT pe.plan_id, pe.entity_type, pe.entity_id, pe.created_at,
					CASE pe.entity_type
						WHEN 'portfolio' THEN pf.title
						WHEN 'programme' THEN pg.title
						ELSE pj.title
					END
				FROM plan_p3m_entities pe
				LEFT JOIN p3m_portfolios pf ON pe.entity_type = 'portfolio' AND pf.id = pe.entity_id
				LEFT JOIN p3m_programmes pg ON pe.entity_type = 'programme' AND pg.id = pe.entity_id
				LEFT JOIN p3m_projects pj ON pe.entity_type = 'project' AND pj.id = pe.entity_id
				WHERE pe.plan_id = ?
				ORDER BY pe.entity_type, pe.entity_id`, args.PlanID)
			if err != nil {
				return nil, err
			}
			for p3mRows.Next() {
				var l models.PlanP3MEntity
				var createdAt time.Time
				var title sql.NullString
				if err := p3mRows.Scan(&l.PlanID, &l.EntityType, &l.EntityID, &createdAt, &title); err != nil {
					p3mRows.Close()
					return nil, err
				}
				l.CreatedAt = createdAt
				l.EntityTitle = title.String
				out.P3MEntities = append(out.P3MEntities, l)
			}
			if err := p3mRows.Err(); err != nil {
				p3mRows.Close()
				return nil, err
			}
			p3mRows.Close()

			shRows, err := env.DB.QueryContext(ctx, `
				SELECT ps.plan_id, ps.stakeholder_id, ps.created_at,
					s.name, s.role_classification, s.organization
				FROM plan_stakeholders ps
				JOIN sms_stakeholders s ON s.id = ps.stakeholder_id
				WHERE ps.plan_id = ?
				ORDER BY s.name COLLATE NOCASE`, args.PlanID)
			if err != nil {
				return nil, err
			}
			for shRows.Next() {
				var l models.PlanStakeholder
				var createdAt time.Time
				if err := shRows.Scan(&l.PlanID, &l.StakeholderID, &createdAt, &l.Name, &l.RoleClassification, &l.Organization); err != nil {
					shRows.Close()
					return nil, err
				}
				l.CreatedAt = createdAt
				out.Stakeholders = append(out.Stakeholders, l)
			}
			if err := shRows.Err(); err != nil {
				shRows.Close()
				return nil, err
			}
			shRows.Close()

			fRows, err := env.DB.QueryContext(ctx, `SELECT id, plan_id, name, cql_query, created_at FROM plan_filters WHERE plan_id = ? ORDER BY name COLLATE NOCASE`, args.PlanID)
			if err != nil {
				return nil, err
			}
			out.Filters = make([]models.PlanFilter, 0)
			for fRows.Next() {
				var f models.PlanFilter
				if err := fRows.Scan(&f.ID, &f.PlanID, &f.Name, &f.CQLQuery, &f.CreatedAt); err != nil {
					fRows.Close()
					return nil, err
				}
				out.Filters = append(out.Filters, f)
			}
			if err := fRows.Err(); err != nil {
				fRows.Close()
				return nil, err
			}
			fRows.Close()

			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_plan
	// ------------------------------------------------------------------------
	Register(Default, Tool[createPlanArgs]{
		Name:        "create_plan",
		Group:       CapabilityPlans,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create a delivery plan. Optionally link workspaces, P3M entities, and SMS stakeholders.",
		Scopes:      []string{auth.ScopePlansWrite},
		Run: func(ctx context.Context, env *Env, args createPlanArgs) (any, error) {
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			name := strings.TrimSpace(args.Name)
			if name == "" {
				return map[string]string{"error": "name is required"}, nil
			}
			status := args.Status
			if status == "" {
				status = models.PlanStatusActive
			}
			if !planStatuses[status] {
				return map[string]string{"error": "status must be draft, active, completed or archived"}, nil
			}
			startDate, err := aiParsePlanDate(args.StartDate)
			if err != nil {
				return map[string]string{"error": err.Error()}, nil
			}
			endDate, err := aiParsePlanDate(args.EndDate)
			if err != nil {
				return map[string]string{"error": err.Error()}, nil
			}
			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx, `
				INSERT INTO plans (name, description, status, color, owner_id, start_date, end_date, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				name, args.Description, status, args.Color, nullIntPtr(args.OwnerID),
				nullTimePtr(startDate), nullTimePtr(endDate), now, now)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			planID := int(id64)
			if err := aiSetPlanLinks(ctx, env.DB, planID, args.WorkspaceIDs, args.P3MEntities, args.StakeholderIDs); err != nil {
				return map[string]string{"error": "plan created (id " + fmt.Sprintf("%d", planID) + ") but linking failed: " + err.Error()}, nil
			}
			p, err := getPlanRow(env.DB, planID)
			if err != nil {
				return nil, err
			}
			env.AuditWrite("plan", planID, "create_plan", name)
			return p, nil
		},
	})

	// ------------------------------------------------------------------------
	// update_plan
	// ------------------------------------------------------------------------
	Register(Default, Tool[updatePlanArgs]{
		Name:        "update_plan",
		Group:       CapabilityPlans,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Update a delivery plan's name, description, status, color, owner, and dates. Only provided fields change.",
		Scopes:      []string{auth.ScopePlansWrite},
		Run: func(ctx context.Context, env *Env, args updatePlanArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			existing, err := getPlanRow(env.DB, args.PlanID)
			if err != nil {
				return map[string]string{"error": "plan not found"}, nil
			}
			name := existing.Name
			if strings.TrimSpace(args.Name) != "" {
				name = strings.TrimSpace(args.Name)
			}
			status := existing.Status
			if args.Status != "" {
				if !planStatuses[args.Status] {
					return map[string]string{"error": "status must be draft, active, completed or archived"}, nil
				}
				status = args.Status
			}
			startDate := existing.StartDate
			if args.StartDate != nil {
				parsed, err := aiParsePlanDate(*args.StartDate)
				if err != nil {
					return map[string]string{"error": err.Error()}, nil
				}
				startDate = parsed
			}
			endDate := existing.EndDate
			if args.EndDate != nil {
				parsed, err := aiParsePlanDate(*args.EndDate)
				if err != nil {
					return map[string]string{"error": err.Error()}, nil
				}
				endDate = parsed
			}
			description := existing.Description
			if args.Description != "" {
				description = args.Description
			}
			color := existing.Color
			if args.Color != "" {
				color = args.Color
			}
			ownerID := existing.OwnerID
			if args.OwnerID != nil {
				ownerID = args.OwnerID
			}
			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx,
				`UPDATE plans SET name = ?, description = ?, status = ?, color = ?, owner_id = ?, start_date = ?, end_date = ?, updated_at = ? WHERE id = ?`,
				name, description, status, color, nullIntPtr(ownerID),
				nullTimePtr(startDate), nullTimePtr(endDate), now, args.PlanID)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return map[string]string{"error": "plan not found"}, nil
			}
			p, err := getPlanRow(env.DB, args.PlanID)
			if err != nil {
				return nil, err
			}
			env.AuditWrite("plan", args.PlanID, "update_plan", name)
			return p, nil
		},
	})

	// ------------------------------------------------------------------------
	// set_plan_workspaces
	// ------------------------------------------------------------------------
	Register(Default, Tool[setPlanWorkspacesArgs]{
		Name:        "set_plan_workspaces",
		Group:       CapabilityPlans,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Replace the set of workspaces linked to a plan. This is a full replace: pass the complete desired list of workspace IDs.",
		Scopes:      []string{auth.ScopePlansWrite},
		Run: func(ctx context.Context, env *Env, args setPlanWorkspacesArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if _, err := getPlanRow(env.DB, args.PlanID); err != nil {
				return map[string]string{"error": "plan not found"}, nil
			}
			seen := make(map[int]bool, len(args.WorkspaceIDs))
			for _, wsID := range args.WorkspaceIDs {
				if seen[wsID] {
					continue
				}
				seen[wsID] = true
				var exists bool
				if err := env.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id = ?)`, wsID).Scan(&exists); err != nil {
					return nil, err
				}
				if !exists {
					return map[string]string{"error": fmt.Sprintf("workspace %d not found", wsID)}, nil
				}
			}
			tx, err := env.DB.Begin()
			if err != nil {
				return nil, err
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.Exec(`DELETE FROM plan_workspaces WHERE plan_id = ?`, args.PlanID); err != nil {
				return nil, err
			}
			for wsID := range seen {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO plan_workspaces (plan_id, workspace_id, created_at) VALUES (?, ?, ?)`, args.PlanID, wsID, time.Now().UTC()); err != nil {
					return nil, err
				}
			}
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			env.AuditWrite("plan", args.PlanID, "set_plan_workspaces", fmt.Sprintf("%d workspace(s)", len(seen)))
			return map[string]any{"plan_id": args.PlanID, "workspace_ids": makeIntSlice(seen)}, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_plan_items
	// ------------------------------------------------------------------------
	Register(Default, Tool[listPlanItemsArgs]{
		Name:        "list_plan_items",
		Group:       CapabilityPlans,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List the work items in a plan (across its linked workspaces and CQL filters), with status, type, assignee, and dates.",
		Scopes:      []string{auth.ScopePlansRead},
		Run: func(ctx context.Context, env *Env, args listPlanItemsArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			sourceSQL, sourceArgs, err := aiPlanSourceCondition(ctx, env.DB, args.PlanID)
			if err != nil {
				return nil, err
			}
			where := "i.merged_into_item_id IS NULL AND (" + sourceSQL + ")"
			qArgs := append([]any{}, sourceArgs...)
			if s := strings.TrimSpace(args.Search); s != "" {
				where += " AND i.title LIKE ? ESCAPE '\\'"
				qArgs = append(qArgs, "%"+escapeLike(s)+"%")
			}
			if t := strings.TrimSpace(args.Type); t != "" {
				where += " AND it.name = ?"
				qArgs = append(qArgs, t)
			}
			limit := args.Limit
			if limit <= 0 {
				limit = 200
			}
			if limit > 1000 {
				limit = 1000
			}
			var total int
			if err := env.DB.QueryRowContext(ctx, "SELECT COUNT(*) "+aiPlanItemFromClause+" WHERE "+where, append([]any{}, qArgs...)...).Scan(&total); err != nil {
				return nil, err
			}
			pageArgs := append(append([]any{}, qArgs...), limit, args.Offset)
			rows, err := env.DB.QueryContext(ctx, "SELECT "+aiPlanItemSelect+" "+aiPlanItemFromClause+" WHERE "+where+" ORDER BY w.name COLLATE NOCASE, i.workspace_item_number LIMIT ? OFFSET ?", pageArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			items := make([]models.PlanItem, 0)
			for rows.Next() {
				item, err := scanPlanItemRow(rows)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return map[string]any{"items": items, "total": total}, nil
		},
	})

	// ------------------------------------------------------------------------
	// get_plan_summary
	// ------------------------------------------------------------------------
	Register(Default, Tool[getPlanSummaryArgs]{
		Name:        "get_plan_summary",
		Group:       CapabilityPlans,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get aggregate progress metrics for a plan: totals, progress %, breakdowns by status/type/workspace, and upcoming due items.",
		Scopes:      []string{auth.ScopePlansRead},
		Run: func(ctx context.Context, env *Env, args getPlanSummaryArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			sourceSQL, sourceArgs, err := aiPlanSourceCondition(ctx, env.DB, args.PlanID)
			if err != nil {
				return nil, err
			}
			where := "i.merged_into_item_id IS NULL AND (" + sourceSQL + ")"
			summary := models.PlanSummary{ByStatus: []models.PlanCount{}, ByType: []models.PlanCount{}, ByWorkspace: []models.PlanWorkspaceStat{}, Upcoming: []models.PlanItem{}}
			var done int
			if err := env.DB.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(CASE WHEN sc.is_completed THEN 1 ELSE 0 END), 0) "+aiPlanItemFromClause+" WHERE "+where, append([]any{}, sourceArgs...)...).Scan(&summary.TotalItems, &done); err != nil {
				return nil, err
			}
			summary.DoneItems = done
			if summary.TotalItems > 0 {
				summary.ProgressPct = float64(done) / float64(summary.TotalItems) * 100
			}
			sRows, err := env.DB.QueryContext(ctx, `SELECT COALESCE(sc.name, 'Uncategorized'), COALESCE(sc.color, ''), COUNT(*) `+aiPlanItemFromClause+` WHERE `+where+` GROUP BY sc.id ORDER BY COUNT(*) DESC`, append([]any{}, sourceArgs...)...)
			if err != nil {
				return nil, err
			}
			for sRows.Next() {
				var c models.PlanCount
				if err := sRows.Scan(&c.Name, &c.Color, &c.Count); err != nil {
					sRows.Close()
					return nil, err
				}
				summary.ByStatus = append(summary.ByStatus, c)
			}
			if err := sRows.Err(); err == nil {
				sRows.Close()
			} else {
				sRows.Close()
				return nil, err
			}
			tRows, err := env.DB.QueryContext(ctx, "SELECT COALESCE(it.name, 'Unknown'), COUNT(*) "+aiPlanItemFromClause+" WHERE "+where+" GROUP BY it.id ORDER BY COUNT(*) DESC", append([]any{}, sourceArgs...)...)
			if err != nil {
				return nil, err
			}
			for tRows.Next() {
				var c models.PlanCount
				if err := tRows.Scan(&c.Name, &c.Count); err != nil {
					tRows.Close()
					return nil, err
				}
				summary.ByType = append(summary.ByType, c)
			}
			if err := tRows.Err(); err == nil {
				tRows.Close()
			} else {
				tRows.Close()
				return nil, err
			}
			wRows, err := env.DB.QueryContext(ctx, `SELECT w.id, w.key, w.name, COUNT(*), COALESCE(SUM(CASE WHEN sc.is_completed THEN 1 ELSE 0 END), 0) `+aiPlanItemFromClause+` WHERE `+where+` GROUP BY w.id ORDER BY w.name COLLATE NOCASE`, append([]any{}, sourceArgs...)...)
			if err != nil {
				return nil, err
			}
			for wRows.Next() {
				var s models.PlanWorkspaceStat
				if err := wRows.Scan(&s.WorkspaceID, &s.WorkspaceKey, &s.WorkspaceName, &s.ItemCount, &s.DoneCount); err != nil {
					wRows.Close()
					return nil, err
				}
				if s.ItemCount > 0 {
					s.ProgressPct = float64(s.DoneCount) / float64(s.ItemCount) * 100
				}
				summary.ByWorkspace = append(summary.ByWorkspace, s)
			}
			if err := wRows.Err(); err == nil {
				wRows.Close()
			} else {
				wRows.Close()
				return nil, err
			}
			now := time.Now().UTC()
			horizon := now.AddDate(0, 0, 14)
			upArgs := append(append([]any{}, sourceArgs...), now, horizon)
			uRows, err := env.DB.QueryContext(ctx, "SELECT "+aiPlanItemSelect+" "+aiPlanItemFromClause+` WHERE `+where+`
				AND i.due_date IS NOT NULL AND i.due_date >= ? AND i.due_date <= ?
				AND (sc.is_completed IS NULL OR sc.is_completed = 0)
				ORDER BY i.due_date LIMIT 25`, upArgs...)
			if err != nil {
				return nil, err
			}
			for uRows.Next() {
				item, err := scanPlanItemRow(uRows)
				if err != nil {
					uRows.Close()
					return nil, err
				}
				summary.Upcoming = append(summary.Upcoming, item)
			}
			if err := uRows.Err(); err == nil {
				uRows.Close()
			} else {
				uRows.Close()
				return nil, err
			}
			return summary, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_plan_dependencies
	// ------------------------------------------------------------------------
	Register(Default, Tool[listPlanDependenciesArgs]{
		Name:        "list_plan_dependencies",
		Group:       CapabilityPlans,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List item dependencies (blocks/related) where both items belong to a plan.",
		Scopes:      []string{auth.ScopePlansRead},
		Run: func(ctx context.Context, env *Env, args listPlanDependenciesArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			sourceSQL, sourceArgs, err := aiPlanSourceCondition(ctx, env.DB, args.PlanID)
			if err != nil {
				return nil, err
			}
			where := "i.merged_into_item_id IS NULL AND (" + sourceSQL + ")"
			idRows, err := env.DB.QueryContext(ctx, "SELECT i.id "+aiPlanItemFromClause+" WHERE "+where, append([]any{}, sourceArgs...)...)
			if err != nil {
				return nil, err
			}
			inPlan := make(map[int]bool)
			for idRows.Next() {
				var itemID int
				if err := idRows.Scan(&itemID); err != nil {
					idRows.Close()
					return nil, err
				}
				inPlan[itemID] = true
			}
			if err := idRows.Err(); err == nil {
				idRows.Close()
			} else {
				idRows.Close()
				return nil, err
			}
			deps := make([]models.ItemDependency, 0)
			if len(inPlan) == 0 {
				return deps, nil
			}
			rows, err := env.DB.QueryContext(ctx, `
				SELECT d.id, d.source_item_id, d.target_item_id, d.dep_type, d.created_by, d.created_at,
					si.workspace_id, sw.key || '-' || CAST(si.workspace_item_number AS TEXT), si.title,
					ti.workspace_id, tw.key || '-' || CAST(ti.workspace_item_number AS TEXT), ti.title
				FROM item_dependencies d
				JOIN items si ON si.id = d.source_item_id
				JOIN workspaces sw ON sw.id = si.workspace_id
				JOIN items ti ON ti.id = d.target_item_id
				JOIN workspaces tw ON tw.id = ti.workspace_id
				ORDER BY d.id`)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			for rows.Next() {
				var d models.ItemDependency
				var srcWS, tgtWS int
				var createdBy sql.NullInt64
				var createdAt time.Time
				if err := rows.Scan(&d.ID, &d.SourceItemID, &d.TargetItemID, &d.DepType, &createdBy, &createdAt,
					&srcWS, &d.SourceItemKey, &d.SourceItemTitle,
					&tgtWS, &d.TargetItemKey, &d.TargetItemTitle); err != nil {
					return nil, err
				}
				if !inPlan[d.SourceItemID] || !inPlan[d.TargetItemID] {
					continue
				}
				d.CreatedAt = createdAt
				deps = append(deps, d)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return deps, nil
		},
	})

	// ------------------------------------------------------------------------
	// add_plan_dependency
	// ------------------------------------------------------------------------
	Register(Default, Tool[addPlanDependencyArgs]{
		Name:        "add_plan_dependency",
		Group:       CapabilityPlans,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Add a dependency between two items in the same plan. source_item_id blocks (or is related to) target_item_id. Both items must belong to the plan.",
		Scopes:      []string{auth.ScopePlansWrite},
		Run: func(ctx context.Context, env *Env, args addPlanDependencyArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			if args.SourceItemID <= 0 || args.TargetItemID <= 0 {
				return map[string]string{"error": "source_item_id and target_item_id are required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			depType := args.DepType
			if depType == "" {
				depType = models.ItemDepBlocks
			}
			if depType != models.ItemDepBlocks && depType != models.ItemDepRelated {
				return map[string]string{"error": "dep_type must be 'blocks' or 'related'"}, nil
			}
			if args.SourceItemID == args.TargetItemID {
				return map[string]string{"error": "an item cannot depend on itself"}, nil
			}
			var exists int
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id IN (?, ?)`, args.SourceItemID, args.TargetItemID).Scan(&exists); err != nil {
				return nil, err
			}
			if exists < 2 {
				return map[string]string{"error": "one or both items not found"}, nil
			}
			var dup bool
			if err := env.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM item_dependencies WHERE source_item_id = ? AND target_item_id = ?)`, args.SourceItemID, args.TargetItemID).Scan(&dup); err != nil {
				return nil, err
			}
			if dup {
				return map[string]string{"error": "dependency already exists"}, nil
			}
			sourceSQL, sourceArgs, err := aiPlanSourceCondition(ctx, env.DB, args.PlanID)
			if err != nil {
				return nil, err
			}
			var inPlan int
			if err := env.DB.QueryRowContext(ctx, "SELECT COUNT(*) "+aiPlanItemFromClause+" WHERE i.merged_into_item_id IS NULL AND ("+sourceSQL+") AND i.id IN (?, ?)", append(sourceArgs, args.SourceItemID, args.TargetItemID)...).Scan(&inPlan); err != nil {
				return nil, err
			}
			if inPlan < 2 {
				return map[string]string{"error": "both items must belong to this plan"}, nil
			}
			res, err := env.DB.ExecContext(ctx,
				`INSERT INTO item_dependencies (source_item_id, target_item_id, dep_type, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
				args.SourceItemID, args.TargetItemID, depType, env.UserID, time.Now().UTC())
			if err != nil {
				return nil, err
			}
			dID, _ := res.LastInsertId()
			env.AuditWrite("item_dependency", int(dID), "add_plan_dependency", fmt.Sprintf("%d -> %d", args.SourceItemID, args.TargetItemID))
			return map[string]any{"id": int(dID), "source_item_id": args.SourceItemID, "target_item_id": args.TargetItemID, "dep_type": depType}, nil
		},
	})

	// ------------------------------------------------------------------------
	// remove_plan_dependency
	// ------------------------------------------------------------------------
	Register(Default, Tool[removePlanDependencyArgs]{
		Name:        "remove_plan_dependency",
		Group:       CapabilityPlans,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Remove a dependency between two items in a plan by dependency ID. Both items must belong to the plan.",
		Scopes:      []string{auth.ScopePlansWrite},
		Run: func(ctx context.Context, env *Env, args removePlanDependencyArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			if args.DependencyID <= 0 {
				return map[string]string{"error": "dependency_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			var srcID, tgtID int
			if err := env.DB.QueryRowContext(ctx, `SELECT source_item_id, target_item_id FROM item_dependencies WHERE id = ?`, args.DependencyID).Scan(&srcID, &tgtID); err != nil {
				return map[string]string{"error": "dependency not found"}, nil
			}
			sourceSQL, sourceArgs, err := aiPlanSourceCondition(ctx, env.DB, args.PlanID)
			if err != nil {
				return nil, err
			}
			var inPlan int
			if err := env.DB.QueryRowContext(ctx, "SELECT COUNT(*) "+aiPlanItemFromClause+" WHERE i.merged_into_item_id IS NULL AND ("+sourceSQL+") AND i.id IN (?, ?)", append(sourceArgs, srcID, tgtID)...).Scan(&inPlan); err != nil {
				return nil, err
			}
			if inPlan < 2 {
				return map[string]string{"error": "dependency not found"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `DELETE FROM item_dependencies WHERE id = ?`, args.DependencyID)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return map[string]string{"error": "dependency not found"}, nil
			}
			env.AuditWrite("item_dependency", args.DependencyID, "remove_plan_dependency", fmt.Sprintf("%d -> %d", srcID, tgtID))
			return map[string]any{"removed": true, "dependency_id": args.DependencyID}, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_plan_scenarios
	// ------------------------------------------------------------------------
	Register(Default, Tool[listPlanScenariosArgs]{
		Name:        "list_plan_scenarios",
		Group:       CapabilityPlans,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List the 'what if' scenarios for a plan, with their item-override counts.",
		Scopes:      []string{auth.ScopePlansRead},
		Run: func(ctx context.Context, env *Env, args listPlanScenariosArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			rows, err := env.DB.QueryContext(ctx, `
				SELECT s.id, s.plan_id, s.name, s.description, s.status, s.created_at, s.updated_at,
					COALESCE(c.cnt, 0)
				FROM plan_scenarios s
				LEFT JOIN (SELECT scenario_id, COUNT(*) AS cnt FROM plan_scenario_items GROUP BY scenario_id) c ON c.scenario_id = s.id
				WHERE s.plan_id = ?
				ORDER BY s.created_at DESC`, args.PlanID)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.PlanScenario, 0)
			for rows.Next() {
				var s models.PlanScenario
				if err := rows.Scan(&s.ID, &s.PlanID, &s.Name, &s.Description, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.ItemCount); err != nil {
					return nil, err
				}
				out = append(out, s)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// get_plan_scenario
	// ------------------------------------------------------------------------
	Register(Default, Tool[getPlanScenarioArgs]{
		Name:        "get_plan_scenario",
		Group:       CapabilityPlans,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get a plan scenario with its per-item date overrides. (Full dependency ripple impact is computed by the UI.)",
		Scopes:      []string{auth.ScopePlansRead},
		Run: func(ctx context.Context, env *Env, args getPlanScenarioArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			if args.ScenarioID <= 0 {
				return map[string]string{"error": "scenario_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			var s models.PlanScenario
			if err := env.DB.QueryRowContext(ctx, `SELECT id, plan_id, name, description, status, created_at, updated_at FROM plan_scenarios WHERE id = ?`, args.ScenarioID).
				Scan(&s.ID, &s.PlanID, &s.Name, &s.Description, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
				return map[string]string{"error": "scenario not found"}, nil
			}
			if s.PlanID != args.PlanID {
				return map[string]string{"error": "scenario not found"}, nil
			}
			rows, err := env.DB.QueryContext(ctx, `
				SELECT psi.id, psi.scenario_id, psi.item_id, psi.start_date, psi.end_date, psi.due_date, psi.notes,
					w.key || '-' || CAST(i.workspace_item_number AS TEXT), i.title
				FROM plan_scenario_items psi
				JOIN items i ON i.id = psi.item_id
				JOIN workspaces w ON w.id = i.workspace_id
				WHERE psi.scenario_id = ?
				ORDER BY i.workspace_item_number`, args.ScenarioID)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			items := make([]models.PlanScenarioItem, 0)
			for rows.Next() {
				var it models.PlanScenarioItem
				var startDate, endDate, dueDate sql.NullTime
				if err := rows.Scan(&it.ID, &it.ScenarioID, &it.ItemID, &startDate, &endDate, &dueDate, &it.Notes, &it.ItemKey, &it.ItemTitle); err != nil {
					return nil, err
				}
				if startDate.Valid {
					it.StartDate = new(startDate.Time)
				}
				if endDate.Valid {
					it.EndDate = new(endDate.Time)
				}
				if dueDate.Valid {
					it.DueDate = new(dueDate.Time)
				}
				items = append(items, it)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			s.ItemCount = len(items)
			return models.PlanScenarioDetail{Scenario: s, Items: items, Impact: models.PlanScenarioImpact{}}, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_plan_scenario
	// ------------------------------------------------------------------------
	Register(Default, Tool[createPlanScenarioArgs]{
		Name:        "create_plan_scenario",
		Group:       CapabilityPlans,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create a 'what if' scenario for a plan. New scenarios start as drafts.",
		Scopes:      []string{auth.ScopePlansWrite},
		Run: func(ctx context.Context, env *Env, args createPlanScenarioArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			name := strings.TrimSpace(args.Name)
			if name == "" {
				return map[string]string{"error": "name is required"}, nil
			}
			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx,
				`INSERT INTO plan_scenarios (plan_id, name, description, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
				args.PlanID, name, strings.TrimSpace(args.Description), models.PlanScenarioDraft, now, now)
			if err != nil {
				return nil, err
			}
			sID, _ := res.LastInsertId()
			env.AuditWrite("plan_scenario", int(sID), "create_plan_scenario", name)
			var s models.PlanScenario
			if err := env.DB.QueryRowContext(ctx, `SELECT id, plan_id, name, description, status, created_at, updated_at FROM plan_scenarios WHERE id = ?`, int(sID)).
				Scan(&s.ID, &s.PlanID, &s.Name, &s.Description, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
				return nil, err
			}
			return s, nil
		},
	})

	// ------------------------------------------------------------------------
	// update_plan_scenario
	// ------------------------------------------------------------------------
	Register(Default, Tool[updatePlanScenarioArgs]{
		Name:        "update_plan_scenario",
		Group:       CapabilityPlans,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Update a plan scenario's name, description, or status. Only provided fields change.",
		Scopes:      []string{auth.ScopePlansWrite},
		Run: func(ctx context.Context, env *Env, args updatePlanScenarioArgs) (any, error) {
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			if args.ScenarioID <= 0 {
				return map[string]string{"error": "scenario_id is required"}, nil
			}
			ok, err := aiRequirePlansView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			var existing models.PlanScenario
			if err := env.DB.QueryRowContext(ctx, `SELECT id, plan_id, name, description, status, created_at, updated_at FROM plan_scenarios WHERE id = ?`, args.ScenarioID).
				Scan(&existing.ID, &existing.PlanID, &existing.Name, &existing.Description, &existing.Status, &existing.CreatedAt, &existing.UpdatedAt); err != nil {
				return map[string]string{"error": "scenario not found"}, nil
			}
			if existing.PlanID != args.PlanID {
				return map[string]string{"error": "scenario not found"}, nil
			}
			name := existing.Name
			if args.Name != nil && strings.TrimSpace(*args.Name) != "" {
				name = strings.TrimSpace(*args.Name)
			}
			description := existing.Description
			if args.Description != nil {
				description = strings.TrimSpace(*args.Description)
			}
			status := existing.Status
			if args.Status != nil {
				switch *args.Status {
				case models.PlanScenarioDraft, models.PlanScenarioActive, models.PlanScenarioArchived:
					status = *args.Status
				default:
					return map[string]string{"error": "status must be 'draft', 'active' or 'archived'"}, nil
				}
			}
			now := time.Now().UTC()
			if _, err := env.DB.ExecContext(ctx, `UPDATE plan_scenarios SET name = ?, description = ?, status = ?, updated_at = ? WHERE id = ?`,
				name, description, status, now, args.ScenarioID); err != nil {
				return nil, err
			}
			env.AuditWrite("plan_scenario", args.ScenarioID, "update_plan_scenario", name)
			var s models.PlanScenario
			if err := env.DB.QueryRowContext(ctx, `SELECT id, plan_id, name, description, status, created_at, updated_at FROM plan_scenarios WHERE id = ?`, args.ScenarioID).
				Scan(&s.ID, &s.PlanID, &s.Name, &s.Description, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
				return nil, err
			}
			return s, nil
		},
	})
}

// planEntityLink is one P3M entity to link to a plan.
type planEntityLink struct {
	EntityType string `json:"entity_type" jsonschema:"One of: portfolio, programme, project."`
	EntityID   int    `json:"entity_id" jsonschema:"The P3M entity's ID."`
}

func aiSetPlanLinks(ctx context.Context, db database.Database, planID int, workspaceIDs []int, p3mEntities []planEntityLink, stakeholderIDs []int) error {
	now := time.Now().UTC()
	for _, wsID := range workspaceIDs {
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO plan_workspaces (plan_id, workspace_id, created_at) VALUES (?, ?, ?)`, planID, wsID, now); err != nil {
			return err
		}
	}
	for _, e := range p3mEntities {
		if e.EntityType == "" || e.EntityID <= 0 {
			continue
		}
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO plan_p3m_entities (plan_id, entity_type, entity_id, created_at) VALUES (?, ?, ?, ?)`, planID, e.EntityType, e.EntityID, now); err != nil {
			return err
		}
	}
	for _, shID := range stakeholderIDs {
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO plan_stakeholders (plan_id, stakeholder_id, created_at) VALUES (?, ?, ?)`, planID, shID, now); err != nil {
			return err
		}
	}
	return nil
}

func nullIntPtr(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullTimePtr(v *time.Time) any {
	if v == nil {
		return nil
	}
	return *v
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func makeIntSlice(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// args structs for plan tools (declared at package scope for schema generation).
type getPlanArgs struct {
	PlanID int `json:"plan_id" jsonschema:"The plan's numeric ID."`
}

type createPlanArgs struct {
	Name           string           `json:"name" jsonschema:"Plan name (required)."`
	Description    string           `json:"description,omitempty" jsonschema:"Plan description."`
	Status         string           `json:"status,omitempty" jsonschema:"Status: draft, active, completed, archived (default active)."`
	Color          string           `json:"color,omitempty" jsonschema:"Display color."`
	OwnerID        *int             `json:"owner_id,omitempty" jsonschema:"Owner user ID."`
	StartDate      string           `json:"start_date,omitempty" jsonschema:"Start date (YYYY-MM-DD or RFC 3339)."`
	EndDate        string           `json:"end_date,omitempty" jsonschema:"End date (YYYY-MM-DD or RFC 3339)."`
	WorkspaceIDs   []int            `json:"workspace_ids,omitempty" jsonschema:"Workspace IDs to link."`
	P3MEntities    []planEntityLink `json:"p3m_entities,omitempty" jsonschema:"P3M entities to link (portfolio/programme/project)."`
	StakeholderIDs []int            `json:"stakeholder_ids,omitempty" jsonschema:"SMS stakeholder IDs to link."`
}

type updatePlanArgs struct {
	PlanID      int     `json:"plan_id" jsonschema:"The plan's numeric ID."`
	Name        string  `json:"name,omitempty" jsonschema:"New name (omit to keep)."`
	Description string  `json:"description,omitempty" jsonschema:"New description."`
	Status      string  `json:"status,omitempty" jsonschema:"New status: draft, active, completed, archived."`
	Color       string  `json:"color,omitempty" jsonschema:"New color."`
	OwnerID     *int    `json:"owner_id,omitempty" jsonschema:"New owner user ID."`
	StartDate   *string `json:"start_date,omitempty" jsonschema:"New start date (YYYY-MM-DD)."`
	EndDate     *string `json:"end_date,omitempty" jsonschema:"New end date (YYYY-MM-DD)."`
}

type setPlanWorkspacesArgs struct {
	PlanID       int   `json:"plan_id" jsonschema:"The plan's numeric ID."`
	WorkspaceIDs []int `json:"workspace_ids" jsonschema:"The complete desired list of workspace IDs (full replace)."`
}

type listPlanItemsArgs struct {
	PlanID  int    `json:"plan_id" jsonschema:"The plan's numeric ID."`
	Search  string `json:"search,omitempty" jsonschema:"Filter by title (substring)."`
	Type    string `json:"type,omitempty" jsonschema:"Filter by item type name."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Max items (default 200, max 1000)."`
	Offset  int    `json:"offset,omitempty" jsonschema:"Offset for pagination."`
}

type getPlanSummaryArgs struct {
	PlanID int `json:"plan_id" jsonschema:"The plan's numeric ID."`
}

type listPlanDependenciesArgs struct {
	PlanID int `json:"plan_id" jsonschema:"The plan's numeric ID."`
}

type addPlanDependencyArgs struct {
	PlanID       int    `json:"plan_id" jsonschema:"The plan's numeric ID."`
	SourceItemID int    `json:"source_item_id" jsonschema:"The item that blocks/is related to the target."`
	TargetItemID int    `json:"target_item_id" jsonschema:"The blocked/related item."`
	DepType      string `json:"dep_type,omitempty" jsonschema:"Dependency type: blocks (default) or related."`
}

type removePlanDependencyArgs struct {
	PlanID       int `json:"plan_id" jsonschema:"The plan's numeric ID."`
	DependencyID int `json:"dependency_id" jsonschema:"The dependency's numeric ID (from list_plan_dependencies)."`
}

type listPlanScenariosArgs struct {
	PlanID int `json:"plan_id" jsonschema:"The plan's numeric ID."`
}

type getPlanScenarioArgs struct {
	PlanID     int `json:"plan_id" jsonschema:"The plan's numeric ID."`
	ScenarioID int `json:"scenario_id" jsonschema:"The scenario's numeric ID."`
}

type createPlanScenarioArgs struct {
	PlanID      int    `json:"plan_id" jsonschema:"The plan's numeric ID."`
	Name        string `json:"name" jsonschema:"Scenario name (required)."`
	Description string `json:"description,omitempty" jsonschema:"Scenario description."`
}

type updatePlanScenarioArgs struct {
	PlanID      int     `json:"plan_id" jsonschema:"The plan's numeric ID."`
	ScenarioID  int     `json:"scenario_id" jsonschema:"The scenario's numeric ID."`
	Name        *string `json:"name,omitempty" jsonschema:"New name."`
	Description *string `json:"description,omitempty" jsonschema:"New description."`
	Status      *string `json:"status,omitempty" jsonschema:"New status: draft, active, archived."`
}
