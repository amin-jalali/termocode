package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// databaseCardsDemo shows one of the Databases overlays and states on the
// canned local-pg tree (dbdemo console, add-picker, …): shots and goldens,
// nothing is dialled. ok is false for a name it does not know.
func (m *Model) databaseCardsDemo(variant string) (tea.Cmd, bool) {
	switch variant {
	case "console":
		m.databasesDemo("")
		return m.databaseNewConsole("local-pg"), true
	}
	return nil, false
}
