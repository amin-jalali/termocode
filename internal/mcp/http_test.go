package mcp

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

func httpServer(url string) Server {
	return Server{Name: "remote", Type: TypeHTTP, URL: url, Source: SourceUser}
}

func startHTTP(t *testing.T, s Server, ho HTTPOptions) *Process {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := StartHTTP(ctx, s, ho, Options{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// rpcs lists the JSON-RPC methods the fake got by POST.
func rpcs(f *mcptest.HTTPServer) []string {
	var out []string
	for _, r := range f.Requests() {
		if r.HTTPMethod == "POST" {
			out = append(out, r.RPC)
		}
	}
	return out
}

// Spec (MC-5): JSON answers. The handshake runs, the negotiated
// MCP-Protocol-Version header goes on every later request, tools list
// and run; Accept names both JSON and SSE.
func TestHTTPJSON(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{})
	p := startHTTP(t, httpServer(f.MCPURL()), HTTPOptions{})
	if p.Pid() != 0 || p.Client.Version() != ProtocolVersion || p.Client.ServerInfo().Name != "fake-mcp-http" {
		t.Fatalf("pid %d version %q info %+v", p.Pid(), p.Client.Version(), p.Client.ServerInfo())
	}
	tools, err := p.Client.ListTools(context.Background())
	if err != nil || len(tools) != 10 {
		t.Fatalf("tools %d, %v", len(tools), err)
	}
	r, err := p.Client.CallTool(context.Background(), "echo", map[string]any{"text": "hi"})
	if err != nil || r.Text() != "echo: hi" {
		t.Fatalf("call %q %v", r.Text(), err)
	}
	reqs := f.Requests()
	if got := strings.Join(rpcs(f), ","); got != "server/discover,initialize,notifications/initialized,tools/list,tools/call" {
		t.Errorf("rpcs %s", got)
	}
	// MCPX-20: the stateless probe names its revision; the old server's 4xx sends us to initialize.
	if reqs[0].Header.Get("MCP-Protocol-Version") != StatelessVersion {
		t.Errorf("probe: protocol header %q", reqs[0].Header.Get("MCP-Protocol-Version"))
	}
	reqs = reqs[1:]
	if reqs[0].Header.Get("MCP-Protocol-Version") != "" {
		t.Error("initialize sent a protocol version header")
	}
	for _, r := range reqs[1:] {
		if r.Header.Get("MCP-Protocol-Version") != ProtocolVersion {
			t.Errorf("%s: protocol header %q", r.RPC, r.Header.Get("MCP-Protocol-Version"))
		}
	}
	if a := reqs[2].Header.Get("Accept"); !strings.Contains(a, "application/json") || !strings.Contains(a, "text/event-stream") {
		t.Errorf("Accept %q", a)
	}
}

// Spec: SSE answers — progress notifications and the answer come in one
// stream; progress pushes the deadline back over HTTP too.
func TestHTTPSSE(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{SSE: true})
	p := startHTTP(t, httpServer(f.MCPURL()), HTTPOptions{})
	if tools, err := p.Client.ListTools(context.Background()); err != nil || len(tools) != 10 {
		t.Fatalf("tools %d %v", len(tools), err)
	}
	r, err := p.Client.CallToolTimeout(context.Background(), "slow", map[string]any{"total_ms": 800, "progress_ms": 100}, 350*time.Millisecond)
	if err != nil || r.Text() != "slow done" {
		t.Fatalf("slow with progress: %q %v", r.Text(), err)
	}
}

// Spec: the Mcp-Session-Id from initialize is sent back on every later
// request, and Close ends the session with DELETE.
func TestHTTPSession(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Session: true})
	p := startHTTP(t, httpServer(f.MCPURL()), HTTPOptions{})
	if _, err := p.Client.CallTool(context.Background(), "echo", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	reqs := f.Requests()[1:] // after the server/discover probe (MCPX-20)
	sid := reqs[1].Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("no session id sent")
	}
	for _, r := range reqs[1:] {
		if r.Header.Get("Mcp-Session-Id") != sid {
			t.Errorf("%s: session %q", r.RPC, r.Header.Get("Mcp-Session-Id"))
		}
	}
	p.Close()
	if f.Deletes() != 1 {
		t.Errorf("deletes %d", f.Deletes())
	}
	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Error("Done not closed")
	}
}

