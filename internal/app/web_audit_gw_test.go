//go:build unix

package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// webAuditOps reads the gateway's audit log and returns its op events.
func webAuditOps(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, _ := os.ReadFile(path)
	var out []map[string]any
	for _, l := range strings.Split(string(b), "\n") {
		var e map[string]any
		if json.Unmarshal([]byte(l), &e) == nil && e["ev"] == "op" {
			out = append(out, e)
		}
	}
	return out
}

// Spec (3.5, WEB-F2): every web write op reaches the gateway's 0600
// JSONL audit with by=web:<device> and its result, through a real hub
// and gateway: an audited op one line per call (a refused one too, with
// its code); a counted op (term.interrupt) one line with the count when
// the browser leaves; a read op no line.
func TestWebWriteOpsInGatewayAudit(t *testing.T) {
	r := newWebPanelRig(t)
	c := r.browser(r.started())
	ctx := context.Background()
	for _, req := range []string{
		`{"t":"req","id":1,"op":"tasks.run","args":{"label":"no-such"}}`,
		`{"t":"req","id":2,"op":"chat.pin","args":{"on":true}}`,
		`{"t":"req","id":3,"op":"term.interrupt","args":{"buf":424242}}`,
		`{"t":"req","id":4,"op":"term.interrupt","args":{"buf":424242}}`,
	} {
		if err := c.Write(ctx, websocket.MessageText, []byte(req)); err != nil {
			t.Fatal(err)
		}
	}
	answers := make(chan struct{}, 8)
	go func() {
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			if strings.Contains(string(b), `"id":`) {
				answers <- struct{}{}
			}
		}
	}()
	n := 0
	r.pumpUntil("four answers", func() bool {
		for {
			select {
			case <-answers:
				n++
			default:
				return n >= 4
			}
		}
	})
	_ = c.Close(websocket.StatusNormalClosure, "")
	r.pumpUntil("the browser gone", func() bool { return r.hub.browsers() == 0 })
	var ops []map[string]any
	r.pumpUntil("two op lines", func() bool {
		ops = webAuditOps(t, r.audit)
		return len(ops) >= 2
	})
	byOp := map[string]map[string]any{}
	for _, e := range ops {
		byOp[e["op"].(string)] = e
		if by, _ := e["by"].(string); !strings.HasPrefix(by, "web:") || by == "web:" {
			t.Errorf("by %q in %v", by, e)
		}
	}
	if e := byOp["tasks.run"]; e == nil || e["ok"] != false || e["why"] != "not_found" || e["summary"] != "no-such" {
		t.Errorf("tasks.run: %v", e)
	}
	if e := byOp["term.interrupt"]; e == nil || e["why"] != "counted" || !strings.Contains(e["summary"].(string), "2 calls") {
		t.Errorf("term.interrupt: %v", e)
	}
	if byOp["chat.pin"] != nil {
		t.Error("a read op was audited")
	}
}
