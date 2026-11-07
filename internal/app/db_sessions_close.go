package app

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Databases: Sessions… (DBX-40): every open session (a connection's main
// one and the per-database ones) with its transaction; Enter asks, then
// closes that one session. The server rolls back an open transaction.

const pickerKindDBSessions pickerKindEnum = 1390

const confirmKindDBSessionClose confirmKindEnum = 1390

// dbSessionsPickerWidth gives the session hint room; it still fits the window.
const dbSessionsPickerWidth = 110

// dbSessionTitle is "local-pg" or "local-pg › audit" for a session key.
func (s *dbState) dbSessionTitle(key string) string {
	conn, rest, _ := strings.Cut(key, "/")
	name := conn
	if c, ok := s.connection(conn); ok {
		name = dbConnName(c)
	}
	if db, ok := strings.CutPrefix(rest, "db/"); ok && db != "" {
		name += " › " + db
	}
	if n := dbExtraSessionNumber(key); n > 0 {
		name += fmt.Sprintf(" · session %d", n)
	}
	return sanitizeDBText(name)
}

// dbSessionHint is the mode and transaction of a session key.
func (s *dbState) dbSessionHint(key string) string {
	mode := "auto-commit"
	if s.manualMode(dbTxConn(key)) {
		mode = "manual commit"
	}
	if s.txOpen(key) {
		t := s.tx(key)
		return fmt.Sprintf("%s · transaction open · %d not committed", mode, t.writes)
	}
	return mode
}

func (m *Model) databaseSessionsPicker() tea.Cmd {
	m.ensureDatabasesLoaded()
	s := m.databases
	keys := make([]string, 0, len(s.sessions))
	for key, sess := range s.sessions {
		if sess != nil {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return m.pushToastDetail(toast.Info, "No open database sessions", "Connect a database first.")
	}
	sort.Strings(keys)
	items := make([]picker.Item, 0, len(keys))
	for _, key := range keys {
		items = append(items, picker.Item{ID: key, Title: s.dbSessionTitle(key), Hint: s.dbSessionHint(key)})
	}
	m.picker = picker.NewItems("Database sessions · Enter closes one", items)
	// Wide box: the hint ("manual commit · transaction open · 2 not
	// committed") is cut at half the row, so the count would be lost.
	m.picker.SetWidth(dbSessionsPickerWidth)
	m.picker.HideCountWhenEmpty()
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSessions
	m.pickerOpen = true
	return nil
}

// databaseSessionsPick asks before closing the picked session.
func (m *Model) databaseSessionsPick(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBSessions {
		return nil, false
	}
	s := m.databases
	if s == nil || s.sessions[id] == nil {
		return nil, true
	}
	if c := m.dbConsole(); c.running != nil {
		return m.pushToastDetail(toast.Warn, "A query is running", "Esc cancels it."), true
	}
	body := "Opens again on the next query."
	if id == dbTxConn(id) {
		body = "This is the connection's main session: the connection is disconnected."
	}
	if s.txOpen(id) {
		body = fmt.Sprintf("The open transaction is rolled back (%d not committed). ", s.tx(id).writes) + body
	}
	s.closeSessionKey = id
	m.confirm = confirm.New("Close session "+s.dbSessionTitle(id)+"?", body,
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "close", Title: "Close session", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBSessionClose
	return nil, true
}

// databaseSessionsConfirm closes the session on "close".
func (m *Model) databaseSessionsConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBSessionClose {
		return nil, false
	}
	s := m.databases
	if s == nil {
		return nil, true
	}
	key := s.closeSessionKey
	s.closeSessionKey = ""
	if id != "close" || key == "" || s.sessions[key] == nil {
		return nil, true
	}
	title := s.dbSessionTitle(key)
	conn := dbTxConn(key)
	if key == conn {
		m.databaseDisconnectNow(conn)
	} else {
		_ = s.sessions[key].Close()
		delete(s.sessions, key)
		delete(s.txs, key)
	}
	s.touch()
	return m.pushToast(toast.Info, "Closed session "+title), true
}
