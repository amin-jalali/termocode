package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Stash options and actions (GITX-10), reset from the GRAPH and
// uncommit (GITX-15). Dropping a stash and resetting ask first with
// Cancel ready; a hard reset also wants the branch name typed and saves
// the changes (a stash) and HEAD (a backup ref) before it runs.

const (
	confirmKindStashDrop confirmKindEnum = 3680
	confirmKindGitReset  confirmKindEnum = 3681

	pickerKindStashMode   pickerKindEnum = 3680
	pickerKindStashPick   pickerKindEnum = 3681
	pickerKindStashAction pickerKindEnum = 3682
	pickerKindResetMode   pickerKindEnum = 3683

	promptKindStashMessage   promptKindEnum = 3680
	promptKindStashBranch    promptKindEnum = 3681
	promptKindResetHardTyped promptKindEnum = 3682
)

// gitStashOpsState is what a stash or reset picker, card or prompt is
// about.
type gitStashOpsState struct {
	opts      git.StashOptions // the stash being made
	ref       string           // the stash picked ("stash@{0}")
	resetMode string
	resetTo   string // full hash
	resetTag  string // short hash for the text
	page      string // the last View Changes text (tests read it)
}

func (m *Model) gitOpsToast(err error, fail, ok, detail string) tea.Cmd {
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

func (m *Model) gitOpsPicker(kind pickerKindEnum, title string, items []picker.Item) {
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = kind
}

func (m *Model) gitOpsPrompt(kind promptKindEnum, title, label, value string) {
	m.prompt = prompt.New(title, label, value)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = kind
}

func (m *Model) gitOpsCard(kind confirmKindEnum, title, text string, act confirm.Button) {
	m.confirm = confirm.Risky(title, text, confirm.Button{ID: "cancel", Title: "Cancel"}, act)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = kind
}

// ── stash ─────────────────────────────────────────────────────────────

// openStashModePicker asks what to stash.
func (m *Model) openStashModePicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	items := []picker.Item{
		{ID: "stash-all", Title: "All changes", Hint: "new files stay"},
		{ID: "stash-untracked", Title: "All changes and new files", Hint: "-u"},
		{ID: "stash-staged", Title: "Staged changes only", Hint: "--staged"},
	}
	if _, rel, ok := m.gitLineTarget(); ok {
		items = append(items, picker.Item{ID: "stash-file", Title: "Only the current file", Hint: rel})
	}
	m.gitOpsPicker(pickerKindStashMode, " Stash what? ", items)
	return nil
}

func (m *Model) onStashModePicked(id string) tea.Cmd {
	o := git.StashOptions{}
	switch id {
	case "stash-all":
	case "stash-untracked":
		o.Untracked = true
	case "stash-staged":
		o.StagedOnly = true
	case "stash-file":
		_, rel, ok := m.gitLineTarget()
		if !ok {
			return nil
		}
		o.Paths = []string{rel}
	default:
		return nil
	}
	m.gitStashOps.opts = o
	m.gitOpsPrompt(promptKindStashMessage, "Stash", "Message (optional):", "")
	return nil
}

// openStashActionsPicker lists the stashes.
func (m *Model) openStashActionsPicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	repo, err := m.gitStashRepo()
	if err != nil {
		return m.gitOpsToast(err, "Stash list failed", "", "")
	}
	entries, err := repo.List()
	if err != nil {
		return m.gitOpsToast(err, "Stash list failed", "", "")
	}
	if len(entries) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No stashes")
		return c
	}
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		items = append(items, picker.Item{ID: e.Ref, Title: e.Message, Hint: e.Ref})
	}
	m.gitOpsPicker(pickerKindStashPick, " Stashes ", items)
	return nil
}

func (m *Model) onStashPicked(ref string) tea.Cmd {
	m.gitStashOps.ref = ref
	m.gitOpsPicker(pickerKindStashAction, " "+ref+" ", []picker.Item{
		{ID: "sa-apply", Title: "Apply", Hint: "keep the stash"},
		{ID: "sa-pop", Title: "Pop", Hint: "apply, then drop it"},
		{ID: "sa-diff", Title: "View Changes"},
		{ID: "sa-branch", Title: "Create Branch from Stash…"},
		{ID: "sa-drop", Title: "Drop…"},
	})
	return nil
}

