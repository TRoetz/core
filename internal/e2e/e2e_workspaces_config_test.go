// UC-C series: workspace + catalog configuration use cases (workspaces, item
// types, custom fields, statuses/workflows/transitions, priorities, screens,
// link types, collections, configuration-set export/import/migration).
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// wscfgName returns a unique entity name for the calling test/subtest. Tests
// run serially on a fresh database per invocation, so t.Name() scoping is
// sufficient for uniqueness.
func wscfgName(t *testing.T, suffix string) string {
	t.Helper()
	base := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	return fmt.Sprintf("e2e-wscfg-%s-%s", base, suffix)
}

// wsKeySeq hands out unique workspace keys. The v2 API enforces
// ValidWorkspaceKey (services/workspace_application_service.go): 2-10 chars,
// ^[A-Za-z0-9]+$, normalized to uppercase — so long dashed names are not
// usable as keys and a short counter-based scheme is required.
var wsKeySeq int32

func wscfgWSKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("WSCFG%02d", atomic.AddInt32(&wsKeySeq, 1))
}

// v2Patch sends a merge-patch to the v2 API. The v2 router strictly requires
// Content-Type application/merge-patch+json for PATCH (restapi/v2/request.go).
func v2Patch(t *testing.T, c *Client, path string, body map[string]any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("v2Patch(%s): marshal: %v", path, err)
	}
	return c.DoRaw(t, http.MethodPatch, path, "application/merge-patch+json", raw)
}

// v2ID extracts data.id from a v2 single-object response envelope. Some
// resources nest the record under a sub-key (custom fields use
// data.custom_field), so when there is no top-level id it falls back to a
// depth-first search within Data.
func v2ID(t *testing.T, body []byte) int {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Data == nil {
		t.Fatalf("v2ID: no data envelope in %s", snippet(body))
	}
	id, ok := env.Data["id"].(float64)
	if !ok || id <= 0 {
		var walk func(any) int
		walk = func(n any) int {
			switch x := n.(type) {
			case map[string]any:
				if id, ok := x["id"].(float64); ok && id > 0 {
					return int(id)
				}
				for _, val := range x {
					if r := walk(val); r != 0 {
						return r
					}
				}
			case []any:
				for _, val := range x {
					if r := walk(val); r != 0 {
						return r
					}
				}
			}
			return 0
		}
		id = float64(walk(env.Data))
		if id <= 0 {
			t.Fatalf("v2ID: no positive id in %s", snippet(body))
		}
	}
	return int(id)
}

// v2DataRecord returns the record object inside a v2 single-object envelope.
// Most resources put the DTO directly under data; custom fields nest it under
// data.custom_field, so fall back to the first nested map that carries "id".
func v2DataRecord(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Data == nil {
		t.Fatalf("v2DataRecord: no data envelope in %s", snippet(body))
	}
	if _, ok := env.Data["id"]; ok {
		return env.Data
	}
	for _, val := range env.Data {
		if m, ok := val.(map[string]any); ok {
			if _, hasID := m["id"]; hasID {
				return m
			}
		}
	}
	t.Fatalf("v2DataRecord: no record object in %s", snippet(body))
	return nil
}

// v2DataArray extracts the data array from a v2 list response envelope.
func v2DataArray(t *testing.T, body []byte) []any {
	t.Helper()
	var env struct {
		Data any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Data == nil {
		t.Fatalf("v2DataArray: no data envelope in %s", snippet(body))
	}
	arr, ok := env.Data.([]any)
	if !ok {
		t.Fatalf("v2DataArray: data is not an array in %s", snippet(body))
	}
	return arr
}

// findByName returns the first object in a v2 list whose "name" equals name.
func findByName(arr []any, name string) (map[string]any, bool) {
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if obj["name"] == name {
			return obj, true
		}
	}
	return nil, false
}

// toInt converts a value from a dbQueryOne row (SQLite integers surface as
// int64) or JSON (float64) to int.
func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// firstID walks a JSON document depth-first and returns the first positive
// integer "id" value (v1 create responses wrap the record in varying shapes).
func firstID(body []byte) int {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return 0
	}
	var walk func(any) int
	walk = func(n any) int {
		switch x := n.(type) {
		case map[string]any:
			if id, ok := x["id"].(float64); ok && id > 0 {
				return int(id)
			}
			for _, val := range x {
				if r := walk(val); r != 0 {
					return r
				}
			}
		case []any:
			for _, val := range x {
				if r := walk(val); r != 0 {
					return r
				}
			}
		}
		return 0
	}
	return walk(v)
}

// jsonArrayLen returns the length of the array at top-level key path, or -1.
func jsonArrayLen(body []byte, key string) int {
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		return -1
	}
	arr, ok := v[key].([]any)
	if !ok {
		return -1
	}
	return len(arr)
}

// multipartUpload POSTs a single file field to path (config-set import).
func multipartUpload(t *testing.T, c *Client, path string, field, filename string, content []byte) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("multipartUpload: create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("multipartUpload: write content: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("multipartUpload: close writer: %v", err)
	}
	return c.DoRaw(t, http.MethodPost, path, w.FormDataContentType(), buf.Bytes())
}

