// Use cases UC-K1..UC-K3: channel taxonomy, mailbox intakes, request types,
// asset reports, public form submission, and the notification tray.
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// cfChannelConfig decodes the stored config JSON of a channel from the DB.
func cfChannelConfig(t *testing.T, chID int) map[string]any {
	t.Helper()
	row := dbQueryOne(t, "SELECT config FROM channels WHERE id = ?", chID)
	var cfg map[string]any
	if err := json.Unmarshal([]byte(cfStr(row["config"])), &cfg); err != nil {
		t.Fatalf("channel %d config is not JSON: %v", chID, row["config"])
	}
	return cfg
}

func cfContainsInt(list []any, want int) bool {
	for _, v := range list {
		if cfInt(v) == want {
			return true
		}
	}
	return false
}

// UC-K1: channel category + channel CRUD (config, toggle), managers, mailbox
// intakes, request types with field config, and asset reports with visibility.
func TestChannelsCRUD(t *testing.T) {
	admin := e2eEnv.admin
	// workspace keys are limited to 2-10 alphanumeric characters by the v2 API
	wsID := createWorkspace(t, admin, "cfk1")
	itemTypeID := cfAnyItemType(t)

	var catID, formChID int

	t.Run("ChannelCategory", func(t *testing.T) {
		status, body := admin.Post(t, "/api/channel-categories", map[string]any{
			"name": "e2e-chforms-cat", "color": "#ff8800", "description": "k1 category",
		})
		wantStatus(t, "POST", "/api/channel-categories", status, body, http.StatusCreated)
		catID = cfExtractID(t, body)

		// duplicate name (case-insensitive unique) -> 409
		status, body = admin.Post(t, "/api/channel-categories", map[string]any{"name": "E2E-CHFORMS-CAT"})
		wantStatus(t, "POST", "/api/channel-categories", status, body, http.StatusConflict)

		// missing name -> 400
		status, body = admin.Post(t, "/api/channel-categories", map[string]any{"color": "#123456"})
		wantStatus(t, "POST", "/api/channel-categories", status, body, http.StatusBadRequest)

		// update persists (name + color are both required on update)
		status, body = admin.Put(t, fmt.Sprintf("/api/channel-categories/%d", catID), map[string]any{
			"name": "e2e-chforms-cat", "color": "#00ff88", "description": "updated by e2e",
		})
		wantStatus(t, "PUT", "/api/channel-categories/{id}", status, body, http.StatusOK)

		status, body = admin.Get(t, fmt.Sprintf("/api/channel-categories/%d", catID))
		wantStatus(t, "GET", "/api/channel-categories/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "color", "#00ff88")

		row := dbQueryOne(t, "SELECT name, color FROM channel_categories WHERE id = ?", catID)
		if cfStr(row["name"]) != "e2e-chforms-cat" || cfStr(row["color"]) != "#00ff88" {
			t.Fatalf("channel category not persisted: %v", row)
		}
	})

	t.Run("FormChannel", func(t *testing.T) {
		formSlug := "e2e-chforms-form-k1"
		status, body := admin.Post(t, "/api/channels", map[string]any{
			"name": "e2e-chforms-form", "type": "form", "direction": "inbound",
			"slug": formSlug, "category_id": catID,
		})
		wantStatus(t, "POST", "/api/channels", status, body, http.StatusCreated)
		formChID = cfExtractID(t, body)

		// missing name -> 400 ("name, type, and direction are required")
		status, body = admin.Post(t, "/api/channels", map[string]any{"type": "form", "direction": "inbound"})
		wantStatus(t, "POST", "/api/channels", status, body, http.StatusBadRequest)

		// invalid slug format -> 400
		status, body = admin.Post(t, "/api/channels", map[string]any{
			"name": "e2e-chforms-badslug", "type": "form", "direction": "inbound", "slug": "Bad_Slug!",
		})
		wantStatus(t, "POST", "/api/channels", status, body, http.StatusBadRequest)

		// read back: identity fields + slug stored in config (DB check)
		status, body = admin.Get(t, fmt.Sprintf("/api/channels/%d", formChID))
		wantStatus(t, "GET", "/api/channels/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "name", "e2e-chforms-form")
		assertJSONField(t, body, "type", "form")
		if got := cfStr(cfChannelConfig(t, formChID)["form_slug"]); got != formSlug {
			t.Fatalf("config.form_slug = %q, want %q", got, formSlug)
		}

		// config update: serve the test workspace -> 200 success:true
		status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/config", formChID), map[string]any{
			"config": map[string]any{"form_workspace_ids": []int{wsID}},
		})
		wantStatus(t, "PUT", "/api/channels/{id}/config", status, body, http.StatusOK)
		assertJSONField(t, body, "success", true)

		// missing config field -> 400
		status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/config", formChID), map[string]any{})
		wantStatus(t, "PUT", "/api/channels/{id}/config", status, body, http.StatusBadRequest)

		cfg := cfChannelConfig(t, formChID)
		fws, _ := cfg["form_workspace_ids"].([]any)
		if !cfContainsInt(fws, wsID) {
			t.Fatalf("channel config does not serve workspace %d: %v", wsID, cfg)
		}

		// toggle flips status and toggling again restores it (DB check both ways)
		before := cfStr(dbQueryOne(t, "SELECT status FROM channels WHERE id = ?", formChID)["status"])
		status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/toggle", formChID), nil)
		wantStatus(t, "PUT", "/api/channels/{id}/toggle", status, body, http.StatusOK)
		after := cfStr(dbQueryOne(t, "SELECT status FROM channels WHERE id = ?", formChID)["status"])
		if after == "" || before == after {
			t.Fatalf("toggle did not flip channel status: %q -> %q", before, after)
		}
		status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/toggle", formChID), nil)
		wantStatus(t, "PUT", "/api/channels/{id}/toggle", status, body, http.StatusOK)
		back := cfStr(dbQueryOne(t, "SELECT status FROM channels WHERE id = ?", formChID)["status"])
		if back != before {
			t.Fatalf("second toggle did not restore status: %q -> %q (want %q)", before, back, before)
		}
	})

	t.Run("Managers", func(t *testing.T) {
		uid := createUser(t, "e2e-chforms-mgr@example.test", "e2echformsmgr", "E2eMgr123!")

		status, body := admin.Post(t, fmt.Sprintf("/api/channels/%d/managers", formChID), map[string]any{
			"manager_type": "user", "manager_ids": []int{uid},
		})
		wantStatus(t, "POST", "/api/channels/{id}/managers", status, body, http.StatusCreated, http.StatusOK)

		status, body = admin.Get(t, fmt.Sprintf("/api/channels/%d/managers", formChID))
		wantStatus(t, "GET", "/api/channels/{id}/managers", status, body, http.StatusOK)
		found := false
		for _, m := range cfListEntries(t, body) {
			if cfInt(m["manager_id"]) == uid {
				found = true
			}
		}
		if !found {
			t.Fatalf("manager %d not in channel manager list: %s", uid, snippet(body))
		}

		if n := dbCount(t, "channel_managers", "channel_id = ? AND manager_id = ?", formChID, uid); n != 1 {
			t.Fatalf("expected 1 channel_managers row for user %d, got %d", uid, n)
		}

		// invalid manager_type -> 400; empty manager_ids -> 400
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/managers", formChID), map[string]any{
			"manager_type": "bogus", "manager_ids": []int{uid},
		})
		wantStatus(t, "POST", "/api/channels/{id}/managers", status, body, http.StatusBadRequest)
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/managers", formChID), map[string]any{
			"manager_type": "user", "manager_ids": []int{},
		})
		wantStatus(t, "POST", "/api/channels/{id}/managers", status, body, http.StatusBadRequest)

		// the route param is the channel_managers row PK, not the user id
		row := dbQueryOne(t, "SELECT id FROM channel_managers WHERE channel_id = ? AND manager_id = ?", formChID, uid)
		mgrRowID := cfInt(row["id"])
		if mgrRowID <= 0 {
			t.Fatalf("manager row missing from DB before delete")
		}
		status, body = admin.Delete(t, fmt.Sprintf("/api/channels/%d/managers/%d", formChID, mgrRowID))
		wantStatus(t, "DELETE", "/api/channels/{id}/managers/{managerId}", status, body, http.StatusNoContent, http.StatusOK)
		if n := dbCount(t, "channel_managers", "channel_id = ? AND manager_id = ?", formChID, uid); n != 0 {
			t.Fatalf("expected channel manager row removed for user %d, still %d rows", uid, n)
		}
	})

	t.Run("MailboxIntakes", func(t *testing.T) {
		status, body := admin.Post(t, "/api/channels", map[string]any{
			"name": "e2e-chforms-mail", "type": "email", "direction": "inbound",
		})
		wantStatus(t, "POST", "/api/channels", status, body, http.StatusCreated)
		mailID := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/intakes", mailID), map[string]any{
			"folder": "e2e-chforms-inbox", "workspace_id": wsID, "item_type_id": itemTypeID,
			"processing_disposition": "leave",
		})
		wantStatus(t, "POST", "/api/channels/{id}/intakes", status, body, http.StatusCreated)
		intakeID := cfExtractID(t, body)

		// missing folder -> 400; invalid disposition -> 400
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/intakes", mailID), map[string]any{
			"workspace_id": wsID, "item_type_id": itemTypeID,
		})
		wantStatus(t, "POST", "/api/channels/{id}/intakes", status, body, http.StatusBadRequest)
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/intakes", mailID), map[string]any{
			"folder": "e2e-chforms-x", "workspace_id": wsID, "item_type_id": itemTypeID,
			"processing_disposition": "nuke",
		})
		wantStatus(t, "POST", "/api/channels/{id}/intakes", status, body, http.StatusBadRequest)

		status, body = admin.Get(t, fmt.Sprintf("/api/channels/%d/intakes", mailID))
		wantStatus(t, "GET", "/api/channels/{id}/intakes", status, body, http.StatusOK)
		found := false
		for _, m := range cfListEntries(t, body) {
			if cfStr(m["folder"]) == "e2e-chforms-inbox" {
				found = true
			}
		}
		if !found {
			t.Fatalf("intake folder not in list: %s", snippet(body))
		}

		status, body = admin.Delete(t, fmt.Sprintf("/api/channels/%d/intakes/%d", mailID, intakeID))
		wantStatus(t, "DELETE", "/api/channels/{id}/intakes/{intakeId}", status, body, http.StatusNoContent, http.StatusOK)
		status, body = admin.Get(t, fmt.Sprintf("/api/channels/%d/intakes", mailID))
		wantStatus(t, "GET", "/api/channels/{id}/intakes", status, body, http.StatusOK)
		if n := len(cfListEntries(t, body)); n != 0 {
			t.Fatalf("expected no intakes after delete: %s", snippet(body))
		}

		// channel lifecycle end: delete the now-empty mailbox channel -> GET 404
		status, body = admin.Delete(t, fmt.Sprintf("/api/channels/%d", mailID))
		wantStatus(t, "DELETE", "/api/channels/{id}", status, body, http.StatusNoContent, http.StatusOK)
		if n := dbCount(t, "channels", "id = ?", mailID); n != 0 {
			t.Fatalf("channel row still present after delete")
		}
		// OBSERVED: GetChannel's manage-access gate runs before the existence
		// check (requireChannelManageAccess -> UserCanManage is false for a
		// missing channel), so a deleted channel surfaces as 403, not 404.
		status, body = admin.Get(t, fmt.Sprintf("/api/channels/%d", mailID))
		wantStatus(t, "GET", "/api/channels/{id}", status, body, http.StatusForbidden, http.StatusNotFound)
	})

	t.Run("RequestTypes", func(t *testing.T) {
		status, body := admin.Post(t, fmt.Sprintf("/api/channels/%d/request-types", formChID), map[string]any{
			"name": "e2e-chforms-rt", "item_type_id": itemTypeID, "workspace_id": wsID,
		})
		wantStatus(t, "POST", "/api/channels/{channel_id}/request-types", status, body, http.StatusCreated)
		rtID := cfExtractID(t, body)

		// missing name -> 400; unknown item type -> 400; duplicate in channel -> 409
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/request-types", formChID), map[string]any{
			"item_type_id": itemTypeID, "workspace_id": wsID,
		})
		wantStatus(t, "POST", "/api/channels/{channel_id}/request-types", status, body, http.StatusBadRequest)
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/request-types", formChID), map[string]any{
			"name": "e2e-chforms-rt-bad", "item_type_id": 999999, "workspace_id": wsID,
		})
		wantStatus(t, "POST", "/api/channels/{channel_id}/request-types", status, body, http.StatusBadRequest)
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/request-types", formChID), map[string]any{
			"name": "e2e-chforms-rt", "item_type_id": itemTypeID, "workspace_id": wsID,
		})
		wantStatus(t, "POST", "/api/channels/{channel_id}/request-types", status, body, http.StatusConflict)

		// field config round-trip: title (required) + description with order
		fields := []map[string]any{
			{"field_identifier": "title", "field_type": "default", "display_order": 1, "is_required": true},
			{"field_identifier": "description", "field_type": "default", "display_order": 2, "is_required": false},
		}
		status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/request-types/%d/fields", formChID, rtID), fields)
		wantStatus(t, "PUT", "/api/channels/{channel_id}/request-types/{id}/fields", status, body, http.StatusOK)

		status, body = admin.Get(t, fmt.Sprintf("/api/request-types/%d/fields", rtID))
		wantStatus(t, "GET", "/api/request-types/{id}/fields", status, body, http.StatusOK)
		got := cfListEntries(t, body)
		if len(got) != 2 {
			t.Fatalf("expected 2 request type fields, got %d: %s", len(got), snippet(body))
		}
		order := map[string]int{}
		for _, f := range got {
			order[cfStr(f["field_identifier"])] = cfInt(f["display_order"])
		}
		if order["title"] != 1 || order["description"] != 2 {
			t.Fatalf("request type field order wrong: %v", order)
		}

		// identifier not available for this request type -> 400
		status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/request-types/%d/fields", formChID, rtID), []map[string]any{
			{"field_identifier": "bogus_field", "field_type": "default", "display_order": 1},
		})
		wantStatus(t, "PUT", "/api/channels/{channel_id}/request-types/{id}/fields", status, body, http.StatusBadRequest)

		row := dbQueryOne(t, "SELECT name, channel_id FROM request_types WHERE id = ?", rtID)
		if cfStr(row["name"]) != "e2e-chforms-rt" || cfInt(row["channel_id"]) != formChID {
			t.Fatalf("request type not persisted correctly: %v", row)
		}
	})

	t.Run("AssetReports", func(t *testing.T) {
		// asset reports are only supported on inbound portal channels
		status, body := admin.Post(t, "/api/channels", map[string]any{
			"name": "e2e-chforms-portal", "type": "portal", "direction": "inbound",
			"slug": "e2e-chforms-portal-k1",
		})
		wantStatus(t, "POST", "/api/channels", status, body, http.StatusCreated)
		portalChID := cfExtractID(t, body)

		status, body = admin.Post(t, "/api/v2/asset-sets", map[string]any{"name": "e2e-chforms-set"})
		wantStatus(t, "POST", "/api/v2/asset-sets", status, body, http.StatusCreated)
		setID := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/asset-reports", portalChID), map[string]any{
			"name": "e2e-chforms-ar", "asset_set_id": setID, "cql_query": "true",
		})
		wantStatus(t, "POST", "/api/channels/{channel_id}/asset-reports", status, body, http.StatusCreated)
		arID := cfExtractID(t, body)

		// missing name -> 400; unknown asset set -> 400; duplicate in channel -> 409
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/asset-reports", portalChID), map[string]any{
			"asset_set_id": setID, "cql_query": "true",
		})
		wantStatus(t, "POST", "/api/channels/{channel_id}/asset-reports", status, body, http.StatusBadRequest)
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/asset-reports", portalChID), map[string]any{
			"name": "e2e-chforms-ar-bad", "asset_set_id": 999999, "cql_query": "true",
		})
		wantStatus(t, "POST", "/api/channels/{channel_id}/asset-reports", status, body, http.StatusBadRequest)
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/asset-reports", portalChID), map[string]any{
			"name": "e2e-chforms-ar", "asset_set_id": setID, "cql_query": "true",
		})
		wantStatus(t, "POST", "/api/channels/{channel_id}/asset-reports", status, body, http.StatusConflict)

		// visibility round-trip: restrict to customer org 7, verify, clear
		status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/asset-reports/%d/visibility", portalChID, arID), map[string]any{
			"group_ids": []int{}, "org_ids": []int{7},
		})
		wantStatus(t, "PUT", "/api/channels/{channel_id}/asset-reports/{id}/visibility", status, body, http.StatusOK)
		status, body = admin.Get(t, fmt.Sprintf("/api/asset-reports/%d", arID))
		wantStatus(t, "GET", "/api/asset-reports/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "visibility_org_ids.0", 7)
		status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/asset-reports/%d/visibility", portalChID, arID), map[string]any{
			"group_ids": []int{}, "org_ids": []int{},
		})
		wantStatus(t, "PUT", "/api/channels/{channel_id}/asset-reports/{id}/visibility", status, body, http.StatusOK)

		status, body = admin.Get(t, fmt.Sprintf("/api/channels/%d/asset-reports", portalChID))
		wantStatus(t, "GET", "/api/channels/{channel_id}/asset-reports", status, body, http.StatusOK)
		found := false
		for _, m := range cfListEntries(t, body) {
			if cfStr(m["name"]) == "e2e-chforms-ar" {
				found = true
			}
		}
		if !found {
			t.Fatalf("asset report not in channel list: %s", snippet(body))
		}

		row := dbQueryOne(t, "SELECT name, channel_id, asset_set_id FROM asset_reports WHERE id = ?", arID)
		if cfStr(row["name"]) != "e2e-chforms-ar" || cfInt(row["channel_id"]) != portalChID || cfInt(row["asset_set_id"]) != setID {
			t.Fatalf("asset report not persisted correctly: %v", row)
		}
	})

	t.Run("CategoryDeleteBlocked", func(t *testing.T) {
		// the category is still referenced by the form channel -> 409
		status, body := admin.Delete(t, fmt.Sprintf("/api/channel-categories/%d", catID))
		wantStatus(t, "DELETE", "/api/channel-categories/{id}", status, body, http.StatusConflict)
	})
}

