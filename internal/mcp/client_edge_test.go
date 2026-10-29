package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedServer answers each JSON-RPC method with a fixed result (raw
// JSON) or, for tools/call, with an SSE body that may carry server
// messages first.
func scriptedServer(t *testing.T, results map[string]string, callSSE string) *rawServer {
	t.Helper()
	return newRawServer(t, func(w http.ResponseWriter, _ *http.Request, body string) {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal([]byte(body), &m)
		if len(m.ID) == 0 || m.Method == "" { // notifications and our replies
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if m.Method == "tools/call" && callSSE != "" {
			sseAnswer(w, callSSE+"data: {\"jsonrpc\":\"2.0\",\"id\":"+string(m.ID)+",\"result\":{\"content\":[]}}\n\n")
			return
		}
		res, ok := results[m.Method]
		if !ok {
			jsonAnswer(w, `{"jsonrpc":"2.0","id":`+string(m.ID)+`,"error":{"code":-32601,"message":"no `+m.Method+`"}}`)
			return
		}
		jsonAnswer(w, `{"jsonrpc":"2.0","id":`+string(m.ID)+`,"result":`+res+`}`)
	})
}

const goodInit = `{"protocolVersion":"2025-06-18","serverInfo":{"name":"s","version":"1"}}`

// Spec (MC-1): a handshake with a broken answer or another protocol
// version fails with the reason.
func TestClientHandshakeErrors(t *testing.T) {
	for init, want := range map[string]string{
		`"not an object"`:                  "initialize: bad answer",
		`{"protocolVersion":"1999-01-01"}`: `the server speaks MCP "1999-01-01"`,
	} {
		s := scriptedServer(t, map[string]string{"initialize": init}, "")
		_, err := StartHTTP(callCtx(t), Server{Name: "s", Type: TypeHTTP, URL: s.srv.URL}, HTTPOptions{}, Options{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", init, err, want)
		}
	}
	s := scriptedServer(t, map[string]string{}, "")
	if _, err := StartHTTP(callCtx(t), Server{Name: "s", Type: TypeHTTP, URL: s.srv.URL}, HTTPOptions{}, Options{}); err == nil || !strings.Contains(err.Error(), "initialize: no initialize") {
		t.Errorf("refused initialize: err = %v", err)
	}
}

// Spec (MC-1): broken tools/list and tools/call answers are errors that
// name the method.
func TestClientBadAnswers(t *testing.T) {
	s := scriptedServer(t, map[string]string{"initialize": goodInit, "tools/list": `"nope"`}, "")
	p, err := StartHTTP(callCtx(t), Server{Name: "s", Type: TypeHTTP, URL: s.srv.URL}, HTTPOptions{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := p.Client.ListTools(callCtx(t)); err == nil || !strings.Contains(err.Error(), "tools/list: bad answer") {
		t.Fatalf("list: err = %v", err)
	}
	if _, err := p.Client.CallToolTimeout(callCtx(t), "x", nil, 0); err == nil || !strings.Contains(err.Error(), "no tools/call") {
		t.Fatalf("call: err = %v", err)
	}

	s2 := scriptedServer(t, map[string]string{"initialize": goodInit, "tools/call": `["not","a","result"]`}, "")
	p2, err := StartHTTP(callCtx(t), Server{Name: "s", Type: TypeHTTP, URL: s2.srv.URL}, HTTPOptions{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Close()
	if _, err := p2.Client.CallTool(callCtx(t), "x", nil); err == nil || !strings.Contains(err.Error(), "tools/call: bad answer") {
		t.Fatalf("call: err = %v", err)
	}
}

// Spec (MC-1): during a call the client answers the server's ping,
// refuses sampling (a server never drives our model), logs messages
// whatever their data, and ignores progress it did not ask for.
func TestClientAnswersServerMessages(t *testing.T) {
	sse := "data: {\"jsonrpc\":\"2.0\",\"id\":\"p1\",\"method\":\"ping\"}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":\"s1\",\"method\":\"sampling/createMessage\",\"params\":{}}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"level\":\"info\",\"data\":{\"k\":1}}}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progressToken\":42,\"progress\":1}}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/unknown\"}\n\n"
	s := scriptedServer(t, map[string]string{"initialize": goodInit}, sse)
	var mu sync.Mutex
	var logs []string
	p, err := StartHTTP(callCtx(t), Server{Name: "s", Type: TypeHTTP, URL: s.srv.URL}, HTTPOptions{},
		Options{Log: func(l string) { mu.Lock(); logs = append(logs, l); mu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := p.Client.CallTool(callCtx(t), "x", nil); err != nil {
		t.Fatal(err)
	}
	replies := map[string]string{}
	deadline := time.Now().Add(5 * time.Second)
	for len(replies) < 2 && time.Now().Before(deadline) {
		s.mu.Lock()
		for _, b := range s.bodies {
			var m map[string]any
			if json.Unmarshal([]byte(b), &m) == nil && m["method"] == nil {
				if id, ok := m["id"].(string); ok {
					replies[id] = b
				}
			}
		}
		s.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(replies["p1"], `"result":{}`) {
		t.Errorf("ping reply %q", replies["p1"])
	}
	if !strings.Contains(replies["s1"], "-32601") || !strings.Contains(replies["s1"], "does not support sampling/createMessage") {
		t.Errorf("sampling reply %q", replies["s1"])
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(logs, "\n"), `info: {"k":1}`) {
		t.Errorf("logs %q", logs)
	}
}

// Spec (MC-3): every bad entry gets a problem that says what is wrong.
func TestParseEntryProblems(t *testing.T) {
	for raw, want := range map[string]string{
		`5`: "invalid entry",
		`{"type":"websocket","url":"https://x.example"}`: "unknown type websocket",
		`{"type":"stdio","url":"https://x.example"}`:     "stdio needs a command",
		`{"type":"http","command":"x"}`:                  "http needs a url",
		`{"command":"x","auth":{"gh":"amin"}}`:           "auth.gh needs an http server",
	} {
		if s := parseEntry("x", json.RawMessage(raw)); !strings.Contains(s.Problem, want) {
			t.Errorf("%s: problem %q, want %q", raw, s.Problem, want)
		}
	}
	s := parseEntry("ok", json.RawMessage(`{"command":"x"}`))
	if s.Problem != "" || string(s.Raw()) != `{"command":"x"}` {
		t.Fatalf("good entry: %q %s", s.Problem, s.Raw())
	}
}
