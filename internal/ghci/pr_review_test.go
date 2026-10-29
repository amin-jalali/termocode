package ghci

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReviewArgs(t *testing.T) {
	for _, c := range []struct {
		kind ReviewKind
		body string
		want string
	}{
		{ReviewApprove, "", "pr review 7 --approve"},
		{ReviewApprove, " LGTM ", "pr review 7 --approve --body LGTM"},
		{ReviewRequestChanges, "add tests", "pr review 7 --request-changes --body add tests"},
		{ReviewComment, "-b looks odd", "pr review 7 --comment --body -b looks odd"},
	} {
		f := &fakeGH{answers: map[string]fakeAnswer{"pr review": {}}}
		if err := Review(context.Background(), f.run, "/r", 7, c.kind, c.body); err != nil {
			t.Errorf("%v: %v", c.kind, err)
			continue
		}
		if got := strings.Join(f.calls[0], " "); got != c.want {
			t.Errorf("args %q want %q", got, c.want)
		}
	}
}

// Request changes and Comment without a message never call gh.
func TestReviewNeedsText(t *testing.T) {
	for _, k := range []ReviewKind{ReviewRequestChanges, ReviewComment} {
		f := &fakeGH{}
		if err := Review(context.Background(), f.run, "/r", 7, k, "  "); !errors.Is(err, ErrNeedsText) || len(f.calls) != 0 {
			t.Errorf("%v: err %v calls %d", k, err, len(f.calls))
		}
	}
	if err := Review(context.Background(), (&fakeGH{}).run, "/r", 7, "bogus", "x"); err == nil {
		t.Error("unknown kind accepted")
	}
}

func TestReviewError(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr review": {errOut: "GraphQL: Can not approve your own pull request", err: errString("exit 1")}}}
	err := Review(context.Background(), f.run, "/r", 7, ReviewApprove, "")
	if err == nil || !strings.Contains(err.Error(), "own pull request") {
		t.Errorf("err %v", err)
	}
}
