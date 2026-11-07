package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
)

// CMP-61 safe auto-approve (internal/ai/autopilot.go): the agent's
// approval policy is built from ai_autopilot, ai_approval_rules and
// ai_guardian. AI: Autopilot Level… steps the level off → low → medium;
// raising it opens a Cancel-first card that says what will run without
// asking. High-risk calls always ask; deny rules always refuse.

const confirmKindAIAutopilot confirmKindEnum = 2661

// aiAutopilotLevel is the effective ai_autopilot level.
func (s *aiState) aiAutopilotLevel() string { return ai.CheckAutopilot(s.settings.Autopilot) }

// aiApprovalPolicy is the agent's policy, or nil when nothing is set
// (then the agent keeps its old cards-and-classes behavior).
func (s *aiState) aiApprovalPolicy() *ai.ApprovalPolicy {
	level := s.aiAutopilotLevel()
	if level == ai.AutopilotOff && len(s.settings.ApprovalRules) == 0 {
		return nil
	}
	p := &ai.ApprovalPolicy{Rules: s.settings.ApprovalRules, Autopilot: level}
	if g := strings.TrimSpace(s.settings.Guardian); g != "" && g != "off" && level != ai.AutopilotOff {
		if slot, err := s.resolveSlot(g); err == nil {
			p.Guardian = ai.NewGuardian(slot.p, slot.cfg.Model)
		} else {
			recordError("[ai] ai_guardian: " + err.Error())
		}
	}
	return p
}

// aiAutopilotNext is the level after cur in the cycle.
func aiAutopilotNext(cur string) string {
	for i, l := range ai.AutopilotLevels {
		if l == cur {
			return ai.AutopilotLevels[(i+1)%len(ai.AutopilotLevels)]
		}
	}
	return ai.AutopilotLow
}

// aiAutopilotText says what a level lets the agent do without a card.
func aiAutopilotText(level string) string {
	switch level {
	case ai.AutopilotLow:
		return "Low-risk actions run without asking: read-only commands (ls, cat, grep, git status / diff / log)."
	case ai.AutopilotMedium:
		return "Low- and medium-risk actions run without asking: file edits in the project, builds, tests and other local commands."
	}
	return "Every change asks first (the classes you set to Always allow still run)."
}

// cycleAIAutopilot is AI: Autopilot Level…: lowering saves at once,
// raising asks on a Cancel-first card.
func (m *Model) cycleAIAutopilot() tea.Cmd {
	s := m.aiS()
	next := aiAutopilotNext(s.aiAutopilotLevel())
	if next == ai.AutopilotOff {
		return m.setAIAutopilot(next)
	}
	guard := "No guardian model: set ai_guardian (a model slot, e.g. \"quick\") to have a second model check medium-risk actions."
	if g := strings.TrimSpace(s.settings.Guardian); g != "" && g != "off" {
		guard = "Guardian: the " + aiOneLine(aiCleanText(g)) + " model checks medium-risk actions first and may send them back to you."
	}
	msg := fmt.Sprintf("%s\n\nHigh-risk actions always ask: deletes, git push / pull, rm, sudo, network commands, writes outside the project or to .env, .git and key files.\nRules in ai_approval_rules still apply (deny always wins).\n%s",
		aiAutopilotText(next), guard)
	s.autopilotNext = next
	m.confirm = confirm.Risky("Turn autopilot to "+next+"?", msg,
		confirm.Button{ID: "cancel", Title: "Cancel"}, confirm.Button{ID: "on", Title: "Autopilot " + next, Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmKind = confirmKindAIAutopilot
	m.confirmOpen = true
	return nil
}

// answerAIAutopilot applies the card's answer.
func (m *Model) answerAIAutopilot(id string) tea.Cmd {
	s := m.aiS()
	next := s.autopilotNext
	s.autopilotNext = ""
	if id != "on" || next == "" {
		return nil
	}
	return m.setAIAutopilot(next)
}

// setAIAutopilot saves ai_autopilot. It applies from the next turn.
func (m *Model) setAIAutopilot(level string) tea.Cmd {
	s := m.aiS()
	p, err := ai.ConfigPath()
	if err != nil {
		return m.aiToastErr("Could not save autopilot", err)
	}
	cur := ai.LoadSettings(p)
	cur.Autopilot = level
	if err := ai.SaveSettings(p, cur); err != nil {
		return m.aiToastErr("Could not save autopilot", err)
	}
	s.settings.Autopilot = cur.Autopilot
	return m.aiInfo("Autopilot " + level + ": " + aiAutopilotText(level))
}

// aiApprovalNoteText is the card's extra line for a policy note.
func aiApprovalNoteText(n ai.ApprovalNote, ok bool) string {
	if !ok {
		return ""
	}
	return n.RiskLabel()
}
