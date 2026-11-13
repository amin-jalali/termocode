package editor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTranslateKeyEdgeCases(t *testing.T) {
	if got := translateKey(tea.KeyMsg{Type: tea.KeyRunes}); got != "" {
		t.Fatalf("no runes: %q", got)
	}
	for name, want := range map[string]string{
		"":         "",
		"ctrl++":   "<C-+>",
		"alt+f13":  "<M-S-F1>",
		"ctrl+f5":  "<C-F5>",
		"x":        "x",
		"ctrl+x":   "<C-x>",
		"<":        "<lt>",
		"shift+up": "<S-Up>",
	} {
		if got := keyNameToNvim(name); got != want {
			t.Errorf("keyNameToNvim(%q) = %q, want %q", name, got, want)
		}
	}
}

// An editor with no size draws nothing; ViewLines gives one empty row.
func TestEditorWithoutSizeDrawsNothing(t *testing.T) {
	m := New(nil)
	if m.View(true) != "" {
		t.Fatal("View without a size")
	}
	if rows := m.ViewLines(true); len(rows) != 1 || rows[0] != "" {
		t.Fatalf("ViewLines %q", rows)
	}
	if m.LineCount() < 0 {
		t.Fatal("line count")
	}
	if text, runs := m.RowRuns(0, 0, 3); text != "" && len(runs) == 0 {
		t.Fatalf("RowRuns %q %v", text, runs)
	}
}
