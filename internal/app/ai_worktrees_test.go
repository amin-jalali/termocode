package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
)

// G-WT: parallel agent steps in their own git worktrees.

// gitInit makes dir a git repository with a.txt committed.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"},
		{"-c", "user.name=T", "-c", "user.email=t@e", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// aiWorktreeRunModel is a chat on a profile whose steps left (main) and
// right (quick) run at the same time and write a.txt and b.txt; join
// (read-only, main) runs after both.
func aiWorktreeRunModel(t *testing.T, repo bool) (*Model, string, *recordingAIProvider) {
	t.Helper()
	left := &recordingAIProvider{replies: []ai.Chunk{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "write_file", Args: map[string]any{"path": "a.txt", "content": "left\n"}}}},
		{Delta: "left done"}, {Delta: "joined"}}}
	right := &recordingAIProvider{replies: []ai.Chunk{
		{ToolCalls: []ai.ToolCall{{ID: "c2", Name: "create_file", Args: map[string]any{"path": "b.txt", "content": "right\n"}}}},
		{Delta: "right done"}}}
	m, dir := aiRunModel(t, left, right)
	if repo {
		gitInit(t, dir)
	}
	m.ai.settings.AutoApprove = ai.ToolClasses // no review card
	m.ai.settings.ParallelMax = 2
	writeProfiles(t, `{"profiles":{"two":{"steps":[
		{"id":"left","slot":"main"},
		{"id":"right","slot":"quick"},
		{"id":"join","slot":"main","deps":["left","right"],"tools":{"preset":"read-only"}}]}}}`)
	m.ai.loadProfiles(ai.Credentials{})
	m.ai.profile = "two"
	return m, dir, left
}

// Two writing steps of one wave each get their own worktree: the user's
// tree is untouched, each card names its branch and the files it
// changed, and the step after them is told their changes wait for a
// merge.
func TestAIRunParallelStepsOwnWorktrees(t *testing.T) {
	m, dir, main := aiWorktreeRunModel(t, true)
	driveAIChat(t, m, m.sendAIChat("change things"), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone {
		t.Fatalf("run %+v steps %+v", rm.Run, rm.Steps)
	}
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Errorf("user's a.txt changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err == nil {
		t.Error("b.txt written into the user's tree")
	}
	want := map[string]string{"left": "a.txt", "right": "b.txt"}
	for _, st := range rm.Steps[:2] {
		wt := st.Worktree
		if wt == nil || wt.State != ai.WorktreeReady || wt.Files != 1 || st.Files != 1 || !strings.HasPrefix(wt.Branch, git.AgentBranchPrefix) {
			t.Fatalf("%s worktree %+v", st.ID, wt)
		}
		if _, err := os.Stat(filepath.Join(wt.Path, want[st.ID])); err != nil {
			t.Errorf("%s: %s not in its worktree", st.ID, want[st.ID])
		}
	}
	if rm.Steps[2].Worktree != nil {
		t.Errorf("join (alone in its wave) got a worktree %+v", rm.Steps[2].Worktree)
	}
	if in := lastUserContent(main.reqs[len(main.reqs)-1]); !strings.Contains(in, "own git worktree") {
		t.Errorf("join is not told about the worktrees: %q", in)
	}
	got := aiTranscriptText(m, 100)
	if !strings.Contains(got, "⑂ termocode/agent-") || !strings.Contains(got, "1 file changed") {
		t.Errorf("transcript:\n%s", got)
	}
	m.ai.agents.sel, m.ai.agents.manual = "left", true
	c := m.agentsBuild(100, time.Now())
	if body := ansi.Strip(strings.Join(c.body, "\n")); !strings.Contains(body, " 1 file changed ") || !strings.Contains(body, "termocode/agent-") {
		t.Errorf("AGENTS cards have no worktree line:\n%s", body)
	}
}

// Not a git repository, or ai_worktrees off: every step works in the
// user's tree, as before.
func TestAIRunParallelStepsSameTree(t *testing.T) {
	for _, tc := range []struct {
		name string
		repo bool
		off  bool
	}{{"no repo", false, false}, {"setting off", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			m, dir, _ := aiWorktreeRunModel(t, tc.repo)
			if tc.off {
				m.ai.settings.Worktrees = ai.WorktreesOff
			}
			driveAIChat(t, m, m.sendAIChat("change things"), nil)
			rm := aiRunMsg(t, m)
			if got := readFile(filepath.Join(dir, "a.txt")); got != "left\n" {
				t.Errorf("a.txt %q", got)
			}
			if got := readFile(filepath.Join(dir, "b.txt")); got != "right\n" {
				t.Errorf("b.txt %q", got)
			}
			for _, st := range rm.Steps {
				if st.Worktree != nil {
					t.Errorf("%s has a worktree", st.ID)
				}
			}
		})
	}
}

