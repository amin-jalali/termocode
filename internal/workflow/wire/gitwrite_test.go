package wire

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

// WFX-13: git writes. pr commits the item's worktree on its branch and
// opens the pull request through Writer.PR (the app pushes and runs gh);
// commit commits it; both pass the gate card first, carry the run marker
// (PR body, commit message), keep an undo recipe in the ledger and are
// never sent twice. Never real gh: PR is a fake here.

type prRig struct {
	repo string
	env  run.ItemEnv
	w    *Writer
	asks []GateAsk
	sent []PRCall
	fail error
}

func newPRRig(t *testing.T, d GateDecision) *prRig {
	t.Helper()
	r := &prRig{repo: gitRepo(t)}
	wts := &Worktrees{Workspace: r.repo, RunID: "r1"}
	env, err := wts.Enter(context.Background(), run.EachCall{Step: eachStep(true, ""), Item: run.Item{Key: "LIN-1"}})
	if err != nil {
		t.Fatal(err)
	}
	r.env = env
	if err := os.WriteFile(filepath.Join(env.Worktree, "fix.txt"), []byte("fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	l, err := ledger.Open(root, "bugs", "r1")
	if err != nil {
		t.Fatal(err)
	}
	r.w = &Writer{Root: root, Workflow: "bugs", Hash: "h1", Ledger: l, Workspace: r.repo,
		Ask: func(_ context.Context, a GateAsk) (GateDecision, error) { r.asks = append(r.asks, a); return d, nil },
		PR: func(_ context.Context, p PRCall) (ledger.Result, error) {
			r.sent = append(r.sent, p)
			if r.fail != nil {
				return ledger.Result{}, r.fail
			}
			return ledger.Result{ExternalID: "https://github.example/o/r/pull/42"}, nil
		}}
	return r
}

func (r *prRig) call(to string) run.WriteCall {
	st := &workflow.Step{ID: to, Type: workflow.TypeWrite}
	spec := workflow.WriteSpec{To: to, Title: "fix: nil map", Body: "Fixes LIN-1.", Base: "main", Draft: true, Message: "fix: nil map"}
	return run.WriteCall{Step: st, Item: &run.Item{Key: "LIN-1"}, Env: r.env, Spec: spec, Marker: ledger.Marker("r1", to, "LIN-1")}
}

func (r *prRig) branchLog(t *testing.T) string {
	t.Helper()
	return gitT(t, r.repo, "log", "--format=%B", "main.."+r.env.Branch)
}

func TestPRWriteAsksCommitsAndOpens(t *testing.T) {
	r := newPRRig(t, GateAllowOnce)
	out, err := r.w.Write(context.Background(), r.call("pr"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.asks) != 1 || !strings.Contains(r.asks[0].Target, r.env.Branch) || !strings.Contains(r.asks[0].Target, "main") {
		t.Fatalf("gate card %+v", r.asks)
	}
	if !strings.Contains(r.asks[0].Undo, "close") {
		t.Errorf("the card has no undo: %q", r.asks[0].Undo)
	}
	if len(r.sent) != 1 {
		t.Fatalf("%d PRs", len(r.sent))
	}
	p := r.sent[0]
	if p.Branch != r.env.Branch || p.Base != "main" || !p.Draft || p.Title != "fix: nil map" || p.Dir != r.env.Worktree {
		t.Errorf("pr %+v", p)
	}
	if !strings.HasPrefix(p.Body, "Fixes LIN-1.") || !strings.Contains(p.Body, "<!-- termocode-run:r1/pr/LIN-1 -->") {
		t.Errorf("body %q", p.Body)
	}
	// The agent's edit is committed on the item's branch before the push.
	if log := r.branchLog(t); !strings.Contains(log, "fix: nil map") {
		t.Errorf("branch log %q", log)
	}
	if out.URL != "https://github.example/o/r/pull/42" {
		t.Errorf("url %q", out.URL)
	}
	if n, _ := out.JSON.(map[string]any)["number"].(float64); n != 42 {
		t.Errorf("number in %v", out.JSON)
	}
	e, ok := r.w.Ledger.Get("pr", "LIN-1")
	if !ok || e.State != ledger.Done || e.ExternalID != out.URL || e.Undo == nil || !strings.Contains(e.Undo.Title, r.env.Branch) {
		t.Errorf("ledger %+v", e)
	}
	// Again (a resumed run): done in the ledger, not sent twice.
	if _, err := r.w.Write(context.Background(), r.call("pr")); err != nil || len(r.sent) != 1 {
		t.Errorf("again: %v, %d PRs", err, len(r.sent))
	}
}

func TestPRWriteDeniedSendsAndCommitsNothing(t *testing.T) {
	r := newPRRig(t, GateDeny)
	if _, err := r.w.Write(context.Background(), r.call("pr")); !errors.Is(err, run.ErrDenied) {
		t.Fatalf("err %v", err)
	}
	if len(r.sent) != 0 {
		t.Error("a denied PR was sent")
	}
	if log := r.branchLog(t); strings.TrimSpace(log) != "" {
		t.Errorf("a denied PR committed: %q", log)
	}
}

func TestPRWriteResumeFindsItByMarker(t *testing.T) {
	r := newPRRig(t, GateAllowOnce)
	r.fail = errors.New("connection reset")
	if _, err := r.w.Write(context.Background(), r.call("pr")); err == nil {
		t.Fatal("no error")
	}
	var looked string
	r.w.Find = func(_ context.Context, spec workflow.WriteSpec, marker string) (ledger.Result, bool, error) {
		looked = spec.To + " " + marker
		return ledger.Result{ExternalID: "https://github.example/o/r/pull/43"}, true, nil
	}
	r.fail = nil
	out, err := r.w.Write(context.Background(), r.call("pr"))
	if err != nil || out.URL != "https://github.example/o/r/pull/43" {
		t.Fatalf("%+v %v", out, err)
	}
	if len(r.sent) != 1 || looked != "pr termocode-run:r1/pr/LIN-1" {
		t.Errorf("sent %d, looked %q", len(r.sent), looked)
	}
}

func TestPRWriteNeedsAWorktree(t *testing.T) {
	r := newPRRig(t, GateAllowOnce)
	c := r.call("pr")
	c.Env = run.ItemEnv{}
	if _, err := r.w.Write(context.Background(), c); err == nil || !strings.Contains(err.Error(), "worktree") {
		t.Errorf("err %v", err)
	}
	if len(r.sent) != 0 {
		t.Error("sent without a worktree")
	}
}

func TestCommitWrite(t *testing.T) {
	r := newPRRig(t, GateAllowOnce)
	out, err := r.w.Write(context.Background(), r.call("commit"))
	if err != nil {
		t.Fatal(err)
	}
	log := r.branchLog(t)
	if !strings.Contains(log, "fix: nil map") || !strings.Contains(log, "termocode-run:r1/commit/LIN-1") {
		t.Errorf("commit message %q", log)
	}
	sha := strings.TrimSpace(gitT(t, r.repo, "rev-parse", r.env.Branch))
	e, _ := r.w.Ledger.Get("commit", "LIN-1")
	if e.ExternalID != sha || out.Text == "" || e.Undo == nil {
		t.Errorf("ledger %+v out %+v (sha %s)", e, out, sha)
	}
	if len(r.sent) != 0 {
		t.Error("a commit opened a PR")
	}
	// A resumed commit finds its marker in the branch: not committed twice.
	if err := r.w.Ledger.Forget("commit", "LIN-1"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := findCommit(r.repo, r.env, ledger.Marker("r1", "commit", "LIN-1")); err != nil || !found {
		t.Errorf("findCommit: %v %v", found, err)
	}
}

func TestPRWriteBodyFileIsFilled(t *testing.T) {
	r := newPRRig(t, GateAllowOnce)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "body.md"), []byte("Body for {{item.key}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.w.Dir = dir
	c := r.call("pr")
	c.Spec.Body, c.Spec.BodyFile = "", "body.md"
	c.Render = func(s string) (string, error) { return strings.ReplaceAll(s, "{{item.key}}", "LIN-1"), nil }
	if _, err := r.w.Write(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.sent[0].Body, "Body for LIN-1") {
		t.Errorf("body %q", r.sent[0].Body)
	}
	c.Spec.BodyFile = "../../etc/passwd"
	c.Step = &workflow.Step{ID: "pr2", Type: workflow.TypeWrite}
	if _, err := r.w.Write(context.Background(), c); err == nil {
		t.Error("a body file outside the workflow folder")
	}
}
