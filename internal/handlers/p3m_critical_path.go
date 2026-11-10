package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"windshift/internal/models"
)

// cpmNode is one dated work item fed to computeCPM. Start and End are whole
// days (UTC midnight, End on or after Start); the inclusive day count between
// them is the task's duration.
type cpmNode struct {
	id          int
	title       string
	workspaceID int
	start       time.Time
	end         time.Time
}

// cpmEdge is a "blocks" dependency: src must finish before dst starts.
type cpmEdge struct{ src, dst int }

// cpmResult holds the scheduled output of computeCPM. projectStart/projectEnd
// are only meaningful when tasks is non-empty.
type cpmResult struct {
	tasks        []models.P3MCriticalPathTask
	projectStart time.Time // earliest start across all tasks (UTC midnight)
	projectEnd   time.Time // latest inclusive finish across all tasks (UTC midnight)
	criticalIDs  []int     // critical task ids in topological order
}

const cpmDay = 24 * time.Hour

var cpmEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// cpmDayIndex maps a timestamp to its whole-day index (UTC).
func cpmDayIndex(t time.Time) int64 {
	return int64(t.UTC().Truncate(cpmDay).Sub(cpmEpoch) / cpmDay)
}

// cpmDateAt maps a day index back to its UTC midnight.
func cpmDateAt(idx int64) time.Time { return cpmEpoch.Add(time.Duration(idx) * cpmDay) }

// computeCPM runs a classic CPM forward/backward pass over nodes with "blocks"
// edges. Each node's planned window contributes an earliest-start constraint
// (its start date) and its duration (inclusive day count); the project end is
// the latest earliest finish, which anchors the backward pass. A task is
// critical when its total float is zero; criticalIDs lists them in topological
// order so they read as the path that determines completion. Returns
// *models.P3MCPMCycleError when the edges contain a cycle.
func computeCPM(nodes []cpmNode, edges []cpmEdge) (*cpmResult, error) {
	if len(nodes) == 0 {
		return &cpmResult{tasks: []models.P3MCriticalPathTask{}, criticalIDs: []int{}}, nil
	}

	idx := make(map[int]int, len(nodes))
	for i, n := range nodes {
		idx[n.id] = i
	}

	preds := make([][]int, len(nodes))
	succs := make([][]int, len(nodes))
	indeg := make([]int, len(nodes))
	for _, e := range edges {
		si, okS := idx[e.src]
		di, okD := idx[e.dst]
		if !okS || !okD || si == di {
			continue // defensive: ignore dangling or self edges
		}
		preds[di] = append(preds[di], si)
		succs[si] = append(succs[si], di)
		indeg[di]++
	}

	// Kahn topological sort, deterministic (lowest node index first).
	queue := make([]int, 0, len(nodes))
	for i := range nodes {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	order := make([]int, 0, len(nodes))
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		order = append(order, i)
		for _, s := range succs[i] {
			indeg[s]--
			if indeg[s] == 0 {
				queue = append(queue, s)
			}
		}
	}
	if len(order) < len(nodes) {
		remaining := make(map[int]bool, len(nodes)-len(order))
		for i, n := range nodes {
			if indeg[i] > 0 {
				remaining[n.id] = true
			}
		}
		succByID := make(map[int][]int, len(remaining))
		for _, e := range edges {
			if remaining[e.src] && remaining[e.dst] {
				succByID[e.src] = append(succByID[e.src], e.dst)
			}
		}
		return nil, &models.P3MCPMCycleError{ItemIDs: findCPMCycle(remaining, succByID)}
	}

	dur := make([]float64, len(nodes))
	es := make([]float64, len(nodes))
	ef := make([]float64, len(nodes))
	for i, n := range nodes {
		s := cpmDayIndex(n.start)
		e := cpmDayIndex(n.end) + 1 // exclusive finish: successor may start the next day
		d := float64(e - s)
		if d < 0 {
			d = 0
		}
		dur[i] = d
		es[i] = float64(s)
	}

	// Forward pass in topological order.
	for _, i := range order {
		for _, p := range preds[i] {
			if ef[p] > es[i] {
				es[i] = ef[p]
			}
		}
		ef[i] = es[i] + dur[i]
	}

	projectEndExcl := 0.0
	for _, v := range ef {
		if v > projectEndExcl {
			projectEndExcl = v
		}
	}

	// Backward pass in reverse topological order; the project end anchors all
	// terminal tasks.
	lf := make([]float64, len(nodes))
	ls := make([]float64, len(nodes))
	for i := len(order) - 1; i >= 0; i-- {
		node := order[i]
		lf[node] = projectEndExcl
		for _, s := range succs[node] {
			if ls[s] < lf[node] {
				lf[node] = ls[s]
			}
		}
		ls[node] = lf[node] - dur[node]
	}

	tasks := make([]models.P3MCriticalPathTask, 0, len(nodes))
	critIDs := []int{}
	projectStartDay := int64(0)
	for i, v := range es {
		if i == 0 || v < float64(projectStartDay) {
			projectStartDay = int64(v)
		}
	}
	for _, i := range order { // topological order: critical ids read as a path
		n := nodes[i]
		tf := ls[i] - es[i]
		crit := tf > -1e-9 && tf < 1e-9
		if crit {
			critIDs = append(critIDs, n.id)
		}
		tasks = append(tasks, models.P3MCriticalPathTask{
			ItemID:         n.id,
			Title:          n.title,
			WorkspaceID:    n.workspaceID,
			StartDate:      cpmDateAt(cpmDayIndex(n.start)),
			EndDate:        cpmDateAt(cpmDayIndex(n.end)),
			DurationDays:   dur[i],
			EarliestStart:  cpmDateAt(int64(es[i])),
			EarliestFinish: cpmDateAt(int64(ef[i]) - 1),
			LatestStart:    cpmDateAt(int64(ls[i])),
			LatestFinish:   cpmDateAt(int64(lf[i]) - 1),
			TotalFloatDays: tf,
			IsCritical:     crit,
		})
	}

	return &cpmResult{
		tasks:        tasks,
		projectStart: cpmDateAt(projectStartDay),
		projectEnd:   cpmDateAt(int64(projectEndExcl) - 1),
		criticalIDs:  critIDs,
	}, nil
}

