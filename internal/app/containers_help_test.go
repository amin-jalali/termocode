package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// helpSession is a Containers view with HELP open.
func helpSession(t *testing.T) (*Model, *servicesState) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	st := svcHostState()
	st.collapsed[hostSection("help")] = false
	m := svcTestModel(st, 100, 60)
	return &m, st
}

func helpRow(t *testing.T, st *servicesState, name string) svcRow {
	t.Helper()
	return selectRow(t, st, func(r svcRow) bool {
		return r.kind == svcRowHostItem && r.section == hostSection("help") && r.name == name
	})
}

func pressEnter(m *Model) tea.Cmd {
	next, cmd := m.handleServicesSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = next
	return cmd
}

func TestHelpSectionRows(t *testing.T) {
	_, st := helpSession(t)
	for _, name := range []string{"Open guide", "Keys", "Report a problem"} {
		helpRow(t, st, name)
	}
	// The engine info row is not selectable.
	found := false
	for _, r := range st.rows() {
		if r.section == hostSection("help") && r.kind == svcRowHint && r.text == "docker 27.3.1" {
			found = true
		}
	}
	if !found {
		t.Errorf("no engine info row:\n%s", svcRowsText(st))
	}
}

func TestHelpEngineInfoTexts(t *testing.T) {
	join := func(st *servicesState) string { return strings.Join(st.helpEngineLines(), " | ") }
	_, st := helpSession(t)
	st.containerDetected = false
	if got := join(st); got != "Looking for an engine…" {
		t.Errorf("detecting: %q", got)
	}
	st.containerDetected = true
	st.containerEngine.Binary = ""
	if got := join(st); got != "No container engine" {
		t.Errorf("absent: %q", got)
	}
	st = svcHostState()
	st.sessionContext = "prod"
	if got := join(st); got != "docker 27.3.1 | context prod" {
		t.Errorf("with context: %q", got)
	}
	st.containerEngine.Reachable = false
	if got := join(st); got != "docker 27.3.1 | context prod | not running" {
		t.Errorf("down: %q", got)
	}
}

func TestHelpOpenGuideAndKeysOpenTheContainersPage(t *testing.T) {
	for name, anchor := range map[string]string{"Open guide": "", "Keys": "keys"} {
		m, st := helpSession(t)
		helpRow(t, st, name)
		cmd := pressEnter(m)
		if cmd == nil {
			t.Fatalf("%s: no command", name)
		}
		msg, ok := cmd().(PreviewMsg)
		if !ok || msg.help == nil {
			t.Fatalf("%s: not a guide page: %T", name, msg)
		}
		if msg.help.loc.Page != "guides/containers.md" || msg.help.loc.Anchor != anchor {
			t.Errorf("%s: opened %+v, want guides/containers.md#%s", name, msg.help.loc, anchor)
		}
	}
}

func TestHelpReportProblemOpensIssueURL(t *testing.T) {
	m, st := helpSession(t)
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	defer func() { osOpen = old }()
	helpRow(t, st, "Report a problem")
	pressEnter(m)
	if len(opened) != 1 || opened[0] != "https://github.com/amin-jalali/termocode/issues/new" {
		t.Errorf("osOpen calls = %v", opened)
	}
}

func TestHelpReportProblemCopiesOverSSH(t *testing.T) {
	m, st := helpSession(t)
	t.Setenv("SSH_CONNECTION", "1.2.3.4 5 6.7.8.9 22")
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	old := osOpen
	osOpen = func(u string) error { t.Errorf("osOpen called over SSH: %s", u); return nil }
	defer func() { osOpen = old }()
	helpRow(t, st, "Report a problem")
	pressEnter(m)
	if len(copied) != 1 || copied[0] != "https://github.com/amin-jalali/termocode/issues/new" {
		t.Errorf("copied = %v", copied)
	}
}

func TestHelpRowsClickAndDoubleClick(t *testing.T) {
	m, st := helpSession(t)
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	defer func() { osOpen = old }()
	idx := -1
	for i, r := range st.rows() {
		if r.kind == svcRowHostItem && r.name == "Report a problem" {
			idx = i
		}
	}
	y := -1
	for i, li := range svcBodyLines(st.rows(), st.cursor, svcBodyH(m.h)) {
		if li == idx {
			y = svcListTop + i
		}
	}
	if y < 0 {
		t.Fatal("row is off screen")
	}
	// A click on an action row runs it (like Connect registry…).
	next, _ := m.handleServicesSidebarMouse(3, y, tea.MouseLeft)
	*m = next
	if len(opened) != 1 {
		t.Errorf("click: osOpen calls = %v", opened)
	}
}

func TestHelpGlyphsUnicodeAndASCII(t *testing.T) {
	defer func(mode theme.IconMode) { theme.CurrentIconMode = mode }(theme.CurrentIconMode)
	for _, mode := range []theme.IconMode{theme.IconModeUnicode, theme.IconModeASCII} {
		theme.CurrentIconMode = mode
		m, _ := helpSession(t)
		lines := plainLines(m, 29, 60)
		for _, name := range []string{"Open guide", "Keys", "Report a problem"} {
			l := lineWith(lines, name)
			if l == "" {
				t.Fatalf("mode %v: no %q row:\n%s", mode, name, strings.Join(lines, "\n"))
			}
			if mode == theme.IconModeASCII && ansi.StringWidth(l) != len([]rune(l)) {
				t.Errorf("ASCII row has wide or non-ASCII glyphs: %q", l)
			}
			for _, r := range l {
				if mode == theme.IconModeASCII && r > 126 {
					t.Errorf("ASCII row %q has %q", l, r)
				}
			}
		}
	}
}

func TestHelpPaletteAndAIPolicy(t *testing.T) {
	titles := map[string]string{}
	for _, e := range PaletteCatalog() {
		titles[e.ID] = e.Title
	}
	if titles["containers-help"] != "Containers: Open Guide" {
		t.Errorf("palette title = %q", titles["containers-help"])
	}
	if p, ok := aiActionPolicies["containers-help"]; !ok || p.Gate != aiGateAuto {
		t.Errorf("AI policy = %+v ok=%v, want auto", p, ok)
	}
	m, _ := helpSession(t)
	cmd, ok := m.dispatchServicesPalette("containers-help")
	if !ok || cmd == nil {
		t.Fatalf("palette not handled: ok=%v", ok)
	}
}

func TestHelpSectionInSectionsMenu(t *testing.T) {
	m, st := helpSession(t)
	m.openContainersSectionsMenu("")
	if view := ansi.Strip(m.menu.View()); !strings.Contains(view, "Help") {
		t.Fatalf("no Help in the menu:\n%s", view)
	}
	m.handleContainersMenuSelect("section:help")
	if st.sectionShown("help") {
		t.Error("HELP still shown after the toggle")
	}
	for _, r := range st.rows() {
		if r.section == hostSection("help") {
			t.Fatalf("HELP rows remain: %s", svcRowsText(st))
		}
	}
	m.handleContainersMenuSelect("section:help")
	if !st.sectionShown("help") {
		t.Error("HELP not shown after the second toggle")
	}
}
