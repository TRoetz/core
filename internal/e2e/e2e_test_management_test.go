package e2e

// End-to-end use-case coverage for test management (UC-J1..J3): test case
// CRUD with steps, labels and connections; Gherkin feature validation and
// import; requirement coverage configuration plus milestone test statistics.
// Runs against the real server on a fresh SQLite database via the shared
// harness.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func tmName(t *testing.T, suffix string) string {
	t.Helper()
	base := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	return fmt.Sprintf("e2e-tm-%s-%s", base, suffix)
}

// tmFeature is a minimal valid Gherkin feature with exactly one scenario.
const tmFeature = `Feature: E2E login flow

  Scenario: valid credentials grant access
    Given a registered user exists
    When the user logs in with correct credentials
    Then access is granted
`

func TestTestManagement(t *testing.T) {
	admin := e2eEnv.admin
	wsID := createWorkspace(t, admin, wscfgWSKey(t))

	t.Run("TestCaseCRUD", func(t *testing.T) { // UC-J1
		title := tmName(t, "case")
		status, body := admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases", wsID), map[string]any{
			"title": title, "preconditions": "user is registered",
		})
		wantStatus(t, "POST", "/workspaces/{id}/test-cases", status, body, http.StatusCreated)
		tcID := v2ID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d", wsID, tcID))
		wantStatus(t, "GET", "/workspaces/{id}/test-cases/{case_id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.title", title)

		stepIDs := make([]int, 0, 3)
		for i, action := range []string{"step one", "step two", "step three"} {
			status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d/steps", wsID, tcID), map[string]any{
				"action": action, "data": fmt.Sprintf("input %d", i+1), "expected": "ok",
			})
			wantStatus(t, "POST", "/workspaces/{id}/test-cases/{case_id}/steps", status, body, http.StatusCreated)
			stepIDs = append(stepIDs, v2ID(t, body))
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d/steps/reorder", wsID, tcID), map[string]any{
			"ids": []int{stepIDs[2], stepIDs[0], stepIDs[1]},
		})
		wantStatus(t, "POST", "/workspaces/{id}/test-cases/{case_id}/steps/reorder", status, body, http.StatusOK)
		assertJSONField(t, body, "data.reordered", true)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d/steps", wsID, tcID))
		wantStatus(t, "GET", "/workspaces/{id}/test-cases/{case_id}/steps", status, body, http.StatusOK)
		got := make([]int, 0, 3)
		for _, s := range v2DataArray(t, body) {
			got = append(got, int(s.(map[string]any)["id"].(float64)))
		}
		if len(got) != 3 || got[0] != stepIDs[2] || got[1] != stepIDs[0] || got[2] != stepIDs[1] {
			t.Fatalf("step order after reorder = %v, want [%d %d %d]", got, stepIDs[2], stepIDs[0], stepIDs[1])
		}

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d/steps/%d", wsID, tcID, stepIDs[0]), map[string]any{
			"action": "updated step",
		})
		wantStatus(t, "PATCH", "/workspaces/{id}/test-cases/{case_id}/steps/{step_id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.action", "updated step")

		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d/steps/%d", wsID, tcID, stepIDs[1]))
		wantStatus(t, "DELETE", "/workspaces/{id}/test-cases/{case_id}/steps/{step_id}", status, nil, http.StatusNoContent)

		if n := dbCount(t, "test_steps", "test_case_id = ?", tcID); n != 2 {
			t.Fatalf("test_steps count after delete = %d, want 2", n)
		}

		labelName := tmName(t, "label")
		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-labels", wsID), map[string]any{
			"name": labelName, "color": "#ff0000",
		})
		wantStatus(t, "POST", "/workspaces/{id}/test-labels", status, body, http.StatusCreated)
		labelID := v2ID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d/labels", wsID, tcID), map[string]any{
			"label_id": labelID,
		})
		wantStatus(t, "POST", "/workspaces/{id}/test-cases/{case_id}/labels", status, body, http.StatusOK)
		assertJSONField(t, body, "data.linked", true)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d/labels", wsID, tcID))
		wantStatus(t, "GET", "/workspaces/{id}/test-cases/{case_id}/labels", status, body, http.StatusOK)
		if !containsID(v2DataArray(t, body), labelID) {
			t.Fatalf("case labels missing %d: %s", labelID, snippet(body))
		}

		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d/labels/%d", wsID, tcID, labelID))
		wantStatus(t, "DELETE", "/workspaces/{id}/test-cases/{case_id}/labels/{label_id}", status, nil, http.StatusNoContent)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d/labels", wsID, tcID))
		wantStatus(t, "GET", "/workspaces/{id}/test-cases/{case_id}/labels (after unlink)", status, body, http.StatusOK)
		env := mustJSON(t, body).(map[string]any)
		if arr, _ := env["data"].([]any); containsID(arr, labelID) {
			t.Fatalf("case labels still contain %d after delete: %s", labelID, snippet(body))
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-plans", wsID), map[string]any{
			"name": tmName(t, "plan"), "description": "e2e plan",
		})
		wantStatus(t, "POST", "/workspaces/{id}/test-plans", status, body, http.StatusCreated)
		planID := v2ID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-plans/%d/test-cases", wsID, planID), map[string]any{
			"test_case_id": tcID,
		})
		wantStatus(t, "POST", "/workspaces/{id}/test-plans/{plan_id}/test-cases", status, body, http.StatusOK)
		assertJSONField(t, body, "data.linked", true)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/%d/connections", wsID, tcID))
		wantStatus(t, "GET", "/workspaces/{id}/test-cases/{case_id}/connections", status, body, http.StatusOK)
		assertJSONField(t, body, "data.test_plans.0.id", planID)

		row := dbQueryOne(t, "SELECT title, preconditions FROM test_cases WHERE id = ?", tcID)
		if row["title"] != title || row["preconditions"] != "user is registered" {
			t.Fatalf("test_cases row = %v, want title=%q preconditions=%q", row, title, "user is registered")
		}
	})

	t.Run("FeatureImport", func(t *testing.T) { // UC-J2
		base := dbCount(t, "test_cases", "workspace_id = ?", wsID)

		status, body := admin.DoRaw(t, http.MethodPost, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/validate-feature", wsID), "text/x-gherkin", []byte(tmFeature))
		wantStatus(t, "POST", "/workspaces/{id}/test-cases/validate-feature", status, body, http.StatusOK)
		assertJSONField(t, body, "data.valid", true)

		status, body = admin.DoRaw(t, http.MethodPost, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/import-feature", wsID), "text/x-gherkin", []byte(tmFeature))
		wantStatus(t, "POST", "/workspaces/{id}/test-cases/import-feature", status, body, http.StatusCreated)
		env := mustJSON(t, body).(map[string]any)
		data, ok := env["data"].(map[string]any)
		if !ok {
			t.Fatalf("import-feature data is not an object: %s", snippet(body))
		}
		cases, _ := data["cases"].([]any)
		if len(cases) != 1 {
			t.Fatalf("import-feature created %d cases, want 1 (one per scenario)", len(cases))
		}

		if n := dbCount(t, "test_cases", "workspace_id = ?", wsID); n != base+1 {
			t.Fatalf("test_cases count after import = %d, want %d", n, base+1)
		}

		bad := []byte("this is not gherkin at all\n")
		status, body = admin.DoRaw(t, http.MethodPost, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/validate-feature", wsID), "text/x-gherkin", bad)
		wantStatus(t, "POST", "/workspaces/{id}/test-cases/validate-feature (invalid)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.valid", false)
		env = mustJSON(t, body).(map[string]any)
		data, _ = env["data"].(map[string]any)
		if errs, _ := data["errors"].([]any); len(errs) == 0 {
			t.Fatalf("validate-feature reported no errors for invalid gherkin: %s", snippet(body))
		}

		status, body = admin.DoRaw(t, http.MethodPost, fmt.Sprintf("/api/v2/workspaces/%d/test-cases/import-feature", wsID), "text/x-gherkin", bad)
		wantStatus(t, "POST", "/workspaces/{id}/test-cases/import-feature (invalid)", status, body, http.StatusBadRequest)

		if n := dbCount(t, "test_cases", "workspace_id = ?", wsID); n != base+1 {
			t.Fatalf("invalid import changed test_cases count: %d -> %d", base+1, n)
		}
	})

	t.Run("CoverageAndStatistics", func(t *testing.T) { // UC-J3
		status, body := admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/item-types", wsID))
		wantStatus(t, "GET", "/workspaces/{id}/item-types", status, body, http.StatusOK)
		arr := v2DataArray(t, body)
		if len(arr) == 0 {
			t.Fatalf("workspace has no item types: %s", snippet(body))
		}
		itemTypeID := int(arr[0].(map[string]any)["id"].(float64))

		mkItem := func(suffix string) int {
			status, body := admin.Post(t, "/api/v2/items", map[string]any{
				"workspace_id": wsID, "item_type_id": itemTypeID, "title": tmName(t, suffix),
			})
			wantStatus(t, "POST", "/api/v2/items (requirement)", status, body, http.StatusCreated)
			return v2ID(t, body)
		}
		itemA := mkItem("req-a")
		mkItem("req-b")

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-coverage/config", wsID), map[string]any{
			"requirement_item_type_ids": []int{itemTypeID},
		})
		wantStatus(t, "POST", "/workspaces/{id}/test-coverage/config", status, body, http.StatusCreated)
		cfgID := v2ID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/test-coverage/config", wsID))
		wantStatus(t, "GET", "/workspaces/{id}/test-coverage/config", status, body, http.StatusOK)
		assertJSONField(t, body, "data.requirement_item_type_ids.0", itemTypeID)

		status, body = admin.Post(t, "/api/v2/item-types", map[string]any{
			"name": tmName(t, "it-req"), "hierarchy_level": 0,
		})
		wantStatus(t, "POST", "/api/v2/item-types (second requirement)", status, body, http.StatusCreated)
		itemTypeB := v2ID(t, body)

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/workspaces/%d/test-coverage/config/%d", wsID, cfgID), map[string]any{
			"requirement_item_type_ids": []int{itemTypeID, itemTypeB},
		})
		wantStatus(t, "PATCH", "/workspaces/{id}/test-coverage/config/{config_id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.requirement_item_type_ids.1", itemTypeB)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/test-coverage/summary", wsID))
		wantStatus(t, "GET", "/workspaces/{id}/test-coverage/summary (unlinked)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.total", 2)
		assertJSONField(t, body, "data.covered", 0)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-cases", wsID), map[string]any{
			"title": tmName(t, "cov-case"),
		})
		wantStatus(t, "POST", "/workspaces/{id}/test-cases (coverage)", status, body, http.StatusCreated)
		tcID := v2ID(t, body)

		linkTypeID := int(dbQueryOne(t, "SELECT id FROM link_types WHERE builtin_key = 'tests'")["id"].(int64))
		status, body = admin.Post(t, "/api/v2/links", map[string]any{
			"link_type_id": linkTypeID,
			"source_type":  "test_case", "source_id": tcID,
			"target_type": "item", "target_id": itemA,
		})
		wantStatus(t, "POST", "/api/v2/links (tests)", status, body, http.StatusCreated)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/test-coverage/summary", wsID))
		wantStatus(t, "GET", "/workspaces/{id}/test-coverage/summary (linked)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.total", 2)
		assertJSONField(t, body, "data.covered", 1)
		assertJSONField(t, body, "data.not_covered", 1)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/test-coverage/requirements", wsID))
		wantStatus(t, "GET", "/workspaces/{id}/test-coverage/requirements", status, body, http.StatusOK)
		found := false
		for _, rec := range v2DataArray(t, body) {
			obj := rec.(map[string]any)
			if int(obj["item_id"].(float64)) == itemA {
				found = true
				if obj["is_covered"] != true || int(obj["linked_test_count"].(float64)) != 1 {
					t.Fatalf("requirement %d coverage = covered=%v linked=%v, want covered=true linked=1", itemA, obj["is_covered"], obj["linked_test_count"])
				}
			}
		}
		if !found {
			t.Fatalf("requirements page missing item %d: %s", itemA, snippet(body))
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/milestones", wsID), map[string]any{
			"name": tmName(t, "ms"), "description": "e2e milestone",
			"target_date": "2026-12-31", "status": "planning",
		})
		wantStatus(t, "POST", "/workspaces/{id}/milestones", status, body, http.StatusCreated)
		msID := v2ID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-plans", wsID), map[string]any{
			"name": tmName(t, "ms-plan"), "description": "e2e milestone plan", "milestone_id": msID,
		})
		wantStatus(t, "POST", "/workspaces/{id}/test-plans (milestone)", status, body, http.StatusCreated)
		planID := v2ID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/test-plans/%d/test-cases", wsID, planID), map[string]any{
			"test_case_id": tcID,
		})
		wantStatus(t, "POST", "/workspaces/{id}/test-plans/{plan_id}/test-cases (milestone)", status, body, http.StatusOK)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/milestones/%d/test-statistics", msID))
		wantStatus(t, "GET", "/milestones/{id}/test-statistics", status, body, http.StatusOK)
		assertJSONField(t, body, "data.total_test_plans", 1)
		assertJSONField(t, body, "data.total_test_cases", 1)

		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/workspaces/%d/test-coverage/config/%d", wsID, cfgID))
		wantStatus(t, "DELETE", "/workspaces/{id}/test-coverage/config/{config_id}", status, nil, http.StatusNoContent)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/test-coverage/summary", wsID))
		wantStatus(t, "GET", "/workspaces/{id}/test-coverage/summary (config deleted)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.total", 0)
	})
}

// containsID reports whether arr contains an object with id == want.
func containsID(arr []any, want int) bool {
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := obj["id"].(float64); int(id) == want {
			return true
		}
	}
	return false
}