// A step that shares its wave but cannot get its own worktree (a git
// error) must not write in the user's tree next to another step: it
// stops with the reason and the user's files stay as they are.
func TestAIRunWorktreeCreateFailsStopsStep(t *testing.T) {
	m, dir, _ := aiWorktreeRunModel(t, true)
	orig := createAgentWorktree
	createAgentWorktree = func(string, string) (git.AgentWorktree, error) {
		return git.AgentWorktree{}, errors.New("git worktree: fatal: failed to read .git/worktrees/x/commondir")
	}
	t.Cleanup(func() { createAgentWorktree = orig })
	driveAIChat(t, m, m.sendAIChat("change things"), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateFailed {
		t.Fatalf("run %+v", rm.Run)
	}
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Errorf("user's a.txt changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err == nil {
		t.Error("b.txt written into the user's tree")
	}
	failed := false
	for _, st := range rm.Steps[:2] {
		if st.Worktree != nil {
			t.Errorf("%s has a worktree %+v", st.ID, st.Worktree)
		}
		if strings.Contains(st.Error, "own git worktree") && strings.Contains(st.Error, "commondir") {
			failed = true
		}
	}
	if !failed {
		t.Errorf("no step names the worktree error: %+v", rm.Steps)
	}
}

// runWorktreeCmd runs a worktree action's cmd and routes its message.
func runWorktreeCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no cmd")
	}
	if _, ok := m.handleAIMsg(cmd()); !ok {
		t.Fatal("message not routed")
	}
}

func gitBranchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "branch", "--list", branch).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out)) != ""
}

// A finished worktree step offers Review, Merge and Discard. Review
// shows the agent's changes; Merge writes them into the user's tree and
// removes the worktree and its branch.
func TestAIWorktreeReviewAndMerge(t *testing.T) {
	m, dir, _ := aiWorktreeRunModel(t, true)
	driveAIChat(t, m, m.sendAIChat("change things"), nil)
	rm := aiRunMsg(t, m)
	got := aiTranscriptText(m, 100)
	if !strings.Contains(got, "Review   Merge   Discard") {
		t.Fatalf("no worktree chips:\n%s", got)
	}
	key := rm.Run.ID + ":left"
	runWorktreeCmd(t, m, m.onAIButton("wt-review:"+key))
	if m.ai.review == nil || m.ai.review.card.Files() != 1 || m.ai.review.card.File(0).Name != "a.txt" || m.ai.review.card.File(0).New != "left\n" {
		t.Fatalf("review card %+v", m.ai.review)
	}
	m.ai.review = nil

	wt := *rm.Steps[0].Worktree
	runWorktreeCmd(t, m, m.onAIButton("wt-merge:"+key))
	if got := readFile(filepath.Join(dir, "a.txt")); got != "left\n" {
		t.Errorf("a.txt after merge %q", got)
	}
	if st := aiRunMsg(t, m).Steps[0].Worktree; st.State != ai.WorktreeMerged {
		t.Errorf("state %q", st.State)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Error("worktree still there after merge")
	}
	if gitBranchExists(t, dir, wt.Branch) {
		t.Error("branch still there after a clean merge")
	}
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "merged into your tree") {
		t.Errorf("transcript:\n%s", got)
	}
}

