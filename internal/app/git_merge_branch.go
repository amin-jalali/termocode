package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Merge a branch into the current one (GITX-1): Git: Merge Branch… picks
// a branch, a Cancel-first card says how many commits come in, the merge
// runs off the loop. A merge, cherry-pick or revert that stopped shows a
// banner in Source Control; its card (Close first) aborts or goes on.

const (
	confirmKindGitMergeBranch confirmKindEnum = 3610
	confirmKindGitOpControl   confirmKindEnum = 3611

	pickerKindMergeBranch pickerKindEnum = 3610
)

// gitMergeDoneMsg is the end of a merge.
type gitMergeDoneMsg struct {
	ref, into string
	conflicts []string
	err       error
}

// gitOpDoneMsg is the end of Continue or Abort of a stopped op.
type gitOpDoneMsg struct {
	op, verb string
	err      error
}

// gitMergeBranchItems are the picker rows: every branch but the current
// one, local first, then remote-tracking ones.
func gitMergeBranchItems(local, remote []string, current string) []picker.Item {
	var items []picker.Item
	for _, b := range local {
		if b != current {
			items = append(items, picker.Item{ID: "mg-" + b, Title: b})
		}
	}
	for _, b := range remote {
		items = append(items, picker.Item{ID: "mg-" + b, Title: b, Hint: "remote"})
	}
	return items
}

// openMergeBranchPicker shows the branches to merge into the current one.
func (m *Model) openMergeBranchPicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	var c tea.Cmd
	if m.gitStopped != "" || m.gitRebase.InProgress {
		m.toast, c = m.toast.Push(toast.Warn, "Finish or abort the "+m.gitStoppedName()+" first")
		return c
	}
	local, err := git.Branches(cwd)
	if err != nil {
		m.toast, c = m.toast.PushDetail(toast.Errr, "Branches failed", err.Error())
		return c
	}
	remote, _ := git.RemoteBranches(cwd)
	items := gitMergeBranchItems(local, remote, m.gitBranch.Name)
	if len(items) == 0 {
		m.toast, c = m.toast.Push(toast.Info, "No other branch to merge")
		return c
	}
	m.picker = picker.NewItems(" Merge which branch into "+m.gitBranch.Name+"? ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMergeBranch
	return nil
}

// gitStoppedName is the op the banner is about ("merge" when unsure).
func (m Model) gitStoppedName() string {
	if m.gitRebase.InProgress {
		return "rebase"
	}
	if m.gitStopped == "" {
		return "merge"
	}
	return m.gitStopped
}

// onMergeBranchPicked asks before merging ref: Cancel first, then Merge
// (fast-forward when it can) and Merge (no fast-forward).
func (m *Model) onMergeBranchPicked(id string) tea.Cmd {
	ref, ok := strings.CutPrefix(id, "mg-")
	if !ok {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	var c tea.Cmd
	n, err := git.MergeIncoming(cwd, ref)
	switch {
	case err != nil:
		m.toast, c = m.toast.PushDetail(toast.Errr, "Can't merge "+ref, err.Error())
		return c
	case n == 0:
		m.toast, c = m.toast.PushDetail(toast.Info, "Already up to date", ref+" is merged into "+m.gitBranch.Name)
		return c
	}
	come := "come"
	if n == 1 {
		come = "comes"
	}
	msg := fmt.Sprintf("%d commit%s of %s %s into %s.", n, plural(n), ref, come, m.gitBranch.Name)
	if _, changed := m.gitFileCounts(); changed > 0 {
		msg += " You have local changes: git stops if the merge would change them."
	}
	msg += "\nIf files conflict, the merge stops: fix them in CONFLICTS, then Continue (or Abort)."
	m.confirm = confirm.Risky("Merge "+ref+" into "+m.gitBranch.Name+"?", msg,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "merge", Title: "Merge", Style: confirm.StylePrimary},
		confirm.Button{ID: "merge-no-ff", Title: "Merge (No Fast-Forward)"})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitMergeBranch
	m.confirmTargetPath = ref
	return nil
}

