package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"windshift/internal/cql"
	"windshift/internal/models"
	"windshift/internal/repository"
)

// ---------------------------------------------------------------------------
// Plan item sources: linked workspaces + CQL filter queries
// ---------------------------------------------------------------------------

// planSourceCondition builds the WHERE fragment selecting every item that
// belongs to a plan: items in linked workspaces OR matching any of the plan's
// named CQL filters. The returned SQL references only aliases from
// repository.ItemListFilterFromClause().
func (h *PlanHandler) planSourceCondition(ctx context.Context, planID int) (string, []any, error) {
	var wsIDs []int
	rows, err := h.db.QueryContext(ctx, `SELECT workspace_id FROM plan_workspaces WHERE plan_id = ?`, planID)
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
	fRows, err := h.db.QueryContext(ctx, `SELECT cql_query FROM plan_filters WHERE plan_id = ? ORDER BY id`, planID)
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
		wsMap, err := repository.NewWorkspaceRepository(h.db).BuildWorkspaceMap()
		if err != nil {
			return "", nil, fmt.Errorf("build workspace map: %w", err)
		}
		cfMap, err := repository.NewItemRepository(h.db).GetCQLCustomFieldMap()
		if err != nil {
			return "", nil, fmt.Errorf("load custom field map: %w", err)
		}
		evaluator := cql.NewEvaluator(wsMap, cfMap, h.db.GetDriverName())
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

// planItemSelect is the shared column list for plan item queries. It must be
// paired with planItemFromClause and scanned via scanPlanItem.
const planItemSelect = `
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

// planItemFromClause extends the shared CQL alias set with teams and users so
// any valid filter fragment resolves in every plan query.
var planItemFromClause = repository.ItemListFilterFromClause() + `
	LEFT JOIN teams tm ON i.team_id = tm.id
	LEFT JOIN users u ON i.assignee_id = u.id`

func scanPlanItem(row interface{ Scan(...any) error }) (models.PlanItem, error) {
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

// planItemSortColumns whitelists server-side sort fields for the list view.
var planItemSortColumns = map[string]string{
	"key":      "w.name COLLATE NOCASE, i.workspace_item_number",
	"title":    "i.title COLLATE NOCASE",
	"status":   "st.name COLLATE NOCASE",
	"type":     "it.name COLLATE NOCASE",
	"team":     "tm.name COLLATE NOCASE",
	"assignee": "u.first_name COLLATE NOCASE, u.last_name COLLATE NOCASE",
	"start_date": "i.start_date IS NULL, i.start_date",
	"end_date":   "i.end_date IS NULL, i.end_date",
	"due_date":   "i.due_date IS NULL, i.due_date",
}

// ListPlanItems handles GET /api/plans/{id}/items. Query params: search, type,
// sort (key|title|status|type|team|assignee|start_date|end_date|due_date),
// limit (default 200, max 1000), offset.
func (h *PlanHandler) ListPlanItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	sourceSQL, sourceArgs, err := h.planSourceCondition(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	where := "i.merged_into_item_id IS NULL AND (" + sourceSQL + ")"
	args := append([]any{}, sourceArgs...)
	if s := strings.TrimSpace(r.URL.Query().Get("search")); s != "" {
		where += " AND i.title LIKE ? ESCAPE '\\'"
		args = append(args, "%"+escapeLike(s)+"%")
	}
	if t := strings.TrimSpace(r.URL.Query().Get("type")); t != "" {
		where += " AND it.name = ?"
		args = append(args, t)
	}

	sortField := r.URL.Query().Get("sort")
	orderSQL, okSort := planItemSortColumns[sortField]
	if !okSort {
		orderSQL = planItemSortColumns["key"]
	}

	limit := 200
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	var total int
	countArgs := append([]any{}, args...)
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) "+planItemFromClause+" WHERE "+where, countArgs...).Scan(&total); err != nil {
		respondInternalError(w, r, fmt.Errorf("count plan items: %w", err))
		return
	}

	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := h.db.QueryContext(ctx, "SELECT "+planItemSelect+" "+planItemFromClause+" WHERE "+where+" ORDER BY "+orderSQL+" LIMIT ? OFFSET ?", pageArgs...)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("list plan items: %w", err))
		return
	}
	defer rows.Close()

	items := make([]models.PlanItem, 0)
	for rows.Next() {
		item, err := scanPlanItem(rows)
		if err != nil {
			respondInternalError(w, r, fmt.Errorf("scan plan item: %w", err))
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, map[string]any{"items": items, "total": total})
}

// escapeLike escapes LIKE wildcards for user-supplied search terms.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ---------------------------------------------------------------------------
// Plan summary (aggregate metrics for the Summary view)
// ---------------------------------------------------------------------------

// GetPlanSummary handles GET /api/plans/{id}/summary.
func (h *PlanHandler) GetPlanSummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	sourceSQL, sourceArgs, err := h.planSourceCondition(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	where := "i.merged_into_item_id IS NULL AND (" + sourceSQL + ")"

	summary := models.PlanSummary{ByStatus: []models.PlanCount{}, ByType: []models.PlanCount{}, ByWorkspace: []models.PlanWorkspaceStat{}, Upcoming: []models.PlanItem{}}

	var done int
	countArgs := append([]any{}, sourceArgs...)
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(CASE WHEN sc.is_completed THEN 1 ELSE 0 END), 0) "+planItemFromClause+" WHERE "+where, countArgs...).Scan(&summary.TotalItems, &done); err != nil {
		respondInternalError(w, r, fmt.Errorf("summarize plan items: %w", err))
		return
	}
	summary.DoneItems = done
	if summary.TotalItems > 0 {
		summary.ProgressPct = float64(done) / float64(summary.TotalItems) * 100
	}

	statusArgs := append([]any{}, sourceArgs...)
	sRows, err := h.db.QueryContext(ctx, `
		SELECT COALESCE(sc.name, 'Uncategorized'), COALESCE(sc.color, ''), COUNT(*)
		`+planItemFromClause+` WHERE `+where+`
		GROUP BY sc.id ORDER BY COUNT(*) DESC`, statusArgs...)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("summarize by status: %w", err))
		return
	}
	for sRows.Next() {
		var c models.PlanCount
		if err := sRows.Scan(&c.Name, &c.Color, &c.Count); err != nil {
			sRows.Close()
			respondInternalError(w, r, err)
			return
		}
		summary.ByStatus = append(summary.ByStatus, c)
	}
	if err := sRows.Err(); err == nil {
		sRows.Close()
	} else {
		sRows.Close()
		respondInternalError(w, r, err)
		return
	}

	typeArgs := append([]any{}, sourceArgs...)
	tRows, err := h.db.QueryContext(ctx, "SELECT COALESCE(it.name, 'Unknown'), COUNT(*) "+planItemFromClause+" WHERE "+where+" GROUP BY it.id ORDER BY COUNT(*) DESC", typeArgs...)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("summarize by type: %w", err))
		return
	}
	for tRows.Next() {
		var c models.PlanCount
		if err := tRows.Scan(&c.Name, &c.Count); err != nil {
			tRows.Close()
			respondInternalError(w, r, err)
			return
		}
		summary.ByType = append(summary.ByType, c)
	}
	if err := tRows.Err(); err == nil {
		tRows.Close()
	} else {
		tRows.Close()
		respondInternalError(w, r, err)
		return
	}

	wsArgs := append([]any{}, sourceArgs...)
	wRows, err := h.db.QueryContext(ctx, `
		SELECT w.id, w.key, w.name, COUNT(*), COALESCE(SUM(CASE WHEN sc.is_completed THEN 1 ELSE 0 END), 0)
		`+planItemFromClause+` WHERE `+where+`
		GROUP BY w.id ORDER BY w.name COLLATE NOCASE`, wsArgs...)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("summarize by workspace: %w", err))
		return
	}
	for wRows.Next() {
		var s models.PlanWorkspaceStat
		if err := wRows.Scan(&s.WorkspaceID, &s.WorkspaceKey, &s.WorkspaceName, &s.ItemCount, &s.DoneCount); err != nil {
			wRows.Close()
			respondInternalError(w, r, err)
			return
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
		respondInternalError(w, r, err)
		return
	}

	// Upcoming: open items due within the next 14 days.
	now := time.Now().UTC()
	horizon := now.AddDate(0, 0, 14)
	upArgs := append(append([]any{}, sourceArgs...), now, horizon)
	uRows, err := h.db.QueryContext(ctx, "SELECT "+planItemSelect+" "+planItemFromClause+` WHERE `+where+`
		AND i.due_date IS NOT NULL AND i.due_date >= ? AND i.due_date <= ?
		AND (sc.is_completed IS NULL OR sc.is_completed = 0)
		ORDER BY i.due_date LIMIT 25`, upArgs...)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("summarize upcoming: %w", err))
		return
	}
	for uRows.Next() {
		item, err := scanPlanItem(uRows)
		if err != nil {
			uRows.Close()
			respondInternalError(w, r, err)
			return
		}
		summary.Upcoming = append(summary.Upcoming, item)
	}
	if err := uRows.Err(); err == nil {
		uRows.Close()
	} else {
		uRows.Close()
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, summary)
}

// ---------------------------------------------------------------------------
// Plan filters (named CQL sources)
// ---------------------------------------------------------------------------

func (h *PlanHandler) listPlanFilters(ctx context.Context, planID int) ([]models.PlanFilter, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, plan_id, name, cql_query, created_at FROM plan_filters WHERE plan_id = ? ORDER BY id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.PlanFilter, 0)
	for rows.Next() {
		var f models.PlanFilter
		var createdAt time.Time
		if err := rows.Scan(&f.ID, &f.PlanID, &f.Name, &f.CQLQuery, &createdAt); err != nil {
			return nil, err
		}
		f.CreatedAt = createdAt
		out = append(out, f)
	}
	return out, rows.Err()
}

// AddPlanFilter handles POST /api/plans/{id}/filters with body
// { "name": "...", "cql_query": "..." }. The CQL query is validated before
// persisting so a plan can never be left with an unevaluable source.
func (h *PlanHandler) AddPlanFilter(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name     string `json:"name"`
		CQLQuery string `json:"cql_query"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.CQLQuery = strings.TrimSpace(in.CQLQuery)
	if in.Name == "" || in.CQLQuery == "" {
		respondValidationError(w, r, "name and cql_query are required")
		return
	}

	if err := h.validateCQLQuery(in.CQLQuery); err != nil {
		respondValidationError(w, r, "cql_query is invalid: "+err.Error())
		return
	}

	var dup int
	err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM plan_filters WHERE plan_id = ? AND name = ?`, id, in.Name).Scan(&dup)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("check filter name: %w", err))
		return
	}
	if dup > 0 {
		respondValidationError(w, r, "a filter named '"+in.Name+"' already exists")
		return
	}

	res, err := h.db.ExecContext(r.Context(),
		`INSERT INTO plan_filters (plan_id, name, cql_query, created_at) VALUES (?, ?, ?, ?)`,
		id, in.Name, in.CQLQuery, time.Now().UTC())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			respondValidationError(w, r, "a filter named '"+in.Name+"' already exists")
			return
		}
		respondInternalError(w, r, fmt.Errorf("create plan filter: %w", err))
		return
	}
	fID, _ := res.LastInsertId()
	out, err := h.getPlanFilterByID(r.Context(), int(fID))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *PlanHandler) getPlanFilterByID(ctx context.Context, filterID int) (*models.PlanFilter, error) {
	var f models.PlanFilter
	var createdAt time.Time
	err := h.db.QueryRowContext(ctx, `SELECT id, plan_id, name, cql_query, created_at FROM plan_filters WHERE id = ?`, filterID).
		Scan(&f.ID, &f.PlanID, &f.Name, &f.CQLQuery, &createdAt)
	if err != nil {
		return nil, err
	}
	f.CreatedAt = createdAt
	return &f, nil
}

// RemovePlanFilter handles DELETE /api/plans/{id}/filters/{filter_id}.
func (h *PlanHandler) RemovePlanFilter(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	filterID, ok := requireIDParam(w, r, "filter_id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM plan_filters WHERE id = ? AND plan_id = ?`, filterID, id)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("delete plan filter: %w", err))
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "plan_filter")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateCQLQuery compiles the expression against the current workspace and
// custom field catalog so a plan can never be left with an unevaluable source.
func (h *PlanHandler) validateCQLQuery(query string) error {
	wsMap, err := repository.NewWorkspaceRepository(h.db).BuildWorkspaceMap()
	if err != nil {
		return fmt.Errorf("build workspace map: %w", err)
	}
	cfMap, err := repository.NewItemRepository(h.db).GetCQLCustomFieldMap()
	if err != nil {
		return fmt.Errorf("load custom field map: %w", err)
	}
	evaluator := cql.NewEvaluator(wsMap, cfMap, h.db.GetDriverName())
	_, _, err = evaluator.EvaluateToSQL(query)
	return err
}

