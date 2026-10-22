package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Transactions (plan F7): auto-commit by default; in manual mode a
// connection's first statement begins a transaction on its session and
// every later one joins it until Commit (K) or Rollback (Z). The status
// chip and row 2 of the result page say "manual commit · N not committed". Quit,
// disconnect and the switch back to auto-commit ask first when changes
// are waiting. Hiding the view keeps a session with an open transaction.

const (
	pickerKindDBTxMode pickerKindEnum = 1300 + iota
)

const (
	confirmKindDBTxEnd confirmKindEnum = 1300 + iota
	confirmKindDBContinue
	confirmKindDBSubmit
	confirmKindDBDiscard
	confirmKindDBRevertAll
	confirmKindDBStale
)

// dbTxState is the transaction of one session (key: the connection id, or
// the tree path of a database with its own session).
type dbTxState struct {
	open   bool
	writes int // statements that changed something since it began
}

// dbTxConn is the connection a session key belongs to.
func dbTxConn(key string) string {
	conn, _, _ := strings.Cut(key, "/")
	return conn
}

func (s *dbState) manualMode(conn string) bool { return s != nil && s.manual[conn] }

// tx is the transaction state of a session key (never nil).
func (s *dbState) tx(key string) *dbTxState {
	if s.txs == nil {
		s.txs = map[string]*dbTxState{}
	}
	t := s.txs[key]
	if t == nil {
		t = &dbTxState{}
		s.txs[key] = t
	}
	return t
}

func (s *dbState) txOpen(key string) bool {
	if s == nil || s.txs == nil {
		return false
	}
	t := s.txs[key]
	return t != nil && t.open
}

