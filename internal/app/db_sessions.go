package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Sessions, locks and server info (DBX-17). The views are one-line reads
// written into the connection's console and run there, so the result is
// a normal grid (sort, filter, export). Cancel / End asks for the session
// id, then writes the stop statement and sends it through the run card
// (Cancel first): it is never a read.

const promptKindDBStopSession promptKindEnum = 1493

// databaseSessionConn is the SQL connection the session commands act on,
// or a hint toast.
func (m *Model) databaseSessionConn() (dbclient.Conn, tea.Cmd, bool) {
	id := m.databaseContextConn()
	c, ok := m.databases.connection(id)
	if id == "" || !ok {
		return dbclient.Conn{}, m.pushToastDetail(toast.Info, "Select a connection", "Pick a connection row in the Databases view, or open its console."), false
	}
	return c, nil, true
}

// databaseShowActivity runs the sessions (info=false) or server info
// query of the current connection in its console.
func (m *Model) databaseShowActivity(info bool) tea.Cmd {
	c, cmd, ok := m.databaseSessionConn()
	if !ok {
		return cmd
	}
	if c.Driver == "redis" { // DBX-51: INFO grid, CLIENT LIST
		if info {
			return m.databaseRunInConsole(c.ID, "INFO")
		}
		return m.databaseRunInConsole(c.ID, "CLIENT LIST")
	}
	sql, ok := dbclient.SessionsSQL(c.Driver)
	if info {
		sql, ok = dbclient.ServerInfoSQL(c.Driver)
	}
	if !ok {
		return m.pushToastDetail(toast.Info, "Not available for "+sanitizeDBText(c.Driver), "Sessions and locks need a PostgreSQL or MySQL server.")
	}
	if err := dbReadOnlyCheck(c.Driver, sql); err != nil {
		return m.dbConsoleError(err)
	}
	return m.databaseRunInConsole(c.ID, sql)
}

// databaseStopSessionPrompt asks for the id of the session to cancel or
// end.
func (m *Model) databaseStopSessionPrompt(end bool) tea.Cmd {
	c, cmd, ok := m.databaseSessionConn()
	if !ok {
		return cmd
	}
	if _, err := dbclient.StopSessionSQL(c.Driver, 1, end); err != nil {
		return m.pushToastDetail(toast.Info, "Not available for "+sanitizeDBText(c.Driver), "Sessions can be stopped on PostgreSQL and MySQL.")
	}
	s := m.dbConsole()
	s.stopConn, s.stopEnd = c.ID, end
	title := "Cancel the running query of a session"
	if end {
		title = "End a session (its open transaction rolls back)"
	}
	return m.openHTTPPrompt(promptKindDBStopSession, title, "Session id (pid / id column of Show Sessions):", "")
}

// databaseStopSessionTyped checks the typed id and sends the stop
// statement through the run card.
func (m *Model) databaseStopSessionTyped(value string) tea.Cmd {
	s := m.dbConsole()
	c, ok := m.databases.connection(s.stopConn)
	if !ok {
		return m.pushToastDetail(toast.Warn, "Connection not found", sanitizeDBText(s.stopConn))
	}
	id, err := dbclient.ParseSessionID(value)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Session id: "+err.Error(), "")
	}
	sql, err := dbclient.StopSessionSQL(c.Driver, id, s.stopEnd)
	if err != nil {
		return m.pushToastDetail(toast.Warn, err.Error(), "")
	}
	return m.databaseRunInConsole(c.ID, sql)
}

// databaseRunInConsole writes the one-line statement stmt at the end of
// the connection's console and sends it from there (a read runs, any other
// statement shows the run card first).
func (m *Model) databaseRunInConsole(conn, stmt string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	s := m.dbConsole()
	if s.running != nil {
		return m.pushToastDetail(toast.Warn, "A query is running", "Esc cancels it.")
	}
	path, err := m.databaseConsoleFile(conn)
	if err != nil {
		return m.dbConsoleError(err)
	}
	m.ensureDBLua()
	m.ensureEditorWindowCurrent()
	var line int
	if err := m.nvim.ExecLuaResult(`local f,stmt=...
vim.cmd('edit '..vim.fn.fnameescape(f)); vim.bo.filetype='sql'
local all=vim.api.nvim_buf_get_lines(0,0,-1,false)
local n=#all
while n>0 and all[n]=='' do n=n-1 end
if n>0 and all[n]==stmt then
 vim.api.nvim_win_set_cursor(0,{n,0})
 return n
end
local at
if n>0 then vim.api.nvim_buf_set_lines(0,n,-1,false,{'',stmt,''}); at=n+2
else vim.api.nvim_buf_set_lines(0,0,-1,false,{stmt,''}); at=1 end
pcall(vim.cmd,'silent write')
vim.api.nvim_win_set_cursor(0,{at,0})
return at`, &line, path, stmt); err != nil {
		return m.dbConsoleError(fmt.Errorf("console: %w", err))
	}
	m.focus = FocusEditor
	r := &dbRun{conn: conn, file: path, sql: stmt, line: max(line, 1), by: "user", statements: []string{stmt}}
	return tea.Batch(m.fetchStateCmd(), m.databaseReviewRun(r))
}