// ListPlanFilters handles GET /api/plans/{id}/filters.
func (h *PlanHandler) ListPlanFilters(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	filters, err := h.listPlanFilters(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("list plan filters: %w", err))
		return
	}
	respondJSONOK(w, filters)
}

// UpdatePlanFilter handles PUT /api/plans/{id}/filters/{filter_id}. Either or
// both fields may be provided; the resulting pair must be non-empty and the CQL
// must compile.
func (h *PlanHandler) UpdatePlanFilter(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	filterID, ok := requireIDParam(w, r, "filter_id")
	if !ok {
		return
	}
	var in struct {
		Name     *string `json:"name"`
		CQLQuery *string `json:"cql_query"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	f, err := h.getPlanFilterByID(r.Context(), filterID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondNotFound(w, r, "plan_filter")
			return
		}
		respondInternalError(w, r, fmt.Errorf("load plan filter: %w", err))
		return
	}
	if f.PlanID != id {
		respondNotFound(w, r, "plan_filter")
		return
	}
	name := f.Name
	cqlQuery := f.CQLQuery
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.CQLQuery != nil {
		cqlQuery = strings.TrimSpace(*in.CQLQuery)
	}
	if name == "" || cqlQuery == "" {
		respondValidationError(w, r, "name and cql_query are required")
		return
	}
	if err := h.validateCQLQuery(cqlQuery); err != nil {
		respondValidationError(w, r, "cql_query is invalid: "+err.Error())
		return
	}
	if _, err := h.db.ExecContext(r.Context(), `UPDATE plan_filters SET name = ?, cql_query = ? WHERE id = ?`, name, cqlQuery, filterID); err != nil {
		respondInternalError(w, r, fmt.Errorf("update plan filter: %w", err))
		return
	}
	out, err := h.getPlanFilterByID(r.Context(), filterID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// ---------------------------------------------------------------------------
// Item dependencies within a plan
// ---------------------------------------------------------------------------

// ListPlanDependencies handles GET /api/plans/{id}/dependencies: every
// dependency edge whose both endpoints are items of this plan.
func (h *PlanHandler) ListPlanDependencies(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	sourceSQL, sourceArgs, err := h.planSourceCondition(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	where := "i.merged_into_item_id IS NULL AND (" + sourceSQL + ")"

	idRows, err := h.db.QueryContext(ctx, "SELECT i.id "+planItemFromClause+" WHERE "+where, append([]any{}, sourceArgs...)...)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("collect plan item ids: %w", err))
		return
	}
	inPlan := make(map[int]bool)
	for idRows.Next() {
		var itemID int
		if err := idRows.Scan(&itemID); err != nil {
			idRows.Close()
			respondInternalError(w, r, err)
			return
		}
		inPlan[itemID] = true
	}
	if err := idRows.Err(); err == nil {
		idRows.Close()
	} else {
		idRows.Close()
		respondInternalError(w, r, err)
		return
	}

	deps := make([]models.ItemDependency, 0)
	if len(inPlan) == 0 {
		respondJSONOK(w, deps)
		return
	}

	rows, err := h.db.QueryContext(ctx, `
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
		respondInternalError(w, r, fmt.Errorf("list dependencies: %w", err))
		return
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
			respondInternalError(w, r, fmt.Errorf("scan dependency: %w", err))
			return
		}
		if !inPlan[d.SourceItemID] || !inPlan[d.TargetItemID] {
			continue
		}
		d.CreatedAt = createdAt
		deps = append(deps, d)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, deps)
}

