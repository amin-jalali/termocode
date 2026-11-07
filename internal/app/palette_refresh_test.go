package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	extpkg "github.com/amin-jalali/termocode/internal/ext"
	"github.com/amin-jalali/termocode/internal/keymap"
)

// The palette opened before the editor is ready has no extension rows
// yet; when the extensions load, the open palette gets them, and the
// typed search text stays.
func TestOpenPaletteRefreshesWhenExtensionsLoad(t *testing.T) {
	m := Model{keys: keymap.Default(), ext: &extState{loading: true}}
	m, _ = m.dispatchWelcomeAction(keymap.ActionCommandPalette)
	if !m.pickerOpen || m.pickerKind != pickerKindPalette {
		t.Fatal("the palette did not open")
	}
	m.picker, _ = m.picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zqxhello")})
	if it, ok := m.picker.Current(); ok {
		t.Fatalf("row %q before the extensions loaded", it.Title)
	}
	m.onExtLoaded(extLoadedMsg{reg: extpkg.Registry{
		Commands: []extpkg.Command{{ID: "t.zqxhello", Title: "Zqxhello", Ext: "t"}},
	}})
	it, ok := m.picker.Current()
	if !ok || it.ID != extPaletteIDPfx+"t.zqxhello" {
		t.Fatalf("open palette not refreshed: row %+v %v", it, ok)
	}
	if m.picker.Query() != "zqxhello" {
		t.Errorf("search text %q lost", m.picker.Query())
	}
}

// Another picker open: the extension load leaves it alone.
func TestExtensionsLoadLeavesOtherPickers(t *testing.T) {
	m := Model{keys: keymap.Default(), ext: &extState{loading: true}}
	m, _ = m.dispatchWelcomeAction(keymap.ActionCommandPalette)
	m.pickerKind = pickerKindFiles
	m.picker, _ = m.picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zqxhello")})
	m.onExtLoaded(extLoadedMsg{reg: extpkg.Registry{
		Commands: []extpkg.Command{{ID: "t.zqxhello", Title: "Zqxhello", Ext: "t"}},
	}})
	if it, ok := m.picker.Current(); ok {
		t.Fatalf("files picker got palette row %q", it.Title)
	}
}
