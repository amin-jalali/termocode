package app

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// More result page conveniences (DBX-26): column types in the header (T,
// db_column_types), row numbers (I, db_row_numbers), a cell menu (m),
// column → DDL and the first rows of a result inline under the query in
// the .sql buffer (db_inline_result).

const pickerKindDBCellMenu pickerKindEnum = 4318

// dbInlineRows is the number of rows drawn under the query.
const dbInlineRows = 5

func (s *dbConsoleState) showRowNums() bool {
	if s.rowNums != nil {
		return *s.rowNums
	}
	v := loadDBConsoleSettings().RowNumbers
	return v != nil && *v
}

func (s *dbConsoleState) showColTypes() bool {
	if s.colTypes != nil {
		return *s.colTypes
	}
	v := loadDBConsoleSettings().ColumnTypes
	return v != nil && *v
}

// databaseToggleRowNums is I: row numbers on or off for this session.
func (m *Model) databaseToggleRowNums() tea.Cmd {
	s := m.dbConsole()
	v := !s.showRowNums()
	s.rowNums = &v
	if s.shown == nil {
		return nil
	}
	s.keepCursor = true
	return m.databaseShowPage()
}

// databaseToggleColTypes is T: column types in the header on or off.
func (m *Model) databaseToggleColTypes() tea.Cmd {
	s := m.dbConsole()
	v := !s.showColTypes()
	s.colTypes = &v
	if s.shown == nil {
		return nil
	}
	s.keepCursor = true
	return m.databaseShowPage()
}

// dbCellMenuItems is the cell menu: the ID is the page key it runs.
func dbCellMenuItems(table bool) []picker.Item {
	items := []picker.Item{
		{ID: "v", Title: "Open the cell", Hint: "v"},
		{ID: "y", Title: "Copy the cell", Hint: "y"},
		{ID: "Y", Title: "Copy the column", Hint: "Y"},
		{ID: "ddl", Title: "Copy column definition (DDL)"},
		{ID: "ddl-all", Title: "Copy all columns as CREATE TABLE"},
		{ID: "=", Title: "Count, sum, avg, min, max of the column", Hint: "="},
		{ID: "e", Title: "Edit the cell", Hint: "e"},
	}
	if table {
		items = append(items, picker.Item{ID: "*", Title: "Show only rows with this value", Hint: "*"})
	}
	return append(items,
		picker.Item{ID: "H", Title: "Hide the column", Hint: "H"},
		picker.Item{ID: "o", Title: "Sort by a column…", Hint: "o"},
	)
}

// databaseCellMenu is m: the actions for the cell under the cursor.
func (m *Model) databaseCellMenu(row, col int) tea.Cmd {
	s, cmd, ok := m.databaseResultReady()
	if !ok {
		return cmd
	}
	if s.view != dbViewGrid {
		return m.pushToastDetail(toast.Info, "The cell menu is for the Grid", "Tab switches back to the Grid.")
	}
	s.menuRow, s.menuCol = row, col
	m.picker = picker.NewItems("Cell", dbCellMenuItems(s.shown.tv != nil))
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBCellMenu
	m.pickerOpen = true
	return nil
}

func (m *Model) databaseCellMenuPick(id string) tea.Cmd {
	s := m.dbConsole()
	row, col := s.menuRow, s.menuCol
	switch id {
	case "ddl", "ddl-all":
		return m.databaseCopyColumnDDL(id == "ddl-all", row, col)
	case "=":
		return m.databaseAggregate(0, col, -1)
	}
	return m.databasePageKey(id, row, col)
}

// databaseCopyColumnDDL copies the column under the cursor (or every
// column) as SQL: `"name" type`, or a CREATE TABLE of the result.
func (m *Model) databaseCopyColumnDDL(all bool, row, col int) tea.Cmd {
	s, cmd, ok := m.databaseResultReady()
	if !ok {
		return cmd
	}
	cols := s.shown.result.Columns
	var d dbclient.Dialect
	if m.databases != nil {
		if c, ok := m.databases.connection(s.shown.conn); ok {
			d = dbclient.DialectFor(c.Driver)
		}
	}
	if all {
		table := ""
		if tv := s.shown.tv; tv != nil {
			table = tv.qualified()
		}
		return m.copyWithToast(dbRedact(dbResultDDL(d, table, cols)), "Copied CREATE TABLE of "+dbPlural(len(cols), "column"))
	}
	i := s.gridColumn(col)
	if s.trans {
		i = row // the cursor line is the column
	}
	if i < 0 || i >= len(cols) {
		return m.pushToastDetail(toast.Info, "Put the cursor on a column", "")
	}
	return m.copyWithToast(dbRedact(dbColumnDDL(d, cols[i])), "Copied the definition of "+dbOneLine(cols[i].Name))
}

// dbColumnDDL is one column definition; a column the driver gave no type
// for is text.
func dbColumnDDL(d dbclient.Dialect, c dbclient.Column) string {
	typ := strings.TrimSpace(dbOneLine(c.Type))
	if typ == "" {
		typ = "text"
	}
	name := dbOneLine(c.Name)
	if d != nil {
		name = d.QuoteIdent(c.Name)
	}
	return name + " " + typ
}