// workspacePayload builds the full v2 workspace create body (all required
// fields per api/openapi-v2.json).
func workspacePayload(key, name, description string) map[string]any {
	return map[string]any{
		"key":                   key,
		"name":                  name,
		"description":           description,
		"color":                 "#4f8cff",
		"default_view":          "list",
		"icon":                  "briefcase",
		"is_personal":           false,
		"restricted_to_creator": false,
		"template_pack":         "",
	}
}

// UC-C1: workspace lifecycle over v2 (create/bootstrap/update/list/delete).
func TestWorkspacesLifecycle(t *testing.T) {
	c := e2eEnv.admin
	key := wscfgWSKey(t)
	name := wscfgName(t, "ws-name")
	newName := wscfgName(t, "ws-renamed")

	status, body := c.Post(t, "/api/v2/workspaces", workspacePayload(key, name, "e2e lifecycle workspace"))
	wantStatus(t, "POST", "/api/v2/workspaces", status, body, http.StatusCreated)
	wsID := v2ID(t, body)

	// Tier 2: persistence of the created row.
	row := dbQueryOne(t, "SELECT id, key, name FROM workspaces WHERE id = ?", wsID)
	if fmt.Sprint(row["key"]) != key || fmt.Sprint(row["name"]) != name {
		t.Fatalf("workspaces row mismatch after create: %v", row)
	}

	t.Run("bootstrap", func(t *testing.T) {
		status, body := c.Get(t, fmt.Sprintf("/api/workspaces/%d/bootstrap", wsID))
		wantStatus(t, "GET", "/api/workspaces/{id}/bootstrap", status, body, http.StatusOK)
		for _, field := range []string{"item_types", "statuses", "status_categories", "priorities"} {
			if n := jsonArrayLen(body, field); n <= 0 {
				t.Fatalf("bootstrap %s missing or empty (len=%d): %s", field, n, snippet(body))
			}
		}
	})

	t.Run("update", func(t *testing.T) {
		status, body := v2Patch(t, c, fmt.Sprintf("/api/v2/workspaces/%d", wsID), map[string]any{
			"name":        newName,
			"description": "updated by e2e lifecycle test",
		})
		wantStatus(t, "PATCH", "/api/v2/workspaces/{id}", status, body, http.StatusOK)

		status, body = c.Get(t, fmt.Sprintf("/api/v2/workspaces/%d", wsID))
		wantStatus(t, "GET", "/api/v2/workspaces/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", newName)

		row := dbQueryOne(t, "SELECT name, description FROM workspaces WHERE id = ?", wsID)
		if fmt.Sprint(row["name"]) != newName || fmt.Sprint(row["description"]) != "updated by e2e lifecycle test" {
			t.Fatalf("workspaces row not updated: %v", row)
		}
	})

	t.Run("list", func(t *testing.T) {
		status, body := c.Get(t, "/api/v2/workspaces")
		wantStatus(t, "GET", "/api/v2/workspaces", status, body, http.StatusOK)
		arr := v2DataArray(t, body)
		if _, ok := findByName(arr, newName); !ok {
			t.Fatalf("workspace %q not found in list: %s", newName, snippet(body))
		}
	})

	t.Run("delete_empty", func(t *testing.T) {
		key2 := wscfgWSKey(t)
		status, body := c.Post(t, "/api/v2/workspaces", workspacePayload(key2, key2, "e2e disposable"))
		wantStatus(t, "POST", "/api/v2/workspaces", status, body, http.StatusCreated)
		wsID2 := v2ID(t, body)

		status, body = c.Delete(t, fmt.Sprintf("/api/v2/workspaces/%d", wsID2))
		wantStatus(t, "DELETE", "/api/v2/workspaces/{id}", status, body, http.StatusNoContent, http.StatusOK)

		status, body = c.Get(t, fmt.Sprintf("/api/v2/workspaces/%d", wsID2))
		wantStatus(t, "GET", "/api/v2/workspaces/{id} (deleted)", status, body, http.StatusNotFound)
	})
}

