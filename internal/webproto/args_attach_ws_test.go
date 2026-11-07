package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (MOB-55): an attach may name its project (ws: a workspace key,
// "" = the hub's own); never a path. An issue or pr attach names a
// number ("12", "#12", or GitLab's "!12"), nothing else.
func TestChatSendAttachProjectRules(t *testing.T) {
	send := func(attach string) string { return `{"text":"look","attach":[` + attach + `]}` }
	good := []string{
		`{"kind":"file","path":"a.go","ws":"0123456789abcdef"}`,
		`{"kind":"ptask","path":"TASKS.md","id":"MOB-55","ws":"0123456789abcdef"}`,
		`{"kind":"issue","id":"12"}`,
		`{"kind":"issue","id":"#12","ws":"fedcba9876543210"}`,
		`{"kind":"pr","id":"7"}`,
		`{"kind":"pr","id":"!7"}`,
		`{"kind":"pr","id":"999999999"}`,
		`{"kind":"project","id":"0a1b2c3d4e5f"}`,
	}
	for _, a := range good {
		if _, err := DecodeArgs(OpChatSend, json.RawMessage(send(a))); err != nil {
			t.Errorf("%s refused: %v", a, err)
		}
	}
	bad := []string{
		`{"kind":"file","path":"a.go","ws":"../shop"}`,
		`{"kind":"file","path":"a.go","ws":"/home/u/shop"}`,
		`{"kind":"file","path":"a.go","ws":"0123456789ABCDEF"}`,
		`{"kind":"file","path":"a.go","ws":"0123456789abcde"}`,
		`{"kind":"file","path":"a.go","ws":"0123456789abcdef0"}`,
		`{"kind":"project","id":"0a1b2c3d4e5f","ws":"g123456789abcdef"}`,
		`{"kind":"issue","id":"0"}`,
		`{"kind":"issue","id":"#"}`,
		`{"kind":"issue","id":"12a"}`,
		`{"kind":"issue","id":"!12"}`,
		`{"kind":"issue","id":"-3"}`,
		`{"kind":"pr","id":"1234567890"}`,
		`{"kind":"pr","id":"#!7"}`,
		`{"kind":"pr","id":"07"}`,
	}
	for _, a := range bad {
		if _, err := DecodeArgs(OpChatSend, json.RawMessage(send(a))); err == nil {
			t.Errorf("%s accepted", a)
		}
	}
}

func TestAttachNumber(t *testing.T) {
	cases := []struct {
		kind, id string
		n        int
		ok       bool
	}{
		{AttachIssue, "12", 12, true},
		{AttachIssue, "#12", 12, true},
		{AttachIssue, "!12", 0, false}, // a merge request is a pr
		{AttachPR, "!12", 12, true},
		{AttachPR, "#5", 5, true},
		{AttachPR, "", 0, false},
		{AttachPR, "05", 0, false},
		{AttachPR, "1 2", 0, false},
	}
	for _, c := range cases {
		n, ok := AttachNumber(c.kind, c.id)
		if n != c.n || ok != c.ok {
			t.Errorf("AttachNumber(%s, %q) = %d %v, want %d %v", c.kind, c.id, n, ok, c.n, c.ok)
		}
	}
	if !AttachIsMR("!3") || AttachIsMR("3") {
		t.Error("AttachIsMR")
	}
}

func TestAttachWSDefaultIsOmitted(t *testing.T) {
	b, err := json.Marshal(Attach{Kind: AttachIssue, ID: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"kind":"issue","id":"3"}` {
		t.Errorf("home attach = %s", b)
	}
}

func FuzzAttachWS(f *testing.F) {
	for _, s := range []string{"", "0123456789abcdef", "../x", "~/shop", "/etc", "0123456789abcde\x00"} {
		f.Add(s, "12")
	}
	f.Fuzz(func(t *testing.T, ws, id string) {
		a := Attach{Kind: AttachIssue, ID: id, WS: ws}
		if a.check() == nil {
			if !validWSField(ws) {
				t.Fatalf("ws %q accepted", ws)
			}
			if n, ok := AttachNumber(AttachIssue, id); !ok || n <= 0 {
				t.Fatalf("issue id %q accepted", id)
			}
		}
	})
}
