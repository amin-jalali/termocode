//go:build unix

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// webTaskRepo is a real git repository with a commit and a setup line in
// .termocode/worktrees.json (not trusted yet); the Model works in it and
// one agent (Claude Code) is installed.
func webTaskRepo(t *testing.T) (*Model, *webConn, string) {
	t.Helper()
	withConfigDir(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(repo, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	wtGit(t, repo, "init", "-q", "-b", "main")
	for p, body := range map[string]string{"README.md": "x\n", ".termocode/worktrees.json": `{"setup": ["make deps"]}`} {
		if err := os.WriteFile(filepath.Join(repo, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wtGit(t, repo, "add", "-A")
	wtGit(t, repo, "commit", "-qm", "init")
	old := wtNewAgents
	wtNewAgents = func() []agents.Agent { return []agents.Agent{{ID: "claude", Title: "Claude Code", Bin: "claude"}} }
	t.Cleanup(func() { wtNewAgents = old })
	m := aiTestModel(t)
	webTestHub(t, &m)
	m.ws = workspace.Workspace{Root: repo}
	m.w, m.h = 150, 42
	c := webTestSub(t, &m)
	return &m, c, repo
}

// Spec (CMP-49): the page's "New task" form reads what the laptop's
// Worktree: New… offers: the start point, the branches, the installed
// agents (id and title) and whether the repository's setup asks first.
func TestWorktreesTaskForm(t *testing.T) {
	m, c, _ := webTaskRepo(t)
	call, cmd := webTestCallLater(t, m, c, webproto.OpWorktreesTaskForm, `{}`)
	if f := webTestAnswer(t, c, call); f != nil {
		t.Fatalf("answered before the repository was read: %v", f)
	}
	if cmd == nil {
		t.Fatal("no read")
	}
	cmd()
	f := webTestAnswer(t, c, call)
	var form webproto.WorktreeTaskForm
	webTestData(t, f, &form)
	if form.State != "ready" || form.From != "main" || len(form.Refs) == 0 || form.Refs[0] != "main" || form.Setup != "asks" {
		t.Fatalf("form %+v", form)
	}
	if len(form.Agents) != 1 || form.Agents[0] != (webproto.WorktreeTaskAgent{ID: "claude", Title: "Claude Code"}) {
		t.Errorf("agents %+v", form.Agents)
	}
	if !strings.HasSuffix(form.Base, "app.worktrees") {
		t.Errorf("base %q", form.Base)
	}
}

// Spec (CMP-49): a new task from the page makes a worktree off the UI
// thread (the branch drafted from the task when no name is given),
// stores that the agent works there with a note who asked, answers with
// the worktree's id as the worktrees topic names it, and asks on the
// laptop before the repository's untrusted setup runs. The same task
// twice is refused (the branch is there); an agent that is not
// installed is refused before anything is made.
func TestWorktreesNewTask(t *testing.T) {
	m, c, repo := webTaskRepo(t)
	if f, _ := webTestCall(t, m, c, webproto.OpWorktreesNewTask, `{"agent":"aider","task":"fix it"}`); webTestErr(f) != "not_found" {
		t.Fatalf("unknown agent: %v", f)
	}
	call, cmd := webTestCallLater(t, m, c, webproto.OpWorktreesNewTask, `{"agent":"claude","task":"Fix the login bug"}`)
	if cmd == nil {
		t.Fatal("no create")
	}
	msg := cmd()
	if _, ok := msg.(wtWebTaskMsg); !ok {
		t.Fatalf("msg %T", msg)
	}
	if _, ok := m.handleWTMsg(msg); !ok {
		t.Fatal("the worktrees router does not take the task")
	}
	var done webproto.WorktreeTaskDone
	webTestData(t, webTestAnswer(t, c, call), &done)
	folder := filepath.Join(filepath.Dir(repo), "app.worktrees", "task-fix-login-bug")
	if done.Branch != "task/fix-login-bug" || done.ID != webWorktreeID(folder) || !strings.HasSuffix(done.Where, "app.worktrees/task-fix-login-bug") {
		t.Fatalf("done %+v", done)
	}
	if out, _ := exec.Command("git", "-C", folder, "branch", "--show-current").Output(); strings.TrimSpace(string(out)) != "task/fix-login-bug" {
		t.Errorf("branch %q", out)
	}
	all, err := worktrees.Load(filepath.Join(repo, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	md := all[folder]
	n, ok := md.LastNote()
	if md.Agent != "claude" || md.Created.IsZero() || !ok || !strings.Contains(n.Text, "phone") || !strings.Contains(n.Text, "Fix the login bug") {
		t.Errorf("meta %+v note %+v", md, n)
	}
	if !m.confirmOpen || m.confirmKind != confirmKindWTSetupTrust {
		t.Errorf("the untrusted setup did not ask on the laptop (open %v kind %v)", m.confirmOpen, m.confirmKind)
	}
	if done.Note == "" {
		t.Error("the page is not told the setup asks first")
	}
	call, cmd = webTestCallLater(t, m, c, webproto.OpWorktreesNewTask, `{"agent":"claude","task":"Fix the login bug"}`)
	m.handleWTMsg(cmd())
	if f := webTestAnswer(t, c, call); webTestErr(f) != "conflict" {
		t.Errorf("same task twice: %v", f)
	}
}
