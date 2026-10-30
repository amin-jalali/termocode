//go:build unix

package webgw

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (3.5, WEB-F2): the hub sends a summary of every web write op on
// the control connection; the gateway writes it to its 0600 JSONL audit
// as an op event with by=web:<device>. Text passes the audit cleaner (a
// long token-like run is masked); a bad note is ignored.
func TestCtlAuditNoteLoggedAsOp(t *testing.T) {
	r := startedGateway(t, nil)
	tok := "sk-" + "abcdefghijklmnopqrstuvwxyz0123"
	r.ctl.send(t, `{"t":"ev","kind":"audit","data":{"conn":"c-9","device":"phone","op":"git.stage","ok":true,"summary":"a.txt `+tok+`"}}`)
	r.ctl.send(t, `{"t":"ev","kind":"audit","data":"nope"}`)
	r.ctl.send(t, `{"t":"ev","kind":"audit","data":{"conn":"c-9","device":"phone","op":"term.close","ok":false,"why":"cancelled_laptop","summary":"terminal 2"}}`)
	var lines []map[string]any
	waitFor(t, "two op lines", func() bool {
		lines = lines[:0]
		for _, l := range strings.Split(auditText(t, r.cfg.AuditPath), "\n") {
			var e map[string]any
			if json.Unmarshal([]byte(l), &e) == nil && e["ev"] == "op" {
				lines = append(lines, e)
			}
		}
		return len(lines) == 2
	})
	a, b := lines[0], lines[1]
	if a["op"] != "git.stage" || a["by"] != "web:phone" || a["conn"] != "c-9" || a["ok"] != true || a["summary"] != "a.txt [masked]" {
		t.Fatalf("first %v", a)
	}
	if b["op"] != "term.close" || b["ok"] != false || b["why"] != "cancelled_laptop" || b["by"] != "web:phone" {
		t.Fatalf("second %v", b)
	}
	if strings.Contains(auditText(t, r.cfg.AuditPath), tok) {
		t.Fatal("a token reached the audit log")
	}
}
