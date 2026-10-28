package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/keymap"
)

// AH-11: keys and overflow. Nothing may vanish at a narrow width without a
// trace; AGENTS and USAGE have a key card; the tooltips teach the key that
// really does what the click does; the two keys a terminal may swallow
// have a second route.

func imRune(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// Spec: `?` in AGENTS and in USAGE opens a key card that lists the view's
// keys and both routes of the two global keys.
func TestAgentsAndUsageHaveAKeyCard(t *testing.T) {
	for _, usage := range []bool{false, true} {
		m, _ := aiProfileTestModel(t)
		m.w, m.h = 200, 40
		m.showAgents("")
		m.ai.agents.usage = usage
		cmd, ok := m.routeAgentsKey(imRune("?"))
		_ = cmd
		if !ok {
			t.Fatalf("usage=%v: ? was not taken", usage)
		}
		if !m.confirmOpen || m.confirmKind != confirmKindKeyCard {
			t.Fatalf("usage=%v: no key card", usage)
		}
		card := ansi.Strip(m.confirm.View())
		title := "Agents keys"
		if usage {
			title = "Usage keys"
		}
		wants := []string{title, "Alt+Shift+A", "Alt+G", "Alt+Shift+P", "Alt+N"}
		if !usage {
			wants = append(wants, "details", "usage")
		}
		for _, want := range wants {
			if !strings.Contains(card, want) {
				t.Errorf("usage=%v: card lacks %q:\n%s", usage, want, card)
			}
		}
		if m.keyCardGuide.Page == "" {
			t.Errorf("usage=%v: card has no guide page", usage)
		}
	}
}

// Spec: the footer of AGENTS names the card.
func TestAgentsFooterNamesTheKeyCard(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.w, m.h = 80, 24
	m.showAgents("")
	f := ansi.Strip(m.renderActionsPanel(m.actionsColumnWidth(), m.h-2))
	if !strings.Contains(f, "? keys") {
		t.Errorf("footer lacks \"? keys\":\n%s", f)
	}
}

// Spec: a chip that does not fit becomes "⋯"; the chips that fit stay.
func TestHeaderOverflowChip(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	var more bool
	for _, c := range m.actionsHeaderChips(60) {
		if c.id == "more" {
			t.Errorf("a ⋯ chip at 60 although every chip fits: %v", m.actionsHeaderChips(60))
		}
	}
	for _, c := range m.actionsHeaderChips(36) {
		if c.id == "more" {
			more = true
			if c.label != aiMoreLabel() {
				t.Errorf("label %q", c.label)
			}
		}
	}
	if !more {
		t.Fatalf("no ⋯ chip at 36 (usage does not fit): %v", m.actionsHeaderChips(36))
	}
	// At the 80-column panel width the profile stays (its name gets shorter).
	if head := ansi.Strip(m.renderActionsHeader(32)); !strings.Contains(head, aiMoreLabel()) || !strings.Contains(head, aiSpark()+" def") {
		t.Errorf("header at 32: %q, want the shortened profile chip and ⋯", head)
	}
	// Never wider than the panel, never over the buttons.
	for w := 20; w <= 80; w++ {
		if got := ansi.StringWidth(ansi.Strip(m.renderActionsHeader(w))); got != w {
			t.Fatalf("width %d: header is %d cells", w, got)
		}
		end := 0
		for _, c := range m.actionsHeaderChips(w) {
			if c.x0 < end {
				t.Fatalf("width %d: chips overlap", w)
			}
			end = c.x0 + ansi.StringWidth(c.label)
		}
		if end > 0 && end+1+actionsHeaderButtonsW > w {
			t.Fatalf("width %d: chips run into the buttons", w)
		}
	}
}

// Spec: a click on ⋯ opens a small menu with what did not fit: Profile,
// Services, Usage, Agents. Each row opens its thing.
func TestHeaderOverflowMenu(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.w, m.h = 80, 24
	w := m.actionsColumnWidth()
	var x int
	for _, c := range m.actionsHeaderChips(w) {
		if c.id == "more" {
			x = m.w - w + c.x0
		}
	}
	if x == 0 {
		t.Skipf("the 80-column panel (%d) shows every chip", w)
	}
	if id := m.aiPanelTargetAt(x, 0); id != "ai:more" {
		t.Fatalf("target at ⋯ = %q", id)
	}
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 0, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.pickerOpen || m.pickerKind != pickerKindAIMore {
		t.Fatal("a click on ⋯ did not open the menu")
	}
	m.pickerOpen = false
	m.pickerKind = pickerKindAIMore
	if _, ok := m.handleAIProfilePickerSelect("agents"); !ok || !m.agentsShowing() {
		t.Error("the Agents row did not show AGENTS")
	}
	if _, ok := m.handleAIProfilePickerSelect("usage"); !ok || !m.ai.agents.usage {
		t.Error("the Usage row did not show USAGE")
	}
}

// Spec: the MCP chip has a tooltip; the profile chip's tooltip names what
// the click does (pick) and the key of the next profile.
func TestChipTooltips(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	name, _ := m.tooltipLabel("ai:mcp")
	if !strings.Contains(name, "Services") || !strings.Contains(name, "click") {
		t.Errorf("ai:mcp tooltip %q", name)
	}
	if tooltipHelp("ai:mcp") == "" {
		t.Error("ai:mcp has no guide topic")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// Only the keymap is needed. A real nvim would outlive the test and
	// write into XDG_CONFIG_HOME while t.TempDir removes it.
	t.Setenv("TERMOCODE_NO_NVIM", "1")
	k := New() // the live keymap gives the key
	k.ai = m.ai
	got := k.tooltipText("ai:profile")
	for _, want := range []string{"Profile", "click", "pick", "Alt+Shift+P", "next"} {
		if !strings.Contains(got, want) {
			t.Errorf("ai:profile tooltip %q lacks %q", got, want)
		}
	}
	if m.ai.activeProfile().Name != "" && !strings.Contains(got, m.ai.activeProfile().Name) {
		t.Errorf("ai:profile tooltip %q lacks the active profile", got)
	}
	if got := m.tooltipText("ai:more"); !strings.Contains(got, "More") {
		t.Errorf("ai:more tooltip %q", got)
	}
}

// Spec: Alt+Shift+A and Alt+Shift+P have a second key that a terminal
// does not swallow (no Shift): Alt+G (agents) and Alt+N (next profile).
func TestSecondKeyRoutes(t *testing.T) {
	km := keymap.Default()
	for r, want := range map[rune]keymap.Action{
		'g': keymap.ActionAIToggleAgents,
		'n': keymap.ActionAINextProfile,
	} {
		if got := km.Match(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true}); got != want {
			t.Errorf("alt+%c -> %v, want %v", r, got, want)
		}
	}
}
