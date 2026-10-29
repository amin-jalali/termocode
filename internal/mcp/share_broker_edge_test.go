//go:build unix

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
)

// Spec (G-MCP): a broker that gets a damaged config or can not start
// the server says "error: …" and exits with 1.
func TestRunShareBrokerErrors(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "x.sock")
	var out bytes.Buffer
	if code := runShareBroker(sock, strings.NewReader("{oops"), &out); code != 1 || !strings.HasPrefix(out.String(), "error: bad config") {
		t.Fatalf("bad config: %d %q", code, out.String())
	}
	out.Reset()
	cfg, _ := json.Marshal(shareConfig{Command: "termocode-no-such-server"})
	if code := runShareBroker(sock, bytes.NewReader(cfg), &out); code != 1 || !strings.HasPrefix(out.String(), "error: ") || !strings.Contains(out.String(), "not found") {
		t.Fatalf("missing server: %d %q", code, out.String())
	}
	if _, err := startBroker(sock, shareConfig{}, session.CheckPeer); err == nil || !strings.Contains(err.Error(), "no command") {
		t.Fatalf("no command: err = %v", err)
	}
}

// Spec (G-MCP): the broker answers a session's ping itself and passes a
// server error back to the session that asked.
func TestShareBrokerPingAndServerErrors(t *testing.T) {
	_, sock := startTestBroker(t, fakeShareConfig(nil))
	c := dialTestShare(t, sock, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.call(ctx, "ping", nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	_, err := c.CallTool(ctx, "no_such_tool", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown tool: no_such_tool") {
		t.Fatalf("err = %v", err)
	}
	if res, err := c.CallTool(ctx, "echo", map[string]any{"text": "still fine"}); err != nil || res.Text() != "echo: still fine" {
		t.Fatalf("after the error: %q %v", res.Text(), err)
	}
}

// Spec (G-MCP): progress tokens that are not the broker's own, or are
// damaged, are left alone and never routed.
func TestShareProgressTokenEdgeCases(t *testing.T) {
	for _, in := range []string{
		`{"progressToken": broken`,
		`{"name":"x","note":"progressToken"}`,
		`{"_meta":{"other":1},"progressToken":1}`,
	} {
		if got := string(rewriteProgress(json.RawMessage(in), 3)); got != in {
			t.Errorf("rewriteProgress(%s) = %s", in, got)
		}
	}
	for _, in := range []string{
		`not json`,
		`{"progressToken":5}`,
		`{"progressToken":"cX:\"t\""}`,
		`{"progressToken":"c1:not-json"}`,
	} {
		if _, _, ok := routeProgress(json.RawMessage(in)); ok {
			t.Errorf("routeProgress(%s) routed", in)
		}
	}
}
