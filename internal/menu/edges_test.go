package menu

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCtrlCClosesTheMenu(t *testing.T) {
	_, cmd := New(sampleItems(), 0, 0).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Ctrl+C did nothing")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Errorf("Ctrl+C sent %T", cmd())
	}
}

func TestOnlySeparatorsSelectNothing(t *testing.T) {
	m := New([]Item{{Sep: true}, {Sep: true}}, 0, 0)
	if m.SelectedID() != "" {
		t.Errorf("SelectedID = %q on a separator-only menu", m.SelectedID())
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Error("Enter selected something")
	}
	e := New(nil, 0, 0)
	if e.SelectedID() != "" {
		t.Error("empty menu has a selection")
	}
	if _, cmd := e.Update(tea.KeyMsg{Type: tea.KeyDown}); cmd != nil {
		t.Error("Down on an empty menu did something")
	}
}

func TestSelectIDUnknownKeepsPlace(t *testing.T) {
	m := New(sampleItems(), 0, 0)
	before := m.SelectedID()
	m.SelectID("no-such-row")
	if m.SelectedID() != before {
		t.Errorf("unknown id moved the highlight to %q", m.SelectedID())
	}
}

func TestOtherMouseButtonsDoNothing(t *testing.T) {
	m := New(sampleItems(), 2, 2)
	m.SetScreenSize(80, 24)
	x, y := m.clampedAnchor()
	for _, typ := range []tea.MouseEventType{tea.MouseRight, tea.MouseRelease, tea.MouseWheelUp} {
		if _, cmd := m.HandleMouse(tea.MouseMsg{Type: typ, X: x + 2, Y: y + 1}); cmd != nil {
			t.Errorf("%v on a row sent %T", typ, cmd())
		}
	}
	// A click on the border selects nothing and keeps the menu open.
	if _, cmd := m.HandleMouse(tea.MouseMsg{Type: tea.MouseLeft, X: x + 2, Y: y}); cmd != nil {
		t.Errorf("click on the top border sent %T", cmd())
	}
}

func TestMenuStaysOnTinyScreens(t *testing.T) {
	m := New(sampleItems(), 5, 5)
	m.SetScreenSize(10, 3) // smaller than the menu
	x, y := m.clampedAnchor()
	if x != 0 || y != 0 {
		t.Errorf("anchor on a tiny screen = %d,%d; want 0,0", x, y)
	}
	bx, by, bw, bh := m.Bounds()
	if bx != x || by != y {
		t.Errorf("Bounds at %d,%d, anchor at %d,%d", bx, by, x, y)
	}
	if lines := strings.Split(m.View(), "\n"); len(lines) != bh || ansi.StringWidth(lines[0]) != bw {
		t.Errorf("View %d lines × %d, Bounds says %d × %d", len(lines), ansi.StringWidth(lines[0]), bh, bw)
	}
}

func TestWideIconsAndLongHintsKeepRowsEven(t *testing.T) {
	items := []Item{
		{ID: "a", Title: "Open", Icon: "📂", Hint: "Ctrl+O"},
		{ID: "b", Title: "Close", Icon: "x"},
		{Sep: true},
		{ID: "c", Title: "A much longer title here", Hint: "Ctrl+Shift+Alt+L"},
		{ID: "d", Title: "سلام", Icon: ""},
	}
	m := New(items, 0, 0)
	m.SetScreenSize(120, 40)
	lines := strings.Split(m.View(), "\n")
	want := ansi.StringWidth(lines[0])
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != want {
			t.Errorf("row %d is %d wide, want %d: %q", i, w, want, ansi.Strip(l))
		}
	}
	plain := ansi.Strip(m.View())
	for _, s := range []string{"Ctrl+Shift+Alt+L", "سلام", "📂 Open"} {
		if !strings.Contains(plain, s) {
			t.Errorf("menu lacks %q", s)
		}
	}
	// A row too narrow for its hint still keeps a 2-cell gap.
	row := renderItemRow(Item{Title: "Title", Hint: "Hint"}, false, 5, false)
	if !strings.Contains(ansi.Strip(row), "Title  Hint") {
		t.Errorf("narrow row = %q, want a 2-cell gap before the hint", ansi.Strip(row))
	}
}

func TestOverlayPadsShortBaseLines(t *testing.T) {
	m := New([]Item{{ID: "a", Title: "Alpha"}}, 10, 1)
	m.SetScreenSize(80, 24)
	base := "short\n\nx" // lines shorter than the anchor
	out := strings.Split(m.Overlay(base), "\n")
	if len(out) != 3 {
		t.Fatalf("overlay has %d lines, want the base's 3", len(out))
	}
	if !strings.HasPrefix(ansi.Strip(out[1]), strings.Repeat(" ", 10)+"╭") {
		t.Errorf("menu top not at column 10: %q", ansi.Strip(out[1]))
	}
	if !strings.HasPrefix(ansi.Strip(out[0]), "short") {
		t.Errorf("line above the menu changed: %q", ansi.Strip(out[0]))
	}
	if !strings.Contains(ansi.Strip(out[2]), "Alpha") {
		t.Errorf("menu row missing: %q", ansi.Strip(out[2]))
	}
}
