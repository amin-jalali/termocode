package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
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
	case "filter":
		// "/" and "us": the tree narrows while the text is typed.
		m.databasesDemo("")
		d := m.databases
		d.filter, d.filterTyping = "us", true
		d.cursor = 0
		d.touch()
		return nil, true
	case "keys":
		// ? on the tree: the key card.
		m.databasesDemo("")
		return m.databaseKeysHelp(), true
	case "disconnect-card":
		// x on a connection with nothing waiting: the plain question.
		m.databasesDemo("")
		return m.databaseDisconnectAsk("local-pg"), true
	case "redis-delete":
		// D on the selected key: the card with the exact DEL command.
		m.databaseRedisDemo()
		return m.databaseRedisAsk(&dbRedisWrite{conn: "prod-cache", db: 0, key: "session:ab12", args: []string{"DEL", "session:ab12"}, deleted: true}), true
	case "password-move":
		// Enter on a personal connection whose file holds a password.
		m.databasesDemo("")
		d := m.databases
		c := dbDemoConnection()
		c.Source, c.Password = dbclient.SourceUser, "not-a-real-password"
		d.conns = []dbclient.Conn{c}
		return m.databaseOpen(c.ID, false), true
	case "ai-card":
		// The AI asks to run a write: the card shows the target and the
		// exact statement. The wait command is dropped: nobody answers.
		m.databasesDemo("")
		c := dbDemoConnection()
		human := dbAIDescribe(&dbAICall{conn: c, sql: "UPDATE users SET plan = 'pro' WHERE id = 42", kind: dbclient.KindWrite}, "db_query")
		tool := ai.Tool{Name: "db_query", Display: "run a database statement"}
		m.onAIChatConfirm(aiChatConfirmMsg{gen: m.aiS().chatGen, tool: tool, human: human, reply: make(chan bool, 1), ch: make(chan tea.Msg)})
		return nil, true
	case "connecting":
		// Enter was pressed on local-pg: the row waits for the server.
		m.databasesDemo("")
		d := m.databases
		n := d.nodes["local-pg"]
		n.children, n.loaded, n.loading = nil, false, true
		d.nodes["local-pg"] = n
		d.cursor = 0
		d.touch()
		return nil, true
	}
	return nil, false
}
