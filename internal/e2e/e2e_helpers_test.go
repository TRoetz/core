package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// snippet returns a short single-purpose excerpt of a response body for error messages.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// mustJSON decodes a JSON body, failing the test on invalid JSON.
func mustJSON(t *testing.T, body []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, snippet(body))
	}
	return v
}

// cfInt converts a DB row or JSON value to int; unknown types yield 0.
func cfInt(v any) int {
	switch n := v.(type) {
	case nil:
		return 0
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

// cfStr converts a DB row or JSON value to string; nil yields "".
func cfStr(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", s)
	}
}

// cfBool converts a DB row or JSON value to bool; unknown types yield false.
func cfBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case int:
		return b != 0
	case int64:
		return b != 0
	case float64:
		return b != 0
	case string:
		x, _ := strconv.ParseBool(strings.TrimSpace(b))
		return x
	default:
		return false
	}
}

// cfExtractID pulls the created record's id out of a v1 create response. The
// envelope is not uniform across handlers (bare record, {"data": {...}}, or a
// list), so each shape is tried in turn.
func cfExtractID(t *testing.T, body []byte) int {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("cfExtractID: invalid JSON: %s", snippet(body))
	}
	idFrom := func(m map[string]any) (int, bool) {
		if id, ok := m["id"]; ok {
			return cfInt(id), true
		}
		return 0, false
	}
	switch n := v.(type) {
	case map[string]any:
		if id, ok := idFrom(n); ok {
			return id
		}
		if d, ok := n["data"].(map[string]any); ok {
			if id, ok := idFrom(d); ok {
				return id
			}
		}
	case []any:
		for _, e := range n {
			if m, ok := e.(map[string]any); ok {
				if id, ok := idFrom(m); ok {
					return id
				}
			}
		}
	}
	t.Fatalf("cfExtractID: no id in response: %s", snippet(body))
	return 0
}

// cfListEntries parses a v1 list response into its object entries. Handles both
// bare arrays and {"data": [...]} envelopes.
func cfListEntries(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("cfListEntries: invalid JSON: %s", snippet(body))
	}
	var arr []any
	switch n := v.(type) {
	case []any:
		arr = n
	case map[string]any:
		if a, ok := n["data"].([]any); ok {
			arr = a
		} else if d, ok := n["data"].(map[string]any); ok {
			if a, ok := d["items"].([]any); ok {
				arr = a
			}
		}
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("cfListEntries: non-object entry in list: %s", snippet(body))
		}
		out = append(out, m)
	}
	return out
}

// cfAnyItemType returns the id of any seeded item type (default first).
func cfAnyItemType(t *testing.T) int {
	t.Helper()
	row := dbQueryOne(t, "SELECT id FROM item_types ORDER BY is_default DESC, id LIMIT 1")
	id := cfInt(row["id"])
	if id <= 0 {
		t.Fatal("no item types found in database")
	}
	return id
}

// createWorkspace creates a workspace through the v2 API and returns its id.
func createWorkspace(t *testing.T, admin *Client, key string) int {
	t.Helper()
	status, body := admin.Post(t, "/api/v2/workspaces", workspacePayload(key, "E2E "+key, "e2e test workspace"))
	if status != http.StatusCreated {
		t.Fatalf("create workspace %q: got %d: %s", key, status, snippet(body))
	}
	return v2ID(t, body)
}

// newClient returns an unauthenticated client with a fresh cookie jar.
func newClient(baseURL string) *Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(err)
	}
	return &Client{c: &http.Client{Jar: jar}, baseURL: baseURL}
}

// spWaitFor polls cond every 250ms until it reports true or d elapses.
func spWaitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	if cond() {
		return
	}
	t.Fatalf("timed out after %s waiting for %s", d, what)
}

// spExtractMagicToken pulls the token out of a portal magic-link email. The
// verify URL carries it in the fragment (#token=...). The sender encodes both
// MIME parts as quoted-printable, where a literal '=' becomes =3D and long
// lines are soft-wrapped with '=\r\n'; decoding the text/plain part first
// keeps the fragment intact for matching.
func spExtractMagicToken(t *testing.T, raw []byte) string {
	t.Helper()
	var s string
	if part := qpTextPart(raw); part != nil {
		decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(part)))
		if err != nil {
			t.Fatalf("decode quoted-printable text part: %v", err)
		}
		s = string(decoded)
	} else {
		// Not a multipart message with a text/plain part; fall back to the raw payload.
		s = strings.ReplaceAll(string(raw), "=\r\n", "")
	}
	re := regexp.MustCompile(`#token=([A-Za-z0-9+/=_-]+)`)
	m := re.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("no magic-link token found in email: %s", snippet(raw))
	}
	return m[1]
}

// qpTextPart returns the body of the first text/plain MIME part (up to the next
// boundary), or nil if raw is not a multipart message containing one.
func qpTextPart(raw []byte) []byte {
	ct := bytes.Index(raw, []byte("Content-Type: text/plain"))
	if ct < 0 {
		return nil
	}
	headerEnd := bytes.Index(raw[ct:], []byte("\r\n\r\n"))
	if headerEnd < 0 {
		return nil
	}
	bodyStart := ct + headerEnd + len("\r\n\r\n")
	rest := raw[bodyStart:]
	boundary := bytes.Index(rest, []byte("\r\n--"))
	if boundary < 0 {
		boundary = len(rest)
	}
	return rest[:boundary]
}

// smtpSink is a minimal plaintext SMTP server that captures every DATA payload.
type smtpSink struct {
	ln   net.Listener
	next chan []byte
}

// startFakeSMTP boots the sink on an ephemeral localhost port and returns it.
func startFakeSMTP(t *testing.T) *smtpSink {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start fake smtp: %v", err)
	}
	sink := &smtpSink{ln: ln, next: make(chan []byte, 16)}
	go sink.serve()
	t.Cleanup(func() { ln.Close() })
	return sink
}

func (s *smtpSink) port() int {
	return s.ln.Addr().(*net.TCPAddr).Port
}

// waitMessage blocks until the next captured message arrives or d elapses.
func (s *smtpSink) waitMessage(t *testing.T, d time.Duration) []byte {
	t.Helper()
	select {
	case m := <-s.next:
		return m
	case <-time.After(d):
		t.Fatalf("timed out after %s waiting for SMTP message", d)
		return nil
	}
}

func (s *smtpSink) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

// handle speaks just enough SMTP for net/smtp in plaintext mode: greeting,
// EHLO/HELO, MAIL FROM, RCPT TO, DATA (until the terminating dot), QUIT.
func (s *smtpSink) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	fmt.Fprintln(conn, "220 e2e-smtp ready")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			fmt.Fprintln(conn, "250 e2e-smtp")
		case strings.HasPrefix(cmd, "MAIL FROM"):
			fmt.Fprintln(conn, "250 OK")
		case strings.HasPrefix(cmd, "RCPT TO"):
			fmt.Fprintln(conn, "250 OK")
		case cmd == "DATA":
			fmt.Fprintln(conn, "354 Go ahead")
			var msg bytes.Buffer
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				msg.WriteString(l)
				if strings.TrimRight(l, "\r\n") == "." {
					break
				}
			}
			fmt.Fprintln(conn, "250 OK queued")
			select {
			case s.next <- msg.Bytes():
			default: // buffer full; drop rather than wedge the sender
			}
		case cmd == "QUIT":
			fmt.Fprintln(conn, "221 bye")
			return
		default:
			fmt.Fprintln(conn, "250 OK")
		}
	}
}
