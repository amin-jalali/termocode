package mcptest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// HTTPOptions configure the fake Streamable HTTP MCP server.
type HTTPOptions struct {
	// SSE answers tools/list and tools/call as a text/event-stream (with
	// progress events); otherwise every answer is application/json.
	SSE bool
	// Session hands out an Mcp-Session-Id at initialize and requires it
	// on every later request (404 for an unknown one).
	Session bool
	// DropStream (with SSE) ends the first tools/call stream after its
	// first event; the answer is only sent on a GET with Last-Event-ID.
	DropStream bool
	// Bearer requires "Authorization: Bearer <Bearer>" (a static PAT).
	Bearer string
	// Validate requires a bearer token it accepts (AuthServer.Valid).
	Validate func(token string) bool
	// AuthServer is the issuer listed in the protected-resource metadata.
	AuthServer string
	// Scopes go into the metadata and the WWW-Authenticate header.
	Scopes []string
	// NoMetadataHeader leaves resource_metadata out of WWW-Authenticate,
	// so the client has to probe the well-known URL.
	NoMetadataHeader bool
	// Toolset "2" lists the changed tool set (ToolList).
	Toolset string
	// Login adds an identity tool get_me that answers {"login": Login}
	// (GitHub's MCP server has one).
	Login string
	// BearerScheme is the scheme Bearer must come with ("" = "Bearer";
	// Sentry's token mode uses "Sentry-Bearer").
	BearerScheme string
}

// Recorded is one request the fake got.
type Recorded struct {
	HTTPMethod string // POST, GET, DELETE
	RPC        string // JSON-RPC method ("" for answers / GET / DELETE)
	Header     http.Header
	Body       string
}

// HTTPServer is a fake remote MCP server at URL()+"/mcp".
type HTTPServer struct {
	*httptest.Server
	O HTTPOptions

	mu       sync.Mutex
	reqs     []Recorded
	sessions map[string]bool
	events   map[string][]string // Last-Event-ID → the events after it
	nextEv   int
	dropped  bool
	deletes  int
}

// NewHTTPServer starts the fake; it closes with the test.
func NewHTTPServer(t testing.TB, o HTTPOptions) *HTTPServer {
	s := &HTTPServer{O: o, sessions: map[string]bool{}, events: map[string][]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.serveMCP)
	// The read-only endpoint (GitHub /mcp/readonly, Linear …/readonly):
	// the same server with the read-only tools only.
	mux.HandleFunc("/mcp/readonly", s.serveMCP)
	prm := func(w http.ResponseWriter, r *http.Request) {
		if s.O.AuthServer == "" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"resource": s.MCPURL(), "authorization_servers": []string{s.O.AuthServer},
			"scopes_supported": s.O.Scopes, "bearer_methods_supported": []string{"header"}})
	}
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", prm)
	mux.HandleFunc("/.well-known/oauth-protected-resource", prm)
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp/readonly", prm)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// MCPURL is the server's MCP endpoint.
func (s *HTTPServer) MCPURL() string { return s.URL + "/mcp" }

// Requests returns what the fake got so far.
func (s *HTTPServer) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Recorded(nil), s.reqs...)
}

// Deletes counts DELETE requests (session ends).
func (s *HTTPServer) Deletes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deletes
}

