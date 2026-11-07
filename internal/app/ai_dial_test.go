package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// CMP-24: the effort dial sets the profile and the chat's reasoning
// level in one step.

func TestAIDialHighSetsProfileAndLevel(t *testing.T) {
	m, p := aiReasoningModel(t)
	m.applyAIDial(ai.DialHigh)
	if got := m.ai.activeProfile().Name; got != ai.ProfileDefault {
		t.Fatalf("profile %q, want default", got)
	}
	if got := m.aiDialMode(); got != ai.DialHigh {
		t.Errorf("dial %q, want high", got)
	}
	if got := m.aiReasoningChipLabel(); got != "think high" {
		t.Errorf("chip %q", got)
	}
	driveAIChat(t, m, m.sendAIChat("why?"), nil)
	if got := lastReasoning(t, p); got != ai.ReasoningHigh {
		t.Errorf("turn level %q, want high", got)
	}
}

func TestAIDialCycle(t *testing.T) {
	m, _ := aiReasoningModel(t)
	if got := m.aiDialMode(); got != "" {
		t.Fatalf("a new chat has no dial mode: %q", got)
	}
	m.cycleAIDial()
	if got := m.aiDialMode(); got != ai.DialLow {
		t.Fatalf("after one step %q, want low", got)
	}
	if got := m.ai.activeProfile().Name; got != ai.ProfileFast {
		t.Errorf("low runs on profile %q, want fast", got)
	}
	for _, want := range []string{ai.DialMedium, ai.DialHigh, ai.DialUltra, ai.DialLow} {
		m.cycleAIDial()
		if got := m.aiDialMode(); got != want {
			t.Fatalf("dial %q, want %q", got, want)
		}
	}
}

func TestAIDialPickerAndToggles(t *testing.T) {
	m, _ := aiReasoningModel(t)
	m.openAIDialPicker()
	if !m.pickerOpen || m.pickerKind != pickerKindAIDial {
		t.Fatalf("picker open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	if _, ok := m.handleAIProfilePickerSelect(ai.DialUltra); !ok {
		t.Fatal("the dial picker is not routed")
	}
	if got := m.aiDialMode(); got != ai.DialUltra {
		t.Errorf("dial %q, want ultra", got)
	}
	m.onAIDialPicked(aiDialPlanID)
	if got := m.ai.activeProfile().Name; got != ai.ProfilePlan {
		t.Errorf("plan toggle: profile %q", got)
	}
	m.onAIDialPicked(aiDialPlanID)
	if got := m.ai.activeProfile().Name; got != ai.ProfileDefault {
		t.Errorf("plan toggle off: profile %q", got)
	}
	m.onAIDialPicked(aiDialFastID)
	if got := m.ai.activeProfile().Name; got != ai.ProfileFast {
		t.Errorf("fast toggle: profile %q", got)
	}
}

// A dial mode that names a missing profile changes nothing.
func TestAIDialMissingProfile(t *testing.T) {
	m, _ := aiReasoningModel(t)
	m.ai.settings.Dial = map[string]ai.DialSetting{ai.DialHigh: {Profile: "nope"}}
	m.applyAIDial(ai.DialHigh)
	if got := m.ai.activeProfile().Name; got != ai.ProfileDefault {
		t.Errorf("profile %q", got)
	}
	if got := m.aiReasoningChipLabel(); got != "" {
		t.Errorf("level set anyway: %q", got)
	}
}
