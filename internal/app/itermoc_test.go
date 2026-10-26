package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// itermocTestModel is a model with the Itermoc view open at sidebar width
// sw (the profile test model: built-in profiles, an untrusted project
// profile "house", one fake main slot).
func itermocTestModel(t *testing.T, sw, h int) *Model {
	t.Helper()
	m, _ := aiProfileTestModel(t)
	m.w, m.h = 200, h
	m.explorerWidth, m.showExp = sw+1, true
	m.hoverX, m.hoverY = -1, -1
	m.revealItermocView(true, "")
	return m
}

// imIcons sets the icon mode for one test.
func imIcons(t *testing.T, mode theme.IconMode) {
	t.Helper()
	old := theme.CurrentIconMode
	theme.CurrentIconMode = mode
	t.Cleanup(func() { theme.CurrentIconMode = old })
}

// imPress sends one key to the view.
func imPress(m *Model, key string) tea.Cmd {
	var k tea.KeyMsg
	switch key {
	case "enter":
		k = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		k = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		k = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		k = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		k = tea.KeyMsg{Type: tea.KeyDown}
	case " ":
		k = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	nm, cmd := m.handleItermocSidebarKey(k)
	*m = nm
	return cmd
}

// imFrame is the plain text of the view.
func imFrame(m *Model) string {
	return ansi.Strip(m.renderItermocSidebar(m.explorerWidth-1, m.h))
}

// imCursorTo moves the cursor onto the row of kind and name.
func imCursorTo(t *testing.T, m *Model, kind imRowKind, name string) {
	t.Helper()
	st := m.itermocS()
	for i, r := range st.rows() {
		if r.kind == kind && r.name == name {
			st.cursor = i
			return
		}
	}
	t.Fatalf("no row %v %q in %+v", kind, name, st.rows())
}

// Spec: the view lists the profiles with the active one marked, and a
// project profile that was never asked shows "project" at the right. No row
// carries a [Label (key)] button.
func TestItermocProfilesRows(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 70, 40)
	f := imFrame(m)
	for _, want := range []string{"ITERMOC", "PROFILES", "active", "default", "review", "fast", "local-only", "house", "project", "+ new profile…"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	if !strings.Contains(f, "● default") {
		t.Errorf("the active profile has no mark:\n%s", f)
	}
}

// Spec: the view fits any width: every line is exactly the sidebar width
// and the sidebar height, at the 80x24 window's sidebar too.
func TestItermocNoOverflow(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	for _, sz := range [][2]int{{29, 24}, {30, 24}, {40, 24}, {58, 50}, {16, 12}} {
		m := itermocTestModel(t, sz[0], sz[1])
		out := m.renderItermocSidebar(sz[0], sz[1])
		lines := strings.Split(out, "\n")
		if len(lines) != sz[1] {
			t.Errorf("%dx%d: %d lines", sz[0], sz[1], len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != sz[0] {
				t.Errorf("%dx%d: line %d is %d wide: %q", sz[0], sz[1], i, w, ansi.Strip(l))
			}
		}
	}
}

// Spec: Enter on a profile opens its actions menu; Use switches the chat
// to the profile, Details opens the details card of that profile.
func TestItermocProfileActionsRoute(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	imCursorTo(t, m, imRowProfile, "review")
	imPress(m, "enter")
	if !m.pickerOpen || m.pickerKind != pickerKindItermocActions {
		t.Fatalf("no actions menu: open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	var ids []string
	for _, a := range func() []itermocAction { _, a := m.itermocActionsFor(m.itermoc.menuRow); return a }() {
		ids = append(ids, a.id)
	}
	if got := strings.Join(ids, ","); got != "use,details,edit,duplicate,runs,json" {
		t.Errorf("verbs %s", got)
	}
	m.pickerOpen = false
	if _, ok := m.handleItermocPickerSelect("details"); !ok || !m.confirmOpen || m.confirmKind != confirmKindAIProfileShow {
		t.Fatal("Details did not open the details card")
	}
	if !strings.Contains(ansi.Strip(m.confirm.View()), "Profile review") {
		t.Errorf("card is not about review:\n%s", ansi.Strip(m.confirm.View()))
	}
	if m.ai.activeProfile().Name != ai.ProfileDefault {
		t.Error("Details switched the profile")
	}
	m.confirmOpen = false
	m.pickerKind = pickerKindItermocActions
	m.handleItermocPickerSelect("use")
	if m.ai.activeProfile().Name != "review" {
		t.Errorf("Use left profile %q", m.ai.activeProfile().Name)
	}
}

// Spec: Use on a project profile nobody trusted yet asks on the trust card
// (Deny first); it never switches without it.
func TestItermocUseUntrustedProfileAsks(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	imCursorTo(t, m, imRowProfile, "house")
	imPress(m, "u")
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileTrust {
		t.Fatal("no trust card")
	}
	if bs := m.confirm.Buttons(); len(bs) == 0 || bs[0].ID != "deny" {
		t.Errorf("trust card buttons %+v: Deny must be first", bs)
	}
	if m.ai.activeProfile().Name == "house" {
		t.Error("the profile was used before the answer")
	}
}

// Spec: the view is reachable from the palette and opens in the sidebar.
func TestItermocPaletteOpens(t *testing.T) {
	m := itermocTestModel(t, 60, 30)
	m.activity.SetActive(activity.ViewFiles)
	m.focus = FocusEditor
	if _, ok := m.dispatchItermocPalette("itermoc-open"); !ok {
		t.Fatal("palette id not handled")
	}
	if m.activity.Active() != activity.ViewItermoc || m.focus != FocusExplorer {
		t.Errorf("view %v focus %v", m.activity.Active(), m.focus)
	}
}
