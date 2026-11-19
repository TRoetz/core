package e2e

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeJira is a minimal Atlassian Cloud instance serving exactly what the
// Windshift Jira import client calls for project "E2E": two Task issues with
// mapped statuses and two users. It records every request (method, path, auth
// header, response status) so tests can assert on the exact call sequence and
// catch any endpoint the fake does not model — those land in unexpected.
type fakeJira struct {
	*httptest.Server

	mu         sync.Mutex
	requests   []fakeJiraCall
	unexpected []string             // paths that matched no known route (404'd)
	issues     map[string]map[string]any // issue key -> full JiraIssue payload
}

type fakeJiraCall struct {
	Method string
	Path   string // path without query
	Query  string // raw query, may be empty
	Auth   string
	Status int
}

// startFakeJira boots the fake on an ephemeral localhost port. The returned
// URL is what tests pass as instance_url to /api/admin/jira-import/connect.
func startFakeJira(t *testing.T) *fakeJira {
	t.Helper()
	f := &fakeJira{}

	alice := fakeJiraUser("acc-alice-1", "Alice Importer")
	bob := fakeJiraUser("acc-bob-2", "Bob Reporter")

	mux := http.NewServeMux()

	// Routing probes made by jira.NewClient for deployment_type "cloud".
	// Both 404 on purpose: no cloud ID (legacy-token site routing) and no
	// Assets workspace, so the client uses {site}/rest/api/3 with assets off.
	mux.HandleFunc("GET /_edge/tenant_info", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONError(w, http.StatusNotFound, "no tenant info"))
	})
	mux.HandleFunc("GET /rest/servicedeskapi/assets/workspace", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONError(w, http.StatusNotFound, "assets not available"))
	})

	// Connect probe (TestConnection): serverInfo is required, myself best-effort.
	mux.HandleFunc("GET /rest/api/3/serverInfo", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONOK(w, map[string]any{
			"baseUrl":        f.URL,
			"version":        "100.1.0",
			"deploymentType": "Cloud",
			"serverTitle":    "Fake Jira E2E",
		}))
	})
	mux.HandleFunc("GET /rest/api/3/myself", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONOK(w, map[string]any{
			"accountId":    "acc-admin-9",
			"accountType":  "atlassian",
			"emailAddress": "admin@fake-jira.test",
			"displayName":  "Jira Admin",
			"active":       true,
		}))
	})

	// Import execution: rank-ordered key listing (single page, no nextPageToken).
	mux.HandleFunc("POST /rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONOK(w, map[string]any{
			"issues": []map[string]any{{"key": "E2E-1"}, {"key": "E2E-2"}},
			"total":  2,
		}))
	})

	// Import execution: full issue payloads for the requested keys.
	mux.HandleFunc("POST /rest/api/3/issue/bulkfetch", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			IssueIdsOrKeys []string `json:"issueIdsOrKeys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			f.record(r, jiraJSONError(w, http.StatusBadRequest, "bad bulkfetch body"))
			return
		}
		issues := make([]map[string]any, 0, len(req.IssueIdsOrKeys))
		for _, key := range req.IssueIdsOrKeys {
			issue, ok := f.issues[key]
			if !ok {
				f.record(r, jiraJSONError(w, http.StatusNotFound, "unknown issue "+key))
				return
			}
			issues = append(issues, issue)
		}
		f.record(r, jiraJSONOK(w, map[string]any{"issues": issues}))
	})

	// Import execution: project metadata and issue-type/status membership.
	mux.HandleFunc("GET /rest/api/3/project/E2E", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONOK(w, map[string]any{
			"id":             "10000",
			"key":            "E2E",
			"name":           "E2E Project",
			"description":    "E2E import project",
			"projectTypeKey": "software",
		}))
	})
	mux.HandleFunc("GET /rest/api/3/project/E2E/statuses", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONOK(w, []map[string]any{{
			"id":      "10002",
			"name":    "Task",
			"subtask": false,
			"statuses": []map[string]any{
				{"id": "3", "name": "Done"},
				{"id": "2", "name": "In Progress"},
			},
		}}))
	})

	// Import execution: project-scoped custom fields (none) and the configured
	// workflow graph for issue type 10002.
	mux.HandleFunc("GET /rest/api/3/field/search", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONOK(w, map[string]any{
			"values": []map[string]any{}, "startAt": 0, "maxResults": 50, "total": 0, "isLast": true,
		}))
	})
	mux.HandleFunc("POST /rest/api/3/workflows", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONOK(w, map[string]any{
			"statuses": []map[string]any{
				{"id": "2", "statusReference": "st-progress"},
				{"id": "3", "statusReference": "st-done"},
			},
			"workflows": []map[string]any{{
				"id":          "wf-e2e-1",
				"name":        "E2E Workflow",
				"description": "",
				"statuses":    []map[string]any{{"statusReference": "st-progress"}, {"statusReference": "st-done"}},
				"transitions": []map[string]any{
					{"id": "t0", "name": "Initial", "type": "INITIAL", "toStatusReference": "st-progress", "links": []map[string]any{}},
					{"id": "t1", "name": "Complete", "type": "DIRECTED", "toStatusReference": "st-done", "links": []map[string]any{{"fromStatusReference": "st-progress"}}},
				},
			}},
		}))
	})

	// Import execution: screen scheme (none configured) and agile boards
	// (none), so the screen/iteration stages are clean no-ops.
	mux.HandleFunc("GET /rest/api/3/issuetypescreenscheme/project", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONOK(w, map[string]any{
			"values": []map[string]any{}, "startAt": 0, "maxResults": 100, "total": 0, "isLast": true,
		}))
	})
	mux.HandleFunc("GET /rest/agile/1.0/board", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONOK(w, map[string]any{
			"values": []map[string]any{}, "startAt": 0, "maxResults": 50, "total": 0, "isLast": true,
		}))
	})

	// Import execution: saved filters (none).
	mux.HandleFunc("GET /rest/api/3/filter/search", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, jiraJSONOK(w, map[string]any{
			"values": []map[string]any{}, "startAt": 0, "maxResults": 50, "total": 0, "isLast": true,
		}))
	})

	// User email lookup (Cloud omits emails from issue payloads).
	mux.HandleFunc("GET /rest/api/3/user/email", func(w http.ResponseWriter, r *http.Request) {
		accountID := r.URL.Query().Get("accountId")
		if email, ok := fakeJiraUserEmails[accountID]; ok {
			f.record(r, jiraJSONOK(w, map[string]any{"accountId": accountID, "email": email}))
			return
		}
		f.record(r, jiraJSONError(w, http.StatusNotFound, "unknown account "+accountID))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = ""
		}
		f.mu.Lock()
		f.unexpected = append(f.unexpected, r.Method+" "+path)
		f.mu.Unlock()
		jiraJSONError(w, http.StatusNotFound, "no route for "+r.Method+" "+path)
	})

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)

	f.issues = map[string]map[string]any{
		"E2E-1": fakeJiraIssuePayload(f.URL, "10001", "E2E-1", "First imported task", "3", "Done", alice, bob),
		"E2E-2": fakeJiraIssuePayload(f.URL, "10002", "E2E-2", "Second imported task", "2", "In Progress", bob, alice),
	}
	return f
}

// fakeJiraUserEmails maps the account IDs used by the E2E issues to emails so
// ensureUsers creates real, assertable Windshift users.
var fakeJiraUserEmails = map[string]string{
	"acc-alice-1": "alice@example.com",
	"acc-bob-2":   "bob@example.com",
}

func fakeJiraUser(accountID, displayName string) map[string]any {
	return map[string]any{
		"accountId":   accountID,
		"accountType": "atlassian",
		"displayName": displayName,
		"active":      true,
	}
}

// fakeJiraIssuePayload builds one full JiraIssue document. watchCount 0 makes
// completeIssueWatchers skip the per-issue watchers endpoint entirely; absent
// comments/worklogs/attachments fields keep those subresource stages no-ops.
func fakeJiraIssuePayload(baseURL, id, key, summary string, statusID, statusName string, assignee, reporter map[string]any) map[string]any {
	return map[string]any{
		"id":   id,
		"key":  key,
		"self": baseURL + "/rest/api/3/issue/" + id,
		"fields": map[string]any{
			"summary": summary,
			"description": map[string]any{
				"type":    "doc",
				"version": 1,
				"content": []map[string]any{{
					"type": "paragraph",
					"content": []map[string]any{{
						"type": "text",
						"text": summary + " — imported by the E2E suite.",
					}},
				}},
			},
			"issuetype": map[string]any{"id": "10002", "name": "Task"},
			"project":   map[string]any{"key": "E2E", "name": "E2E Project"},
			"status":    map[string]any{"id": statusID, "name": statusName},
			"assignee":  assignee,
			"reporter":  reporter,
			"creator":   reporter,
			"created":   "2026-11-01T10:00:00.000+0000",
			"updated":   "2026-11-02T11:30:00.000+0000",
			"labels":    []string{"e2e-import"},
			"watches":   map[string]any{"self": "", "watchCount": 0, "isWatching": false},
		},
	}
}

// record stores the call and returns the response status for logging.
func (f *fakeJira) record(r *http.Request, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, fakeJiraCall{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Auth:   r.Header.Get("Authorization"),
		Status: status,
	})
}

// calls returns a copy of the recorded request sequence.
func (f *fakeJira) calls() []fakeJiraCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeJiraCall, len(f.requests))
	copy(out, f.requests)
	return out
}

// assertNoUnexpected fails if any request hit a route the fake does not model.
func (f *fakeJira) assertNoUnexpected(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.unexpected) > 0 {
		t.Fatalf("fake Jira received unmodeled requests: %v", f.unexpected)
	}
}

// expectBasicAuth asserts some call to a path ending in suffix carried Basic
// auth for email/token (the Cloud credential contract).
func (f *fakeJira) expectBasicAuth(t *testing.T, suffix, email, token string) {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token))
	for _, c := range f.calls() {
		if strings.HasSuffix(c.Path, suffix) && c.Auth == want {
			return
		}
	}
	t.Fatalf("no request to %s with Basic auth for %q (calls: %v)", suffix, email, summarizeCalls(f.calls()))
}

func summarizeCalls(calls []fakeJiraCall) string {
	parts := make([]string, 0, len(calls))
	for _, c := range calls {
		parts = append(parts, fmt.Sprintf("%s %s -> %d", c.Method, c.Path, c.Status))
	}
	return strings.Join(parts, ", ")
}

func jiraJSONOK(w http.ResponseWriter, v any) int {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError) //nolint:staticcheck // test server only
		return http.StatusInternalServerError
	}
	return http.StatusOK
}

func jiraJSONError(w http.ResponseWriter, status int, message string) int {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":        status,
		"errorMessages": []string{message},
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError) //nolint:staticcheck // test server only
	}
	return status
}