// findCPMCycle returns one concrete cycle in the remaining subgraph, in edge
// order and starting from its lowest item id. The Kahn remainder is closed
// under successors, so a DFS restricted to it always finds a back edge.
func findCPMCycle(remaining map[int]bool, succs map[int][]int) []int {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS path
		black = 2 // fully explored
	)
	color := make(map[int]int, len(remaining))
	var stack []int

	var dfs func(u int) []int
	dfs = func(u int) []int {
		color[u] = gray
		stack = append(stack, u)
		nexts := slices.Clone(succs[u])
		slices.Sort(nexts) // deterministic neighbor order
		for _, v := range nexts {
			if !remaining[v] || color[v] == black {
				continue
			}
			if color[v] == gray {
				start := 0
				for start < len(stack) && stack[start] != v {
					start++
				}
				cycle := append(slices.Clone(stack[start:]), v)
				return cycle
			}
			if c := dfs(v); c != nil {
				return c
			}
		}
		stack = stack[:len(stack)-1]
		color[u] = black
		return nil
	}

	starts := make([]int, 0, len(remaining))
	for id := range remaining {
		starts = append(starts, id)
	}
	slices.Sort(starts)
	for _, s := range starts {
		if color[s] == white {
			if c := dfs(s); c != nil {
				return c
			}
		}
	}
	// Unreachable: the Kahn remainder always contains a cycle.
	return []int{}
}

// loadCPMSchedule gathers every dated work item in the workspaces linked to
// the given P3M projects plus their "blocks" dependencies, and runs computeCPM
// over them. dependencyCount is the number of edges actually used (both ends
// present among the dated items).
func (h *P3MHandler) loadCPMSchedule(ctx context.Context, projectIDs []int) (*cpmResult, int, error) {
	nodes, edges, err := h.loadCPMNodesAndEdges(ctx, projectIDs)
	if err != nil {
		return nil, 0, err
	}
	res, err := computeCPM(nodes, edges)
	if err != nil {
		return nil, 0, err
	}
	return res, len(edges), nil
}

