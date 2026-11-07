package trigger

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow"
)

// fakeGH answers gh calls from a map of "first two args" → JSON.
type fakeGH struct {
	out   map[string]string
	calls [][]string
	fail  error
}

func (f *fakeGH) run(_ context.Context, _ string, args ...string) (string, string, error) {
	f.calls = append(f.calls, args)
	if f.fail != nil {
		return "", "gh: not logged in\nmore", f.fail
	}
	return f.out[args[0]+" "+args[1]], "", nil
}

// The first poll records and never fires; a new issue after it fires once.
func TestGitHubIssueOpenedBaseline(t *testing.T) {
	f := &fakeGH{out: map[string]string{"issue list": `[{"number":1,"title":"old","labels":[]}]`}}
	g := &GitHub{Run: f.run, State: MemState()}
	tr := workflow.Trigger{GitHub: KindIssueOpened}
	evs, err := g.Poll(context.Background(), "wf", 0, tr)
	if err != nil || len(evs) != 0 {
		t.Fatalf("first poll %v %v", evs, err)
	}
	f.out["issue list"] = `[{"number":2,"title":"new bug\nline","url":"u","author":{"login":"ann"},"labels":[{"name":"bug"}]},{"number":1,"title":"old"}]`
	evs, err = g.Poll(context.Background(), "wf", 0, tr)
	if err != nil || len(evs) != 1 {
		t.Fatalf("second poll %v %v", evs, err)
	}
	e := evs[0]
	if e.Key != "issue:2" || e.Number != 2 || e.Author != "ann" || e.Workflow != "wf" || e.Kind != KindIssueOpened {
		t.Errorf("event %+v", e)
	}
	if v := e.Vars(); v["trigger_number"] != "2" || v["trigger_labels"] != "bug" || v["trigger_kind"] != "issue_opened" {
		t.Errorf("vars %v", v)
	}
	if s := e.Summary(); strings.Contains(s, "\n") || !strings.Contains(s, "new issue #2") {
		t.Errorf("summary %q", s)
	}
	if evs, _ := g.Poll(context.Background(), "wf", 0, tr); len(evs) != 0 {
		t.Errorf("third poll fired again: %v", evs)
	}
}

// issue_labeled fires when a known issue gets a wanted label, or a new
// issue comes with one; an issue that had the label stays quiet.
func TestGitHubIssueLabeled(t *testing.T) {
	f := &fakeGH{out: map[string]string{"issue list": `[{"number":1,"labels":[{"name":"agent"}]},{"number":2,"labels":[]}]`}}
	g := &GitHub{Run: f.run, State: MemState()}
	tr := workflow.Trigger{GitHub: KindIssueLabeled, Labels: []string{"Agent"}}
	if evs, _ := g.Poll(context.Background(), "wf", 0, tr); len(evs) != 0 {
		t.Fatal(evs)
	}
	f.out["issue list"] = `[{"number":1,"labels":[{"name":"agent"}]},{"number":2,"labels":[{"name":"agent"}]},{"number":3,"labels":[{"name":"agent"}]},{"number":4,"labels":[{"name":"x"}]}]`
	evs, err := g.Poll(context.Background(), "wf", 0, tr)
	if err != nil {
		t.Fatal(err)
	}
	var nums []int
	for _, e := range evs {
		nums = append(nums, e.Number)
	}
	if len(nums) != 2 || nums[0] != 2 || nums[1] != 3 {
		t.Errorf("fired for %v, want [2 3]", nums)
	}
}

// A changed trigger is a new id: it starts with a quiet poll again.
func TestGitHubTriggerChangeRebaselines(t *testing.T) {
	a := workflow.Trigger{GitHub: KindPROpened}
	b := workflow.Trigger{GitHub: KindPROpened, Labels: []string{"x"}}
	if ID("wf", 0, a) == ID("wf", 0, b) || ID("wf", 0, a) == ID("wf", 1, a) {
		t.Error("ids collide")
	}
}

