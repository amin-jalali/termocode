package app

import "github.com/amin-jalali/termocode/internal/picker"

// commandPaletteItems are the rows of the command palette, built from the
// live state (keymap, user commands, extensions, recents).
func (m Model) commandPaletteItems() []picker.Item {
	// Splice user-defined commands at the bottom so they're visible but
	// don't push built-ins down the fuzzy-match list.
	items := m.selfReloadPaletteFilter(updatePaletteFilter(paletteItems())) // G-UPD: Roll Back only with termocode.old; reload only with a new build
	items = append(items, agentPaletteFilter(tasksPaletteItems())...)       // Group C; agents only when installed
	// Hints come from the live keymap (keymap.json overrides too).
	items = withKeyHints(items, m.keys)
	items = append(items, userCommandsItems()...)
	items = append(items, m.extPaletteItems()...) // Group I
	// Promote frequently-used commands to the front so muscle memory
	// works: type a few letters and the right command is already
	// near the cursor.
	return promoteRecents(items, loadPaletteRecents())
}

// refreshOpenPalette rebuilds the rows of an open command palette. A
// palette opened before the editor was ready has no extension rows (they
// load after nvim.ReadyMsg); this runs on ready and on the extension load.
// The search text and the row under the cursor stay.
func (m *Model) refreshOpenPalette() {
	if !m.pickerOpen || m.pickerKind != pickerKindPalette {
		return
	}
	m.picker.SetItems(m.commandPaletteItems())
}
