package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// Plan scenarios ("what if" planning with dependency ripple impact)
// ---------------------------------------------------------------------------

// flexTime accepts either an RFC 3339 timestamp or a bare "YYYY-MM-DD" date,
// normalizing to UTC so scenario overrides round-trip cleanly. Planning UIs
// deal in calendar dates; both forms are valid input.
type flexTime struct {
	t   time.Time
	set bool
}

func (f *flexTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		t, err := time.Parse(layout, s)
		if err == nil {
			f.t = t.UTC()
			f.set = true
			return nil
		}
	}
	return fmt.Errorf("invalid date %q (want RFC 3339 or YYYY-MM-DD)", s)
}

func nullFlexTime(f *flexTime) any {
	if f == nil || !f.set {
		return nil
	}
	return f.t
}

func (h *PlanHandler) getScenarioRow(ctx context.Context, scenarioID int) (*models.PlanScenario, error) {
	var s models.PlanScenario
	err := h.db.QueryRowContext(ctx, `SELECT id, plan_id, name, description, status, created_at, updated_at FROM plan_scenarios WHERE id = ?`, scenarioID).
		Scan(&s.ID, &s.PlanID, &s.Name, &s.Description, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListPlanScenarios handles GET /api/plans/{id}/scenarios.
func (h *PlanHandler) ListPlanScenarios(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT s.id, s.plan_id, s.name, s.description, s.status, s.created_at, s.updated_at,
			COALESCE(c.cnt, 0)
		FROM plan_scenarios s
		LEFT JOIN (SELECT scenario_id, COUNT(*) AS cnt FROM plan_scenario_items GROUP BY scenario_id) c ON c.scenario_id = s.id
		WHERE s.plan_id = ?
		ORDER BY s.created_at DESC`, id)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("list scenarios: %w", err))
		return
	}
	defer rows.Close()
	out := make([]models.PlanScenario, 0)
	for rows.Next() {
		var s models.PlanScenario
		if err := rows.Scan(&s.ID, &s.PlanID, &s.Name, &s.Description, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.ItemCount); err != nil {
			respondInternalError(w, r, fmt.Errorf("scan scenario: %w", err))
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

// CreatePlanScenario handles POST /api/plans/{id}/scenarios with body
// { "name": "...", "description": "..." }. New scenarios start as drafts.
func (h *PlanHandler) CreatePlanScenario(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(),
		`INSERT INTO plan_scenarios (plan_id, name, description, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, in.Name, strings.TrimSpace(in.Description), models.PlanScenarioDraft, now, now)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("create scenario: %w", err))
		return
	}
	sID, _ := res.LastInsertId()
	s, err := h.getScenarioRow(r.Context(), int(sID))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, s)
}

// UpdatePlanScenario handles PUT /api/plans/{id}/scenarios/{sid}. Only the
// provided fields are changed.
func (h *PlanHandler) UpdatePlanScenario(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	sid, ok := requireIDParam(w, r, "sid")
	if !ok {
		return
	}
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	s, err := h.getScenarioRow(r.Context(), sid)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "scenario")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if s.PlanID != id {
		respondNotFound(w, r, "scenario")
		return
	}

	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		respondValidationError(w, r, "name cannot be empty")
		return
	}
	if in.Status != nil {
		switch *in.Status {
		case models.PlanScenarioDraft, models.PlanScenarioActive, models.PlanScenarioArchived:
		default:
			respondValidationError(w, r, "status must be 'draft', 'active' or 'archived'")
			return
		}
	}

	now := time.Now().UTC()
	if in.Name != nil {
		if _, err := h.db.ExecContext(r.Context(), `UPDATE plan_scenarios SET name = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(*in.Name), now, sid); err != nil {
			respondInternalError(w, r, fmt.Errorf("rename scenario: %w", err))
			return
		}
	}
	if in.Description != nil {
		if _, err := h.db.ExecContext(r.Context(), `UPDATE plan_scenarios SET description = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(*in.Description), now, sid); err != nil {
			respondInternalError(w, r, fmt.Errorf("update scenario: %w", err))
			return
		}
	}
	if in.Status != nil {
		if _, err := h.db.ExecContext(r.Context(), `UPDATE plan_scenarios SET status = ?, updated_at = ? WHERE id = ?`, *in.Status, now, sid); err != nil {
			respondInternalError(w, r, fmt.Errorf("update scenario: %w", err))
			return
		}
	}

	out, err := h.getScenarioRow(r.Context(), sid)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// DeletePlanScenario handles DELETE /api/plans/{id}/scenarios/{sid}.
