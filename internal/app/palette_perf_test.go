package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// paletteModel is a 120×40 true-colour window with the command palette open on every
// command (F1).
func paletteModel(tb testing.TB) Model {
	tb.Helper()
	tb.Setenv("XDG_CONFIG_HOME", tb.TempDir())
	perfColour(tb)
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m1, _ := m0.(Model).Update(tea.KeyMsg{Type: tea.KeyF1})
	m := m1.(Model)
	if !m.pickerOpen || !strings.Contains(m.View(), "Command Palette") {
		tb.Fatal("F1 did not open the palette")
	}
	return m
}

// BenchmarkPaletteFrame is one frame of the open palette (every command
// listed): what F1 costs once the items are built.
func BenchmarkPaletteFrame(b *testing.B) {
	m := paletteModel(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// BenchmarkPaletteFilterKey is one typed letter in the palette search:
// Update (the fuzzy filter) and the frame after it.
func BenchmarkPaletteFilterKey(b *testing.B) {
	m := paletteModel(b)
	keys := []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("g")}, {Type: tea.KeyRunes, Runes: []rune("i")}, {Type: tea.KeyBackspace}, {Type: tea.KeyBackspace}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nm, _ := m.Update(keys[i%len(keys)])
		m = nm.(Model)
		_ = m.View()
	}
}
