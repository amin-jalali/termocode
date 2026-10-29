package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// covScreen is the plain text of the model's view, one string per row.
func covScreen(m Model) []string {
	return strings.Split(ansi.Strip(m.View()), "\n")
}

// covFind returns the cell (x, y) where text first appears on screen.
func covFind(m Model, text string) (int, int, bool) {
	for y, l := range covScreen(m) {
		if i := strings.Index(l, text); i >= 0 {
			return ansi.StringWidth(l[:i]), y, true
		}
	}
	return 0, 0, false
}

// covFitsScreen fails when the view is wider or taller than the screen.
func covFitsScreen(t *testing.T, m Model, what string) {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	if len(lines) > m.h {
		t.Errorf("%s: %d rows on a %d-row screen", what, len(lines), m.h)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.w {
			t.Errorf("%s: row %d is %d wide on a %d-wide screen", what, i, w, m.w)
			return
		}
	}
}

func coreKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func coreRunes(s string) tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func covSettingsModel(t *testing.T, w, h int) Model {
	t.Helper()
	withConfigDir(t)
	m := New()
	m, _ = m.update(tea.WindowSizeMsg{Width: w, Height: h})
	_ = m.dispatchPaletteAction("open-settings-ui")
	if !m.settingsModalOpen {
		t.Fatal("settings did not open")
	}
	return m
}

func covSelected(m Model) string {
	s := m.settingsModal
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return ""
	}
	return s.rows[s.cursor].Row.ID
}

func TestCovCoreSettingsModalKeys(t *testing.T) {
	m := covSettingsModel(t, 120, 40)
	first := covSelected(m)
	if first != "setting-theme" {
		t.Fatalf("cursor starts on %q", first)
	}
	// Up on the first row stays.
	m, _ = m.update(coreKey(tea.KeyUp))
	if covSelected(m) != first {
		t.Errorf("up from the first row moved to %q", covSelected(m))
	}
	m, _ = m.update(coreKey(tea.KeyDown))
	m, _ = m.update(coreKey(tea.KeyCtrlN))
	if covSelected(m) != "setting-animations" {
		t.Errorf("two downs: %q", covSelected(m))
	}
	m, _ = m.update(coreKey(tea.KeyCtrlP))
	if covSelected(m) != "setting-icons" {
		t.Errorf("ctrl+p: %q", covSelected(m))
	}
	// Down past the last Appearance row skips the next header.
	for i := 0; i < 5; i++ {
		m, _ = m.update(coreKey(tea.KeyDown))
	}
	if r := m.settingsModal.rows[m.settingsModal.cursor]; r.Header {
		t.Error("the cursor sits on a header")
	}

	// Filter: typing narrows, mouse report fragments and escapes are dropped.
	m, _ = m.update(coreRunes("[<35;70"))
	m, _ = m.update(coreRunes("tab\x1b[A"))
	if m.settingsModal.input != "tab" {
		t.Errorf("filter = %q", m.settingsModal.input)
	}
	if covSelected(m) != "setting-tab-size" {
		t.Errorf("filter tab → %q", covSelected(m))
	}
	m, _ = m.update(coreKey(tea.KeySpace))
	m, _ = m.update(coreKey(tea.KeyBackspace))
	m, _ = m.update(coreKey(tea.KeyBackspace))
	if m.settingsModal.input != "ta" {
		t.Errorf("after space + 2 backspaces: %q", m.settingsModal.input)
	}
	// Backspace on an empty filter is fine.
	m.settingsModal.input = ""
	m, _ = m.update(coreKey(tea.KeyBackspace))
	// No match: Enter does nothing.
	m, _ = m.update(coreRunes("zzzz"))
	_, cmd := m.update(coreKey(tea.KeyEnter))
	if cmd != nil {
		if msg, ok := cmd().(SettingsSelectMsg); ok && msg.ID == "" {
			t.Error("enter on no match selected an empty row")
		}
	}
	// Other keys and messages are ignored.
	m, _ = m.update(coreKey(tea.KeyTab))
	m.settingsModal, _ = m.settingsModal.Update(tea.FocusMsg{})

	// Enter on a row opens its value prompt.
	m.settingsModal.input = ""
	m.settingsModal.refitCursor()
	m, cmd = m.update(coreKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter gave no command")
	}
	m, _ = m.update(cmd())
	if m.settingsModalOpen || !(m.promptOpen || m.pickerOpen) {
		t.Errorf("enter: modal %v, prompt %v, picker %v", m.settingsModalOpen, m.promptOpen, m.pickerOpen)
	}
	// Esc closes.
	m = covSettingsModel(t, 120, 40)
	_, cmd = m.update(coreKey(tea.KeyEsc))
	m, _ = m.update(cmd())
	if m.settingsModalOpen {
		t.Error("esc left the modal open")
	}
}

