package app

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Branch actions (GITX-2): rename the current branch, delete a branch
// (safe first, then a second card for a force delete), delete a branch on
// the remote (card, then the typed name), create a branch from any
// branch, tag or GRAPH commit, and check a commit out on its own.

const (
	confirmKindGitDeleteBranch      confirmKindEnum = 3620
	confirmKindGitForceDeleteBranch confirmKindEnum = 3621
	confirmKindGitDeleteRemote      confirmKindEnum = 3622

	pickerKindDeleteBranch pickerKindEnum = 3620
	pickerKindBranchFrom   pickerKindEnum = 3621

	promptKindGitRenameBranch      promptKindEnum = 3620
	promptKindGitBranchFrom        promptKindEnum = 3621
	promptKindGitDeleteRemoteTyped promptKindEnum = 3622
)

// gitBranchToast shows err as a failure titled fail, else ok / detail.
func (m *Model) gitBranchToast(err error, fail, ok, detail string) tea.Cmd {
	var c tea.Cmd
	switch {
	case err != nil:
		m.toast, c = m.toast.PushDetail(toast.Errr, fail, err.Error())
	case detail != "":
		m.toast, c = m.toast.PushDetail(toast.Info, ok, detail)
	default:
		m.toast, c = m.toast.Push(toast.Info, ok)
	}
	if err == nil && m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	return tea.Batch(fetchGitCmd(), c)
}

// openRenameBranchPrompt asks for the current branch's new name.
func (m *Model) openRenameBranchPrompt() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	name := pushBranch(m.gitBranch)
	if name == "" {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Warn, "No branch to rename (detached HEAD)")
		return c
	}
	m.gitBranchRef = name
	m.prompt = prompt.New("Rename Branch", "New name for "+name+":", name)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindGitRenameBranch
	return nil
}

// gitDeleteBranchItems are the delete picker's rows: local branches but
// the current one ("del-"), then remote ones ("delr-").
func gitDeleteBranchItems(local, remote []string, current string) []picker.Item {
	var items []picker.Item
	for _, b := range local {
		if b != current {
			items = append(items, picker.Item{ID: "del-" + b, Title: b})
		}
	}
	for _, b := range remote {
		items = append(items, picker.Item{ID: "delr-" + b, Title: b, Hint: "on the remote"})
	}
	return items
}

// openDeleteBranchPicker shows the branches that can be deleted.
func (m *Model) openDeleteBranchPicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	local, err := git.Branches(cwd)
	if err != nil {
		return m.gitBranchToast(err, "Branches failed", "", "")
	}
	items := gitDeleteBranchItems(local, git.RemoteTrackingBranches(cwd), m.gitBranch.Name)
	if len(items) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No other branch to delete")
		return c
	}
	m.picker = picker.NewItems(" Delete which branch? ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDeleteBranch
	return nil
}

// onDeleteBranchPicked asks before deleting; Cancel is first and ready.
func (m *Model) onDeleteBranchPicked(id string) tea.Cmd {
	if ref, ok := strings.CutPrefix(id, "delr-"); ok {
		m.confirm = confirm.Risky("Delete "+ref+" on the remote?",
			"The branch goes for everyone who uses this remote. Your local branches stay. You type the name next to confirm.",
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "delete-remote", Title: "Delete on Remote…", Style: confirm.StyleDestructive})
		m.openBranchCard(confirmKindGitDeleteRemote, ref)
		return nil
	}
	name, ok := strings.CutPrefix(id, "del-")
	if !ok {
		return nil
	}
	m.confirm = confirm.Risky("Delete branch "+name+"?",
		"Only a branch whose commits are on another branch is deleted. If "+name+" has commits of its own, termocode asks again.",
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive})
	m.openBranchCard(confirmKindGitDeleteBranch, name)
	return nil
}

func (m *Model) openBranchCard(kind confirmKindEnum, ref string) {
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = kind
	m.gitBranchRef = ref
}

