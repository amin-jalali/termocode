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
	if !wt.Open() || wt.State == ai.WorktreeRunning || agentdag.State(st.State) == agentdag.StateRunning || (wt.Files == 0 && wt.State != ai.WorktreeConflict) {
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

// onAIWorktreeButton runs a worktree chip ("wt-review:run:step", ...).
func (m *Model) onAIWorktreeButton(id string) tea.Cmd {
	act, key, _ := strings.Cut(id, ":")
	s := m.aiS()
	st := s.aiWorktreeStep(key)
	if st == nil || !st.Worktree.Open() {
		return m.aiInfo("That worktree is gone")
	}
	switch act {
	case "wt-review":
		return aiWorktreeReviewCmd(aiWorkspaceRoot(), st.Title, *st.Worktree)
	case "wt-merge":
		return aiWorktreeMergeCmd(aiWorkspaceRoot(), key, *st.Worktree, false)
	case "wt-discard":
		return m.askAIWorktreeDiscard(key, st)
	}
	return nil
}

// aiWorktreeReviewMsg opens the review card of a step's worktree.
type aiWorktreeReviewMsg struct {
	title string
	files []diffview.File
	err   error
}

func aiWorktreeReviewCmd(root, step string, wt ai.StoredWorktree) tea.Cmd {
	return func() tea.Msg {
		fs, err := git.AgentFiles(root, wt.ID)
		if err != nil {
			return aiWorktreeReviewMsg{err: err}
		}
		files := make([]diffview.File, 0, len(fs))
		for _, f := range fs {
			files = append(files, diffview.File{Name: f.Path + aiWorktreeStatusWord(f.Status), Old: f.Before, New: f.After})
		}
		return aiWorktreeReviewMsg{title: "Changes of " + aiCleanText(step) + " · " + wt.Branch, files: files}
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

// onAIWorktreeMerged records a merge on the card: a conflict opens the
// conflict card; a merge refreshes the editor, explorer and git views.
func (m *Model) onAIWorktreeMerged(msg aiWorktreeMergedMsg) tea.Cmd {
	s := m.aiS()
	st := s.aiWorktreeStep(msg.key)
	if st == nil || st.Worktree == nil {
		return nil
	}
	wt := st.Worktree
	defer func() {
		s.runGen++
		if !s.streaming {
			_ = s.store.Save(&s.conv)
		}
	}()
	if msg.err != nil {
		return m.aiToastErr("Merge failed; the agent's work is safe on "+wt.Branch, msg.err)
	}
	if msg.kept {
		wt.State = ai.WorktreeOnBranch
		return m.aiInfo("Worktree removed; the work stays on branch " + wt.Branch)
	}
	if len(msg.res.Conflicts) > 0 && !msg.force {
		wt.State, wt.Conflicts = ai.WorktreeConflict, msg.res.Conflicts
		return m.askAIWorktreeConflict(msg.key, st)
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
	return tea.Batch(sync, m.aiInfo(fmt.Sprintf("Merged %d file%s from %s", n, plural(n), aiCleanText(st.Title))))
}

// askAIWorktreeConflict is the conflict card: nothing was written yet.
func (m *Model) askAIWorktreeConflict(key string, st *ai.StoredStep) tea.Cmd {
	s := m.aiS()
	s.wtTarget = key
	wt := st.Worktree
	body := "You changed these files too since the step started:\n  " + strings.Join(wt.Conflicts, "\n  ") +
		"\n\nNothing was written yet. Your files and the agent's work (branch " + wt.Branch + ") are both safe." +
		"\n\nMerge with markers: writes the agent's other files and puts both versions with conflict markers (<<<<<<<) in these files for you to fix; the branch stays." +
		"\nKeep on branch: removes the worktree and keeps the work on the branch, to merge later with git."
	m.confirm = confirm.New("Merge conflict · "+aiCleanText(st.Title), body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "markers", Title: "Merge with markers", Style: confirm.StylePrimary},
		{ID: "branch", Title: "Keep on branch"},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindAIWorktreeConflict
	return nil
}

// askAIWorktreeDiscard asks before the worktree and its branch go.
func (m *Model) askAIWorktreeDiscard(key string, st *ai.StoredStep) tea.Cmd {
	s := m.aiS()
	s.wtTarget = key
	wt := st.Worktree
	body := fmt.Sprintf("%s changed %d file%s in its own worktree. Discard removes the worktree and the branch %s. Your files do not change. This cannot be undone.",
		aiCleanText(st.Title), wt.Files, plural(wt.Files), wt.Branch)
	m.confirm = confirm.New("Discard the changes of "+aiCleanText(st.Title)+"?", body, []confirm.Button{
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
	st := s.aiWorktreeStep(key)
	if st == nil || !st.Worktree.Open() {
		return nil
	}
	root, wt := aiWorkspaceRoot(), *st.Worktree
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
	s := m.aiS()
	st := s.aiWorktreeStep(msg.key)
	if st == nil || st.Worktree == nil {
		return nil
	}
	if msg.err != nil {
		return m.aiToastErr("Could not discard the worktree", msg.err)
	}
	st.Worktree.State = ai.WorktreeDiscarded
	s.runGen++
	if !s.streaming {
		_ = s.store.Save(&s.conv)
	}
	return m.aiInfo("Discarded the changes of " + aiCleanText(st.Title))
}

// aiWarn pushes a warning toast.
func (m *Model) aiWarn(text string) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Warn, text)
	return c
}