func (m *Model) onStashActionPicked(id string) tea.Cmd {
	repo, err := m.gitStashRepo()
	if err != nil {
		return m.gitOpsToast(err, "Stash failed", "", "")
	}
	ref := m.gitStashOps.ref
	switch id {
	case "sa-apply":
		return m.gitOpsToast(repo.Apply(ref), "Apply failed", "Applied "+ref, "The stash stays")
	case "sa-pop":
		return m.gitOpsToast(repo.Pop(ref), "Pop failed", "Popped "+ref, "")
	case "sa-diff":
		d, err := repo.Diff(ref)
		if err != nil {
			return m.gitOpsToast(err, "Could not read the stash", "", "")
		}
		m.gitStashOps.page = d
		m.focus = FocusEditor
		m.ensureEditorWindowCurrent()
		if err := m.openReadOnlyPage("Stash "+ref, d, "", "diff"); err != nil {
			m.err = "stash: " + err.Error()
		}
	case "sa-branch":
		m.gitOpsPrompt(promptKindStashBranch, "Branch from "+ref, "New branch name (the stash is applied there, then dropped):", "")
	case "sa-drop":
		hash := repo.Hash(ref)
		m.gitOpsCard(confirmKindStashDrop, "Drop "+ref+"?",
			fmt.Sprintf("The stash is deleted. Its commit %s stays in git for a while: the toast says how to bring it back.", hash[:min(7, len(hash))]),
			confirm.Button{ID: "drop", Title: "Drop", Style: confirm.StyleDestructive})
	}
	return nil
}

// ── reset ─────────────────────────────────────────────────────────────

// gitResetFromRow asks which reset to the GRAPH commit under the cursor.
func (m *Model) gitResetFromRow() tea.Cmd {
	cur, ok := m.gitCurrentRow()
	if !ok || cur.kind != gitRowCommit {
		return nil
	}
	c := m.gitCommits[cur.ci]
	m.gitStashOps.resetTo, m.gitStashOps.resetTag = c.FullHash, c.Hash
	m.gitOpsPicker(pickerKindResetMode, " Reset "+gitResetBranchLabel(m)+" to "+c.Hash+" ", []picker.Item{
		{ID: "reset-soft", Title: "Soft", Hint: "keep the changes, staged"},
		{ID: "reset-mixed", Title: "Mixed", Hint: "keep the changes, not staged"},
		{ID: "reset-keep", Title: "Keep", Hint: "keep your local changes; stop if they are in the way"},
		{ID: "reset-hard", Title: "Hard…", Hint: "drop the changes (saved first)"},
	})
	return nil
}

func gitResetBranchLabel(m *Model) string {
	if b := pushBranch(m.gitBranch); b != "" {
		return b
	}
	return "HEAD"
}

