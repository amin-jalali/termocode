package statusbar

import (
	"strings"
	"testing"
)

func TestAIBadgeSpan(t *testing.T) {
	m := Model{}
	m.SetWidth(160)
	s := State{Line: 1, Col: 1, AI: "claude-opus-5-5", AIState: AIIdle}
	x0, x1, ok := m.AIBadgeSpan(s)
	if !ok || x1-x0 != len([]rune("✦ claude-opus-5-5")) {
		t.Fatalf("span %d..%d ok=%v", x0, x1, ok)
	}
	if !strings.Contains(stripCSI(m.View(s)), "✦ claude-opus-5-5") {
		t.Error("badge not rendered")
	}
	s.AIState = AIUnconfigured
	if !strings.Contains(stripCSI(m.View(s)), "✦ AI off") {
		t.Error("unconfigured badge")
	}
	s.AIState = AIOff
	if _, _, ok := m.AIBadgeSpan(s); ok {
		t.Error("AIOff must hide the badge")
	}
}
