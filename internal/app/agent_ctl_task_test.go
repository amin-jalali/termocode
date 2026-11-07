package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/worktreeform"
)

// taskTestRepo is a git repository with one commit.
func taskTestRepo(t *testing.T) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(root, "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	wtGit(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wtGit(t, repo, "add", "README.md")
	wtGit(t, repo, "commit", "-qm", "init")
	return repo
}

// taskTestAgents makes claude and codex the installed agents.
func taskTestAgents(t *testing.T) {
	t.Helper()
	old := wtNewAgents
	t.Cleanup(func() { wtNewAgents = old })
	wtNewAgents = func() []agents.Agent {
		var out []agents.Agent
		for _, id := range []string{"claude", "codex"} {
			if a, ok := agents.ByID(id); ok {
				out = append(out, a)
			}
		}
		return out
	}
}

func taskCall(r agentctl.Request) agentCtlCall {
	r.Op = agentctl.OpTaskStart
	return agentCtlCall{req: r, reply: make(chan agentctl.Reply, 1)}
}

func taskReply(t *testing.T, c agentCtlCall) agentctl.Reply {
	t.Helper()
	select {
	case r := <-c.reply:
		return r
	default:
		t.Fatal("no reply")
	}
	return agentctl.Reply{}
}

func taskNoReply(t *testing.T, c agentCtlCall) {
	t.Helper()
	select {
	case r := <-c.reply:
		t.Fatalf("replied before the user answered: %+v", r)
	default:
	}
}

// taskOpen sends a task-start and opens its card.
func taskOpen(t *testing.T, m *Model, c agentCtlCall) {
	t.Helper()
	cmd := m.onAgentCtl(c)
	if cmd == nil {
		t.Fatalf("no card: %+v", taskReply(t, c))
	}
	prep, ok := cmd().(wtNewPrepMsg)
	if !ok {
		t.Fatal("not a repository read")
	}
	m.onWTNewPrep(prep)
}

