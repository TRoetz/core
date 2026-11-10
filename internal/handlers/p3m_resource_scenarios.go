package handlers

import (
	"fmt"
	"math"
	"net/http"
)

// p3mResourceScenarioRow is one resource in a what-if scenario result: its
// committed project-class hours, BAU offset (RES-004), and the proposed
// scenario total against weekly capacity.
type p3mResourceScenarioRow struct {
	ResourceID           int    `json:"resource_id"`
	Name                 string `json:"name"`
	CapacityHoursPerWeek int    `json:"capacity_hours_per_week"`
	BauHours             int    `json:"bau_hours"`
	CommittedHours       int    `json:"committed_hours"`
	ScenarioHours        int    `json:"scenario_hours"`
	UtilizationPct       int    `json:"utilization_pct"`
	OverAllocated        bool   `json:"over_allocated"`
}

// p3mResourceScenarioResult is the RunResourceScenario response.
type p3mResourceScenarioResult struct {
	Resources []p3mResourceScenarioRow `json:"resources"`
}

// RunResourceScenario handles POST /api/p3m/resource-scenarios (RES-004/006).
// It is a pure in-memory what-if: it loads the resource inventory (optionally
// filtered to one pool), each resource's committed project-class allocations
// and BAU hours, applies the requested deltas on top, and reports utilization.
// Requested resource ids must belong to the loaded set — with a pool filter
// that means the pool; anything else is rejected as unknown. No DB writes.
func (h *P3MHandler) RunResourceScenario(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	_ = user // read-only what-if: no committed mutation, so no audit entry

	var in struct {
		Allocations []struct {
			ResourceID   int `json:"resource_id"`
			HoursPerWeek int `json:"hours_per_week"`
		} `json:"allocations"`
		Pool string `json:"pool,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	deltaByResource := make(map[int]int, len(in.Allocations))
	for _, a := range in.Allocations {
		if a.HoursPerWeek < 0 {
			respondValidationError(w, r, fmt.Sprintf("hours_per_week must be zero or greater for resource %d", a.ResourceID))
			return
		}
		deltaByResource[a.ResourceID] += a.HoursPerWeek
	}

	q := "SELECT id, name, capacity_hours_per_week FROM p3m_resources"
	var args []any
	if in.Pool != "" {
		q += " WHERE pool = ?"
		args = append(args, in.Pool)
	}
	q += " ORDER BY id"
	rows, err := h.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	type resourceRef struct {
		id       int
		name     string
		capacity int
	}
	var refs []resourceRef
	refSet := make(map[int]bool)
	for rows.Next() {
		var ref resourceRef
		if err := rows.Scan(&ref.id, &ref.name, &ref.capacity); err != nil {
			respondInternalError(w, r, err)
			return
		}
		refs = append(refs, ref)
		refSet[ref.id] = true
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	checked := make(map[int]bool, len(in.Allocations))
	for _, a := range in.Allocations {
		if checked[a.ResourceID] {
			continue
		}
		checked[a.ResourceID] = true
		if !refSet[a.ResourceID] {
			respondValidationError(w, r, fmt.Sprintf("unknown resource_id %d", a.ResourceID))
			return
		}
	}

	committedByResource := make(map[int]int)
	bauByResource := make(map[int]int)
	if len(refs) > 0 {
		ids := make([]any, 0, len(refs))
		for _, ref := range refs {
			ids = append(ids, ref.id)
		}
		allocRows, err := h.db.QueryContext(r.Context(), `SELECT resource_id, allocation_class, SUM(hours_per_week) FROM p3m_resource_allocations WHERE resource_id IN (`+questionPlaceholders(len(ids))+`) GROUP BY resource_id, allocation_class`, ids...)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		defer allocRows.Close()
		for allocRows.Next() {
			var rid int
			var class string
			var hours int
			if err := allocRows.Scan(&rid, &class, &hours); err != nil {
				respondInternalError(w, r, err)
				return
			}
			if class == "bau" {
				bauByResource[rid] += hours
			} else {
				committedByResource[rid] += hours
			}
		}
		if err := allocRows.Err(); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	out := make([]p3mResourceScenarioRow, 0, len(refs))
	for _, ref := range refs {
		committed := committedByResource[ref.id]
		scenario := committed + deltaByResource[ref.id] // zero when not requested: unchanged
		bau := bauByResource[ref.id]
		total := scenario + bau

		utilization := 0
		if ref.capacity > 0 {
			utilization = int(math.Round(100.0 * float64(total) / float64(ref.capacity)))
		}
		out = append(out, p3mResourceScenarioRow{
			ResourceID:           ref.id,
			Name:                 ref.name,
			CapacityHoursPerWeek: ref.capacity,
			BauHours:             bau,
			CommittedHours:       committed,
			ScenarioHours:        scenario,
			UtilizationPct:       utilization,
			OverAllocated:        total > ref.capacity,
		})
	}

	respondJSONOK(w, p3mResourceScenarioResult{Resources: out})
}
