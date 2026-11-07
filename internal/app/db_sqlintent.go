package app

import (
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// SQL intentions (DBX-42): Databases: SQL Intentions… lists the rewrites
// that fit the statement at the cursor (introduce alias, qualify names,
// expand *, wrap a table in a subquery, extract or inline a CTE, turn a
// literal into a parameter, DISTINCT ↔ GROUP BY). Enter rewrites the
// statement's lines in ONE undo step (u undoes it). Expand * and Qualify
// use the columns completion already knows; nothing is asked of the
// server.

const pickerKindDBSQLIntent pickerKindEnum = 5391

// dbSQLCursorLua returns the cursor line (1-based) and byte column.
const dbSQLCursorLua = `
local c=vim.api.nvim_win_get_cursor(0)
return vim.json.encode({line=c[1],col=c[2]})
`

// dbIntentTarget is the statement the intention picker was opened on.
type dbIntentTarget struct {
	start, end int      // its lines [start, end), 0-based
	orig       []string // those lines as they were
	stmt       string
	cur        int // cursor byte offset in stmt
	dialect    string
	cols       map[string][]string
}

// dbIntentStatement finds the whole lines of the statement at the cursor
// (line 1-based, col a byte offset) and the cursor offset inside them.
func dbIntentStatement(lines []string, line, col int, dialect string) (start, end, cur int, ok bool) {
	if len(lines) == 0 {
		return 0, 0, 0, false
	}
	line = min(max(line, 1), len(lines))
	text := strings.Join(lines, "\n")
	off := 0
	for i := 0; i < line-1; i++ {
		off += len(lines[i]) + 1
	}
	off += min(max(col, 0), len(lines[line-1]))
	spans := dbStatementSpans(text, dialect)
	if len(spans) == 0 {
		return 0, 0, 0, false
	}
	chosen := spans[len(spans)-1]
	for _, sp := range spans {
		if off <= sp.end {
			chosen = sp
			break
		}
	}
	start = strings.Count(text[:chosen.start], "\n")
	end = strings.Count(text[:chosen.end], "\n") + 1
	lineOff := 0
	for i := 0; i < start; i++ {
		lineOff += len(lines[i]) + 1
	}
	return start, end, max(off-lineOff, 0), true
}

// dbSQLIntentItems are the picker rows: ID is the intention id.
func dbSQLIntentItems(stmt, dialect string, cur int, cols map[string][]string) []picker.Item {
	var items []picker.Item
	for _, in := range dbclient.SQLIntentions(stmt, dialect, cur, cols) {
		items = append(items, picker.Item{ID: in.ID, Title: sanitizeDBText(in.Title)})
	}
	return items
}

// knownColumns are the column names completion has for a connection:
// the background schema first, then the tables loaded one by one.
func (s *dbState) knownColumns(conn string) map[string][]string {
	out := map[string][]string{}
	if f := s.full[conn]; f != nil {
		for table, cols := range f.Cols {
			names := make([]string, 0, len(cols))
			for _, c := range cols {
				names = append(names, c.Name)
			}
			out[table] = names
		}
	}
	for table, names := range s.cols[conn] {
		if _, ok := out[table]; !ok {
			out[table] = names
		}
	}
	return out
}

func (m *Model) databaseSQLIntentions() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.ensureDatabasesLoaded()
	file, src, cmd, ok := m.databaseSQLBuffer()
	if !ok {
		return cmd
	}
	lines := strings.Split(src, "\n")
	var pos struct {
		Line int `json:"line"`
		Col  int `json:"col"`
	}
	var raw string
	_ = m.nvim.ExecLuaResult(dbSQLCursorLua, &raw)
	_ = json.Unmarshal([]byte(raw), &pos)
	connID := m.dbConsole().consoleConn(file, lines)
	driver := ""
	if c, ok := m.databases.connection(connID); ok {
		driver = c.Driver
	}
	dialect, _ := dbSQLFileDialect(src, driver)
	start, end, cur, ok := dbIntentStatement(lines, pos.Line, pos.Col, dialect)
	if !ok {
		return m.pushToastDetail(toast.Info, "No SQL at the cursor", "Put the cursor on a statement.")
	}
	t := &dbIntentTarget{start: start, end: end, orig: lines[start:end], cur: cur, dialect: dialect, cols: m.databases.knownColumns(connID)}
	t.stmt = strings.Join(t.orig, "\n")
	items := dbSQLIntentItems(t.stmt, dialect, cur, t.cols)
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No SQL intentions here",
			"Put the cursor on a table, a *, a literal, a subquery or a CTE. Expand * and Qualify need the columns: connect the file first.")
	}
	m.dbConsole().intent = t
	m.picker = picker.NewItems("SQL intentions", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSQLIntent
	m.pickerOpen = true
	return nil
}

// databaseSQLIntentPicked rewrites the statement with the chosen
// intention, if its lines did not change meanwhile.
func (m *Model) databaseSQLIntentPicked(id string) tea.Cmd {
	s := m.dbConsole()
	t := s.intent
	s.intent = nil
	if t == nil || m.nvim == nil {
		return nil
	}
	out, err := dbclient.ApplySQLIntention(t.stmt, t.dialect, id, t.cur, t.cols)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "SQL not changed", sanitizeDBText(err.Error()))
	}
	var res string
	if err := m.nvim.ExecLuaResult(dbFormatApplyLua, &res, t.start, t.end, strings.Split(out, "\n"), t.orig); err != nil || res != "ok" {
		return m.pushToastDetail(toast.Warn, "SQL not changed", "The buffer changed; try again.")
	}
	m.focus = FocusEditor
	return m.pushToastDetail(toast.Info, "SQL rewritten", "u undoes it.")
}
