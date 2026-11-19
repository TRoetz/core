package e2e

// End-to-end use-case coverage for integrations and AI (UC-P1..P4): Jira import
// against a fake Atlassian Cloud instance, SCM provider credential encryption +
// outbound webhook delivery to a local receiver, LLM connection lifecycle with a
// fake OpenAI-compatible endpoint. Runs against the real server on a fresh
// SQLite database via the shared harness.

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestJiraImportEndToEnd (UC-P1) connects to a fake Atlassian Cloud instance,
// imports project E2E into a new workspace, and validates the imported data in
// the database. A second import of the same project must be rejected as a
// conflict referencing the original job.
func TestJiraImportEndToEnd(t *testing.T) {
	admin := e2eEnv.admin
	fake := startFakeJira(t)
	const (
		jiraEmail = "admin@fake-jira.test"
		jiraToken = "e2e-jira-token-123"
	)

	var connectionID string
	t.Run("Connect", func(t *testing.T) {
		status, body := admin.Post(t, "/api/admin/jira-import/connect", map[string]any{
			"instance_url": fake.URL,
			"email":        jiraEmail,
			"api_token":    jiraToken,
		})
		wantStatus(t, "POST", "/api/admin/jira-import/connect", status, body, http.StatusOK)
		var res struct {
			ConnectionID string `json:"connection_id"`
		}
		if err := json.Unmarshal(body, &res); err != nil || res.ConnectionID == "" {
			t.Fatalf("connect: no connection_id in response: %s", snippet(body))
		}
		connectionID = res.ConnectionID

		fake.expectBasicAuth(t, "/rest/api/3/serverInfo", jiraEmail, jiraToken)
	})

	entityMappings := func() map[string]any {
		t.Helper()
		doneID := cfInt(dbQueryOne(t, "SELECT id FROM statuses WHERE name = 'Done'")["id"])
		inProgressID := cfInt(dbQueryOne(t, "SELECT id FROM statuses WHERE name = 'In Progress'")["id"])
		itemTypeID := cfAnyItemType(t)

		m := map[string]any{
			"issueTypes": []map[string]any{{
				"jiraIds":     []string{"10002"},
				"jiraName":    "Task",
				"windshiftId": &itemTypeID,
			}},
			"statuses": []map[string]any{
				{"jiraIds": []string{"3"}, "jiraName": "Done", "categoryKey": "done", "categoryName": "Done", "color": "#22c55e", "windshiftId": &doneID},
				{"jiraIds": []string{"2"}, "jiraName": "In Progress", "categoryKey": "inprogress", "categoryName": "In Progress", "color": "#3b82f6", "windshiftId": &inProgressID},
			},
		}
		return m
	}

	startImportPayload := func(workspaceMapping map[string]any) map[string]any {
		t.Helper()
		mappings := entityMappings()
		mappings["workspaces"] = []map[string]any{workspaceMapping}
		return map[string]any{
			"connection_id": connectionID,
			"project_keys":  []string{"E2E"},
			"mappings":      mappings,
		}
	}

	var firstJobID string
	t.Run("StartAndComplete", func(t *testing.T) {
		status, body := admin.Post(t, "/api/admin/jira-import/start", startImportPayload(map[string]any{
			"jiraKey": "E2E", "jiraName": "E2E Project", "issueCount": 2,
			"createNew": true, "newWorkspaceName": "E2E Import", "newWorkspaceKey": "e2eimp",
			// The new workspace key differs from the Jira project key, so the
			// import requires explicit alias acknowledgment.
			"keyAliasAcknowledged": true,
		}))
		wantStatus(t, "POST", "/api/admin/jira-import/start", status, body, http.StatusOK)
		var res struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(body, &res); err != nil || res.JobID == "" {
			t.Fatalf("start: no job_id in response: %s", snippet(body))
		}
		firstJobID = res.JobID

		var jobBody []byte
		spWaitFor(t, 90*time.Second, "jira import to reach a terminal state", func() bool {
			st, b := admin.Get(t, "/api/admin/jira-import/jobs/"+firstJobID)
			if st != http.StatusOK {
				return false
			}
			jobBody = b
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				return false
			}
			switch cfStr(m["status"]) {
			case "completed", "completed_with_errors", "failed":
				return true
			}
			return false
		})

		assertJSONField(t, jobBody, "status", "completed")
		assertJSONField(t, jobBody, "progress.imported_issues", 2)
		assertJSONField(t, jobBody, "progress.failed_issues", 0)
	})

	t.Run("DataLanded", func(t *testing.T) {
		wsID := cfInt(dbQueryOne(t, "SELECT id FROM workspaces WHERE key = 'E2EIMP'")["id"])

		if n := dbCount(t, "items", "workspace_id = ?", wsID); n != 2 {
			t.Fatalf("expected 2 imported items in workspace %d, got %d", wsID, n)
		}
		assertItemStatus := func(title, wantName string) {
			t.Helper()
			row := dbQueryOne(t, `SELECT s.name AS status_name FROM items i JOIN statuses s ON s.id = i.status_id WHERE i.workspace_id = ? AND i.title = ?`, wsID, title)
			if got := cfStr(row["status_name"]); got != wantName {
				t.Fatalf("item %q: status = %q, want %q", title, got, wantName)
			}
		}
		assertItemStatus("First imported task", "Done")
		assertItemStatus("Second imported task", "In Progress")

		for _, email := range []string{"alice@example.com", "bob@example.com"} {
			if dbCount(t, "users", "email = ?", email) != 1 {
				t.Fatalf("expected user %s to be created by the import", email)
			}
		}

		fake.assertNoUnexpected(t)
	})

	t.Run("ReimportConflict", func(t *testing.T) {
		wsID := cfInt(dbQueryOne(t, "SELECT id FROM workspaces WHERE key = 'E2EIMP'")["id"])
		status, body := admin.Post(t, "/api/admin/jira-import/start", startImportPayload(map[string]any{
			"jiraKey": "E2E", "jiraName": "E2E Project", "issueCount": 2,
			// Reuse the workspace created by the first import so validation
			// passes and the conflict check (same connection + project key)
			// is what rejects the request.
			"windshiftId": &wsID,
		}))
		wantStatus(t, "POST", "/api/admin/jira-import/start (reimport)", status, body, http.StatusConflict)
		assertJSONField(t, body, "code", "JIRA_IMPORT_CONFLICT")

		var res struct {
			Details struct {
				ConflictingImports []struct {
					JobID string `json:"job_id"`
				} `json:"conflicting_imports"`
			} `json:"details"`
		}
		if err := json.Unmarshal(body, &res); err != nil || len(res.Details.ConflictingImports) == 0 {
			t.Fatalf("reimport: no conflicting_imports in error details: %s", snippet(body))
		}
		if got := res.Details.ConflictingImports[0].JobID; got != firstJobID {
			t.Fatalf("conflict references job %q, want original import job %q", got, firstJobID)
		}
	})
}

