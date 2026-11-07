package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
)

// CMP-53: the critic. When a lane (a step's own git worktree) ends with
// changed files, a second model reads its diff before the user merges
// it (ai_critic, on by default). It only reads text: no tools, no files.
// The verdict shows on the step's card ("critic ok" / "critic:
// problems"); the Critic chip shows the notes or asks again. Merge while
// the critic lists problems asks first (Cancel first): Merge Anyway,
// Send to Chat (the problems go to the chat box, unsent) or Cancel.
// AI: Toggle Critic Review turns it off or on.

const (
	confirmKindAICriticNotes confirmKindEnum = 2333
	confirmKindAICriticMerge confirmKindEnum = 2334
)

const (
	aiCriticTimeout   = 2 * time.Minute
	aiCriticMaxTokens = 1500
)

// aiCriticMsg is the critic's answer for lane key.
type aiCriticMsg struct {
	key          string
	round        int
	state, notes string
	slot         aiSlot
	usage        *ai.Usage
}

// criticSlot is the critic's model: ai_critic_slot, else the other
// built-in model than the step's (main when that one is not set up).
func (s *aiState) criticSlot(stepSlot string) (aiSlot, error) {
	name := ai.CriticSlot(stepSlot, s.settings.CriticSlot)
	slot, err := s.resolveSlot(name)
	if err != nil && strings.TrimSpace(s.settings.CriticSlot) == "" && name != ai.SlotMain {
		slot, err = s.resolveSlot(ai.SlotMain)
	}
	return slot, err
}

// aiCriticStart starts a (new) critic review of lane key ("run:step" or
// "left:<id>"). The card shows "critic reviewing…" until it answers.
func (m *Model) aiCriticStart(key string) tea.Cmd {
	s := m.aiS()
	r, ok := s.aiWorktreeRef(key)
	if !ok || r.wt.State == ai.WorktreeRunning {
		return nil
	}
	stepSlot, task := "", r.title
	if r.st != nil {
		stepSlot = r.st.Slot
		if p := strings.TrimSpace(r.st.Prompt); p != "" {
			task = p
		}
	}
	round := 1
	if r.wt.Critic != nil {
		round = r.wt.Critic.Round + 1
	}
	slot, err := s.criticSlot(stepSlot)
	if err != nil {
		r.wt.Critic = &ai.StoredCritic{State: ai.CriticFailed, Round: round,
			Notes: "No model for the critic: set one up with AI: Edit Model Slots (" + ai.Redact(err.Error()) + ")."}
		m.aiWorktreeDone(r)
		return nil
	}
	r.wt.Critic = &ai.StoredCritic{State: ai.CriticRunning, Model: slot.cfg.Model, Round: round}
	m.aiWorktreeDone(r)
	root, id, model := aiWorkspaceRoot(), r.wt.ID, slot.cfg.Model
	return func() tea.Msg {
		out := aiCriticMsg{key: key, round: round, slot: slot}
		diff, err := git.AgentDiff(root, id)
		if err != nil {
			out.state, out.notes = ai.CriticFailed, "Could not read the lane's changes: "+ai.Redact(err.Error())
			return out
		}
		cut := len(diff) > ai.CriticMaxDiff
		if cut {
			diff = diff[:ai.CriticMaxDiff]
		}
		ctx, cancel := context.WithTimeout(context.Background(), aiCriticTimeout)
		defer cancel()
		text, last := ai.Collect(slot.p.Stream(ctx, ai.Request{Messages: ai.CriticPrompt(task, diff, cut), Model: model, MaxTokens: aiCriticMaxTokens}))
		out.usage = last.Usage
		if last.Err != nil {
			out.state, out.notes = ai.CriticFailed, "The critic did not answer: "+ai.Redact(last.Err.Error())
			return out
		}
		out.state, out.notes = ai.ParseCritic(text)
		return out
	}
}

// onAICritic puts the critic's verdict on the lane's card. An answer of
// an older round, or for a lane merged or discarded meanwhile, is
// dropped.
func (m *Model) onAICritic(msg aiCriticMsg) tea.Cmd {
	s := m.aiS()
	if u := msg.usage; u != nil {
		e := aiUsageEntry{Agent: "critic", Model: msg.slot.cfg.Model, U: *u, Sub: msg.slot.cfg.Kind == ai.KindClaudeOAuth}
		if usd, ok := ai.NewPricing(s.settings.Prices).Cost(msg.slot.cfg.Kind, *u); ok {
			e.Cost = &usd
		}
		s.recordUsage(e)
	}
	r, ok := s.aiWorktreeRef(msg.key)
	if !ok || r.wt.Critic == nil || r.wt.Critic.Round != msg.round {
		return nil
	}
	c := r.wt.Critic
	c.State, c.Notes = msg.state, msg.notes
	m.aiWorktreeDone(r)
	switch c.State {
	case ai.CriticOK:
		return m.aiInfo("Critic: no problems in " + r.title)
	case ai.CriticIssues:
		return m.aiWarn("Critic found problems in " + r.title + ": Critic on its card shows them")
	}
	return m.aiWarn("Critic: no verdict for " + r.title + ": " + aiOneLine(c.Notes))
}