// AddItemDependency handles POST /api/plans/{id}/items/{item_id}/dependencies
// with body { "target_item_id": N, "dep_type": "blocks" }.
func (h *PlanHandler) AddItemDependency(w http.ResponseWriter, r *http.Request) {
	authUser, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	itemID, ok := requireIDParam(w, r, "item_id")
	if !ok {
		return
	}
	var in struct {
		TargetItemID int    `json:"target_item_id"`
		DepType      string `json:"dep_type"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.DepType == "" {
		in.DepType = models.ItemDepBlocks
	}
	switch in.DepType {
	case models.ItemDepBlocks, models.ItemDepRelated:
	default:
		respondValidationError(w, r, "dep_type must be 'blocks' or 'related'")
		return
	}
	if in.TargetItemID == itemID {
		respondValidationError(w, r, "an item cannot depend on itself")
		return
	}

	ctx := r.Context()
	var exists int
	err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id IN (?, ?)`, itemID, in.TargetItemID).Scan(&exists)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("verify dependency endpoints: %w", err))
		return
	}
	if exists < 2 {
		respondNotFound(w, r, "item")
		return
	}

	var dup bool
	err = h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM item_dependencies WHERE source_item_id = ? AND target_item_id = ?)`, itemID, in.TargetItemID).Scan(&dup)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("check duplicate dependency: %w", err))
		return
	}
	if dup {
		respondValidationError(w, r, "dependency already exists")
		return
	}

	sourceSQL, sourceArgs, err := h.planSourceCondition(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var inPlan int
	// The CQL filter fragments in sourceSQL reference the shared alias set
	// (st, it, pri, ...), so the membership count needs the full filter
	// FROM clause, not a bare "FROM items i".
	err = h.db.QueryRowContext(ctx, "SELECT COUNT(*) "+repository.ItemListFilterFromClause()+" WHERE i.merged_into_item_id IS NULL AND ("+sourceSQL+") AND i.id IN (?, ?)", append(sourceArgs, itemID, in.TargetItemID)...).Scan(&inPlan)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("verify plan membership: %w", err))
		return
	}
	if inPlan < 2 {
		respondValidationError(w, r, "both items must belong to this plan")
		return
	}

	res, err := h.db.ExecContext(ctx,
		`INSERT INTO item_dependencies (source_item_id, target_item_id, dep_type, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		itemID, in.TargetItemID, in.DepType, authUser.ID, time.Now().UTC())
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("create dependency: %w", err))
		return
	}
	dID, _ := res.LastInsertId()

	var d models.ItemDependency
	var srcWS, tgtWS int
	var createdBy sql.NullInt64
	var createdAt time.Time
	err = h.db.QueryRowContext(ctx, `
		SELECT d.id, d.source_item_id, d.target_item_id, d.dep_type, d.created_by, d.created_at,
			si.workspace_id, sw.key || '-' || CAST(si.workspace_item_number AS TEXT), si.title,
			ti.workspace_id, tw.key || '-' || CAST(ti.workspace_item_number AS TEXT), ti.title
		FROM item_dependencies d
		JOIN items si ON si.id = d.source_item_id
		JOIN workspaces sw ON sw.id = si.workspace_id
		JOIN items ti ON ti.id = d.target_item_id
		JOIN workspaces tw ON tw.id = ti.workspace_id
		WHERE d.id = ?`, dID).Scan(
		&d.ID, &d.SourceItemID, &d.TargetItemID, &d.DepType, &createdBy, &createdAt,
		&srcWS, &d.SourceItemKey, &d.SourceItemTitle,
		&tgtWS, &d.TargetItemKey, &d.TargetItemTitle)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("load created dependency: %w", err))
		return
	}
	d.CreatedAt = createdAt
	respondJSONOK(w, d)
}

