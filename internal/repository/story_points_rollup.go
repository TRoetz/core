package repository

import (
	"context"
	"fmt"
	"strings"
)

// StoryPointsByAssigneeRow is one assignee's open-item story-point total.
type StoryPointsByAssigneeRow struct {
	AssigneeID  int     `json:"assignee_id"`
	DisplayName string  `json:"display_name"`
	TotalPoints float64 `json:"total_points"`
	ItemCount   int     `json:"item_count"`
}

// GroupOpenStoryPointsByAssignee sums story points of open (non-completed
// status) items per assignee across the given workspace IDs. Callers must
// pre-filter to the user's accessible workspaces. workspaceIDs empty → no rows.
func (r *ItemRepository) GroupOpenStoryPointsByAssignee(workspaceIDs []int) ([]StoryPointsByAssigneeRow, error) {
	if len(workspaceIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(workspaceIDs)), ",")
	query := fmt.Sprintf(`
		SELECT i.assignee_id,
		       COALESCE(NULLIF(TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), u.username, '') AS display_name,
		       COALESCE(SUM(i.story_points), 0) AS total_points,
		       COUNT(*) AS item_count
		FROM items i
		JOIN statuses s ON s.id = i.status_id
		JOIN status_categories sc ON sc.id = s.category_id
		JOIN users u ON u.id = i.assignee_id
		WHERE i.assignee_id IS NOT NULL
		  AND i.story_points IS NOT NULL
		  AND COALESCE(sc.is_completed, FALSE) = FALSE
		  AND i.workspace_id IN (%s)
		GROUP BY i.assignee_id, display_name
		ORDER BY total_points DESC, display_name ASC`, placeholders)

	args := make([]any, len(workspaceIDs))
	for i, id := range workspaceIDs {
		args[i] = id
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("group story points by assignee: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []StoryPointsByAssigneeRow
	for rows.Next() {
		var row StoryPointsByAssigneeRow
		if err := rows.Scan(&row.AssigneeID, &row.DisplayName, &row.TotalPoints, &row.ItemCount); err != nil {
			return nil, fmt.Errorf("scan story points by assignee: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("group story points by assignee: %w", err)
	}
	return out, nil
}

// StoryPointsRollup is the recursive rollup of an item's descendants' story
// points. Points sums every descendant that carries points; Contributors
// counts those point-carrying descendants (at any depth).
type StoryPointsRollup struct {
	Points       float64 `json:"points"`
	Contributors int     `json:"contributors"`
}

// SumDescendantStoryPoints aggregates story points across ALL descendants of
// an item (multi-level) with one capped recursive CTE, mirroring the cycle
// guard in GetDescendantsWithMaxDepthContext. Cost is bounded by the
// subtree size — call sites must be per-item detail loads, never list
// projections (GH #256).
func (r *ItemRepository) SumDescendantStoryPoints(ctx context.Context, parentID int) (StoryPointsRollup, error) {
	var rollup StoryPointsRollup
	err := r.db.QueryRowContext(ctx, `
		WITH RECURSIVE descendants AS (
			SELECT id, parent_id, story_points, 1 as level
			FROM items
			WHERE parent_id = ?
			UNION ALL
			SELECT i.id, i.parent_id, i.story_points, d.level + 1
			FROM items i
			INNER JOIN descendants d ON i.parent_id = d.id
			WHERE d.level < ?
		)
		SELECT COALESCE(SUM(story_points), 0), COUNT(*)
		FROM descendants
		WHERE story_points IS NOT NULL`, parentID, maxItemHierarchyDepth,
	).Scan(&rollup.Points, &rollup.Contributors)
	if err != nil {
		return StoryPointsRollup{}, fmt.Errorf("sum descendant story points: %w", err)
	}
	return rollup, nil
}
