package e2e

// End-to-end use-case coverage for work items (UC-D1..D4): lifecycle with
// custom fields, comments and attachments; workflow transitions; hierarchy
// and bulk operations; CQL search. Runs against the real server on a fresh
// SQLite database via the shared harness.

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// itemMultipartUpload builds a multipart/form-data body with one file part.
func itemMultipartUpload(t *testing.T, field, filename string, content []byte) (string, []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("e2e: create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("e2e: write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("e2e: close multipart writer: %v", err)
	}
	return w.FormDataContentType(), buf.Bytes()
}

// backlogItemIDs extracts the item ids from a v2 page envelope.
func backlogItemIDs(t *testing.T, body []byte) []int {
	t.Helper()
	env := mustJSON(t, body).(map[string]any)
	data, _ := env["data"].([]any)
	ids := make([]int, 0, len(data))
	for _, rec := range data {
		item, ok := rec.(map[string]any)
		if !ok {
			t.Fatalf("backlog page entry is not an object: %s", snippet(body))
		}
		id, _ := item["id"].(float64)
		ids = append(ids, int(id))
	}
	return ids
}

// TestItemLifecycle covers UC-D1: create/get/patch/delete round-trip plus
// custom field values, comments and attachment upload/download.
func TestItemLifecycle(t *testing.T) {
	admin := e2eEnv.admin
	wsID := createWorkspace(t, admin, wscfgWSKey(t))

	t.Run("CreateGetPatchDelete", func(t *testing.T) {
		title := "E2E D1 item"
		status, body := admin.Post(t, "/api/v2/items", map[string]any{"workspace_id": wsID, "title": title})
		wantStatus(t, "POST", "/api/v2/items", status, body, http.StatusCreated)
		id := v2ID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/items/%d", id))
		wantStatus(t, "GET", "/api/v2/items/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "data.title", title)

		status, body = v2Patch(t, admin, fmt.Sprintf("/api/v2/items/%d", id), map[string]any{
			"title": title + " (renamed)",
		})
		wantStatus(t, "PATCH", "/api/v2/items/{id}", status, body, http.StatusOK)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/items/%d", id))
		wantStatus(t, "GET", "/api/v2/items/{id} (after patch)", status, body, http.StatusOK)
		assertJSONField(t, body, "data.title", title+" (renamed)")

		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/items/%d", id))
		wantStatus(t, "DELETE", "/api/v2/items/{id}", status, body, http.StatusNoContent, http.StatusOK)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/items/%d", id))
		wantStatus(t, "GET", "/api/v2/items/{id} (deleted)", status, body, http.StatusNotFound)
	})

	t.Run("CustomFieldValue", func(t *testing.T) {
		// Chain per UC-C3: custom field -> screen -> configuration set -> item type.
		cfName := wscfgName(t, "d1-cf")
		status, body := admin.Post(t, "/api/v2/custom-fields", map[string]any{"name": cfName, "field_type": "text"})
		wantStatus(t, "POST", "/api/v2/custom-fields (d1)", status, body, http.StatusCreated)
		cfID := v2ID(t, body)

		screenName := wscfgName(t, "d1-screen")
		status, body = admin.Post(t, "/api/screens", map[string]any{"name": screenName})
		wantStatus(t, "POST", "/api/screens (d1)", status, body, http.StatusCreated, http.StatusOK)
		screenID := firstID(body)
		if screenID == 0 {
			t.Fatalf("no id in screen create response: %s", snippet(body))
		}

		fields := []map[string]any{
			{"field_type": "custom", "field_identifier": fmt.Sprint(cfID), "display_order": 1, "is_required": false},
		}
		status, body = admin.Put(t, fmt.Sprintf("/api/screens/%d/fields", screenID), fields)
		wantStatus(t, "PUT", "/api/screens/{id}/fields (d1)", status, body, http.StatusOK)

		csName := wscfgName(t, "d1-cs")
		status, body = admin.Post(t, "/api/configuration-sets", map[string]any{"name": csName, "workspace_id": wsID})
		wantStatus(t, "POST", "/api/configuration-sets (d1)", status, body, http.StatusCreated, http.StatusOK)
		csID := firstID(body)
		if csID == 0 {
			t.Fatalf("no id in configuration-set create response: %s", snippet(body))
		}

		itName := wscfgName(t, "d1-it")
		status, body = admin.Post(t, "/api/v2/item-types", map[string]any{
			"name": itName, "hierarchy_level": 0, "configuration_set_ids": []int{csID},
		})
		wantStatus(t, "POST", "/api/v2/item-types (d1)", status, body, http.StatusCreated)
		itID := v2ID(t, body)

		status, body = admin.Post(t, "/api/v2/items", map[string]any{
			"workspace_id":        wsID,
			"title":               "E2E D1 cf item",
			"item_type_id":        itID,
			"custom_field_values": map[string]any{fmt.Sprint(cfID): "e2e-d1-value"},
		})
		wantStatus(t, "POST", "/api/v2/items (cf value)", status, body, http.StatusCreated)
		id := v2ID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/items/%d", id))
		wantStatus(t, "GET", "/api/v2/items/{id} (cf round-trip)", status, body, http.StatusOK)
		assertJSONField(t, body, fmt.Sprintf("data.custom_field_values.%d", cfID), "e2e-d1-value")
	})

	t.Run("Comment", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/items", map[string]any{"workspace_id": wsID, "title": "E2E D1 comment item"})
		wantStatus(t, "POST", "/api/v2/items (comment)", status, body, http.StatusCreated)
		id := v2ID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/items/%d/comments", id), map[string]any{
			"content": "E2E D1 comment text",
		})
		wantStatus(t, "POST", "/api/v2/items/{id}/comments", status, body, http.StatusCreated)

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/items/%d/comments", id))
		wantStatus(t, "GET", "/api/v2/items/{id}/comments", status, body, http.StatusOK)
		if !strings.Contains(string(body), "E2E D1 comment text") {
			t.Fatalf("comment not in list: %s", snippet(body))
		}
	})

	t.Run("Attachment", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/items", map[string]any{"workspace_id": wsID, "title": "E2E D1 attachment item"})
		wantStatus(t, "POST", "/api/v2/items (attachment)", status, body, http.StatusCreated)
		id := v2ID(t, body)

		payload := []byte("windshift e2e attachment payload")
		contentType, raw := itemMultipartUpload(t, "file", "e2e-d1.txt", payload)
		status, body = admin.DoRaw(t, http.MethodPost, fmt.Sprintf("/api/v2/items/%d/attachments", id), contentType, raw)
		wantStatus(t, "POST", "/api/v2/items/{id}/attachments (upload)", status, body, http.StatusCreated)
		attID := v2ID(t, body)

		status, content := admin.Get(t, fmt.Sprintf("/api/v2/attachments/%d/content", attID))
		wantStatus(t, "GET", "/api/v2/attachments/{id}/content", status, content, http.StatusOK)
		if !bytes.Equal(content, payload) {
			t.Fatalf("downloaded bytes mismatch: got %q want %q", content, payload)
		}

		status, body = admin.Delete(t, fmt.Sprintf("/api/v2/attachments/%d", attID))
		wantStatus(t, "DELETE", "/api/v2/attachments/{id}", status, body, http.StatusNoContent, http.StatusOK)
	})
}

