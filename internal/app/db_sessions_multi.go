package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Several sessions per connection (DBX-40). A SQL console can run on its
// own extra session of a connection ("pg/session/2"): two consoles then keep
// separate transactions, temp tables and SET values. Databases: Console
// Session… picks the main session, an open extra one or a new one; the
// extra session opens on the console's next run (after the main one, which
// holds the password and the SSH tunnel). Databases: Sessions… lists and
// closes them; Disconnect closes them all.

const pickerKindDBConsoleSession pickerKindEnum = 1472

const dbExtraSessionTag = "/session/"

// dbIsExtraSession reports whether key is an extra session of a connection.
func dbIsExtraSession(key string) bool {
	conn := dbTxConn(key)
	n, ok := strings.CutPrefix(key, conn+dbExtraSessionTag)
	if !ok || n == "" {
		return false
	}
	_, err := strconv.Atoi(n)
	return err == nil
}

// dbExtraSessionNumber is 2 for "pg/session/2" (0 for any other key).
func dbExtraSessionNumber(key string) int {
	if !dbIsExtraSession(key) {
		return 0
	}
	n, _ := strconv.Atoi(key[len(dbTxConn(key)+dbExtraSessionTag):])
	return n
}

// dbExtraSessionDriverOK: the drivers whose sessions open again with the
// same settings (postgres and mysql families). sqlite shares one file
// process, the others keep one client per connection.
func dbExtraSessionDriverOK(driver string) bool {
	switch dbDriverFamily(driver) {
	case "postgres", "mysql":
		return true
	}
	return false
}

// bindConsoleSession makes file run on key (a connection's extra session).
func (s *dbState) bindConsoleSession(file, key string) {
	if s.consoleSess == nil {
		s.consoleSess = map[string]string{}
	}
	if key == "" || !dbIsExtraSession(key) {
		delete(s.consoleSess, file)
		return
	}
	s.consoleSess[file] = key
}

// consoleSessionKey is the extra session file runs on when it runs on conn
// ("": the connection's main session).
func (s *dbState) consoleSessionKey(file, conn string) string {
	if s == nil || file == "" {
		return ""
	}
	key := s.consoleSess[file]
	if key == "" || dbTxConn(key) != conn {
		return ""
	}
	return key
}

// nextExtraSession is a new extra session key of conn: one more than any
// key used so far (closed ones too), starting at 2.
func (s *dbState) nextExtraSession(conn string) string {
	if s.sessionSeq == nil {
		s.sessionSeq = map[string]int{}
	}
	n := max(s.sessionSeq[conn], 1)
	for key := range s.sessions {
		if dbTxConn(key) == conn {
			n = max(n, dbExtraSessionNumber(key))
		}
	}
	for _, key := range s.consoleSess {
		if dbTxConn(key) == conn {
			n = max(n, dbExtraSessionNumber(key))
		}
	}
	n++
	s.sessionSeq[conn] = n
	return conn + dbExtraSessionTag + strconv.Itoa(n)
}

// databaseConsoleSessionPicker is Databases: Console Session… for the SQL
// buffer in the editor.
func (m *Model) databaseConsoleSessionPicker() tea.Cmd {
	m.ensureDatabasesLoaded()
	file, lines, ok := m.httpBufText(0)
	if !ok || !strings.EqualFold(lastExt(file), ".sql") {
		return m.pushToastDetail(toast.Info, "Open a SQL console first", "The session is chosen for one .sql file.")
	}
	conn := m.dbConsole().consoleConn(file, lines)
	if conn == "" {
		return m.pushToastDetail(toast.Info, "This console has no connection yet", "Run a statement once to pick it, or add a \"-- @conn <id>\" line.")
	}
	return m.databaseConsoleSessionPickerFor(file, conn)
}

