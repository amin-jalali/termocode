package app

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Whole-tree actions of the Source Control panel (GITX-7, GITX-20):
// Commit and Push / Sync, Unstage All, Discard All Changes and Delete
// Untracked Files. Both discards ask first on a Cancel-first card and
// keep a safety copy in the stash list, so nothing is lost for good.

const (
	confirmKindGitDiscardAll     confirmKindEnum = 3600
	confirmKindGitCleanUntracked confirmKindEnum = 3601
)

// What follows a commit (gitInlineCommitThen).
const (
	gitAfterPush = "push"
	gitAfterSync = "sync"
)

// gitAfterCommit starts the push or sync that follows a made commit.
func (m *Model) gitAfterCommit(after string) tea.Cmd {
	switch after {
	case gitAfterPush:
		return m.gitPush()
	case gitAfterSync:
		return m.gitSync()
	}
	return nil
}

// gitChangeCounts counts the changed tracked files and the new ones.
func (m Model) gitChangeCounts() (tracked, untracked int) {
	for _, f := range m.gitFiles {
		switch {
		case f.Untracked():
			untracked++
		case f.Staged() || f.Unstaged():
			tracked++
		}
	}
	return tracked, untracked
}

// gitUnstageAll takes every staged change out of the index.
func (m *Model) gitUnstageAll() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	var c tea.Cmd
	if err := git.UnstageAll(cwd); err != nil {
		m.toast, c = m.toast.PushDetail(toast.Errr, "Unstage all failed", err.Error())
	} else {
		m.toast, c = m.toast.Push(toast.Info, "Unstaged all changes")
	}
	return tea.Batch(fetchGitCmd(), c)
}

// openGitDiscardAllCard asks before taking every change away. Cancel is
// first and focused; the new files are only deleted by their own button.
func (m *Model) openGitDiscardAllCard() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	tracked, untracked := m.gitChangeCounts()
	if tracked == 0 && untracked == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "Nothing to discard")
		return c
	}
	buttons := []confirm.Button{{ID: "discard", Title: "Discard Changes", Style: confirm.StyleDestructive}}
	if untracked > 0 {
		buttons = append(buttons, confirm.Button{ID: "discard-new", Title: fmt.Sprintf("Discard and Delete %d New File%s", untracked, plural(untracked)), Style: confirm.StyleDestructive})
	}
	msg := fmt.Sprintf("%d changed file%s go back to the last commit; staged changes too.", tracked, plural(tracked))
	if untracked > 0 {
		msg += fmt.Sprintf(" %d new file%s stay unless you delete them too.", untracked, plural(untracked))
	}
	msg += " A safety copy goes to the stash list (Git: Show Stash List…)."
	m.confirm = confirm.Risky("Discard all changes?", msg, confirm.Button{ID: "cancel", Title: "Cancel"}, buttons...)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitDiscardAll
	return nil
}

// openGitCleanUntrackedCard asks before deleting the new files.
func (m *Model) openGitCleanUntrackedCard() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	_, untracked := m.gitChangeCounts()
	if untracked == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No untracked files")
		return c
	}
	m.confirm = confirm.Risky("Delete untracked files?",
		fmt.Sprintf("%d new file%s (not ignored ones) are removed. A safety copy goes to the stash list (Git: Show Stash List…).", untracked, plural(untracked)),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "delete", Title: "Delete Untracked Files", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitCleanUntracked
	return nil
}

// onGitBulkConfirm runs the answer of a Discard All or Delete Untracked
// card; Cancel (and any other answer) does nothing.
func (m *Model) onGitBulkConfirm(kind confirmKindEnum, id string) tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	var ref string
	switch {
	case kind == confirmKindGitDiscardAll && (id == "discard" || id == "discard-new"):
		ref, err = git.DiscardAll(cwd, id == "discard-new")
	case kind == confirmKindGitCleanUntracked && id == "delete":
		ref, err = git.DeleteUntracked(cwd)
	default:
		return nil
	}
	var c tea.Cmd
	switch {
	case errors.Is(err, git.ErrNothingToDiscard):
		m.toast, c = m.toast.Push(toast.Info, "Nothing to discard")
	case err != nil:
		m.toast, c = m.toast.PushDetail(toast.Errr, "Discard failed", err.Error())
	case kind == confirmKindGitCleanUntracked:
		m.toast, c = m.toast.PushDetail(toast.Info, "Deleted untracked files", "safety copy: "+ref)
	default:
		m.toast, c = m.toast.PushDetail(toast.Info, "Discarded all changes", "safety copy: "+ref)
	}
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	return tea.Batch(fetchGitCmd(), c)
}
