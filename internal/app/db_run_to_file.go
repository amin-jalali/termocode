package app

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Run Query to File (DBX-36): Databases: Run Query to File… takes the
// SELECT under the cursor of a SQL console, asks for a format and a file,
// then shows a card (Cancel first). Run streams every row into the file in
// a read-only session, with no row cap and no rows kept in memory; the grid
// is not changed. It reuses the Export all path (db_export.go), so the
// file replaces the chosen path only when it is complete, and the CSV
// settings and db_export_mask apply.

const confirmKindDBRunToFile confirmKindEnum = 6357

// dbRunToFileFormats is the export picker without SQL UPDATEs: a query has
// no table key for the WHERE.
func dbRunToFileFormats() []dbclient.ExportFormatInfo {
	var out []dbclient.ExportFormatInfo
	for _, f := range dbExportFormatList() {
		if f.Format != dbclient.FormatSQLUpdate {
			out = append(out, f)
		}
	}
	return out
}

// dbRunToFileSQL is the statement Run to File runs: only one plain read,
// never a write. reason says why not.
func dbRunToFileSQL(driver, text string) (sql, reason string) {
	text = strings.TrimSpace(dbStripComments(dbStripConnHeader(text)))
	if dbclient.DialectFor(driver) == nil {
		return "", "This connection does not use SQL."
	}
	if text == "" {
		return "", "Put the cursor on a SELECT."
	}
	kind, stmts := dbclient.Classify(text, driver)
	if kind != dbclient.KindRead || len(stmts) != 1 {
		return "", "Only one plain SELECT can run into a file."
	}
	return text, ""
}

// databaseRunToFileStart is the palette command: the statement under the
// cursor of the SQL console.
func (m *Model) databaseRunToFileStart() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	file, lines, ok := m.httpBufText(0)
	if !ok || !strings.EqualFold(lastExt(file), ".sql") {
		return m.pushToastDetail(toast.Info, "Open a SQL console", "Put the cursor on the SELECT to run into a file.")
	}
	m.ensureDatabasesLoaded()
	conn := m.dbConsole().consoleConn(file, lines)
	c, ok := m.databases.connection(conn)
	if !ok {
		return m.pushToastDetail(toast.Info, "No connection for this file", "Run a statement first to pick its connection.")
	}
	return m.databaseRunToFileBegin(conn, m.databaseStatementAtCursor(lines, c.Driver))
}

// databaseRunToFileBegin checks the statement and the session, then opens
// the format picker.
func (m *Model) databaseRunToFileBegin(conn, text string) tea.Cmd {
	s := m.dbConsole()
	c, ok := m.databases.connection(conn)
	if !ok {
		return m.pushToastDetail(toast.Info, "No connection for this file", "Run a statement first to pick its connection.")
	}
	if s.job != nil || s.running != nil || s.submitting != nil {
		return m.pushToastDetail(toast.Warn, "A query or transfer is running", "Esc cancels it.")
	}
	sql, why := dbRunToFileSQL(c.Driver, text)
	if why != "" {
		return m.pushToastDetail(toast.Info, "Cannot run into a file", why)
	}
	if m.databases.sessions[conn] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection, or run a statement.")
	}
	o := dbclient.ExportOptions{Driver: c.Driver, Table: "result"}
	if sel, w := dbclient.SimpleSelect(sql, c.Driver); w == "" && sel.Table != "" {
		o.Schema, o.Table = sel.Schema, sel.Table
	}
	s.exp = &dbExport{toFile: true, all: true, sql: sql, driver: c.Driver, conn: conn, sessKey: conn, opts: o}
	var items []picker.Item
	for _, f := range dbRunToFileFormats() {
		items = append(items, picker.Item{ID: string(f.Format), Title: f.Title, Hint: f.Ext})
	}
	m.picker = picker.NewItems("Run query to file as…", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBExportFormat
	m.pickerOpen = true
	return nil
}

// databaseRunToFileCard is the one card before the query runs (Cancel
// first); an existing file is named and replaced only on the red button.
func (m *Model) databaseRunToFileCard() tea.Cmd {
	e := m.dbConsole().exp
	where := sanitizeDBText(e.display)
	msg := "Runs the query again in a read-only session and writes every row to " + where + " as it arrives. The grid is not changed.\n\n" + sanitizeDBText(dbOneLine(e.sql))
	buttons := []confirm.Button{{ID: "cancel", Title: "Cancel"}}
	if st, err := os.Stat(e.path); err == nil && !st.IsDir() {
		msg += "\n\n" + where + " already exists. It is replaced only when the file is complete."
		buttons = append(buttons, confirm.Button{ID: "replace", Title: "Run and replace", Style: confirm.StyleDestructive})
	} else {
		buttons = append(buttons, confirm.Button{ID: "run", Title: "Run", Style: confirm.StylePrimary})
	}
	m.confirm = confirm.New("Run query to file", msg, buttons)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBRunToFile
	return nil
}

func (m *Model) databaseRunToFileConfirm(id string) tea.Cmd {
	s := m.dbConsole()
	e := s.exp
	if e == nil {
		return nil
	}
	switch id {
	case "run":
		return m.databaseExportGo()
	case "replace":
		e.overwrite = true
		return m.databaseExportGo()
	}
	s.exp = nil
	return nil
}
