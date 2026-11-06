package statusbar

import (
	"strings"
	"testing"
)

// planState is a bar with the AGX-12 plan pill.
func planState() State {
	return State{Project: "termocode", Line: 12, Col: 4, Branch: "feat/pin-row", Lang: "Go", Indent: "Tab Size: 4",
		ShowLSP: true, LSP: []string{"gopls"}, AI: "claude-sonnet", AIState: AIIdle,
		Plan: "✦ Claude 62% ↻14:30", PlanShort: "✦62%"}
}

// Spec (AGX-12): the plan pill "✦ Claude 62% ↻14:30" sits on the right;
// its span covers it (a click opens the Plans rows). Three widths: wide
// shows it whole, a narrower bar shrinks it to "✦62%", a narrow bar
// drops it; it never comes back while the bar gets narrower. No Plan,
// no pill.
func TestPlanPillWidths(t *testing.T) {
	useUnicodeIcons(t)
	m := Model{}
	s := planState()
	form := func(w int) string {
		m.SetWidth(w)
		plain := stripCSI(m.View(s))
		switch {
		case strings.Contains(plain, s.Plan):
			return "long"
		case strings.Contains(plain, s.PlanShort):
			return "short"
		}
		return "none"
	}
	seen := map[string]int{}
	order := map[string]int{"long": 0, "short": 1, "none": 2}
	last := "long"
	for w := 200; w >= 40; w-- {
		f := form(w)
		if order[f] < order[last] {
			t.Fatalf("width %d: %s after %s", w, f, last)
		}
		last = f
		seen[f] = w
	}
	for _, f := range []string{"long", "short", "none"} {
		if _, ok := seen[f]; !ok {
			t.Errorf("no width shows the %s form", f)
		}
	}
	m.SetWidth(200)
	x0, x1, ok := m.PlanChipSpan(s)
	plain := []rune(stripCSI(m.View(s)))
	if !ok || string(plain[x0:x1]) != s.Plan {
		t.Errorf("span %d %d %v covers %q", x0, x1, ok, string(plain[max(x0, 0):max(x1, 0)]))
	}
	m.SetWidth(seen["short"])
	x0, x1, ok = m.PlanChipSpan(s)
	plain = []rune(stripCSI(m.View(s)))
	if !ok || string(plain[x0:x1]) != s.PlanShort {
		t.Errorf("short span covers %q", string(plain[max(x0, 0):max(x1, 0)]))
	}
	s.Plan, s.PlanShort = "", ""
	m.SetWidth(200)
	if _, _, ok := m.PlanChipSpan(s); ok || strings.Contains(stripCSI(m.View(s)), "62%") {
		t.Error("an empty plan shows")
	}
}
