package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
)

// G-WT: AI: Agent Worktrees… lists every agent worktree of the
// repository (a step's card owns it, or it is left over after a crash or
// from another chat). Picking one opens a card with Review, Merge and
// Discard (Cancel first). Nothing is ever removed without that card.
// AI: Toggle Agent Worktrees turns the feature on or off (ai_worktrees).

const (
	pickerKindAIWorktrees     pickerKindEnum  = 2330
	confirmKindAIWorktreeLeft confirmKindEnum = 2332
)

// toggleAIWorktrees flips ai_worktrees and saves it.
func (m *Model) toggleAIWorktrees() tea.Cmd {
	s := m.aiS()
	next := ai.WorktreesOff
	if !s.settings.UseWorktrees() {
		next = ""
	}
	if path, err := ai.ConfigPath(); err == nil {
		cur := ai.LoadSettings(path)
		cur.Worktrees = next
		if err := ai.SaveSettings(path, cur); err != nil {
			return m.aiToastErr("Could not save the setting", err)
		}
	}
	s.settings.Worktrees = next
	if next == ai.WorktreesOff {
		return m.aiInfo("Agent worktrees off: every step works in your tree")
	}
	return m.aiInfo("Agent worktrees on: steps that run at the same time work in their own git worktree")
}

// aiWorktreeItem is one listed worktree (files -1: its step still runs,
// not counted).
type aiWorktreeItem struct {
	wt    git.AgentWorktree
	files int
}

type aiWorktreesListMsg struct {
	items []aiWorktreeItem
	err   error
}

// openAIWorktrees lists the agent worktrees (in the background).
func (m *Model) openAIWorktrees() tea.Cmd {
	s := m.aiS()
	root := aiWorkspaceRoot()
	live := map[string]bool{} // the worktrees of steps that run now
	if rm := s.aiRunMessage(); rm != nil && s.run != nil {
		for _, st := range rm.Steps {
			if st.Worktree != nil && agentdag.State(st.State) == agentdag.StateRunning {
				live[st.Worktree.ID] = true
			}
		}
	}
	return func() tea.Msg {
		left, err := git.LeftoverAgentWorktrees(root)
		if err != nil {
			return aiWorktreesListMsg{err: err}
		}
		items := make([]aiWorktreeItem, 0, len(left))
		for _, wt := range left {
			it := aiWorktreeItem{wt: wt, files: -1}
			if !live[wt.ID] {
				if ch, err := git.AgentChanges(root, wt.ID); err == nil {
					it.files = len(ch)
				}
			}
			items = append(items, it)
		}
		return aiWorktreesListMsg{items: items}
	}
}

// aiWorktreeOwner is the key ("run:step") and title of the step of this
// chat that owns worktree id ("" when none).
func (s *aiState) aiWorktreeOwner(id string) (key, title string) {
	for _, msg := range s.conv.Messages {
		if msg.Run == nil {
			continue
		}
		for _, st := range msg.Steps {
			if st.Worktree != nil && st.Worktree.ID == id && st.Worktree.Open() {
				return msg.Run.ID + ":" + st.ID, aiCleanText(st.Title)
			}
		}
	}
	return "", ""
}

// onAIWorktreesList shows the list.
func (m *Model) onAIWorktreesList(msg aiWorktreesListMsg) tea.Cmd {
	if msg.err != nil {
		return m.aiInfo("Agent worktrees need a git repository with a commit")
	}
	if len(msg.items) == 0 {
		return m.aiInfo("No agent worktrees in this repository")
	}
	s := m.aiS()
	s.wtLeft = map[string]*ai.StoredWorktree{}
	items := make([]picker.Item, 0, len(msg.items))
	for _, it := range msg.items {
		files := "running"
		if it.files >= 0 {
			files = fmt.Sprintf("%d file%s", it.files, plural(it.files))
		}
		key, owner := s.aiWorktreeOwner(it.wt.ID)
		if key == "" {
			key = "left:" + it.wt.ID
			owner = "left over"
			s.wtLeft[it.wt.ID] = &ai.StoredWorktree{ID: it.wt.ID, Path: it.wt.Path, Branch: it.wt.Branch, State: ai.WorktreeReady, Files: max(it.files, 0)}
			if it.files < 0 {
				s.wtLeft[it.wt.ID].State = ai.WorktreeRunning
			}
		} else {
			owner = "step " + owner
		}
		items = append(items, picker.Item{ID: key, Title: it.wt.Branch, Hint: files + " · " + owner})
	}
	m.picker = picker.NewItems(" Agent Worktrees ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen, m.pickerKind = true, pickerKindAIWorktrees
	return nil
}

// onAIWorktreePicked opens the card of one worktree.
func (m *Model) onAIWorktreePicked(key string) tea.Cmd {
	s := m.aiS()
	r, ok := s.aiWorktreeRef(key)
	if !ok || r.wt.State == ai.WorktreeRunning {
		return m.aiInfo("That worktree is in use by a running step")
	}
	s.wtTarget = key
	owner := "It belongs to step " + r.title + " of this chat."
	if r.st == nil {
		owner = "No card of this chat owns it: it is left over from a stopped run or another chat."
	}
	body := fmt.Sprintf("Branch %s · %d file%s changed\n%s\n%s\n\nReview shows the changes. Merge writes them into your tree. Discard removes the worktree and the branch (it asks first).",
		r.wt.Branch, r.wt.Files, plural(r.wt.Files), r.wt.Path, owner)
	m.confirm = confirm.New("Agent worktree", body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "review", Title: "Review"},
		{ID: "merge", Title: "Merge", Style: confirm.StylePrimary},
		{ID: "discard", Title: "Discard"},
	}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindAIWorktreeLeft
	return nil
}

// answerAIWorktreeLeft runs the answer of a listed worktree's card.
func (m *Model) answerAIWorktreeLeft(id string) tea.Cmd {
	s := m.aiS()
	key := s.wtTarget
	s.wtTarget = ""
	switch id {
	case "review", "merge", "discard":
		return m.onAIWorktreeButton("wt-" + id + ":" + key)
	}
	return nil
}

// aiSettleWorktrees marks the worktrees of steps that no longer run (a
// stopped or interrupted run) as waiting for the user.
func aiSettleWorktrees(steps []ai.StoredStep) {
	for k := range steps {
		if wt := steps[k].Worktree; wt != nil && wt.State == ai.WorktreeRunning && agentdag.State(steps[k].State) != agentdag.StateRunning {
			wt.State = ai.WorktreeReady
		}
	}
}

// aiWorktreeHelp is the "?" help text of agent worktrees.
const aiWorktreeHelp = "Parallel steps: steps that run at the same time get their own git worktree (⑂ on the card). " +
	"When a step ends: Review · Merge · Discard on its card. AI: Agent Worktrees… lists all of them (also left-over ones). " +
	"AI: Toggle Agent Worktrees turns it off."