// TestSCMProviderCredentials (UC-P2a) validates SCM provider request validation
// and proves personal access tokens are stored encrypted at rest — never in
// plaintext.
func TestSCMProviderCredentials(t *testing.T) {
	admin := e2eEnv.admin
	const pat = "glpat-e2e-secret-token"

	t.Run("Validation", func(t *testing.T) {
		status, body := admin.Post(t, "/api/admin/scm-providers", map[string]any{
			"name": "Missing Slug", "provider_type": "gitlab", "auth_method": "pat",
		})
		wantStatus(t, "POST", "/api/admin/scm-providers (missing fields)", status, body, http.StatusBadRequest)

		status, body = admin.Post(t, "/api/admin/scm-providers", map[string]any{
			"slug": "e2e-badtype", "name": "Bad Type", "provider_type": "bitbucket", "auth_method": "pat",
		})
		wantStatus(t, "POST", "/api/admin/scm-providers (invalid provider type)", status, body, http.StatusBadRequest)
	})

	t.Run("CreateEncryptsToken", func(t *testing.T) {
		payload := map[string]any{
			"slug": "e2e-gitlab", "name": "E2E GitLab", "provider_type": "gitlab", "auth_method": "pat",
			"base_url": "https://gitlab.example.com", "personal_access_token": pat, "enabled": true,
		}
		status, body := admin.Post(t, "/api/admin/scm-providers", payload)
		wantStatus(t, "POST", "/api/admin/scm-providers (create)", status, body, http.StatusCreated)
		assertJSONField(t, body, "has_pat", true)

		row := dbQueryOne(t, "SELECT personal_access_token_encrypted FROM scm_providers WHERE slug = 'e2e-gitlab'")
		enc := cfStr(row["personal_access_token_encrypted"])
		if enc == "" {
			t.Fatal("personal access token was not stored (encrypted column empty)")
		}
		if strings.Contains(enc, pat) {
			t.Fatalf("plaintext PAT leaked into storage: %s", enc)
		}

		status, body = admin.Post(t, "/api/admin/scm-providers", payload)
		wantStatus(t, "POST", "/api/admin/scm-providers (duplicate slug)", status, body, http.StatusConflict)
	})
}