// dbResultDDL is a CREATE TABLE with the result's columns (table: the
// quoted name, "" for "result").
func dbResultDDL(d dbclient.Dialect, table string, cols []dbclient.Column) string {
	if table == "" {
		table = "result"
		if d != nil {
			table = d.QuoteIdent(table)
		}
	}
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = "  " + dbColumnDDL(d, c)
	}
	return "CREATE TABLE " + table + " (\n" + strings.Join(parts, ",\n") + "\n);"
}

// dbInlineLines is the short grid drawn under the query: the header, a
// rule, up to dbInlineRows rows and how many more there are, cut to width.
func dbInlineLines(r dbclient.Result, width int) []string {
	return dbGridLines(r, width, dbInlineRows)
}

// dbGridLines is dbInlineLines for up to maxRows rows.
func dbGridLines(r dbclient.Result, width, maxRows int) []string {
	if r.Err != nil || len(r.Columns) == 0 {
		return nil
	}
	rows := r.Rows[:min(len(r.Rows), maxRows)]
	safe := func(s string) string { return sanitizeDBText(dbRedact(dbOneLine(s))) }
	widths := make([]int, len(r.Columns))
	for i, c := range r.Columns {
		widths[i] = ansi.StringWidth(ansi.Truncate(safe(c.Name), 24, "…"))
		for _, row := range rows {
			if i < len(row) {
				widths[i] = max(widths[i], ansi.StringWidth(ansi.Truncate(safe(row[i].String()), 24, "…")))
			}
		}
	}
	line := func(cells func(i int) string) string {
		parts := make([]string, len(widths))
		for i, w := range widths {
			t := ansi.Truncate(cells(i), 24, "…")
			parts[i] = t + strings.Repeat(" ", max(0, w-ansi.StringWidth(t)))
		}
		return ansi.Truncate("  "+strings.TrimRight(strings.Join(parts, " │ "), " "), max(width, 20), "…")
	}
	out := []string{line(func(i int) string { return safe(r.Columns[i].Name) })}
	rule := make([]string, len(widths))
	for i, w := range widths {
		rule[i] = strings.Repeat("─", w)
	}
	out = append(out, ansi.Truncate("  "+strings.Join(rule, "─┼─"), max(width, 20), "…"))
	for _, row := range rows {
		out = append(out, line(func(i int) string {
			if i < len(row) {
				return safe(row[i].String())
			}
			return ""
		}))
	}
	if n := len(r.Rows) - len(rows); n > 0 {
		more := fmt.Sprintf("  … %d more row", n)
		if n != 1 {
			more += "s"
		}
		if r.Truncated {
			more += " (cut at db_max_rows)"
		}
		out = append(out, more)
	}
	return out
}

// databaseSetInline keeps the inline lines of a finished console run
// (db_inline_result); the lens refresh draws them under the statement.
func (m *Model) databaseSetInline(r *dbRun) {
	if r == nil || r.file == "" || r.sql == "" || r.tv != nil || r.plan != nil || r.redis != nil {
		return
	}
	s := m.dbConsole()
	key := r.file + "\x00" + r.sql
	v := loadDBConsoleSettings().InlineResult
	lines := dbInlineLines(r.result, m.w-8)
	if v == nil || !*v || lines == nil {
		delete(s.inline, key)
		return
	}
	if s.inline == nil {
		s.inline = map[string][]string{}
	}
	s.inlineSeen = true
	s.inline[key] = lines
}

// databaseClearInline is Databases: Clear Inline Results.
func (m *Model) databaseClearInline() tea.Cmd {
	s := m.dbConsole()
	files := map[string]bool{}
	for k := range s.inline {
		f, _, _ := strings.Cut(k, "\x00")
		files[f] = true
	}
	s.inline = nil
	for f := range files {
		m.databaseRefreshLens(f, "", "")
	}
	return nil
}

// dbInlineMarks is the inline results of one buffer for Lua: the line
// under which each one goes (the statement's last line) and its lines.
func (s *dbConsoleState) dbInlineMarks(file, text string, spans []dbStatementSpan) []map[string]any {
	out := []map[string]any{}
	if len(s.inline) == 0 {
		return out
	}
	for _, sp := range spans {
		lines, ok := s.inline[file+"\x00"+sp.sql]
		if !ok {
			continue
		}
		end := min(max(sp.end, 0), len(text))
		last := strings.Count(strings.TrimRight(text[:end], " \t\n;"), "\n") + 1
		out = append(out, map[string]any{"line": max(last, sp.line), "lines": lines})
	}
	return out
}

// databaseCellMenuAtCursor is Databases: Result Cell Menu…: the cell
// under the cursor of the result page.
func (m *Model) databaseCellMenuAtCursor() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	var at []int
	_ = m.nvim.ExecLuaResult(`local D=_G._termocode_db;local b=vim.g.termocode_db_buf;local api=vim.api
if not D or not b or api.nvim_get_current_buf()~=b then return {-2,-1} end
local pos=api.nvim_win_get_cursor(0);local row=-1
if D.first>0 and pos[1]>=D.first then row=pos[1]-D.first end
local x=vim.fn.virtcol('.')-1;local col=-1
for i,bd in ipairs(D.bounds) do if x>=bd-1 then col=i-1 end end
return {row,col}`, &at)
	if len(at) != 2 || at[0] == -2 {
		return m.pushToastDetail(toast.Info, "Open the result page first", "Put the cursor on a cell of the SQL Result page, or press m there.")
	}
	return m.databaseCellMenu(at[0], at[1])
}