// UC-C2: item types with hierarchy levels.
func TestWorkspacesItemTypes(t *testing.T) {
	c := e2eEnv.admin
	parentName := wscfgName(t, "it-parent")
	childName := wscfgName(t, "it-child")

	status, body := c.Post(t, "/api/v2/item-types", map[string]any{"name": parentName, "hierarchy_level": 0})
	wantStatus(t, "POST", "/api/v2/item-types", status, body, http.StatusCreated)
	parentID := v2ID(t, body)

	status, body = c.Post(t, "/api/v2/item-types", map[string]any{"name": childName, "hierarchy_level": 1})
	wantStatus(t, "POST", "/api/v2/item-types", status, body, http.StatusCreated)
	childID := v2ID(t, body)

	// OBSERVED: prepareItemType (services/catalog_mutation_service.go) validates
	// only the name and configuration-set membership; there is no parent/ancestry
	// FK check, so a level-2 type with no level-1 ancestor is accepted.
	status, body = c.Post(t, "/api/v2/item-types", map[string]any{"name": wscfgName(t, "it-orphan"), "hierarchy_level": 2})
	wantStatus(t, "POST", "/api/v2/item-types (orphan level)", status, body, http.StatusCreated)

	// List shows both with their levels.
	status, body = c.Get(t, "/api/v2/item-types")
	wantStatus(t, "GET", "/api/v2/item-types", status, body, http.StatusOK)
	arr := v2DataArray(t, body)
	parent, ok := findByName(arr, parentName)
	if !ok {
		t.Fatalf("parent item type %q not in list: %s", parentName, snippet(body))
	}
	child, ok := findByName(arr, childName)
	if !ok {
		t.Fatalf("child item type %q not in list: %s", childName, snippet(body))
	}
	if lvl, _ := parent["hierarchy_level"].(float64); int(lvl) != 0 {
		t.Fatalf("parent hierarchy_level = %v, want 0", parent["hierarchy_level"])
	}
	if lvl, _ := child["hierarchy_level"].(float64); int(lvl) != 1 {
		t.Fatalf("child hierarchy_level = %v, want 1", child["hierarchy_level"])
	}

	// Tier 2: persistence.
	row := dbQueryOne(t, "SELECT name, hierarchy_level FROM item_types WHERE id = ?", childID)
	if fmt.Sprint(row["name"]) != childName || fmt.Sprint(row["hierarchy_level"]) != "1" {
		t.Fatalf("item_types row mismatch: %v", row)
	}
	row = dbQueryOne(t, "SELECT name FROM item_types WHERE id = ?", parentID)
	if fmt.Sprint(row["name"]) != parentName {
		t.Fatalf("parent item_types row mismatch: %v", row)
	}
}

// UC-C3: custom fields of each supported type, assignment to an item type via
// the screen + configuration-set chain, option updates, deletion.
func TestWorkspacesCustomFields(t *testing.T) {
	c := e2eEnv.admin
	wsID := createWorkspace(t, c, wscfgWSKey(t))

	// Types per validFieldTypes (services/custom_field_provisioning.go).
	// "checkbox" is a canonical alias normalized to the stored type "boolean".
	spec := []struct {
		suffix, ftype, stored string
		options               string
	}{
		{"cf-text", "text", "text", ""},
		{"cf-number", "number", "number", ""},
		{"cf-select", "select", "select", `{"next_id":3,"items":[{"id":1,"label":"Alpha"},{"id":2,"label":"Beta"}]}`},
		{"cf-date", "date", "date", ""},
		{"cf-checkbox", "checkbox", "boolean", ""},
	}
	ids := map[string]int{}
	for _, s := range spec {
		name := wscfgName(t, s.suffix)
		payload := map[string]any{"name": name, "field_type": s.ftype}
		if s.options != "" {
			payload["options"] = s.options // JSON string per models.CustomFieldDefinition.Options
		}
		status, body := c.Post(t, "/api/v2/custom-fields", payload)
		wantStatus(t, "POST", "/api/v2/custom-fields ("+s.ftype+")", status, body, http.StatusCreated)
		id := v2ID(t, body)
		ids[name] = id

		// Tier 2: persistence of each definition.
		row := dbQueryOne(t, "SELECT name, field_type FROM custom_field_definitions WHERE id = ?", id)
		if fmt.Sprint(row["name"]) != name || fmt.Sprint(row["field_type"]) != s.stored {
			t.Fatalf("custom_field_definitions row mismatch for %s: %v", name, row)
		}

		// GET reflects the created field (the v2 settings endpoint is PUT-only;
		// per-field read is the reflection surface).
		status, body = c.Get(t, fmt.Sprintf("/api/v2/custom-fields/%d", id))
		wantStatus(t, "GET", "/api/v2/custom-fields/{id}", status, body, http.StatusOK)
		rec := v2DataRecord(t, body)
		if rec["name"] != name || rec["field_type"] != s.stored {
			t.Fatalf("custom field %d GET mismatch: name=%v field_type=%v", id, rec["name"], rec["field_type"])
		}
	}

	selectName := wscfgName(t, "cf-select")
	status, body := c.Get(t, fmt.Sprintf("/api/v2/custom-fields/%d", ids[selectName]))
	wantStatus(t, "GET", "/api/v2/custom-fields/{id} (options)", status, body, http.StatusOK)
	if !strings.Contains(string(body), "Alpha") {
		t.Fatalf("select options not reflected in GET: %s", snippet(body))
	}

	// Assign the fields to an item type via screen + configuration set.
	screenName := wscfgName(t, "cf-screen")
	status, body = c.Post(t, "/api/screens", map[string]any{"name": screenName, "description": "e2e custom field screen"})
	wantStatus(t, "POST", "/api/screens", status, body, http.StatusCreated, http.StatusOK)
	screenID := firstID(body)
	if screenID == 0 {
		t.Fatalf("no id in screen create response: %s", snippet(body))
	}

	textName := wscfgName(t, "cf-text")
	fields := []map[string]any{
		{"field_type": "custom", "field_identifier": fmt.Sprint(ids[textName]), "display_order": 1, "is_required": false, "field_width": ""},
		{"field_type": "default", "field_identifier": "title", "display_order": 2, "is_required": true, "field_width": ""},
	}
	status, body = c.Put(t, fmt.Sprintf("/api/screens/%d/fields", screenID), fields)
	wantStatus(t, "PUT", "/api/screens/{id}/fields", status, body, http.StatusOK)

	csName := wscfgName(t, "cf-cs")
	status, body = c.Post(t, "/api/configuration-sets", map[string]any{
		"name": csName, "description": "e2e custom field config set", "workspace_id": wsID,
	})
	wantStatus(t, "POST", "/api/configuration-sets", status, body, http.StatusCreated, http.StatusOK)
	csID := firstID(body)
	if csID == 0 {
		t.Fatalf("no id in configuration-set create response: %s", snippet(body))
	}

	itName := wscfgName(t, "cf-it")
	status, body = c.Post(t, "/api/v2/item-types", map[string]any{
		"name": itName, "hierarchy_level": 0, "configuration_set_ids": []int{csID},
	})
	wantStatus(t, "POST", "/api/v2/item-types (config set linked)", status, body, http.StatusCreated)
	itID := v2ID(t, body)

	status, body = c.Get(t, fmt.Sprintf("/api/v2/item-types/%d", itID))
	wantStatus(t, "GET", "/api/v2/item-types/{id}", status, body, http.StatusOK)
	// Membership lives in the join table; the item-type DTO does not expose
	// configuration_set_ids.
	if n := dbCount(t, "configuration_set_item_types", "configuration_set_id = ? AND item_type_id = ?", csID, itID); n != 1 {
		t.Fatalf("item type %d not linked to config set %d (join rows: %d)", itID, csID, n)
	}

	// Update the select option set; the change persists.
	newOpts := `{"next_id":3,"items":[{"id":1,"label":"Alpha"},{"id":2,"label":"Gamma"}]}`
	status, body = v2Patch(t, c, fmt.Sprintf("/api/v2/custom-fields/%d", ids[selectName]), map[string]any{"options": newOpts})
	wantStatus(t, "PATCH", "/api/v2/custom-fields/{id} (options)", status, body, http.StatusOK)
	status, body = c.Get(t, fmt.Sprintf("/api/v2/custom-fields/%d", ids[selectName]))
	wantStatus(t, "GET", "/api/v2/custom-fields/{id} (options after patch)", status, body, http.StatusOK)
	if !strings.Contains(string(body), "Gamma") || strings.Contains(string(body), "Beta") {
		t.Fatalf("option set update not persisted: %s", snippet(body))
	}

	// Delete a field assigned nowhere -> gone from the read endpoint.
	throwaway := wscfgName(t, "cf-del")
	status, body = c.Post(t, "/api/v2/custom-fields", map[string]any{"name": throwaway, "field_type": "text"})
	wantStatus(t, "POST", "/api/v2/custom-fields (throwaway)", status, body, http.StatusCreated)
	throwID := v2ID(t, body)
	status, body = c.Delete(t, fmt.Sprintf("/api/v2/custom-fields/%d", throwID))
	wantStatus(t, "DELETE", "/api/v2/custom-fields/{id}", status, body, http.StatusNoContent, http.StatusOK)
	status, body = c.Get(t, fmt.Sprintf("/api/v2/custom-fields/%d", throwID))
	wantStatus(t, "GET", "/api/v2/custom-fields/{id} (deleted)", status, body, http.StatusNotFound)
	if n := dbCount(t, "custom_field_definitions", "id = ?", throwID); n != 0 {
		t.Fatalf("deleted custom field still in DB: %d rows", n)
	}
}