// loadCPMNodesAndEdges loads the dated work items and "blocks" dependencies for
// the given P3M projects without running CPM, so callers can apply hypothetical
// changes before scheduling (what-if preview).
func (h *P3MHandler) loadCPMNodesAndEdges(ctx context.Context, projectIDs []int) ([]cpmNode, []cpmEdge, error) {
	if len(projectIDs) == 0 {
		return nil, nil, nil
	}

	idArgs := make([]any, len(projectIDs))
	ph := make([]string, len(projectIDs))
	for i, id := range projectIDs {
		idArgs[i] = id
		ph[i] = "?"
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT DISTINCT wpw.workspace_id FROM workspace_p3m_projects wpw
		WHERE wpw.p3m_project_id IN (`+joinPlaceholders(ph)+`)`, idArgs...)
	if err != nil {
		return nil, nil, err
	}
	wsIDs := make([]int, 0)
	for rows.Next() {
		var wsID int
		if err := rows.Scan(&wsID); err != nil {
			rows.Close()
			return nil, nil, err
		}
		wsIDs = append(wsIDs, wsID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	if len(wsIDs) == 0 {
		return nil, nil, nil
	}

	wsArgs := make([]any, len(wsIDs))
	wsPh := make([]string, len(wsIDs))
	for i, id := range wsIDs {
		wsArgs[i] = id
		wsPh[i] = "?"
	}

	itemsRows, err := h.db.QueryContext(ctx, `
		SELECT id, title, workspace_id, start_date, end_date FROM items
		WHERE merged_into_item_id IS NULL AND workspace_id IN (`+joinPlaceholders(wsPh)+`)
		  AND start_date IS NOT NULL AND end_date IS NOT NULL AND end_date >= start_date
		ORDER BY id`, wsArgs...)
	if err != nil {
		return nil, nil, err
	}
	nodes := make([]cpmNode, 0)
	itemIDs := make([]int, 0)
	for itemsRows.Next() {
		var n cpmNode
		var start, end sql.NullTime
		if err := itemsRows.Scan(&n.id, &n.title, &n.workspaceID, &start, &end); err != nil {
			itemsRows.Close()
			return nil, nil, err
		}
		n.start = start.Time.UTC().Truncate(cpmDay)
		n.end = end.Time.UTC().Truncate(cpmDay)
		nodes = append(nodes, n)
		itemIDs = append(itemIDs, n.id)
	}
	if err := itemsRows.Err(); err != nil {
		itemsRows.Close()
		return nil, nil, err
	}
	itemsRows.Close()

	if len(itemIDs) == 0 {
		return nil, nil, nil
	}

	itemArgs := make([]any, len(itemIDs))
	itemPh := make([]string, len(itemIDs))
	for i, id := range itemIDs {
		itemArgs[i] = id
		itemPh[i] = "?"
	}

	depRows, err := h.db.QueryContext(ctx, `
		SELECT source_item_id, target_item_id FROM item_dependencies
		WHERE dep_type = 'blocks' AND source_item_id IN (`+joinPlaceholders(itemPh)+`)
		  AND target_item_id IN (`+joinPlaceholders(itemPh)+`)`, append(append([]any{}, itemArgs...), itemArgs...)...)
	if err != nil {
		return nil, nil, err
	}
	edges := make([]cpmEdge, 0)
	for depRows.Next() {
		var e cpmEdge
		if err := depRows.Scan(&e.src, &e.dst); err != nil {
			depRows.Close()
			return nil, nil, err
		}
		edges = append(edges, e)
	}
	if err := depRows.Err(); err != nil {
		depRows.Close()
		return nil, nil, err
	}
	depRows.Close()

	return nodes, edges, nil
}

// applyCPMRegisterCounts annotates each task with its open risks and pending
// decisions linked via p3m_raid_items.item_id (status != 'closed').
func (h *P3MHandler) applyCPMRegisterCounts(ctx context.Context, tasks []models.P3MCriticalPathTask) error {
	if len(tasks) == 0 {
		return nil
	}
	idArgs := make([]any, len(tasks))
	ph := make([]string, len(tasks))
	for i, t := range tasks {
		idArgs[i] = t.ItemID
		ph[i] = "?"
	}

	rows, err := h.db.QueryContext(ctx, `SELECT item_id, item_type FROM p3m_raid_items WHERE status != 'closed' AND item_id IS NOT NULL AND item_id IN (`+joinPlaceholders(ph)+`)`, idArgs...)
	if err != nil {
		return err
	}
	defer rows.Close()

	riskCounts := make(map[int]int, len(tasks))
	decisionCounts := make(map[int]int, len(tasks))
	for rows.Next() {
		var itemID int
		var itemType string
		if err := rows.Scan(&itemID, &itemType); err != nil {
			return err
		}
		switch itemType {
		case "risk":
			riskCounts[itemID]++
		case models.P3MRAIDItemTypeDecision:
			decisionCounts[itemID]++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range tasks {
		tasks[i].OpenRisks = riskCounts[tasks[i].ItemID]
		tasks[i].PendingDecisions = decisionCounts[tasks[i].ItemID]
	}
	return nil
}

// respondCPMCycleError writes the 422 dependency-cycle response: a flat JSON
// body carrying the cycle's item ids so clients can surface them verbatim.
func respondCPMCycleError(w http.ResponseWriter, r *http.Request, err error) {
	var cyc *models.P3MCPMCycleError
	if !errors.As(err, &cyc) {
		cyc = &models.P3MCPMCycleError{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":    cyc.Error(),
		"code":     "DEPENDENCY_CYCLE",
		"item_ids": cyc.ItemIDs,
	})
}

// GetProjectCriticalPath handles GET /api/p3m/projects/{id}/critical-path. It
// schedules every dated work item in the project's linked workspaces with CPM
// and reports total float plus the zero-float critical path.
func (h *P3MHandler) GetProjectCriticalPath(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	p, err := h.getProjectRow(ctx, id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	res, depCount, err := h.loadCPMSchedule(ctx, []int{id})
	if err != nil {
		var cyc *models.P3MCPMCycleError
		if errors.As(err, &cyc) {
			respondCPMCycleError(w, r, err)
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if err := h.applyCPMRegisterCounts(ctx, res.tasks); err != nil {
		respondInternalError(w, r, err)
		return
	}

	out := models.P3MCriticalPath{
		GeneratedAt:       time.Now().UTC(),
		EntityType:        "project",
		EntityID:          id,
		Title:             p.Title,
		ItemCount:         len(res.tasks),
		DependencyCount:   depCount,
		CriticalPathDays:  cpmSpanDays(res),
		Tasks:             res.tasks,
		CriticalTaskIDs:   res.criticalIDs,
	}
	if len(res.tasks) > 0 {
		ps, pe := res.projectStart, res.projectEnd
		out.ProjectStart = &ps
		out.ProjectEnd = &pe
	}
	respondJSONOK(w, out)
}

// GetProgrammeCriticalPath handles GET /api/p3m/programmes/{id}/critical-path.
// It unions the dated work items of every project in the programme (including
// cross-project dependencies) and schedules them as one network, so the
// critical path is the chain that determines the programme's completion date.
func (h *P3MHandler) GetProgrammeCriticalPath(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	pg, err := h.getProgrammeRow(ctx, id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_programme")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(ctx, `SELECT id FROM p3m_projects WHERE programme_id = ? ORDER BY id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	projectIDs := make([]int, 0)
	for rows.Next() {
		var pid int
		if err := rows.Scan(&pid); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		projectIDs = append(projectIDs, pid)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	rows.Close()

	res, depCount, err := h.loadCPMSchedule(ctx, projectIDs)
	if err != nil {
		var cyc *models.P3MCPMCycleError
		if errors.As(err, &cyc) {
			respondCPMCycleError(w, r, err)
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if err := h.applyCPMRegisterCounts(ctx, res.tasks); err != nil {
		respondInternalError(w, r, err)
		return
	}

	out := models.P3MCriticalPath{
		GeneratedAt:       time.Now().UTC(),
		EntityType:        "programme",
		EntityID:          id,
		Title:             pg.Title,
		ItemCount:         len(res.tasks),
		DependencyCount:   depCount,
		CriticalPathDays:  cpmSpanDays(res),
		Tasks:             res.tasks,
		CriticalTaskIDs:   res.criticalIDs,
	}
	if len(res.tasks) > 0 {
		ps, pe := res.projectStart, res.projectEnd
		out.ProjectStart = &ps
		out.ProjectEnd = &pe
	}
	respondJSONOK(w, out)
}

// cpmSpanDays is the working-day span from first start to last finish.
func cpmSpanDays(res *cpmResult) float64 {
	if len(res.tasks) == 0 {
		return 0
	}
	return float64(cpmDayIndex(res.projectEnd)-cpmDayIndex(res.projectStart)) + 1
}
