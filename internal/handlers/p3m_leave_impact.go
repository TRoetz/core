// P3M leave impact: how active user leaves hit project resource allocations
// and critical-path tasks. Visibility only — no rescheduling is performed.
package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"windshift/internal/models"
)

// p3mParseLeaveDate parses a stored leave-period boundary. The column is a
// DATE but drivers may hand back either the bare date or a normalized
// timestamp depending on backend and storage path.
func p3mParseLeaveDate(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized leave date %q", value)
}

// p3mActiveLeavePeriods fetches a user's active leave periods with the
// substitute's name when one was named. Dates are stored as YYYY-MM-DD
// strings and parsed to UTC midnight; EndDate is inclusive.
func (h *P3MHandler) p3mActiveLeavePeriods(ctx context.Context, userID int) ([]models.P3MLeavePeriodRef, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT l.start_date, l.end_date, u.first_name || ' ' || u.last_name
		FROM user_leave_periods l
		LEFT JOIN users u ON u.id = l.substitute_user_id
		WHERE l.user_id = ? AND l.is_active = 1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.P3MLeavePeriodRef, 0)
	for rows.Next() {
		var startS, endS string
		var substitute sql.NullString
		if err := rows.Scan(&startS, &endS, &substitute); err != nil {
			return nil, err
		}
		start, err := p3mParseLeaveDate(startS)
		if err != nil {
			return nil, err
		}
		end, err := p3mParseLeaveDate(endS)
		if err != nil {
			return nil, err
		}
		ref := models.P3MLeavePeriodRef{StartDate: start, EndDate: end}
		if substitute.Valid && substitute.String != "" {
			ref.SubstituteName = substitute.String
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// p3mComputeLeaveImpact builds the leave-impact payload for a set of P3M
// projects: resource allocations whose user-backed window overlaps active
// leave, and CPM tasks whose assignee has overlapping active leave. Only rows
// with at least one day of overlap are included.
func (h *P3MHandler) p3mComputeLeaveImpact(ctx context.Context, projectIDs []int) (*models.P3MLeaveImpact, error) {
	impact := &models.P3MLeaveImpact{
		GeneratedAt: time.Now().UTC(),
		Resources:   make([]models.P3MLeaveResourceRow, 0),
		Tasks:       make([]models.P3MLeaveTaskRow, 0),
	}
	if len(projectIDs) == 0 {
		return impact, nil
	}

	idArgs := make([]any, len(projectIDs))
	ph := make([]string, len(projectIDs))
	for i, id := range projectIDs {
		idArgs[i] = id
		ph[i] = "?"
	}

	// Resource rows: user-backed allocations per project. Allocation windows
	// are half-open [start, end) like the capacity view; leave periods are
	// inclusive on both ends.
	rows, err := h.db.QueryContext(ctx, `
		SELECT a.resource_id, r.name, r.user_id, u.first_name || ' ' || u.last_name,
		       a.start_date, a.end_date, a.hours_per_week
		FROM p3m_resource_allocations a
		JOIN p3m_resources r ON r.id = a.resource_id
		LEFT JOIN users u ON u.id = r.user_id
		WHERE a.project_id IN (`+joinPlaceholders(ph)+`) AND r.user_id IS NOT NULL`, idArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			resourceID int
			name       string
			userID     sql.NullInt64
			userName   sql.NullString
			start      time.Time
			end        time.Time
			hours      int
		)
		if err := rows.Scan(&resourceID, &name, &userID, &userName, &start, &end, &hours); err != nil {
			return nil, err
		}

		leaves, err := h.p3mActiveLeavePeriods(ctx, int(userID.Int64))
		if err != nil {
			return nil, err
		}

		var overlap int
		kept := make([]models.P3MLeavePeriodRef, 0)
		covered := true
		for _, lp := range leaves {
			if d := p3mOverlapDays(start, end, lp.StartDate, lp.EndDate.Add(cpmDay)); d > 0 {
				overlap += d
				kept = append(kept, lp)
				if lp.SubstituteName == "" {
					covered = false
				}
			}
		}
		if overlap == 0 {
			continue
		}

		row := models.P3MLeaveResourceRow{
			ResourceID:          resourceID,
			Name:                name,
			UserID:              intPtr(int(userID.Int64)),
			AllocationStart:     start,
			AllocationEnd:       end,
			HoursPerWeek:        hours,
			LeavePeriods:        kept,
			OverlapDays:         float64(overlap),
			LostHours:           float64(hours) * float64(overlap) / 7.0,
			CoveredBySubstitute: covered,
		}
		if userName.Valid && userName.String != "" {
			row.UserName = userName.String
		}
		impact.Resources = append(impact.Resources, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Task rows: CPM tasks whose assignee has overlapping active leave. Task
	// windows are inclusive on both ends.
	res, _, err := h.loadCPMSchedule(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	if len(res.tasks) == 0 {
		return impact, nil
	}

	taskArgs := make([]any, len(res.tasks))
	taskPh := make([]string, len(res.tasks))
	for i, t := range res.tasks {
		taskArgs[i] = t.ItemID
		taskPh[i] = "?"
	}

	type assigneeRef struct {
		id   *int
		name string
	}
	assignees := make(map[int]assigneeRef, len(res.tasks))
	aRows, err := h.db.QueryContext(ctx, `
		SELECT i.id, i.assignee_id, u.first_name || ' ' || u.last_name
		FROM items i
		LEFT JOIN users u ON u.id = i.assignee_id
		WHERE i.id IN (`+joinPlaceholders(taskPh)+`)`, taskArgs...)
	if err != nil {
		return nil, err
	}
	for aRows.Next() {
		var (
			itemID   int
			userID   sql.NullInt64
			userName sql.NullString
		)
		if err := aRows.Scan(&itemID, &userID, &userName); err != nil {
			aRows.Close()
			return nil, err
		}
		ref := assigneeRef{}
		if userID.Valid {
			ref.id = intPtr(int(userID.Int64))
		}
		if userName.Valid && userName.String != "" {
			ref.name = userName.String
		}
		assignees[itemID] = ref
	}
	if err := aRows.Err(); err != nil {
		aRows.Close()
		return nil, err
	}
	aRows.Close()

	leavesByUser := make(map[int][]models.P3MLeavePeriodRef)
	for _, t := range res.tasks {
		ref, ok := assignees[t.ItemID]
		if !ok || ref.id == nil {
			continue
		}
		leaves, cached := leavesByUser[*ref.id]
		if !cached {
			leaves, err = h.p3mActiveLeavePeriods(ctx, *ref.id)
			if err != nil {
				return nil, err
			}
			leavesByUser[*ref.id] = leaves
		}

		var overlap int
		kept := make([]models.P3MLeavePeriodRef, 0)
		for _, lp := range leaves {
			if d := p3mOverlapDays(t.StartDate, t.EndDate.Add(cpmDay), lp.StartDate, lp.EndDate.Add(cpmDay)); d > 0 {
				overlap += d
				kept = append(kept, lp)
			}
		}
		if overlap == 0 {
			continue
		}

		impact.Tasks = append(impact.Tasks, models.P3MLeaveTaskRow{
			ItemID:       t.ItemID,
			Title:        t.Title,
			StartDate:    t.StartDate,
			EndDate:      t.EndDate,
			IsCritical:   t.IsCritical,
			AssigneeID:   ref.id,
			AssigneeName: ref.name,
			LeavePeriods: kept,
			OverlapDays:  float64(overlap),
		})
	}

	return impact, nil
}

// intPtr returns a pointer to v.
func intPtr(v int) *int { return &v }

// GetProjectLeaveImpact handles GET /api/p3m/projects/{id}/leave-impact.
func (h *P3MHandler) GetProjectLeaveImpact(w http.ResponseWriter, r *http.Request) {
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

	impact, err := h.p3mComputeLeaveImpact(ctx, []int{id})
	if err != nil {
		var cyc *models.P3MCPMCycleError
		if errors.As(err, &cyc) {
			respondCPMCycleError(w, r, err)
			return
		}
		respondInternalError(w, r, err)
		return
	}
	impact.EntityType = "project"
	impact.EntityID = id
	impact.Title = p.Title
	respondJSONOK(w, impact)
}

// GetProgrammeLeaveImpact handles GET /api/p3m/programmes/{id}/leave-impact.
func (h *P3MHandler) GetProgrammeLeaveImpact(w http.ResponseWriter, r *http.Request) {
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

	impact, err := h.p3mComputeLeaveImpact(ctx, projectIDs)
	if err != nil {
		var cyc *models.P3MCPMCycleError
		if errors.As(err, &cyc) {
			respondCPMCycleError(w, r, err)
			return
		}
		respondInternalError(w, r, err)
		return
	}
	impact.EntityType = "programme"
	impact.EntityID = id
	impact.Title = pg.Title
	respondJSONOK(w, impact)
}