// UC-C4: status categories, statuses, workflows and transitions.
func TestWorkspacesStatusesWorkflows(t *testing.T) {
	c := e2eEnv.admin

	catName := wscfgName(t, "cat")
	status, body := c.Post(t, "/api/v2/status-categories", map[string]any{"name": catName, "color": "#ff8800"})
	wantStatus(t, "POST", "/api/v2/status-categories", status, body, http.StatusCreated)
	catID := v2ID(t, body)

	stA := wscfgName(t, "st-a")
	stB := wscfgName(t, "st-b")
	status, body = c.Post(t, "/api/v2/statuses", map[string]any{"name": stA, "category_id": catID})
	wantStatus(t, "POST", "/api/v2/statuses (A)", status, body, http.StatusCreated)
	aID := v2ID(t, body)
	status, body = c.Post(t, "/api/v2/statuses", map[string]any{"name": stB, "category_id": catID})
	wantStatus(t, "POST", "/api/v2/statuses (B)", status, body, http.StatusCreated)
	bID := v2ID(t, body)

	// Tier 2: persistence of category + statuses.
	row := dbQueryOne(t, "SELECT name, color FROM status_categories WHERE id = ?", catID)
	if fmt.Sprint(row["name"]) != catName {
		t.Fatalf("status_categories row mismatch: %v", row)
	}
	row = dbQueryOne(t, "SELECT name, category_id FROM statuses WHERE id = ?", bID)
	if fmt.Sprint(row["name"]) != stB || fmt.Sprint(row["category_id"]) != fmt.Sprint(catID) {
		t.Fatalf("statuses row mismatch: %v", row)
	}

	wfName := wscfgName(t, "wf")
	status, body = c.Post(t, "/api/v2/workflows", map[string]any{"name": wfName})
	wantStatus(t, "POST", "/api/v2/workflows", status, body, http.StatusCreated)
	wfID := v2ID(t, body)

	// Transition A -> B.
	status, body = c.Put(t, fmt.Sprintf("/api/v2/workflows/%d/transitions", wfID), map[string]any{
		"transitions": []map[string]any{{"from_status_id": aID, "to_status_id": bID}},
	})
	wantStatus(t, "PUT", "/api/v2/workflows/{id}/transitions", status, body, http.StatusOK)

	// Transition list shows it.
	status, body = c.Get(t, fmt.Sprintf("/api/v2/workflows/%d/transitions", wfID))
	wantStatus(t, "GET", "/api/v2/workflows/{id}/transitions", status, body, http.StatusOK)
	if !strings.Contains(string(body), fmt.Sprint(aID)) || !strings.Contains(string(body), fmt.Sprint(bID)) {
		t.Fatalf("transition A->B not in list: %s", snippet(body))
	}

	// Tier 2: persistence of the transition row.
	row = dbQueryOne(t, "SELECT workflow_id, from_status_id, to_status_id FROM workflow_transitions WHERE workflow_id = ? AND to_status_id = ?", wfID, bID)
	if fmt.Sprint(row["from_status_id"]) != fmt.Sprint(aID) {
		t.Fatalf("workflow_transitions row mismatch: %v", row)
	}

	// OBSERVED: ReplaceTransitions (repository/workflow_repository.go) dedupes
	// duplicate (from,to) pairs by key and allows self-loops; no rejection code
	// exists for either, so the replace succeeds with 2 rows.
	status, body = c.Put(t, fmt.Sprintf("/api/v2/workflows/%d/transitions", wfID), map[string]any{
		"transitions": []map[string]any{
			{"from_status_id": aID, "to_status_id": bID},
			{"from_status_id": aID, "to_status_id": bID},
			{"from_status_id": aID, "to_status_id": aID},
		},
	})
	wantStatus(t, "PUT", "/api/v2/workflows/{id}/transitions (dup+self-loop)", status, body, http.StatusOK)
	if n := dbCount(t, "workflow_transitions", "workflow_id = ?", wfID); n != 2 {
		t.Fatalf("expected duplicate transition to be deduped (2 rows), got %d", n)
	}

	// Negative: transition target that does not exist -> 400
	// (ErrTransitionToStatusNotFound per repository/workflow_repository.go).
	status, body = c.Put(t, fmt.Sprintf("/api/v2/workflows/%d/transitions", wfID), map[string]any{
		"transitions": []map[string]any{{"from_status_id": aID, "to_status_id": 999999}},
	})
	wantStatus(t, "PUT", "/api/v2/workflows/{id}/transitions (bad target)", status, body, http.StatusBadRequest)

	// Negative: status without category -> 400 ("Category ID is required").
	status, body = c.Post(t, "/api/v2/statuses", map[string]any{"name": wscfgName(t, "st-nocat")})
	wantStatus(t, "POST", "/api/v2/statuses (no category)", status, body, http.StatusBadRequest)

	// Negative: duplicate status name -> 409 conflict.
	status, body = c.Post(t, "/api/v2/statuses", map[string]any{"name": stA, "category_id": catID})
	wantStatus(t, "POST", "/api/v2/statuses (duplicate)", status, body, http.StatusConflict)
}