// onMergeBranchConfirm runs the merge off the loop (hooks may be slow).
func (m *Model) onMergeBranchConfirm(id string) tea.Cmd {
	if id != "merge" && id != "merge-no-ff" {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	ref, into := m.confirmTargetPath, m.gitBranch.Name
	return func() tea.Msg {
		conflicts, err := git.MergeBranch(cwd, ref, id == "merge-no-ff")
		return gitMergeDoneMsg{ref: ref, into: into, conflicts: conflicts, err: err}
	}
}

// onGitMergeDone tells how the merge went and reloads the git state.
func (m *Model) onGitMergeDone(msg gitMergeDoneMsg) tea.Cmd {
	var c tea.Cmd
	switch {
	case len(msg.conflicts) > 0:
		m.toast, c = m.toast.PushDetail(toast.Warn, "Merge stopped: "+conflictWord(len(msg.conflicts)),
			"Fix them in CONFLICTS, then Continue in the merge banner.")
	case msg.err != nil:
		m.toast, c = m.toast.PushDetail(toast.Errr, "Merge failed", msg.err.Error())
	default:
		m.toast, c = m.toast.Push(toast.Info, "Merged "+msg.ref+" into "+msg.into)
	}
	if m.nvim != nil {
		m.nvimCheckTimeAll()
	}
	return tea.Batch(fetchGitCmd(), c)
}

// gitStoppedConflicts lists the conflicted files of the snapshot.
func (m Model) gitStoppedConflicts() []string {
	var out []string
	for _, f := range m.gitFiles {
		if f.Conflicted() {
			out = append(out, f.Path)
		}
	}
	return out
}

// gitStoppedVerb is the banner's word for op.
func gitStoppedVerb(op string) string {
	switch op {
	case "cherry-pick":
		return "Cherry-picking"
	case "revert":
		return "Reverting"
	}
	return "Merging"
}

// renderGitStoppedRow draws the banner of a stopped merge / pick / revert.
func (m Model) renderGitStoppedRow(active, focused bool, w int) string {
	bg := gitSelBg(active, focused)
	label := "⇄ " + gitStoppedVerb(m.gitStopped)
	if n := len(m.gitStoppedConflicts()); n > 0 {
		label += " · " + conflictWord(n)
	} else {
		label += " · ready to finish"
	}
	label += " ›"
	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg))
	sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(theme.LG(theme.AccentAmber)).Bold(true).Render("  " + label))
	return clampSidebarRow(sb.String(), 1+2+runewidth.StringWidth(label), w, lipgloss.NewStyle().Background(bg))
}

// openGitOpControl asks what to do with the stopped op. Close is first
// and focused, so Enter or Esc never throw conflict fixes away.
func (m *Model) openGitOpControl() tea.Cmd {
	if m.gitStopped == "" {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No merge in progress")
		return c
	}
	op := m.gitStopped
	var lines []string
	if cs := m.gitStoppedConflicts(); len(cs) > 0 {
		lines = append(lines, conflictWord(len(cs))+": "+strings.Join(cs, ", "),
			"Fix them in CONFLICTS, then Continue (it stages the fixed files).")
	} else {
		lines = append(lines, "No conflicts left. Continue makes the commit.")
	}
	lines = append(lines, "", "Abort puts the branch back as it was before the "+op+".")
	m.confirm = confirm.Risky(strings.ToUpper(op[:1])+op[1:]+" in progress", strings.Join(lines, "\n"),
		confirm.Button{ID: "op-close", Title: "Close"},
		confirm.Button{ID: "op-abort", Title: "Abort", Style: confirm.StyleDestructive},
		confirm.Button{ID: "op-continue", Title: "Continue", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitOpControl
	return nil
}

// onGitOpControl runs Continue or Abort off the loop; Close does nothing.
func (m *Model) onGitOpControl(id string) tea.Cmd {
	verb := strings.TrimPrefix(id, "op-")
	if verb != "continue" && verb != "abort" {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	op := m.gitStopped
	return func() tea.Msg {
		msg := gitOpDoneMsg{op: op, verb: verb}
		if verb == "abort" {
			msg.err = git.OpAbort(cwd, op)
		} else {
			msg.err = git.OpContinue(cwd, op)
		}
		return msg
	}
}

// onGitOpDone tells how Continue / Abort went.
func (m *Model) onGitOpDone(msg gitOpDoneMsg) tea.Cmd {
	name := strings.ToUpper(msg.op[:1]) + msg.op[1:]
	var c tea.Cmd
	switch {
	case msg.err != nil:
		m.toast, c = m.toast.PushDetail(toast.Errr, "Can't "+msg.verb+" the "+msg.op, msg.err.Error())
	case msg.verb == "abort":
		m.toast, c = m.toast.Push(toast.Info, name+" aborted")
	default:
		m.toast, c = m.toast.Push(toast.Info, name+" finished")
	}
	if m.nvim != nil {
		m.nvimCheckTimeAll()
	}
	return tea.Batch(fetchGitCmd(), c)
}

// onGitMergeBranchMsg routes this file's messages.
func (m *Model) onGitMergeBranchMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case gitMergeDoneMsg:
		return m.onGitMergeDone(msg)
	case gitOpDoneMsg:
		return m.onGitOpDone(msg)
	}
	return nil
}