// ExpireSessions forgets every session: the next request gets 404.
func (s *HTTPServer) ExpireSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = map[string]bool{}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// authorized checks the bearer token; on failure it answers 401.
func (s *HTTPServer) authorized(w http.ResponseWriter, r *http.Request) bool {
	if s.O.Bearer == "" && s.O.Validate == nil {
		return true
	}
	scheme := s.O.BearerScheme
	if scheme == "" {
		scheme = "Bearer"
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), scheme+" ")
	if ok && ((s.O.Bearer != "" && tok == s.O.Bearer) || (s.O.Validate != nil && s.O.Validate(tok))) {
		return true
	}
	h := `Bearer error="invalid_token"`
	if !s.O.NoMetadataHeader && s.O.AuthServer != "" {
		h += fmt.Sprintf(`, resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, s.URL)
	}
	if len(s.O.Scopes) > 0 {
		h += fmt.Sprintf(`, scope="%s"`, strings.Join(s.O.Scopes, " "))
	}
	w.Header().Set("WWW-Authenticate", h)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func (s *HTTPServer) record(r *http.Request, rpc, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, Recorded{HTTPMethod: r.Method, RPC: rpc, Header: r.Header.Clone(), Body: body})
}

func (s *HTTPServer) serveMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodDelete:
		s.record(r, "", "")
		s.mu.Lock()
		s.deletes++
		delete(s.sessions, r.Header.Get("Mcp-Session-Id"))
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodGet:
		s.record(r, "", "")
		if !s.authorized(w, r) {
			return
		}
		s.mu.Lock()
		evs, ok := s.events[r.Header.Get("Last-Event-ID")]
		s.mu.Unlock()
		if !ok {
			http.Error(w, "no stream", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range evs {
			_, _ = io.WriteString(w, e)
		}
		return
	case http.MethodPost:
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, _ := io.ReadAll(r.Body)
	var m extproc.Message
	if err := json.Unmarshal(b, &m); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.record(r, m.Method, string(b))
	if !s.authorized(w, r) {
		return
	}
	if s.O.Session && m.Method != "initialize" {
		sid := r.Header.Get("Mcp-Session-Id")
		s.mu.Lock()
		known := s.sessions[sid]
		s.mu.Unlock()
		switch {
		case sid == "":
			http.Error(w, "missing Mcp-Session-Id", http.StatusBadRequest)
			return
		case !known:
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
	}
	if !m.IsRequest() { // a notification or an answer
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(m.Params, &p)
		v := supported[0]
		for _, x := range supported {
			if x == p.ProtocolVersion {
				v = x
			}
		}
		if s.O.Session {
			sid := randomHex(16)
			s.mu.Lock()
			s.sessions[sid] = true
			s.mu.Unlock()
			w.Header().Set("Mcp-Session-Id", sid)
		}
		writeJSON(w, response(m.ID, map[string]any{"protocolVersion": v,
			"capabilities": map[string]any{"tools": map[string]any{"listChanged": true}},
			"serverInfo":   map[string]any{"name": "fake-mcp-http", "version": "1.0.0"}}, nil))
	case "ping":
		writeJSON(w, response(m.ID, map[string]any{}, nil))
	case "tools/list":
		res := map[string]any{"tools": s.tools(r.URL.Path == "/mcp/readonly")}
		if s.O.SSE {
			s.stream(w, []any{response(m.ID, res, nil)}, false)
			return
		}
		writeJSON(w, response(m.ID, res, nil))
	case "tools/call":
		s.call(w, r.Context(), m)
	default:
		writeJSON(w, response(m.ID, nil, &extproc.Error{Code: extproc.CodeMethodNotFound, Message: "method not found: " + m.Method}))
	}
}

// tools is the tool list: ToolList plus get_me with Login, only the
// read-only ones at /mcp/readonly.
func (s *HTTPServer) tools(readOnly bool) []map[string]any {
	list := ToolList(s.O.Toolset)
	if s.O.Login != "" {
		list = append(list, map[string]any{"name": "get_me", "description": "The signed-in user.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"annotations": map[string]any{"readOnlyHint": true}})
	}
	if !readOnly {
		return list
	}
	var out []map[string]any
	for _, t := range list {
		if a, _ := t["annotations"].(map[string]any); a != nil && a["readOnlyHint"] == true {
			out = append(out, t)
		}
	}
	return out
}

func response(id json.RawMessage, result any, rerr *extproc.Error) map[string]any {
	out := map[string]any{"jsonrpc": "2.0", "id": id}
	if rerr != nil {
		out["error"] = rerr
	} else {
		out["result"] = result
	}
	return out
}

// sseEvent renders one event with a fresh id.
func (s *HTTPServer) sseEvent(v any) (id, text string) {
	b, _ := json.Marshal(v)
	s.mu.Lock()
	s.nextEv++
	id = fmt.Sprintf("e%d", s.nextEv)
	s.mu.Unlock()
	return id, fmt.Sprintf("id: %s\ndata: %s\n\n", id, b)
}

// stream writes msgs as SSE events; drop keeps all but the first for a
// GET with Last-Event-ID.
func (s *HTTPServer) stream(w http.ResponseWriter, msgs []any, drop bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	f, _ := w.(http.Flusher)
	var kept []string
	firstID := ""
	for i, v := range msgs {
		id, e := s.sseEvent(v)
		if drop && i > 0 {
			kept = append(kept, e)
			continue
		}
		if i == 0 {
			firstID = id
		}
		_, _ = io.WriteString(w, e)
		if f != nil {
			f.Flush()
		}
	}
	if drop {
		s.mu.Lock()
		s.events[firstID] = kept
		s.mu.Unlock()
	}
}

// sseCaller lets the shared fake tools send progress on an SSE stream.
type sseCaller struct {
	notify func(method string, params any)
}

func (c sseCaller) Notify(method string, params any) error {
	if c.notify != nil {
		c.notify(method, params)
	}
	return nil
}

func (sseCaller) Call(context.Context, string, any) (json.RawMessage, error) {
	return nil, errors.New("server→client requests are not supported by the HTTP fake")
}

func (s *HTTPServer) call(w http.ResponseWriter, ctx context.Context, m extproc.Message) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
		Meta      struct {
			ProgressToken json.RawMessage `json:"progressToken"`
		} `json:"_meta"`
	}
	_ = json.Unmarshal(m.Params, &p)
	if p.Name == "get_me" && s.O.Login != "" {
		b, _ := json.Marshal(map[string]any{"login": s.O.Login, "name": "Test User"})
		writeJSON(w, response(m.ID, text(string(b)), nil))
		return
	}
	if !s.O.SSE {
		res, rerr := call(ctx, sseCaller{}, p.Name, p.Arguments, p.Meta.ProgressToken)
		if ctx.Err() != nil {
			return
		}
		writeJSON(w, response(m.ID, res, rerr))
		return
	}
	s.mu.Lock()
	drop := s.O.DropStream && !s.dropped
	if drop {
		s.dropped = true
	}
	s.mu.Unlock()
	if drop {
		res, rerr := call(ctx, sseCaller{}, p.Name, p.Arguments, nil)
		note := map[string]any{"jsonrpc": "2.0", "method": "notifications/message",
			"params": map[string]any{"level": "info", "data": "working"}}
		s.stream(w, []any{note, response(m.ID, res, rerr)}, true)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	f, _ := w.(http.Flusher)
	var mu sync.Mutex
	send := func(v any) {
		mu.Lock()
		defer mu.Unlock()
		_, e := s.sseEvent(v)
		_, _ = io.WriteString(w, e)
		if f != nil {
			f.Flush()
		}
	}
	notify := func(method string, params any) {
		send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	}
	res, rerr := call(ctx, sseCaller{notify: notify}, p.Name, p.Arguments, p.Meta.ProgressToken)
	if ctx.Err() != nil {
		return
	}
	send(response(m.ID, res, rerr))
}