// d2WorkflowSetup creates a status category with three statuses (A -> B -> C),
// a workflow over them, and links the workflow to wsID through a configuration
// set + item type so items of that type resolve the workflow.
func d2WorkflowSetup(t *testing.T, admin *Client, wsID int) (itID, aID, bID, cID int) {
	t.Helper()

	catName := wscfgName(t, "d2-cat")
	status, body := admin.Post(t, "/api/v2/status-categories", map[string]any{"name": catName, "color": "#0ea5e9"})
	wantStatus(t, "POST", "/api/v2/status-categories (d2)", status, body, http.StatusCreated)
	catID := v2ID(t, body)

	mkStatus := func(suffix string) int {
		status, body := admin.Post(t, "/api/v2/statuses", map[string]any{
			"name": wscfgName(t, suffix), "category_id": catID,
		})
		wantStatus(t, "POST", "/api/v2/statuses ("+suffix+")", status, body, http.StatusCreated)
		return v2ID(t, body)
	}
	aID, bID, cID = mkStatus("d2-a"), mkStatus("d2-b"), mkStatus("d2-c")

	wfName := wscfgName(t, "d2-wf")
	status, body = admin.Post(t, "/api/v2/workflows", map[string]any{"name": wfName})
	wantStatus(t, "POST", "/api/v2/workflows (d2)", status, body, http.StatusCreated)
	wfID := v2ID(t, body)

	// A NULL from_status_id marks the initial status (creation-only marker).
	status, body = admin.Put(t, fmt.Sprintf("/api/v2/workflows/%d/transitions", wfID), map[string]any{
		"transitions": []map[string]any{
			{"to_status_id": aID},
			{"from_status_id": aID, "to_status_id": bID},
			{"from_status_id": bID, "to_status_id": cID},
		},
	})
	wantStatus(t, "PUT", "/api/v2/workflows/{id}/transitions (d2)", status, body, http.StatusOK)

	// Link the workflow to this workspace: a configuration set carrying the
	// workflow, assigned through the workspace's configuration-set endpoint.
	csName := wscfgName(t, "d2-cs")
	status, body = admin.Post(t, "/api/configuration-sets", map[string]any{
		"name": csName, "workflow_id": wfID,
	})
	wantStatus(t, "POST", "/api/configuration-sets (d2)", status, body, http.StatusCreated)
	csID := firstID(body)
	if csID == 0 {
		t.Fatalf("no id in configuration-set create response: %s", snippet(body))
	}

	status, body = admin.Put(t, fmt.Sprintf("/api/workspaces/%d/configuration-set", wsID), map[string]any{
		"configuration_set_id": csID,
	})
	wantStatus(t, "PUT", "/api/workspaces/{id}/configuration-set (d2)", status, body, http.StatusOK)

	itName := wscfgName(t, "d2-it")
	status, body = admin.Post(t, "/api/v2/item-types", map[string]any{
		"name": itName, "hierarchy_level": 0, "configuration_set_ids": []int{csID},
	})
	wantStatus(t, "POST", "/api/v2/item-types (d2)", status, body, http.StatusCreated)
	itID = v2ID(t, body)
	return itID, aID, bID, cID
}