func (h *PlanHandler) DeletePlanScenario(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	sid, ok := requireIDParam(w, r, "sid")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM plan_scenarios WHERE id = ? AND plan_id = ?`, sid, id)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("delete scenario: %w", err))
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "scenario")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Scenario detail with dependency ripple impact
// ---------------------------------------------------------------------------

// scenarioDateState tracks one item's dates through a scenario: the stored
// base dates, the effective dates after overrides (orig), and the final dates
// after dependency propagation.
type scenarioDateState struct {
	baseStart *time.Time
	baseEnd   *time.Time
	origStart *time.Time
	origEnd   *time.Time
	start     *time.Time
	end       *time.Time
}

// moved reports whether the item's effective dates differ from its base.
func (st *scenarioDateState) moved() bool {
	if st.origStart != nil && (st.start == nil || !st.start.Equal(*st.origStart)) {
		return true
	}
	if st.origEnd != nil && (st.end == nil || !st.end.Equal(*st.origEnd)) {
		return true
	}
	return false
}

// shiftDays is the end-date slip in days relative to base (0 when unknown).
func (st *scenarioDateState) shiftDays() int {
	if st.end == nil || st.baseEnd == nil {
		return 0
	}
	return int(math.Round(st.end.Sub(*st.baseEnd).Hours() / 24))
}

// computeScenarioImpact resolves effective dates for every plan item: overrides
// win over base dates, then "blocks" dependencies push dependents later in a
// fixpoint pass (durations are preserved; cycles terminate via the pass cap).
func (h *PlanHandler) computeScenarioImpact(ctx context.Context, planID int, overrides map[int]models.PlanScenarioItem) (map[int]*scenarioDateState, error) {
	sourceSQL, sourceArgs, err := h.planSourceCondition(ctx, planID)
	if err != nil {
		return nil, err
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT i.id, i.start_date, i.end_date
		FROM items i
		WHERE i.merged_into_item_id IS NULL AND (`+sourceSQL+`)`, append([]any{}, sourceArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("load plan item dates: %w", err)
	}
	defer rows.Close()

	state := make(map[int]*scenarioDateState)
	for rows.Next() {
		var itemID int
		var baseStart, baseEnd sql.NullTime
		if err := rows.Scan(&itemID, &baseStart, &baseEnd); err != nil {
			return nil, err
		}
		st := &scenarioDateState{}
		if baseStart.Valid {
			st.baseStart = new(baseStart.Time)
		}
		if baseEnd.Valid {
			st.baseEnd = new(baseEnd.Time)
		}
		if ov, ok := overrides[itemID]; ok {
			if ov.StartDate != nil {
				st.origStart = new(*ov.StartDate)
			} else if st.baseStart != nil {
				st.origStart = new(*st.baseStart)
			}
			if ov.EndDate != nil {
				st.origEnd = new(*ov.EndDate)
			} else if st.baseEnd != nil {
				st.origEnd = new(*st.baseEnd)
			}
		} else {
			st.origStart, st.origEnd = st.baseStart, st.baseEnd
		}
		st.start, st.end = st.origStart, st.origEnd
		state[itemID] = st
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// "blocks" edges inside the plan: source must finish before target starts.
	eRows, err := h.db.QueryContext(ctx, `SELECT source_item_id, target_item_id FROM item_dependencies WHERE dep_type = ?`, models.ItemDepBlocks)
	if err != nil {
		return nil, fmt.Errorf("load dependency edges: %w", err)
	}
	type edge struct{ src, dst int }
	var edges []edge
	for eRows.Next() {
		var e edge
		if err := eRows.Scan(&e.src, &e.dst); err != nil {
			eRows.Close()
			return nil, err
		}
		if _, ok := state[e.src]; !ok {
			continue
		}
		if _, ok := state[e.dst]; !ok {
			continue
		}
		edges = append(edges, e)
	}
	err = eRows.Err()
	eRows.Close()
	if err != nil {
		return nil, err
	}

	for range 100 {
		changed := false
		for _, e := range edges {
			src, dst := state[e.src], state[e.dst]
			if src.end == nil || dst.start == nil {
				continue
			}
			if !src.end.After(*dst.start) {
				continue
			}
			delta := src.end.Sub(*dst.start)
			newStart := dst.start.Add(delta)
			dst.start = &newStart
			if dst.end != nil {
				dst.end = new((*dst.end).Add(delta))
			}
			changed = true
		}
		if !changed {
			break
		}
	}

	return state, nil
}

// GetPlanScenarioDetail handles GET /api/plans/{id}/scenarios/{sid}: the
// scenario with its overrides plus every item shifted by propagation.
func (h *PlanHandler) GetPlanScenarioDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	sid, ok := requireIDParam(w, r, "sid")
	if !ok {
		return
	}
	ctx := r.Context()

	s, err := h.getScenarioRow(ctx, sid)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "scenario")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if s.PlanID != id {
		respondNotFound(w, r, "scenario")
		return
	}

	rows, err := h.db.QueryContext(ctx, `SELECT item_id, start_date, end_date, due_date, notes FROM plan_scenario_items WHERE scenario_id = ? ORDER BY item_id`, sid)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("load scenario overrides: %w", err))
		return
	}
	defer rows.Close()
	overrides := make(map[int]models.PlanScenarioItem)
	for rows.Next() {
		var ov models.PlanScenarioItem
		var start, end, due sql.NullTime
		if err := rows.Scan(&ov.ItemID, &start, &end, &due, &ov.Notes); err != nil {
			respondInternalError(w, r, fmt.Errorf("scan scenario override: %w", err))
			return
		}
		if start.Valid {
			t := start.Time
			ov.StartDate = &t
		}
		if end.Valid {
			t := end.Time
			ov.EndDate = &t
		}
		if due.Valid {
			t := due.Time
			ov.DueDate = &t
		}
		overrides[ov.ItemID] = ov
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	state, err := h.computeScenarioImpact(ctx, id, overrides)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	// Downstream counts: direct "blocks" targets that ended up shifted.
	dRows, err := h.db.QueryContext(ctx, `SELECT source_item_id, target_item_id FROM item_dependencies WHERE dep_type = ?`, models.ItemDepBlocks)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("load dependency edges: %w", err))
		return
	}
	downstream := make(map[int]int)
	for dRows.Next() {
		var src, dst int
		if err := dRows.Scan(&src, &dst); err != nil {
			dRows.Close()
			respondInternalError(w, r, err)
			return
		}
		dstState, inPlan := state[dst]
		if !inPlan {
			continue
		}
		if dstState.moved() {
			downstream[src]++
		}
	}
	err = dRows.Err()
	dRows.Close()
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	reported := make(map[int]bool, len(overrides))
	for itemID, st := range state {
		if _, isOverride := overrides[itemID]; isOverride || st.moved() {
			reported[itemID] = true
		}
	}

	detailRows, err := h.db.QueryContext(ctx, `
		SELECT i.id, w.key || '-' || CAST(i.workspace_item_number AS TEXT), i.title
		FROM items i
		JOIN workspaces w ON w.id = i.workspace_id
		WHERE i.merged_into_item_id IS NULL AND (`+planItemIDsPlaceholder(len(reported))+`)`, planItemIDArgs(reported)...)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("load scenario item details: %w", err))
		return
	}
	defer detailRows.Close()
	keys := make(map[int]string, len(reported))
	titles := make(map[int]string, len(reported))
	for detailRows.Next() {
		var itemID int
		var key, title string
		if err := detailRows.Scan(&itemID, &key, &title); err != nil {
			respondInternalError(w, r, fmt.Errorf("scan scenario item: %w", err))
			return
		}
		keys[itemID] = key
		titles[itemID] = title
	}
	if err := detailRows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	items := make([]models.PlanScenarioItem, 0, len(reported))
	impact := models.PlanScenarioImpact{}
	for itemID := range reported {
		st := state[itemID]
		out := overrides[itemID] // zero value when only shifted by propagation
		out.ItemID = itemID
		out.ItemKey = keys[itemID]
		out.ItemTitle = titles[itemID]
		out.BaseStartDate, out.BaseEndDate = st.baseStart, st.baseEnd
		out.EffectiveEnd = st.end
		out.ShiftDays = st.shiftDays()
		out.Downstream = downstream[itemID]
		items = append(items, out)

		if st.moved() {
			impact.AffectedItemCount++
			if out.ShiftDays > impact.MaxShiftDays {
				impact.MaxShiftDays = out.ShiftDays
			}
		}
	}

	sortPlanScenarioItems(items)
	respondJSONOK(w, models.PlanScenarioDetail{Scenario: *s, Items: items, Impact: impact})
}