// When the user changed the same file, Merge writes nothing and shows a
// conflict card with Cancel first; Merge with markers then writes both
// versions and keeps the branch; Keep on branch only removes the tree.
func TestAIWorktreeMergeConflict(t *testing.T) {
	m, dir, _ := aiWorktreeRunModel(t, true)
	driveAIChat(t, m, m.sendAIChat("change things"), nil)
	rm := aiRunMsg(t, m)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := rm.Run.ID + ":left"
	runWorktreeCmd(t, m, m.onAIButton("wt-merge:"+key))
	if !m.confirmOpen || m.confirmKind != confirmKindAIWorktreeConflict {
		t.Fatalf("no conflict card (open %v kind %v)", m.confirmOpen, m.confirmKind)
	}
	if b := m.confirm.Buttons(); b[0].ID != "cancel" {
		t.Errorf("first button %q, want cancel", b[0].ID)
	}
	if got := readFile(filepath.Join(dir, "a.txt")); got != "user\n" {
		t.Errorf("a.txt written on a conflict: %q", got)
	}
	if st := aiRunMsg(t, m).Steps[0].Worktree; st.State != ai.WorktreeConflict || strings.Join(st.Conflicts, ",") != "a.txt" {
		t.Errorf("worktree %+v", st)
	}
	// Cancel: nothing happens.
	if cmd, _ := m.handleAIConfirm("cancel"); cmd != nil {
		t.Error("cancel returned a cmd")
	}
	// Again, then merge with markers.
	runWorktreeCmd(t, m, m.onAIButton("wt-merge:"+key))
	cmd, _ := m.handleAIConfirm("markers")
	runWorktreeCmd(t, m, cmd)
	a := readFile(filepath.Join(dir, "a.txt"))
	if !strings.Contains(a, "<<<<<<<") || !strings.Contains(a, "user") || !strings.Contains(a, "left") {
		t.Errorf("a.txt %q", a)
	}
	wt := aiRunMsg(t, m).Steps[0].Worktree
	if wt.State != ai.WorktreeKept || !gitBranchExists(t, dir, wt.Branch) {
		t.Errorf("after markers %+v", wt)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Error("worktree still there")
	}

	// right: Keep on branch.
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("mine too\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rkey := rm.Run.ID + ":right"
	runWorktreeCmd(t, m, m.onAIButton("wt-merge:"+rkey))
	cmd, _ = m.handleAIConfirm("branch")
	runWorktreeCmd(t, m, cmd)
	rwt := aiRunMsg(t, m).Steps[1].Worktree
	if rwt.State != ai.WorktreeOnBranch || !gitBranchExists(t, dir, rwt.Branch) || readFile(filepath.Join(dir, "b.txt")) != "mine too\n" {
		t.Errorf("keep on branch %+v", rwt)
	}
}

// Discard asks first (Cancel first); Cancel keeps everything, Discard
// removes the worktree and its branch and leaves the user's tree alone.
func TestAIWorktreeDiscard(t *testing.T) {
	m, dir, _ := aiWorktreeRunModel(t, true)
	driveAIChat(t, m, m.sendAIChat("change things"), nil)
	rm := aiRunMsg(t, m)
	key := rm.Run.ID + ":right"
	wt := *rm.Steps[1].Worktree
	m.onAIButton("wt-discard:" + key)
	if !m.confirmOpen || m.confirmKind != confirmKindAIWorktreeDiscard || m.confirm.Buttons()[0].ID != "cancel" {
		t.Fatalf("discard card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	if cmd := m.onAIConfirmClosed(); cmd != nil { // Esc
		t.Error("Esc returned a cmd")
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatal("Esc removed the worktree")
	}
	m.onAIButton("wt-discard:" + key)
	cmd, _ := m.handleAIConfirm("discard")
	runWorktreeCmd(t, m, cmd)
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Error("worktree still there")
	}
	if gitBranchExists(t, dir, wt.Branch) {
		t.Error("branch still there")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err == nil {
		t.Error("b.txt appeared in the user's tree")
	}
	if st := aiRunMsg(t, m).Steps[1].Worktree; st.State != ai.WorktreeDiscarded {
		t.Errorf("state %q", st.State)
	}
	if got := aiTranscriptText(m, 100); strings.Count(got, "Review   Merge   Discard") != 1 {
		t.Errorf("chips after discard:\n%s", got)
	}
}

// A worktree left over (a crash, another chat) shows in AI: Agent
// Worktrees…; its card has Cancel first, and Discard asks once more
// before the worktree and branch go. Merge brings its files in.
func TestAIWorktreesPaletteLeftover(t *testing.T) {
	m, dir := aiIDETestModel(t, fakeAIProvider{reply: "x"})
	gitInit(t, dir)
	mk := func(id, file string) git.AgentWorktree {
		w, err := git.CreateAgentWorktree(dir, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(w.Path, file), []byte(id+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return w
	}
	old, keep := mk("old-x", "x.txt"), mk("old-y", "y.txt")

	cmd, ok := m.dispatchAIPalette("ai-worktrees")
	if !ok {
		t.Fatal("palette entry missing")
	}
	runWorktreeCmd(t, m, cmd)
	if !m.pickerOpen || m.pickerKind != pickerKindAIWorktrees {
		t.Fatal("no worktree list")
	}
	if got := ansi.Strip(m.picker.View()); !strings.Contains(got, old.Branch) || !strings.Contains(got, "1 file · left over") {
		t.Errorf("list:\n%s", got)
	}
	m.pickerOpen = false
	m.handleAIPickerSelect("left:old-x")
	if !m.confirmOpen || m.confirmKind != confirmKindAIWorktreeLeft || m.confirm.Buttons()[0].ID != "cancel" {
		t.Fatalf("card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc
	if _, err := os.Stat(old.Path); err != nil {
		t.Fatal("Esc removed the worktree")
	}
	m.handleAIPickerSelect("left:old-x")
	m.confirmOpen = false
	m.handleAIConfirm("discard") // the second card
	if !m.confirmOpen || m.confirmKind != confirmKindAIWorktreeDiscard || m.confirm.Buttons()[0].ID != "cancel" {
		t.Fatal("discard did not ask first")
	}
	m.confirmOpen = false
	cmd, _ = m.handleAIConfirm("discard")
	runWorktreeCmd(t, m, cmd)
	if _, err := os.Stat(old.Path); !os.IsNotExist(err) || gitBranchExists(t, dir, old.Branch) {
		t.Error("old-x still there")
	}

	m.handleAIPickerSelect("left:old-y")
	m.confirmOpen = false
	cmd, _ = m.handleAIConfirm("merge")
	runWorktreeCmd(t, m, cmd)
	if got := readFile(filepath.Join(dir, "y.txt")); got != "old-y\n" {
		t.Errorf("y.txt %q", got)
	}
	if _, err := os.Stat(keep.Path); !os.IsNotExist(err) {
		t.Error("old-y worktree still there after merge")
	}
}

// AI: Toggle Agent Worktrees saves ai_worktrees.
func TestAIWorktreesToggle(t *testing.T) {
	m, _ := aiIDETestModel(t, fakeAIProvider{reply: "x"})
	m.dispatchAIPalette("ai-worktrees-toggle")
	p, _ := ai.ConfigPath()
	if m.ai.settings.UseWorktrees() || ai.LoadSettings(p).Worktrees != ai.WorktreesOff {
		t.Error("not off")
	}
	m.dispatchAIPalette("ai-worktrees-toggle")
	if !m.ai.settings.UseWorktrees() || ai.LoadSettings(p).Worktrees != "" {
		t.Error("not on again")
	}
}

// After a crash (the chat is loaded again) a step's worktree that was
// still in use waits for the user: its card offers Review, Merge and
// Discard; in AGENTS, w opens its card.
func TestAIWorktreeAfterCrash(t *testing.T) {
	m, _ := aiIDETestModel(t, fakeAIProvider{reply: "x"})
	c := ai.Conversation{ID: "c1", Messages: []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "go"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r1", State: ai.RunStateRunning}, Steps: []ai.StoredStep{
			{ID: "left", Title: "Left", State: "running", Worktree: &ai.StoredWorktree{ID: "r1-left", Path: "/x", Branch: "termocode/agent-r1-left", State: ai.WorktreeRunning}},
		}},
	}}
	aiMarkInterrupted(&c)
	m.ai.conv = c
	if wt := c.Messages[1].Steps[0].Worktree; wt.State != ai.WorktreeReady {
		t.Fatalf("state %q", wt.State)
	}
	got := aiTranscriptText(m, 100)
	if !strings.Contains(got, "Review   Merge   Discard") || !strings.Contains(got, "stopped · Review shows its changes") {
		t.Errorf("transcript:\n%s", got)
	}
	m.ai.agents.on = true
	m.ai.agents.sel, m.ai.agents.manual = "left", true
	m.routeAgentsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if !m.confirmOpen || m.confirmKind != confirmKindAIWorktreeLeft {
		t.Error("w did not open the worktree card")
	}
}
