package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// G-WT: Review, Merge and Discard of a finished step's own worktree.
// Review shows what the agent changed (a read-only diff card). Merge
// writes the agent's files into the user's tree; when the user changed
// one of them too, nothing is written and a card asks (Cancel first):
// merge with conflict markers, or keep the work on the branch. Discard
// asks first (Cancel first and focused, AH-1). The worktree goes after a
// merge or a discard; the branch goes only when nothing can be lost.

const (
	confirmKindAIWorktreeConflict confirmKindEnum = 2330
	confirmKindAIWorktreeDiscard  confirmKindEnum = 2331
)

// aiWorktreeButtons are the Review / Merge / Discard chips of a step
// whose worktree waits for the user (none while the step runs).
func (m Model) aiWorktreeButtons(msg ai.StoredMessage, st ai.StoredStep, width int) []aiLine {
	wt := st.Worktree
	if !wt.Open() || wt.State == ai.WorktreeRunning || agentdag.State(st.State) == agentdag.StateRunning {
		return nil
	}
	key := msg.Run.ID + ":" + st.ID
	return []aiLine{aiChips(width, []aiChipSpec{
		{"Review", "wt-review:" + key}, {"Merge", "wt-merge:" + key}, {"Discard", "wt-discard:" + key}})}
}

// aiChipSpec is one chip of aiChips.
type aiChipSpec struct{ label, id string }

// aiChips draws chips in one row (each its own button).
func aiChips(width int, chips []aiChipSpec) aiLine {
	bg := theme.Bg(theme.BgPanel)
	row := bg.Render(strings.Repeat(" ", aiIndent))
	x := aiIndent
	var buttons []aiLineButton
	for i, c := range chips {
		cw := runewidth.StringWidth(c.label) + 2
		gap := 0
		if i > 0 {
			gap = 1
		}
		if x+gap+cw > width {
			break
		}
		if gap > 0 {
			row += bg.Render(" ")
			x++
		}
		row += lipgloss.NewStyle().Background(theme.Mix(theme.BgPanel, theme.AIAccent, 0.16)).
			Foreground(theme.LG(theme.TextPrimary)).Render(" " + c.label + " ")
		buttons = append(buttons, aiLineButton{x, x + cw, c.id})
		x += cw
	}
	return aiLine{styled: row, buttons: buttons}
}

// aiWorktreeStep finds step key ("run:step") in the chat.
func (s *aiState) aiWorktreeStep(key string) *ai.StoredStep {
	run, step, ok := strings.Cut(key, ":")
	if !ok {
		return nil
	}
	for i := range s.conv.Messages {
		if r := s.conv.Messages[i].Run; r != nil && r.ID == run {
			return aiFindStep(&s.conv.Messages[i], step)
		}
	}
	return nil
}

// aiWorktreeRef is the worktree an action is about: a chat step's (st
// set; changes land on its card) or a left-over one listed by AI: Agent
// Worktrees… (key "left:<id>").
type aiWorktreeRef struct {
	key, title string
	wt         *ai.StoredWorktree
	st         *ai.StoredStep
}

// aiWorktreeRef finds the open worktree of key.
func (s *aiState) aiWorktreeRef(key string) (aiWorktreeRef, bool) {
	if id, ok := strings.CutPrefix(key, "left:"); ok {
		wt, ok := s.wtLeft[id]
		if !ok || !wt.Open() {
			return aiWorktreeRef{}, false
		}
		return aiWorktreeRef{key: key, title: wt.Branch, wt: wt}, true
	}
	st := s.aiWorktreeStep(key)
	if st == nil || !st.Worktree.Open() {
		return aiWorktreeRef{}, false
	}
	return aiWorktreeRef{key: key, title: aiCleanText(st.Title), wt: st.Worktree, st: st}, true
}

// done records a worktree's new state: a step's card is drawn again and
// the chat saved; a left-over one leaves the list once it is closed.
func (m *Model) aiWorktreeDone(r aiWorktreeRef) {
	s := m.aiS()
	if r.st == nil {
		if !r.wt.Open() {
			delete(s.wtLeft, strings.TrimPrefix(r.key, "left:"))
		}
		return
	}
	s.runGen++
	if !s.streaming {
		_ = s.store.Save(&s.conv)
	}
}