// TestItemWorkflowTransition covers UC-D2: valid transition chain with DB and
// history verification, plus rejection of a transition absent from the graph.
func TestItemWorkflowTransition(t *testing.T) {
	admin := e2eEnv.admin
	wsID := createWorkspace(t, admin, wscfgWSKey(t))
	itID, aID, bID, cID := d2WorkflowSetup(t, admin, wsID)

	mkItem := func(title string, statusID int) int {
		status, body := admin.Post(t, "/api/v2/items", map[string]any{
			"workspace_id": wsID, "title": title, "item_type_id": itID, "status_id": statusID,
		})
		wantStatus(t, "POST", "/api/v2/items (d2 "+title+")", status, body, http.StatusCreated)
		return v2ID(t, body)
	}

	t.Run("ValidTransitionChain", func(t *testing.T) {
		id := mkItem("E2E D2 chain item", aID)

		status, body := admin.Get(t, fmt.Sprintf("/api/v2/items/%d/available-transitions", id))
		wantStatus(t, "GET", "/api/v2/items/{id}/available-transitions", status, body, http.StatusOK)
		if !strings.Contains(string(body), fmt.Sprint(bID)) {
			t.Fatalf("transition to B (%d) not offered from A: %s", bID, snippet(body))
		}
		if strings.Contains(string(body), fmt.Sprint(cID)) {
			t.Fatalf("unreachable C (%d) offered from A: %s", cID, snippet(body))
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/items/%d/transition", id), map[string]any{"to_status_id": bID})
		wantStatus(t, "POST", "/api/v2/items/{id}/transition (A->B)", status, body, http.StatusOK)
		row := dbQueryOne(t, "SELECT status_id FROM items WHERE id = ?", id)
		if fmt.Sprint(row["status_id"]) != fmt.Sprint(bID) {
			t.Fatalf("items.status_id after A->B: %v", row)
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/v2/items/%d/transition", id), map[string]any{"to_status_id": cID})
		wantStatus(t, "POST", "/api/v2/items/{id}/transition (B->C)", status, body, http.StatusOK)
		row = dbQueryOne(t, "SELECT status_id FROM items WHERE id = ?", id)
		if fmt.Sprint(row["status_id"]) != fmt.Sprint(cID) {
			t.Fatalf("items.status_id after B->C: %v", row)
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/items/%d/history", id))
		wantStatus(t, "GET", "/api/v2/items/{id}/history", status, body, http.StatusOK)
		if !strings.Contains(string(body), fmt.Sprint(bID)) || !strings.Contains(string(body), fmt.Sprint(cID)) {
			t.Fatalf("history missing transition rows: %s", snippet(body))
		}
	})

	t.Run("InvalidTransitionRejected", func(t *testing.T) {
		id := mkItem("E2E D2 invalid item", aID)

		// A -> C is not in the workflow graph.
		status, body := admin.Post(t, fmt.Sprintf("/api/v2/items/%d/transition", id), map[string]any{"to_status_id": cID})
		wantStatus(t, "POST", "/api/v2/items/{id}/transition (A->C invalid)", status, body, http.StatusBadRequest)

		row := dbQueryOne(t, "SELECT status_id FROM items WHERE id = ?", id)
		if fmt.Sprint(row["status_id"]) != fmt.Sprint(aID) {
			t.Fatalf("rejected transition changed status: %v", row)
		}
	})
}

// TestItemHierarchyAndBatch covers UC-D3: parent/children tree endpoints,
// batch-ancestors, bulk-update, bulk-transition and bulk-delete.
func TestItemHierarchyAndBatch(t *testing.T) {
	admin := e2eEnv.admin
	wsID := createWorkspace(t, admin, wscfgWSKey(t))
	itID, aID, bID, _ := d2WorkflowSetup(t, admin, wsID)

	// Children must use an item type one hierarchy level below the parent's.
	csRow := dbQueryOne(t, "SELECT configuration_set_id FROM workspace_configuration_sets WHERE workspace_id = ?", wsID)
	csID, err := strconv.Atoi(fmt.Sprint(csRow["configuration_set_id"]))
	if err != nil {
		t.Fatalf("parse config set id %v: %v", csRow["configuration_set_id"], err)
	}
	childItName := wscfgName(t, "d3-child-it")
	status, body := admin.Post(t, "/api/v2/item-types", map[string]any{
		"name": childItName, "hierarchy_level": 1, "configuration_set_ids": []int{csID},
	})
	wantStatus(t, "POST", "/api/v2/item-types (d3 child)", status, body, http.StatusCreated)
	childItID := v2ID(t, body)

	mkItem := func(title string, itemType int, parentID *int) int {
		payload := map[string]any{"workspace_id": wsID, "title": title, "item_type_id": itemType, "status_id": aID}
		if parentID != nil {
			payload["parent_id"] = *parentID
		}
		status, body := admin.Post(t, "/api/v2/items", payload)
		wantStatus(t, "POST", "/api/v2/items (d3 "+title+")", status, body, http.StatusCreated)
		return v2ID(t, body)
	}

	parent := mkItem("E2E D3 parent", itID, nil)
	c1 := mkItem("E2E D3 child 1", childItID, &parent)
	c2 := mkItem("E2E D3 child 2", childItID, &parent)

	t.Run("TreeEndpoints", func(t *testing.T) {
		status, body := admin.Get(t, fmt.Sprintf("/api/v2/items/%d/children", parent))
		wantStatus(t, "GET", "/api/v2/items/{id}/children", status, body, http.StatusOK)
		for _, want := range []int{c1, c2} {
			if !strings.Contains(string(body), fmt.Sprint(want)) {
				t.Fatalf("child %d not in children list: %s", want, snippet(body))
			}
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/v2/items/%d/ancestors", c1))
		wantStatus(t, "GET", "/api/v2/items/{id}/ancestors", status, body, http.StatusOK)
		if !strings.Contains(string(body), fmt.Sprint(parent)) {
			t.Fatalf("parent %d not in ancestors: %s", parent, snippet(body))
		}

		status, body = admin.Post(t, "/api/v2/items/batch-ancestors", map[string]any{"ids": []int{c1, c2}})
		wantStatus(t, "POST", "/api/v2/items/batch-ancestors", status, body, http.StatusOK)
		if !strings.Contains(string(body), fmt.Sprint(parent)) {
			t.Fatalf("batch-ancestors missing parent chain: %s", snippet(body))
		}
	})

	t.Run("BulkUpdate", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/items/bulk-update", map[string]any{
			"item_ids": []int{c1, c2},
			"set":      map[string]any{"title": "E2E D3 bulk renamed"},
		})
		wantStatus(t, "POST", "/api/v2/items/bulk-update", status, body, http.StatusOK)

		for _, id := range []int{c1, c2} {
			row := dbQueryOne(t, "SELECT title FROM items WHERE id = ?", id)
			if fmt.Sprint(row["title"]) != "E2E D3 bulk renamed" {
				t.Fatalf("bulk-update did not persist on item %d: %v", id, row)
			}
		}
	})

	t.Run("BulkTransition", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/items/bulk-transition", map[string]any{
			"item_ids": []int{c1, c2}, "to_status_id": bID,
		})
		wantStatus(t, "POST", "/api/v2/items/bulk-transition", status, body, http.StatusOK)

		for _, id := range []int{c1, c2} {
			row := dbQueryOne(t, "SELECT status_id FROM items WHERE id = ?", id)
			if fmt.Sprint(row["status_id"]) != fmt.Sprint(bID) {
				t.Fatalf("bulk-transition did not move item %d: %v", id, row)
			}
		}
	})

	t.Run("BulkDelete", func(t *testing.T) {
		status, body := admin.Post(t, "/api/v2/items/bulk-delete", map[string]any{"item_ids": []int{c1, c2}})
		wantStatus(t, "POST", "/api/v2/items/bulk-delete", status, body, http.StatusOK)

		for _, id := range []int{c1, c2} {
			status, body = admin.Get(t, fmt.Sprintf("/api/v2/items/%d", id))
			wantStatus(t, "GET", fmt.Sprintf("/api/v2/items/%d (bulk-deleted)", id), status, body, http.StatusNotFound)
		}
	})
}