// planItemIDsPlaceholder builds a comma-separated placeholder list.
func planItemIDsPlaceholder(n int) string {
	if n == 0 {
		return "1=0"
	}
	ph := make([]string, n)
	for i := range ph {
		ph[i] = "?"
	}
	return "i.id IN (" + strings.Join(ph, ",") + ")"
}

// planItemIDArgs returns the item ids for a reported-set placeholder list.
func planItemIDArgs(reported map[int]bool) []any {
	args := make([]any, 0, len(reported))
	for id := range reported {
		args = append(args, id)
	}
	return args
}

// sortPlanScenarioItems orders detail rows by key for stable rendering.
func sortPlanScenarioItems(items []models.PlanScenarioItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].ItemKey < items[j-1].ItemKey; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// ---------------------------------------------------------------------------
// Scenario item overrides
// ---------------------------------------------------------------------------

// UpsertScenarioItems handles PUT /api/plans/{id}/scenarios/{sid}/items with
// body { "items": [ {item_id, start_date?, end_date?, due_date?, notes?} ],
// "remove": [item_ids] }. An entry whose dates are all null and notes empty
// resets the item to its base values (the override row is deleted).
func (h *PlanHandler) UpsertScenarioItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	sid, ok := requireIDParam(w, r, "sid")
	if !ok {
		return
	}
	ctx := r.Context()

	var in struct {
		Items []struct {
			ItemID    int       `json:"item_id"`
			StartDate *flexTime `json:"start_date"`
			EndDate   *flexTime `json:"end_date"`
			DueDate   *flexTime `json:"due_date"`
			Notes     string    `json:"notes"`
		} `json:"items"`
		Remove []int `json:"remove"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	s, err := h.getScenarioRow(ctx, sid)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "scenario")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	if s.PlanID != id {
		respondNotFound(w, r, "scenario")
		return
	}

	sourceSQL, sourceArgs, err := h.planSourceCondition(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	inPlanIDs := make(map[int]bool)
	idRows, err := h.db.QueryContext(ctx, "SELECT i.id FROM items i WHERE i.merged_into_item_id IS NULL AND ("+sourceSQL+")", append([]any{}, sourceArgs...)...)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("collect plan item ids: %w", err))
		return
	}
	for idRows.Next() {
		var itemID int
		if err := idRows.Scan(&itemID); err != nil {
			idRows.Close()
			respondInternalError(w, r, err)
			return
		}
		inPlanIDs[itemID] = true
	}
	err = idRows.Err()
	idRows.Close()
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("begin scenario update: %w", err))
		return
	}
	defer func() { _ = tx.Rollback() }()

	for _, itemID := range in.Remove {
		if _, err := tx.Exec(`DELETE FROM plan_scenario_items WHERE scenario_id = ? AND item_id = ?`, sid, itemID); err != nil {
			respondInternalError(w, r, fmt.Errorf("remove scenario override: %w", err))
			return
		}
	}

	for _, it := range in.Items {
		if !inPlanIDs[it.ItemID] {
			respondValidationError(w, r, fmt.Sprintf("item %d is not part of this plan", it.ItemID))
			return
		}
		reset := it.StartDate == nil && it.EndDate == nil && it.DueDate == nil && strings.TrimSpace(it.Notes) == ""
		if reset {
			if _, err := tx.Exec(`DELETE FROM plan_scenario_items WHERE scenario_id = ? AND item_id = ?`, sid, it.ItemID); err != nil {
				respondInternalError(w, r, fmt.Errorf("reset scenario override: %w", err))
				return
			}
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO plan_scenario_items (scenario_id, item_id, start_date, end_date, due_date, notes)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(scenario_id, item_id) DO UPDATE SET
				start_date = excluded.start_date,
				end_date = excluded.end_date,
				due_date = excluded.due_date,
				notes = excluded.notes`,
			sid, it.ItemID, nullFlexTime(it.StartDate), nullFlexTime(it.EndDate), nullFlexTime(it.DueDate), strings.TrimSpace(it.Notes)); err != nil {
			respondInternalError(w, r, fmt.Errorf("upsert scenario override: %w", err))
			return
		}
	}

	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, fmt.Errorf("commit scenario update: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
