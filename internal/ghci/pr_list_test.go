package ghci

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const prListJSON = `[
 {"number":14,"title":"Add parser","author":{"login":"ann"},"headRefName":"feat/parser","baseRefName":"main","url":"https://github.com/o/r/pull/14",
  "isDraft":false,"reviewDecision":"APPROVED","statusCheckRollup":[
   {"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"SUCCESS"}]},
 {"number":13,"title":"WIP cache","author":{"login":"bob"},"headRefName":"wip/cache","baseRefName":"main","url":"u13",
  "isDraft":true,"reviewDecision":"","statusCheckRollup":[
   {"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"SUCCESS"},
   {"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"FAILURE"}]},
 {"number":9,"title":"Docs","author":{"login":"cy"},"headRefName":"docs","baseRefName":"main","url":"u9",
  "isDraft":false,"reviewDecision":"REVIEW_REQUIRED","statusCheckRollup":[]}]`

// ListPRs asks gh with fixed args and maps number, author, branch, draft,
// review state and the sum of the checks.
func TestListPRs(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr list": {out: prListJSON}}}
	items, err := ListPRs(context.Background(), f.run, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items %+v", items)
	}
	a := items[0]
	if a.Number != 14 || a.Author != "ann" || a.Branch != "feat/parser" || a.Base != "main" || a.Review != "APPROVED" || a.Draft {
		t.Errorf("first %+v", a)
	}
	if !items[1].Draft || items[1].ChecksState() != StateFail || items[0].ChecksState() != StatePass || items[2].ChecksState() != StateNone {
		t.Errorf("draft/checks %+v", items)
	}
	got := strings.Join(f.calls[0], " ")
	if !strings.HasPrefix(got, "pr list --state open --limit 50 --json ") || f.dirs[0] != "/repo" {
		t.Errorf("args %q dir %q", got, f.dirs[0])
	}
}

func TestListPRsErrors(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr list": {errOut: "To get started with GitHub CLI, please run:  gh auth login", err: errors.New("exit 4")}}}
	if _, err := ListPRs(context.Background(), f.run, "/r"); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err %v", err)
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"pr list": {out: "not json"}}}
	if _, err := ListPRs(context.Background(), f.run, "/r"); err == nil {
		t.Error("bad json accepted")
	}
}

// A filter picks the state and adds author, assignee, search and draft;
// values go in one argument with "=", so a value never reads as a flag.
func TestListPRsFilter(t *testing.T) {
	for _, c := range []struct {
		f    PRFilter
		want string
	}{
		{PRFilter{}, "pr list --state open --limit 50 --json " + prListFields},
		{PRFilter{State: "merged"}, "pr list --state merged --limit 50 --json " + prListFields},
		{PRFilter{Author: "@me"}, "pr list --state open --limit 50 --json " + prListFields + " --author=@me"},
		{PRFilter{Assignee: "@me", Draft: true}, "pr list --state open --limit 50 --json " + prListFields + " --assignee=@me --draft"},
		{PRFilter{State: "all", Search: "-label:wip review-requested:@me"}, "pr list --state all --limit 50 --json " + prListFields + " --search=-label:wip review-requested:@me"},
	} {
		f := &fakeGH{answers: map[string]fakeAnswer{"pr list": {out: `[{"number":3,"state":"MERGED"}]`}}}
		items, err := ListPRsFilter(context.Background(), f.run, "/r", c.f)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(f.calls[0], " "); got != c.want {
			t.Errorf("%+v:\n got %q\nwant %q", c.f, got, c.want)
		}
		if items[0].State != "MERGED" {
			t.Errorf("state %+v", items[0])
		}
	}
	if _, err := ListPRsFilter(context.Background(), (&fakeGH{}).run, "/r", PRFilter{State: "bogus"}); err == nil {
		t.Error("unknown state accepted")
	}
}

// MyPRs reads the pull requests that ask for my review and my own.
func TestMyPRs(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr list": {out: prListJSON}}}
	review, mine, err := MyPRs(context.Background(), f.run, "/r")
	if err != nil || len(review) != 3 || len(mine) != 3 || len(f.calls) != 2 {
		t.Fatalf("review %d mine %d calls %d err %v", len(review), len(mine), len(f.calls), err)
	}
	if !strings.HasSuffix(strings.Join(f.calls[0], " "), "--search=review-requested:@me") ||
		!strings.HasSuffix(strings.Join(f.calls[1], " "), "--author=@me") {
		t.Errorf("calls %v", f.calls)
	}
}
