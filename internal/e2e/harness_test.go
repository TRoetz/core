package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"windshift/internal/config"
	"windshift/internal/database"
	"windshift/internal/server"
)

var (
	e2eEnv struct {
		baseURL string
		db      database.Database
		admin   *Client
	}

	// Admin identity created by TestMain's setup completion.
	adminEmail    = "e2e-admin@example.test"
	adminUsername = "eeadmin"
	adminPassword = "E2eAdmin123!"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ws-e2e")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	// Single-worker e2e runs must not trip shared limiter budgets, and the
	// /api/test/* hooks (SLA clock, SCM ref injection) are only mounted with
	// WINDSHIFT_E2E_TEST_HOOKS. Production never sets either.
	os.Setenv("WINDSHIFT_E2E_DISABLE_RATE_LIMITS", "1")
	os.Setenv("WINDSHIFT_E2E_TEST_HOOKS", "1")

	cfg := config.Config{
		Port:               "0",
		DisableCSRF:        true,
		DisableIPRateLimit: true,
		DB:                 config.DBConfig{SQLitePath: dir + "/ws.db", RequestTimeout: 15 * time.Second},
		Auth:               config.AuthConfig{SessionSecret: "e2e-session-secret"},
		AttachmentPath:     dir + "/data",
	}

	srv, err := server.New(cfg)
	if err != nil {
		fmt.Printf("Failed to create server: %v\n", err)
		os.Exit(1)
	}

	if err := srv.Start(); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		os.Exit(1)
	}
	defer srv.Shutdown(context.Background())

	// Poll /api/setup/status until 200
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d", srv.Port()) + "/api/setup/status")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			panic("Timed out waiting for /api/setup/status to return 200")
		}
		time.Sleep(1 * time.Second)
	}

	// Complete setup
	setupBody, _ := json.Marshal(map[string]any{
		"admin_user": map[string]any{
			"email":      adminEmail,
			"username":   adminUsername,
			"first_name": "E2E",
			"last_name":  "Admin",
			"language":   "en",
			"password":   adminPassword,
		},
		"module_settings": map[string]any{},
	})
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d", srv.Port())+"/api/setup/complete", bytes.NewBuffer(setupBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		panic(fmt.Sprintf("Failed to complete setup: %v", err))
	}
	resp.Body.Close()

	// Login as admin
	e2eEnv.baseURL = fmt.Sprintf("http://localhost:%d", srv.Port())
	adminClient := bootstrapLogin(adminEmail, adminPassword)
	e2eEnv.admin = adminClient
	e2eEnv.db = srv.DB()

	os.Exit(m.Run())
}

type Client struct {
	c       *http.Client
	baseURL string
}

func (c *Client) Do(t *testing.T, method, path string, body any) (int, []byte) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	req, err := http.NewRequest(method, c.baseURL+path, &buf)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.c.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, b
}

func (c *Client) Get(t *testing.T, path string) (int, []byte) {
	return c.Do(t, "GET", path, nil)
}

func (c *Client) Post(t *testing.T, path string, body any) (int, []byte) {
	return c.Do(t, "POST", path, body)
}

func (c *Client) Put(t *testing.T, path string, body any) (int, []byte) {
	return c.Do(t, "PUT", path, body)
}

func (c *Client) Patch(t *testing.T, path string, body any) (int, []byte) {
	return c.Do(t, "PATCH", path, body)
}

func (c *Client) Delete(t *testing.T, path string) (int, []byte) {
	return c.Do(t, "DELETE", path, nil)
}

