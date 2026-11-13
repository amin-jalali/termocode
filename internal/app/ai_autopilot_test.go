//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func TestAIAutopilotRaiseAsksCancelFirstAndLowerSavesAtOnce(t *testing.T) {
	withConfigDir(t)
	m := &Model{w: 100, h: 40}
	m.ai = &aiState{}
	m.cycleAIAutopilot()
	if !m.confirmOpen || m.confirmKind != confirmKindAIAutopilot || focusedCardID(t, *m) != "cancel" {
		t.Fatal("raising autopilot must open a Cancel-first card")
	}
	m.answerAIAutopilot("cancel")
	if m.aiS().aiAutopilotLevel() != ai.AutopilotOff {
		t.Fatal("Cancel changed the level")
	}
	p, err := ai.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if ai.LoadSettings(p).Autopilot != "" {
		t.Fatal("Cancel saved a level")
	}

	m.confirmOpen = false
	m.cycleAIAutopilot()
	m.answerAIAutopilot("on")
	if got := ai.LoadSettings(p).Autopilot; got != ai.AutopilotLow || m.aiS().aiAutopilotLevel() != ai.AutopilotLow {
		t.Fatalf("on: saved %q", got)
	}
	m.confirmOpen = false
	m.cycleAIAutopilot()
	m.answerAIAutopilot("on")
	if ai.LoadSettings(p).Autopilot != ai.AutopilotMedium {
		t.Fatal("medium not saved")
	}
	m.confirmOpen = false
	m.cycleAIAutopilot() // back to off: no card
	if m.confirmOpen {
		t.Fatal("turning autopilot off must not ask")
	}
	if ai.LoadSettings(p).Autopilot != "" || m.aiS().aiAutopilotLevel() != ai.AutopilotOff {
		t.Fatal("off not saved")
	}
}

func TestAIApprovalPolicyFromSettings(t *testing.T) {
	s := &aiState{}
	if s.aiApprovalPolicy() != nil {
		t.Fatal("no settings: no policy (the old cards-and-classes behavior)")
	}
	s.settings.ApprovalRules = []ai.ApprovalRule{{Tool: "run_command", Action: ai.RuleDeny}}
	if p := s.aiApprovalPolicy(); p == nil || p.Autopilot != ai.AutopilotOff || len(p.Rules) != 1 {
		t.Fatalf("rules only: %+v", p)
	}
	s.settings.Autopilot = "bogus"
	if p := s.aiApprovalPolicy(); p.Autopilot != ai.AutopilotOff {
		t.Fatal("an unknown level must be off")
	}
}

func TestAIToolCardShowsThePolicyNote(t *testing.T) {
	m := &Model{w: 100, h: 40}
	m.ai = &aiState{}
	reply := make(chan bool, 1)
	m.onAIChatConfirm(aiChatConfirmMsg{tool: ai.Tool{Name: "run_command", Mutating: true, Class: ai.ClassRun}, human: "Run: rm -rf x",
		reply: reply, note: aiApprovalNoteText(ai.ApprovalNote{Risk: ai.RiskHigh, Why: "high risk: always asks"}, true)})
	if !strings.Contains(m.confirm.View(), "Risk: high") {
		t.Fatal("the card must name the risk")
	}
}

func TestAIAutopilotIsUserOnly(t *testing.T) {
	if p, ok := aiActionPolicies["ai-autopilot"]; !ok || p.Gate != aiGateNever {
		t.Fatalf("ai-autopilot must be never for the AI: %+v", p)
	}
}
