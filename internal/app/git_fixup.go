package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Fixup into… (G-RBS): the staged changes become part of an older
// commit — `git commit --fixup` and an autosquash rebase, after the same
// kind of confirm as an interactive rebase.

// fixupCheckMsg is what the fixup confirm says.
type fixupCheckMsg struct {
	target, short, subject string
	commits                int    // commits the autosquash replays
	upstream               string // the branch's upstream
	pushed                 int    // replayed commits on it already
	err                    error
}

// gitFixupIntoCmd starts a fixup of the staged changes into commit full.
func (m *Model) gitFixupIntoCmd(full string) tea.Cmd {
	if full == "" {
		return nil
	}
	if m.gitRebase.InProgress {
		return m.rebaseBusyToast()
	}
	if staged, _ := m.gitFileCounts(); staged == 0 {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Nothing is staged",
			"Stage the changes that belong in the older commit first.")
		return cmd
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		msg := fixupCheckMsg{target: full}
		base, _ := git.ParentOf(cwd, full)
		todo, err := git.LoadRebaseTodo(cwd, base)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.commits = len(todo.Items)
		msg.short, msg.subject = todo.Items[0].Hash, todo.Items[0].Subject
		msg.upstream, msg.pushed = git.RebasePushed(cwd, base)
		return msg
	}
}

// openFixupConfirm asks before the fixup: Cancel first and focused.
func (m *Model) openFixupConfirm(msg fixupCheckMsg) tea.Cmd {
	if msg.err != nil {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "Can't fix up into this commit", msg.err.Error())
		return cmd
	}
	staged, unstaged := 0, false
	for _, f := range m.gitFiles {
		if f.Staged() {
			staged++
		}
		if f.Unstaged() {
			unstaged = true
		}
	}
	lines := []string{
		fmt.Sprintf("Fold the staged changes (%d %s) into %s %q.", staged, fileWord(staged), msg.short, msg.subject),
		fmt.Sprintf("This commits them as a fixup and replays %s.", rebaseCount(msg.commits)),
	}
	if msg.pushed > 0 {
		lines = append(lines, "", fmt.Sprintf("⚠ %d of these commits are on %s already; you will need to force-push.", msg.pushed, msg.upstream),
			"termocode never force-pushes for you.")
	}
	run := confirm.Button{ID: "fixup", Title: "Fixup", Style: confirm.StyleDestructive}
	if unstaged {
		lines = append(lines, "", "Unstaged changes are stashed and put back after.")
		run = confirm.Button{ID: "fixup-stash", Title: "Stash and Fixup", Style: confirm.StyleDestructive}
	}
	lines = append(lines, "", "Undo afterwards: git reset --hard ORIG_HEAD")
	m.fixupTarget = msg.target
	m.confirm = confirm.Risky("Fixup into "+msg.short+"?", strings.Join(lines, "\n"),
		confirm.Button{ID: "cancel", Title: "Cancel"}, run)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindRebaseFixup
	return nil
}

func fileWord(n int) string {
	if n == 1 {
		return "file"
	}
	return "files"
}

// onFixupConfirm runs the fixup off the loop.
func (m *Model) onFixupConfirm(id string) tea.Cmd {
	target := m.fixupTarget
	m.fixupTarget = ""
	if target == "" || (id != "fixup" && id != "fixup-stash") {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	opt := git.RebaseOptions{Autostash: id == "fixup-stash"}
	return func() tea.Msg {
		out, err := git.FixupInto(cwd, target, opt)
		return rebaseDoneMsg{summary: "staged changes folded into " + target[:min(7, len(target))], out: out, err: err}
	}
}
