package app

import (
	"context"
	"regexp"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Savepoints (DBX-40): inside an open manual transaction, Databases: Set
// Savepoint… names a point to come back to, and Roll Back to Savepoint…
// (a Cancel-first card) drops the changes after it; the transaction stays
// open. They are sent on the transaction's own session, never through the
// console (where ROLLBACK is refused in manual mode).

const (
	pickerKindDBSavepoint          pickerKindEnum  = 5382
	promptKindDBSavepoint          promptKindEnum  = 5382
	confirmKindDBSavepointRollback confirmKindEnum = 5382
)

var dbSavepointName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// databaseSavepointKey is the session key with the open transaction of the
// connection the user works on (the only open one when none is in view).
func (m *Model) databaseSavepointKey() (string, tea.Cmd, bool) {
	if m.databases == nil {
		return "", m.pushToastDetail(toast.Info, "No open transaction", "Savepoints need manual commit mode and an open transaction."), false
	}
	conn := m.databaseContextConn()
	var keys []string
	if conn != "" {
		keys = m.databases.txKeys(conn)
	} else {
		for key, t := range m.databases.txs {
			if t.open {
				keys = append(keys, key)
			}
		}
	}
	if len(keys) != 1 {
		if len(keys) == 0 {
			return "", m.pushToastDetail(toast.Info, "No open transaction", "Savepoints need manual commit mode and an open transaction."), false
		}
		return "", m.pushToastDetail(toast.Info, "Several open transactions", "Open the console of the one you mean first."), false
	}
	return keys[0], nil, true
}

func (m *Model) databaseSavepointStart() tea.Cmd {
	key, cmd, ok := m.databaseSavepointKey()
	if !ok {
		return cmd
	}
	m.databases.savepointKey = key
	return m.openHTTPPrompt(promptKindDBSavepoint, "Set a savepoint on "+m.databases.dbSessionTitle(key), "Savepoint name (letters, digits, _):", "")
}

type dbSavepointMsg struct {
	key      string
	name     string
	rollback bool
	err      error
}

// databaseSavepointTyped sends SAVEPOINT name on the session of key.
func (m *Model) databaseSavepointTyped(key, name string) tea.Cmd {
	if !dbSavepointName.MatchString(name) {
		return m.pushToastDetail(toast.Warn, "Not a savepoint name", "Use letters, digits and _, starting with a letter.")
	}
	return m.databaseSavepointSend(key, name, false)
}

func (m *Model) databaseSavepointSend(key, name string, rollback bool) tea.Cmd {
	s := m.databases
	sess := s.sessions[key]
	if sess == nil || !s.txOpen(key) {
		return m.pushToastDetail(toast.Info, "No open transaction", s.dbSessionTitle(key))
	}
	if m.dbSessionBusy() || m.dbConsole().running != nil {
		return m.pushToastDetail(toast.Warn, "A query is running", "Esc cancels it.")
	}
	sql := "SAVEPOINT " + name
	if rollback {
		sql = "ROLLBACK TO SAVEPOINT " + name
	}
	timeout := loadDBConsoleSettings().timeout()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		r := sess.Run(ctx, sql, dbclient.RunOptions{})
		return dbSavepointMsg{key: key, name: name, rollback: rollback, err: r.Err}
	}
}

func (m *Model) onDBSavepoint(msg dbSavepointMsg) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Savepoint "+msg.name+" failed", dbErrorText(msg.err))
	}
	t := s.tx(msg.key)
	if !msg.rollback {
		for i, n := range t.savepoints { // the same name again moves it here
			if n == msg.name {
				t.savepoints = append(t.savepoints[:i:i], t.savepoints[i+1:]...)
				break
			}
		}
		t.savepoints = append(t.savepoints, msg.name)
		s.touch()
		return m.pushToastDetail(toast.Info, "Savepoint "+msg.name+" set", "Databases: Roll Back to Savepoint… comes back here.")
	}
	for i, n := range t.savepoints {
		if n == msg.name {
			t.savepoints = t.savepoints[:i+1]
			break
		}
	}
	s.touch()
	return tea.Batch(m.databaseRefreshAfterTx(dbTxConn(msg.key)), m.pushToastDetail(toast.Info, "Rolled back to "+msg.name, "The transaction is still open."))
}

// databaseSavepointPicker lists the savepoints of the open transaction,
// the newest first.
func (m *Model) databaseSavepointPicker() tea.Cmd {
	key, cmd, ok := m.databaseSavepointKey()
	if !ok {
		return cmd
	}
	sps := m.databases.tx(key).savepoints
	if len(sps) == 0 {
		return m.pushToastDetail(toast.Info, "No savepoints yet", "Databases: Set Savepoint… makes one.")
	}
	items := make([]picker.Item, 0, len(sps))
	for i := len(sps) - 1; i >= 0; i-- {
		items = append(items, picker.Item{ID: sps[i], Title: sps[i]})
	}
	m.databases.savepointKey = key
	m.picker = picker.NewItems("Roll back to savepoint", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSavepoint
	m.pickerOpen = true
	return nil
}

// databaseSavepointPicked asks before rolling back. ok=false: not ours.
func (m *Model) databaseSavepointPicked(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBSavepoint {
		return nil, false
	}
	s := m.databases
	if s == nil || s.savepointKey == "" {
		return nil, true
	}
	s.savepointName = id
	m.confirm = confirm.New("Roll back to savepoint "+id+"?",
		"Every change after it in "+s.dbSessionTitle(s.savepointKey)+" is dropped. The transaction stays open.",
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "rollback", Title: "Roll back", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBSavepointRollback
	return nil, true
}

func (m *Model) databaseSavepointConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBSavepointRollback {
		return nil, false
	}
	s := m.databases
	if s == nil {
		return nil, true
	}
	key, name := s.savepointKey, s.savepointName
	s.savepointKey, s.savepointName = "", ""
	if id != "rollback" || key == "" || name == "" {
		return nil, true
	}
	return m.databaseSavepointSend(key, name, true), true
}