// RemoveItemDependency handles DELETE /api/plans/{id}/items/dependencies/{dep_id}.
func (h *PlanHandler) RemoveItemDependency(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	depID, ok := requireIDParam(w, r, "dep_id")
	if !ok {
		return
	}
	ctx := r.Context()

	var srcID, tgtID int
	err := h.db.QueryRowContext(ctx, `SELECT source_item_id, target_item_id FROM item_dependencies WHERE id = ?`, depID).Scan(&srcID, &tgtID)
	if err != nil {
		if isNotFound(err) || err == sql.ErrNoRows {
			respondNotFound(w, r, "dependency")
			return
		}
		respondInternalError(w, r, fmt.Errorf("load dependency: %w", err))
		return
	}

	sourceSQL, sourceArgs, err := h.planSourceCondition(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var inPlan int
	err = h.db.QueryRowContext(ctx, "SELECT COUNT(*) "+repository.ItemListFilterFromClause()+" WHERE i.merged_into_item_id IS NULL AND ("+sourceSQL+") AND i.id IN (?, ?)", append(sourceArgs, srcID, tgtID)...).Scan(&inPlan)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("verify plan membership: %w", err))
		return
	}
	if inPlan < 2 {
		respondNotFound(w, r, "dependency")
		return
	}

	res, err := h.db.ExecContext(ctx, `DELETE FROM item_dependencies WHERE id = ?`, depID)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("delete dependency: %w", err))
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "dependency")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Teams workload + capacity for the Teams view
// ---------------------------------------------------------------------------