// UC-C5: priorities, screens (+ field layout), link types, collections CRUD.
func TestWorkspacesMiscCatalog(t *testing.T) {
	c := e2eEnv.admin

	t.Run("priority", func(t *testing.T) {
		name := wscfgName(t, "pri")
		status, body := c.Post(t, "/api/v2/priorities", map[string]any{"name": name})
		wantStatus(t, "POST", "/api/v2/priorities", status, body, http.StatusCreated)
		id := v2ID(t, body)

		status, body = c.Get(t, fmt.Sprintf("/api/v2/priorities/%d", id))
		wantStatus(t, "GET", "/api/v2/priorities/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", name)

		newName := wscfgName(t, "pri-renamed")
		status, body = v2Patch(t, c, fmt.Sprintf("/api/v2/priorities/%d", id), map[string]any{"name": newName})
		wantStatus(t, "PATCH", "/api/v2/priorities/{id}", status, body, http.StatusOK)
		status, body = c.Get(t, fmt.Sprintf("/api/v2/priorities/%d", id))
		wantStatus(t, "GET", "/api/v2/priorities/{id} (after patch)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", newName)

		row := dbQueryOne(t, "SELECT name FROM priorities WHERE id = ?", id)
		if fmt.Sprint(row["name"]) != newName {
			t.Fatalf("priorities row not updated: %v", row)
		}

		status, body = c.Delete(t, fmt.Sprintf("/api/v2/priorities/%d", id))
		wantStatus(t, "DELETE", "/api/v2/priorities/{id}", status, body, http.StatusNoContent, http.StatusOK)
		status, body = c.Get(t, fmt.Sprintf("/api/v2/priorities/%d", id))
		wantStatus(t, "GET", "/api/v2/priorities/{id} (deleted)", status, body, http.StatusNotFound)
	})

	t.Run("screen", func(t *testing.T) {
		name := wscfgName(t, "scr")
		status, body := c.Post(t, "/api/screens", map[string]any{"name": name, "description": "e2e screen"})
		wantStatus(t, "POST", "/api/screens", status, body, http.StatusCreated, http.StatusOK)
		id := firstID(body)
		if id == 0 {
			t.Fatalf("no id in screen create response: %s", snippet(body))
		}

		status, body = c.Get(t, fmt.Sprintf("/api/screens/%d", id))
		wantStatus(t, "GET", "/api/screens/{id}", status, body, http.StatusOK)
		if !strings.Contains(string(body), name) {
			t.Fatalf("screen GET does not contain name: %s", snippet(body))
		}

		// Field layout round-trip (v1 admin route).
		cfName := wscfgName(t, "scr-cf")
		status, body = c.Post(t, "/api/v2/custom-fields", map[string]any{"name": cfName, "field_type": "text"})
		wantStatus(t, "POST", "/api/v2/custom-fields (screen field)", status, body, http.StatusCreated)
		cfID := v2ID(t, body)

		fields := []map[string]any{
			{"field_type": "custom", "field_identifier": fmt.Sprint(cfID), "display_order": 1, "is_required": false, "field_width": ""},
			{"field_type": "default", "field_identifier": "title", "display_order": 2, "is_required": true, "field_width": ""},
		}
		status, body = c.Put(t, fmt.Sprintf("/api/screens/%d/fields", id), fields)
		wantStatus(t, "PUT", "/api/screens/{id}/fields", status, body, http.StatusOK)

		status, body = c.Get(t, fmt.Sprintf("/api/screens/%d/fields", id))
		wantStatus(t, "GET", "/api/screens/{id}/fields", status, body, http.StatusOK)
		if !strings.Contains(string(body), fmt.Sprint(cfID)) {
			t.Fatalf("screen fields round-trip missing custom field %d: %s", cfID, snippet(body))
		}

		status, body = c.Delete(t, fmt.Sprintf("/api/screens/%d", id))
		wantStatus(t, "DELETE", "/api/screens/{id}", status, body, http.StatusNoContent, http.StatusOK)
		status, body = c.Get(t, fmt.Sprintf("/api/screens/%d", id))
		wantStatus(t, "GET", "/api/screens/{id} (deleted)", status, body, http.StatusNotFound)
	})

	t.Run("link_type", func(t *testing.T) {
		name := wscfgName(t, "lt")
		status, body := c.Post(t, "/api/v2/link-types", map[string]any{
			"name": name, "forward_label": "e2e fwd", "reverse_label": "e2e rev",
		})
		wantStatus(t, "POST", "/api/v2/link-types", status, body, http.StatusCreated)
		id := v2ID(t, body)

		status, body = c.Get(t, fmt.Sprintf("/api/v2/link-types/%d", id))
		wantStatus(t, "GET", "/api/v2/link-types/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", name)
		assertJSONField(t, body, "data.forward_label", "e2e fwd")

		status, body = v2Patch(t, c, fmt.Sprintf("/api/v2/link-types/%d", id), map[string]any{"description": "e2e link type desc"})
		wantStatus(t, "PATCH", "/api/v2/link-types/{id}", status, body, http.StatusOK)
		status, body = c.Get(t, fmt.Sprintf("/api/v2/link-types/%d", id))
		wantStatus(t, "GET", "/api/v2/link-types/{id} (after patch)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.description", "e2e link type desc")

		row := dbQueryOne(t, "SELECT name, forward_label FROM link_types WHERE id = ?", id)
		if fmt.Sprint(row["name"]) != name || fmt.Sprint(row["forward_label"]) != "e2e fwd" {
			t.Fatalf("link_types row mismatch: %v", row)
		}

		status, body = c.Delete(t, fmt.Sprintf("/api/v2/link-types/%d", id))
		wantStatus(t, "DELETE", "/api/v2/link-types/{id}", status, body, http.StatusNoContent, http.StatusOK)
		status, body = c.Get(t, fmt.Sprintf("/api/v2/link-types/%d", id))
		wantStatus(t, "GET", "/api/v2/link-types/{id} (deleted)", status, body, http.StatusNotFound)
	})

	t.Run("collection", func(t *testing.T) {
		name := wscfgName(t, "coll")
		status, body := c.Post(t, "/api/v2/collections", map[string]any{"name": name, "description": "e2e collection"})
		wantStatus(t, "POST", "/api/v2/collections", status, body, http.StatusCreated)
		id := v2ID(t, body)

		status, body = c.Get(t, fmt.Sprintf("/api/v2/collections/%d", id))
		wantStatus(t, "GET", "/api/v2/collections/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.name", name)

		status, body = v2Patch(t, c, fmt.Sprintf("/api/v2/collections/%d", id), map[string]any{"description": "e2e collection updated"})
		wantStatus(t, "PATCH", "/api/v2/collections/{id}", status, body, http.StatusOK)
		status, body = c.Get(t, fmt.Sprintf("/api/v2/collections/%d", id))
		wantStatus(t, "GET", "/api/v2/collections/{id} (after patch)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.description", "e2e collection updated")

		row := dbQueryOne(t, "SELECT name FROM collections WHERE id = ?", id)
		if fmt.Sprint(row["name"]) != name {
			t.Fatalf("collections row mismatch: %v", row)
		}

		status, body = c.Delete(t, fmt.Sprintf("/api/v2/collections/%d", id))
		wantStatus(t, "DELETE", "/api/v2/collections/{id}", status, body, http.StatusNoContent, http.StatusOK)
		status, body = c.Get(t, fmt.Sprintf("/api/v2/collections/%d", id))
		wantStatus(t, "GET", "/api/v2/collections/{id} (deleted)", status, body, http.StatusNotFound)
	})
}

