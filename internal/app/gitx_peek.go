package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Gutter peek (GITX-21): a click on the git mark of a line (or Git: Peek
// Change on the cursor line) opens a card with that change in colour.
// Close is first and has the focus; Stage (Unstage for a staged change)
// and Next act at once; Revert asks again on a Cancel-first card.

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-peek-change", Title: "Git: Peek Change Under Cursor"},
		},
		policies: map[string]aiActionPolicy{
			"git-peek-change": aiNever("opens a card for the user to read; its Revert asks the user"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id != "git-peek-change" {
				return nil, false
			}
			return m.gitPeekAt(m.cursorLine), true
		},
	})
	// TcGitMarkClick (git_signs_lua.go) sends the clicked line and file.
	registerNotifyHandler("termocode_git_peek", func(m *Model, args []any) tea.Cmd {
		return m.gitPeekFile(notifyArgString(args, 1), notifyArgInt(args, 0))
	})
}

// gitPeekAt opens the peek card for line of the open file.
func (m *Model) gitPeekAt(line int) tea.Cmd { return m.gitPeekFile("", line) }

// gitPeekFile opens the peek card for line of path ("" = the open file).
func (m *Model) gitPeekFile(path string, line int) tea.Cmd {
	if path == "" {
		path = m.editor.Path()
	}
	cwd, err := m.gitRootDir()
	if path == "" || line <= 0 || err != nil {
		return nil
	}
	rel := path
	if r, e := filepath.Rel(cwd, path); e == nil && !strings.HasPrefix(r, "..") {
		rel = r
	}
	var c tea.Cmd
	// Stage and Revert cut the hunk from the saved file: unsaved edits
	// would move its lines.
	if m.gitPeekUnsaved(path) {
		m.toast, c = m.toast.PushDetail(toast.Warn, "Save the file first", "The change is read from the saved file")
		return c
	}
	p, err := git.PeekHunk(cwd, rel, line)
	if err != nil {
		m.toast, c = m.toast.PushDetail(toast.Info, fmt.Sprintf("No change on line %d", line), filepath.Base(rel))
		return c
	}
	m.openGitPeekCard(cwd, rel, line, p)
	return nil
}

// gitPeekUnsaved reports whether the editor holds unsaved edits of path.
func (m *Model) gitPeekUnsaved(path string) bool {
	if m.nvim == nil {
		return false
	}
	s, err := m.nvim.EvalLuaStringArgs(`local b = vim.fn.bufnr(...)
if b <= 0 then return '' end
return vim.bo[b].modified and '1' or ''`, path)
	return err == nil && s == "1"
}

func (m *Model) openGitPeekCard(cwd, rel string, line int, p git.HunkPeek) {
	title := fmt.Sprintf("Change %d of %d · %s", p.Index, p.Count, filepath.Base(rel))
	text := rel + "  " + strings.TrimSpace(p.Header)
	if p.Staged {
		text += "\nThis change is staged."
	}
	stage := confirm.Button{ID: "stage", Title: "Stage", Style: confirm.StylePrimary}
	if p.Staged {
		stage = confirm.Button{ID: "unstage", Title: "Unstage"}
	}
	buttons := []confirm.Button{stage}
	if !p.Staged {
		buttons = append(buttons, confirm.Button{ID: "revert", Title: "Revert…", Style: confirm.StyleDestructive})
	}
	if p.Count > 1 {
		buttons = append(buttons, confirm.Button{ID: "next", Title: "Next Change"})
	}
	m.confirm = confirm.Risky(title, text, confirm.Button{ID: "close", Title: "Close"}, buttons...).WithDiff(p.Lines)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitx
	m.gitx.onConfirm = func(m *Model, id string) tea.Cmd {
		switch id {
		case "stage":
			return m.gitPeekDone(git.StageHunk(cwd, rel, line), "Staged change", rel, false)
		case "unstage":
			return m.gitPeekDone(git.UnstageHunk(cwd, rel, line), "Unstaged change", rel, false)
		case "revert":
			m.gitxCard("Revert this change?",
				fmt.Sprintf("This puts back the text of \"%s\" around line %d as the index has it. Your edits in this change are lost. This cannot be undone.", rel, line),
				func(m *Model, id string) tea.Cmd {
					if id != "revert" {
						return nil
					}
					return m.gitPeekDone(git.DiscardHunk(cwd, rel, line), "Reverted change", rel, true)
				},
				confirm.Button{ID: "revert", Title: "Revert", Style: confirm.StyleDestructive})
		case "next":
			next := gitPeekNext(p.Starts, line)
			if m.nvim != nil {
				_ = m.nvim.Command(fmt.Sprintf("silent! %d", next))
			}
			m.cursorLine = next
			return m.gitPeekFile(filepath.Join(cwd, rel), next)
		}
		return nil
	}
}

// gitPeekNext is the first hunk start below line, or the first one.
func gitPeekNext(starts []int, line int) int {
	for _, s := range starts {
		if s > line {
			return s
		}
	}
	if len(starts) > 0 {
		return starts[0]
	}
	return line
}

// gitPeekDone reports a Stage / Unstage / Revert and refreshes git.
func (m *Model) gitPeekDone(err error, verb, rel string, reload bool) tea.Cmd {
	var c tea.Cmd
	if err != nil {
		m.toast, c = m.toast.PushDetail(toast.Errr, "Git", err.Error())
		return c
	}
	if reload && m.nvim != nil {
		m.nvimCheckTimeAll()
	}
	m.toast, c = m.toast.PushDetail(toast.Info, verb, filepath.Base(rel))
	return tea.Batch(fetchGitCmd(), c)
}
