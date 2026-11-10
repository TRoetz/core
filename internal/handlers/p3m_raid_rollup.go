package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// RAID roll-up across a programme or portfolio (RAID-006).
// ---------------------------------------------------------------------------

// p3mRollupProjectIDs returns the child project ids under a programme or
// portfolio.
func (h *P3MHandler) p3mRollupProjectIDs(ctx context.Context, entityType string, entityID int) ([]int, error) {
	var query string
	switch entityType {
	case "programme":
		query = `SELECT id FROM p3m_projects WHERE programme_id = ?`
	case "portfolio":
		query = `SELECT id FROM p3m_projects WHERE portfolio_id = ?`
	default:
		return nil, fmt.Errorf("unsupported rollup entity type %q", entityType)
	}

	rows, err := h.db.QueryContext(ctx, query, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]int, 0)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// p3mRaidRollup aggregates every open RAID item at the entity's own tier plus
// across all child projects: totals by type and status, escalated count,
// overdue items (due date passed while still open), high-priority items and
// the top ten risks by score.
func (h *P3MHandler) p3mRaidRollup(w http.ResponseWriter, r *http.Request, entityType string) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	projectIDs, err := h.p3mRollupProjectIDs(ctx, entityType, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	query := `SELECT ` + p3mRaidColumns + ` FROM p3m_raid_items WHERE status IN ('open', 'mitigating', 'escalated') AND (entity_type = ? AND entity_id = ?`
	args := []any{entityType, id}
	if len(projectIDs) > 0 {
		query += ` OR (entity_type = 'project' AND entity_id IN (` + joinPlaceholders(make([]string, len(projectIDs))) + `))`
		for _, pid := range projectIDs {
			args = append(args, pid)
		}
	} else {
		query += `)`
	}

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]models.P3MRaidItem, 0)
	for rows.Next() {
		item, err := scanP3MRaidItem(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		p3mApplyComputedRaid(&item)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	today := time.Now().UTC().Truncate(cpmDay)
	byItemType := map[string]int{}
	byStatus := map[string]int{}
	escalated := 0
	overdue := make([]map[string]any, 0)
	highPriority := make([]models.P3MRaidItem, 0)
	risks := make([]models.P3MRaidItem, 0)

	for _, item := range items {
		byItemType[item.ItemType]++
		byStatus[item.Status]++
		if item.Status == "escalated" {
			escalated++
		}
		if item.DueDate != nil && item.DueDate.UTC().Truncate(cpmDay).Before(today) {
			overdue = append(overdue, map[string]any{
				"id":           item.ID,
				"title":        item.Title,
				"item_type":    item.ItemType,
				"entity_type":  item.EntityType,
				"entity_id":    item.EntityID,
				"due_date":     item.DueDate.Format("2006-01-02"),
				"risk_score":   item.RiskScore,
			})
		}
		if item.PriorityBucket == "high" {
			highPriority = append(highPriority, item)
		}
		if item.ItemType == "risk" {
			risks = append(risks, item)
		}
	}

	sortRaidByScoreDesc(highPriority)
	sortRaidByScoreDesc(risks)
	topRisks := risks
	if len(topRisks) > 10 {
		topRisks = topRisks[:10]
	}

	respondJSONOK(w, map[string]any{
		"entity_type":    entityType,
		"entity_id":      id,
		"total_open":     len(items),
		"by_item_type":   byItemType,
		"by_status":      byStatus,
		"escalated_count": escalated,
		"overdue":        overdue,
		"high_priority":  highPriority,
		"top_risks":      topRisks,
	})
}

// GetProgrammeRaidRollup handles GET /api/p3m/programmes/{id}/raid-rollup.
func (h *P3MHandler) GetProgrammeRaidRollup(w http.ResponseWriter, r *http.Request) {
	h.p3mRaidRollup(w, r, "programme")
}

// GetPortfolioRaidRollup handles GET /api/p3m/portfolios/{id}/raid-rollup.
func (h *P3MHandler) GetPortfolioRaidRollup(w http.ResponseWriter, r *http.Request) {
	h.p3mRaidRollup(w, r, "portfolio")
}

// sortRaidByScoreDesc orders items by risk score descending, id ascending as a
// stable tie-break.
func sortRaidByScoreDesc(items []models.P3MRaidItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].RiskScore > items[j-1].RiskScore || (items[j].RiskScore == items[j-1].RiskScore && items[j].ID < items[j-1].ID); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// ---------------------------------------------------------------------------
// CPM what-if preview (RAID-004): re-run the schedule with hypothetical date or
// duration changes, without persisting anything.
// ---------------------------------------------------------------------------

type p3mCpmPreviewChange struct {
	ItemID       int     `json:"item_id"`
	NewStartDate *string `json:"new_start_date"` // YYYY-MM-DD
	DurationDays *int    `json:"duration_days"`  // inclusive day count, >= 1
}

// GetCpmPreview handles POST /api/p3m/projects/{id}/cpm-preview. The body is
// {changes: [{item_id, new_start_date?, duration_days?}]}; each change may set
// a new start date and/or a new inclusive duration. The baseline schedule and
// the preview are both computed server-side so clients can show the completion
// impact of a proposed change before it is made.
func (h *P3MHandler) GetCpmPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	var in struct {
		Changes []p3mCpmPreviewChange `json:"changes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if len(in.Changes) == 0 {
		respondValidationError(w, r, "changes is required (at least one item change)")
		return
	}

	baselineRes, _, err := h.loadCPMSchedule(ctx, []int{id})
	if err != nil {
		var cpe *models.P3MCPMCycleError
		if errors.As(err, &cpe) {
			respondCPMCycleError(w, r, err)
			return
		}
		respondInternalError(w, r, err)
		return
	}

	nodes, edges, err := h.loadCPMNodesAndEdges(ctx, []int{id})
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	byID := make(map[int]*cpmNode, len(nodes))
	for i := range nodes {
		byID[nodes[i].id] = &nodes[i]
	}

	for _, ch := range in.Changes {
		if ch.NewStartDate == nil && ch.DurationDays == nil {
			respondValidationError(w, r, fmt.Sprintf("change for item %d must set new_start_date and/or duration_days", ch.ItemID))
			return
		}
		n, exists := byID[ch.ItemID]
		if !exists {
			respondValidationError(w, r, fmt.Sprintf("item %d is not a dated work item in this project", ch.ItemID))
			return
		}
		if ch.NewStartDate != nil && *ch.NewStartDate != "" {
			d, perr := time.Parse("2006-01-02", *ch.NewStartDate)
			if perr != nil {
				respondValidationError(w, r, "new_start_date must be YYYY-MM-DD")
				return
			}
			n.start = d.UTC().Truncate(cpmDay)
		}
		if ch.DurationDays != nil {
			if *ch.DurationDays < 1 {
				respondValidationError(w, r, fmt.Sprintf("duration_days for item %d must be >= 1", ch.ItemID))
				return
			}
			n.end = n.start.Add(time.Duration(int64(*ch.DurationDays)-1) * cpmDay)
		}
		if n.end.Before(n.start) {
			respondValidationError(w, r, fmt.Sprintf("item %d would end before it starts", ch.ItemID))
			return
		}
	}

	previewRes, err := computeCPM(nodes, edges)
	if err != nil {
		var cpe *models.P3MCPMCycleError
		if errors.As(err, &cpe) {
			respondCPMCycleError(w, r, err)
			return
		}
		respondInternalError(w, r, err)
		return
	}

	out := map[string]any{
		"baseline": p3mCpmSummary(baselineRes),
		"preview":  p3mCpmSummary(previewRes),
	}
	if len(baselineRes.tasks) > 0 && len(previewRes.tasks) > 0 {
		out["delta_days"] = int(previewRes.projectEnd.Sub(baselineRes.projectEnd).Hours() / 24)
	}
	respondJSONOK(w, out)
}

// p3mCpmSummary shapes one CPM result for the preview response.
func p3mCpmSummary(res *cpmResult) map[string]any {
	if len(res.tasks) == 0 {
		return map[string]any{"task_count": 0, "critical_path": []int{}}
	}
	return map[string]any{
		"project_start": res.projectStart.Format("2006-01-02"),
		"project_end":   res.projectEnd.Format("2006-01-02"),
		"span_days":     cpmSpanDays(res),
		"task_count":    len(res.tasks),
		"critical_path": res.criticalIDs,
	}
}
