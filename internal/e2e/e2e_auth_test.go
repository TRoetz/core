package e2e

import (
	"net/http"
	"testing"
)

// UC-A1 Setup & first login: the setup wizard is completed in TestMain; a fresh
// admin login succeeds and /auth/me reports the admin identity.
func TestAuthSetupAndFirstLogin(t *testing.T) {
	t.Run("SetupCompleted", func(t *testing.T) {
		st, b := e2eEnv.admin.Get(t, "/api/setup/status")
		wantStatus(t, "GET", "/api/setup/status", st, b, http.StatusOK)
		assertJSONField(t, b, "setup_completed", true)
	})

	t.Run("FirstLogin", func(t *testing.T) {
		c := login(t, adminUsername, adminPassword, adminUsername)
		st, b := c.Get(t, "/api/auth/me")
		wantStatus(t, "GET", "/auth/me", st, b, http.StatusOK)
		assertJSONField(t, b, "user.username", adminUsername)
	})
}

// UC-A2 Login negatives: wrong password 401, unknown user 401 (no enumeration),
// missing password field 400. Ends with a successful login so the shared
// per-IP failed-attempt counter is cleared for later test files.
func TestAuthLoginNegatives(t *testing.T) {
	t.Run("WrongPassword", func(t *testing.T) {
		cl := newClient(e2eEnv.baseURL)
		st, b := cl.Post(t, "/api/auth/login", map[string]any{
			"email_or_username": adminUsername,
			"password":          "DefinitelyWrong1!",
		})
		wantStatus(t, "POST", "/auth/login wrong password", st, b, http.StatusUnauthorized)
	})

	t.Run("UnknownUser", func(t *testing.T) {
		cl := newClient(e2eEnv.baseURL)
		st, b := cl.Post(t, "/api/auth/login", map[string]any{
			"email_or_username": "no-such-user-e2e",
			"password":          "Whatever123!",
		})
		wantStatus(t, "POST", "/auth/login unknown user", st, b, http.StatusUnauthorized)
	})

	t.Run("MissingPasswordField", func(t *testing.T) {
		cl := newClient(e2eEnv.baseURL)
		st, b := cl.Post(t, "/api/auth/login", map[string]any{
			"email_or_username": adminUsername,
		})
		wantStatus(t, "POST", "/auth/login missing password field", st, b, http.StatusBadRequest)
	})

	login(t, adminUsername, adminPassword, adminUsername) // clears failed-attempt counter
}

// UC-A3 Session lifecycle: logout invalidates the session; change-password
// requires the current password and makes the old one fail.
func TestAuthSessionLifecycle(t *testing.T) {
	const (
		email    = "e2e-auth-user@example.test"
		username = "e2eauthuser"
		password = "AuthUser123!"
		newPass  = "AuthUser456!"
	)
	id := createUser(t, email, username, password)

	hashBefore := dbQueryOne(t, "SELECT password_hash FROM users WHERE id = ?", id)["password_hash"]
	if hashBefore == nil {
		t.Fatal("users.password_hash missing for created user")
	}

	c := login(t, username, password, username)

	t.Run("LogoutInvalidatesSession", func(t *testing.T) {
		st, b := c.Post(t, "/api/auth/logout", nil)
		wantStatus(t, "POST", "/auth/logout", st, b, http.StatusOK)
		assertJSONField(t, b, "success", true)

		st, b = c.Get(t, "/api/auth/me")
		wantStatus(t, "GET", "/auth/me after logout", st, b, http.StatusUnauthorized)
	})

	t.Run("ChangePassword", func(t *testing.T) {
		c2 := login(t, username, password, username)
		st, b := c2.Post(t, "/api/auth/change-password", map[string]any{
			"current_password": password,
			"new_password":     newPass,
		})
		wantStatus(t, "POST", "/auth/change-password", st, b, http.StatusOK)
		assertJSONField(t, b, "success", true)

		// New password works; old one is rejected.
		login(t, username, newPass, username) // success also clears the failure counter
		cl := newClient(e2eEnv.baseURL)
		st, b = cl.Post(t, "/api/auth/login", map[string]any{
			"email_or_username": username,
			"password":          password,
		})
		wantStatus(t, "POST", "/auth/login old password after change", st, b, http.StatusUnauthorized)
		login(t, username, newPass, username) // clear counter for later files

		hashAfter := dbQueryOne(t, "SELECT password_hash FROM users WHERE id = ?", id)["password_hash"]
		if hashAfter == nil || hashAfter == hashBefore {
			t.Fatalf("users.password_hash not updated after change (before=%v after=%v)", hashBefore, hashAfter)
		}
	})
}

// UC-A4 Unauthenticated access: protected endpoints reject cookie-less clients.
func TestAuthUnauthenticatedAccess(t *testing.T) {
	cl := newClient(e2eEnv.baseURL) // no session cookie
	for _, path := range []string{"/api/sms/stakeholders", "/api/p3m/hierarchy", "/api/plans"} {
		st, b := cl.Get(t, path)
		wantStatus(t, "GET", path+" unauthenticated", st, b, http.StatusUnauthorized)
	}
}
