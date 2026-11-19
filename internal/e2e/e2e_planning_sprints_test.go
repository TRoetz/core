package e2e

// Planning & sprints use-case coverage (UC-E1..E3): milestone and iteration
// CRUD with validation, progress/test-statistics reports driven by real item
// assignments, and the review + calendar-feed loop.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPlanningSprints(t *testing.T) {
	admin := e2eEnv.admin
	wsID := createWorkspace(t, admin, wscfgWSKey(t))
	itID, aID, bID, cID := d2WorkflowSetup(t, admin, wsID)

	var iterID, msID int

	t.Run("IterationLifecycle", func(t *testing.T) {
		status, body := admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/iterations", wsID), map[string]any{
			"name": wscfgName(t, "iter"), "description": "E2E sprint",
			"start_date": "2026-11-09", "end_date": "2026-11-23", "status": "planned",
		})
		wantStatus(t, "POST", "/api/v2/workspaces/{ws}/iterations", status, body, http.StatusCreated)
		iterID = v2ID(t, body)

		row := dbQueryOne(t, `SELECT name, start_date, end_date, status, workspace_id FROM iterations WHERE id = ?`, iterID)
		if row["status"] != "planned" || fmt.Sprint(row["workspace_id"]) != fmt.Sprint(wsID) {
			t.Fatalf("iterations row after create: %v", row)
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/iterations", wsID))
		wantStatus(t, "GET", "/api/v2/workspaces/{ws}/iterations", status, body, http.StatusOK)
		if !strings.Contains(string(body), fmt.Sprint(iterID)) {
			t.Fatalf("iteration list missing %d: %s", iterID, snippet(body))
		}

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/workspaces/%d/iterations/%d", wsID, iterID), map[string]any{"status": "active"})
		wantStatus(t, "PATCH", "/api/v2/workspaces/{ws}/iterations/{id} (planned->active)", status, body, http.StatusOK)
		row = dbQueryOne(t, `SELECT status FROM iterations WHERE id = ?`, iterID)
		if row["status"] != "active" {
			t.Fatalf("iterations.status after patch: %v", row)
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/iterations", wsID), map[string]any{
			"name": wscfgName(t, "iter-bad"), "start_date": "2026-11-23", "end_date": "2026-11-09",
		})
		wantStatus(t, "POST", "/api/v2/workspaces/{ws}/iterations (end<start)", status, body, http.StatusBadRequest)

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/workspaces/%d/iterations/%d", wsID, iterID), map[string]any{"status": "bogus"})
		wantStatus(t, "PATCH", "/api/v2/workspaces/{ws}/iterations/{id} (bad status)", status, body, http.StatusBadRequest)
		row = dbQueryOne(t, `SELECT status FROM iterations WHERE id = ?`, iterID)
		if row["status"] != "active" {
			t.Fatalf("rejected patch changed status: %v", row)
		}
	})

	t.Run("MilestoneLifecycle", func(t *testing.T) {
		status, body := admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/milestones", wsID), map[string]any{
			"name": wscfgName(t, "ms"), "description": "E2E milestone",
			"target_date": "2026-12-31", "status": "planning",
		})
		wantStatus(t, "POST", "/api/v2/workspaces/{ws}/milestones", status, body, http.StatusCreated)
		msID = v2ID(t, body)

		row := dbQueryOne(t, `SELECT name, target_date, status, workspace_id FROM milestones WHERE id = ?`, msID)
		if row["status"] != "planning" || fmt.Sprint(row["workspace_id"]) != fmt.Sprint(wsID) {
			t.Fatalf("milestones row after create: %v", row)
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/milestones", wsID))
		wantStatus(t, "GET", "/api/v2/workspaces/{ws}/milestones", status, body, http.StatusOK)
		if !strings.Contains(string(body), fmt.Sprint(msID)) {
			t.Fatalf("milestone list missing %d: %s", msID, snippet(body))
		}

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/workspaces/%d/milestones/%d", wsID, msID), map[string]any{"status": "in-progress"})
		wantStatus(t, "PATCH", "/api/v2/workspaces/{ws}/milestones/{id} (planning->in-progress)", status, body, http.StatusOK)
		row = dbQueryOne(t, `SELECT status FROM milestones WHERE id = ?`, msID)
		if row["status"] != "in-progress" {
			t.Fatalf("milestones.status after patch: %v", row)
		}

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/workspaces/%d/milestones/%d", wsID, msID), map[string]any{"status": "planned"})
		wantStatus(t, "PATCH", "/api/v2/workspaces/{ws}/milestones/{id} (iteration status rejected)", status, body, http.StatusBadRequest)
	})

	t.Run("ProgressReports", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/items", map[string]any{
			"workspace_id": wsID, "title": wscfgName(t, "progress-item"),
			"item_type_id": itID, "status_id": aID,
		})
		wantStatus(t, "POST", "/api/v2/items (planning)", status, body, http.StatusCreated)
		itemID := v2ID(t, body)

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/items/%d", itemID), map[string]any{"iteration_id": iterID})
		wantStatus(t, "PATCH", "/api/v2/items/{id} (schedule to iteration)", status, body, http.StatusOK)
		row := dbQueryOne(t, `SELECT iteration_id FROM items WHERE id = ?`, itemID)
		if fmt.Sprint(row["iteration_id"]) != fmt.Sprint(iterID) {
			t.Fatalf("items.iteration_id after patch: %v", row)
		}

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/items/%d", itemID), map[string]any{"milestone_ids": []int{msID}})
		wantStatus(t, "PATCH", "/api/v2/items/{id} (attach milestone)", status, body, http.StatusOK)
		if n := dbCount(t, "item_milestones", "item_id = ? AND milestone_id = ?", itemID, msID); n != 1 {
			t.Fatalf("item_milestones rows for (%d,%d): %d", itemID, msID, n)
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/iterations/%d/progress", iterID))
		wantStatus(t, "GET", "/api/v2/iterations/{id}/progress", status, body, http.StatusOK)
		if !strings.Contains(string(body), `"total_items":1`) {
			t.Fatalf("iteration progress missing total_items=1: %s", snippet(body))
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/milestones/%d/progress", msID))
		wantStatus(t, "GET", "/api/v2/milestones/{id}/progress", status, body, http.StatusOK)
		if !strings.Contains(string(body), `"total_items":1`) {
			t.Fatalf("milestone progress missing total_items=1: %s", snippet(body))
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/milestones/%d/test-statistics", msID))
		wantStatus(t, "GET", "/api/v2/milestones/{id}/test-statistics", status, body, http.StatusOK)
		for _, field := range []string{`"total_test_plans":0`, `"total_test_runs":0`, `"successful_test_runs":0`, `"failed_test_runs":0`, `"in_progress_test_runs":0`, `"total_test_cases":0`} {
			if !strings.Contains(string(body), field) {
				t.Fatalf("test-statistics missing %s: %s", field, snippet(body))
			}
		}
	})

	t.Run("ReviewsAndCalendarFeed", func(t *testing.T) {
		// Mark the final workflow status's category as completed so that an
		// A->B->C transition counts as a completion event.
		catRow := dbQueryOne(t, `SELECT category_id FROM statuses WHERE id = ?`, cID)
		status, body := v2Patch(t, admin, fmt.Sprintf("/api/v2/status-categories/%d", catRow["category_id"]), map[string]any{"is_completed": true})
		wantStatus(t, "PATCH", "/api/v2/status-categories/{id} (is_completed)", status, body, http.StatusOK)

		status, body = admin.Post(t, "/api/reviews", map[string]any{
			"review_date": "2026-11-12", "review_type": "daily",
			"review_data": `{"summary":"e2e daily review"}`,
		})
		wantStatus(t, "POST", "/api/reviews", status, body, http.StatusCreated)
		reviewID := firstID(body)
		if reviewID == 0 {
			t.Fatalf("no id in review create response: %s", snippet(body))
		}

		status, body = admin.Post(t, "/api/reviews", map[string]any{
			"review_date": "2026-11-12", "review_type": "daily",
			"review_data": `{"summary":"duplicate"}`,
		})
		wantStatus(t, "POST", "/api/reviews (duplicate date+type)", status, body, http.StatusConflict)

		status, body = admin.Post(t, "/api/reviews", map[string]any{
			"review_date": "2026-11-13", "review_type": "monthly",
			"review_data": `{"summary":"bad type"}`,
		})
		wantStatus(t, "POST", "/api/reviews (invalid type)", status, body, http.StatusBadRequest)

		status, body = admin.Put(t, fmt.Sprintf("/api/reviews/%d", reviewID), map[string]any{
			"review_data": `{"summary":"updated e2e review"}`,
		})
		wantStatus(t, "PUT", "/api/reviews/{id}", status, body, http.StatusOK)
		row := dbQueryOne(t, `SELECT review_data FROM reviews WHERE id = ?`, reviewID)
		if !strings.Contains(fmt.Sprint(row["review_data"]), "updated e2e review") {
			t.Fatalf("reviews.review_data after update: %v", row)
		}

		// Completed-items journey: an item assigned to the admin that is
		// transitioned into a completed status must appear in the range.
		adminID := int(dbQueryOne(t, `SELECT id FROM users WHERE email = ?`, adminEmail)["id"].(int64))
		status, body = admin.Post(t, "/api/v2/items", map[string]any{
			"workspace_id": wsID, "title": wscfgName(t, "completed-item"),
			"item_type_id": itID, "status_id": aID, "assignee_id": adminID,
		})
		wantStatus(t, "POST", "/api/v2/items (assigned to admin)", status, body, http.StatusCreated)
		itemID := v2ID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/items/%d/transition", itemID), map[string]any{"to_status_id": bID})
		wantStatus(t, "POST", "/api/v2/items/{id}/transition (A->B)", status, body, http.StatusOK)
		status, body = admin.Post(t, fmt.Sprintf("/api/v2/items/%d/transition", itemID), map[string]any{"to_status_id": cID})
		wantStatus(t, "POST", "/api/v2/items/{id}/transition (B->C completed)", status, body, http.StatusOK)

		now := time.Now()
		status, body = admin.Get(t, fmt.Sprintf("/api/reviews/completed-items?start_date=%s&end_date=%s",
			now.AddDate(0, 0, -7).Format("2006-01-02"), now.AddDate(0, 0, 7).Format("2006-01-02")))
		wantStatus(t, "GET", "/api/reviews/completed-items", status, body, http.StatusOK)
		if !strings.Contains(string(body), wscfgName(t, "completed-item")) {
			t.Fatalf("completed-items missing the completed item: %s", snippet(body))
		}

		status, body = admin.Post(t, "/api/calendar/feed/token", nil)
		wantStatus(t, "POST", "/api/calendar/feed/token", status, body, http.StatusCreated)
		var feedResp struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(body, &feedResp); err != nil || feedResp.Token == "" {
			t.Fatalf("no token in feed create response: %s", snippet(body))
		}

		feedURL := e2eEnv.baseURL + "/api/calendar/feed/" + feedResp.Token
		resp, err := http.Get(feedURL) // public endpoint: token auth only
		if err != nil {
			t.Fatalf("GET %s: %v", feedURL, err)
		}
		icsBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(icsBody), "BEGIN:VCALENDAR") {
			t.Fatalf("ICS feed status=%d body=%s", resp.StatusCode, snippet(icsBody))
		}

		status, body = admin.Delete(t, "/api/calendar/feed/token")
		wantStatus(t, "DELETE", "/api/calendar/feed/token", status, body, http.StatusNoContent)

		resp, err = http.Get(feedURL)
		if err != nil {
			t.Fatalf("GET %s after revoke: %v", feedURL, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("revoked token still serves feed: status=%d", resp.StatusCode)
		}
	})
}