// aiCriticWord is the critic part of a lane's card line.
func aiCriticWord(wt *ai.StoredWorktree) string {
	if wt == nil || wt.Critic == nil || !wt.Open() {
		return ""
	}
	switch wt.Critic.State {
	case ai.CriticRunning:
		return " · critic reviewing…"
	case ai.CriticOK:
		return " · critic ok"
	case ai.CriticIssues:
		return " · critic: problems"
	}
	return " · critic: no verdict"
}

// aiCriticChip reports whether a lane's card shows the Critic chip.
func (m Model) aiCriticChip(wt *ai.StoredWorktree) bool {
	return wt != nil && (wt.Critic != nil || m.ai != nil && m.ai.settings.UseCritic())
}

// onAICriticButton runs the Critic chip: a review when there is none
// (or it failed), the notes card when there is a verdict.
func (m *Model) onAICriticButton(r aiWorktreeRef) tea.Cmd {
	c := r.wt.Critic
	switch {
	case c == nil || c.State == ai.CriticFailed && c.Notes == "":
		return tea.Batch(m.aiCriticStart(r.key), m.aiInfo("Asking the critic about "+r.title+"…"))
	case c.State == ai.CriticRunning:
		return m.aiInfo("The critic is still reviewing " + r.title)
	}
	m.aiS().wtTarget = r.key
	body := aiCriticBody(c)
	m.confirm = confirm.New("Critic · "+r.title, body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "again", Title: "Ask Again"},
		{ID: "chat", Title: "Send to Chat"},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindAICriticNotes
	return nil
}

// aiCriticBody is the text of a critic's verdict.
func aiCriticBody(c *ai.StoredCritic) string {
	who := "The critic"
	if c.Model != "" {
		who += " (" + c.Model + ")"
	}
	switch c.State {
	case ai.CriticOK:
		text := who + " found no problems."
		if c.Notes != "" {
			text += "\n\n" + c.Notes
		}
		return text
	case ai.CriticIssues:
		return who + " found problems:\n\n" + c.Notes
	}
	return who + " gave no verdict:\n\n" + c.Notes
}

// askAICriticMerge is the card of a Merge while the critic lists
// problems: nothing is written yet.
func (m *Model) askAICriticMerge(r aiWorktreeRef) tea.Cmd {
	m.aiS().wtTarget = r.key
	body := aiCriticBody(r.wt.Critic) +
		"\n\nNothing was merged yet; the lane's work is safe on " + r.wt.Branch + "." +
		"\nMerge Anyway writes its changes into your tree. Send to Chat puts the problems in the chat box (not sent)."
	m.confirm = confirm.New("The critic found problems in "+r.title, body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "chat", Title: "Send to Chat"},
		{ID: "merge-anyway", Title: "Merge Anyway"},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindAICriticMerge
	return nil
}

// answerAICriticCard runs the answer of a critic card ("" or "cancel":
// nothing happens).
func (m *Model) answerAICriticCard(kind confirmKindEnum, id string) tea.Cmd {
	s := m.aiS()
	key := s.wtTarget
	s.wtTarget = ""
	r, ok := s.aiWorktreeRef(key)
	if !ok {
		return nil
	}
	switch id {
	case "again":
		return m.aiCriticStart(key)
	case "chat":
		return m.aiCriticToChat(r)
	case "merge-anyway":
		if kind == confirmKindAICriticMerge {
			return aiWorktreeMergeCmd(aiWorkspaceRoot(), key, *r.wt, false)
		}
	}
	return nil
}

// aiCriticToChat puts the critic's problems in the chat box, unsent: the
// user reads them and decides.
func (m *Model) aiCriticToChat(r aiWorktreeRef) tea.Cmd {
	s := m.aiS()
	if s.streaming || s.chatCancel != nil {
		return m.aiInfo("The AI is still answering: try again when it is done")
	}
	c := r.wt.Critic
	if c == nil || c.Notes == "" {
		return m.aiInfo("The critic has no notes for " + r.title)
	}
	text := "A critic reviewed the changes of " + r.title + " (branch " + r.wt.Branch + ", worktree " + r.wt.Path +
		", not merged yet) and found these problems:\n\n" + c.Notes +
		"\n\nFix them in that worktree; I review and merge it after."
	s.input = []rune(text)
	s.caret = len(s.input)
	return tea.Batch(m.openAIPanel(true), m.aiInfo("Check the request in the chat and press Enter to send it"))
}

// toggleAICritic flips ai_critic and saves it.
func (m *Model) toggleAICritic() tea.Cmd {
	s := m.aiS()
	next := ai.CriticOff
	if !s.settings.UseCritic() {
		next = ""
	}
	if path, err := ai.ConfigPath(); err == nil {
		cur := ai.LoadSettings(path)
		cur.Critic = next
		if err := ai.SaveSettings(path, cur); err != nil {
			return m.aiToastErr("Could not save the setting", err)
		}
	}
	s.settings.Critic = next
	if next == ai.CriticOff {
		return m.aiInfo("Critic review off: finished lanes wait for your own review")
	}
	return m.aiInfo("Critic review on: a second model reviews each finished lane before Merge")
}