// onAIWorktreeButton runs a worktree chip ("wt-review:run:step", ...).
func (m *Model) onAIWorktreeButton(id string) tea.Cmd {
	act, key, _ := strings.Cut(id, ":")
	r, ok := m.aiS().aiWorktreeRef(key)
	if !ok {
		return m.aiInfo("That worktree is gone")
	}
	switch act {
	case "wt-review":
		return aiWorktreeReviewCmd(aiWorkspaceRoot(), r.title, *r.wt)
	case "wt-merge":
		return aiWorktreeMergeCmd(aiWorkspaceRoot(), key, *r.wt, false)
	case "wt-discard":
		return m.askAIWorktreeDiscard(r)
	}
	return nil
}

// aiWorktreeReviewMsg opens the review card of a worktree.
type aiWorktreeReviewMsg struct {
	title string
	files []diffview.File
	err   error
}

func aiWorktreeReviewCmd(root, title string, wt ai.StoredWorktree) tea.Cmd {
	return func() tea.Msg {
		fs, err := git.AgentFiles(root, wt.ID)
		if err != nil {
			return aiWorktreeReviewMsg{err: err}
		}
		files := make([]diffview.File, 0, len(fs))
		for _, f := range fs {
			files = append(files, diffview.File{Name: f.Path + aiWorktreeStatusWord(f.Status), Old: f.Before, New: f.After})
		}
		return aiWorktreeReviewMsg{title: "Changes of " + title + " · " + wt.Branch, files: files}
	}
}

func aiWorktreeStatusWord(st string) string {
	switch st {
	case "A":
		return " (new)"
	case "D":
		return " (deleted)"
	}
	return ""
}

// onAIWorktreeReview shows the review card (read-only). An AI review
// that still waits keeps the card.
func (m *Model) onAIWorktreeReview(msg aiWorktreeReviewMsg) tea.Cmd {
	if msg.err != nil {
		return m.aiToastErr("Could not read the worktree", msg.err)
	}
	if len(msg.files) == 0 {
		return m.aiInfo("No changes in this worktree")
	}
	s := m.aiS()
	if s.review != nil && m.aiReviewPending() > 0 {
		return m.aiInfo("Finish the AI review first (AI: Review Pending Changes)")
	}
	s.review = &aiReview{card: diffview.New(msg.title, msg.files, false)}
	return m.showAIReview()
}

// aiWorktreeMergedMsg is the result of a merge (or of keeping the work
// on the branch).
type aiWorktreeMergedMsg struct {
	key   string
	res   git.AgentMerge
	err   error
	force bool
	kept  bool // Keep on branch: the worktree went, nothing merged
}

// aiWorktreeMergeCmd merges the worktree into the user's tree. Without
// force a conflict writes nothing. After a merge the worktree goes; the
// branch too unless the merge had conflicts.
func aiWorktreeMergeCmd(root, key string, wt ai.StoredWorktree, force bool) tea.Cmd {
	return func() tea.Msg {
		res, err := git.MergeAgentWorktree(root, wt.ID, force)
		if err != nil || (len(res.Conflicts) > 0 && !force) {
			return aiWorktreeMergedMsg{key: key, res: res, err: err, force: force}
		}
		err = git.RemoveAgentWorktree(root, wt.ID, len(res.Conflicts) == 0)
		return aiWorktreeMergedMsg{key: key, res: res, err: err, force: force}
	}
}

// aiWorktreeKeepCmd removes the worktree and keeps its branch.
func aiWorktreeKeepCmd(root, key string, wt ai.StoredWorktree) tea.Cmd {
	return func() tea.Msg {
		if _, err := git.AgentChanges(root, wt.ID); err != nil { // the work is committed on the branch first
			return aiWorktreeMergedMsg{key: key, err: err, kept: true}
		}
		return aiWorktreeMergedMsg{key: key, err: git.RemoveAgentWorktree(root, wt.ID, false), kept: true}
	}
}

