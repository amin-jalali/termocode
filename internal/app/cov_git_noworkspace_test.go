package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// The workspace folder is unknown (its folder was deleted under the
// process): every git action does nothing instead of running git in
// the wrong place, and nothing panics.
func TestCovGitNoWorkspaceFolder(t *testing.T) {
	old := processWS.Load()
	SetWorkspace(workspace.Workspace{ID: "gone"})
	t.Cleanup(func() { processWS.Store(old) })
	if _, err := procRootDir(); err == nil {
		t.Fatal("the folder is known")
	}
	m := &Model{w: 160, h: 40, gitLoaded: true, gitIsRepo: true, gitBranch: git.Branch{Name: "main"},
		gitFiles:   []git.FileStatus{{Code: " M", Path: "a.txt"}, {Code: "M ", Path: "b.txt"}},
		gitCommits: []git.GraphCommit{{Hash: "abc1234", FullHash: "abc1234full"}}}
	m.editor.SetMeta("/somewhere/a.txt", false, "")
	m.cursorLine = 1
	m.gitCommitMsg = "msg"
	m.confirmTargetPath = "a.txt"
	m.commitViewS().info = git.CommitInfo{FullHash: "abc1234full", Files: []git.CommitFile{{Path: "a.txt"}}}
	m.commitView.pick = "abc1234full"
	m.fixupTarget = "abc1234full"
	m.rebase = &gitRebaseState{start: &rebaseCheckMsg{}}
	m.diffLeftPath = ""

	cmds := map[string]func() tea.Cmd{
		"hunk":           m.gitStageHunk,
		"stage file":     m.gitStageActiveFile,
		"stage all":      m.gitStageAll,
		"diff file":      m.gitDiffActiveFile,
		"discard":        m.gitDiscardConfirmed,
		"commit prompt":  func() tea.Cmd { return m.gitCommitFromPrompt("x") },
		"inline commit":  func() tea.Cmd { return m.gitInlineCommit(false, false) },
		"stage commit":   m.gitStageAllAndCommit,
		"push":           m.gitPush,
		"branches":       m.openBranchPicker,
		"checkout":       func() tea.Cmd { return m.gitCheckoutSelected("co-main") },
		"new branch":     func() tea.Cmd { return m.gitCreateBranchFromPrompt("b") },
		"stash":          m.gitStash,
		"stash pop":      m.gitStashPop,
		"stash list":     m.openStashPicker,
		"stash apply":    func() tea.Cmd { return m.applyStashSelected("stash@{0}") },
		"blame":          m.gitBlameCurrentLine,
		"history":        m.gitFileHistoryPicker,
		"tags":           m.gitTagPicker,
		"tag checkout":   func() tea.Cmd { return m.checkoutTagSelected("tag-v1") },
		"log":            m.gitLogPicker,
		"show commit":    func() tea.Cmd { return m.gitShowCommitCmd("abc1234") },
		"compare rev":    func() tea.Cmd { return m.runCompareWithRevision("HEAD") },
		"lines":          m.gitStageLines,
		"discard lines":  m.gitDiscardLinesConfirmed,
		"rebase from":    func() tea.Cmd { return m.gitRebaseFromCmd("abc1234full") },
		"rebase check":   func() tea.Cmd { return m.rebaseCheckCmd(git.RebaseTodo{}) },
		"rebase start":   func() tea.Cmd { return m.onRebaseConfirm("rebase-start") },
		"rebase control": func() tea.Cmd { return m.onRebaseControl("rebase-abort") },
		"fixup":          func() tea.Cmd { return m.gitFixupIntoCmd("abc1234full") },
		"fixup confirm":  func() tea.Cmd { return m.onFixupConfirm("fixup") },
		"commit view":    func() tea.Cmd { return m.gitOpenCommitViewCmd("abc1234") },
		"compare":        func() tea.Cmd { return m.gitOpenCompareCmd(commitRange{a: "a", b: "b"}) },
		"at commit":      func() tea.Cmd { return m.commitAction("open-at-commit", 0) },
		"pick":           func() tea.Cmd { return m.runCommitPick("revert") },
		"hover":          func() tea.Cmd { return m.fetchCommitDetailCmd("abc1234") },
	}
	for name, run := range cmds {
		if cmd := run(); cmd != nil {
			if msg := cmd(); msg != nil {
				t.Errorf("%s ran with no workspace folder: %T", name, msg)
			}
		}
	}
	if m.commitLoadFile(0) != nil {
		t.Error("commit file loaded with no folder")
	}
	if _, ok := m.activeGitRel(); ok {
		t.Error("a path relative to no folder")
	}
	if gitFileAbsPath("a.txt") != "a.txt" {
		t.Error("abs path with no folder")
	}
	// Pull requests and CI need the folder too.
	m.prViewCmd(1)
	if !toastHas(m, "Pull requests need a folder with a git repository") {
		t.Error("PR view: no note")
	}
	if m.ciLogCmd(false) != nil {
		t.Error("CI log with no folder")
	}
}
