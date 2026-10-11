package specui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// Spec: Given the command palette with every command,
// when the user types a command's full title and presses Enter,
// then that command (not a look-alike) is the one selected.
func TestPaletteEveryTitleSelectsItsCommand(t *testing.T) {
	cat := app.PaletteCatalog()
	items := make([]picker.Item, len(cat))
	for i, e := range cat {
		items[i] = picker.Item{ID: e.ID, Title: e.Title, Hint: e.Hint}
	}
	seen := map[string]string{}
	for _, e := range cat {
		if other, dup := seen[e.Title]; dup {
			t.Errorf("title %q is used by both %q and %q", e.Title, other, e.ID)
			continue
		}
		seen[e.Title] = e.ID
		m := picker.NewItems("palette", items)
		m.SetSize(120, 40)
		for _, r := range e.Title {
			k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
			if r == ' ' {
				k = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
			}
			m, _ = m.Update(k)
		}
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Errorf("%q: Enter selects nothing", e.Title)
			continue
		}
		sel, ok := cmd().(picker.SelectMsg)
		if !ok || sel.ID != e.ID {
			t.Errorf("typing %q selects %q, want %q", e.Title, sel.ID, e.ID)
		}
	}
}

// Spec: Given the dashboard,
// when the user opens the palette (F1), types part of a title and presses
// Enter, then that command runs: the cheat sheet, the Settings UI and
// Zen mode open.
func TestPaletteRunsCommandByTitle(t *testing.T) {
	d := testkit.New(t, testkit.Options{W: 120, H: 36})
	run := func(query string) {
		t.Helper()
		d.Key("f1")
		d.Type(query)
		d.Key("enter")
	}
	run("Help: Show Shortcuts")
	mustSee(t, d, "Keyboard Shortcuts")
	d.Key("esc")
	run("Open Settings (UI)")
	mustSee(t, d, "⚙ Settings")
	d.Key("esc")
	mustSee(t, d, "EXPLORER")
	run("Toggle Zen Mode")
	mustNotSee(t, d, "EXPLORER")
}

// Spec: Given the user ran "Help: Show Shortcuts" from the palette,
// when the palette opens again,
// then that command is the first, pre-selected row.
func TestPaletteRecentCommandComesFirst(t *testing.T) {
	d := testkit.New(t, testkit.Options{W: 120, H: 36})
	d.Key("f1")
	d.Type("Help: Show Shortcuts")
	d.Key("enter", "esc")
	d.Key("f1")
	sel := lineWith(d, "│▌")
	if !strings.Contains(sel, "Help: Show Shortcuts") {
		t.Fatalf("selected row = %q, want Help: Show Shortcuts\n%s", sel, screen(d))
	}
}

// Spec: Given keymap.json binds Ctrl+G to Zen mode,
// when the user looks the command up in the palette,
// then its row shows Ctrl+G, and Ctrl+G toggles Zen mode.
func TestPaletteHintFollowsKeymapOverride(t *testing.T) {
	d := testkit.New(t, testkit.Options{W: 120, H: 36,
		Config: map[string]string{"keymap.json": `{"ctrl+g": "ToggleZenMode"}`}})
	d.Key("f1")
	d.Type("Toggle Zen")
	if row := lineWith(d, "View: Toggle Zen Mode"); !strings.Contains(row, "Ctrl+G") {
		t.Errorf("palette row %q does not show Ctrl+G", row)
	}
	d.Key("esc")
	mustSee(t, d, "EXPLORER")
	d.Key("ctrl+g")
	mustNotSee(t, d, "EXPLORER")
}

// Spec: Given keymap.json moves Save to Alt+S and gives Ctrl+S to Zen mode,
// when the user opens Help: Show Shortcuts,
// then the sheet shows Alt+S for Save (not Ctrl+S) and Ctrl+S for Zen
// mode: it is built from the live keymap.
func TestCheatSheetShowsKeymapOverrides(t *testing.T) {
	d := testkit.New(t, testkit.Options{W: 120, H: 60,
		Config: map[string]string{"keymap.json": `{"alt+s": "Save", "ctrl+s": "ToggleZenMode"}`}})
	d.Palette("cheat-sheet")
	mustSee(t, d, "Keyboard Shortcuts")
	save := lineWith(d, "Save the active file")
	if !strings.Contains(save, "Alt+S") || strings.Contains(save, "Ctrl+S") {
		t.Errorf("Save line = %q, want Alt+S only", save)
	}
	for i := 0; i < 20 && lineWith(d, "Zen mode") == ""; i++ {
		d.Key("pgdown") // the sheet scrolls
	}
	if zen := lineWith(d, "Zen mode"); !strings.Contains(zen, "Ctrl+S") {
		t.Errorf("Zen line = %q, want Ctrl+S", zen)
	}
}
