package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Console work (DBX-6): saved queries (bookmarks), the history of one
// connection, several consoles per connection. Live templates are in the
// omnifunc (db_console_lua.go), fed by schemaPayload.

const (
	promptKindDBSaveQuery      promptKindEnum = 4933
	pickerKindDBSaved          pickerKindEnum = 4934
	pickerKindDBSavedDelete    pickerKindEnum = 4935
	dbConsoleMaxPerConn                       = 9
	dbSavedPickerDeleteID                     = "\x00delete"
	dbSavedQueryDefaultNameMax                = 40
)

// dbSaveDraft is the query waiting for its name.
type dbSaveDraft struct {
	conn, sql string
}

func (s *dbConsoleState) savedStore() *dbclient.SavedQueries {
	if s.saved == nil {
		s.saved = &dbclient.SavedQueries{Path: dbConfigPath("saved-queries.json")}
	}
	return s.saved
}

// dbStripConnHeader drops the "-- @conn" lines a console starts with.
func dbStripConnHeader(sql string) string {
	var keep []string
	for _, l := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "-- @conn") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

// databaseSaveQueryStart asks for the name of the statement at the
// cursor.
func (m *Model) databaseSaveQueryStart() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	file, lines, ok := m.httpBufText(0)
	if !ok || !strings.EqualFold(lastExt(file), ".sql") {
		return m.pushToastDetail(toast.Info, "Open a SQL console", "Put the cursor on the statement to save.")
	}
	m.ensureDatabasesLoaded()
	conn := m.dbConsole().consoleConn(file, lines)
	c, ok := m.databases.connection(conn)
	if !ok {
		return m.pushToastDetail(toast.Info, "No connection for this file", "Run a statement first to pick its connection.")
	}
	sql := dbStripConnHeader(m.databaseStatementAtCursor(lines, c.Driver))
	if sql == "" {
		return m.pushToastDetail(toast.Info, "No statement here", "Put the cursor on the statement to save.")
	}
	m.dbConsole().saveDraft = &dbSaveDraft{conn: conn, sql: sql}
	name := dbOneLine(sql)
	if r := []rune(name); len(r) > dbSavedQueryDefaultNameMax {
		name = strings.TrimSpace(string(r[:dbSavedQueryDefaultNameMax]))
	}
	return m.openHTTPPrompt(promptKindDBSaveQuery, "Save Query · "+sanitizeDBText(conn), "Name:", name)
}

// databaseSaveQueryNamed stores the draft under name.
func (m *Model) databaseSaveQueryNamed(name string) tea.Cmd {
	s := m.dbConsole()
	d := s.saveDraft
	s.saveDraft = nil
	if d == nil {
		return nil
	}
	q, err := s.savedStore().Save(dbclient.SavedQuery{Name: name, Conn: d.conn, SQL: d.sql})
	if err != nil {
		return m.dbConsoleError(err)
	}
	return m.pushToastDetail(toast.Info, "Query saved: "+sanitizeDBText(q.Name), "Databases: Saved Queries… puts it back in a console.")
}

// databaseSavedQueries lists the saved queries; Enter puts one in the SQL
// buffer (or the connection's console).
func (m *Model) databaseSavedQueries() tea.Cmd {
	s := m.dbConsole()
	list, err := s.savedStore().List("")
	if err != nil {
		return m.dbConsoleError(err)
	}
	if len(list) == 0 {
		return m.pushToastDetail(toast.Info, "No saved queries yet", "Databases: Save Query… saves the statement at the cursor.")
	}
	s.savedList = list
	items := make([]picker.Item, 0, len(list)+1)
	for _, q := range list {
		items = append(items, picker.Item{ID: q.ID, Title: sanitizeDBText(q.Name), Hint: sanitizeDBText(q.Conn) + " · " + dbOneLine(q.SQL)})
	}
	items = append(items, picker.Item{ID: dbSavedPickerDeleteID, Title: "Delete a saved query…"})
	m.picker = picker.NewItems("Saved queries · Enter puts it in the console", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSaved
	m.pickerOpen = true
	return nil
}

func (m *Model) databaseSavedPicker(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindDBSaved, pickerKindDBSavedDelete:
	default:
		return nil, false
	}
	s := m.dbConsole()
	if m.pickerKind == pickerKindDBSavedDelete {
		if id == "" {
			return nil, true
		}
		if err := s.savedStore().Delete(id); err != nil {
			return m.dbConsoleError(err), true
		}
		return m.pushToast(toast.Info, "Saved query deleted"), true
	}
	if id == dbSavedPickerDeleteID {
		items := make([]picker.Item, 0, len(s.savedList))
		for _, q := range s.savedList {
			items = append(items, picker.Item{ID: q.ID, Title: sanitizeDBText(q.Name), Hint: sanitizeDBText(q.Conn)})
		}
		m.picker = picker.NewItems("Delete a saved query · Enter deletes it", items)
		m.picker.SetSize(m.w, m.h)
		m.pickerKind = pickerKindDBSavedDelete
		m.pickerOpen = true
		return nil, true
	}
	for _, q := range s.savedList {
		if q.ID == id {
			return m.databaseInsertSaved(q), true
		}
	}
	return nil, true
}