func (m *Model) databaseConsoleSessionPickerFor(file, conn string) tea.Cmd {
	s := m.databases
	c, ok := s.connection(conn)
	if !ok {
		return m.pushToastDetail(toast.Warn, "Connection not found", sanitizeDBText(conn))
	}
	if !dbExtraSessionDriverOK(c.Driver) {
		return m.pushToastDetail(toast.Info, "One session per connection", sanitizeDBText(dbConnName(c))+" ("+c.Driver+") consoles share the connection's session.")
	}
	current := s.consoleSessionKey(file, conn)
	mark := func(key, hint string) string {
		if key == current || key == conn && current == "" {
			return "in use · " + hint
		}
		return hint
	}
	mainHint := "not open"
	if s.sessions[conn] != nil {
		mainHint = s.dbSessionHint(conn)
	}
	items := []picker.Item{{ID: conn, Title: "Main session", Hint: mark(conn, mainHint)}}
	var extra []string
	for key, sess := range s.sessions {
		if sess != nil && dbTxConn(key) == conn && dbIsExtraSession(key) {
			extra = append(extra, key)
		}
	}
	if current != "" && s.sessions[current] == nil {
		extra = append(extra, current) // chosen, opens on the next run
	}
	sort.Slice(extra, func(i, j int) bool { return dbExtraSessionNumber(extra[i]) < dbExtraSessionNumber(extra[j]) })
	for _, key := range extra {
		hint := "opens on the next run"
		if s.sessions[key] != nil {
			hint = s.dbSessionHint(key)
		}
		items = append(items, picker.Item{ID: key, Title: fmt.Sprintf("Session %d", dbExtraSessionNumber(key)), Hint: mark(key, hint)})
	}
	items = append(items, picker.Item{ID: "new", Title: "New session", Hint: "its own transaction, temp tables and SET values"})
	s.sessPickFile, s.sessPickConn = file, conn
	m.picker = picker.NewItems("Run this console on which session of "+sanitizeDBText(dbConnName(c))+"?", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBConsoleSession
	m.pickerOpen = true
	return nil
}

// databaseConsoleSessionPick binds the console to the picked session.
func (m *Model) databaseConsoleSessionPick(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBConsoleSession {
		return nil, false
	}
	s := m.databases
	if s == nil || s.sessPickFile == "" {
		return nil, true
	}
	file, conn := s.sessPickFile, s.sessPickConn
	s.sessPickFile, s.sessPickConn = "", ""
	old := s.consoleSessionKey(file, conn)
	key := id
	switch {
	case id == "new":
		key = s.nextExtraSession(conn)
	case id == conn:
		key = ""
	case !dbIsExtraSession(id) || dbTxConn(id) != conn:
		return nil, true
	}
	s.bindConsoleSession(file, key)
	s.touch()
	m.databaseRefreshLens(file, "", "")
	detail := "Its own transaction and temp tables; it opens on the next run."
	if key == "" {
		detail = "It shares the connection's transaction and temp tables."
	} else if s.sessions[key] != nil {
		detail = "Its own transaction and temp tables."
	}
	if old != key && s.txOpen(old) {
		detail += " The open transaction stays on " + s.dbSessionTitle(old) + "."
	} else if old != key && old == "" && s.txOpen(conn) {
		detail += " The open transaction stays on the main session."
	}
	return m.pushToastDetail(toast.Info, "This console runs on "+dbConsoleSessionName(key), detail), true
}

func dbConsoleSessionName(key string) string {
	if key == "" {
		return "the main session"
	}
	return fmt.Sprintf("session %d", dbExtraSessionNumber(key))
}

// databaseOpenExtraSession opens the extra session key of a connection
// whose main session is open (its password and tunnel are known).
func (m *Model) databaseOpenExtraSession(key string) tea.Cmd {
	s := m.databases
	conn := dbTxConn(key)
	c, ok := s.connection(conn)
	if !ok {
		m.dbConsole().waiting = nil
		return m.pushToastDetail(toast.Warn, "Connection not found", sanitizeDBText(conn))
	}
	dial, err := s.dbDialFor(c)
	if err != nil {
		m.dbConsole().waiting = nil
		return m.pushToastDetail(toast.Errr, "Could not open the session", dbErrorText(err))
	}
	c = dbResolveConnPaths(c, s.root, dbSSHHome())
	c.Dial = dial
	return s.connectionCommand(conn, dbDatabaseConnectCommand(key, c, s.passwords[conn], s.generation))
}

// onDBExtraSessionConnect stores an opened extra session (no tree node) and
// starts the run that waited for it.
func (m *Model) onDBExtraSessionConnect(msg dbConnectMsg) tea.Cmd {
	s := m.databases
	c := s.console
	waiting := c != nil && c.waiting != nil && c.waiting.sessKey == msg.id
	if msg.err != nil {
		if waiting {
			c.waiting = nil
		}
		return m.pushToastDetail(toast.Errr, "Could not open "+s.dbSessionTitle(msg.id), dbErrorText(msg.err))
	}
	if old := s.sessions[msg.id]; old != nil && old != msg.session {
		_ = old.Close()
	}
	s.sessions[msg.id] = msg.session
	s.touch()
	if waiting {
		return m.databaseStartRun(c.waiting)
	}
	return nil
}

// databaseTempTableHint says once per session that a temp table made in
// auto-commit lives in one server session: the next statement may run on
// another pooled session and miss it, and other consoles never see it.
func (m *Model) databaseTempTableHint(msg dbRunMsg) tea.Cmd {
	d := m.databases
	r := msg.run
	if d == nil || r == nil || msg.key == "" || msg.result.Err != nil || r.tv != nil || r.redis != nil || r.plan != nil {
		return nil
	}
	c, ok := d.connection(r.conn)
	if !ok || !dbExtraSessionDriverOK(c.Driver) || d.txOpen(msg.key) {
		return nil
	}
	name, ok := dbclient.TempTableName(r.sql, c.Driver)
	if !ok || d.tempHinted[msg.key] {
		return nil
	}
	if d.tempHinted == nil {
		d.tempHinted = map[string]bool{}
	}
	d.tempHinted[msg.key] = true
	return m.pushToastDetail(toast.Info, "Temp table "+sanitizeDBText(name)+" lives in one session",
		"In auto-commit the next statement may run on another session and miss it. Switch to manual commit (Databases: Transaction Mode…) to keep one session; other consoles never see it.")
}