// UC-C6: configuration-set export -> import into a second workspace ->
// analyze-migration -> execute-migration.
func TestWorkspacesConfigSetsExportImport(t *testing.T) {
	c := e2eEnv.admin
	wsA := createWorkspace(t, c, wscfgWSKey(t))
	wsB := createWorkspace(t, c, wscfgWSKey(t))

	// Build a configuration set in workspace A with its own workflow + statuses.
	wfName := wscfgName(t, "csa-wf")
	status, body := c.Post(t, "/api/v2/workflows", map[string]any{"name": wfName})
	wantStatus(t, "POST", "/api/v2/workflows (A)", status, body, http.StatusCreated)
	wfID := v2ID(t, body)

	catName := wscfgName(t, "csa-cat")
	status, body = c.Post(t, "/api/v2/status-categories", map[string]any{"name": catName, "color": "#00aa88"})
	wantStatus(t, "POST", "/api/v2/status-categories (A)", status, body, http.StatusCreated)
	catID := v2ID(t, body)

	stA := wscfgName(t, "csa-st-a")
	stB := wscfgName(t, "csa-st-b")
	status, body = c.Post(t, "/api/v2/statuses", map[string]any{"name": stA, "category_id": catID})
	wantStatus(t, "POST", "/api/v2/statuses (A1)", status, body, http.StatusCreated)
	aID := v2ID(t, body)
	status, body = c.Post(t, "/api/v2/statuses", map[string]any{"name": stB, "category_id": catID})
	wantStatus(t, "POST", "/api/v2/statuses (A2)", status, body, http.StatusCreated)
	bID := v2ID(t, body)

	status, body = c.Put(t, fmt.Sprintf("/api/v2/workflows/%d/transitions", wfID), map[string]any{
		"transitions": []map[string]any{{"from_status_id": aID, "to_status_id": bID}},
	})
	wantStatus(t, "PUT", "/api/v2/workflows/{id}/transitions (A)", status, body, http.StatusOK)

	csName := wscfgName(t, "csa-cs")
	status, body = c.Post(t, "/api/configuration-sets", map[string]any{
		"name": csName, "description": "e2e export source set", "workspace_id": wsA, "workflow_id": wfID,
	})
	wantStatus(t, "POST", "/api/configuration-sets (A)", status, body, http.StatusCreated, http.StatusOK)
	csID := firstID(body)
	if csID == 0 {
		t.Fatalf("no id in configuration-set create response: %s", snippet(body))
	}

	itName := wscfgName(t, "csa-it")
	status, body = c.Post(t, "/api/v2/item-types", map[string]any{
		"name": itName, "hierarchy_level": 0, "configuration_set_ids": []int{csID},
	})
	wantStatus(t, "POST", "/api/v2/item-types (A)", status, body, http.StatusCreated)

	beforeCS := dbCount(t, "configuration_sets", "1=1")
	maxRow := dbQueryOne(t, "SELECT MAX(id) AS m FROM configuration_sets")
	beforeMaxID := toInt(maxRow["m"])

	// Export the set from A.
	status, body = c.Get(t, fmt.Sprintf("/api/configuration-sets/%d/export", csID))
	wantStatus(t, "GET", "/api/configuration-sets/{id}/export", status, body, http.StatusOK)
	var tpl struct {
		SchemaVersion int `json:"schema_version"`
		Payload       struct {
			ConfigurationSet struct {
				Name string `json:"name"`
			} `json:"configuration_set"`
			Workflows []struct {
				Name string `json:"name"`
			} `json:"workflows"`
			ItemTypes []struct {
				Name string `json:"name"`
			} `json:"item_types"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &tpl); err != nil {
		t.Fatalf("export body is not a config-set template: %v\n%s", err, snippet(body))
	}
	if tpl.Payload.ConfigurationSet.Name != csName {
		t.Fatalf("export payload configuration_set.name = %q, want %q", tpl.Payload.ConfigurationSet.Name, csName)
	}
	if len(tpl.Payload.Workflows) == 0 {
		t.Fatalf("export bundle contains no workflows: %s", snippet(body))
	}

	// Import the bundle (multipart "file" field per handlers/configuration_sets_export_import.go).
	status, body = multipartUpload(t, c, "/api/configuration-sets/import", "file", "e2e-export.json", body)
	wantStatus(t, "POST", "/api/configuration-sets/import", status, body, http.StatusCreated, http.StatusOK)

	// Tier 2: the import created a new configuration-set row.
	if after := dbCount(t, "configuration_sets", "1=1"); after < beforeCS+1 {
		t.Fatalf("import did not create a configuration set (before=%d after=%d)", beforeCS, after)
	}
	newRow := dbQueryOne(t, "SELECT id FROM configuration_sets WHERE id > ? ORDER BY id DESC LIMIT 1", beforeMaxID)
	newCSID := toInt(newRow["id"])
	if newCSID == 0 {
		t.Fatalf("import did not create a configuration-set row after id %d", beforeMaxID)
	}

	// Analyze migration of workspace B onto the imported set: no items exist in
	// B yet, so nothing is affected.
	status, body = c.Get(t, fmt.Sprintf("/api/configuration-sets/%d/analyze-migration?workspace_id=%d", newCSID, wsB))
	wantStatus(t, "GET", "/api/configuration-sets/{id}/analyze-migration", status, body, http.StatusOK)
	assertJSONField(t, body, "total_affected_items", 0)

	// Execute the migration for workspace B.
	status, body = c.Post(t, "/api/configuration-sets/execute-migration", map[string]any{
		"configuration_set_id": newCSID, "workspace_ids": []int{wsB},
	})
	wantStatus(t, "POST", "/api/configuration-sets/execute-migration", status, body, http.StatusOK)

	// After migration, workspace B's item types match A's: equal counts and at
	// least one shared type name (catalog entities are global; the importer
	// resolves existing identities rather than duplicating them).
	statusA, bodyA := c.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/item-types", wsA))
	wantStatus(t, "GET", "/api/v2/workspaces/{id}/item-types (A)", statusA, bodyA, http.StatusOK)
	statusB, bodyB := c.Get(t, fmt.Sprintf("/api/v2/workspaces/%d/item-types", wsB))
	wantStatus(t, "GET", "/api/v2/workspaces/{id}/item-types (B)", statusB, bodyB, http.StatusOK)
	arrA := v2DataArray(t, bodyA)
	arrB := v2DataArray(t, bodyB)
	if len(arrA) == 0 || len(arrB) == 0 {
		t.Fatalf("scoped item-type lists empty after migration: A=%s B=%s", snippet(bodyA), snippet(bodyB))
	}
	if len(arrA) != len(arrB) {
		t.Fatalf("item type counts differ after migration: A=%d B=%d (A=%s, B=%s)", len(arrA), len(arrB), snippet(bodyA), snippet(bodyB))
	}
	shared := 0
	for _, item := range arrA {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := obj["name"].(string)
		if name == "" {
			continue
		}
		if _, ok := findByName(arrB, name); ok {
			shared++
		}
	}
	if shared == 0 {
		t.Fatalf("no shared item type names between A and B after migration: A=%s B=%s", snippet(bodyA), snippet(bodyB))
	}
}

// Negatives for the workspace/catalog surface.
func TestWorkspacesNegatives(t *testing.T) {
	c := e2eEnv.admin

	t.Run("duplicate_workspace_key", func(t *testing.T) {
		key := wscfgWSKey(t)
		status, body := c.Post(t, "/api/v2/workspaces", workspacePayload(key, key, "e2e dup"))
		wantStatus(t, "POST", "/api/v2/workspaces (first)", status, body, http.StatusCreated)

		// Pinned: duplicate key -> 409 conflict ("workspace key already exists"
		// in restapi/v2/scoped_catalog.go).
		status, body = c.Post(t, "/api/v2/workspaces", workspacePayload(key, wscfgName(t, "dup-name"), "e2e dup again"))
		wantStatus(t, "POST", "/api/v2/workspaces (duplicate key)", status, body, http.StatusConflict)
	})

	t.Run("custom_field_missing_name", func(t *testing.T) {
		// Pinned: 400 invalid_request ("Field name is required" in
		// services/custom_field_provisioning.go validateAndNormalize).
		status, body := c.Post(t, "/api/v2/custom-fields", map[string]any{"field_type": "text"})
		wantStatus(t, "POST", "/api/v2/custom-fields (no name)", status, body, http.StatusBadRequest)
	})

	t.Run("custom_field_invalid_type", func(t *testing.T) {
		status, body := c.Post(t, "/api/v2/custom-fields", map[string]any{"name": wscfgName(t, "cf-bad"), "field_type": "not-a-type"})
		wantStatus(t, "POST", "/api/v2/custom-fields (bad type)", status, body, http.StatusBadRequest)
	})

	t.Run("item_type_missing_name", func(t *testing.T) {
		status, body := c.Post(t, "/api/v2/item-types", map[string]any{"hierarchy_level": 0})
		wantStatus(t, "POST", "/api/v2/item-types (no name)", status, body, http.StatusBadRequest)
	})

	t.Run("status_category_missing_color", func(t *testing.T) {
		status, body := c.Post(t, "/api/v2/status-categories", map[string]any{"name": wscfgName(t, "cat-nocolor")})
		wantStatus(t, "POST", "/api/v2/status-categories (no color)", status, body, http.StatusBadRequest)
	})
}
