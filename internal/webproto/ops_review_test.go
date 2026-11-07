package webproto

import (
	"strings"
	"testing"
)

// Spec (MOB-14): sending review notes is a chat send: controller only,
// audited, never a laptop card; the args are a worktree id, an optional
// chat id and 1..MaxReviewNotes notes, each a repository path, a line
// (1-based), a side (new|old) and a short, printable text.
func TestAIWorktreeNotesOp(t *testing.T) {
	p, ok := Lookup(OpAIWorktreeNotes)
	if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes || p.Phase != PhaseV2 {
		t.Fatalf("policy %+v", p)
	}
	if Allowed(RoleViewer, OpAIWorktreeNotes) {
		t.Error("a viewer may send notes")
	}
	note := func(s string) string { return `{"id":"r1-left","notes":[` + s + `]}` }
	good := []string{
		note(`{"path":"src/a.go","line":12,"side":"new","text":"Check the error."}`),
		`{"id":"r1-left","chat_id":"c-1","notes":[{"path":"a.go","line":1,"side":"old","text":"why\nremoved?"}]}`,
		note(strings.TrimSuffix(strings.Repeat(`{"path":"a.go","line":3,"side":"new","text":"x"},`, MaxReviewNotes), ",")),
		note(`{"path":"a.go","line":3,"side":"new","text":"` + strings.Repeat("y", MaxReviewNote) + `"}`),
	}
	bad := []string{
		`{"id":"r1-left","notes":[]}`,
		`{"id":"-x","notes":[{"path":"a.go","line":1,"side":"new","text":"x"}]}`,
		`{"id":"../x","notes":[{"path":"a.go","line":1,"side":"new","text":"x"}]}`,
		note(`{"path":"a.go","line":0,"side":"new","text":"x"}`),
		note(`{"path":"a.go","line":1000001,"side":"new","text":"x"}`),
		note(`{"path":"a.go","line":1,"side":"both","text":"x"}`),
		note(`{"path":"a.go","line":1,"side":"new","text":"  "}`),
		note(`{"path":"a.go","line":1,"side":"new","text":"a\u001bb"}`),
		note(`{"path":"a.go","line":1,"side":"new","text":"` + strings.Repeat("y", MaxReviewNote+1) + `"}`),
		note(`{"path":"../etc/passwd","line":1,"side":"new","text":"x"}`),
		note(`{"path":"/etc/passwd","line":1,"side":"new","text":"x"}`),
		note(`{"path":"src/","line":1,"side":"new","text":"x"}`),
		note(`{"path":"a.go","line":1,"side":"new","text":"x","extra":1}`),
		note(strings.TrimSuffix(strings.Repeat(`{"path":"a.go","line":3,"side":"new","text":"x"},`, MaxReviewNotes+1), ",")),
	}
	for _, b := range good {
		if _, err := DecodeArgs(OpAIWorktreeNotes, []byte(b)); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
	for _, b := range bad {
		if _, err := DecodeArgs(OpAIWorktreeNotes, []byte(b)); err == nil {
			t.Errorf("%s accepted", b)
		}
	}
}