// Spec (CMP-39): the worktree name is drafted from the task's first words.
func TestTaskNameFromTask(t *testing.T) {
	for in, want := range map[string]string{
		"fix the flaky login test now please": "task/fix the flaky login test",
		"  a/b c \nsecond line":               "task/a b c",
		"   ":                                 "",
	} {
		if got := taskNameFromTask(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if b := worktreeform.BranchFromName(taskNameFromTask("Fix the login")); b != "task/fix-the-login" {
		t.Errorf("branch %q", b)
	}
}

// Spec (CMP-39): the asked agent goes first; an agent that is not
// installed is refused.
func TestWTTaskAgents(t *testing.T) {
	c, _ := agents.ByID("claude")
	x, _ := agents.ByID("codex")
	got, ok := wtTaskAgents([]agents.Agent{c, x}, "codex")
	if !ok || got[0].ID != "codex" || got[1].ID != "claude" {
		t.Errorf("%v %v", got, ok)
	}
	if _, ok := wtTaskAgents([]agents.Agent{c}, "codex"); ok {
		t.Error("codex is not installed")
	}
	if _, ok := wtTaskAgents(nil, ""); ok {
		t.Error("no agent installed")
	}
}

// Spec (CMP-39): a bad task-start is answered at once and opens nothing.
func TestTaskStartRefused(t *testing.T) {
	withConfigDir(t)
	m := &Model{w: 150, h: 42}
	for _, r := range []agentctl.Request{{Text: "  "}, {Text: strings.Repeat("x", 33<<10)}, {Text: "go", Agent: "nobody"}} {
		c := taskCall(r)
		if cmd := m.onAgentCtl(c); cmd != nil {
			t.Errorf("%+v: a card would open", r)
		}
		if rep := taskReply(t, c); rep.OK || rep.Error == "" {
			t.Errorf("%+v: %+v", r, rep)
		}
	}
	if m.wt == nil || m.wt.taskAsk || m.wt.form != nil {
		t.Error("state left behind")
	}
}

// Spec (CMP-39): a task-start opens the New worktree card filled in
// (Cancel first, says who asks); Cancel answers "cancelled" and makes
// nothing; a second request while it is open is refused.
func TestTaskStartCancel(t *testing.T) {
	withConfigDir(t)
	taskTestAgents(t)
	repo := taskTestRepo(t)
	m := &Model{w: 150, h: 42}
	c := taskCall(agentctl.Request{Text: "fix the flaky login test\nwith care", Agent: "codex", Dir: repo})
	taskOpen(t, m, c)
	if !m.wtFormOpen() {
		t.Fatal("the card did not open")
	}
	if m.wt.taskAsk {
		t.Error("taskAsk not cleared")
	}
	card := m.connect.Card()
	if card.Buttons[0].ID != "cancel" {
		t.Errorf("first button %q", card.Buttons[0].ID)
	}
	if v := m.connect.InputValue("name"); v != "task/fix the flaky login test" {
		t.Errorf("name %q", v)
	}
	if v := m.connect.InputValue("task"); !strings.Contains(v, "fix the flaky login test") {
		t.Errorf("task %q", v)
	}
	if v := m.connect.InputValue("from"); v != "main" {
		t.Errorf("from %q", v)
	}
	if m.connect.Choice("agent") != 1 || m.wt.form.agents[0].ID != "codex" {
		t.Errorf("agent %d %v", m.connect.Choice("agent"), m.wt.form.agents)
	}
	if box := ansi.Strip(m.connect.Box()); !strings.Contains(box, "Asked by "+wtTaskAskerCLI) {
		t.Errorf("no asker on the card:\n%s", box)
	}
	taskNoReply(t, c)
	c2 := taskCall(agentctl.Request{Text: "another", Dir: repo})
	if cmd := m.onAgentCtl(c2); cmd != nil {
		t.Error("a second card would open")
	}
	if rep := taskReply(t, c2); rep.OK || !strings.Contains(rep.Error, "asking something else") {
		t.Errorf("second %+v", rep)
	}
	m.onWTFormButton("cancel")
	if rep := taskReply(t, c); rep.OK || !strings.Contains(rep.Error, "cancelled") {
		t.Errorf("cancel %+v", rep)
	}
	if m.connectOpen || m.wt.form != nil {
		t.Error("card still open")
	}
	if _, err := os.Stat(repo + ".worktrees"); err == nil {
		t.Error("a worktree was made on Cancel")
	}
}

// Spec (CMP-39): Create + Start answers with the new worktree, its
// branch and the agent; Create alone answers "created".
func TestTaskStartCreate(t *testing.T) {
	withConfigDir(t)
	taskTestAgents(t)
	repo := taskTestRepo(t)
	var made []string
	old := wtCreateRun
	wtCreateRun = func(f *wtForm, folder, branch, ref string) ([]worktreeform.File, error) {
		made = append(made, branch+"|"+ref)
		return nil, nil
	}
	t.Cleanup(func() { wtCreateRun = old })
	for _, tc := range []struct{ button, state, agent string }{{"start", "started", "codex"}, {"create", "created", ""}} {
		m := &Model{w: 150, h: 42}
		c := taskCall(agentctl.Request{Text: "Fix the login", Agent: "codex", From: "main", Dir: repo})
		taskOpen(t, m, c)
		cmd := m.onWTFormButton(tc.button)
		if cmd == nil {
			t.Fatalf("%s: nothing runs (%s)", tc.button, m.wt.form.fail)
		}
		created, ok := cmd().(wtCreatedMsg)
		if !ok {
			t.Fatalf("%s: not a create", tc.button)
		}
		taskNoReply(t, c)
		m.onWTCreated(created)
		rep := taskReply(t, c)
		if !rep.OK || rep.State != tc.state || len(rep.Worktrees) != 1 {
			t.Fatalf("%s: %+v", tc.button, rep)
		}
		w := rep.Worktrees[0]
		if w.Branch != "task/fix-the-login" || w.Agent != tc.agent || !strings.HasPrefix(w.Path, repo+".worktrees") {
			t.Errorf("%s: %+v", tc.button, w)
		}
		if m.connectOpen || m.wt.form != nil {
			t.Errorf("%s: card still open", tc.button)
		}
	}
	if len(made) != 2 || made[0] != "task/fix-the-login|main" {
		t.Errorf("made %v", made)
	}
}

// Spec (CMP-39): the MCP tool sends a task-start over the in-process
// queue and turns the answer into words; an ended context gives up.
func TestAITaskStartTool(t *testing.T) {
	tool := aiTaskStartTool("/src/app")
	if tool.Validate(map[string]any{"task": " "}) == nil {
		t.Error("an empty task is valid")
	}
	if mut, _ := tool.Gate(map[string]any{"task": "x"}); mut {
		t.Error("the bridge must not add a card: the New worktree card asks")
	}
	got := make(chan agentctl.Request, 1)
	go func() {
		c := <-agentCtlReqs
		got <- c.req
		c.reply <- agentctl.Reply{OK: true, State: "started", Worktrees: []agentctl.Worktree{{Path: "/src/app.worktrees/task-x", Branch: "task/x", Agent: "claude"}}}
	}()
	out, err := tool.Run(context.Background(), map[string]any{"task": "do x", "agent": "claude", "name": "x"})
	if err != nil || !strings.Contains(out, "/src/app.worktrees/task-x") || !strings.Contains(out, "claude") {
		t.Fatalf("%q %v", out, err)
	}
	if r := <-got; r.Op != agentctl.OpTaskStart || r.Tab != wtTaskMCPTab || r.Dir != "/src/app" || r.Text != "do x" || r.Agent != "claude" || r.Name != "x" {
		t.Errorf("request %+v", r)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := tool.Run(ctx, map[string]any{"task": "do y"}); err == nil {
		t.Error("no error when nobody answers")
	}
}
