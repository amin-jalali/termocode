package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// API-17: the WebSocket page lists the session's messages and its keys.
func TestHTTPWSPage(t *testing.T) {
	unicodeIcons(t)
	ws := &httpWSState{
		run:   &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "WEBSOCKET", URL: "wss://x.io/chat?token=sekrit-123"}}, label: "chat"},
		saved: []string{`{"op":"ping"}`},
		state: httpWSOpen,
		log: []httpclient.WSMessage{
			{Dir: httpclient.WSOut, Text: `{"op":"ping"}`, At: time.Unix(0, 0)},
			{Dir: httpclient.WSIn, Text: "line1\nline2", At: time.Unix(1, 0)},
			{Dir: httpclient.WSIn, Binary: true, Text: "\x00\x01", At: time.Unix(2, 0)},
		},
	}
	p := buildHTTPWSPage(ws)
	text := strings.Join(p.lines, "\n")
	for _, want := range []string{"connected · 1 sent · 2 received", `→ {"op":"ping"}`, "← line1", "  line2", "(binary, 2 B)"} {
		if !strings.Contains(text, want) {
			t.Errorf("page lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "sekrit-123") {
		t.Fatalf("page leaks the token:\n%s", text)
	}
	hints := map[string]bool{}
	for _, h := range p.head["hints"].([][]string) {
		hints[h[1]] = true
	}
	if !hints["send saved"] || !hints["type"] || !hints["disconnect"] {
		t.Fatalf("hints = %v", p.head["hints"])
	}
	ws.state = httpWSClosed
	ws.err = "connection reset"
	if !strings.Contains(strings.Join(buildHTTPWSPage(ws).lines, "\n"), "closed · connection reset") {
		t.Fatal("closed state not shown")
	}
}

// API-17: the AI tool's text shows the exchange, redacted.
func TestHTTPWSToolText(t *testing.T) {
	out := httpWSToolText("wss://x.io/s?token=sekrit-123", []httpclient.WSMessage{
		{Dir: httpclient.WSOut, Text: "hi"}, {Dir: httpclient.WSIn, Text: "yo"},
	})
	if !strings.Contains(out, "> hi") || !strings.Contains(out, "< yo") || strings.Contains(out, "sekrit-123") {
		t.Fatalf("text:\n%s", out)
	}
}