// TestWebhookDelivery (UC-P2b) delivers a manual webhook trigger to a local
// receiver and validates the payload; non-webhook channels must be rejected.
func TestWebhookDelivery(t *testing.T) {
	admin := e2eEnv.admin

	type received struct {
		path string
		body []byte
	}
	var mu sync.Mutex
	var got []received
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, received{r.URL.Path, b})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	wsID := createWorkspace(t, admin, wscfgWSKey(t))
	status, body := admin.Post(t, "/api/v2/items", map[string]any{
		"workspace_id": wsID, "title": "E2E webhook item",
	})
	wantStatus(t, "POST", "/api/v2/items (webhook source)", status, body, http.StatusCreated)
	itemID := v2ID(t, body)

	configJSON, err := json.Marshal(map[string]any{
		"webhook_url":        receiver.URL + "/hook",
		"webhook_scope_type": "all",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e2eEnv.db.ExecWrite(
		`INSERT INTO channels (name, type, direction, status, config) VALUES (?, 'webhook', 'outbound', 'enabled', ?)`,
		"E2E webhook", string(configJSON),
	)
	if err != nil {
		t.Fatalf("insert webhook channel: %v", err)
	}
	channelID, _ := res.LastInsertId()

	status, body = admin.Post(t, fmt.Sprintf("/api/webhooks/%d/trigger", channelID), map[string]any{
		"item_id": itemID,
	})
	wantStatus(t, "POST", "/api/webhooks/{id}/trigger", status, body, http.StatusOK)
	assertJSONField(t, body, "success", true)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected exactly one webhook delivery to the receiver, got %d", len(got))
	}
	var payload struct {
		Event     string         `json:"event"`
		WebhookID int            `json:"webhook_id"`
		Item      map[string]any `json:"item"`
	}
	if err := json.Unmarshal(got[0].body, &payload); err != nil {
		t.Fatalf("bad webhook payload JSON: %v\n%s", err, snippet(got[0].body))
	}
	if payload.Event != "manual" {
		t.Fatalf("webhook event = %q, want manual", payload.Event)
	}
	if payload.WebhookID != int(channelID) {
		t.Fatalf("webhook_id = %d, want channel id %d", payload.WebhookID, channelID)
	}
	if got := cfStr(payload.Item["title"]); got != "E2E webhook item" {
		t.Fatalf("payload item title = %q, want the triggered item's title", got)
	}

	t.Run("NonWebhookChannelRejected", func(t *testing.T) {
		res, err := e2eEnv.db.ExecWrite(
			`INSERT INTO channels (name, type, direction, status, config) VALUES (?, 'smtp', 'outbound', 'enabled', '{}')`,
			"E2E smtp channel",
		)
		if err != nil {
			t.Fatalf("insert smtp channel: %v", err)
		}
		smtpID, _ := res.LastInsertId()

		status, body := admin.Post(t, fmt.Sprintf("/api/webhooks/%d/trigger", smtpID), map[string]any{
			"item_id": itemID,
		})
		wantStatus(t, "POST", "/api/webhooks/{id}/trigger (non-webhook channel)", status, body, http.StatusBadRequest)
	})
}

