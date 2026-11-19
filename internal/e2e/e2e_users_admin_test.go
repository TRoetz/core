package e2e

// UC-B1..B6: user & admin surface — lifecycle, permissions/roles, API tokens,
// audit log, input rejection, SCIM provisioning.
//
// OBSERVED comments pin behavior that differs from the plan document's
// assumptions; each was verified against handler code before pinning.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// bearerJSON performs a JSON request with an explicit Authorization: Bearer
// header over the shared client's transport. No session cookie is involved —
// this is for crw_ API tokens and SCIM tokens, which never authenticate via
// cookies.
func bearerJSON(t *testing.T, cl *Client, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("bearerJSON: marshal %s %s: %v", method, path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, cl.baseURL+path, rdr)
	if err != nil {
		t.Fatalf("bearerJSON: new request %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cl.c.Do(req)
	if err != nil {
		t.Fatalf("bearerJSON: %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("bearerJSON: read body %s %s: %v", method, path, err)
	}
	return resp.StatusCode, data
}

// UC-B1 User lifecycle: create via API → identity verified (DB row + real
// login), regional settings and avatar updates return 200, deactivation kills
// the existing session and rejects login, reactivation restores login.
func TestUsersAdminLifecycle(t *testing.T) {
	email := "e2e-b1-user@example.test"
	username := "e2eb1user"
	password := "B1UserPass123!"

	status, body := e2eEnv.admin.Post(t, "/api/users", map[string]any{
		"email": email, "username": username,
		"first_name": "E2E", "last_name": "Lifecycle", "password": password,
		"is_active": true, // omitted → user is created inactive and cannot log in
	})
	wantStatus(t, "POST", "/api/users", status, body, http.StatusCreated)

	var created struct {
		ID       int    `json:"id"`
		Email    string `json:"email"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ID == 0 {
		t.Fatalf("POST /api/users: no id in response: %s", snippet(body))
	}

	// Persistence: the row exists with the submitted identity and is active.
	row := dbQueryOne(t, "SELECT email, username, is_active FROM users WHERE id = ?", created.ID)
	if fmt.Sprint(row["email"]) != email || fmt.Sprint(row["username"]) != username || fmt.Sprint(row["is_active"]) != "1" {
		t.Fatalf("created user not persisted correctly: %v", row)
	}

	// OBSERVED: the /api surface has no session-authenticated user list
	// endpoint, so "appears with correct identity" is verified through a real
	// login + /auth/me plus the DB row above.
	ucl := login(t, username, password, username)
	status, body = ucl.Get(t, "/api/auth/me")
	wantStatus(t, "GET", "/api/auth/me (new user)", status, body, http.StatusOK)
	assertJSONField(t, body, "user.email", email)

	// Regional settings update by the admin on another user.
	status, body = e2eEnv.admin.Put(t, fmt.Sprintf("/api/users/%d/regional-settings", created.ID), map[string]any{
		"timezone": "Europe/Berlin", "language": "de",
	})
	wantStatus(t, "PUT", "/api/users/{id}/regional-settings", status, body, http.StatusOK)
	assertJSONField(t, body, "timezone", "Europe/Berlin")
	assertJSONField(t, body, "language", "de")

	// Avatar update (handler invalidates the target's sessions afterwards).
	status, body = e2eEnv.admin.Put(t, fmt.Sprintf("/api/users/%d/avatar", created.ID), map[string]any{
		"avatar_url": "https://example.test/e2e-b1-avatar.png",
	})
	wantStatus(t, "PUT", "/api/users/{id}/avatar", status, body, http.StatusOK)
	assertJSONField(t, body, "avatar_url", "https://example.test/e2e-b1-avatar.png")

	// OBSERVED: UpdateAvatar's invalidateUserSessions only clears security
	// caches; the session row survives, so the user stays logged in.
	status, body = ucl.Get(t, "/api/auth/me")
	wantStatus(t, "GET", "/api/auth/me (post-avatar session)", status, body, http.StatusOK)

	// Deactivate: existing sessions die and login is rejected.
	status, body = e2eEnv.admin.Post(t, fmt.Sprintf("/api/users/%d/deactivate", created.ID), nil)
	wantStatus(t, "POST", "/api/users/{id}/deactivate", status, body, http.StatusOK)
	assertJSONField(t, body, "message", "User deactivated successfully")

	row = dbQueryOne(t, "SELECT is_active FROM users WHERE id = ?", created.ID)
	if fmt.Sprint(row["is_active"]) != "0" {
		t.Fatalf("deactivated user still active in DB: %v", row["is_active"])
	}

	status, body = ucl.Get(t, "/api/auth/me")
	wantStatus(t, "GET", "/api/auth/me (deactivated session)", status, body, http.StatusUnauthorized)

	fresh := newClient(e2eEnv.baseURL)
	status, body = fresh.Post(t, "/api/auth/login", map[string]any{
		"email_or_username": username, "password": password,
	})
	wantStatus(t, "POST", "/api/auth/login (deactivated)", status, body, http.StatusUnauthorized)

	// Reactivate: login works again.
	status, body = e2eEnv.admin.Post(t, fmt.Sprintf("/api/users/%d/activate", created.ID), nil)
	wantStatus(t, "POST", "/api/users/{id}/activate", status, body, http.StatusOK)
	assertJSONField(t, body, "message", "User activated successfully")
	login(t, username, password, username)
}

// UC-B2 Permissions & roles: workspace role create/assign/list; global
// permission grant/revoke through the API gates real endpoints (sms.view read
// allowed, sms.manage write denied, revoke closes the read too).
func TestUsersAdminPermissionsRoles(t *testing.T) {
	// Workspace keys must be 2-10 purely alphanumeric characters.
	wsID := createWorkspace(t, e2eEnv.admin, "e2eb2ws")

	status, body := e2eEnv.admin.Post(t, "/api/workspace-roles", map[string]any{
		"name": "E2E B2 Role", "description": "role for UC-B2",
	})
	wantStatus(t, "POST", "/api/workspace-roles", status, body, http.StatusCreated)

	var role struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &role); err != nil || role.ID == 0 {
		t.Fatalf("POST /api/workspace-roles: no id in response: %s", snippet(body))
	}

	userID := createUser(t, "e2e-b2-user@example.test", "e2eb2user", "B2UserPass123!")

	status, body = e2eEnv.admin.Post(t, "/api/workspace-roles/assign", map[string]any{
		"user_id": userID, "workspace_id": wsID, "role_id": role.ID,
	})
	wantStatus(t, "POST", "/api/workspace-roles/assign", status, body, http.StatusCreated)

	// The assigned role is listed for the user in that workspace.
	status, body = e2eEnv.admin.Get(t, fmt.Sprintf("/api/users/%d/workspaces/%d/roles", userID, wsID))
	wantStatus(t, "GET", "/api/users/{id}/workspaces/{ws}/roles", status, body, http.StatusOK)

	var roles []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &roles); err != nil {
		t.Fatalf("GET user workspace roles: bad JSON: %v\n%s", err, snippet(body))
	}
	found := false
	for _, r := range roles {
		if r.ID == role.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("assigned role %d not in list: %s", role.ID, snippet(body))
	}

	// Global permission grant through the API (not the DB helper): resolve
	// sms.view's id from the catalog first.
	status, body = e2eEnv.admin.Get(t, "/api/permissions")
	wantStatus(t, "GET", "/api/permissions", status, body, http.StatusOK)

	var perms []struct {
		ID            int    `json:"id"`
		PermissionKey string `json:"permission_key"`
	}
	if err := json.Unmarshal(body, &perms); err != nil {
		t.Fatalf("GET /api/permissions: bad JSON: %v\n%s", err, snippet(body))
	}
	smsViewID := 0
	for _, p := range perms {
		if p.PermissionKey == "sms.view" {
			smsViewID = p.ID
		}
	}
	if smsViewID == 0 {
		t.Fatalf("permission sms.view not found in catalog: %s", snippet(body))
	}

	status, body = e2eEnv.admin.Post(t, "/api/permissions/global/grant", map[string]any{
		"user_id": userID, "permission_id": smsViewID,
	})
	wantStatus(t, "POST", "/api/permissions/global/grant", status, body, http.StatusCreated)

	ucl := login(t, "e2eb2user", "B2UserPass123!", "e2eb2user")

	// Read allowed with sms.view; write denied without sms.manage.
	status, body = ucl.Get(t, "/api/sms/stakeholders")
	wantStatus(t, "GET", "/api/sms/stakeholders (sms.view)", status, body, http.StatusOK)
	status, body = ucl.Post(t, "/api/sms/stakeholders", map[string]any{"name": "must be rejected"})
	wantStatus(t, "POST", "/api/sms/stakeholders (no sms.manage)", status, body, http.StatusForbidden)

	// Revoke: the read is denied too.
	status, body = e2eEnv.admin.Delete(t, fmt.Sprintf("/api/users/%d/permissions/global/%d", userID, smsViewID))
	wantStatus(t, "DELETE", "/api/users/{id}/permissions/global/{pid}", status, body, http.StatusOK)
	assertJSONField(t, body, "message", "Permission revoked successfully")

	status, body = ucl.Get(t, "/api/sms/stakeholders")
	wantStatus(t, "GET", "/api/sms/stakeholders (after revoke)", status, body, http.StatusForbidden)
}

// UC-B3 API tokens: create with an explicit scope → bearer works on the REST
// v1 surface; delete → 401.
//
// OBSERVED: the plan assumed crw_ tokens authenticate /api/auth/me; they do
// not — middleware/auth.go rejects API tokens outside /rest/api/* (that
// surface is session-cookie only). The token's effective surface is
// /rest/api/v1/*, which this test pins instead.
func TestUsersAdminAPITokens(t *testing.T) {
	status, body := e2eEnv.admin.Post(t, "/api/api-tokens", map[string]any{
		"name": "e2e-b3-token", "permissions": []string{"users:read"},
	})
	// OBSERVED: the handler responds 200 (respondJSONOK), not the plan's 201.
	wantStatus(t, "POST", "/api/api-tokens", status, body, http.StatusOK)

	var created struct {
		Token    string `json:"token"`
		APIToken struct {
			ID int `json:"id"`
		} `json:"api_token"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.Token == "" || created.APIToken.ID == 0 {
		t.Fatalf("POST /api/api-tokens: no token/id in response: %s", snippet(body))
	}

	// Bearer works on the v1 surface with the granted scope.
	status, body = bearerJSON(t, e2eEnv.admin, http.MethodGet, "/rest/api/v1/users/me", created.Token, nil)
	wantStatus(t, "GET", "/rest/api/v1/users/me (bearer)", status, body, http.StatusOK)
	assertJSONField(t, body, "username", adminUsername)

	// OBSERVED: the same token is rejected on the session API surface.
	status, body = bearerJSON(t, e2eEnv.admin, http.MethodGet, "/api/auth/me", created.Token, nil)
	wantStatus(t, "GET", "/api/auth/me (bearer)", status, body, http.StatusUnauthorized)

	// Revoke → the bearer stops working.
	status, body = e2eEnv.admin.Delete(t, fmt.Sprintf("/api/api-tokens/%d", created.APIToken.ID))
	wantStatus(t, "DELETE", "/api/api-tokens/{id}", status, body, http.StatusNoContent)

	status, body = bearerJSON(t, e2eEnv.admin, http.MethodGet, "/rest/api/v1/users/me", created.Token, nil)
	wantStatus(t, "GET", "/rest/api/v1/users/me (revoked)", status, body, http.StatusUnauthorized)
}

// UC-B4 Audit log: a user creation is recorded and queryable by action type.
func TestUsersAdminAuditLog(t *testing.T) {
	username := "e2eb4audit"
	status, body := e2eEnv.admin.Post(t, "/api/users", map[string]any{
		"email": "e2e-b4-audit@example.test", "username": username,
		"first_name": "E2E", "last_name": "Audit",
	})
	wantStatus(t, "POST", "/api/users (audit target)", status, body, http.StatusCreated)

	status, body = e2eEnv.admin.Get(t, "/api/admin/audit-logs?action_type=user.create&per_page=100")
	wantStatus(t, "GET", "/api/admin/audit-logs", status, body, http.StatusOK)

	var res struct {
		Total   int `json:"total"`
		Entries []struct {
			ActionType   string `json:"action_type"`
			ResourceName string `json:"resource_name"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("GET /api/admin/audit-logs: bad JSON: %v\n%s", err, snippet(body))
	}
	found := false
	for _, e := range res.Entries {
		if e.ActionType == "user.create" && e.ResourceName == username {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no user.create audit entry for %q among %d entries (total=%d)", username, len(res.Entries), res.Total)
	}
}

// UC-B5 Input rejection on user create: duplicate username → 409, malformed
// email → 400.
func TestUsersAdminCreateNegatives(t *testing.T) {
	status, body := e2eEnv.admin.Post(t, "/api/users", map[string]any{
		"email": "e2e-b5-dup@example.test", "username": adminUsername, // collides with the setup admin
		"first_name": "Dup", "last_name": "Test",
	})
	wantStatus(t, "POST", "/api/users (duplicate username)", status, body, http.StatusConflict)

	status, body = e2eEnv.admin.Post(t, "/api/users", map[string]any{
		"email": "not-an-email", "username": "e2eb5badmail",
		"first_name": "Bad", "last_name": "Mail",
	})
	wantStatus(t, "POST", "/api/users (malformed email)", status, body, http.StatusBadRequest)
}

// UC-B6 SCIM provisioning: token mint; user provision/deactivate/delete over
// /scim/v2 with bearer auth; unauthenticated calls rejected.
func TestUsersAdminSCIM(t *testing.T) {
	// Unauthenticated SCIM call → 401.
	anon := newClient(e2eEnv.baseURL)
	status, body := anon.Get(t, "/scim/v2/Users")
	wantStatus(t, "GET", "/scim/v2/Users (unauthenticated)", status, body, http.StatusUnauthorized)

	status, body = e2eEnv.admin.Post(t, "/api/admin/scim-tokens", map[string]any{"name": "e2e-b6-scim"})
	wantStatus(t, "POST", "/api/admin/scim-tokens", status, body, http.StatusCreated)

	var tok struct {
		Token     string `json:"token"`
		SCIMToken struct {
			ID int `json:"id"`
		} `json:"scim_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.Token == "" || tok.SCIMToken.ID == 0 {
		t.Fatalf("POST /api/admin/scim-tokens: no token/id in response: %s", snippet(body))
	}

	status, body = bearerJSON(t, e2eEnv.admin, http.MethodGet, "/scim/v2/ServiceProviderConfig", tok.Token, nil)
	wantStatus(t, "GET", "/scim/v2/ServiceProviderConfig (bearer)", status, body, http.StatusOK)

	// Provision a user.
	scimUser := map[string]any{
		"schemas":  []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"userName": "e2eb6scim",
		"name":     map[string]any{"givenName": "E2E", "familyName": "SCIM"},
		"emails":   []map[string]any{{"value": "e2e-b6-scim@example.test", "type": "work", "primary": true}},
		"active":   true,
	}
	status, body = bearerJSON(t, e2eEnv.admin, http.MethodPost, "/scim/v2/Users", tok.Token, scimUser)
	wantStatus(t, "POST", "/scim/v2/Users (provision)", status, body, http.StatusCreated)

	var su struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &su); err != nil || su.ID == "" {
		t.Fatalf("SCIM provision: no id in response: %s", snippet(body))
	}

	row := dbQueryOne(t, "SELECT email, is_active FROM users WHERE username = ?", "e2eb6scim")
	if fmt.Sprint(row["email"]) != "e2e-b6-scim@example.test" || fmt.Sprint(row["is_active"]) != "1" {
		t.Fatalf("SCIM-provisioned user not persisted correctly: %v", row)
	}

	// Deactivate via PATCH (RFC 7644 PatchOp).
	patch := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{{
			"op": "replace", "path": "active", "value": false,
		}},
	}
	status, body = bearerJSON(t, e2eEnv.admin, http.MethodPatch, "/scim/v2/Users/"+su.ID, tok.Token, patch)
	wantStatus(t, "PATCH", "/scim/v2/Users/{id} (active=false)", status, body, http.StatusOK)

	row = dbQueryOne(t, "SELECT is_active FROM users WHERE username = ?", "e2eb6scim")
	if fmt.Sprint(row["is_active"]) != "0" {
		t.Fatalf("SCIM PATCH active=false not persisted: %v", row["is_active"])
	}

	// Delete → tombstoned: subsequent GET 404s and re-provisioning the same
	// email conflicts (RFC 7644 §3.6, enforced in handlers/scim.go).
	status, body = bearerJSON(t, e2eEnv.admin, http.MethodDelete, "/scim/v2/Users/"+su.ID, tok.Token, nil)
	wantStatus(t, "DELETE", "/scim/v2/Users/{id}", status, body, http.StatusNoContent)

	status, body = bearerJSON(t, e2eEnv.admin, http.MethodGet, "/scim/v2/Users/"+su.ID, tok.Token, nil)
	wantStatus(t, "GET", "/scim/v2/Users/{id} (after delete)", status, body, http.StatusNotFound)

	status, body = bearerJSON(t, e2eEnv.admin, http.MethodPost, "/scim/v2/Users", tok.Token, scimUser)
	wantStatus(t, "POST", "/scim/v2/Users (tombstoned email)", status, body, http.StatusConflict)
}
