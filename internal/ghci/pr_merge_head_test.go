package ghci

import (
	"context"
	"strings"
	"testing"
)

// CMP-34: the babysitter merges only the commit it saw green.
func TestMergeHeadArgs(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr merge": {}}}
	if err := MergeHead(context.Background(), f.run, "/r", 7, MergeSquash, "abc123"); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(f.calls[0], " "), "pr merge 7 --squash --match-head-commit abc123"; got != want {
		t.Errorf("args %q want %q", got, want)
	}
	for _, bad := range []string{"", "-x", "abc 1", "zz!"} {
		f := &fakeGH{}
		if err := MergeHead(context.Background(), f.run, "/r", 7, MergeSquash, bad); err == nil || len(f.calls) != 0 {
			t.Errorf("sha %q: err %v calls %d", bad, err, len(f.calls))
		}
	}
	f = &fakeGH{}
	if err := MergeHead(context.Background(), f.run, "/r", 7, "fast", "abc123"); err == nil || len(f.calls) != 0 {
		t.Errorf("unknown method: err %v", err)
	}
}

// CMP-34: the CI snapshot carries the pull request's mergeable state, so
// the babysitter sees conflicts.
func TestFetchPRMergeable(t *testing.T) {
	pr := `{"number":12,"title":"x","state":"OPEN","isDraft":false,"url":"u","reviewDecision":"","mergeable":"CONFLICTING","statusCheckRollup":[]}`
	f := &fakeGH{answers: map[string]fakeAnswer{"run list": {out: "[]"}, "pr view": {out: pr}}}
	snap, err := Fetch(context.Background(), f.run, "/r", "feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if snap.PR == nil || snap.PR.Mergeable != "CONFLICTING" || !snap.PR.Conflicting() {
		t.Fatalf("pr %+v", snap.PR)
	}
	if !strings.Contains(strings.Join(f.calls[1], " "), "mergeable") {
		t.Errorf("pr view does not ask for mergeable: %v", f.calls[1])
	}
}

func TestConflictPrompt(t *testing.T) {
	p := ConflictPrompt("feat/x", "main", 12)
	for _, want := range []string{"feat/x", "#12", "origin/main", "conflict"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
}
