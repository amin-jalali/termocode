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
	case "add-picker":
		m.databasesDemo("")
		return m.openDatabaseTypePicker(), true
	case "tx-card":
		// Manual commit with two changes waiting: x on the connection asks
		// what to do with them.
		m.databasesDemo("")
		d := m.databases
		d.manual = map[string]bool{"local-pg": true}
		d.txs = map[string]*dbTxState{"local-pg": {open: true, writes: 2}}
		return m.databaseDisconnectAsk("local-pg"), true
	case "disconnect-card":
		// x on a connection with nothing waiting: the plain question.
		m.databasesDemo("")
		return m.databaseDisconnectAsk("local-pg"), true
	}
	return nil, false
}