// Spec: a session the server forgot (404) ends the connection with
// ErrSessionExpired, so the Manager connects again.
func TestHTTPSessionExpired(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Session: true})
	p := startHTTP(t, httpServer(f.MCPURL()), HTTPOptions{})
	f.ExpireSessions()
	_, err := p.Client.CallTool(context.Background(), "echo", map[string]any{"text": "x"})
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Error("expired connection not done")
	}
}

// Spec: 401 → *AuthError with the resource_metadata URL and scope from
// WWW-Authenticate.
func TestHTTPUnauthorized(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: "right", AuthServer: "https://as.example", Scopes: []string{"mcp", "read"}})
	_, err := StartHTTP(context.Background(), httpServer(f.MCPURL()), HTTPOptions{}, Options{})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err %v", err)
	}
	if ae.ResourceMetadata != f.URL+"/.well-known/oauth-protected-resource/mcp" || ae.Scope != "mcp read" {
		t.Errorf("auth error %+v", ae)
	}
}

// Spec: static headers (a PAT as Authorization) are sent on every
// request.
func TestHTTPStaticBearer(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: "pat-1234567890"})
	s := httpServer(f.MCPURL())
	s.Headers = map[string]string{"Authorization": "Bearer pat-1234567890", "X-Team": "a"}
	p := startHTTP(t, s, HTTPOptions{})
	if _, err := p.Client.CallTool(context.Background(), "echo", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range f.Requests() {
		if r.Header.Get("X-Team") != "a" {
			t.Errorf("%s %s: no static header", r.HTTPMethod, r.RPC)
		}
	}
}

// Spec: a token that the server refuses is refreshed once and the request
// sent again; when the refresh does not help, the 401 comes back (no
// loop).
func TestHTTPRefreshOn401(t *testing.T) {
	var good atomic.Value
	good.Store("new-token-abc")
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: func(tok string) bool { return tok == good.Load().(string) },
		AuthServer: "https://as.example"})
	var cur atomic.Value
	cur.Store("old-token-abc")
	var refreshes atomic.Int32
	ho := HTTPOptions{
		Token: func(context.Context) string { return cur.Load().(string) },
		Refresh: func(context.Context, *AuthError) bool {
			refreshes.Add(1)
			cur.Store("new-token-abc")
			return true
		},
	}
	p := startHTTP(t, httpServer(f.MCPURL()), ho)
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes %d", refreshes.Load())
	}
	good.Store("something-else")
	before := len(f.Requests())
	_, err := p.Client.CallTool(context.Background(), "echo", map[string]any{"text": "x"})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err %v", err)
	}
	if n := len(f.Requests()) - before; n != 2 || refreshes.Load() != 2 {
		t.Errorf("after a useless refresh: %d requests, %d refreshes (want 2, 2)", n, refreshes.Load())
	}
}

// Spec (resumption): when the SSE stream ends before the answer, the
// client sends GET with Last-Event-ID and gets the answer there.
func TestHTTPResume(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{SSE: true, DropStream: true})
	p := startHTTP(t, httpServer(f.MCPURL()), HTTPOptions{})
	r, err := p.Client.CallTool(context.Background(), "echo", map[string]any{"text": "again"})
	if err != nil || r.Text() != "echo: again" {
		t.Fatalf("call %q %v", r.Text(), err)
	}
	var got bool
	for _, q := range f.Requests() {
		if q.HTTPMethod == "GET" && q.Header.Get("Last-Event-ID") != "" {
			got = true
		}
	}
	if !got {
		t.Error("no GET with Last-Event-ID")
	}
}

// Spec: cancelling a call closes its stream at once and tells the server
// (notifications/cancelled).
func TestHTTPCancel(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{SSE: true})
	p := startHTTP(t, httpServer(f.MCPURL()), HTTPOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := p.Client.CallTool(ctx, "slow", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("cancel took %v", d)
	}
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		if strings.Contains(strings.Join(rpcs(f), ","), "notifications/cancelled") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("no notifications/cancelled: %v", rpcs(f))
}
