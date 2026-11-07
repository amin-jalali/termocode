package ghci

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const issueListJSON = `[
 {"number":41,"title":"Crash on start","author":{"login":"ann"},"url":"https://github.com/o/r/issues/41","state":"OPEN",
  "labels":[{"name":"bug"},{"name":"p1"}],"assignees":[{"login":"bob"}],"updatedAt":"2026-11-06T10:00:00Z"},
 {"number":40,"title":"Docs","author":{"login":"cy"},"url":"u40","state":"OPEN","labels":[],"assignees":[],"updatedAt":""}]`

// Spec (MOB-52): ListIssues asks gh with fixed args for the open issues
// and maps number, title, author, labels, assignees and the update time.
func TestListIssues(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"issue list": {out: issueListJSON}}}
	items, err := ListIssues(context.Background(), f.run, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items %+v", items)
	}
	a := items[0]
	if a.Number != 41 || a.Author != "ann" || a.State != "OPEN" || strings.Join(a.Labels, ",") != "bug,p1" || strings.Join(a.Assignees, ",") != "bob" || a.Updated == "" {
		t.Errorf("first %+v", a)
	}
	if len(items[1].Labels) != 0 {
		t.Errorf("second %+v", items[1])
	}
	if got := strings.Join(f.calls[0], " "); got != "issue list --state open --limit 50 --json "+issueListFields || f.dirs[0] != "/repo" {
		t.Errorf("args %q dir %q", got, f.dirs[0])
	}
}

func TestListIssuesErrors(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"issue list": {errOut: "To get started with GitHub CLI, please run:  gh auth login", err: errors.New("exit 4")}}}
	if _, err := ListIssues(context.Background(), f.run, "/r"); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err %v", err)
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"issue list": {out: "not json"}}}
	if _, err := ListIssues(context.Background(), f.run, "/r"); err == nil {
		t.Error("bad json accepted")
	}
}
