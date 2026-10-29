package statusbar

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

// G-UPD: a quiet "⬆ v0.2.0" chip when a newer release is out.
func TestUpdateChip(t *testing.T) {
	useUnicodeIcons(t)
	if got := renderUpdateChip(State{}); got != "" {
		t.Fatalf("no update: %q", got)
	}
	if got := stripCSI(renderUpdateChip(State{Update: "v0.2.0"})); got != "⬆ v0.2.0" {
		t.Fatalf("got %q", got)
	}
	theme.CurrentIconMode = theme.IconModeASCII
	if got := stripCSI(renderUpdateChip(State{Update: "v0.2.0"})); got != "^ v0.2.0" {
		t.Fatalf("ascii: got %q", got)
	}
}

func TestUpdateChipSpan(t *testing.T) {
	useUnicodeIcons(t)
	m := Model{}
	m.SetWidth(160)
	s := State{Line: 1, Col: 1, Branch: "main", Update: "v0.2.0"}
	x0, x1, ok := m.UpdateChipSpan(s)
	if !ok {
		t.Fatal("no span")
	}
	plain := []rune(stripCSI(m.View(s)))
	if got := string(plain[x0:x1]); got != "⬆ v0.2.0" {
		t.Errorf("span covers %q", got)
	}
	// The terminal has the keyboard: the chip still shows.
	s.TermFocused, s.TermLabel = true, "zsh"
	if _, _, ok := m.UpdateChipSpan(s); !ok {
		t.Error("no chip with the terminal focused")
	}
	if _, _, ok := m.UpdateChipSpan(State{Line: 1, Col: 1}); ok {
		t.Error("no update has a span")
	}
	// A narrow bar drops the chip before the cursor position.
	m.SetWidth(30)
	if _, _, ok := m.UpdateChipSpan(State{Line: 1, Col: 1, Project: "p", Update: "v0.2.0"}); ok {
		t.Error("narrow bar keeps the chip")
	}
}

func useUnicodeIcons(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = old })
}