// GetPlanTeams handles GET /api/plans/{id}/teams: per-team workload within the
// plan plus configured capacity and overload detection.
func (h *PlanHandler) GetPlanTeams(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	sourceSQL, sourceArgs, err := h.planSourceCondition(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	where := "i.merged_into_item_id IS NULL AND (" + sourceSQL + ")"

	tRows, err := h.db.QueryContext(ctx, `
		SELECT t.id, t.name, COALESCE(t.color, ''),
			COALESCE(mc.c, 0)
		FROM teams t
		LEFT JOIN (SELECT team_id, COUNT(*) AS c FROM team_members GROUP BY team_id) mc ON mc.team_id = t.id
		WHERE t.is_active
		ORDER BY t.name COLLATE NOCASE`)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("list teams: %w", err))
		return
	}
	type teamBase struct {
		id          int
		name        string
		color       string
		memberCount int
	}
	var teams []teamBase
	for tRows.Next() {
		var tb teamBase
		if err := tRows.Scan(&tb.id, &tb.name, &tb.color, &tb.memberCount); err != nil {
			tRows.Close()
			respondInternalError(w, r, err)
			return
		}
		teams = append(teams, tb)
	}
	if err := tRows.Err(); err == nil {
		tRows.Close()
	} else {
		tRows.Close()
		respondInternalError(w, r, err)
		return
	}

	out := make([]models.PlanTeamStat, 0, len(teams))
	for _, tb := range teams {
		stat := models.PlanTeamStat{
			TeamID:      tb.id,
			Name:        tb.name,
			Color:       tb.color,
			MemberCount: tb.memberCount,
			Members:     []models.PlanTeamMember{},
		}

		mRows, err := h.db.QueryContext(ctx, `SELECT user_id FROM team_members WHERE team_id = ?`, tb.id)
		if err != nil {
			respondInternalError(w, r, fmt.Errorf("list team members: %w", err))
			return
		}
		var memberIDs []int
		for mRows.Next() {
			var uid int
			if err := mRows.Scan(&uid); err != nil {
				mRows.Close()
				respondInternalError(w, r, err)
				return
			}
			memberIDs = append(memberIDs, uid)
		}
		if err := mRows.Err(); err == nil {
			mRows.Close()
		} else {
			mRows.Close()
			respondInternalError(w, r, err)
			return
		}

		// Workload: items in the plan assigned to the team or any member.
		wArgs := append(append([]any{}, sourceArgs...), tb.id)
		if len(memberIDs) > 0 {
			ph := make([]string, len(memberIDs))
			for i, uid := range memberIDs {
				ph[i] = "?"
				wArgs = append(wArgs, uid)
			}
			workloadSQL := "SELECT COUNT(*), COALESCE(SUM(CASE WHEN sc.is_completed IS NULL OR sc.is_completed = 0 THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN sc.is_completed IS NULL OR sc.is_completed = 0 THEN i.estimate_minutes ELSE 0 END), 0), COALESCE(SUM(CASE WHEN sc.is_completed IS NULL OR sc.is_completed = 0 THEN i.story_points ELSE 0 END), 0) " + planItemFromClause + " WHERE " + where + " AND (i.team_id = ?"
			if len(ph) > 0 {
				workloadSQL += " OR i.assignee_id IN (" + strings.Join(ph, ",") + ")"
			}
			workloadSQL += ")"
			var openEstimate float64
			err := h.db.QueryRowContext(ctx, workloadSQL, wArgs...).Scan(&stat.ItemCount, &stat.OpenItemCount, &openEstimate, &stat.TotalStoryPoints)
			if err != nil {
				respondInternalError(w, r, fmt.Errorf("compute team workload: %w", err))
				return
			}
			stat.TotalEstimateMinutes = int64(openEstimate)
		} else {
			var openEstimate float64
			err := h.db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(CASE WHEN sc.is_completed IS NULL OR sc.is_completed = 0 THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN sc.is_completed IS NULL OR sc.is_completed = 0 THEN i.estimate_minutes ELSE 0 END), 0), COALESCE(SUM(CASE WHEN sc.is_completed IS NULL OR sc.is_completed = 0 THEN i.story_points ELSE 0 END), 0) "+planItemFromClause+" WHERE "+where+" AND i.team_id = ?", append(append([]any{}, sourceArgs...), tb.id)...).Scan(&stat.ItemCount, &stat.OpenItemCount, &openEstimate, &stat.TotalStoryPoints)
			if err != nil {
				respondInternalError(w, r, fmt.Errorf("compute team workload: %w", err))
				return
			}
			stat.TotalEstimateMinutes = int64(openEstimate)
		}

		// Per-member open item counts.
		for _, uid := range memberIDs {
			var m models.PlanTeamMember
			m.UserID = uid
			err := h.db.QueryRowContext(ctx, `SELECT COALESCE(first_name || ' ' || last_name, ''), COALESCE(avatar_url, '') FROM users WHERE id = ?`, uid).Scan(&m.Name, &m.AvatarURL)
			if err != nil {
				respondInternalError(w, r, fmt.Errorf("load team member: %w", err))
				return
			}
			err = h.db.QueryRowContext(ctx, "SELECT COUNT(*) "+planItemFromClause+" WHERE "+where+" AND i.assignee_id = ? AND (sc.is_completed IS NULL OR sc.is_completed = 0)", append(append([]any{}, sourceArgs...), uid)...).Scan(&m.ItemCount)
			if err != nil {
				respondInternalError(w, r, fmt.Errorf("count member items: %w", err))
				return
			}
			stat.Members = append(stat.Members, m)
		}

		// Capacity + overload.
		var unit sql.NullString
		var weekly sql.NullFloat64
		err = h.db.QueryRowContext(ctx, `SELECT unit, weekly_value FROM team_capacities WHERE team_id = ?`, tb.id).Scan(&unit, &weekly)
		if err == nil && unit.Valid {
			stat.CapacityUnit = unit.String
			stat.WeeklyCapacity = new(float64(weekly.Float64))
			switch unit.String {
			case "hours":
				stat.Overloaded = float64(stat.TotalEstimateMinutes)/60 > weekly.Float64
			case "days":
				stat.Overloaded = float64(stat.TotalEstimateMinutes)/1440 > weekly.Float64
			case "points":
				stat.Overloaded = stat.TotalStoryPoints > weekly.Float64
			}
		} else if err != sql.ErrNoRows {
			respondInternalError(w, r, fmt.Errorf("load team capacity: %w", err))
			return
		}

		out = append(out, stat)
	}

	respondJSONOK(w, models.PlanTeams{Teams: out})
}

