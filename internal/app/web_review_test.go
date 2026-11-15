//go:build unix

package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/now"
	"github.com/amin-jalali/termocode/internal/webproto"
)

const reviewViewText = `{"number":412,"title":"Add retry to webhook","body":"## Why\nWebhooks fail on short network drops. ENG-231\n\n## What\nRetry 3 times with backoff. Stop on 4xx.",
 "author":{"login":"sara"},"state":"OPEN","isDraft":false,"url":"https://github.com/o/r/pull/412","reviewDecision":"REVIEW_REQUIRED",
 "headRefName":"feat/retry-webhook","headRefOid":"3f2a1c9aa11","baseRefName":"main","mergeable":"MERGEABLE",
 "statusCheckRollup":[{"__typename":"CheckRun","name":"Unit tests","status":"COMPLETED","conclusion":"SUCCESS"},
                      {"__typename":"CheckRun","name":"e2e-linux","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://x/2"},
                      {"__typename":"CheckRun","name":"e2e-macos","status":"COMPLETED","conclusion":"SKIPPED"}],
 "files":[{"path":"webhook/send.go","additions":18,"deletions":2},{"path":"webhook/send_test.go","additions":20,"deletions":1},{"path":"config/app.yaml","additions":3,"deletions":0}],
 "reviews":[],"comments":[]}`

const reviewRunsText = `[{"databaseId":5,"workflowName":"e2e-linux","status":"completed","conclusion":"failure","headSha":"3f2a1c9aa11","url":"https://x/5"},
 {"databaseId":4,"workflowName":"Race test","status":"completed","conclusion":"success","headSha":"9b1e0d2bb22","url":"https://x/4"}]`

const reviewListText = `[{"number":412,"title":"Add retry to webhook","author":{"login":"sara"},"headRefName":"feat/retry-webhook","baseRefName":"main","url":"https://github.com/o/r/pull/412","isDraft":false,"state":"OPEN","reviewDecision":"REVIEW_REQUIRED","statusCheckRollup":[]},
 {"number":418,"title":"Cache user list","author":{"login":"sara"},"headRefName":"feat/cache","baseRefName":"main","url":"https://github.com/o/r/pull/418","isDraft":false,"state":"OPEN","reviewDecision":"","statusCheckRollup":[]},
 {"number":420,"title":"Bump deps","author":{"login":"dependabot[bot]"},"headRefName":"dep/x","baseRefName":"main","url":"https://github.com/o/r/pull/420","isDraft":false,"state":"OPEN","reviewDecision":"","statusCheckRollup":[]}]`

func webTestReview(t *testing.T) (*Model, *webConn, *prFakeGH) {
	t.Helper()
	f := &prFakeGH{answers: map[string]string{"pr view": reviewViewText, "run list": reviewRunsText, "pr list": reviewListText, "pr review": ""}}
	m, c := webTestPR(t, f)
	m.nowS = now.Open(filepath.Join(t.TempDir(), "now.json"))
	return m, c, f
}

// Spec (Review): review.get reads one pull request on the laptop and
// answers it in the review's fixed order: question (with its task) and
// solution from its text, the changes grouped with the main change
// first, one proof row per check bound to its commit (an older commit's
// run is stale, a skipped check did not run), the risk with its reasons
// and one short suggestion.
func TestReviewGet(t *testing.T) {
	m, c, _ := webTestReview(t)
	var r webproto.ReviewSnap
	webTestData(t, webTestCallBg(t, m, c, webproto.OpReviewGet, `{"number":412}`), &r)
	if r.Number != 412 || r.Head != "3f2a1c9aa11" || r.Author != "sara" || r.Title != "Add retry to webhook" {
		t.Fatalf("review %+v", r)
	}
	if r.Question != "Webhooks fail on short network drops. ENG-231" || r.Task != "ENG-231" || r.Solution != "Retry 3 times with backoff. Stop on 4xx." {
		t.Errorf("text %q %q %q", r.Question, r.Task, r.Solution)
	}
	if len(r.Groups) != 2 || !r.Groups[0].Main || r.Groups[0].Title != "send.go" || r.Groups[1].Title != "Rest: test, config" {
		t.Errorf("groups %+v", r.Groups)
	}
	var rows []string
	for _, p := range r.Proof {
		rows = append(rows, p.Name+":"+p.State+":"+p.SHA)
	}
	if got := strings.Join(rows, ","); got != "e2e-linux:fail:3f2a1c9,Race test:stale:9b1e0d2,Unit tests:ok:3f2a1c9,e2e-macos:notrun:3f2a1c9" {
		t.Errorf("proof %s", got)
	}
	if r.Risk.Level != "medium" || r.Suggestion.Text != "Approve after e2e-linux is green." {
		t.Errorf("risk %+v suggestion %+v", r.Risk, r.Suggestion)
	}
}

// Spec (Review): review.act approves (or comments on) only the commit
// the user saw: when new code came in it is refused, nothing is sent,
// and the answer says both commits.
func TestReviewActBoundToHead(t *testing.T) {
	m, c, f := webTestReview(t)
	if got := webTestErr(webTestCallBg(t, m, c, webproto.OpReviewAct, `{"number":412,"head":"3f2a1c9aa11","do":"approve"}`)); got != "" {
		t.Fatalf("approve: %q", got)
	}
	if !f.called("pr review 412 --approve") {
		t.Fatalf("no approve sent: %v", f.calls)
	}
	f.calls = nil
	res := webTestCallBg(t, m, c, webproto.OpReviewAct, `{"number":412,"head":"0000000aa","do":"approve"}`)
	if got := webTestErr(res); got != string(webproto.CodeConflict) {
		t.Fatalf("head moved: %v", res)
	}
	if f.called("pr review") {
		t.Fatalf("an approve was sent for code the user did not see: %v", f.calls)
	}
	if got := webTestErr(webTestCallBg(t, m, c, webproto.OpReviewAct, `{"number":412,"head":"3f2a1c9aa11","do":"comment","text":"Please add a test."}`)); got != "" {
		t.Fatalf("comment: %q", got)
	}
	if !f.called("pr review 412 --comment") {
		t.Fatalf("no comment sent: %v", f.calls)
	}
}

// Spec (Review, Now): review.list reads the pull requests that ask for
// the user's review; they join the inbox as your turn ("sara asked
// you", rule Reviews), a bot's pull request never takes a turn.
func TestReviewListFeedsNow(t *testing.T) {
	m, c, _ := webTestReview(t)
	var q webproto.ReviewQueue
	webTestData(t, webTestCallBg(t, m, c, webproto.OpReviewList, `{}`), &q)
	if len(q.Items) != 3 || q.Items[0].Number != 412 {
		t.Fatalf("queue %+v", q)
	}
	snap, err := webInboxSnapshot(m, "")
	if err != nil {
		t.Fatal(err)
	}
	s := snap.(webproto.InboxSnap)
	pr := webTestInboxItem(t, s, "pr:412")
	if pr.Kind != webproto.InboxPR || pr.Turn != now.TurnMe || pr.Signal != now.SigReview || pr.Rule != "reviews" || pr.Why != "sara asked you" || pr.Source != "github" {
		t.Errorf("pr item %+v", pr)
	}
	if bot := webTestInboxItem(t, s, "pr:420"); bot.Turn != now.TurnNone || bot.Rule != "nobots" {
		t.Errorf("bot item %+v", bot)
	}
}
