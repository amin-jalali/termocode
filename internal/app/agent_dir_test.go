//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/git"
)

// AGX-2: outside_agent_start's worktree argument must be a listed
// worktree (by path or branch) or a folder of the workspace.
func TestAgentDirIn(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	ws := filepath.Join(root, "app")
	wt := filepath.Join(root, "app.worktrees", "feat-x")
	sub := filepath.Join(ws, "web")
	for _, d := range []string{ws, wt, sub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	list := []git.Worktree{
		{Path: ws, Branch: "main", Main: true},
		{Path: wt, Branch: "feat/x"},
		{Path: filepath.Join(root, "gone"), Branch: "gone", Prunable: true},
	}
	for arg, want := range map[string]string{
		wt:       wt,
		"feat/x": wt,
		"main":   "", // the workspace itself
		ws:       "",
		sub:      sub,
	} {
		got, err := agentDirIn(arg, ws, list)
		if err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", arg, got, err, want)
		}
	}
	for _, bad := range []string{"gone", filepath.Join(root, "gone"), "/etc", root, "../app.worktrees/feat-x", "nope"} {
		if got, err := agentDirIn(bad, ws, list); err == nil {
			t.Errorf("%q passed as %q", bad, got)
		}
	}
}

// AGX-2: an agent started in a folder gets a line that cds there first,
// and its MCP server gets that folder as --dir.
func TestAgentLaunchLineInFolder(t *testing.T) {
	m := agentMCPTestModel(t)
	t.Setenv("HOME", t.TempDir())
	mcpServeExecutable = func() (string, error) { return "/opt/tc/termocode", nil }
	t.Cleanup(func() { mcpServeExecutable = os.Executable })
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	rec := fakeAgentBin(t, "codex")
	line, _ := m.agentLaunchLineIn(agentByID(t, "codex"), dir)
	if !strings.HasPrefix(line, "cd ") {
		t.Fatalf("line %q", line)
	}
	argv := runLaunchLine(t, line, rec)
	if !strings.Contains(strings.Join(argv, " "), `"--dir", "`+dir+`"`) {
		t.Errorf("codex argv %q (want --dir %s)", argv, dir)
	}
	// A plain launch is as before.
	if line, _ := m.agentLaunchLineIn(agentByID(t, "codex"), ""); strings.HasPrefix(line, "cd ") {
		t.Errorf("workspace line %q", line)
	}
}

// AGX-2: a tab is "that agent here" only in its own folder, so the
// same agent can run in two worktrees.
func TestAgentStartedIn(t *testing.T) {
	tab := terminalTab{Agent: "claude", Worktree: "/src/app.worktrees/x"}
	if !agentStartedIn(tab, "claude", "/src/app.worktrees/x/") || agentStartedIn(tab, "claude", "") || agentStartedIn(tab, "codex", "/src/app.worktrees/x") {
		t.Error("worktree tab")
	}
	if !agentStartedIn(terminalTab{Agent: "claude"}, "claude", "") {
		t.Error("workspace tab")
	}
}

// AGX-2: Agent: Start… from the WORKTREES view starts in the selected
// worktree; from anywhere else in the workspace.
func TestAgentPickerFromWorktrees(t *testing.T) {
	m := aiTestModel(t)
	st, _ := wtFixture(t)
	m.wt = st
	m.showExp = true
	m.activity.SetActive(activity.ViewWorktrees)
	m.focus = FocusExplorer
	st.cursorPath = "/src/app.worktrees/toast"
	m.openAgentPicker()
	if m.agentPickDir != "/src/app.worktrees/toast" {
		t.Errorf("dir %q", m.agentPickDir)
	}
	m.focus = FocusEditor
	m.openAgentPicker()
	if m.agentPickDir != "" {
		t.Errorf("from the editor: %q", m.agentPickDir)
	}
}

// WTX-8: the first task is typed once, when the agent shows its input
// box, as one line.
func TestAgentFirstTaskOnce(t *testing.T) {
	done := []string{"╭──╮", "│ > │", "╰──╯", "  ? for shortcuts"}
	tab := &terminalTab{Agent: "claude", FirstTask: "fix it"}
	if got := agentFirstTaskDue(tab, []string{"✻ Working… (esc to interrupt)"}); got != "" {
		t.Errorf("typed while working: %q", got)
	}
	if got := agentFirstTaskDue(tab, append(done, "Do it? (y/n)")); got != "" {
		t.Errorf("typed into a y/n prompt: %q", got)
	}
	if got := agentFirstTaskDue(tab, done); got != "fix it" {
		t.Errorf("not typed at the input box: %q", got)
	}
	tab.FirstTask = ""
	if got := agentFirstTaskDue(tab, done); got != "" {
		t.Errorf("typed twice: %q", got)
	}
	if got := agentCleanTask("a\nb\x1b[2J\tc\r"); got != "a b[2J c" {
		t.Errorf("clean %q", got)
	}
}
