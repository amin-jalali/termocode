package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// More quick documentation (DBX-30):
//   - Databases: Show Parameter Info lists the parameters of the routine
//     call around the cursor and marks the one the cursor is in: common
//     built-in functions from a list, the tree's routines from a read-only
//     catalog read.
//   - Databases: Show JOIN Cardinality Hints writes 1:1, N:1, 1:N or N:M
//     before each joined table, from the primary keys, unique indexes and
//     foreign keys the tree has loaded (no query runs).
//   - gK on a result cell shows the whole value and its row.
//   - K on a table in the tree also shows its first rows (one read-only
//     SELECT … LIMIT 5 when the connection is open).

// dbQuickDocRowCount is how many rows the tree doc of a table shows, and
// dbQuickDocRowWidth how wide one row line may be.
const (
	dbQuickDocRowCount = 5
	dbQuickDocRowWidth = 120
)

// setUniqueIndexes keeps the column sets of the unique indexes of the
// table at path (the JOIN hints read them).
func (s *dbState) setUniqueIndexes(path string, sets [][]string) {
	if s.uniques == nil {
		s.uniques = map[string][][]string{}
	}
	if len(sets) == 0 {
		delete(s.uniques, path)
		return
	}
	s.uniques[path] = sets
}

// dbCovers reports whether cols holds every name of set (case insensitive).
func dbCovers(cols, set []string) bool {
	if len(set) == 0 {
		return false
	}
	for _, k := range set {
		found := false
		for _, c := range cols {
			if strings.EqualFold(c, k) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// dbColsUnique reports whether cols of the table at path hold its primary
// key or one of its unique indexes.
func dbColsUnique(s *dbState, path string, cols []string) bool {
	var pk []string
	for _, c := range s.nodes[path].children {
		if n := s.nodes[c]; n.kind == dbNodeColumn && n.detail == "PK" {
			name, _ := dbColumnParts(n)
			pk = append(pk, name)
		}
	}
	if dbCovers(cols, pk) {
		return true
	}
	for _, set := range s.uniques[path] {
		if dbCovers(cols, set) {
			return true
		}
	}
	return false
}

// dbFKTo reports whether the table at from has a foreign key from its
// column fromCol to table.col: the referenced column is a key, so unique.
func dbFKTo(s *dbState, from string, fromCols []string, table string, cols []string) bool {
	if len(fromCols) != 1 || len(cols) != 1 {
		return false
	}
	for _, c := range s.nodes[from].children {
		n := s.nodes[c]
		if n.kind != dbNodeDetail {
			continue
		}
		rest, ok := strings.CutPrefix(n.name, "Foreign key · ")
		if !ok {
			continue
		}
		col, ref, ok := strings.Cut(rest, " → ")
		i := strings.LastIndex(ref, ".")
		if !ok || i < 0 {
			continue
		}
		refTable := ref[:i]
		if j := strings.LastIndex(refTable, "."); j >= 0 {
			refTable = refTable[j+1:]
		}
		if strings.EqualFold(col, fromCols[0]) && strings.EqualFold(refTable, table) && strings.EqualFold(ref[i+1:], cols[0]) {
			return true
		}
	}
	return false
}

// dbJoinCardinality is the hint of h on conn: other:joined, "1" for the
// side whose compared columns are unique; "" when a table is not loaded.
func dbJoinCardinality(s *dbState, conn string, h dbclient.JoinHint) string {
	jp := s.dbFindTablePath(conn, h.Joined.Schema, h.Joined.Table)
	op := s.dbFindTablePath(conn, h.Other.Schema, h.Other.Table)
	if jp == "" || op == "" || !s.nodes[jp].loaded || !s.nodes[op].loaded {
		return ""
	}
	ju := dbColsUnique(s, jp, h.Joined.Columns) || dbFKTo(s, op, h.Other.Columns, s.nodes[jp].name, h.Joined.Columns)
	ou := dbColsUnique(s, op, h.Other.Columns) || dbFKTo(s, jp, h.Joined.Columns, s.nodes[op].name, h.Other.Columns)
	switch {
	case ju && ou:
		return "1:1"
	case ju:
		return "N:1"
	case ou:
		return "1:N"
	}
	return "N:M"
}

// dbJoinHintPositions are the JOIN cardinality hints of the buffer lines.
func dbJoinHintPositions(s *dbState, conn string, lines []string, driver string) []dbHintPos {
	var offs []int
	var texts []string
	for _, h := range dbclient.JoinHints(strings.Join(lines, "\n"), driver) {
		if t := dbJoinCardinality(s, conn, h); t != "" {
			offs = append(offs, h.Offset)
			texts = append(texts, t)
		}
	}
	return dbOffsetPositions(lines, offs, texts)
}

// databaseJoinHints writes the cardinality before each joined table.
func (m *Model) databaseJoinHints() tea.Cmd {
	lines, c, cmd, ok := m.dbSQLBuffer()
	if !ok {
		return cmd
	}
	pos := dbJoinHintPositions(m.databases, c.ID, lines, c.Driver)
	if len(pos) == 0 {
		return m.pushToastDetail(toast.Info, "No JOIN cardinality hints", "Hints need JOIN … ON a.x = b.y (or USING) on tables the Databases tree has expanded.")
	}
	var n int
	_ = m.nvim.ExecLuaResult(dbInsertHintsLua, &n, pos, "termocode_db_join_hints")
	return m.pushToast(toast.Info, "JOIN cardinality hints shown (they clear when you edit)")
}

// dbParamDoc is the parameter info card of routine name; arg is the
// 0-based argument the cursor is in ("…" as the last parameter takes the
// arguments past the end).
func dbParamDoc(name string, params []string, arg int) (string, []string) {
	title := "Parameters of " + name
	if len(params) == 0 {
		return title, []string{name + "()", "", "No parameters."}
	}
	cur := arg
	if cur >= len(params) && params[len(params)-1] == "…" {
		cur = len(params) - 1
	}
	lines := []string{name + "(" + strings.Join(params, ", ") + ")", ""}
	for i, p := range params {
		mark := "  "
		if i == cur {
			mark = "▸ "
		}
		lines = append(lines, mark+strconv.Itoa(i+1)+". "+p)
	}
	if cur >= len(params) {
		lines = append(lines, "", "The cursor is in argument "+strconv.Itoa(arg+1)+": past the last parameter.")
	}
	return title, lines
}

// dbParamInfoMsg brings the signature of a tree routine, read off the UI
// goroutine.
type dbParamInfoMsg struct {
	gen     uint64
	name    string
	arg     int
	routine dbclient.CanonRoutine
	found   bool
	err     error
}

// databaseParamInfo is Databases: Show Parameter Info.
func (m *Model) databaseParamInfo() tea.Cmd {
	lines, c, cmd, ok := m.dbSQLBuffer()
	if !ok {
		return cmd
	}
	var pos struct {
		Row int `json:"row"`
		Col int `json:"col"`
	}
	var raw string
	_ = m.nvim.ExecLuaResult(dbCursorRowLua, &raw)
	_ = json.Unmarshal([]byte(raw), &pos)
	call, ok := dbclient.SQLCallAt(strings.Join(lines, "\n"), dbOffsetAt(lines, pos.Row, pos.Col), c.Driver)
	if !ok {
		return m.pushToastDetail(toast.Info, "Not inside a call", "Put the cursor between the parentheses of a function call.")
	}
	// A routine of the tree wins over a built-in of the same name.
	for _, p := range m.databases.dbFindNodes(c.ID, dbNodeRoutine, call.Routine) {
		if call.Schema != "" && !strings.EqualFold(dbAncestorSchema(p), call.Schema) && !strings.EqualFold(dbAncestorDatabase(p), call.Schema) {
			continue
		}
		if _, ok := m.databaseRoutineAt(p); ok {
			return m.databaseParamInfoLoad(p, call)
		}
	}
	if params, ok := dbclient.BuiltinParams(c.Driver, call.Routine); ok {
		title, body := dbParamDoc(call.Routine, params, call.Arg)
		return m.databaseQuickDocCard(title, body)
	}
	return m.databaseQuickDocCard("Parameters of "+call.Routine, []string{"Not a routine of the Databases tree and not a known built-in function.", "Expand its schema in the Databases view, or Refresh."})
}

// databaseParamInfoLoad reads the signature of the routine at path on a
// read-only session.
func (m *Model) databaseParamInfoLoad(path string, call dbclient.SQLCall) tea.Cmd {
	r, ok := m.databaseRoutineAt(path)
	if !ok {
		return nil
	}
	sess := m.databases.sessions[r.sessKey]
	if sess == nil {
		return m.databaseQuickDocCard("Parameters of "+dbQualName(path, r.name), []string{"Not connected: connect first (Enter on the connection) to read its parameters."})
	}
	m.databases.docGen++
	gen := m.databases.docGen
	name := dbQualName(path, r.name)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		msg := dbParamInfoMsg{gen: gen, name: name, arg: call.Arg}
		ro, err := sess.ReadOnlySession()
		if err != nil {
			msg.err = err
			return msg
		}
		defer ro.Close()
		schema, err := dbclient.ReadCanonSchema(ctx, ro, r.driver, r.schema)
		if err != nil {
			msg.err = err
			return msg
		}
		for _, rt := range schema.Routines {
			if rt.Name == r.name {
				msg.routine, msg.found = rt, true
				break
			}
		}
		return msg
	}
}

func (m *Model) onDBParamInfo(msg dbParamInfoMsg) tea.Cmd {
	if m.databases == nil || m.databases.docGen != msg.gen {
		return nil
	}
	if msg.err != nil {
		return m.dbConsoleError(msg.err)
	}
	if !msg.found {
		return m.pushToastDetail(toast.Info, "Routine not found", "Refresh the tree (r) and try again.")
	}
	var params []string
	for _, a := range dbParseRoutineArgs(msg.routine.Args) {
		p := strings.TrimSpace(strings.Join(strings.Fields(a.mode+" "+a.name+" "+a.typ), " "))
		params = append(params, p)
	}
	title, body := dbParamDoc(msg.name, params, msg.arg)
	if msg.routine.Returns != "" {
		body = append(body, "", "Returns: "+msg.routine.Returns)
	}
	return m.databaseQuickDocCard(title, body)
}

// dbCellDoc is the quick documentation of cell col of row (rowNum is the
// 1-based row number shown to people): the whole value, then the row.
func dbCellDoc(cols []dbclient.Column, row []dbclient.Cell, col, rowNum int) (string, []string) {
	name := "column " + strconv.Itoa(col+1)
	var typ string
	if col >= 0 && col < len(cols) {
		name, typ = cols[col].Name, cols[col].Type
	}
	title := "Cell " + dbOneLine(name) + " · row " + strconv.Itoa(rowNum)
	var lines []string
	if typ != "" {
		lines = append(lines, "Type: "+typ)
	}
	cell := dbclient.Cell{Null: true}
	if col >= 0 && col < len(row) {
		cell = row[col]
	}
	if cell.Null {
		lines = append(lines, "Value: NULL")
	} else {
		text := dbSafeMultiline(cell.Text)
		lines = append(lines, "Length: "+strconv.Itoa(len([]rune(cell.Text))), "Value:")
		for _, l := range strings.Split(text, "\n") {
			lines = append(lines, "  "+l)
		}
	}
	lines = append(lines, "", "Row "+strconv.Itoa(rowNum)+":")
	for i, c := range cols {
		v := dbclient.Cell{Null: true}
		if i < len(row) {
			v = row[i]
		}
		lines = append(lines, "  "+truncRunes(dbOneLine(c.Name)+" = "+dbOneLine(v.String()), dbQuickDocRowWidth))
	}
	return title, lines
}

// databaseCellDoc is gK on a result page.
func (m *Model) databaseCellDoc(row, col int) tea.Cmd {
	s, cmd, ok := m.databaseResultReady()
	if !ok {
		return cmd
	}
	page := s.pageResult()
	if s.trans && s.trow < len(page.Rows) {
		row, col = s.trow, row // the cursor line is the column
	}
	if s.view != dbViewGrid || row < 0 || row >= len(page.Rows) || col < 0 || col >= len(page.Columns) {
		return m.pushToastDetail(toast.Info, "Put the cursor on a cell", "gK shows the cell's whole value and its row.")
	}
	first := s.page * s.pageSize()
	if tv := s.shown.tv; tv != nil {
		first = max(tv.offset, 0)
	}
	title, body := dbCellDoc(page.Columns, page.Rows[row], col, first+row+1)
	return m.databaseQuickDocCard(title, body)
}

// dbQuickDocRows are the first rows of a table for its tree doc, one line
// each: name=value · name=value, cut at dbQuickDocRowWidth.
func dbQuickDocRows(cols []dbclient.Column, rows [][]dbclient.Cell) []string {
	if len(rows) == 0 {
		return []string{"", "The table is empty."}
	}
	out := []string{"", "First " + dbPlural(len(rows), "row") + ":"}
	for _, r := range rows {
		parts := make([]string, 0, len(cols))
		for i, c := range cols {
			v := dbclient.Cell{Null: true}
			if i < len(r) {
				v = r[i]
			}
			parts = append(parts, dbOneLine(c.Name)+"="+dbOneLine(v.String()))
		}
		out = append(out, "  "+truncRunes(strings.Join(parts, " · "), dbQuickDocRowWidth))
	}
	return out
}

// dbQuickDocRowsMsg brings the first rows of the table of an open tree doc.
type dbQuickDocRowsMsg struct {
	gen    uint64
	result dbclient.Result
}

// databaseQuickDocRowsLoad reads the first rows of the table at path on a
// read-only session; nil when it is not a table or not connected.
func (m *Model) databaseQuickDocRowsLoad(path string) tea.Cmd {
	t, ok := m.databaseTableAt(path)
	if !ok {
		return nil
	}
	sess := m.databases.sessions[t.sessKey]
	d := dbclient.DialectFor(t.driver)
	if sess == nil || d == nil {
		return nil
	}
	sql := "SELECT * FROM " + t.qualified() + d.Limit(dbQuickDocRowCount, 0)
	if err := dbReadOnlyCheck(t.driver, sql); err != nil {
		return nil
	}
	gen := m.databases.docGen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		ro, err := sess.ReadOnlySession()
		if err != nil {
			return dbQuickDocRowsMsg{gen: gen, result: dbclient.Result{Err: err}}
		}
		defer ro.Close()
		return dbQuickDocRowsMsg{gen: gen, result: ro.Run(ctx, sql, dbclient.RunOptions{MaxRows: dbQuickDocRowCount})}
	}
}

// onDBQuickDocRows adds the rows to the tree doc card when it is still
// the open one.
func (m *Model) onDBQuickDocRows(msg dbQuickDocRowsMsg) tea.Cmd {
	s := m.databases
	if s == nil || s.docGen != msg.gen || !m.confirmOpen || m.confirmKind != confirmKindDBInfo {
		return nil
	}
	body := append([]string(nil), s.docBody...)
	if msg.result.Err != nil {
		body = append(body, "", "First rows: "+dbOneLine(msg.result.Err.Error()))
	} else {
		body = append(body, dbQuickDocRows(msg.result.Columns, msg.result.Rows)...)
	}
	return m.databaseQuickDocCard(s.docTitle, body)
}
