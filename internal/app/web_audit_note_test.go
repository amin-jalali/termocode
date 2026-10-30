package app

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestCtl adds a socketless control connection to the hub; its queue
// holds what the gateway would get.
func webTestCtl(t *testing.T, h *webHub) *webConn {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	c := &webConn{hub: h, nc: a, id: "ctl-t", role: webproto.RoleCtl, out: newWebQueue(), subs: map[string]*webSub{}}
	c.ctx, c.cancel = context.WithCancel(h.ctx)
	h.mu.Lock()
	h.all[a] = c
	h.mu.Unlock()
	return c
}

// webTestNotes are the audit notes queued on a control connection.
func webTestNotes(t *testing.T, ctl *webConn) []webproto.AuditNote {
	t.Helper()
	var out []webproto.AuditNote
	for _, f := range webTestNow(t, ctl) {
		if f["t"] == "ev" && f["kind"] == webproto.EvAudit {
			var n webproto.AuditNote
			webTestData(t, f, &n)
			out = append(out, n)
		}
	}
	return out
}

// Spec (3.5, T43): the audit summary of an op keeps names and counts
// only: chat text, a commit message and typed terminal text never reach
// the gateway's log, and a registered secret is redacted.
func TestNoSecretInAuditSummary(t *testing.T) {
	text := "my private words " + "sk-" + "test0123456789abcdef"
	for _, args := range []any{
		&webproto.ChatSendArgs{Text: text},
		&webproto.GitCommitArgs{Message: text},
		&webproto.TermSendArgs{Buf: 3, Text: text},
		&webproto.SetTextArgs{Buf: 3, Lines: []string{text}},
	} {
		if s := webAuditSummary(args); strings.Contains(s, "private") || strings.Contains(s, "sk-") {
			t.Errorf("%T: summary %q", args, s)
		}
	}
	if s := webAuditSummary(&webproto.ChatSendArgs{Text: text}); s != strconv.Itoa(len(text))+" bytes" {
		t.Errorf("chat summary %q", s)
	}
	if s := webAuditSummary(&webproto.PathsArgs{Paths: []string{"a.txt", "b/c.go"}}); s != "a.txt b/c.go" {
		t.Errorf("paths summary %q", s)
	}
}

// Spec (3.5, WEB-F2): a laptop card gives the gateway two notes: the
// ask ("laptop_card") and how it ended, with the asking connection and
// device.
func TestLaptopCardOutcomeInGatewayAudit(t *testing.T) {
	r := newWebFilesRig(t)
	r.m.output = newOutputStore()
	r.withNvim()
	webTestShell(t, r, "sh")
	gone := webTestShell(t, r, "sh")
	r.c.device = "phone"
	ctl := webTestCtl(t, r.h)

	var ref webproto.AskRef
	webResData(t, r.call(webproto.OpTermClose, &webproto.BufArgs{Buf: gone}), &ref)
	webTestLaptopSelect(t, r.m, webproto.ChoiceCancel)
	notes := webTestNotes(t, ctl)
	if len(notes) != 2 {
		t.Fatalf("notes %+v", notes)
	}
	want := []webproto.AuditNote{
		{Conn: "c-1", Device: "phone", Op: "term.close", OK: true, Why: "laptop_card", Summary: "buf " + strconv.Itoa(gone)},
		{Conn: "c-1", Device: "phone", Op: "term.close", OK: false, Why: "cancelled_laptop", Summary: "terminal 2"},
	}
	for i := range want {
		if notes[i] != want[i] {
			b, _ := json.Marshal(notes[i])
			t.Errorf("note %d: %s, want %+v", i, b, want[i])
		}
	}
}
