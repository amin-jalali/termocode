package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