func containsInt(list []int, want int) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// TestItemSearch covers UC-D4: CQL title and label filters return exactly the
// matching items; malformed queries are rejected.
func TestItemSearch(t *testing.T) {
	admin := e2eEnv.admin
	wsID := createWorkspace(t, admin, wscfgWSKey(t))

	labelName := wscfgName(t, "d4-label")
	status, body := admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/labels", wsID), map[string]any{"name": labelName})
	wantStatus(t, "POST", "/api/v2/workspaces/{id}/labels", status, body, http.StatusCreated)
	labelID := v2ID(t, body)

	mkItem := func(title string, labels []int) int {
		payload := map[string]any{"workspace_id": wsID, "title": title}
		if len(labels) > 0 {
			payload["label_ids"] = labels
		}
		status, body := admin.Post(t, "/api/v2/items", payload)
		wantStatus(t, "POST", "/api/v2/items (d4 "+title+")", status, body, http.StatusCreated)
		return v2ID(t, body)
	}

	alpha := mkItem("E2E D4 Alpha", []int{labelID})
	beta := mkItem("E2E D4 Beta", nil)

	backlog := func(ql string) (int, []byte) {
		q := url.Values{}
		q.Set("workspace_id", fmt.Sprint(wsID))
		if ql != "" {
			q.Set("ql", ql)
		}
		return admin.Get(t, "/api/v2/items/backlog?"+q.Encode())
	}

	t.Run("TitleFilter", func(t *testing.T) {
		status, body := backlog("")
		wantStatus(t, "GET", "/api/v2/items/backlog (unfiltered)", status, body, http.StatusOK)
		all := backlogItemIDs(t, body)
		if len(all) != 2 || !containsInt(all, alpha) || !containsInt(all, beta) {
			t.Fatalf("workspace should hold exactly [%d %d], got %v", alpha, beta, all)
		}

		status, body = backlog(`title ~ "alpha"`)
		wantStatus(t, `GET`, `/api/v2/items/backlog (ql title~"alpha")`, status, body, http.StatusOK)
		ids := backlogItemIDs(t, body)
		if len(ids) != 1 || ids[0] != alpha {
			t.Fatalf("title filter matched %v, want [%d]", ids, alpha)
		}
	})

	t.Run("LabelFilter", func(t *testing.T) {
		status, body := backlog(fmt.Sprintf(`label = "%s"`, labelName))
		wantStatus(t, "GET", "/api/v2/items/backlog (ql label=)", status, body, http.StatusOK)
		ids := backlogItemIDs(t, body)
		if len(ids) != 1 || ids[0] != alpha {
			t.Fatalf("label filter matched %v, want [%d]", ids, alpha)
		}
	})

	t.Run("MalformedQuery", func(t *testing.T) {
		status, body := backlog(`title ~`)
		wantStatus(t, "GET", "/api/v2/items/backlog (malformed ql)", status, body, http.StatusBadRequest)
	})
}