// onBranchConfirm runs a branch card's answer; Cancel does nothing.
func (m *Model) onBranchConfirm(kind confirmKindEnum, id string) tea.Cmd {
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	name := m.gitBranchRef
	switch {
	case kind == confirmKindGitDeleteBranch && id == "delete":
		tip := git.BranchTip(cwd, name)
		err := git.DeleteBranch(cwd, name, false)
		if errors.Is(err, git.ErrBranchNotMerged) {
			m.confirm = confirm.Risky(name+" is not merged",
				fmt.Sprintf("%s has commits no other branch has. Force delete drops the branch; `git branch %s %s` brings it back while git keeps the commits.", name, name, tip),
				confirm.Button{ID: "cancel", Title: "Cancel"},
				confirm.Button{ID: "force-delete", Title: "Force Delete", Style: confirm.StyleDestructive})
			m.openBranchCard(confirmKindGitForceDeleteBranch, name)
			return nil
		}
		return m.gitBranchToast(err, "Delete failed", "Deleted branch "+name, "was at "+tip)
	case kind == confirmKindGitForceDeleteBranch && id == "force-delete":
		tip := git.BranchTip(cwd, name)
		return m.gitBranchToast(git.DeleteBranch(cwd, name, true), "Delete failed", "Deleted branch "+name,
			fmt.Sprintf("was at %s · git branch %s %s brings it back", tip, name, tip))
	case kind == confirmKindGitDeleteRemote && id == "delete-remote":
		m.prompt = prompt.New("Delete "+name+" on the remote", "Type "+name+" to delete it:", "")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindGitDeleteRemoteTyped
	}
	return nil
}

// openBranchFromPicker shows the branches and tags a new branch can
// start at.
func (m *Model) openBranchFromPicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	local, err := git.Branches(cwd)
	if err != nil {
		return m.gitBranchToast(err, "Branches failed", "", "")
	}
	var items []picker.Item
	for _, b := range local {
		items = append(items, picker.Item{ID: "from-" + b, Title: b, Hint: "branch"})
	}
	for _, b := range git.RemoteTrackingBranches(cwd) {
		items = append(items, picker.Item{ID: "from-" + b, Title: b, Hint: "remote"})
	}
	tags, _ := git.Tags(cwd)
	for _, tg := range tags {
		items = append(items, picker.Item{ID: "from-" + tg, Title: tg, Hint: "tag"})
	}
	m.picker = picker.NewItems(" Create a branch from… ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindBranchFrom
	return nil
}

// openBranchNamePrompt asks the name of a new branch starting at ref.
func (m *Model) openBranchNamePrompt(ref, label string) {
	m.gitBranchRef = ref
	m.prompt = prompt.New("New Branch", "Branch name (starts at "+label+"):", "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindGitBranchFrom
}

// onBranchFromPicked asks the new branch's name.
func (m *Model) onBranchFromPicked(id string) tea.Cmd {
	if ref, ok := strings.CutPrefix(id, "from-"); ok {
		m.openBranchNamePrompt(ref, ref)
	}
	return nil
}

// handleBranchPromptSubmit answers this file's prompts.
func (m *Model) handleBranchPromptSubmit(value string) (tea.Cmd, bool) {
	switch m.promptKind {
	case promptKindGitRenameBranch, promptKindGitBranchFrom, promptKindGitDeleteRemoteTyped:
	default:
		return nil, false
	}
	value = strings.TrimSpace(value)
	cwd, err := m.gitRootDir()
	if err != nil || value == "" {
		return nil, true
	}
	ref := m.gitBranchRef
	switch m.promptKind {
	case promptKindGitRenameBranch:
		if value == ref {
			return nil, true
		}
		return m.gitBranchToast(git.RenameBranch(cwd, ref, value), "Rename failed", "Renamed "+ref+" to "+value, ""), true
	case promptKindGitBranchFrom:
		return m.gitBranchToast(git.CreateBranchAt(cwd, value, ref), "Branch failed", "Created branch "+value, "starts at "+ref), true
	}
	if value != ref {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Name did not match", "Nothing was deleted. Type "+ref+" exactly.")
		return c, true
	}
	return m.gitRemoteOp("Deleting "+ref+"…", func(cwd string) gitRemoteDoneMsg {
		return gitRemoteDoneMsg{failTitle: "Delete failed", okText: "Deleted " + ref + " on the remote", err: git.DeleteRemoteBranch(cwd, ref)}
	}), true
}

// gitBranchHereFromRow asks a branch name for the GRAPH commit under
// the cursor.
func (m *Model) gitBranchHereFromRow() tea.Cmd {
	if cur, ok := m.gitCurrentRow(); ok && cur.kind == gitRowCommit {
		c := m.gitCommits[cur.ci]
		m.openBranchNamePrompt(c.FullHash, c.Hash)
	}
	return nil
}

// gitCheckoutCommitFromRow checks the GRAPH commit under the cursor out
// on its own (detached HEAD); git refuses when local changes are in the
// way.
func (m *Model) gitCheckoutCommitFromRow() tea.Cmd {
	cur, ok := m.gitCurrentRow()
	if !ok || cur.kind != gitRowCommit {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	c := m.gitCommits[cur.ci]
	return m.gitBranchToast(git.CheckoutDetached(cwd, c.FullHash), "Checkout failed", "Detached HEAD at "+c.Hash,
		"Create Branch Here… keeps work you commit here")
}