// databaseInsertSaved puts q below the cursor of the current SQL buffer
// when it belongs to q's connection, else at the end of the connection's
// console.
func (m *Model) databaseInsertSaved(q dbclient.SavedQuery) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	text := strings.Split(strings.TrimRight(q.SQL, "\n"), "\n")
	if file, lines, ok := m.httpBufText(0); ok && strings.EqualFold(lastExt(file), ".sql") && m.dbConsole().consoleConn(file, lines) == q.Conn {
		_ = m.nvim.ExecLuaArgs(`local lines=...;local r=vim.api.nvim_win_get_cursor(0)[1]
table.insert(lines,1,'');vim.api.nvim_buf_set_lines(0,r,r,false,lines);vim.api.nvim_win_set_cursor(0,{r+2,0})`, text)
		m.focus = FocusEditor
		return m.fetchStateCmd()
	}
	if _, ok := m.databases.connection(q.Conn); !ok {
		return m.pushToastDetail(toast.Warn, "Connection not found", sanitizeDBText(q.Conn)+" is not in the Databases view any more.")
	}
	cmd := m.databaseNewConsole(q.Conn)
	_ = m.nvim.ExecLuaArgs(`local lines=...;local n=vim.api.nvim_buf_line_count(0)
table.insert(lines,1,'');vim.api.nvim_buf_set_lines(0,n,n,false,lines);vim.api.nvim_win_set_cursor(0,{n+2,0})`, text)
	return cmd
}

// databaseHistoryOfConn opens the history of the connection in context.
func (m *Model) databaseHistoryOfConn() tea.Cmd {
	conn := m.databaseContextConn()
	if conn == "" {
		return m.pushToastDetail(toast.Info, "No connection here", "Open a console or select a connection first.")
	}
	return m.databaseHistoryFor(conn)
}

// databaseConsolePathN is the path of console n of a connection: 1 is
// the first console, 2… are the others (<hash>-2.sql).
func (m *Model) databaseConsolePathN(id string, n int) string {
	p := m.databaseConsolePath(id)
	if n <= 1 || p == "" {
		return p
	}
	return strings.TrimSuffix(p, ".sql") + "-" + strconv.Itoa(n) + ".sql"
}

// dbConsoleNumber splits "<hash>-2" into the hash and 2 (1 for "<hash>").
func dbConsoleNumber(base string) (string, int) {
	if i := strings.LastIndexByte(base, '-'); i > 0 {
		if n, err := strconv.Atoi(base[i+1:]); err == nil && n > 1 {
			return base[:i], n
		}
	}
	return base, 1
}

// databaseAnotherConsole opens the next free console of the connection in
// context (up to 9 per connection).
func (m *Model) databaseAnotherConsole() tea.Cmd {
	m.ensureDatabasesLoaded()
	conn := m.databaseContextConn()
	if conn == "" {
		if r, ok := m.databaseCurrentRow(); ok {
			conn = r.conn
		}
	}
	c, ok := m.databases.connection(conn)
	if !ok {
		return m.pushToastDetail(toast.Info, "No connection here", "Open a console or select a connection first.")
	}
	if c.Driver == "redis" {
		return m.databaseNewConsole(conn)
	}
	for n := 2; n <= dbConsoleMaxPerConn; n++ {
		p := m.databaseConsolePathN(conn, n)
		if p == "" {
			return nil
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return m.databaseNewConsoleAt(conn, p)
		}
	}
	return m.pushToastDetail(toast.Info, fmt.Sprintf("%d consoles already", dbConsoleMaxPerConn), "Reuse one of them: they are in "+filepath.Dir(m.databaseConsolePath(conn))+".")
}
