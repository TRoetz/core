package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
)

// assetsID decodes the v2 single-item envelope {"data":{"id":N}}.
func assetsID(t *testing.T, body []byte) int {
	t.Helper()
	var res struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.Data.ID == 0 {
		t.Fatalf("decode v2 id from response: %v\n%s", err, body)
	}
	return res.Data.ID
}

// assetsPageNames decodes the v2 page envelope {"data":[{"name":...}, ...]}.
func assetsPageNames(t *testing.T, body []byte) []string {
	t.Helper()
	var res struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode v2 page response: %v\n%s", err, body)
	}
	names := make([]string, 0, len(res.Data))
	for _, d := range res.Data {
		names = append(names, d.Name)
	}
	return names
}

// assetsSeedSetTypeStatus creates a set with one type and one status through
// the v2 API and returns their IDs.
func assetsSeedSetTypeStatus(t *testing.T, c *Client, setName string) (setID, typeID, statusID int) {
	t.Helper()
	status, body := c.Post(t, "/api/v2/asset-sets", map[string]any{"name": setName})
	wantStatus(t, "POST", "/api/v2/asset-sets", status, body, http.StatusCreated)
	setID = assetsID(t, body)

	status, body = c.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/types", setID), map[string]any{
		"name": "E2E H Type", "color": "#3b82f6",
	})
	wantStatus(t, "POST", "/api/v2/asset-sets/{id}/types", status, body, http.StatusCreated)
	typeID = assetsID(t, body)

	status, body = c.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/statuses", setID), map[string]any{
		"name": "E2E H Active", "color": "#22c55e",
	})
	wantStatus(t, "POST", "/api/v2/asset-sets/{id}/statuses", status, body, http.StatusCreated)
	statusID = assetsID(t, body)
	return setID, typeID, statusID
}