func (m *Model) onResetModePicked(id string) tea.Cmd {
	mode, ok := strings.CutPrefix(id, "reset-")
	if !ok {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	s := &m.gitStashOps
	s.resetMode = mode
	old := git.StashHash(cwd, "HEAD")
	n := gitCountCommits(cwd, s.resetTo+"..HEAD")
	label := gitResetBranchLabel(m)
	text := fmt.Sprintf("%s moves to %s.", label, s.resetTag)
	if n > 0 {
		text += fmt.Sprintf(" %d commit%s leave the branch; `git reset %s` brings them back.", n, plural(n), old[:min(7, len(old))])
	}
	switch mode {
	case git.ResetSoft:
		text += " Their changes stay, staged."
	case git.ResetMixed:
		text += " Their changes stay in your files, not staged."
	case git.ResetKeep:
		text += " Your local changes stay; git stops if one is in the way."
	case git.ResetHard:
		text += " The changes in your files go: termocode saves them as a stash and HEAD as a backup ref first. You type " + label + " next."
	}
	title := "Reset " + label + " to " + s.resetTag + "?"
	m.gitOpsCard(confirmKindGitReset, title, text, confirm.Button{ID: "reset", Title: "Reset", Style: confirm.StyleDestructive})
	return nil
}

// onGitOpsConfirm answers this file's cards; Cancel does nothing.
func (m *Model) onGitOpsConfirm(kind confirmKindEnum, id string) tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	s := m.gitStashOps
	switch {
	case kind == confirmKindStashDrop && id == "drop":
		repo, err := m.gitStashRepo()
		if err != nil {
			return m.gitOpsToast(err, "Drop failed", "", "")
		}
		hash := repo.Hash(s.ref)
		name := ""
		if es, err := repo.List(); err == nil {
			for _, e := range es {
				if e.Ref == s.ref {
					name = e.Message
				}
			}
		}
		return m.gitOpsToast(repo.Drop(s.ref), "Drop failed", "Dropped "+s.ref,
			fmt.Sprintf("git stash store -m %q %s brings it back", name, hash))
	case kind == confirmKindGitReset && id == "reset":
		if s.resetMode == git.ResetHard {
			label := gitResetBranchLabel(m)
			m.gitOpsPrompt(promptKindResetHardTyped, "Hard reset "+label, "Type "+label+" to reset it hard to "+s.resetTag+":", "")
			return nil
		}
		old := git.StashHash(cwd, "HEAD")
		return m.gitOpsToast(git.Reset(cwd, s.resetMode, s.resetTo), "Reset failed",
			"Reset "+gitResetBranchLabel(m)+" to "+s.resetTag+" ("+s.resetMode+")", "git reset "+old[:min(7, len(old))]+" goes back")
	}
	return nil
}

// gitUncommit undoes the last commit, keeping its changes staged.
func (m *Model) gitUncommit() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	old := git.StashHash(cwd, "HEAD")
	subject, _ := runGitSubject(cwd)
	err = git.Uncommit(cwd)
	short := old[:min(7, len(old))]
	return m.gitOpsToast(err, "Uncommit failed", "Undid commit "+short+" "+subject, "Its changes are staged · git reset "+short+" brings it back")
}

func runGitSubject(cwd string) (string, error) {
	es, err := git.Log(cwd, 1)
	if err != nil || len(es) == 0 {
		return "", err
	}
	return es[0].Subject, nil
}

// ── prompts ───────────────────────────────────────────────────────────

// handleGitOpsPromptSubmit answers this file's prompts.
func (m *Model) handleGitOpsPromptSubmit(value string) (tea.Cmd, bool) {
	switch m.promptKind {
	case promptKindStashMessage, promptKindStashBranch, promptKindResetHardTyped:
	default:
		return nil, false
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil, true
	}
	value = strings.TrimSpace(value)
	s := &m.gitStashOps
	switch m.promptKind {
	case promptKindStashMessage:
		o := s.opts
		o.Message = value
		repo, err := m.gitStashRepo()
		if err == nil {
			err = repo.Push(o)
		}
		return m.gitOpsToast(err, "Stash failed", "Stashed changes", value), true
	case promptKindStashBranch:
		if value == "" {
			return nil, true
		}
		repo, err := m.gitStashRepo()
		if err == nil {
			err = repo.Branch(value, s.ref)
		}
		return m.gitOpsToast(err, "Branch from stash failed", "Created branch "+value, "from "+s.ref), true
	}
	label := gitResetBranchLabel(m)
	if value != label {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Name did not match", "Nothing was reset. Type "+label+" exactly.")
		return c, true
	}
	b, err := git.ResetHardSafe(cwd, s.resetTo)
	detail := "HEAD was " + b.Old + ", saved as " + b.Ref
	if b.Stashed {
		detail += " · your changes are in the stash list"
	}
	return m.gitOpsToast(err, "Hard reset failed", "Reset "+label+" hard to "+s.resetTag, detail), true
}

// gitCountCommits counts the commits of a range ("a..b"); -1 when
// unknown.
func gitCountCommits(cwd, rng string) int {
	cmd := spawn.Command("git", "rev-list", "--count", rng)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return -1
	}
	return n
}
