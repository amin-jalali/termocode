package app

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/git"
)

// GitMsg carries the latest git snapshot.
type GitMsg struct {
	IsRepo bool
	Branch git.Branch
	Files  []git.FileStatus
	Graph  []git.GraphLine
}

// gitGraphLimit caps how many commits the Source Control GRAPH section loads.
const gitGraphLimit = 200

// fetchGitCmd asynchronously runs git status + branch + graph and returns a
// GitMsg.
func fetchGitCmd() tea.Cmd {
	return func() tea.Msg {
		cwd, err := os.Getwd()
		if err != nil {
			return GitMsg{}
		}
		if !git.IsRepo(cwd) {
			return GitMsg{IsRepo: false}
		}
		branch, _ := git.GetBranch(cwd)
		files, _ := git.Status(cwd)
		graph, _ := git.GraphLog(cwd, gitGraphLimit)
		return GitMsg{IsRepo: true, Branch: branch, Files: files, Graph: graph}
	}
}

// gitFileAbsPath returns the absolute path of a git status entry.
func gitFileAbsPath(rel string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return rel
	}
	return filepath.Join(cwd, rel)
}

// gitStatusMap builds an absolute-path → status-letter map for the explorer.
func (m Model) gitStatusMap() map[string]string {
	if !m.gitIsRepo || len(m.gitFiles) == 0 {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(m.gitFiles))
	for _, f := range m.gitFiles {
		abs := filepath.Join(cwd, f.Path)
		// Pick the more meaningful column: index col first, then worktree col.
		var letter string
		if f.Untracked() {
			letter = "U"
		} else if len(f.Code) >= 1 && f.Code[0] != ' ' {
			letter = string(f.Code[0])
		} else if len(f.Code) >= 2 && f.Code[1] != ' ' {
			letter = string(f.Code[1])
		}
		if letter != "" {
			out[abs] = letter
		}
	}
	return out
}

// handleGitSidebarMouse processes a click inside the Git sidebar. x is
// sidebar-relative (0 = first column), y is absolute with the title at row 0.
//
//   - The tree/flat glyph on the title row toggles the view mode.
//   - An accordion section header (CHANGES / GRAPH) folds/unfolds it.
//   - A directory header folds/unfolds it.
//   - A changed file shows its diff (VSCode-style single click).
//   - A graph commit shows that commit's diff.
func (m Model) handleGitSidebarMouse(x, y int, t tea.MouseEventType) (tea.Model, tea.Cmd) {
	if t != tea.MouseLeft {
		return m, nil
	}
	m.focus = FocusExplorer
	if !m.gitIsRepo {
		return m, nil
	}
	contentW := m.explorerWidth - 1
	// "tree · flat" toggle lives on the right edge of the sub-header row.
	if y == gitSubheaderRow {
		if x >= contentW-gitViewToggleWidth {
			m.toggleGitViewMode()
		}
		return m, nil
	}
	// The title + hairline rows above the sub-header are inert.
	if y < gitSubheaderRow {
		return m, nil
	}
	// Map the click to a body row through the same scroll layout the renderer
	// used, so it lands on the row actually drawn there.
	top, bodyH, _ := m.gitPanelLayout(m.h)
	bodyRow := y - m.gitPanelTopOffset()
	if bodyRow < 0 || bodyRow >= bodyH {
		return m, nil
	}
	idx := top + bodyRow
	rows := m.gitPanelRows()
	if idx < 0 || idx >= len(rows) || !rows[idx].selectable() {
		return m, nil
	}
	m.gitCursor = idx
	r := rows[idx]
	switch r.kind {
	case gitRowSection:
		m.gitToggleSection(r.section)
	case gitRowDir:
		m.gitToggleCollapse(r.dirPath)
	case gitRowFile:
		return m, m.gitDiffCmd()
	case gitRowCommit:
		return m, m.gitShowCommitCmd(r.hash)
	}
	return m, nil
}
