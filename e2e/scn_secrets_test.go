//go:build e2e

package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// SCN-1: secrets never leak. The real binary stores four secrets the real
// ways (an AI key through the key prompt, a database password, an MCP
// server secret, an HTTP $secret), uses each one against a fake, and then
// looks for them everywhere: every byte termocode drew on the terminal,
// every file under HOME, the workspace and the runtime dir, what the
// model and an outside MCP agent got back from tools, and the argv and
// environment of every process it started. The only place a value may sit
// is the encrypted store, and there it is not in clear.

// scnCanaries are the four secrets. They share one random core, built at
// run time (no token-shaped literal in the source), so one search finds
// any of them.
type scnCanaries struct {
	core                    string
	aiKey, db, mcpTok, http string
}

func newSCNCanaries() scnCanaries {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	core := "cnry" + hex.EncodeToString(b)
	return scnCanaries{core: core, aiKey: "sk-" + core + "ai", db: core + "db", mcpTok: core + "mcp", http: core + "http"}
}

// scnModel is the fake AI provider: it answers request i with calls[i]
// (one native tool call), then "fake-done", and keeps every request body
// and Authorization header.
type scnChatModel struct {
	mu     sync.Mutex
	bodies []string
	auth   []string
}

func newSCNChatModel(t *testing.T, calls []scnCall) (*scnChatModel, *httptest.Server) {
	t.Helper()
	m := &scnChatModel{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		i := len(m.bodies)
		m.bodies = append(m.bodies, string(body))
		m.auth = append(m.auth, r.Header.Get("Authorization"))
		m.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		var delta map[string]any
		if i < len(calls) {
			a, _ := json.Marshal(calls[i].args)
			delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("call_%d", i),
				"function": map[string]any{"name": calls[i].name, "arguments": string(a)}}}}
		} else {
			delta = map[string]any{"content": "fake-done"}
		}
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
	}))
	t.Cleanup(srv.Close)
	return m, srv
}

type scnCall struct {
	name string
	args map[string]any
}

// toolResults are the tool results of the last request.
func (m *scnChatModel) toolResults() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.bodies) == 0 {
		return nil
	}
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal([]byte(m.bodies[len(m.bodies)-1]), &req)
	var out []string
	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			out = append(out, msg.Content)
		}
	}
	return out
}

