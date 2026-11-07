package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Query log and console versions (DBX-48).
//
// Databases: Show Query Log opens every statement the SQL sessions sent
// this run (the newest 1,000), the internal ones too (catalog reads,
// read-only sessions), in a scratch SQL buffer. The log lives in memory
// only: statements can hold literal values.
//
// Databases: Show Console Version History lists the local-history
// versions of a console (the active one, or the console of the connection
// under the tree cursor) to compare or restore. Every run from a console
// saves it first, so each run leaves a version.

const dbQueryLogMax = 1000

// loggedSession puts a new SQL session's statements in the query log.
// Redis sessions keep their own type (their commands are not SQL).
func (s *dbState) loggedSession(sess dbclient.Session, id string) dbclient.Session {
	if sess == nil {
		return nil
	}
	if _, redis := sess.(dbRedis); redis {
		return sess
	}
	if s.queryLog == nil {
		s.queryLog = dbclient.NewQueryLog(dbQueryLogMax)
	}
	return dbclient.LogSession(sess, s.queryLog, id)
}

// databaseQueryLog opens the query log in a scratch buffer.
func (m *Model) databaseQueryLog() tea.Cmd {
	var entries []dbclient.QueryLogEntry
	if m.databases != nil {
		entries = m.databases.queryLog.Entries()
	}
	if len(entries) == 0 {
		return m.pushToastDetail(toast.Info, "No statements sent yet", "Connect and run a statement; every statement sent is logged here for this run.")
	}
	for i := range entries {
		entries[i].SQL = sanitizeDBText(entries[i].SQL)
		entries[i].Conn = sanitizeDBText(entries[i].Conn)
		entries[i].Err = dbclient.Redact(sanitizeDBText(entries[i].Err))
	}
	text := dbclient.FormatQueryLog(entries)
	if m.nvim == nil {
		return m.copyWithToast(text, "No editor: query log copied instead")
	}
	m.ensureDBLua()
	m.ensureEditorWindowCurrent()
	title := fmt.Sprintf("Query Log (%d statements)", len(entries))
	_ = m.nvim.ExecLuaArgs(`_G._termocode_db_ddl(...)`, title, strings.Split(strings.TrimRight(text, "\n"), "\n"))
	m.focus = FocusEditor
	return m.fetchStateCmd()
}

// dbIsConsolePath reports a console file (db/consoles/… in the config).
func dbIsConsolePath(path string) bool {
	dir := dbConfigPath("consoles")
	return path != "" && dir != "" && strings.HasPrefix(filepath.Clean(path), dir+string(filepath.Separator))
}

// databaseConsoleSave writes a changed console buffer to its file; the
// save hook of the local history keeps the version.
func (m *Model) databaseConsoleSave(path string) {
	if m.nvim == nil || !dbIsConsolePath(path) {
		return
	}
	_ = m.nvim.ExecLuaArgs(`local p=...; local b=vim.fn.bufnr(p)
if b>0 and vim.api.nvim_buf_is_loaded(b) and vim.bo[b].modified then
  vim.api.nvim_buf_call(b, function() vim.cmd('silent! update') end)
end`, path)
}

// databaseConsoleVersion saves the console before a run from it.
func (m *Model) databaseConsoleVersion(path string) {
	m.databaseConsoleSave(path)
}

// databaseConsoleHistoryPath is the console to show: the active buffer
// when it is a console, else the console of the connection under the tree
// cursor.
func (m *Model) databaseConsoleHistoryPath() string {
	if p, ok := m.localHistActive(); ok && dbIsConsolePath(p) {
		return p
	}
	if m.databases == nil {
		return ""
	}
	r, ok := m.databaseCurrentRow()
	if !ok || r.conn == "" {
		return ""
	}
	p := m.databaseConsolePath(r.conn)
	if c, ok := m.databases.connection(r.conn); ok && c.Driver == "redis" && p != "" {
		p = strings.TrimSuffix(p, ".sql") + ".redis"
	}
	return p
}

// databaseConsoleHistory lists the versions of a console.
func (m *Model) databaseConsoleHistory() tea.Cmd {
	path := m.databaseConsoleHistoryPath()
	if path == "" {
		return tea.Batch(m.revealDatabasesView(false), m.pushToastDetail(toast.Info, "Select a connection or open its console first",
			"Put the cursor on a connection in the Databases view, or open its console."))
	}
	if _, err := os.Stat(path); err != nil {
		return m.pushToastDetail(toast.Info, "This connection has no console yet", "Open it with Databases: New Console and run something.")
	}
	m.databaseConsoleSave(path)
	if st := localHistStore(); st != nil && localHistOn() {
		if b, err := os.ReadFile(path); err == nil {
			if _, err := st.Save(path, b, time.Now()); err != nil {
				recordError("[local history] " + err.Error())
			}
		}
	}
	return m.openLocalHistoryOf(path)
}
