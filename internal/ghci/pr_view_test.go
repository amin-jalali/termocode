package ghci

import (
	"context"
	"strings"
	"testing"
)

const prViewJSON = `{"number":14,"title":"Add parser","body":"Adds a parser.\n\n- one\n- two","author":{"login":"ann"},
 "state":"OPEN","isDraft":false,"url":"https://github.com/o/r/pull/14","reviewDecision":"CHANGES_REQUESTED",
 "headRefName":"feat/parser","baseRefName":"main","mergeable":"MERGEABLE",
 "statusCheckRollup":[{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"d1"}],
 "files":[{"path":"parser.go","additions":40,"deletions":2},{"path":"docs/a.md","additions":3,"deletions":0}],
 "reviews":[{"author":{"login":"bob"},"state":"CHANGES_REQUESTED","body":"Please add tests","submittedAt":"2026-10-28T10:00:00Z"}],
 "comments":[{"author":{"login":"ann"},"body":"Added.","createdAt":"2026-10-28T11:00:00Z"},
             {"author":{"login":"cy"},"body":"first!","createdAt":"2026-10-27T09:00:00Z"}]}`

// ViewPR maps the body, files, checks, and merges reviews and comments
// oldest first.
func TestViewPR(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr view": {out: prViewJSON}}}
	d, err := ViewPR(context.Background(), f.run, "/repo", 14)
	if err != nil {
		t.Fatal(err)
	}
	if d.Number != 14 || d.Author != "ann" || d.State != "OPEN" || d.Branch != "feat/parser" || d.Base != "main" ||
		d.Mergeable != "MERGEABLE" || !strings.HasPrefix(d.Body, "Adds a parser.") {
		t.Errorf("detail %+v", d)
	}
	if len(d.Files) != 2 || d.Files[0] != (PRFile{Path: "parser.go", Additions: 40, Deletions: 2}) {
		t.Errorf("files %+v", d.Files)
	}
	if d.ChecksState() != StateFail {
		t.Errorf("checks %v", d.ChecksState())
	}
	if len(d.Reviews) != 3 || d.Reviews[0].Author != "cy" || d.Reviews[1].State != "CHANGES_REQUESTED" || d.Reviews[2].Body != "Added." {
		t.Errorf("reviews %+v", d.Reviews)
	}
	if got := strings.Join(f.calls[0], " "); !strings.HasPrefix(got, "pr view 14 --json number,title,body") {
		t.Errorf("args %q", got)
	}
}

func TestViewPRMissing(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr view": {errOut: "Could not resolve to a PullRequest with the number of 99.", err: errString("exit 1")}}}
	_, err := ViewPR(context.Background(), f.run, "/r", 99)
	if err == nil || !strings.Contains(err.Error(), "Could not resolve") {
		t.Errorf("err %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