// scnLeaksInTree lists the files under root that hold core (in clear).
func scnLeaksInTree(root, core string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !e.Type().IsRegular() {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte(core)) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// scnLeaksInProcs lists the processes of home / dirs whose argv holds
// core, or whose environment holds it unless envOK says that process may
// have it (an MCP server gets its own secret). Linux only (/proc).
func scnLeaksInProcs(home string, dirs []string, core string, envOK func(argv string) bool) []string {
	var out []string
	for _, p := range procsOf(home, dirs) {
		pid, err := strconv.Atoi(strings.Fields(p)[0])
		if err != nil {
			continue
		}
		base := filepath.Join("/proc", strconv.Itoa(pid))
		argv, _ := os.ReadFile(filepath.Join(base, "cmdline"))
		if bytes.Contains(argv, []byte(core)) {
			out = append(out, "argv of "+p)
		}
		env, _ := os.ReadFile(filepath.Join(base, "environ"))
		if bytes.Contains(env, []byte(core)) && !envOK(string(bytes.ReplaceAll(argv, []byte{0}, []byte{' '}))) {
			out = append(out, "environment of "+p)
		}
	}
	return out
}

// scnFakeRedisAuth is the fake Redis of db_test.go that also wants a
// password: it records the AUTH it got.
func (f *fakeRedisTCP) auths() []string {
	var out []string
	for _, c := range f.commands() {
		if strings.HasPrefix(strings.ToUpper(c), "AUTH ") {
			out = append(out, c)
		}
	}
	return out
}

// Spec (SCN-1): see the top of the file.
func TestSCNSecretsNeverLeak(t *testing.T) {
	c := newSCNCanaries()
	r := newSessionRig(t)

	// The fakes: a Redis that takes a password, an API that wants the
	// HTTP token, an MCP server that gets a secret in its environment and
	// repeats it in a tool result.
	redis := newFakeRedisTCP(t, map[string]string{"greeting": "hello"})
	var apiMu sync.Mutex
	var apiAuth []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		apiMu.Lock()
		apiAuth = append(apiAuth, req.Header.Get("Authorization"))
		apiMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":42,"name":"Ada"}`)
	}))
	defer api.Close()
	script, err := filepath.Abs("testdata/fake-mcp-secret.sh")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(r.home, "fake-mcp.running")

	cfg := r.configDir()
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpJSON, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fake": map[string]any{
		"command": "sh", "args": []string{script},
		// http_tok is named here only so the servers list offers to set
		// it: HTTP {{$secret NAME}} reads the same store entries.
		"env": map[string]string{"FAKE_TOKEN": "${secret:mcp_tok}", "FAKE_HTTP": "${secret:http_tok}", "FAKE_MARKER": marker}}}})
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(cfg, "mcp.json"), string(mcpJSON))
	write(filepath.Join(cfg, "config.json"), `{"setup_done":true,"mcp_server":"on","mcp_idle_timeout":"10m"}`)
	write(filepath.Join(r.ws, ".termocode", "databases.json"),
		fmt.Sprintf(`{"connections":[{"id":"cache","name":"cache","driver":"redis","host":"127.0.0.1","port":%d}]}`, redis.port()))
	write(filepath.Join(r.ws, "api", "users.http"), "@host = "+api.URL+
		"\n\n### Get one user\n# @name getUser\nGET {{host}}/users/42\nAuthorization: Bearer {{$secret http_tok}}\n")

	// The model asks for every tool that lists what the user set up, the
	// MCP tool that repeats its secret, and an HTTP send.
	calls := []scnCall{
		{"mcp__fake__whoami", map[string]any{}},
		{"profiles_list", map[string]any{}},
		{"models_list", map[string]any{}},
		{"mcp_services_list", map[string]any{}},
		{"settings_list", map[string]any{}},
		{"db_connections", map[string]any{}},
		{"redis_keys", map[string]any{"conn": "cache", "pattern": "*"}},
		{"http_environments", map[string]any{}},
		{"http_list_requests", map[string]any{}},
		{"http_history", map[string]any{}},
		{"get_ide_state", map[string]any{}},
		{"services_list", map[string]any{}},
		{"outside_agents_status", map[string]any{}},
		{"usage_summary", map[string]any{}},
	}
	model, srv := newSCNChatModel(t, calls)
	r.env = append(r.env, "TERMOCODE_AI_PROVIDER=openai", "TERMOCODE_AI_MODEL=fake", "TERMOCODE_AI_BASE_URL="+srv.URL+"/v1")

	tm := r.start()
	defer func() {
		if t.Failed() {
			t.Logf("canary core %s", c.core)
		}
	}()

	// 1. The AI key, through AI: Configure Provider (creates the store).
	tm.startAIKeyPrompt("OpenAI")
	tm.typeMasked(secretsPrompt, secretsPass)
	tm.typeMasked("Type it again:", secretsPass)
	tm.WaitText("OpenAI API key", secretsWait)
	tm.settle()
	tm.Type(c.aiKey)
	tm.Send(KeyEnter)
	tm.WaitText("AI Model", secretsWait)
	tm.Send(KeyEsc)
	tm.WaitGone("AI Model", uiTimeout)

	// 2. The MCP server's secret and the HTTP token, from the servers list.
	for _, sec := range []struct{ name, value string }{{"mcp_tok", c.mcpTok}, {"http_tok", c.http}} {
		tm.Palette("AI: MCP Servers")
		tm.WaitText("fake", uiTimeout)
		tm.settle()
		tm.Type("fake")
		tm.settle()
		tm.Send(KeyEnter)
		tm.WaitText("Set secret "+sec.name, uiTimeout)
		tm.Type("Set secret " + sec.name)
		tm.settle()
		tm.Send(KeyEnter)
		tm.typeMasked("Value of ${secret:"+sec.name+"}:", sec.value)
		tm.WaitText("Saved secret "+sec.name, uiTimeout)
		tm.settle()
	}
	// 3. The database password, from the Databases view (p on the
	// connection), then a connect that uses it.
	openDatabases(tm)
	tm.WaitText("cache", uiTimeout)
	tm.Send("p")
	tm.typeMasked("Password:", c.db)
	tm.settle()
	tm.Send(KeyEnter) // connect
	tm.WaitText("db0", 10*time.Second)
	if a := redis.auths(); len(a) == 0 || !strings.Contains(a[0], c.db) {
		t.Errorf("Redis got no AUTH with the password: %q", redis.commands())
	}
	tm.Send(KeyEsc)
	tm.settle()

	// 4. The HTTP token, in a request sent from the .http file.
	tm.Palette("HTTP: Send Request…")
	tm.WaitText("getUser", uiTimeout)
	tm.Type("getUser")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("200 OK", uiTimeout)
	tm.settle()
	tm.Send("q")
	tm.WaitGone("HTTP Response", uiTimeout)
	apiMu.Lock()
	if len(apiAuth) == 0 || apiAuth[0] != "Bearer "+c.http {
		t.Errorf("the API did not get the HTTP token: %q", apiAuth)
	}
	apiMu.Unlock()

	// 5. The AI chat: the key reaches the provider (and only as the
	// header); the MCP server starts with its secret; the tool results
	// hold no secret.
	tm.Send("\x1ba")
	tm.WaitText("AI CHAT", uiTimeout)
	tm.Type("what is set up here?")
	tm.Send(KeyEnter)
	tm.WaitText("fake-done", 60*time.Second)
	model.mu.Lock()
	if len(model.auth) == 0 || model.auth[0] != "Bearer "+c.aiKey {
		t.Errorf("the provider did not get the stored key: %q", model.auth)
	}
	for i, b := range model.bodies {
		if strings.Contains(b, c.core) {
			t.Errorf("request %d to the model holds a secret", i)
		}
	}
	model.mu.Unlock()
	res := model.toolResults()
	if len(res) != len(calls) {
		t.Errorf("%d tool results for %d calls", len(res), len(calls))
	}
	for i, out := range res {
		if i < len(calls) && strings.Contains(out, c.core) {
			t.Errorf("tool %s gave the model a secret: %s", calls[i].name, out)
		}
	}
	if len(res) > 0 && !strings.Contains(res[0], "signed in with token") {
		t.Errorf("the MCP tool did not run: %q", res[0])
	}
	if !exists(marker) {
		t.Error("the MCP server is not running")
	}

	// 6. Every process termocode started, while they run: no secret in
	// argv; in the environment only for the MCP server that was given it.
	allowed := 0
	isMCP := func(argv string) bool {
		ok := strings.Contains(argv, "fake-mcp-secret.sh")
		if ok {
			allowed++
		}
		return ok
	}
	for _, l := range scnLeaksInProcs(r.home, []string{r.ws}, c.core, isMCP) {
		t.Errorf("secret in the %s", l)
	}
	if runtime.GOOS == "linux" && allowed == 0 {
		t.Error("the process scan did not see the MCP server's secret (is the scan blind?)")
	}

	// 7. An outside agent over MCP lists what it can and reads files:
	// no secret in any reply.
	relay := startRelayEnv(t, r.home, r.env, "--dir", r.ws)
	relay.call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"scn-agent"}}}`)
	if _, err := io.WriteString(relay.in, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	for i, call := range []string{
		`"db_connections","arguments":{}`,
		`"redis_keys","arguments":{"conn":"cache","pattern":"*"}`,
		`"http_environments","arguments":{}`,
		`"http_list_requests","arguments":{}`,
		`"http_history","arguments":{}`,
		`"get_ide_state","arguments":{}`,
		`"services_list","arguments":{}`,
		`"read_file","arguments":{"path":"api/users.http"}`,
		`"search","arguments":{"query":"` + c.core + `"}`,
	} {
		out, _ := relay.call(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%s}}`, i+2, call))
		if strings.Contains(out, c.core) {
			t.Errorf("MCP %s gave a secret: %s", call, out)
		}
	}
	_ = relay.in.Close()

	// 8. Reconnect, so the session restores the screen, then quit.
	tm.Disconnect()
	raws := []string{tm.rawOutput()}
	tm2 := r.start()
	tm2.settle()
	raws = append(raws, tm2.rawOutput())
	tm2.Quit()
	raws = append(raws, tm2.rawOutput())
	_ = r.run("kill")
	r.waitSocket(false)

	// 9. Every byte drawn on the terminal, and every file.
	for i, out := range raws {
		if strings.Contains(out, c.core) {
			j := strings.Index(out, c.core)
			t.Errorf("client %d drew a secret on the terminal: …%q…", i, out[max(0, j-80):min(len(out), j+40)])
		}
	}
	for _, root := range []string{r.home, r.ws, r.runtime} {
		for _, f := range scnLeaksInTree(root, c.core) {
			t.Errorf("secret in clear in %s", f)
		}
	}
	if _, err := os.Stat(r.ageFile()); err != nil {
		t.Errorf("no encrypted store: %v", err)
	}
}
