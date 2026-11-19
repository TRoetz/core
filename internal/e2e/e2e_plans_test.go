package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// plansCreateItem creates an item in wsID through the v2 API and returns its ID.
func plansCreateItem(t *testing.T, c *Client, wsID int, title string) int {
	t.Helper()
	status, body := c.Post(t, "/api/v2/items", map[string]any{
		"workspace_id": wsID,
		"title":        title,
	})
	wantStatus(t, "POST", "/api/v2/items", status, body, http.StatusCreated)
	var res struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode item create response: %v\n%s", err, body)
	}
	return res.Data.ID
}

// UC-O1 — Plan aggregation. Items in a plan come ONLY from linked workspaces
// (exact ID set, search filter respected); P3M and SMS links are display-only
// and must not contribute items; detail/summary rollups stay consistent after
// unlinking. A user without plans grants is denied on all plan endpoints.
func TestPlanAggregation(t *testing.T) {
	admin := e2eEnv.admin

	// Seed: two workspaces (items in both), one P3M project, one SMS stakeholder.
	wsA := createWorkspace(t, admin, "plana")
	wsB := createWorkspace(t, admin, "planb")
	itemA1 := plansCreateItem(t, admin, wsA, "E2E Plan item A-1")
	itemA2 := plansCreateItem(t, admin, wsA, "E2E Plan item A-2")
	itemB1 := plansCreateItem(t, admin, wsB, "E2E Plan item B-1 (must not appear)")

	status, body := admin.Post(t, "/api/p3m/portfolios", map[string]any{"title": "E2E Plan P3M portfolio"})
	wantStatus(t, "POST", "/api/p3m/portfolios", status, body, http.StatusCreated)
	var pf struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(body, &pf); err != nil {
		t.Fatalf("decode p3m portfolio create response: %v\n%s", err, body)
	}

	status, body = admin.Post(t, "/api/p3m/projects", map[string]any{
		"title":        "E2E Plan P3M project",
		"portfolio_id": pf.ID,
	})
	wantStatus(t, "POST", "/api/p3m/projects", status, body, http.StatusCreated)
	var p3m struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(body, &p3m); err != nil {
		t.Fatalf("decode p3m project create response: %v\n%s", err, body)
	}

	status, body = admin.Post(t, "/api/sms/stakeholders", map[string]any{"name": "E2E Plan stakeholder"})
	wantStatus(t, "POST", "/api/sms/stakeholders", status, body, http.StatusCreated)
	var sh struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(body, &sh); err != nil {
		t.Fatalf("decode sms stakeholder create response: %v\n%s", err, body)
	}

	status, body = admin.Post(t, "/api/plans", map[string]any{
		"name":        "E2E Plan O1",
		"description": "plan aggregation coverage",
	})
	wantStatus(t, "POST", "/api/plans", status, body, http.StatusCreated)
	var plan struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatalf("decode plan create response: %v\n%s", err, body)
	}

	link := func(path string, reqBody any) {
		st, b := admin.Post(t, path, reqBody)
		wantStatus(t, "POST", path, st, b, http.StatusNoContent)
	}
	link(fmt.Sprintf("/api/plans/%d/workspaces/%d", plan.ID, wsA), nil)
	link(fmt.Sprintf("/api/plans/%d/p3m-entities", plan.ID), map[string]any{
		"entity_type": "project",
		"entity_id":   p3m.ID,
	})
	link(fmt.Sprintf("/api/plans/%d/stakeholders", plan.ID), map[string]any{"stakeholder_id": sh.ID})

	// The link endpoints return 204 with no body; confirm the join rows were
	// actually persisted.
	if row := dbQueryOne(t, `SELECT workspace_id FROM plan_workspaces WHERE plan_id = ?`, plan.ID); cfInt(row["workspace_id"]) != wsA {
		t.Fatalf("plan_workspaces row for plan %d = %v; want workspace %d", plan.ID, row, wsA)
	}
	if row := dbQueryOne(t, `SELECT entity_type, entity_id FROM plan_p3m_entities WHERE plan_id = ?`, plan.ID); cfStr(row["entity_type"]) != "project" || cfInt(row["entity_id"]) != p3m.ID {
		t.Fatalf("plan_p3m_entities row for plan %d = %v; want project %d", plan.ID, row, p3m.ID)
	}
	if row := dbQueryOne(t, `SELECT stakeholder_id FROM plan_stakeholders WHERE plan_id = ?`, plan.ID); cfInt(row["stakeholder_id"]) != sh.ID {
		t.Fatalf("plan_stakeholders row for plan %d = %v; want stakeholder %d", plan.ID, row, sh.ID)
	}

	t.Run("DetailRollups", func(t *testing.T) {
		st, b := admin.Get(t, fmt.Sprintf("/api/plans/%d/detail", plan.ID))
		wantStatus(t, "GET", "/api/plans/{id}/detail", st, b, http.StatusOK)
		var detail struct {
			Workspaces []struct {
				WorkspaceID int `json:"workspace_id"`
				ItemCount   int `json:"item_count"`
			} `json:"workspaces"`
			P3MEntities []struct {
				EntityType string `json:"entity_type"`
				EntityID   int    `json:"entity_id"`
			} `json:"p3m_entities"`
			Stakeholders []struct {
				StakeholderID int `json:"stakeholder_id"`
			} `json:"stakeholders"`
		}
		if err := json.Unmarshal(b, &detail); err != nil {
			t.Fatalf("decode plan detail: %v\n%s", err, b)
		}
		if len(detail.Workspaces) != 1 || detail.Workspaces[0].WorkspaceID != wsA || detail.Workspaces[0].ItemCount != 2 {
			t.Errorf("detail workspaces = %+v; want exactly [{workspace_id:%d item_count:2}]", detail.Workspaces, wsA)
		}
		if len(detail.P3MEntities) != 1 || detail.P3MEntities[0].EntityType != "project" || detail.P3MEntities[0].EntityID != p3m.ID {
			t.Errorf("detail p3m_entities = %+v; want exactly [{project %d}]", detail.P3MEntities, p3m.ID)
		}
		if len(detail.Stakeholders) != 1 || detail.Stakeholders[0].StakeholderID != sh.ID {
			t.Errorf("detail stakeholders = %+v; want exactly [%d]", detail.Stakeholders, sh.ID)
		}
	})

	t.Run("ItemsOnlyFromLinkedWorkspaces", func(t *testing.T) {
		st, b := admin.Get(t, fmt.Sprintf("/api/plans/%d/items?limit=100", plan.ID))
		wantStatus(t, "GET", "/api/plans/{id}/items", st, b, http.StatusOK)
		var resp struct {
			Items []struct {
				ID  int    `json:"id"`
				Key string `json:"key"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			t.Fatalf("decode plan items: %v\n%s", err, b)
		}
		if resp.Total != 2 || len(resp.Items) != 2 {
			t.Fatalf("plan items total = %d (%d rows); want exactly the 2 wsA items\n%s", resp.Total, len(resp.Items), b)
		}
		got := map[int]bool{}
		for _, it := range resp.Items {
			got[it.ID] = true
		}
		if !got[itemA1] || !got[itemA2] {
			t.Errorf("plan items ids = %v; want exactly {%d, %d}", got, itemA1, itemA2)
		}
		if got[itemB1] {
			t.Errorf("item %d from unlinked workspace leaked into plan items", itemB1)
		}

		// Search filter narrows the same linked-workspace set.
		st, b = admin.Get(t, fmt.Sprintf("/api/plans/%d/items?limit=100&search=A-1", plan.ID))
		wantStatus(t, "GET", "/api/plans/{id}/items?search", st, b, http.StatusOK)
		var filtered struct {
			Items []struct {
				ID int `json:"id"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(b, &filtered); err != nil {
			t.Fatalf("decode filtered plan items: %v\n%s", err, b)
		}
		if filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].ID != itemA1 {
			t.Errorf("search=A-1 returned %+v; want exactly [%d]", filtered, itemA1)
		}
	})

	t.Run("SummaryConsistent", func(t *testing.T) {
		st, b := admin.Get(t, fmt.Sprintf("/api/plans/%d/summary", plan.ID))
		wantStatus(t, "GET", "/api/plans/{id}/summary", st, b, http.StatusOK)
		var sum struct {
			TotalItems  int     `json:"total_items"`
			DoneItems   int     `json:"done_items"`
			ProgressPct float64 `json:"progress_pct"`
		}
		if err := json.Unmarshal(b, &sum); err != nil {
			t.Fatalf("decode plan summary: %v\n%s", err, b)
		}
		if sum.TotalItems != 2 || sum.DoneItems != 0 || sum.ProgressPct != 0 {
			t.Errorf("summary = %+v; want total_items=2 done_items=0 progress_pct=0 (both items incomplete)", sum)
		}
	})

	t.Run("UnlinkWorkspaceDropsItems", func(t *testing.T) {
		st, b := admin.Delete(t, fmt.Sprintf("/api/plans/%d/workspaces/%d", plan.ID, wsA))
		wantStatus(t, "DELETE", "/api/plans/{id}/workspaces/{ws}", st, b, http.StatusNoContent)

		st, b = admin.Get(t, fmt.Sprintf("/api/plans/%d/items?limit=100", plan.ID))
		wantStatus(t, "GET", "/api/plans/{id}/items after unlink", st, b, http.StatusOK)
		var resp struct {
			Items []struct {
				ID int `json:"id"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			t.Fatalf("decode plan items after unlink: %v\n%s", err, b)
		}
		if resp.Total != 0 || len(resp.Items) != 0 {
			t.Errorf("items after unlink = %+v; want empty (P3M/SMS links must not contribute items)", resp)
		}

		// Deleting an already-unlinked workspace is a 404, not a silent no-op.
		st, b = admin.Delete(t, fmt.Sprintf("/api/plans/%d/workspaces/%d", plan.ID, wsA))
		wantStatus(t, "DELETE", "/api/plans/{id}/workspaces/{ws} again", st, b, http.StatusNotFound)
	})

	t.Run("Negatives", func(t *testing.T) {
		st, b := admin.Post(t, "/api/plans", map[string]any{"description": "missing name"})
		wantStatus(t, "POST", "/api/plans without name", st, b, http.StatusBadRequest)

		st, b = admin.Post(t, fmt.Sprintf("/api/plans/%d/workspaces/999999", plan.ID), nil)
		wantStatus(t, "POST", "/api/plans/{id}/workspaces/999999", st, b, http.StatusNotFound)
	})

	t.Run("PermissionDeniedWithoutPlansGrants", func(t *testing.T) {
		createUser(t, "e2e-plan-noperm@example.test", "e2eplannoperm", "E2ePlanNoperm123!")
		c := login(t, "e2eplannoperm", "E2ePlanNoperm123!", "e2eplannoperm")

		st, b := c.Get(t, "/api/plans")
		wantStatus(t, "GET", "/api/plans without grants", st, b, http.StatusUnauthorized, http.StatusForbidden)

		st, b = c.Post(t, "/api/plans", map[string]any{"name": "E2E Plan denied"})
		wantStatus(t, "POST", "/api/plans create without grants", st, b, http.StatusUnauthorized, http.StatusForbidden)

		st, b = c.Get(t, fmt.Sprintf("/api/plans/%d/detail", plan.ID))
		wantStatus(t, "GET", "/api/plans/{id}/detail without grants", st, b, http.StatusUnauthorized, http.StatusForbidden)
	})
}