// DoRaw sends a request with an explicit content type and raw body.
func (c *Client) DoRaw(t *testing.T, method, path string, contentType string, body []byte) (int, []byte) {
	req, err := http.NewRequest(method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.c.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, b
}

// performLogin runs the password-login + /api/auth/me verification round trip.
// On failure it returns a nil client with the status and response body so
// callers can decide how to report it (panic in TestMain, Fatalf in tests).
func performLogin(identifier, password string) (*Client, int, []byte) {
	body, _ := json.Marshal(map[string]any{
		"email_or_username": identifier,
		"password":          password,
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, 0, []byte(err.Error())
	}
	c := &http.Client{Jar: jar}

	resp, err := c.Post(e2eEnv.baseURL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, b
	}

	me, err := c.Get(e2eEnv.baseURL + "/api/auth/me")
	if err != nil || me.StatusCode != http.StatusOK {
		st := 0
		var b2 []byte
		if me != nil {
			st = me.StatusCode
			b2, _ = io.ReadAll(me.Body)
		} else {
			b2 = []byte(err.Error())
		}
		return nil, st, b2
	}
	me.Body.Close()

	return &Client{c: c, baseURL: e2eEnv.baseURL}, 0, nil
}

// bootstrapLogin performs password login outside of a test (TestMain); panics on failure.
func bootstrapLogin(identifier, password string) *Client {
	c, st, b := performLogin(identifier, password)
	if c == nil {
		panic(fmt.Sprintf("login failed (%d): %s", st, b))
	}
	return c
}

// login logs in as identifier and verifies /api/auth/me reports expectUsername.
func login(t *testing.T, identifier, password, expectUsername string) *Client {
	t.Helper()
	c, st, b := performLogin(identifier, password)
	if c == nil {
		t.Fatalf("login as %s failed (%d): %s", identifier, st, b)
	}

	me, err := c.c.Get(c.baseURL + "/api/auth/me")
	if err != nil || me.StatusCode != http.StatusOK {
		b2, _ := io.ReadAll(me.Body)
		t.Fatalf("login verification failed: %s", b2)
	}
	defer me.Body.Close()

	var meBody struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.NewDecoder(me.Body).Decode(&meBody); err != nil {
		t.Fatalf("decode /api/auth/me: %v", err)
	}
	if expectUsername != "" && meBody.User.Username != expectUsername {
		t.Fatalf("login as %s: got username %q, want %q", identifier, meBody.User.Username, expectUsername)
	}

	return c
}

func createUser(t *testing.T, email, username, password string) int {
	t.Helper()
	st, b := e2eEnv.admin.Post(t, "/api/users", map[string]any{
		"email":      email,
		"username":   username,
		"first_name": "E2E",
		"last_name":  "Test",
		"password":   password,
		"is_active":  true, // omitted → user is created inactive and cannot log in
	})
	if st != http.StatusCreated {
		t.Fatalf("create user failed: %s", snippet(b))
	}
	var res struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(b, &res); err != nil || res.ID <= 0 {
		t.Fatalf("no id in create-user response: %s", snippet(b))
	}
	return res.ID
}

// grantGlobalPermission grants a global permission key to a user through the
// admin API so the server-side permission cache is invalidated. The key must
// be a seeded global-scope permission.
func grantGlobalPermission(t *testing.T, userID int, key string) {
	t.Helper()
	row := dbQueryOne(t, "SELECT id FROM permissions WHERE permission_key = ? AND scope = 'global'", key)
	permID := cfInt(row["id"])
	if permID <= 0 {
		t.Fatalf("grantGlobalPermission: no global-scope permission with key %q", key)
	}
	status, body := e2eEnv.admin.Post(t, "/api/permissions/global/grant", map[string]any{
		"user_id":       userID,
		"permission_id": permID,
	})
	if status != http.StatusCreated {
		t.Fatalf("grant global permission %q to user %d: got %d: %s", key, userID, status, snippet(body))
	}
}

func dbQueryOne(t *testing.T, query string, args ...any) map[string]any {
	t.Helper()
	rows, err := e2eEnv.db.Query(query, args...)
	if err != nil {
		t.Fatalf("dbQueryOne failed: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil
	}

	cols, _ := rows.Columns()
	values := make([]any, len(cols))
	iface := make([]any, len(cols))
	for i := range values {
		iface[i] = &values[i]
	}
	if err := rows.Scan(iface...); err != nil {
		t.Fatalf("dbQueryOne scan failed: %v", err)
	}

	res := make(map[string]any, len(cols))
	for i, col := range cols {
		if b, ok := values[i].([]byte); ok {
			res[col] = string(b)
		} else {
			res[col] = values[i]
		}
	}
	return res
}

func dbCount(t *testing.T, table, where string, args ...any) int {
	t.Helper()
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if where != "" {
		query += " WHERE " + where
	}
	var n int
	if err := e2eEnv.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("dbCount failed: %v", err)
	}
	return n
}

// assertJSONField checks that the value at dot-path (map keys, or numeric
// segments for arrays) equals want. Numbers compare across JSON float64 and Go int.
func assertJSONField(t *testing.T, body []byte, path string, want any) {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	parts := strings.Split(path, ".")

	curr := any(data)
	for _, p := range parts[:len(parts)-1] {
		switch node := curr.(type) {
		case map[string]any:
			next, ok := node[p]
			if !ok {
				t.Fatalf("field %s not found in %s", path, snippet(body))
			}
			curr = next
		case []any:
			idx, err := strconv.Atoi(p)
			if err != nil || idx < 0 || idx >= len(node) {
				t.Fatalf("field %s: bad array index %q in %s", path, p, snippet(body))
			}
			curr = node[idx]
		default:
			t.Fatalf("field %s: cannot descend into %T at %q", path, curr, p)
		}
	}

	last := parts[len(parts)-1]
	var got any
	switch parent := curr.(type) {
	case map[string]any:
		exists := false
		got, exists = parent[last]
		if !exists {
			t.Fatalf("field %s not found in %s", path, snippet(body))
		}
	case []any:
		idx, err := strconv.Atoi(last)
		if err != nil || idx < 0 || idx >= len(parent) {
			t.Fatalf("field %s: bad array index %q in %s", path, last, snippet(body))
		}
		got = parent[idx]
	default:
		t.Fatalf("field %s: parent is not an object or array (%T)", path, curr)
	}
	if !jsonValueEqual(got, want) {
		t.Fatalf("field %s: got %v (%T), want %v (%T)", path, got, got, want, want)
	}
}

func jsonValueEqual(got, want any) bool {
	gf, gotNum := toFloat64(got)
	wf, wantNum := toFloat64(want)
	if gotNum && wantNum {
		return gf == wf
	}
	return got == want
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func TestHarnessReady(t *testing.T) {
	res, b := e2eEnv.admin.Get(t, "/api/auth/me")
	if res != http.StatusOK {
		t.Fatalf("Harness failed: got %d", res)
	}
	assertJSONField(t, b, "user.username", adminUsername)
}
func wantStatus(t *testing.T, method, path string, status int, body []byte, wants ...int) {
	t.Helper()
	ok := false
	for _, w := range wants {
		if status == w {
			ok = true
			break
		}
	}
	if !ok {
		t.Errorf("%s %s: got %d, want one of %v", method, path, status, wants)
		return
	}
	if status == http.StatusNoContent && len(body) > 0 {
		t.Errorf("%s %s: expected empty body for 204, got %q", method, path, string(body))
	}
}