// UC-H1 — Asset taxonomy CRUD. Set/type/category/status round-trip through the
// real v2 API; category move re-parents a child to root. Roles are read-only
// over HTTP (no create endpoint exists and SQLite seeds no rows), so role
// coverage is list/get with an OBSERVED note when empty.
func TestAssetTaxonomyCRUD(t *testing.T) {
	admin := e2eEnv.admin

	t.Run("Set", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/asset-sets", map[string]any{
			"name": "E2E H1 set", "description": "taxonomy round-trip",
		})
		wantStatus(t, "POST", "/api/v2/asset-sets", status, body, http.StatusCreated)
		setID := assetsID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/asset-sets/%d", setID))
		wantStatus(t, "GET", "/api/v2/asset-sets/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", "E2E H1 set")

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/asset-sets/%d", setID), map[string]any{
			"name": "E2E H1 set (renamed)",
		})
		wantStatus(t, "PATCH", "/api/v2/asset-sets/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", "E2E H1 set (renamed)")

		status, _ = v2Patch(t, admin, "/api/v2/asset-sets/999999", map[string]any{"name": "nope"})
		wantStatus(t, "PATCH", "/api/v2/asset-sets/{unknown}", status, body, http.StatusNotFound)

		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/asset-sets/%d", setID))
		wantStatus(t, "DELETE", "/api/v2/asset-sets/{id}", status, body, http.StatusNoContent)
	})

	t.Run("Type", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/asset-sets", map[string]any{"name": "E2E H1 type set"})
		wantStatus(t, "POST", "/api/v2/asset-sets (type)", status, body, http.StatusCreated)
		setID := assetsID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/types", setID), map[string]any{
			"name": "Server", "color": "#3b82f6", "display_order": 1,
		})
		wantStatus(t, "POST", "/api/v2/asset-sets/{id}/types", status, body, http.StatusCreated)
		typeID := assetsID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/asset-types/%d", typeID))
		wantStatus(t, "GET", "/api/v2/asset-types/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", "Server")

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/asset-types/%d", typeID), map[string]any{
			"color": "#ef4444",
		})
		wantStatus(t, "PATCH", "/api/v2/asset-types/{id}", status, body, http.StatusOK)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/asset-sets/%d/types", setID))
		wantStatus(t, "GET", "/api/v2/asset-sets/{id}/types", status, body, http.StatusOK)
		if !slices.Contains(assetsPageNames(t, body), "Server") {
			t.Fatalf("type list does not contain created type: %s", body)
		}

		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/asset-types/%d", typeID))
		wantStatus(t, "DELETE", "/api/v2/asset-types/{id}", status, body, http.StatusNoContent)
	})

	t.Run("Category", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/asset-sets", map[string]any{"name": "E2E H1 category set"})
		wantStatus(t, "POST", "/api/v2/asset-sets (category)", status, body, http.StatusCreated)
		setID := assetsID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/categories", setID), map[string]any{
			"name": "Parent",
		})
		wantStatus(t, "POST", "/api/v2/asset-sets/{id}/categories", status, body, http.StatusCreated)
		parentID := assetsID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/categories", setID), map[string]any{
			"name": "Child", "parent_id": parentID,
		})
		wantStatus(t, "POST", "/api/v2/asset-sets/{id}/categories (child)", status, body, http.StatusCreated)
		childID := assetsID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/asset-categories/%d", childID))
		wantStatus(t, "GET", "/api/v2/asset-categories/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", "Child")

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/asset-categories/%d", childID), map[string]any{
			"name": "Child (renamed)",
		})
		wantStatus(t, "PATCH", "/api/v2/asset-categories/{id}", status, body, http.StatusOK)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/asset-categories/%d/move", childID), map[string]any{
			"parent_id": nil,
		})
		wantStatus(t, "POST", "/api/v2/asset-categories/{id}/move (to root)", status, body, http.StatusOK)

		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/asset-categories/%d", childID))
		wantStatus(t, "DELETE", "/api/v2/asset-categories/{id}", status, body, http.StatusNoContent)
		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/asset-categories/%d", parentID))
		wantStatus(t, "DELETE", "/api/v2/asset-categories/{id} (parent)", status, body, http.StatusNoContent)
	})

	t.Run("Status", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/asset-sets", map[string]any{"name": "E2E H1 status set"})
		wantStatus(t, "POST", "/api/v2/asset-sets (status)", status, body, http.StatusCreated)
		setID := assetsID(t, body)

		// Set creation seeds four default statuses; names are unique per set.
		status, body = admin.Get(t, fmt.Sprintf("/api/v2/asset-sets/%d/statuses", setID))
		wantStatus(t, "GET", "/api/v2/asset-sets/{id}/statuses (seeded)", status, body, http.StatusOK)
		names := assetsPageNames(t, body)
		for _, want := range []string{"Active", "Inactive", "Maintenance", "Retired"} {
			if !slices.Contains(names, want) {
				t.Fatalf("seeded statuses %v missing %q", names, want)
			}
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/statuses", setID), map[string]any{
			"name": "E2E H1 Custom", "color": "#8b5cf6",
		})
		wantStatus(t, "POST", "/api/v2/asset-sets/{id}/statuses", status, body, http.StatusCreated)
		statusID := assetsID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/asset-statuses/%d", statusID))
		wantStatus(t, "GET", "/api/v2/asset-statuses/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", "E2E H1 Custom")

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/asset-statuses/%d", statusID), map[string]any{
			"description": "e2e h1 status desc",
		})
		wantStatus(t, "PATCH", "/api/v2/asset-statuses/{id}", status, body, http.StatusOK)

		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/asset-statuses/%d", statusID))
		wantStatus(t, "DELETE", "/api/v2/asset-statuses/{id}", status, body, http.StatusNoContent)
	})

	t.Run("RolesReadOnly", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/asset-sets", map[string]any{"name": "E2E H1 role set"})
		wantStatus(t, "POST", "/api/v2/asset-sets (roles)", status, body, http.StatusCreated)

		status, body = admin.Get(t, "/api/v2/asset-roles")
		wantStatus(t, "GET", "/api/v2/asset-roles", status, body, http.StatusOK)
		var res struct {
			Data []struct {
				ID int `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatalf("decode role list: %v\n%s", err, body)
		}
		if len(res.Data) == 0 {
			t.Log("OBSERVED: asset_roles is empty on SQLite (seed rows exist only in the Postgres schema and no HTTP create endpoint exists); role get skipped")
			return
		}
		status, body = admin.Get(t, fmt.Sprintf("/api/v2/asset-roles/%d", res.Data[0].ID))
		wantStatus(t, "GET", "/api/v2/asset-roles/{id}", status, body, http.StatusOK)
	})
}

// UC-H2 — Asset lifecycle and relationships. Assets are created inside a set
// with type/status, round-trip through get/patch, link to each other via a
// link type, appear in batch summaries, and persist scoped to the set in the
// database. A user without assets grants is denied on read and write.
func TestAssetLifecycleAndLinks(t *testing.T) {
	admin := e2eEnv.admin
	setID, typeID, statusID := assetsSeedSetTypeStatus(t, admin, "E2E H2 set")

	t.Run("CreateGetPatchDelete", func(t *testing.T) {
		status, body := admin.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/assets", setID), map[string]any{
			"asset_type_id": typeID,
			"title":         "E2E H2 asset A",
			"status_id":     statusID,
		})
		wantStatus(t, "POST", "/api/v2/asset-sets/{id}/assets", status, body, http.StatusCreated)
		assetA := assetsID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/assets/%d", assetA))
		wantStatus(t, "GET", "/api/v2/assets/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.title", "E2E H2 asset A")

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/assets/%d", assetA), map[string]any{
			"title": "E2E H2 asset A (renamed)",
		})
		wantStatus(t, "PATCH", "/api/v2/assets/{id}", status, body, http.StatusOK)

		row := dbQueryOne(t, "SELECT set_id FROM assets WHERE id = ?", assetA)
		if got := row["set_id"]; int(got.(int64)) != setID {
			t.Fatalf("asset %d persisted with set_id=%v, want %d", assetA, got, setID)
		}

		status, _ = admin.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/assets", setID), map[string]any{
			"asset_type_id": 999999,
			"title":         "E2E H2 unknown type",
		})
		wantStatus(t, "POST", "/api/v2/asset-sets/{id}/assets (unknown type)", status, body, http.StatusBadRequest, http.StatusNotFound)

		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/assets/%d", assetA))
		wantStatus(t, "DELETE", "/api/v2/assets/{id}", status, body, http.StatusNoContent)
	})

	t.Run("LinksAndSummaries", func(t *testing.T) {
		mkAsset := func(title string) int {
			status, body := admin.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/assets", setID), map[string]any{
				"asset_type_id": typeID,
				"title":         title,
			})
			wantStatus(t, "POST", "/api/v2/asset-sets/{id}/assets ("+title+")", status, body, http.StatusCreated)
			return assetsID(t, body)
		}
		assetA := mkAsset("E2E H2 link A")
		assetB := mkAsset("E2E H2 link B")

		status, body := admin.Post(t, "/api/v2/link-types", map[string]any{
			"name": "E2E H2 asset link", "forward_label": "fwd", "reverse_label": "rev",
		})
		wantStatus(t, "POST", "/api/v2/link-types", status, body, http.StatusCreated)
		linkTypeID := assetsID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/assets/%d/links", assetA), map[string]any{
			"link_type_id": linkTypeID,
			"target_type":  "asset",
			"target_id":    assetB,
		})
		wantStatus(t, "POST", "/api/v2/assets/{id}/links", status, body, http.StatusCreated)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/assets/%d/links", assetA))
		wantStatus(t, "GET", "/api/v2/assets/{id}/links", status, body, http.StatusOK)
		var links struct {
			Data struct {
				Outgoing []struct {
					TargetID int `json:"target_id"`
				} `json:"outgoing"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &links); err != nil {
			t.Fatalf("decode asset links: %v\n%s", err, body)
		}
		found := false
		for _, l := range links.Data.Outgoing {
			if l.TargetID == assetB {
				found = true
			}
		}
		if !found {
			t.Fatalf("outgoing links do not contain target %d: %s", assetB, body)
		}

		status, body = admin.Post(t, "/api/v2/assets/summaries", map[string]any{
			"ids": []int{assetA, assetB},
		})
		wantStatus(t, "POST", "/api/v2/assets/summaries", status, body, http.StatusOK)
		var sums struct {
			Data []struct {
				ID int `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &sums); err != nil || len(sums.Data) != 2 {
			t.Fatalf("summaries for 2 assets: got %d entries: %v\n%s", len(sums.Data), err, body)
		}

		if n := dbCount(t, "assets", "set_id = ?", setID); n < 2 {
			t.Fatalf("expected at least 2 assets in set %d, found %d", setID, n)
		}
	})

	t.Run("PermissionDeniedWithoutAssetsGrants", func(t *testing.T) {
		createUser(t, "e2e-h2-noperm@example.test", "e2eh2noperm", "E2eH2Noperm123!")
		c := login(t, "e2eh2noperm", "E2eH2Noperm123!", "e2eh2noperm")

		// The v2 asset API hides sets from users who cannot see them: denial
		// surfaces as 404 (not_found), not 403.
		status, body := c.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/assets", setID), map[string]any{
			"asset_type_id": typeID,
			"title":         "E2E H2 denied asset",
		})
		wantStatus(t, "POST", "/api/v2/asset-sets/{id}/assets without grants", status, body, http.StatusNotFound)

		// The row exists (created by admin); only the permission check can
		// produce the 404.
		status, body = admin.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/assets", setID), map[string]any{
			"asset_type_id": typeID,
			"title":         "E2E H2 visibility probe",
		})
		wantStatus(t, "POST", "/api/v2/asset-sets/{id}/assets (probe)", status, body, http.StatusCreated)
		probeAsset := assetsID(t, body)

		status, body = c.Get(t, fmt.Sprintf("/api/v2/assets/%d", probeAsset))
		wantStatus(t, "GET", "/api/v2/assets/{id} without grants", status, body, http.StatusNotFound)
	})
}

// UC-H3 — Asset set actions. A manual-trigger action round-trips through the
// v1 API (create/get/list/toggle), executes against a set asset, and leaves an
// execution log row in the database. Set-admin access is required: a user
// without grants cannot create or execute actions.
func TestAssetSetActions(t *testing.T) {
	admin := e2eEnv.admin
	setID, typeID, _ := assetsSeedSetTypeStatus(t, admin, "E2E H3 set")

	status, body := admin.Post(t, fmt.Sprintf("/api/v2/asset-sets/%d/assets", setID), map[string]any{
		"asset_type_id": typeID,
		"title":         "E2E H3 action target",
	})
	wantStatus(t, "POST", "/api/v2/asset-sets/{id}/assets (action target)", status, body, http.StatusCreated)
	targetAsset := assetsID(t, body)

	t.Run("ActionLifecycle", func(t *testing.T) {
		status, body = admin.Post(t, fmt.Sprintf("/api/asset-sets/%d/actions", setID), map[string]any{
			"name":         "E2E H3 manual action",
			"trigger_type": "manual",
			"nodes": []map[string]any{
				{"node_type": "trigger", "node_config": "{}"},
			},
		})
		wantStatus(t, "POST", "/api/asset-sets/{id}/actions", status, body, http.StatusCreated)
		var created struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(body, &created); err != nil || created.ID == 0 {
			t.Fatalf("decode action create response: %v\n%s", err, body)
		}
		actionID := created.ID

		status, body = admin.Get(t, fmt.Sprintf("/api/asset-sets/%d/actions", setID))
		wantStatus(t, "GET", "/api/asset-sets/{id}/actions", status, body, http.StatusOK)
		var list []struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			t.Fatalf("decode action list: %v\n%s", err, body)
		}
		found := false
		for _, a := range list {
			if a.ID == actionID {
				found = true
			}
		}
		if !found {
			t.Fatalf("action list does not contain created action %d: %s", actionID, body)
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/asset-sets/%d/actions/%d", setID, actionID))
		wantStatus(t, "GET", "/api/asset-sets/{id}/actions/{aid}", status, body, http.StatusOK)
		assertJSONField(t, body, "name", "E2E H3 manual action")

		status, body = admin.Post(t, fmt.Sprintf("/api/asset-sets/%d/actions/%d/toggle", setID, actionID), nil)
		wantStatus(t, "POST", "/api/asset-sets/{id}/actions/{aid}/toggle (off)", status, body, http.StatusOK)
		status, body = admin.Get(t, fmt.Sprintf("/api/asset-sets/%d/actions/%d", setID, actionID))
		wantStatus(t, "GET", "/api/asset-sets/{id}/actions/{aid} (toggled off)", status, body, http.StatusOK)
		assertJSONField(t, body, "is_enabled", false)

		status, body = admin.Post(t, fmt.Sprintf("/api/asset-sets/%d/actions/%d/toggle", setID, actionID), nil)
		wantStatus(t, "POST", "/api/asset-sets/{id}/actions/{aid}/toggle (on)", status, body, http.StatusOK)

		status, body = admin.Post(t, fmt.Sprintf("/api/asset-sets/%d/actions/%d/execute", setID, actionID), map[string]any{
			"asset_id": targetAsset,
		})
		wantStatus(t, "POST", "/api/asset-sets/{id}/actions/{aid}/execute", status, body, http.StatusOK)

		status, body = admin.Get(t, fmt.Sprintf("/api/asset-sets/%d/actions/%d/logs", setID, actionID))
		wantStatus(t, "GET", "/api/asset-sets/{id}/actions/{aid}/logs", status, body, http.StatusOK)
		var logs []map[string]any
		if err := json.Unmarshal(body, &logs); err != nil || len(logs) == 0 {
			t.Fatalf("expected at least one execution log after execute: %v\n%s", err, body)
		}

		row := dbQueryOne(t, "SELECT COUNT(*) AS n FROM asset_action_execution_logs WHERE action_id = ?", actionID)
		if got := row["n"]; int(got.(int64)) < 1 {
			t.Fatalf("expected execution log rows for action %d in DB, found %v", actionID, got)
		}

		status, body = admin.Delete(t, fmt.Sprintf("/api/asset-sets/%d/actions/%d", setID, actionID))
		wantStatus(t, "DELETE", "/api/asset-sets/{id}/actions/{aid}", status, body, http.StatusNoContent)
	})

	t.Run("PermissionDeniedWithoutSetAdminAccess", func(t *testing.T) {
		createUser(t, "e2e-h3-noperm@example.test", "e2eh3noperm", "E2eH3Noperm123!")
		c := login(t, "e2eh3noperm", "E2eH3Noperm123!", "e2eh3noperm")

		// Set-admin access is enforced by hiding the set: denial surfaces as
		// 404 ("asset set not found"), not 403.
		status, body := c.Post(t, fmt.Sprintf("/api/asset-sets/%d/actions", setID), map[string]any{
			"name":         "E2E H3 denied action",
			"trigger_type": "manual",
		})
		wantStatus(t, "POST", "/api/asset-sets/{id}/actions without grants", status, body, http.StatusNotFound)

		status, body = c.Get(t, fmt.Sprintf("/api/asset-sets/%d/actions", setID))
		wantStatus(t, "GET", "/api/asset-sets/{id}/actions without grants", status, body, http.StatusNotFound)
	})
}
