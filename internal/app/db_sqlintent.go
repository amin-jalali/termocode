package app

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

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

// dbSQLCursorLua returns the cursor line (1-based) and byte column, and
// the last visual selection when the cursor stands at one of its ends
// (marks '< and '>: 1-based lines, 0-based byte columns, '> on the
// first byte of the last character).
const dbSQLCursorLua = `
local c=vim.api.nvim_win_get_cursor(0)
local out={line=c[1],col=c[2]}
local m=vim.fn.mode()
local vs,ve
if m=='v' or m=='V' or m=='\22' then
 vs,ve=vim.fn.getpos('v'),vim.fn.getpos('.')
 if vs[2]>ve[2] or vs[2]==ve[2] and vs[3]>ve[3] then vs,ve=ve,vs end
 out.mode=m
else
 vs,ve=vim.fn.getpos("'<"),vim.fn.getpos("'>")
 out.mode=vim.fn.visualmode()
end
local live=m=='v' or m=='V' or m=='\22'
local at_end=(c[1]==vs[2] and c[2]==vs[3]-1) or (c[1]==ve[2] and c[2]==math.min(ve[3]-1,math.max(#vim.fn.getline(ve[2])-1,0)))
if out.mode=='V' and (c[1]==vs[2] or c[1]==ve[2]) then at_end=true end
if vs[2]>0 and ve[2]>0 and (live or at_end) then
 out.sel={vs[2],vs[3]-1,ve[2],ve[3]-1}
end
return vim.json.encode(out)
`

// dbIntentSelRange turns a selection (from/to: 1-based line, 0-based
// byte column; to is on the first byte of the last character) into byte
// offsets inside the statement lines [start, end). Linewise takes whole
// lines. ok=false when it is not inside the statement.
func dbIntentSelRange(lines []string, start, end int, from, to [2]int, linewise bool) (int, int, bool) {
	if from[0] < start+1 || to[0] > end || from[0] > to[0] || to[0] > len(lines) {
		return 0, 0, false
	}
	off := func(line, col int) int {
		o := 0
		for i := start; i < line-1; i++ {
			o += len(lines[i]) + 1
		}
		return o + col
	}
	fl, tl := lines[from[0]-1], lines[to[0]-1]
	fc, tc := min(max(from[1], 0), len(fl)), min(max(to[1], 0), len(tl))
	if linewise {
		fc, tc = 0, len(tl)
	} else if tc < len(tl) {
		_, n := utf8.DecodeRuneInString(tl[tc:])
		tc += n
	}
	s, e := off(from[0], fc), off(to[0], tc)
	return s, e, e > s
}

// dbIntentTarget is the statement the intention picker was opened on.
type dbIntentTarget struct {
	start, end int      // its lines [start, end), 0-based
	orig       []string // those lines as they were
	stmt       string
	cur        int // cursor byte offset in stmt
	dialect    string
	cols       map[string][]string
	opts       dbclient.SQLIntentOpts // the selection, if any
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
	return dbSQLIntentItemsWith(stmt, dialect, cur, cols, dbclient.SQLIntentOpts{})
}

func dbSQLIntentItemsWith(stmt, dialect string, cur int, cols map[string][]string, o dbclient.SQLIntentOpts) []picker.Item {
	var items []picker.Item
	for _, in := range dbclient.SQLIntentionsWith(stmt, dialect, cur, cols, o) {
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

// knownColumnTypes are the column types the background schema has:
// lower-case table → lower-case column → type (columns without a type
// are left out).
func (s *dbState) knownColumnTypes(conn string) map[string]map[string]string {
	out := map[string]map[string]string{}
	if f := s.full[conn]; f != nil {
		for table, cols := range f.Cols {
			for _, c := range cols {
				if c.Type == "" {
					continue
				}
				t := strings.ToLower(table)
				if out[t] == nil {
					out[t] = map[string]string{}
				}
				out[t][strings.ToLower(c.Name)] = c.Type
			}
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
		Line int    `json:"line"`
		Col  int    `json:"col"`
		Mode string `json:"mode"`
		Sel  []int  `json:"sel"`
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
	t.opts.Types = m.databases.knownColumnTypes(connID) // Extract as table function
	if len(pos.Sel) == 4 {
		if s, e, ok := dbIntentSelRange(lines, start, end, [2]int{pos.Sel[0], pos.Sel[1]}, [2]int{pos.Sel[2], pos.Sel[3]}, pos.Mode == "V"); ok {
			t.opts.SelStart, t.opts.SelEnd = s, e
		}
	}
	items := dbSQLIntentItemsWith(t.stmt, dialect, cur, t.cols, t.opts)
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No SQL intentions here",
			"Put the cursor on a table, a *, a literal, a subquery or a CTE, or select an expression or a subquery. Expand * and Qualify need the columns: connect the file first.")
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
	out, err := dbclient.ApplySQLIntentionWith(t.stmt, t.dialect, id, t.cur, t.cols, t.opts)
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
