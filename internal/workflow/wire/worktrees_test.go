package wire

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

// gitRepo makes a temp repository with one commit on main.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.test"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitT(t, dir, args...)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func eachStep(worktree bool, cleanup string) *workflow.Step {
	return &workflow.Step{ID: "fix", Type: workflow.TypeEach, Each: &workflow.EachSpec{Worktree: worktree, Cleanup: cleanup}}
}

// WFX-10: an each with worktree: true gives every item its own agent
// worktree from the base branch; cleanup "keep" (default) keeps it for
// review, "discard" removes it after a good item only (the branch, and
// so the work, stays in git); a failed or cancelled item is kept.
func TestWorktreesEnterAndLeave(t *testing.T) {
	repo := gitRepo(t)
	w := &Worktrees{Workspace: repo, RunID: "20261106-090000.000"}
	ctx := context.Background()

	env, err := w.Enter(ctx, run.EachCall{Step: eachStep(false, ""), Item: run.Item{Key: "a"}})
	if err != nil || env != (run.ItemEnv{}) {
		t.Fatalf("worktree false: %+v %v", env, err)
	}

	call := run.EachCall{Step: eachStep(true, "keep"), Item: run.Item{Key: "LIN-228"}, Base: "main"}
	env, err = w.Enter(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	if env.Worktree == "" || env.ID == "" || !strings.HasPrefix(env.Branch, git.AgentBranchPrefix) {
		t.Fatalf("env %+v", env)
	}
	if b, err := os.ReadFile(filepath.Join(env.Worktree, "a.txt")); err != nil || string(b) != "one\n" {
		t.Fatalf("worktree a.txt %q %v", b, err)
	}
	if !strings.Contains(env.ID, "fix") || !strings.Contains(env.ID, "lin-228") {
		t.Errorf("id %q names neither the step nor the item", env.ID)
	}
	// Again (a retry or a resume): the same worktree.
	if again, err := w.Enter(ctx, call); err != nil || again != env {
		t.Errorf("again %+v %v", again, err)
	}
	if err := w.Leave(ctx, call, env, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(env.Worktree); err != nil {
		t.Errorf("keep removed the worktree: %v", err)
	}

	disc := run.EachCall{Step: eachStep(true, "discard"), Item: run.Item{Key: "b"}}
	bad, err := w.Enter(ctx, disc)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Leave(ctx, disc, bad, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bad.Worktree); err != nil {
		t.Errorf("discard removed a failed item's worktree: %v", err)
	}
	if err := w.Leave(ctx, disc, bad, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bad.Worktree); !os.IsNotExist(err) {
		t.Errorf("discard kept a good item's worktree: %v", err)
	}
	if out := gitT(t, repo, "branch", "--list", bad.Branch); !strings.Contains(out, bad.Branch) {
		t.Errorf("discard dropped the branch %s", bad.Branch)
	}
}

func TestWorktreesUniquePerItemKey(t *testing.T) {
	repo := gitRepo(t)
	w := &Worktrees{Workspace: repo, RunID: "r1"}
	ctx := context.Background()
	a, err := w.Enter(ctx, run.EachCall{Step: eachStep(true, ""), Item: run.Item{Key: "a b"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.Enter(ctx, run.EachCall{Step: eachStep(true, ""), Item: run.Item{Key: "a-b"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Errorf("two item keys share worktree %s", a.ID)
	}
}

func TestWorktreesNeedARepository(t *testing.T) {
	w := &Worktrees{Workspace: t.TempDir(), RunID: "r1"}
	if _, err := w.Enter(context.Background(), run.EachCall{Step: eachStep(true, ""), Item: run.Item{Key: "a"}}); err == nil {
		t.Error("no error outside a git repository")
	}
}
