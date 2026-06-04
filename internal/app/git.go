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
}

// fetchGitCmd asynchronously runs git status + branch and returns a GitMsg.
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
		return GitMsg{IsRepo: true, Branch: branch, Files: files}
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
//   - Clicking the tree/flat glyph on the title row toggles the view mode.
//   - Clicking a directory header folds/unfolds it.
//   - Clicking a changed file shows its diff (VSCode-style single click).
func (m Model) handleGitSidebarMouse(x, y int, t tea.MouseEventType) (tea.Model, tea.Cmd) {
	if t != tea.MouseLeft {
		return m, nil
	}
	m.focus = FocusExplorer
	if !m.gitIsRepo {
		return m, nil
	}
	// Title-row tree/flat toggle glyph lives in the right ~2 cells.
	if y == 0 {
		if x >= m.explorerWidth-3 {
			m.toggleGitViewMode()
		}
		return m, nil
	}
	if len(m.gitFiles) == 0 {
		return m, nil
	}
	// Header layout: title (1) + branch line (1 if present) + blank (1) + count (1) + rows...
	header := 3
	if m.gitBranch.Name != "" {
		header = 4
	}
	idx := y - header
	rows := m.gitVisibleRows()
	if idx < 0 || idx >= len(rows) {
		return m, nil
	}
	m.gitCursor = idx
	if rows[idx].IsDir {
		m.gitToggleCollapse(rows[idx].DirPath)
		return m, nil
	}
	return m, m.gitDiffCmd()
}
