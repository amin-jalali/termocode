package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// mcpServeHTTPEnv is an MCP source over a workspace with an HTTP
// environment, and a loopback test server that echoes what it got (no
// other network).
type mcpServeHTTPEnv struct {
	root   string
	model  Model
	src    *mcpServeSource
	hub    *mcpServeHub
	srv    *httptest.Server
	mu     sync.Mutex
	hits   []string // "METHOD path auth"
	cards  []mcpServeDBCardSeen
	answer error
}

const mcpServeHTTPToken = "tok-mcp-http-secret-90210"

func newMCPServeHTTPEnv(t *testing.T) *mcpServeHTTPEnv {
	t.Helper()
	e := &mcpServeHTTPEnv{model: aiTestModel(t), root: t.TempDir()}
	e.model.output = newOutputStore()
	if err := os.WriteFile(filepath.Join(e.root, "http-client.private.env.json"), []byte(`{"dev":{"token":"`+mcpServeHTTPToken+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root, "http-client.env.json"), []byte(`{"dev":{"host":"localhost"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.hits = append(e.hits, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		e.mu.Unlock()
		_, _ = w.Write([]byte("you sent: " + r.Header.Get("Authorization")))
	}))
	t.Cleanup(e.srv.Close)
	e.src, e.hub, _ = mcpServeTestSource(e.root, func(_ context.Context, client string, tool ai.Tool, human string) error {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.cards = append(e.cards, mcpServeDBCardSeen{client, tool.Name, tool.Class, human})
		return e.answer
	})
	e.src.allow = &mcpServeAllowSet{}
	t.Cleanup(e.hub.stop)
	return e
}

func (e *mcpServeHTTPEnv) call(t *testing.T, client string, args map[string]any) (mcp.CallResult, error) {
	t.Helper()
	r, err, _ := mcpServeDrive(t, &e.model, e.hub, func() (mcp.CallResult, error) {
		return e.src.Call(mcpServeCtx(client), "http_send", args)
	})
	return r, err
}

// saveRequest saves a request for rawURL in the workspace (a .http
// file), so its host counts as one the user already uses.
func (e *mcpServeHTTPEnv) saveRequest(t *testing.T, rawURL string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.root, "saved.http"), []byte("### saved\nGET "+rawURL+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *mcpServeHTTPEnv) seen() ([]string, []mcpServeDBCardSeen) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.hits...), append([]mcpServeDBCardSeen(nil), e.cards...)
}

// Spec (MS-7): http_send decides exactly like the in-app chat for the
// same input: GET / HEAD / OPTIONS to loopback run at once, other hosts
// and methods ask (per host, /write for other methods), private hosts
// always ask with no "Always allow".
func TestMCPServeHTTPSendGateParity(t *testing.T) {
	e := newMCPServeHTTPEnv(t)
	if err := os.WriteFile(filepath.Join(e.root, "api.http"), []byte("### list\nGET http://localhost:8080/users\n\n### make\nPOST https://api.example.com/users\n\n{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(&e.model)
		return out, err
	}
	chat, _ := ai.FindTool(e.model.aiChatTools(e.root, call), "http_send")
	outside, ok := ai.FindTool(mcpServeTools(e.model.aiChatTools(e.root, call), e.root, call), "http_send")
	if !ok {
		t.Fatal("http_send is not exposed")
	}
	for _, tc := range []map[string]any{
		{"url": "http://localhost:8080/x"},
		{"method": "HEAD", "url": "http://127.0.0.1/x"},
		{"method": "OPTIONS", "url": "http://[::1]:3000/"},
		{"method": "POST", "url": "http://localhost:8080/users", "body": "{}"},
		{"method": "DELETE", "url": "http://127.0.0.1:5000/u/1"},
		{"url": "https://api.example.com/users"},
		{"method": "PATCH", "url": "https://api.example.com/u"},
		{"url": "http://10.0.0.1/"},
		{"url": "http://169.254.169.254/latest/meta-data/"},
		{"url": "http://db:5432/"},
		{"url": "http://{{host}}:8080/me", "env": "dev", "headers": map[string]any{"Authorization": "Bearer {{token}}"}},
		{"path": "api.http", "name": "list"},
		{"path": "api.http", "name": "make"},
	} {
		a1, a2 := copyArgs(tc), copyArgs(tc)
		if err := chat.Validate(a1); err != nil {
			t.Fatalf("%v: chat refused: %v", tc, err)
		}
		if err := outside.Validate(a2); err != nil {
			t.Fatalf("%v: outside refused: %v", tc, err)
		}
		cm, cc := chat.Gate(a1)
		gated, om := mcpServeGate(outside, a2)
		if cm != om || cc != gated.Class {
			t.Errorf("%v: chat (%v, %q), outside (%v, %q)", tc, cm, cc, om, gated.Class)
		}
		httpAICache.Delete(httpAIKey(a1))
		httpAICache.Delete(httpAIKey(a2))
	}
}

func copyArgs(a map[string]any) map[string]any {
	out := make(map[string]any, len(a))
	for k, v := range a {
		out[k] = v
	}
	return out
}

// Spec (MS-7): a secret from the private env file is resolved inside
// termocode (the server gets it), and the agent's result shows it hidden.
// A loopback GET runs at once, like in the chat.
func TestMCPServeHTTPSendSecretResolvedAndRedacted(t *testing.T) {
	e := newMCPServeHTTPEnv(t)
	e.saveRequest(t, e.srv.URL+"/saved")
	r, err := e.call(t, "codex", map[string]any{"url": e.srv.URL + "/me", "env": "dev",
		"headers": map[string]any{"Authorization": "Bearer {{token}}"}})
	if err != nil || r.IsError {
		t.Fatalf("send: %+v %v", r, err)
	}
	hits, cards := e.seen()
	if len(cards) != 0 {
		t.Fatalf("a loopback GET asked: %+v", cards)
	}
	if len(hits) != 1 || hits[0] != "GET /me Bearer "+mcpServeHTTPToken {
		t.Fatalf("the server got %v", hits)
	}
	if text := mcpServeText(r); strings.Contains(text, mcpServeHTTPToken) || !strings.Contains(text, "you sent:") {
		t.Fatalf("result leaks the secret or lacks the body:\n%s", text)
	}
}

// Spec (MS-7): a non-GET asks; the card says a secret is sent without
// showing it; Deny is -32000 and nothing is sent.
func TestMCPServeHTTPSendDenySendsNothing(t *testing.T) {
	e := newMCPServeHTTPEnv(t)
	e.saveRequest(t, e.srv.URL+"/saved")
	e.answer = &mcp.CallError{Code: mcp.CodeDeclined, Message: "declined by the user"}
	r, err := e.call(t, "codex", map[string]any{"method": "POST", "url": e.srv.URL + "/users", "env": "dev", "body": "{}",
		"headers": map[string]any{"Authorization": "Bearer {{token}}"}})
	var ce *mcp.CallError
	if !errors.As(err, &ce) || ce.Code != mcp.CodeDeclined || len(r.Content) != 0 {
		t.Fatalf("deny: %+v %v", r, err)
	}
	hits, cards := e.seen()
	if len(hits) != 0 {
		t.Fatalf("a declined request was sent: %v", hits)
	}
	if len(cards) != 1 || !strings.HasSuffix(cards[0].class, "/write") {
		t.Fatalf("cards = %+v", cards)
	}
	if h := cards[0].human; !strings.Contains(h, "POST") || !strings.Contains(h, "secret") || strings.Contains(h, mcpServeHTTPToken) {
		t.Fatalf("card = %q", h)
	}
}

// Spec (MS-7): Always allow for http_send is per agent and per host (and
// /write for other methods), never for a private host; it does not cover
// another host, another method class or another agent.
func TestMCPServeHTTPSendAlwaysPerHost(t *testing.T) {
	e := newMCPServeHTTPEnv(t)
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(&e.model)
		return out, err
	}
	tool, _ := ai.FindTool(mcpServeTools(e.model.aiChatTools(e.root, call), e.root, call), "http_send")
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"url": "https://api.example.com/users"}, "http_send:api.example.com"},
		{map[string]any{"method": "DELETE", "url": "https://api.example.com/u/1"}, "http_send:api.example.com/write"},
		{map[string]any{"method": "POST", "url": "http://localhost:8080/x"}, "http_send:localhost:8080/write"},
		{map[string]any{"url": "http://10.0.0.1/"}, ""},
	} {
		gated, ask := mcpServeGate(tool, tc.args)
		if got := mcpServeAlwaysKey(gated, tc.args); !ask || got != tc.want {
			t.Errorf("%v: ask %v key %q, want %q", tc.args, ask, got, tc.want)
		}
		httpAICache.Delete(httpAIKey(tc.args))
	}
	host := strings.TrimPrefix(e.srv.URL, "http://")
	e.src.allow.replace(map[string][]string{"codex": {"http_send:" + host + "/write"}})
	post := map[string]any{"method": "POST", "url": e.srv.URL + "/a", "body": "{}"}
	if r, err := e.call(t, "codex", post); err != nil || r.IsError {
		t.Fatalf("remembered POST: %+v %v", r, err)
	}
	e.answer = &mcp.CallError{Code: mcp.CodeDeclined, Message: "declined"}
	e.call(t, "claude-code", map[string]any{"method": "POST", "url": e.srv.URL + "/b", "body": "{}"})
	e.call(t, "codex", map[string]any{"method": "POST", "url": "http://localhost:1/c", "body": "{}"})
	hits, cards := e.seen()
	if len(hits) != 1 || !strings.HasPrefix(hits[0], "POST /a") || len(cards) != 2 {
		t.Fatalf("hits %v, cards %+v", hits, cards)
	}
}

// Spec (MS-7): a request built for an earlier call is never reused for a
// new call whose arguments map happens to have the same address
// (httpAICache is keyed by the map pointer).
func TestMCPServeHTTPSendNoStaleRequest(t *testing.T) {
	e := newMCPServeHTTPEnv(t)
	args := map[string]any{"url": e.srv.URL + "/fresh"}
	httpAICache.Store(httpAIKey(args), httpAIBuild(e.root, map[string]any{"url": e.srv.URL + "/stale"}))
	if r, err := e.call(t, "codex", args); err != nil || r.IsError {
		t.Fatalf("send: %+v %v", r, err)
	}
	if hits, _ := e.seen(); len(hits) != 1 || !strings.HasPrefix(hits[0], "GET /fresh") {
		t.Fatalf("the server got %v", hits)
	}
	if _, ok := httpAICache.Load(httpAIKey(args)); ok {
		t.Fatal("the call left its request in the cache")
	}
}

// Spec (fix 1): a request that sends a saved secret to a host none of the
// user's saved requests uses is refused in Validate (-32602): no card,
// nothing sent. The same request to a saved host works as before.
func TestMCPServeHTTPSendSecretToUnknownHostRefused(t *testing.T) {
	secret := map[string]any{"Authorization": "Bearer {{token}}"}
	t.Run("unknown host", func(t *testing.T) {
		e := newMCPServeHTTPEnv(t)
		r, err := e.call(t, "codex", map[string]any{"method": "POST", "url": e.srv.URL + "/x", "env": "dev", "body": "{}", "headers": secret})
		var ce *mcp.CallError
		if !errors.As(err, &ce) || ce.Code != mcp.CodeInvalidParams || len(r.Content) != 0 {
			t.Fatalf("want -32602, got %+v %v", r, err)
		}
		for _, want := range []string{"sends a saved secret to " + strings.ToLower(strings.TrimSuffix(e.srv.URL, "/")), "not used by any of your saved requests"} {
			if !strings.Contains(ce.Message, want) {
				t.Errorf("message %q lacks %q", ce.Message, want)
			}
		}
		if strings.Contains(ce.Message, mcpServeHTTPToken) {
			t.Fatal("the message leaks the secret")
		}
		if hits, cards := e.seen(); len(hits) != 0 || len(cards) != 0 {
			t.Fatalf("hits %v cards %+v", hits, cards)
		}
	})
	t.Run("saved host, GET runs at once", func(t *testing.T) {
		e := newMCPServeHTTPEnv(t)
		e.saveRequest(t, e.srv.URL+"/other")
		if r, err := e.call(t, "codex", map[string]any{"url": e.srv.URL + "/x", "env": "dev", "headers": secret}); err != nil || r.IsError {
			t.Fatalf("send: %+v %v", r, err)
		}
		if hits, cards := e.seen(); len(hits) != 1 || len(cards) != 0 {
			t.Fatalf("hits %v cards %+v", hits, cards)
		}
	})
	t.Run("saved host, POST asks", func(t *testing.T) {
		e := newMCPServeHTTPEnv(t)
		e.saveRequest(t, e.srv.URL+"/other")
		if r, err := e.call(t, "codex", map[string]any{"method": "POST", "url": e.srv.URL + "/x", "env": "dev", "body": "{}", "headers": secret}); err != nil || r.IsError {
			t.Fatalf("send: %+v %v", r, err)
		}
		if hits, cards := e.seen(); len(hits) != 1 || len(cards) != 1 {
			t.Fatalf("hits %v cards %+v", hits, cards)
		}
	})
	t.Run("a host from an environment value counts", func(t *testing.T) {
		e := newMCPServeHTTPEnv(t)
		if err := os.WriteFile(filepath.Join(e.root, "http-client.env.json"), []byte(`{"dev":{"base":"`+e.srv.URL+`/api"}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if r, err := e.call(t, "codex", map[string]any{"url": e.srv.URL + "/x", "env": "dev", "headers": secret}); err != nil || r.IsError {
			t.Fatalf("send: %+v %v", r, err)
		}
	})
	t.Run("no secret, other host: the old gate", func(t *testing.T) {
		e := newMCPServeHTTPEnv(t)
		if r, err := e.call(t, "codex", map[string]any{"url": e.srv.URL + "/plain"}); err != nil || r.IsError {
			t.Fatalf("send: %+v %v", r, err)
		}
		if hits, cards := e.seen(); len(hits) != 1 || len(cards) != 0 {
			t.Fatalf("hits %v cards %+v", hits, cards)
		}
	})
}

// Spec (fix 1): host matching ignores path and query, letter case and the
// default port; scheme and other ports differ.
func TestMCPServeHTTPOriginMatching(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.http"), []byte("### a\nGET HTTPS://API.Example.COM:443/v1/x?y=1\n\n### b\nGET http://Web.Example.com:80/\n\n### c\nGET http://localhost:8080/z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := mcpServeSavedHTTPOrigins(root)
	for raw, want := range map[string]bool{
		"https://api.example.com/other/path?q=2": true,
		"https://API.EXAMPLE.COM:443/":           true,
		"http://web.example.com/":                true,
		"http://web.example.com:80/q":            true,
		"http://localhost:8080/":                 true,
		"http://api.example.com/":                false, // other scheme
		"https://api.example.com:8443/":          false, // other port
		"http://localhost:9090/":                 false,
		"https://evil.example.net/":              false,
		"not a url":                              false,
	} {
		if got := saved[mcpServeHTTPOrigin(raw)]; got != want {
			t.Errorf("%s: saved = %v, want %v (origin %q)", raw, got, want, mcpServeHTTPOrigin(raw))
		}
	}
}