// txKeys are the session keys of conn with an open transaction, sorted.
func (s *dbState) txKeys(conn string) []string {
	var out []string
	for key, t := range s.txs {
		if t.open && dbTxConn(key) == conn {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// txWrites is the number of uncommitted statements on conn.
func (s *dbState) txWrites(conn string) int {
	n := 0
	for key, t := range s.txs {
		if t.open && dbTxConn(key) == conn {
			n += t.writes
		}
	}
	return n
}

// txMarker is the state for row 2 and the chip: "" in auto-commit,
// "manual commit", "manual commit · in transaction" or
// "manual commit · 2 not committed".
func (s *dbState) txMarker(conn string) string {
	if !s.manualMode(conn) {
		return ""
	}
	if n := s.txWrites(conn); n > 0 {
		return fmt.Sprintf("manual commit · %d not committed", n)
	}
	if len(s.txKeys(conn)) > 0 {
		return "manual commit · in transaction"
	}
	return "manual commit"
}

// dbTxControl reports a statement that starts or ends a transaction: in
// manual mode those go through Commit and Rollback instead.
func dbTxControl(sql string) bool {
	switch strings.ToUpper(firstSQLWord(sql)) {
	case "BEGIN", "START", "COMMIT", "ROLLBACK", "END":
		return true
	}
	return false
}

// dbReadInTx reads inside the open transaction (it sees its own changes)
// between a savepoint and a rollback to it, so a function in a filter that
// writes leaves nothing behind.
func dbReadInTx(ctx context.Context, sess dbclient.Session, sql string, opt dbclient.RunOptions) dbclient.Result {
	if r := sess.Run(ctx, "SAVEPOINT termocode_read", dbclient.RunOptions{}); r.Err != nil {
		return r
	}
	res := sess.Run(ctx, sql, opt)
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	sess.Run(bg, "ROLLBACK TO SAVEPOINT termocode_read", dbclient.RunOptions{})
	sess.Run(bg, "RELEASE SAVEPOINT termocode_read", dbclient.RunOptions{})
	return res
}

// databaseContextConn is the SQL connection the user works on: the SQL
// console or result page in the editor, else the tree's row.
func (m *Model) databaseContextConn() string {
	if m.databases == nil {
		return ""
	}
	s := m.dbConsole()
	if m.focus == FocusEditor {
		if base := lastPathPart(m.editor.Path()); base == "SQL Result" && s.shown != nil {
			return s.shown.conn
		}
		if file, lines, ok := m.httpBufText(0); ok && strings.EqualFold(lastExt(file), ".sql") {
			if id := s.consoleConn(file, lines); id != "" {
				return id
			}
		}
	}
	if row, ok := m.databaseCurrentRow(); ok && row.conn != "" {
		if c, ok := m.databases.connection(row.conn); ok && dbclient.DialectFor(c.Driver) != nil {
			return row.conn
		}
	}
	if s.shown != nil {
		return s.shown.conn
	}
	return ""
}

func lastPathPart(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// databaseTxModePicker asks Auto-commit or Manual for conn.
func (m *Model) databaseTxModePicker(conn string) tea.Cmd {
	m.ensureDatabasesLoaded()
	c, ok := m.databases.connection(conn)
	if !ok || dbclient.DialectFor(c.Driver) == nil {
		return m.pushToastDetail(toast.Info, "Select a SQL connection", "Transaction mode is set per SQL connection.")
	}
	auto, manual := "current", ""
	if m.databases.manualMode(conn) {
		auto, manual = "", "current"
	}
	m.databases.txModeConn = conn
	m.picker = picker.NewItems("Transaction mode for "+sanitizeDBText(dbConnName(c)), []picker.Item{
		{ID: "auto", Title: "Auto-commit", Hint: dbJoinHint(auto, "each statement is saved at once")},
		{ID: "manual", Title: "Manual", Hint: dbJoinHint(manual, "Commit (K) or Rollback (Z) ends it")},
	})
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBTxMode
	m.pickerOpen = true
	return nil
}

func dbJoinHint(a, b string) string {
	if a == "" {
		return b
	}
	return a + " · " + b
}

func (m *Model) databaseTxPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBTxMode {
		return nil, false
	}
	conn := m.databases.txModeConn
	m.databases.txModeConn = ""
	if conn == "" {
		return nil, true
	}
	return m.databaseSetTxMode(conn, id == "manual"), true
}

// databaseSetTxMode switches conn's mode. Leaving manual mode with changes
// waiting asks Commit / Roll back first.
func (m *Model) databaseSetTxMode(conn string, manual bool) tea.Cmd {
	s := m.databases
	if s.manual == nil {
		s.manual = map[string]bool{}
	}
	if manual == s.manual[conn] {
		return nil
	}
	name := m.dbConnLabel(conn)
	if manual {
		s.manual[conn] = true
		s.touch()
		return tea.Batch(m.databaseShowPageIf(conn), m.pushToastDetail(toast.Info, "Manual transactions on "+name, "Changes wait for Commit (K) or Rollback (Z)."))
	}
	switchAuto := func(m *Model) tea.Cmd {
		delete(m.databases.manual, conn)
		m.databases.touch()
		return tea.Batch(m.databaseShowPageIf(conn), m.pushToast(toast.Info, "Auto-commit on "+name))
	}
	if s.txWrites(conn) > 0 {
		return m.databaseTxCard(conn, "Auto-commit needs the open transaction ended first.", switchAuto)
	}
	if len(s.txKeys(conn)) > 0 {
		return m.databaseEndTx(conn, false, switchAuto) // nothing changed: just end it
	}
	return switchAuto(m)
}

func (m *Model) dbConnLabel(conn string) string {
	if c, ok := m.databases.connection(conn); ok {
		return sanitizeDBText(dbConnName(c))
	}
	return sanitizeDBText(conn)
}

// databaseShowPageIf redraws the result page when it shows conn.
func (m *Model) databaseShowPageIf(conn string) tea.Cmd {
	if s := m.dbConsole(); s.shown != nil && s.shown.conn == conn && s.running == nil {
		s.keepCursor = true
		return m.databaseShowPage()
	}
	return nil
}

// databaseTxCard asks what to do with the uncommitted changes of conn
// before after runs: Commit, Roll back or Cancel.
func (m *Model) databaseTxCard(conn, why string, after func(*Model) tea.Cmd) tea.Cmd {
	s := m.databases
	n := s.txWrites(conn)
	s.txCardConn, s.txCardAfter = conn, after
	m.confirm = confirm.New(m.dbConnLabel(conn)+" has "+dbPlural(n, "uncommitted change"),
		why+"\n\nCommit saves them. Roll back drops them.", []confirm.Button{
			{ID: "commit", Title: "Commit", Style: confirm.StylePrimary},
			{ID: "rollback", Title: "Roll back", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBTxEnd
	return nil
}

type dbTxDoneMsg struct {
	conn   string
	commit bool
	keys   []string
	errs   []error
	sqlite []bool // the session is a sqlite3 sidecar (a failed COMMIT keeps it open)
	after  func(*Model) tea.Cmd
}

// databaseEndTx commits or rolls back every open transaction of conn off
// the UI goroutine; after runs when all of them ended well.
func (m *Model) databaseEndTx(conn string, commit bool, after func(*Model) tea.Cmd) tea.Cmd {
	if m.dbSessionBusy() {
		return m.pushToastDetail(toast.Warn, "An import is running", "Esc cancels it first.")
	}
	s := m.databases
	keys := s.txKeys(conn)
	if len(keys) == 0 {
		if after != nil {
			return after(m)
		}
		return m.pushToastDetail(toast.Info, "No open transaction", m.dbConnLabel(conn)+" has nothing to commit or roll back.")
	}
	sessions := make([]dbclient.Session, len(keys))
	msg := dbTxDoneMsg{conn: conn, commit: commit, keys: keys, errs: make([]error, len(keys)), sqlite: make([]bool, len(keys)), after: after}
	for i, k := range keys {
		sessions[i] = s.sessions[k]
		_, msg.sqlite[i] = sessions[i].(*dbclient.SQLiteSession)
	}
	timeout := loadDBConsoleSettings().timeout()
	return func() tea.Msg {
		for i, sess := range sessions {
			if sess == nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			if commit {
				msg.errs[i] = sess.Commit(ctx)
			} else {
				msg.errs[i] = sess.Rollback(ctx)
			}
			cancel()
		}
		return msg
	}
}

func (m *Model) onDBTxDone(msg dbTxDoneMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	s := m.databases
	var failed error
	for i, key := range msg.keys {
		if msg.errs[i] != nil {
			failed = msg.errs[i]
			if msg.sqlite[i] {
				continue // sqlite3 keeps the transaction after a failed COMMIT
			}
		}
		delete(s.txs, key)
	}
	s.touch()
	verb, done := "Commit", "Committed"
	if !msg.commit {
		verb, done = "Rollback", "Rolled back"
	}
	refresh := m.databaseRefreshAfterTx(msg.conn)
	if failed != nil {
		return tea.Batch(refresh, m.pushToastDetail(toast.Errr, verb+" failed on "+m.dbConnLabel(msg.conn), dbErrorText(failed)))
	}
	cmds := []tea.Cmd{refresh, m.pushToast(toast.Info, done+" · "+m.dbConnLabel(msg.conn))}
	if msg.after != nil {
		cmds = append(cmds, msg.after(m))
	}
	return tea.Batch(cmds...)
}

// databaseRefreshAfterTx reloads the rows on the page after a commit or a
// rollback of its connection (a rollback undoes what they showed): a data
// view's page, or a console result that is a read.
func (m *Model) databaseRefreshAfterTx(conn string) tea.Cmd {
	cs := m.dbConsole()
	t := cs.curTab()
	if t == nil || t.run == nil || t.run.conn != conn || cs.running != nil {
		return nil
	}
	redraw := m.databaseShowPageIf(conn)
	if !t.dirty() && t.run.by != "ai" && t.run.describe == nil && len(t.run.result.Columns) > 0 {
		return tea.Batch(redraw, m.databaseRefreshTab(t))
	}
	return redraw
}

// databaseTxConfirm handles the uncommitted-changes card.
func (m *Model) databaseTxConfirm(id string) tea.Cmd {
	s := m.databases
	conn, after := s.txCardConn, s.txCardAfter
	s.txCardConn, s.txCardAfter = "", nil
	switch id {
	case "commit":
		return m.databaseEndTx(conn, true, after)
	case "rollback":
		return m.databaseEndTx(conn, false, after)
	}
	return nil
}

// databaseCommitKey and databaseRollbackKey end the transaction of the
// connection the user works on (K / Z on the page, the palette).
func (m *Model) databaseCommitKey(commit bool) tea.Cmd {
	conn := m.databaseContextConn()
	if conn == "" {
		return m.pushToastDetail(toast.Info, "No SQL connection", "Open a console or a result first.")
	}
	return m.databaseEndTx(conn, commit, nil)
}

// databasesBeforeQuit asks about uncommitted changes before quitting.
func (m *Model) databasesBeforeQuit() (tea.Cmd, bool) {
	if m.databases == nil {
		return nil, false
	}
	var conns []string
	for key, t := range m.databases.txs {
		if t.open && t.writes > 0 {
			conns = append(conns, dbTxConn(key))
		}
	}
	if len(conns) == 0 {
		return nil, false
	}
	sort.Strings(conns)
	return m.databaseTxCard(conns[0], "Quit closes the connection.", func(m *Model) tea.Cmd { return m.quitNow() }), true
}

// databaseDisconnectAsk is x / Disconnect: the uncommitted-changes card
// first when changes wait, else the usual question.
func (m *Model) databaseDisconnectAsk(id string) tea.Cmd {
	if m.databases != nil && m.databases.txWrites(id) > 0 {
		return m.databaseTxCard(id, "Disconnect closes the connection.", func(m *Model) tea.Cmd { return m.databaseDisconnectNow(id) })
	}
	m.databaseConfirmDisconnect(id)
	return nil
}

// dbTxStatus is " · manual commit · 2 not committed" for a run's lens.
func (m *Model) dbTxStatus(conn string) string {
	if mk := m.databases.txMarker(conn); mk != "" {
		return " · " + mk
	}
	return ""
}

// dbTxHint is the hint for K / Z while a transaction is open.
func (m *Model) dbTxHints(conn string) []string {
	if len(m.databases.txKeys(conn)) == 0 {
		return nil
	}
	return []string{"K commit", "Z rollback"}
}
