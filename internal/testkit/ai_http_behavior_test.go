package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// HC-8 / HC-9: the AI's http_* tools through the real app, a fake model
// and an httptest API on 127.0.0.1 (no network). The token lives in
// http-client.private.env.json and must never reach the model, the
// audit log, the chat store, history or errors.log.

const aiHTTPToken = "sekrit-token-123"

func aiHTTPFiles(base string) map[string]string {
	files := textFiles()
	files["http-client.env.json"] = `{"dev": {"host": "` + base + `"}}`
	files["http-client.private.env.json"] = `{"dev": {"token": "` + aiHTTPToken + `"}}`
	files["api/users.http"] = "### Get one user\n# @name getUser\nGET {{host}}/users/42\nAuthorization: Bearer {{token}}\n\n" +
		"###\n# @name createUser\nPOST {{host}}/users\nContent-Type: application/json\n\n{\"mail\": \"ada@example.com\"}\n\n" +
		"###\n# @name whoami\nGET {{host}}/auth\nAuthorization: Bearer {{token}}\n\n" +
		"###\n# @name broken\nGET {{host}}/x/{{nope}}\n\n" +
		"###\n# @name big\nGET {{host}}/big\n"
	return files
}

// clickConfirm clicks a button of the "AI wants to" card by its label.
func clickConfirm(d *Driver, label string) {
	d.T.Helper()
	for y, line := range strings.Split(screen(d), "\n") {
		if !strings.Contains(line, "Deny") {
			continue
		}
		if j := strings.Index(line, " "+label); j >= 0 {
			d.Click(ansi.StringWidth(line[:j])+2, y)
			return
		}
	}
	d.T.Fatalf("no %q button:\n%s", label, screen(d))
}

func waitConfirm(d *Driver) {
	d.T.Helper()
	if !d.WaitForText("AI wants to", 8*time.Second) {
		d.T.Fatalf("no confirm card:\n%s", screen(d))
	}
}

func httpWaitDone(d *Driver) {
	d.T.Helper()
	if !d.WaitForText("fake-done", 10*time.Second) {
		d.T.Fatalf("no final answer:\n%s", screen(d))
	}
}

// noToken fails when the token is in any text the plan lists (§5.3).
func noToken(t *testing.T, d *Driver, results []string) {
	t.Helper()
	for i, r := range results {
		if strings.Contains(r, aiHTTPToken) {
			t.Errorf("tool result %d leaks the token:\n%s", i, r)
		}
	}
	for _, l := range d.Model().AIAuditLines() {
		if strings.Contains(l, aiHTTPToken) {
			t.Errorf("audit line leaks the token: %s", l)
		}
	}
	if strings.Contains(screen(d), aiHTTPToken) {
		t.Errorf("screen shows the token:\n%s", screen(d))
	}
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode")
	_ = filepath.Walk(cfg, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || strings.HasSuffix(p, "credentials.json") {
			return nil
		}
		if b, _ := os.ReadFile(p); strings.Contains(string(b), aiHTTPToken) {
			t.Errorf("%s holds the token", p)
		}
		return nil
	})
}

