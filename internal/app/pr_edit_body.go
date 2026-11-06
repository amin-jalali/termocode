package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/toast"
)

// GITX-23 Edit Description…: the description goes to a file in the git
// folder and opens in the editor (like Git: Edit Commit Message in
// Editor). Each save shows a card, Cancel first, that sends the file with
// `gh pr edit --body-file`. The text never goes on a command line.

const confirmKindPREditBody confirmKindEnum = 3217

// prBodyEdit is the open description file.
type prBodyEdit struct {
	number int
	path   string
	mod    time.Time // the last change seen (a newer one asks)
}

// prEditDescription writes the description file and opens it.
func (m *Model) prEditDescription() tea.Cmd {
	d := m.prS().detail
	if d == nil || d.State != "OPEN" {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	path, err := gitPathIn(cwd, fmt.Sprintf("TERMOCODE_PR_%d.md", d.Number))
	if err != nil {
		return m.prFailToast("No git folder", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimRight(d.Body, "\n")+"\n"), 0o600); err != nil {
		return m.prFailToast("Could not write the description file", err)
	}
	e := &prBodyEdit{number: d.Number, path: path}
	if st, err := os.Stat(path); err == nil {
		e.mod = st.ModTime()
	}
	m.prS().editBody = e
	m.confirmOpen = false // the view card closes: the user writes in the editor
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`vim.cmd('edit ' .. vim.fn.fnameescape(...)); vim.bo.filetype = 'markdown'`, path)
		m.focus = FocusEditor
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "Edit the description of "+prRef(d.Number)+", then save",
		"Each save asks before it updates GitHub")
	return c
}

// syncPRBodyFile asks to send the description after a save.
func (m *Model) syncPRBodyFile() {
	if m.pr == nil || m.pr.editBody == nil || m.confirmOpen {
		return
	}
	e := m.pr.editBody
	st, err := os.Stat(e.path)
	if err != nil {
		m.pr.editBody = nil
		return
	}
	if !st.ModTime().After(e.mod) {
		return
	}
	e.mod = st.ModTime()
	b, err := os.ReadFile(e.path)
	if err != nil {
		return
	}
	text := "Update the description of " + prRef(e.number) + " on GitHub with the saved text?\n\n" +
		clipLines(strings.TrimSpace(string(b)), prBodyLines)
	m.confirm = confirm.Risky("Update "+prRef(e.number)+" Description", text,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "update-body", Title: "Update Description", Style: confirm.StylePrimary}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindPREditBody
}

// answerPREditBody sends the file, or keeps it for the next save.
func (m *Model) answerPREditBody(id string) tea.Cmd {
	e := m.prS().editBody
	if id != "update-body" || e == nil {
		return nil
	}
	n, path := e.number, e.path
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		err := ghci.EditBody(ctx, run, dir, n, path)
		return prDoneMsg{number: n, ok: "Updated the description of " + prRef(n), failed: "Could not update " + prRef(n), err: err, reopen: true}
	})
}
