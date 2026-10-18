package app

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/preview"
)

// openCheatSheet opens a non-interactive preview overlay listing the
// keyboard shortcuts. Closes with Esc / q like any other preview. The
// list is built from the live keymap (defaults plus keymap.json), so a
// rebound key shows where the user put it.
func (m *Model) openCheatSheet() {
	m.preview = preview.New(" Keyboard Shortcuts ", cheatSheetBody(m.keys))
	m.preview.SetSize(m.w, m.h)
	m.previewOpen = true
}

// cheatSheetKeyCol is the width of the key column. Longer keys push the
// description to the right instead of being cut.
const cheatSheetKeyCol = 20

// cheatSheetBody renders the sheet for keymap k: every bound action
// grouped by area (keymap.Areas), then the keys termocode adds inside
// Neovim (EditorKeyCatalog), then a few tips.
func cheatSheetBody(k keymap.KeyMap) string {
	var b strings.Builder
	row := func(keys, desc string) {
		pad := cheatSheetKeyCol - len([]rune(keys))
		if pad < 2 {
			pad = 2
		}
		fmt.Fprintf(&b, "  %s%s%s\n", keys, strings.Repeat(" ", pad), strings.TrimSuffix(desc, "."))
	}

	byArea := map[string][]keymap.Action{}
	for _, a := range keymap.AllActions() {
		if len(k.KeysFor(a)) == 0 {
			continue
		}
		area := keymap.Describe(a).Area
		byArea[area] = append(byArea[area], a)
	}
	b.WriteString("\n")
	for _, area := range keymap.Areas {
		acts := byArea[area]
		if len(acts) == 0 {
			continue
		}
		b.WriteString(strings.ToUpper(area) + "\n")
		for _, a := range acts {
			row(humanKeys(k.KeysFor(a)), keymap.Describe(a).Description)
		}
		b.WriteString("\n")
	}

	b.WriteString("EDITOR (NEOVIM)\n")
	for _, e := range EditorKeyCatalog() {
		desc := e.Description
		if e.Mode != "All" {
			desc = strings.TrimSuffix(desc, ".") + " (" + e.Mode + ")"
		}
		row(e.Key, desc)
	}
	b.WriteString("\n")

	b.WriteString("TIPS\n")
	b.WriteString("  Every command is in the command palette, also the ones without a key.\n")
	if keys := humanKeys(k.KeysFor(keymap.ActionContextHelp)); keys != "" {
		b.WriteString("  " + keys + " opens the guide for the focused item; on a hover label, click More info.\n")
	}
	b.WriteString("  Change keys with Preferences: Customize Keybindings (keymap.json).\n")
	b.WriteString("  In the terminal, Ctrl keys go to the shell (see terminal_skip_shell).\n")
	b.WriteString("  TERMOCODE_ICON_MODE=nerd_font|unicode|ascii overrides the icons.\n")
	return b.String()
}

// humanKeys joins key strings as the user reads them ("F8 / Alt+F"),
// dropping duplicates (xterm's "f16" and "shift+f4" are both Shift+F4).
func humanKeys(keys []string) string {
	var out []string
	seen := map[string]bool{}
	for _, s := range keys {
		h := keymap.HumanKey(s)
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return strings.Join(out, " / ")
}