// GET to loopback runs at once; the answer is in the result and on the
// page ("by AI"); the token is used but never shown.
func TestAIHTTPSendGetLoopback(t *testing.T) {
	api := newHTTPAPI(t, false)
	s, srv := fakeToolModel(t,
		toolCall{"http_list_requests", map[string]any{"path": "api/users.http"}},
		toolCall{"http_send", map[string]any{"path": "api/users.http", "name": "whoami", "env": "dev"}},
		toolCall{"http_send", map[string]any{"path": "api/users.http", "name": "getUser", "env": "dev"}},
		toolCall{"http_history", map[string]any{}})
	d := aiDriver(t, srv.URL, aiHTTPFiles(api.srv.URL), false)
	askAI(d, "test getUser")
	httpWaitDone(d)
	if strings.Contains(screen(d), "AI wants to") {
		t.Fatalf("GET to localhost asked:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 4 {
		t.Fatalf("results %q", r)
	}
	if !strings.Contains(r[0], "api/users.http:3  #1  getUser  GET {{host}}/users/42") {
		t.Errorf("list: %s", r[0])
	}
	if !strings.Contains(r[1], "auth=Bearer [REDACTED]") {
		t.Errorf("the server got the token and the model must see it hidden: %s", r[1])
	}
	if !strings.Contains(r[2], "HTTP/1.1 200 OK") || !strings.Contains(r[2], `"name": "Ada Lovelace"`) || !strings.Contains(r[2], "set-cookie: session=[REDACTED]") {
		t.Errorf("send result: %s", r[2])
	}
	if !strings.Contains(r[3], "by ai") {
		t.Errorf("history: %s", r[3])
	}
	if !d.WaitFor(func() bool { return strings.Contains(httpPageText(d), "200 OK") }, 3*time.Second) || !strings.Contains(screen(d), "by AI") {
		t.Errorf("page not shown by AI:\n%s", screen(d))
	}
	noToken(t, d, r)
}

// POST to loopback asks with "Always allow http:127.0.0.1:<port>/write";
// Deny tells the model.
func TestAIHTTPSendPostAsksDeny(t *testing.T) {
	api := newHTTPAPI(t, false)
	s, srv := fakeToolModel(t, toolCall{"http_send", map[string]any{"path": "api/users.http", "name": "createUser", "env": "dev"}})
	d := aiDriver(t, srv.URL, aiHTTPFiles(api.srv.URL), false)
	askAI(d, "create")
	waitConfirm(d)
	host := strings.TrimPrefix(api.srv.URL, "http://")
	if !strings.Contains(screen(d), "http_send") || !strings.Contains(screen(d), "Always allow http:"+host+"/write") {
		t.Fatalf("card:\n%s", screen(d))
	}
	clickConfirm(d, "Deny")
	httpWaitDone(d)
	if r := s.toolResults(); len(r) != 1 || !strings.Contains(r[0], "declined") || api.count("/users") != 0 {
		t.Fatalf("results %q hits %d", r, api.count("/users"))
	}
}

// The plan's end-to-end flow (§4.5): POST → 500 (Always allow), fix the
// request through the review card, POST again silently → 201.
func TestAIHTTPFix500Flow(t *testing.T) {
	api := newHTTPAPI(t, false)
	s, srv := fakeToolModel(t,
		toolCall{"http_send", map[string]any{"path": "api/users.http", "name": "createUser", "env": "dev"}},
		toolCall{"http_save_request", map[string]any{"path": "api/users.http", "name": "createUser", "replace": true,
			"method": "POST", "url": "{{host}}/users", "headers": map[string]any{"Content-Type": "application/json"},
			"body": `{"email": "ada@example.com"}`}},
		toolCall{"http_send", map[string]any{"path": "api/users.http", "name": "createUser", "env": "dev"}})
	d := aiDriver(t, srv.URL, aiHTTPFiles(api.srv.URL), false)
	askAI(d, "test createUser and tell me why it returns 500")
	waitConfirm(d)
	clickConfirm(d, "Always allow")
	acceptAllReview(d)
	httpWaitDone(d)
	r := s.toolResults()
	if len(r) != 3 || !strings.Contains(r[0], "500 Internal Server Error") || !strings.Contains(r[0], "violates not-null") || !strings.Contains(r[2], "201 Created") {
		t.Fatalf("results %q", r)
	}
	file := diskFile(d, "api/users.http")
	if !strings.Contains(file, "### createUser\nPOST {{host}}/users\nContent-Type: application/json\n\n{\"email\": \"ada@example.com\"}\n") ||
		!strings.Contains(file, "# @name getUser") || !strings.Contains(file, "# @name broken") {
		t.Fatalf("file after save:\n%s", file)
	}
	cfg, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "config.json"))
	host := strings.TrimPrefix(api.srv.URL, "http://")
	if !strings.Contains(string(cfg), "http:"+host+"/write") {
		t.Fatalf("Always allow not saved: %s", cfg)
	}
	var audit string
	for _, l := range d.Model().AIAuditLines() {
		audit += l + "\n"
	}
	if !strings.Contains(audit, "http_send") || !strings.Contains(audit, "500 Internal Server Error") {
		t.Errorf("audit (%d lines):\n%s\nresults %q", len(d.Model().AIAuditLines()), audit, r)
	}
	noToken(t, d, r)
}

// A private address always asks and offers no "Always allow".
func TestAIHTTPPrivateAlwaysAsks(t *testing.T) {
	s, srv := fakeToolModel(t, toolCall{"http_send", map[string]any{"url": "http://10.0.0.1/admin"}})
	d := aiDriver(t, srv.URL, nil, false, "ide", "settings", "fileWrite")
	askAI(d, "probe")
	waitConfirm(d)
	if strings.Contains(screen(d), "Always allow") {
		t.Fatalf("a private address must not offer Always allow:\n%s", screen(d))
	}
	clickConfirm(d, "Deny")
	httpWaitDone(d)
	if r := s.toolResults(); len(r) != 1 || !strings.Contains(r[0], "declined") {
		t.Fatalf("results %q", r)
	}
}

