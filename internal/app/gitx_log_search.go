package app

import (
	"path/filepath"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Log search and filters (GITX-9): Search Log… (text plus author:,
// since:, until:, path:, branch:, changes: and all), Show Folder
// History…, Go to Commit…, @ (jump to HEAD) and a GRAPH of all branches.

const gitxLogLimit = 500

// gitGraphAllBranches makes the GRAPH read every branch, not only HEAD.
var gitGraphAllBranches atomic.Bool

// gitGraphCommits reads the GRAPH: HEAD's history, or every branch.
func gitGraphCommits(dir string, skip, limit int) ([]git.GraphCommit, bool, error) {
	if gitGraphAllBranches.Load() {
		return git.CommitsAll(dir, skip, limit)
	}
	return git.Commits(dir, skip, limit)
}

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-log-search", Title: "Git: Search Log…"},
			{ID: "git-log-folder", Title: "Git: Show Folder History…"},
			{ID: "git-goto-commit", Title: "Git: Go to Commit…"},
			{ID: "git-graph-head", Title: "Git: Go to HEAD in GRAPH", Hint: "@"},
			{ID: "git-graph-all", Title: "Git: Toggle All Branches in GRAPH"},
		},
		policies: map[string]aiActionPolicy{
			"git-log-search":  aiNever(aiNoPicker + " (git_log)"),
			"git-log-folder":  aiNever(aiNoPicker + " (git_log)"),
			"git-goto-commit": aiNever(aiNoPicker),
			"git-graph-head":  aiAuto(),
			"git-graph-all":   aiAuto(),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-log-search":
				return m.openLogSearchPrompt(), true
			case "git-log-folder":
				return m.openFolderHistoryPrompt(), true
			case "git-goto-commit":
				return m.openGotoCommitPrompt(), true
			case "git-graph-head":
				return m.gitGotoHead(), true
			case "git-graph-all":
				return m.toggleGraphAllBranches(), true
			}
			return nil, false
		},
		key: func(m *Model, k tea.KeyMsg) (tea.Cmd, bool) {
			if k.Type == tea.KeyRunes && string(k.Runes) == "@" {
				return m.gitGotoHead(), true
			}
			return nil, false
		},
	})
}

func (m *Model) openLogSearchPrompt() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	m.gitxPrompt("Search Log", "Text, and author: since: until: path: branch: changes: all", "", func(m *Model, q string) tea.Cmd {
		return m.gitxSearchLog(git.ParseLogQuery(q))
	})
	return nil
}

// openFolderHistoryPrompt asks a folder; the active file's folder is the
// default.
func (m *Model) openFolderHistoryPrompt() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	dir := ""
	if rel, ok := m.activeGitRel(); ok {
		if d := filepath.Dir(rel); d != "." {
			dir = filepath.ToSlash(d)
		}
	}
	m.gitxPrompt("Folder History", "Folder (empty: the whole repository)", dir, func(m *Model, v string) tea.Cmd {
		return m.gitxSearchLog(git.LogFilter{Path: strings.TrimSpace(v)})
	})
	return nil
}

// gitxSearchLog lists the commits that pass f; Enter opens one.
func (m *Model) gitxSearchLog(f git.LogFilter) tea.Cmd {
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	entries, err := git.SearchLog(cwd, f, gitxLogLimit)
	if err != nil {
		return m.gitxFail("Search failed", err)
	}
	if len(entries) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "No commits match", f.String())
		return c
	}
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		title := e.Hash + "  " + e.Subject
		if e.Refs != "" {
			title += "   " + e.Refs
		}
		items = append(items, picker.Item{ID: "commit-" + e.Hash, Title: title, Hint: e.Author + ", " + e.When})
	}
	label := f.String()
	if label == "" {
		label = "all commits"
	}
	m.gitxPick(" Log: "+label+" ", items, func(m *Model, id string) tea.Cmd {
		return m.gitOpenCommitViewCmd(strings.TrimPrefix(id, "commit-"))
	})
	return nil
}

func (m *Model) openGotoCommitPrompt() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	m.gitxPrompt("Go to Commit", "Hash, branch or tag", "", func(m *Model, v string) tea.Cmd {
		cwd, err := m.gitRootDir()
		if err != nil {
			return nil
		}
		v = strings.TrimSpace(v)
		full, err := git.ResolveCommit(cwd, v)
		if err != nil {
			return m.gitxInfo("No commit " + v)
		}
		if m.gitGraphCursorTo(full) {
			return nil
		}
		// Not in the loaded GRAPH (older, or on another branch): show it.
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "Not in the GRAPH", "Load older commits or show all branches; its changes are open")
		return tea.Batch(c, m.gitShowCommitCmd(full))
	})
	return nil
}

// gitGraphCursorTo shows Source Control and puts the cursor on the GRAPH
// row of commit full (more rows shown and its group unfolded as needed).
// false: the commit is not loaded.
func (m *Model) gitGraphCursorTo(full string) bool {
	ci := -1
	for i, c := range m.gitCommits {
		if c.FullHash == full {
			ci = i
			break
		}
	}
	if ci < 0 {
		return false
	}
	m.gitGraphCollapsed = false
	if ci >= m.gitGraphVisible() {
		m.gitGraphShown = ci + 1
	}
	if ci < len(m.gitCommitGroups) && m.gitGroupFolded[m.gitCommitGroups[ci]] {
		m.gitGroupFolded[m.gitCommitGroups[ci]] = false
	}
	for i, r := range m.gitPanelRows() {
		if r.kind == gitRowCommit && r.ci == ci {
			m.gitCursor = i
			break
		}
	}
	m.activity.SetActive(activity.ViewGit)
	m.showExp = true
	m.focus = FocusExplorer
	m.applyLayout()
	return true
}

// gitGotoHead puts the GRAPH cursor on HEAD.
func (m *Model) gitGotoHead() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	for _, c := range m.gitCommits {
		if c.IsHead {
			m.gitGraphCursorTo(c.FullHash)
			return nil
		}
	}
	return m.gitxInfo("HEAD is not in the GRAPH")
}

func (m *Model) toggleGraphAllBranches() tea.Cmd {
	on := !gitGraphAllBranches.Load()
	gitGraphAllBranches.Store(on)
	text := "GRAPH shows the current branch"
	if on {
		text = "GRAPH shows all branches"
	}
	return tea.Batch(fetchGitCmd(), m.gitxInfo(text))
}