func TestCovCoreSettingsModalMouse(t *testing.T) {
	m := covSettingsModel(t, 120, 40)
	x, y, ok := covFind(m, "tab_size")
	if !ok {
		t.Fatalf("tab_size not on screen:\n%s", strings.Join(covScreen(m), "\n"))
	}
	// Hover moves the highlight.
	m, _ = m.update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion})
	if covSelected(m) != "setting-tab-size" {
		t.Errorf("hover: %q", covSelected(m))
	}
	// Hover outside the box changes nothing.
	m, _ = m.update(tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionMotion})
	if covSelected(m) != "setting-tab-size" {
		t.Errorf("hover outside: %q", covSelected(m))
	}
	// Wheel moves the cursor.
	m, _ = m.update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	down := covSelected(m)
	m, _ = m.update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if down == "setting-tab-size" || covSelected(m) != "setting-tab-size" {
		t.Errorf("wheel: down %q, up %q", down, covSelected(m))
	}
	// A click on a header or the title selects nothing.
	hx, hy, _ := covFind(m, "Editor")
	if _, cmd := m.update(tea.MouseMsg{X: hx, Y: hy, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); cmd != nil {
		t.Error("a click on a header did something")
	}
	tx, ty, _ := covFind(m, "Settings")
	if _, cmd := m.update(tea.MouseMsg{X: tx, Y: ty, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); cmd != nil {
		t.Error("a click on the title did something")
	}
	// A click on the row selects it.
	ax, ay, _ := covFind(m, "auto_save")
	m2, cmd := m.update(tea.MouseMsg{X: ax, Y: ay, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil {
		t.Fatal("click on auto_save gave nothing")
	}
	if sel, ok := cmd().(SettingsSelectMsg); !ok || sel.ID != "setting-auto-save" {
		t.Errorf("click selected %+v", cmd())
	}
	_ = m2
	// A click outside the box closes the modal.
	_, cmd = m.update(tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil {
		t.Fatal("click outside gave nothing")
	}
	if _, ok := cmd().(SettingsCloseMsg); !ok {
		t.Errorf("click outside: %T", cmd())
	}
	// No size yet: the mouse is ignored.
	var empty settingsModal
	if _, cmd := empty.HandleMouse(1, 1, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil || empty.Box() != "" {
		t.Error("an unsized modal reacted")
	}
}

// The modal fits every screen size, and the resize reaches it.
func TestCovCoreSettingsModalSizes(t *testing.T) {
	m := covSettingsModel(t, 120, 40)
	for _, sz := range [][2]int{{120, 40}, {80, 24}, {60, 20}, {40, 12}, {30, 10}, {200, 60}} {
		m, _ = m.update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		if m.settingsModal.w != sz[0] || m.settingsModal.h != sz[1] {
			t.Errorf("modal size %dx%d after resize to %v", m.settingsModal.w, m.settingsModal.h, sz)
		}
		box := m.settingsModal.Box()
		for i, l := range strings.Split(box, "\n") {
			if w := ansi.StringWidth(l); w > sz[0] && sz[0] >= 40 {
				t.Errorf("%v: box row %d is %d wide", sz, i, w)
			}
		}
		if sz[0] >= 40 {
			covFitsScreen(t, m, "settings")
		}
	}
	// Long values are cut to the box.
	m.settingsModal.rows[1].Value = strings.Repeat("very-long-value-", 10)
	for i, l := range strings.Split(m.settingsModal.Box(), "\n") {
		if w := ansi.StringWidth(l); w > m.w {
			t.Errorf("long value: row %d is %d wide", i, w)
		}
	}
}

func TestCovCoreSettingsInputHelpers(t *testing.T) {
	for in, want := range map[string]string{
		"abc":          "abc",
		"a\x1b[31mb":   "ab",
		"a\x1b[":       "a",
		"[<35;70;5Mx":  "x",
		"[<3a":         "[<3a",
		"[<35":         "[<35",
		"tab\x00\x7f!": "tab!",
	} {
		if got := sanitizeSettingsInput(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]bool{
		"[<35;70M": true, ";12": true, "<1;2m": true,
		"a": false, "ab": false, "[x": false, "[": false,
	} {
		if got := isSettingsMouseFragment([]rune(in)); got != want {
			t.Errorf("fragment(%q) = %v", in, got)
		}
	}
}