// Unknown {{names}} are refused before any dialog; a long body pages.
func TestAIHTTPValidateAndPaging(t *testing.T) {
	api := newHTTPAPI(t, false)
	s, srv := fakeToolModel(t,
		toolCall{"http_send", map[string]any{"path": "api/users.http", "name": "broken", "env": "dev"}},
		toolCall{"http_send", map[string]any{"path": "api/users.http", "name": "nosuch"}},
		toolCall{"http_send", map[string]any{"path": "api/users.http", "name": "big", "env": "dev"}},
		toolCall{"http_get_last_response", map[string]any{"part": "body", "offset": 1024}},
		toolCall{"http_environments", map[string]any{}})
	d := aiDriverConfig(t, srv.URL, aiHTTPFiles(api.srv.URL), false, `"http_ai_max_kb":1`)
	askAI(d, "go")
	httpWaitDone(d)
	r := s.toolResults()
	if len(r) != 5 {
		t.Fatalf("results %q", r)
	}
	if !strings.Contains(r[0], "unresolved variables: nope") || api.count("/x") != 0 {
		t.Errorf("broken: %s", r[0])
	}
	if !strings.Contains(r[1], "getUser, createUser, whoami, broken, big") {
		t.Errorf("unknown name: %s", r[1])
	}
	if !strings.Contains(r[2], "[truncated:") || !strings.Contains(r[2], "offset:1024") {
		t.Errorf("cap: %s", r[2])
	}
	if !strings.Contains(r[3], "body (from offset 1024)") {
		t.Errorf("page: %s", r[3])
	}
	if !strings.Contains(r[4], "token = [hidden]") || !strings.Contains(r[4], "host = "+api.srv.URL) {
		t.Errorf("environments: %s", r[4])
	}
	var audit string
	for _, l := range d.Model().AIAuditLines() {
		audit += l + "\n"
	}
	if !strings.Contains(audit, "gate=invalid") {
		t.Errorf("an unresolved name must be an invalid gate in the audit:\n%s", audit)
	}
	noToken(t, d, r)
}

// http_set_environment asks (class ide); settings_set http_timeout asks
// with class settings.
func TestAIHTTPSetEnvironmentAndSetting(t *testing.T) {
	api := newHTTPAPI(t, false)
	s, srv := fakeToolModel(t,
		toolCall{"http_set_environment", map[string]any{"name": "dev"}},
		toolCall{"settings_set", map[string]any{"key": "http_timeout", "value": "5"}})
	d := aiDriver(t, srv.URL, aiHTTPFiles(api.srv.URL), false)
	askAI(d, "set")
	waitConfirm(d)
	if !strings.Contains(screen(d), "Always allow ide") {
		t.Fatalf("env card:\n%s", screen(d))
	}
	clickAIAllow(d)
	waitConfirm(d)
	if !strings.Contains(screen(d), "Always allow settings") {
		t.Fatalf("setting card:\n%s", screen(d))
	}
	clickAIAllow(d)
	httpWaitDone(d)
	r := s.toolResults()
	if len(r) != 2 || !strings.Contains(r[0], "environment set to dev") || !strings.Contains(r[1], "http_timeout = 5") {
		t.Fatalf("results %q", r)
	}
	st, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "http", "state.json"))
	if !strings.Contains(string(st), `"dev"`) {
		t.Fatalf("state.json: %s", st)
	}
}

// show_panel http-response opens the page on the last answer.
func TestAIHTTPShowPanel(t *testing.T) {
	api := newHTTPAPI(t, false)
	_, srv := fakeToolModel(t,
		toolCall{"http_send", map[string]any{"path": "api/users.http", "name": "getUser", "env": "dev"}},
		toolCall{"show_panel", map[string]any{"name": "http-response"}})
	d := aiDriver(t, srv.URL, aiHTTPFiles(api.srv.URL), false)
	open(d, "api/users.http")
	lua(d, `vim.api.nvim_buf_set_lines(0, 0, 0, false, { '# unsaved edit' }); return ''`)
	askAI(d, "go")
	httpWaitDone(d)
	if !strings.Contains(httpPageText(d), "200 OK") {
		t.Fatalf("page:\n%s", httpPageText(d))
	}
}