func TestGitHubPullsMentionsRuns(t *testing.T) {
	f := &fakeGH{out: map[string]string{"pr list": `[]`, "issue list": `[]`, "run list": `[]`}}
	g := &GitHub{Run: f.run, State: MemState()}
	ctx := context.Background()
	pr := workflow.Trigger{GitHub: KindPROpened, Repo: "o/r"}
	mention := workflow.Trigger{GitHub: KindMention, Text: "@termocode"}
	ci := workflow.Trigger{GitHub: KindCIFailed, Branch: "main"}
	for i, tr := range []workflow.Trigger{pr, mention, ci} {
		if _, err := g.Poll(ctx, "wf", i, tr); err != nil {
			t.Fatal(err)
		}
	}
	f.out["pr list"] = `[{"number":7,"title":"Add x","headRefName":"feat/x","body":"please @TermoCode fix","comments":[{"id":"c1","body":"hey @termocode","url":"cu","author":{"login":"bob"}},{"id":"c2","body":"nothing"}]}]`
	f.out["run list"] = `[{"databaseId":99,"displayTitle":"fix","headBranch":"main","status":"completed","conclusion":"failure","url":"ru","workflowName":"ci"},{"databaseId":98,"conclusion":"success"}]`
	evs, _ := g.Poll(ctx, "wf", 0, pr)
	if len(evs) != 1 || evs[0].Branch != "feat/x" || evs[0].Key != "pr:7" {
		t.Errorf("pr %+v", evs)
	}
	evs, _ = g.Poll(ctx, "wf", 1, mention)
	if len(evs) != 2 || evs[0].Key != "pr:7:body" || evs[1].Key != "pr:7:comment:c1" || evs[1].Author != "bob" || evs[1].URL != "cu" {
		t.Errorf("mention %+v", evs)
	}
	evs, _ = g.Poll(ctx, "wf", 2, ci)
	if len(evs) != 1 || evs[0].Key != "run:99" || evs[0].Title != "ci: fix" {
		t.Errorf("ci %+v", evs)
	}
	var sawRepo, sawBranch bool
	for _, c := range f.calls {
		j := strings.Join(c, " ")
		sawRepo = sawRepo || strings.Contains(j, "--repo o/r")
		sawBranch = sawBranch || strings.Contains(j, "run list") && strings.Contains(j, "--branch main")
	}
	if !sawRepo || !sawBranch {
		t.Errorf("calls %v", f.calls)
	}
}

// gh's own error line is shown, cut to one line; no event comes.
func TestGitHubPollError(t *testing.T) {
	g := &GitHub{Run: (&fakeGH{fail: errors.New("exit 1")}).run}
	_, err := g.Poll(context.Background(), "wf", 0, workflow.Trigger{GitHub: KindPROpened})
	if err == nil || !strings.Contains(err.Error(), "not logged in") || strings.Contains(err.Error(), "more") {
		t.Errorf("err %v", err)
	}
	if _, err := (&GitHub{}).Poll(context.Background(), "wf", 0, workflow.Trigger{GitHub: KindPROpened}); err == nil {
		t.Error("no gh: no error")
	}
}

// The state survives a restart: the second process is not in its first
// poll any more.
func TestStateSaved(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenState(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGH{out: map[string]string{"pr list": `[{"number":1}]`}}
	tr := workflow.Trigger{GitHub: KindPROpened}
	if _, err := (&GitHub{Run: f.run, State: st}).Poll(context.Background(), "wf", 0, tr); err != nil {
		t.Fatal(err)
	}
	st2, err := OpenState(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.out["pr list"] = `[{"number":1},{"number":2}]`
	evs, err := (&GitHub{Run: f.run, State: st2}).Poll(context.Background(), "wf", 0, tr)
	if err != nil || len(evs) != 1 || evs[0].Number != 2 {
		t.Errorf("after restart %v %v", evs, err)
	}
	st2.Keep(map[string]bool{})
	if len(st2.data) != 0 {
		t.Error("Keep did not drop")
	}
}