// TestLLMConnectionLifecycle (UC-P3/P4) creates an LLM connection against a fake
// OpenAI-compatible endpoint, proves the API key is stored encrypted and that a
// test call reaches the endpoint with the decrypted key; unreachable endpoints
// must surface as 502 CONNECTION_TEST_FAILED.
func TestLLMConnectionLifecycle(t *testing.T) {
	admin := e2eEnv.admin

	var mu sync.Mutex
	var authHeaders []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost {
			authHeaders = append(authHeaders, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-e2e",
				"object":  "chat.completion",
				"created": time.Now().Unix(),
				"model":   "e2e-model",
				"choices": []map[string]any{{
					"index":         0,
					"message":       map[string]any{"role": "assistant", "content": "pong"},
					"finish_reason": "stop",
				}},
				"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			})
			return
		}
		t.Errorf("fake LLM: unmodeled request %s %s", r.Method, r.URL.Path)
		http.Error(w, "no route", http.StatusNotFound)
	}))
	t.Cleanup(fake.Close)

	t.Run("Validation", func(t *testing.T) {
		status, body := admin.Post(t, "/api/admin/llm-connections", map[string]any{
			"provider_type": "local", "model": "e2e-model",
		})
		wantStatus(t, "POST", "/api/admin/llm-connections (missing name)", status, body, http.StatusBadRequest)

		status, body = admin.Post(t, "/api/admin/llm-connections", map[string]any{
			"name": "Bad Provider", "provider_type": "nosuchprovider", "model": "e2e-model",
		})
		wantStatus(t, "POST", "/api/admin/llm-connections (unknown provider)", status, body, http.StatusBadRequest)

		status, body = admin.Post(t, "/api/admin/llm-connections", map[string]any{
			"name": "No Base URL", "provider_type": "local", "model": "e2e-model",
		})
		wantStatus(t, "POST", "/api/admin/llm-connections (local without base_url)", status, body, http.StatusBadRequest)

		status, body = admin.Post(t, "/api/admin/llm-connections", map[string]any{
			"name": "Bad URL", "provider_type": "local", "model": "e2e-model", "base_url": "ftp://example.com",
		})
		wantStatus(t, "POST", "/api/admin/llm-connections (invalid base_url scheme)", status, body, http.StatusBadRequest)
	})

	t.Run("CreateEncryptsKeyAndTestSucceeds", func(t *testing.T) {
		const apiKey = "sk-e2e-test-key"
		status, body := admin.Post(t, "/api/admin/llm-connections", map[string]any{
			"name": "E2E Fake LLM", "provider_type": "local", "model": "e2e-model",
			"api_key": apiKey, "base_url": fake.URL, "is_enabled": true,
		})
		wantStatus(t, "POST", "/api/admin/llm-connections (create)", status, body, http.StatusCreated)
		assertJSONField(t, body, "has_api_key", true)

		var res struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(body, &res); err != nil || res.ID == 0 {
			t.Fatalf("create: no id in response: %s", snippet(body))
		}

		row := dbQueryOne(t, "SELECT api_key_encrypted FROM llm_connections WHERE id = ?", res.ID)
		enc := cfStr(row["api_key_encrypted"])
		if enc == "" {
			t.Fatal("API key was not stored (encrypted column empty)")
		}
		if strings.Contains(enc, apiKey) {
			t.Fatalf("plaintext API key leaked into storage: %s", enc)
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/admin/llm-connections/%d/test", res.ID), nil)
		wantStatus(t, "POST", "/api/admin/llm-connections/{id}/test", status, body, http.StatusOK)
		assertJSONField(t, body, "status", "ok")

		mu.Lock()
		defer mu.Unlock()
		if len(authHeaders) != 1 {
			t.Fatalf("expected exactly one chat completion call to the fake endpoint, got %d", len(authHeaders))
		}
		if authHeaders[0] != "Bearer "+apiKey {
			t.Fatalf("fake endpoint saw Authorization %q, want Bearer <decrypted key>", authHeaders[0])
		}
	})

	t.Run("UnreachableEndpointFails", func(t *testing.T) {
		// Bind an ephemeral port and close it: the address is valid but nothing
		// listens there, so the connection test must fail with a refused dial.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()

		status, body := admin.Post(t, "/api/admin/llm-connections", map[string]any{
			"name": "E2E Dead LLM", "provider_type": "local", "model": "e2e-model",
			"base_url": fmt.Sprintf("http://127.0.0.1:%d", port), "is_enabled": true,
		})
		wantStatus(t, "POST", "/api/admin/llm-connections (dead endpoint)", status, body, http.StatusCreated)

		var res struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(body, &res); err != nil || res.ID == 0 {
			t.Fatalf("create: no id in response: %s", snippet(body))
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/admin/llm-connections/%d/test", res.ID), nil)
		wantStatus(t, "POST", "/api/admin/llm-connections/{id}/test (unreachable)", status, body, http.StatusBadGateway)
		assertJSONField(t, body, "code", "CONNECTION_TEST_FAILED")
	})
}