// SetTeamCapacity handles PUT /api/plans/{id}/teams/{team_id}/capacity with
// body { "unit": "points|days|hours", "weekly_value": 40 }. Upserts the team's
// capacity; passing weekly_value null clears it.
func (h *PlanHandler) SetTeamCapacity(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	// The plan id namespaces the URL; capacity is a property of the team.
	if _, ok := requireIDParam(w, r, "id"); !ok {
		return
	}
	teamID, ok := requireIDParam(w, r, "team_id")
	if !ok {
		return
	}
	var in struct {
		Unit        string   `json:"unit"`
		WeeklyValue *float64 `json:"weekly_value"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	var teamName string
	err := h.db.QueryRowContext(r.Context(), `SELECT name FROM teams WHERE id = ? AND is_active`, teamID).Scan(&teamName)
	if err != nil {
		if isNotFound(err) || err == sql.ErrNoRows {
			respondNotFound(w, r, "team")
			return
		}
		respondInternalError(w, r, fmt.Errorf("load team: %w", err))
		return
	}

	if in.WeeklyValue == nil {
		if _, err := h.db.ExecContext(r.Context(), `DELETE FROM team_capacities WHERE team_id = ?`, teamID); err != nil {
			respondInternalError(w, r, fmt.Errorf("clear team capacity: %w", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch in.Unit {
	case "points", "days", "hours":
	default:
		respondValidationError(w, r, "unit must be 'points', 'days' or 'hours'")
		return
	}
	if *in.WeeklyValue < 0 {
		respondValidationError(w, r, "weekly_value cannot be negative")
		return
	}

	if _, err := h.db.ExecContext(r.Context(), `
		INSERT INTO team_capacities (team_id, unit, weekly_value) VALUES (?, ?, ?)
		ON CONFLICT(team_id) DO UPDATE SET unit = excluded.unit, weekly_value = excluded.weekly_value`,
		teamID, in.Unit, *in.WeeklyValue); err != nil {
		respondInternalError(w, r, fmt.Errorf("save team capacity: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

