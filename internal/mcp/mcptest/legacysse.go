package mcptest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// LegacySSEServer is a fake MCP server of the old HTTP+SSE transport
// (2024-11-05): GET /sse opens the stream, whose first event "endpoint"
// names where to POST; answers come back on the stream. A POST to /sse
// gets 405, as such servers answer a Streamable HTTP client.
type LegacySSEServer struct {
	*httptest.Server
	// Bearer requires "Authorization: Bearer <Bearer>" on GET and POST.
	Bearer string

	mu      sync.Mutex
	streams map[string]chan []byte
	n       int
	gets    int
}

// NewLegacySSEServer starts the fake; it closes with the test.
func NewLegacySSEServer(t testing.TB) *LegacySSEServer {
	s := &LegacySSEServer{streams: map[string]chan []byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", s.serveSSE)
	mux.HandleFunc("/messages", s.serveMessage)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(func() { s.CloseClientConnections(); s.Close() })
	return s
}

// SSEURL is the server's URL as users write it.
func (s *LegacySSEServer) SSEURL() string { return s.URL + "/sse" }

// Gets counts stream openings.
func (s *LegacySSEServer) Gets() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gets
}

func (s *LegacySSEServer) authorized(w http.ResponseWriter, r *http.Request) bool {
	if s.Bearer == "" || r.Header.Get("Authorization") == "Bearer "+s.Bearer {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func (s *LegacySSEServer) serveSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(w, r) {
		return
	}
	s.mu.Lock()
	s.n++
	s.gets++
	sid := fmt.Sprintf("s%d", s.n)
	ch := make(chan []byte, 16)
	s.streams[sid] = ch
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.streams, sid); s.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	f, _ := w.(http.Flusher)
	_, _ = fmt.Fprintf(w, "event: endpoint\ndata: /messages?session=%s\n\n", sid)
	if f != nil {
		f.Flush()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-ch:
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
			if f != nil {
				f.Flush()
			}
		}
	}
}

func (s *LegacySSEServer) serveMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(w, r) {
		return
	}
	s.mu.Lock()
	ch := s.streams[r.URL.Query().Get("session")]
	s.mu.Unlock()
	if ch == nil {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	b, _ := io.ReadAll(r.Body)
	var m extproc.Message
	if json.Unmarshal(b, &m) != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	if !m.IsRequest() {
		return
	}
	var out any
	switch m.Method {
	case "initialize":
		out = response(m.ID, map[string]any{"protocolVersion": "2025-03-26",
			"capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo":   map[string]any{"name": "fake-mcp-sse", "version": "0.9"}}, nil)
	case "ping":
		out = response(m.ID, map[string]any{}, nil)
	case "tools/list":
		out = response(m.ID, map[string]any{"tools": ToolList("")}, nil)
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(m.Params, &p)
		res, rerr := call(r.Context(), sseCaller{}, p.Name, p.Arguments, nil)
		out = response(m.ID, res, rerr)
	default:
		out = response(m.ID, nil, &extproc.Error{Code: extproc.CodeMethodNotFound, Message: "method not found: " + m.Method})
	}
	enc, _ := json.Marshal(out)
	ch <- enc
}
