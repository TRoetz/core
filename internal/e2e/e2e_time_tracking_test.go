package e2e

// End-to-end use-case coverage for time tracking (UC-F1..F2): project
// categories + time projects, worklogs with per-project rollups, and the
// start/stop timer flow that books a worklog. Runs against the real server on
// a fresh SQLite database via the shared harness.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func ttName(t *testing.T, suffix string) string {
	t.Helper()
	base := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	return fmt.Sprintf("e2e-tt-%s-%s", base, suffix)
}

// ttText coerces a dbQueryOne value to its text form (the driver returns TEXT
// columns as string).
func ttText(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprint(v)
	}
}

// TestTimeTracking covers UC-F1 (categories + projects) and UC-F2
// (worklogs + timers).
func TestTimeTracking(t *testing.T) {
	admin := e2eEnv.admin

	t.Run("ProjectCategoriesAndProjects", func(t *testing.T) { // UC-F1
		catName := ttName(t, "cat")
		status, body := admin.Post(t, "/api/v2/time/project-categories", map[string]any{
			"name": catName, "description": "e2e category", "color": "#123456",
		})
		wantStatus(t, "POST", "/api/v2/time/project-categories", status, body, http.StatusCreated)
		catID := v2ID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/time/project-categories/%d", catID))
		wantStatus(t, "GET", "/api/v2/time/project-categories/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", catName)

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/time/project-categories/%d", catID), map[string]any{
			"description": "e2e category (updated)",
		})
		wantStatus(t, "PATCH", "/api/v2/time/project-categories/{id}", status, body, http.StatusOK)

		// A project may not reference a missing category.
		status, body = admin.Post(t, "/api/v2/time/projects", map[string]any{
			"name": ttName(t, "badcat"), "status": "Active", "category_id": 999999,
		})
		wantStatus(t, "POST", "/api/v2/time/projects (missing category)", status, body, http.StatusBadRequest)

		projName := ttName(t, "proj")
		status, body = admin.Post(t, "/api/v2/time/projects", map[string]any{
			"name": projName, "status": "Active", "hourly_rate": 50, "category_id": catID,
		})
		wantStatus(t, "POST", "/api/v2/time/projects", status, body, http.StatusCreated)
		assertJSONField(t, body, "data.name", projName)
		// No customer supplied: the builtin no-customer placeholder is assigned.
		assertJSONField(t, body, "data.customer_builtin_key", "no_customer")
		tpID := v2ID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/time/projects/%d", tpID))
		wantStatus(t, "GET", "/api/v2/time/projects/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.category_id", catID)

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/time/projects/%d", tpID), map[string]any{
			"description": "e2e project (updated)",
		})
		wantStatus(t, "PATCH", "/api/v2/time/projects/{id}", status, body, http.StatusOK)

		// A throwaway category can be deleted.
		status, body = admin.Post(t, "/api/v2/time/project-categories", map[string]any{
			"name": ttName(t, "throwaway"),
		})
		wantStatus(t, "POST", "/api/v2/time/project-categories (throwaway)", status, body, http.StatusCreated)
		throwawayID := v2ID(t, body)
		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/time/project-categories/%d", throwawayID))
		wantStatus(t, "DELETE", "/api/v2/time/project-categories/{id}", status, nil, http.StatusNoContent, http.StatusOK)

		row := dbQueryOne(t, "SELECT name, category_id FROM time_projects WHERE id = ?", tpID)
		if got := ttText(row["name"]); got != projName {
			t.Fatalf("time_projects.name = %q, want %q", got, projName)
		}
		if got := row["category_id"].(int64); int(got) != catID {
			t.Fatalf("time_projects.category_id = %d, want %d", got, catID)
		}
	})

	t.Run("WorklogsAndTimers", func(t *testing.T) { // UC-F2
		wsID := createWorkspace(t, admin, wscfgWSKey(t))
		status, body := admin.Post(t, "/api/v2/time/projects", map[string]any{
			"name": ttName(t, "bookproj"), "status": "Active",
		})
		wantStatus(t, "POST", "/api/v2/time/projects (booking)", status, body, http.StatusCreated)
		tpID := v2ID(t, body)

		const date = "2026-11-13"
		status, body = admin.Post(t, "/api/v2/time/worklogs", map[string]any{
			"project_id": tpID, "description": "e2e worklog one", "date": date,
			"start_time": "09:00", "end_time": "10:30",
		})
		wantStatus(t, "POST", "/api/v2/time/worklogs", status, body, http.StatusCreated)
		assertJSONField(t, body, "data.duration_minutes", 90)
		wlID := v2ID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/time/worklogs/%d", wlID))
		wantStatus(t, "GET", "/api/v2/time/worklogs/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.project_id", tpID)

		row := dbQueryOne(t, "SELECT project_id, duration_minutes FROM time_worklogs WHERE id = ?", wlID)
		if got := row["project_id"].(int64); int(got) != tpID {
			t.Fatalf("time_worklogs.project_id = %d, want %d", got, tpID)
		}
		if got := row["duration_minutes"].(int64); got != 90 {
			t.Fatalf("time_worklogs.duration_minutes = %d, want 90", got)
		}

		// A worklog linked to an item is listed on that item.
		status, body = admin.Post(t, "/api/v2/items", map[string]any{
			"workspace_id": wsID, "title": ttName(t, "item"),
		})
		wantStatus(t, "POST", "/api/v2/items (worklog link)", status, body, http.StatusCreated)
		itemID := v2ID(t, body)
		status, body = admin.Post(t, "/api/v2/time/worklogs", map[string]any{
			"project_id": tpID, "item_id": itemID, "description": "e2e worklog two",
			"date": date, "start_time": "11:00", "end_time": "11:30",
		})
		wantStatus(t, "POST", "/api/v2/time/worklogs (item)", status, body, http.StatusCreated)
		wl2 := v2ID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/items/%d/worklogs", itemID))
		wantStatus(t, "GET", "/api/v2/items/{id}/worklogs", status, body, http.StatusOK)
		env := mustJSON(t, body).(map[string]any)
		found := false
		for _, entry := range env["data"].([]any) {
			if id, _ := entry.(map[string]any)["id"].(float64); int(id) == wl2 {
				found = true
			}
		}
		if !found {
			t.Fatalf("item worklog list does not contain worklog %d: %s", wl2, snippet(body))
		}

		// Per-project rollup equals the sum of worklog durations (90+30 min).
		status, body = admin.Get(t, fmt.Sprintf("/api/v2/time/projects/%d", tpID))
		wantStatus(t, "GET", "/api/v2/time/projects/{id} (rollup)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.total_hours", 2.0)

		// Updating the first worklog's interval updates the rollup (60+30 min).
		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/time/worklogs/%d", wlID), map[string]any{
			"date": date, "start_time": "09:00", "end_time": "10:00",
		})
		wantStatus(t, "PATCH", "/api/v2/time/worklogs/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.duration_minutes", 60)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/time/projects/%d", tpID))
		wantStatus(t, "GET", "/api/v2/time/projects/{id} (rollup after patch)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.total_hours", 1.5)

		// Deleting a worklog drops the rollup and the row.
		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/time/worklogs/%d", wl2))
		wantStatus(t, "DELETE", "/api/v2/time/worklogs/{id}", status, nil, http.StatusNoContent, http.StatusOK)
		if n := dbCount(t, "time_worklogs", "project_id = ?", tpID); n != 1 {
			t.Fatalf("time_worklogs count for project %d = %d, want 1", tpID, n)
		}
		status, body = admin.Get(t, fmt.Sprintf("/api/v2/time/projects/%d", tpID))
		wantStatus(t, "GET", "/api/v2/time/projects/{id} (rollup after delete)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.total_hours", 1.0)

		// Negative: only one of start_time/end_time is provided.
		status, body = admin.Post(t, "/api/v2/time/worklogs", map[string]any{
			"project_id": tpID, "date": date, "start_time": "10:00",
		})
		wantStatus(t, "POST", "/api/v2/time/worklogs (missing end)", status, body, http.StatusBadRequest)

		// Negative: zero duration with no times.
		status, body = admin.Post(t, "/api/v2/time/worklogs", map[string]any{
			"project_id": tpID, "date": date, "duration_minutes": 0,
		})
		wantStatus(t, "POST", "/api/v2/time/worklogs (zero duration)", status, body, http.StatusBadRequest)

		// Timer: stopping with no active timer is a 404.
		status, body = admin.Post(t, "/api/v2/time/timers/active/stop", nil)
		wantStatus(t, "POST", "/api/v2/time/timers/active/stop (none)", status, body, http.StatusNotFound)

		before := dbCount(t, "time_worklogs", "project_id = ?", tpID)
		status, body = admin.Post(t, "/api/v2/time/timers", map[string]any{
			"workspace_id": wsID, "project_id": tpID, "description": "e2e timer run",
		})
		wantStatus(t, "POST", "/api/v2/time/timers", status, body, http.StatusCreated)
		assertJSONField(t, body, "data.project_id", tpID)

		status, body = admin.Get(t, "/api/v2/time/timers/active")
		wantStatus(t, "GET", "/api/v2/time/timers/active", status, body, http.StatusOK)
		assertJSONField(t, body, "data.project_id", tpID)

		// A second concurrent start is rejected.
		status, body = admin.Post(t, "/api/v2/time/timers", map[string]any{
			"workspace_id": wsID, "project_id": tpID, "description": "e2e timer run (second)",
		})
		wantStatus(t, "POST", "/api/v2/time/timers (already running)", status, body, http.StatusConflict)

		time.Sleep(1100 * time.Millisecond)
		status, body = admin.Post(t, "/api/v2/time/timers/active/stop", nil)
		wantStatus(t, "POST", "/api/v2/time/timers/active/stop", status, body, http.StatusOK)
		assertJSONField(t, body, "data.stopped", true)
		assertJSONField(t, body, "data.worklog_created", true)

		if n := dbCount(t, "time_worklogs", "project_id = ?", tpID); n != before+1 {
			t.Fatalf("timer stop did not book a worklog: count = %d, want %d", n, before+1)
		}

		status, body = admin.Get(t, "/api/v2/time/timers/active")
		wantStatus(t, "GET", "/api/v2/time/timers/active (after stop)", status, body, http.StatusOK)
		if env := mustJSON(t, body).(map[string]any); env["data"] != nil {
			t.Fatalf("expected no active timer after stop: %s", snippet(body))
		}
	})
}
