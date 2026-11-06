package app

import (
	"context"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
)

// GITX-23: Close Pull Request… (asks first), Reopen, Re-run Failed
// Checks. Edit Description… is in pr_edit_body.go.

const confirmKindPRClose confirmKindEnum = 3216

// prAskClose shows the close card: Cancel first and focused.
func (m *Model) prAskClose() tea.Cmd {
	d := m.prS().detail
	if d == nil || d.State != "OPEN" {
		return nil
	}
	text := "Close " + prRef(d.Number) + " \"" + d.Title + "\" without merging?\n" +
		"The branch " + d.Branch + " stays. You can reopen the pull request later."
	m.confirm = confirm.Risky("Close "+prRef(d.Number), text,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "close-pr", Title: "Close Pull Request", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindPRClose
	return nil
}

// answerPRClose runs the close card.
func (m *Model) answerPRClose(id string) tea.Cmd {
	if id != "close-pr" {
		return nil
	}
	return m.prSetClosed(true)
}

// prSetClosed closes or reopens the pull request in the view.
func (m *Model) prSetClosed(closed bool) tea.Cmd {
	d := m.prS().detail
	if d == nil {
		return nil
	}
	n := d.Number
	ok, failed := "Closed "+prRef(n), "Could not close "+prRef(n)
	if !closed {
		ok, failed = "Reopened "+prRef(n), "Could not reopen "+prRef(n)
	}
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		err := ghci.SetClosed(ctx, run, dir, n, closed)
		return prDoneMsg{number: n, ok: ok, failed: failed, err: err, reopen: true}
	})
}

// prRerunFailed re-runs the failed workflow runs of the pull request.
func (m *Model) prRerunFailed() tea.Cmd {
	d := m.prS().detail
	if d == nil {
		return nil
	}
	ids := ghci.FailedRunIDs(d.Checks)
	if len(ids) == 0 {
		return m.aiInfo("No failed GitHub Actions run to re-run")
	}
	n := d.Number
	ok := "Re-running " + strconv.Itoa(len(ids)) + " failed run" + plural(len(ids)) + " of " + prRef(n)
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		err := ghci.RerunFailed(ctx, run, dir, ids)
		return prDoneMsg{number: n, ok: ok, detail: "The checks turn yellow while they run", failed: "Could not re-run the checks of " + prRef(n), err: err, ci: true}
	})
}