// UC-K2: public form submission creates a workspace item in the channel's
// served workspace; invalid submissions are rejected with 4xx.
func TestFormsSubmission(t *testing.T) {
	admin := e2eEnv.admin
	// workspace keys are limited to 2-10 alphanumeric characters by the v2 API
	wsID := createWorkspace(t, admin, "cfk2")
	itemTypeID := cfAnyItemType(t)
	slug := "e2e-chforms-fsub"

	// New channels are always created disabled (service forces it); the public
	// form lookup requires status='enabled', so enable via toggle after the
	// serving workspaces are configured.
	status, body := admin.Post(t, "/api/channels", map[string]any{
		"name": "e2e-chforms-fsub-form", "type": "form", "direction": "inbound",
		"slug": slug,
	})
	wantStatus(t, "POST", "/api/channels", status, body, http.StatusCreated)
	chID := cfExtractID(t, body)

	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/config", chID), map[string]any{
		"config": map[string]any{"form_workspace_ids": []int{wsID}},
	})
	wantStatus(t, "PUT", "/api/channels/{id}/config", status, body, http.StatusOK)

	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/toggle", chID), nil)
	wantStatus(t, "PUT", "/api/channels/{id}/toggle", status, body, http.StatusOK)
	if s := cfStr(dbQueryOne(t, "SELECT status FROM channels WHERE id = ?", chID)["status"]); s != "enabled" {
		t.Fatalf("channel %d not enabled after toggle: %q", chID, s)
	}

	// is_active defaults to false on create; submissions only accept active forms.
	status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/request-types", chID), map[string]any{
		"name": "e2e-chforms-fsub-rt", "item_type_id": itemTypeID, "workspace_id": wsID,
		"is_active": true,
	})
	wantStatus(t, "POST", "/api/channels/{channel_id}/request-types", status, body, http.StatusCreated)
	rtID := cfExtractID(t, body)

	// expose a title field so the submission's title becomes the item title
	// (without it, an anonymous submit needs a title template on the form)
	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/request-types/%d/fields", chID, rtID), []map[string]any{
		{"field_identifier": "title", "field_type": "default", "display_order": 1, "is_required": true},
	})
	wantStatus(t, "PUT", "/api/channels/{channel_id}/request-types/{id}/fields", status, body, http.StatusOK)

	public := newClient(e2eEnv.baseURL) // no session cookie: anonymous visitor

	// the form channel is publicly discoverable without auth
	status, body = public.Get(t, "/api/forms/"+slug)
	wantStatus(t, "GET", "/api/forms/{slug}", status, body, http.StatusOK)

	title := "e2e-chforms-submission-k2"
	status, body = public.Post(t, "/api/forms/"+slug+"/submit", map[string]any{
		"request_type_id": rtID, "title": title, "description": "submitted by e2e",
	})
	wantStatus(t, "POST", "/api/forms/{slug}/submit", status, body, http.StatusCreated)
	var resp struct {
		ItemID int `json:"item_id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.ItemID <= 0 {
		t.Fatalf("form submit response missing item_id: %s", snippet(body))
	}

	// the created item is findable via v2 search by title in the served workspace
	status, body = admin.Get(t, fmt.Sprintf("/api/v2/items/search?q=%s&workspace_id=%d", url.QueryEscape(title), wsID))
	wantStatus(t, "GET", "/api/v2/items/search", status, body, http.StatusOK)
	found := false
	for _, it := range cfListEntries(t, body) {
		if cfInt(it["id"]) == resp.ItemID && cfStr(it["title"]) == title {
			found = true
		}
	}
	if !found {
		t.Fatalf("submitted item %d not found via search: %s", resp.ItemID, snippet(body))
	}

	// persistence: the item row matches the submission
	row := dbQueryOne(t, "SELECT workspace_id, title FROM items WHERE id = ?", resp.ItemID)
	if cfInt(row["workspace_id"]) != wsID || cfStr(row["title"]) != title {
		t.Fatalf("submitted item not persisted correctly: %v", row)
	}

	t.Run("Negatives", func(t *testing.T) {
		// unknown slug -> 404
		status, body := public.Post(t, "/api/forms/e2e-chforms-nope/submit", map[string]any{
			"request_type_id": rtID, "title": "x",
		})
		wantStatus(t, "POST", "/api/forms/{slug}/submit", status, body, http.StatusNotFound)

		// missing request_type_id -> 400
		status, body = public.Post(t, "/api/forms/"+slug+"/submit", map[string]any{"title": "no rt"})
		wantStatus(t, "POST", "/api/forms/{slug}/submit", status, body, http.StatusBadRequest)

		// request type belonging to a different channel -> 400
		slug2 := "e2e-chforms-fsub-other"
		status, body = admin.Post(t, "/api/channels", map[string]any{
			"name": "e2e-chforms-fsub-other", "type": "form", "direction": "inbound", "slug": slug2,
		})
		wantStatus(t, "POST", "/api/channels", status, body, http.StatusCreated)
		chID2 := cfExtractID(t, body)
		status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/config", chID2), map[string]any{
			"config": map[string]any{"form_workspace_ids": []int{wsID}},
		})
		wantStatus(t, "PUT", "/api/channels/{id}/config", status, body, http.StatusOK)
		status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/request-types", chID2), map[string]any{
			"name": "e2e-chforms-fsub-rt2", "item_type_id": itemTypeID, "workspace_id": wsID,
		})
		wantStatus(t, "POST", "/api/channels/{channel_id}/request-types", status, body, http.StatusCreated)
		rtID2 := cfExtractID(t, body)

		status, body = public.Post(t, "/api/forms/"+slug+"/submit", map[string]any{
			"request_type_id": rtID2, "title": "cross channel",
		})
		wantStatus(t, "POST", "/api/forms/{slug}/submit", status, body, http.StatusBadRequest)
	})
}

// UC-K3: the notification tray lists entries for the acting user and read /
// read-all update persisted state.
func TestChannelsNotifications(t *testing.T) {
	admin := e2eEnv.admin
	public := newClient(e2eEnv.baseURL)

	// unauthenticated access is rejected
	status, body := public.Get(t, "/api/notifications")
	wantStatus(t, "GET", "/api/notifications", status, body, http.StatusUnauthorized)

	status, body = admin.Post(t, "/api/notifications", map[string]any{
		"title": "e2e-chforms-n1", "message": "first reminder",
	})
	wantStatus(t, "POST", "/api/notifications", status, body, http.StatusCreated)
	n1 := cfExtractID(t, body)

	status, body = admin.Post(t, "/api/notifications", map[string]any{
		"title": "e2e-chforms-n2", "message": "second reminder",
	})
	wantStatus(t, "POST", "/api/notifications", status, body, http.StatusCreated)
	n2 := cfExtractID(t, body)

	listByTitle := func() map[string]map[string]any {
		status, body := admin.Get(t, "/api/notifications?limit=50")
		wantStatus(t, "GET", "/api/notifications", status, body, http.StatusOK)
		out := map[string]map[string]any{}
		for _, n := range cfListEntries(t, body) {
			out[cfStr(n["title"])] = n
		}
		return out
	}

	// both entries are listed for the acting user and start unread
	byTitle := listByTitle()
	e1, ok1 := byTitle["e2e-chforms-n1"]
	e2, ok2 := byTitle["e2e-chforms-n2"]
	if !ok1 || !ok2 {
		t.Fatalf("created notifications missing from tray: %v", byTitle)
	}
	if cfInt(e1["id"]) != n1 || cfInt(e2["id"]) != n2 {
		t.Fatalf("tray entries do not match created ids: got %d/%d, want %d/%d", cfInt(e1["id"]), cfInt(e2["id"]), n1, n2)
	}
	if cfBool(e1["read"]) || cfBool(e2["read"]) {
		t.Fatalf("expected both notifications to be unread initially")
	}

	// mark one read -> DB flag flips
	status, body = admin.Patch(t, fmt.Sprintf("/api/notifications/%d/read", n1), nil)
	wantStatus(t, "PATCH", "/api/notifications/{id}/read", status, body, http.StatusOK)
	row := dbQueryOne(t, "SELECT read FROM notifications WHERE id = ?", n1)
	if !cfBool(row["read"]) {
		t.Fatalf("notification %d not marked read in DB: %v", n1, row)
	}

	// mark all read -> no unread rows remain for the admin user
	adminID := cfInt(dbQueryOne(t, "SELECT id FROM users WHERE username = ?", adminUsername)["id"])
	status, body = admin.Patch(t, "/api/notifications/read-all", nil)
	wantStatus(t, "PATCH", "/api/notifications/read-all", status, body, http.StatusOK)
	if n := dbCount(t, "notifications", "user_id = ? AND read = FALSE", adminID); n != 0 {
		t.Fatalf("%d unread notifications remain after read-all for user %d", n, adminID)
	}

	// the tray reflects the updated read state
	byTitle = listByTitle()
	for _, title := range []string{"e2e-chforms-n1", "e2e-chforms-n2"} {
		if e, ok := byTitle[title]; !ok || !cfBool(e["read"]) {
			t.Fatalf("notification %q not shown as read in tray: %v", title, byTitle)
		}
	}
}
