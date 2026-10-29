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