// onAIWorktreeMerged records a merge: a conflict opens the conflict
// card; a merge refreshes the editor, explorer and git views.
func (m *Model) onAIWorktreeMerged(msg aiWorktreeMergedMsg) tea.Cmd {
	r, ok := m.aiS().aiWorktreeRef(msg.key)
	if !ok {
		return nil
	}
	wt := r.wt
	defer m.aiWorktreeDone(r)
	if msg.err != nil {
		return m.aiToastErr("Merge failed; the agent's work is safe on "+wt.Branch, msg.err)
	}
	if msg.kept {
		wt.State = ai.WorktreeOnBranch
		return m.aiInfo("Worktree removed; the work stays on branch " + wt.Branch)
	}
	if len(msg.res.Conflicts) > 0 && !msg.force {
		wt.State, wt.Conflicts = ai.WorktreeConflict, msg.res.Conflicts
		return m.askAIWorktreeConflict(r)
	}
	_, sync := m.aiSyncIDE("", aiSyncDisk)
	n := len(msg.res.Applied)
	if len(msg.res.Conflicts) > 0 {
		wt.State = ai.WorktreeKept
		text := fmt.Sprintf("Merged %d file%s; fix the conflict markers in %s (branch %s kept)", n, plural(n), strings.Join(msg.res.Conflicts, ", "), wt.Branch)
		if len(msg.res.Kept) > 0 {
			text += "; kept yours: " + strings.Join(msg.res.Kept, ", ")
		}
		return tea.Batch(sync, m.aiWarn(text))
	}
	wt.State, wt.Conflicts = ai.WorktreeMerged, nil
	return tea.Batch(sync, m.aiInfo(fmt.Sprintf("Merged %d file%s from %s", n, plural(n), r.title)))
}

// askAIWorktreeConflict is the conflict card: nothing was written yet.
func (m *Model) askAIWorktreeConflict(r aiWorktreeRef) tea.Cmd {
	m.aiS().wtTarget = r.key
	wt := r.wt
	body := "You changed these files too since the agent started:\n  " + strings.Join(wt.Conflicts, "\n  ") +
		"\n\nNothing was written yet. Your files and the agent's work (branch " + wt.Branch + ") are both safe." +
		"\n\nMerge with markers: writes the agent's other files and puts both versions with conflict markers (<<<<<<<) in these files for you to fix; the branch stays." +
		"\nKeep on branch: removes the worktree and keeps the work on the branch, to merge later with git."
	m.confirm = confirm.New("Merge conflict · "+r.title, body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "markers", Title: "Merge with markers", Style: confirm.StylePrimary},
		{ID: "branch", Title: "Keep on branch"},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindAIWorktreeConflict
	return nil
}

// askAIWorktreeDiscard asks before the worktree and its branch go.
func (m *Model) askAIWorktreeDiscard(r aiWorktreeRef) tea.Cmd {
	m.aiS().wtTarget = r.key
	wt := r.wt
	changed := fmt.Sprintf("%s changed %d file%s in its own worktree.", r.title, wt.Files, plural(wt.Files))
	if wt.Files == 0 {
		changed = "The agent worked in its own worktree."
	}
	body := changed + " Discard removes the worktree and the branch " + wt.Branch + ". Your files do not change. This cannot be undone."
	m.confirm = confirm.New("Discard the changes of "+r.title+"?", body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "discard", Title: "Discard", Style: confirm.StyleDestructive},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindAIWorktreeDiscard
	return nil
}

// answerAIWorktreeCard runs the answer of the conflict or discard card
// ("" or "cancel": nothing happens).
func (m *Model) answerAIWorktreeCard(kind confirmKindEnum, id string) tea.Cmd {
	s := m.aiS()
	key := s.wtTarget
	s.wtTarget = ""
	r, ok := s.aiWorktreeRef(key)
	if !ok {
		return nil
	}
	root, wt := aiWorkspaceRoot(), *r.wt
	switch {
	case kind == confirmKindAIWorktreeConflict && id == "markers":
		return aiWorktreeMergeCmd(root, key, wt, true)
	case kind == confirmKindAIWorktreeConflict && id == "branch":
		return aiWorktreeKeepCmd(root, key, wt)
	case kind == confirmKindAIWorktreeDiscard && id == "discard":
		return func() tea.Msg {
			return aiWorktreeDiscardedMsg{key: key, err: git.RemoveAgentWorktree(root, wt.ID, true)}
		}
	}
	return nil
}

// aiWorktreeDiscardedMsg is the end of a Discard.
type aiWorktreeDiscardedMsg struct {
	key string
	err error
}

func (m *Model) onAIWorktreeDiscarded(msg aiWorktreeDiscardedMsg) tea.Cmd {
	r, ok := m.aiS().aiWorktreeRef(msg.key)
	if !ok {
		return nil
	}
	if msg.err != nil {
		return m.aiToastErr("Could not discard the worktree", msg.err)
	}
	r.wt.State = ai.WorktreeDiscarded
	m.aiWorktreeDone(r)
	return m.aiInfo("Discarded the changes of " + r.title)
}

// aiWarn pushes a warning toast.
func (m *Model) aiWarn(text string) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Warn, text)
	return c
}
