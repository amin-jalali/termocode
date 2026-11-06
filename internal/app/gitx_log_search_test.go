package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/picker"
)

// gitxLogRepo: main has first, docs, fix; branch side has a commit of its
// own.
func gitxLogRepo(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covWrite(t, dir, "docs/b.md", "b\n")
	covCommit(t, dir, "docs: add b")
	covGit(t, dir, "checkout", "-q", "-b", "side")
	covWrite(t, dir, "c.txt", "c\n")
	covCommit(t, dir, "side work")
	covGit(t, dir, "checkout", "-q", "main")
	covWrite(t, dir, "a.txt", "one\n")
	covCommit(t, dir, "fix: a")
	covRefresh(m)
	return m, dir
}

func gitxPickerIDs(m *Model) []string {
	var ids []string
	for _, it := range m.gitx.items {
		ids = append(ids, it.ID)
	}
	return ids
}

// GITX-9: Search Log… asks one line (text and key:value filters) and
// lists the matching commits; Enter opens one.
func TestGitxSearchLog(t *testing.T) {
	m, dir := gitxLogRepo(t)
	m.dispatchPaletteAction("git-log-search")
	if !m.promptOpen || m.promptKind != promptKindGitx {
		t.Fatal("no prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("FIX")
	fix := covGit(t, dir, "rev-parse", "--short", "HEAD")
	if !m.pickerOpen || m.pickerKind != pickerKindGitx || strings.Join(gitxPickerIDs(m), ",") != "commit-"+fix {
		t.Fatalf("picker %v %v", m.pickerOpen, gitxPickerIDs(m))
	}
	m.pickerOpen = false
	if cmd := m.handlePickerSelect(picker.SelectMsg{ID: "commit-" + fix}); cmd == nil {
		t.Error("Enter opened nothing")
	}
	// all branches, and a filter that matches nothing
	m.dispatchPaletteAction("git-log-search")
	m.promptOpen = false
	m.handlePromptSubmit("side all")
	if ids := gitxPickerIDs(m); len(ids) != 1 {
		t.Errorf("all branches: %v", ids)
	}
	m.pickerOpen = false
	m.dispatchPaletteAction("git-log-search")
	m.promptOpen = false
	m.handlePromptSubmit("nothing-like-this")
	if m.pickerOpen || !toastHas(m, "No commits match") {
		t.Error("no note for an empty result")
	}
	m.dispatchPaletteAction("git-log-search")
	m.promptOpen = false
	m.handlePromptSubmit("branch:nope")
	if m.pickerOpen || !toastHas(m, "Search failed") {
		t.Error("an unknown branch was not reported")
	}
}

// GITX-9 (E25): Show Folder History… lists the commits that touched a
// folder.
func TestGitxFolderHistory(t *testing.T) {
	m, _ := gitxLogRepo(t)
	m.dispatchPaletteAction("git-log-folder")
	if !m.promptOpen || m.promptKind != promptKindGitx {
		t.Fatal("no prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("docs")
	if ids := gitxPickerIDs(m); !m.pickerOpen || len(ids) != 1 {
		t.Errorf("docs history: %v", ids)
	}
}

// GITX-9 (E16): Go to Commit… puts the GRAPH cursor on the commit (it
// shows more rows and unfolds its group when needed).
func TestGitxGoToCommit(t *testing.T) {
	m, dir := gitxLogRepo(t)
	first := covGit(t, dir, "rev-parse", "HEAD~2")
	m.gitGroupFolded = map[string]bool{}
	for _, g := range m.gitCommitGroups {
		m.gitGroupFolded[g] = true
	}
	m.dispatchPaletteAction("git-goto-commit")
	if !m.promptOpen || m.promptKind != promptKindGitx {
		t.Fatal("no prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit(first[:8])
	cur, ok := m.gitCurrentRow()
	if !ok || cur.kind != gitRowCommit || m.gitCommits[cur.ci].FullHash != first {
		t.Fatalf("cursor on %+v", cur)
	}
	if m.activity.Active() != activity.ViewGit {
		t.Error("Source Control not shown")
	}
	m.dispatchPaletteAction("git-goto-commit")
	m.promptOpen = false
	m.handlePromptSubmit("nope")
	if !toastHas(m, "No commit nope") {
		t.Error("no note for an unknown commit")
	}
}

// GITX-9 (E22): @ in Source Control jumps to the HEAD commit.
func TestGitxJumpToHead(t *testing.T) {
	m, _ := gitxLogRepo(t)
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowCommit && !m.gitCommits[r.ci].IsHead })
	if _, ok := covKey(m, covRunes("@")); !ok {
		t.Fatal("@ not handled")
	}
	if cur, ok := m.gitCurrentRow(); !ok || cur.kind != gitRowCommit || !m.gitCommits[cur.ci].IsHead {
		t.Errorf("cursor on %+v", cur)
	}
}

// GITX-9 (E10): the GRAPH shows all branches after the toggle, and HEAD
// only after the second one.
func TestGitxGraphAllBranches(t *testing.T) {
	m, _ := gitxLogRepo(t)
	t.Cleanup(func() { gitGraphAllBranches.Store(false) })
	has := func() bool {
		for _, c := range m.gitCommits {
			if c.Subject == "side work" {
				return true
			}
		}
		return false
	}
	if has() {
		t.Fatal("side branch shown before the toggle")
	}
	m.dispatchPaletteAction("git-graph-all")
	covRefresh(m)
	if !has() || !toastHas(m, "all branches") {
		t.Error("side branch not shown")
	}
	m.dispatchPaletteAction("git-graph-all")
	covRefresh(m)
	if has() {
		t.Error("still all branches")
	}
}
