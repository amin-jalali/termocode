package app

import (
	"encoding/json"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Quick documentation (DBX-30): Databases: Quick Documentation (K in the
// tree) shows a card about the table, column or routine named under the
// cursor of a SQL buffer, or about the tree node. It reads only what the
// tree has loaded: no query runs. Databases: Show INSERT Column Hints puts
// the target column before each value of INSERT … VALUES (inline virtual
// text; they clear on the next edit, run it again after).

// dbQualName is name with the schema (or MySQL database) of its tree path.
func dbQualName(path, name string) string {
	if sch := dbAncestorSchema(path); sch != "" {
		return sch + "." + name
	}
	if db := dbAncestorDatabase(path); db != "" {
		return db + "." + name
	}
	return name
}

// dbColumnParts splits a column node name "id  bigint" into name and type.
func dbColumnParts(n dbNode) (name, typ string) {
	name, typ, _ = strings.Cut(n.name, "  ")
	return name, strings.TrimSpace(typ)
}

// dbFindNodes lists the paths of the nodes of kind on conn named name
// (case insensitive), sorted.
func (s *dbState) dbFindNodes(conn string, kind dbNodeKind, name string) []string {
	var out []string
	for p, n := range s.nodes {
		if n.conn == conn && n.kind == kind && strings.EqualFold(n.name, name) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// dbQuickDoc is the title and lines of the quick documentation of ref on
// conn.
func dbQuickDoc(s *dbState, conn string, ref dbclient.SQLRef) (string, []string) {
	notLoaded := "Not in the Databases tree: expand its schema in the Databases view, or Refresh."
	switch ref.Kind {
	case "table":
		path := s.dbFindTablePath(conn, ref.Schema, ref.Table)
		if path == "" {
			return "Table " + dbJoinName(ref.Schema, ref.Table), []string{notLoaded}
		}
		return dbQuickDocNode(s, path)
	case "column":
		if ref.Table != "" {
			path := s.dbFindTablePath(conn, ref.Schema, ref.Table)
			if path == "" {
				return "Column " + dbJoinName(ref.Schema, ref.Table, ref.Column), []string{notLoaded}
			}
			if c := dbColumnPath(s, path, ref.Column); c != "" {
				return dbQuickDocNode(s, c)
			}
			if !s.nodes[path].loaded {
				return "Column " + dbJoinName(ref.Schema, ref.Table, ref.Column), []string{"The columns of " + dbQualName(path, s.nodes[path].name) + " are not loaded: expand the table in the Databases view."}
			}
			return "Column " + dbJoinName(ref.Schema, ref.Table, ref.Column), []string{dbQualName(path, s.nodes[path].name) + " has no column " + ref.Column + "."}
		}
		var lines []string
		for _, kind := range []dbNodeKind{dbNodeTable, dbNodeView} {
			for p, n := range s.nodes {
				if n.conn != conn || n.kind != kind {
					continue
				}
				if c := dbColumnPath(s, p, ref.Column); c != "" {
					_, typ := dbColumnParts(s.nodes[c])
					lines = append(lines, dbQualName(p, n.name)+"  "+typ)
				}
			}
		}
		if len(lines) == 0 {
			return "Column " + ref.Column, []string{"No loaded table has this column. Expand the tables in the Databases view."}
		}
		sort.Strings(lines)
		return "Column " + ref.Column, append([]string{"In the loaded tables:"}, lines...)
	case "routine":
		for _, p := range s.dbFindNodes(conn, dbNodeRoutine, ref.Routine) {
			if ref.Schema == "" || strings.EqualFold(dbAncestorSchema(p), ref.Schema) || strings.EqualFold(dbAncestorDatabase(p), ref.Schema) {
				return dbQuickDocNode(s, p)
			}
		}
		return "Routine " + dbJoinName(ref.Schema, ref.Routine), []string{"Not a routine of the Databases tree (a built-in function, or its schema is not loaded)."}
	case "schema":
		for _, kind := range []dbNodeKind{dbNodeSchema, dbNodeDatabase} {
			if ps := s.dbFindNodes(conn, kind, ref.Schema); len(ps) > 0 {
				return dbQuickDocNode(s, ps[0])
			}
		}
		return "Schema " + ref.Schema, []string{notLoaded}
	}
	return "", nil
}

func dbJoinName(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ".")
}

// dbColumnPath is the path of column col of the table at path, or "".
func dbColumnPath(s *dbState, path, col string) string {
	for _, c := range s.nodes[path].children {
		n := s.nodes[c]
		if name, _ := dbColumnParts(n); n.kind == dbNodeColumn && strings.EqualFold(name, col) {
			return c
		}
	}
	return ""
}

// dbQuickDocNode is the quick documentation of the tree node at path.
func dbQuickDocNode(s *dbState, path string) (string, []string) {
	n, ok := s.nodes[path]
	if !ok {
		return "", nil
	}
	switch n.kind {
	case dbNodeTable, dbNodeView:
		kind := "Table"
		if n.kind == dbNodeView {
			kind = "View"
		}
		title := kind + " " + dbQualName(path, n.name)
		if !n.loaded {
			return title, []string{"Columns not loaded yet: expand it in the Databases view (Enter)."}
		}
		var cols, more []string
		for _, c := range n.children {
			cn := s.nodes[c]
			switch cn.kind {
			case dbNodeColumn:
				line := "  " + cn.name
				if cn.detail != "" {
					line += "  " + cn.detail
				}
				cols = append(cols, line)
			case dbNodeDetail:
				more = append(more, "  "+cn.name)
			}
		}
		lines := []string{dbPlural(len(cols), "column") + ":"}
		lines = append(lines, cols...)
		if len(more) > 0 {
			lines = append(append(lines, "", "Keys and indexes:"), more...)
		}
		return title, append(lines, "", "o opens the data · D shows the DDL")
	case dbNodeColumn:
		name, typ := dbColumnParts(n)
		table := path
		if i := strings.LastIndex(table, "/col/"); i >= 0 {
			table = table[:i]
		}
		tn := s.nodes[table]
		lines := []string{"Table: " + dbQualName(table, tn.name)}
		if typ != "" {
			lines = append(lines, "Type: "+typ)
		}
		if n.detail == "PK" {
			lines = append(lines, "Primary key")
		}
		for _, c := range tn.children {
			if d := s.nodes[c]; d.kind == dbNodeDetail && strings.Contains(d.name, " "+name+" ") {
				lines = append(lines, d.name)
			}
		}
		return "Column " + dbQualName(table, tn.name+"."+name), lines
	case dbNodeRoutine:
		return "Routine " + dbQualName(path, n.name), []string{"A routine of the Databases tree. Its parameters show in its DDL."}
	case dbNodeSchema, dbNodeDatabase:
		kind := "Schema "
		if n.kind == dbNodeDatabase {
			kind = "Database "
		}
		var lines []string
		for _, c := range n.children {
			if g := s.nodes[c]; g.kind == dbNodeGroup {
				lines = append(lines, g.name+": "+dbItoa(len(g.children)))
			}
		}
		if len(lines) == 0 {
			lines = []string{"Not loaded yet: expand it in the Databases view (Enter)."}
		}
		return kind + n.name, lines
	}
	lines := []string{}
	if n.detail != "" {
		lines = append(lines, n.detail)
	}
	return n.name, lines
}

func dbItoa(n int) string { return utoa(uint64(max(n, 0))) }

// dbOffsetAt is the byte offset of (row, col) (0-based) in the buffer
// lines joined with "\n".
func dbOffsetAt(lines []string, row, col int) int {
	off := 0
	for i := 0; i < row && i < len(lines); i++ {
		off += len(lines[i]) + 1
	}
	return off + col
}

// dbHintPos is an INSERT column hint at a 0-based buffer position.
type dbHintPos struct {
	Row, Col int
	Text     string
}

// dbHintPositions turns the byte offsets of the hints into buffer rows
// and columns.
func dbHintPositions(lines []string, hints []dbclient.InsertHint) []dbHintPos {
	out := make([]dbHintPos, 0, len(hints))
	row, start := 0, 0
	for _, h := range hints {
		for row < len(lines) && h.Offset > start+len(lines[row]) {
			start += len(lines[row]) + 1
			row++
		}
		if row >= len(lines) {
			break
		}
		out = append(out, dbHintPos{Row: row, Col: h.Offset - start, Text: h.Column + ":"})
	}
	return out
}

const dbCursorRowLua = `local c=vim.api.nvim_win_get_cursor(0)
return vim.fn.json_encode({row=c[1]-1,col=c[2]})`

// dbSQLBuffer reads the current SQL buffer, its connection and driver.
func (m *Model) dbSQLBuffer() (lines []string, conn dbclient.Conn, cmd tea.Cmd, ok bool) {
	if m.nvim == nil {
		return nil, dbclient.Conn{}, m.pushToastDetail(toast.Info, "Open a SQL file", "Put the cursor on a name in a SQL console or file."), false
	}
	file, lines, ok := m.httpBufText(0)
	if !ok || !strings.EqualFold(lastExt(file), ".sql") {
		return nil, dbclient.Conn{}, m.pushToastDetail(toast.Info, "Open a SQL file", "Put the cursor on a name in a SQL console or file."), false
	}
	m.ensureDatabasesLoaded()
	id := m.dbConsole().consoleConn(file, lines)
	c, found := m.databases.connection(id)
	if id == "" || !found {
		return nil, dbclient.Conn{}, m.pushToastDetail(toast.Info, "No connection for this file", "Run a statement first to pick its connection."), false
	}
	return lines, c, nil, true
}

// databaseQuickDoc shows the quick documentation of the tree row when
// the Databases view has the focus, else of the SQL name under the cursor.
func (m *Model) databaseQuickDoc() tea.Cmd {
	if m.focus == FocusExplorer && m.activity.Active() == activity.ViewDatabases {
		return m.databaseQuickDocTree()
	}
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
	ref, ok := dbclient.SQLRefAt(strings.Join(lines, "\n"), dbOffsetAt(lines, pos.Row, pos.Col), c.Driver)
	if !ok {
		return m.pushToastDetail(toast.Info, "No name under the cursor", "Put the cursor on a table, column or routine name.")
	}
	title, body := dbQuickDoc(m.databases, c.ID, ref)
	return m.databaseQuickDocCard(title, body)
}

// databaseQuickDocTree is K on a tree row.
func (m *Model) databaseQuickDocTree() tea.Cmd {
	r, ok := m.databaseCurrentRow()
	if !ok || r.path == "" {
		return m.pushToastDetail(toast.Info, "Select an item first", "Put the cursor on a table, column or routine in the Databases view.")
	}
	title, body := dbQuickDocNode(m.databases, r.path)
	if title == "" {
		return nil
	}
	return m.databaseQuickDocCard(title, body)
}

func (m *Model) databaseQuickDocCard(title string, body []string) tea.Cmd {
	for i, l := range body {
		body[i] = sanitizeDBText(l)
	}
	m.confirm = confirm.New(sanitizeDBText(title), strings.Join(body, "\n"), []confirm.Button{{ID: "close", Title: "Close", Style: confirm.StylePrimary}}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBInfo
	return nil
}

// dbInsertHintsLua draws the hints as inline virtual text; they clear on
// the next change of the buffer.
const dbInsertHintsLua = `local hints=...
local ns=vim.api.nvim_create_namespace('termocode_db_insert_hints')
local buf=vim.api.nvim_get_current_buf()
vim.api.nvim_buf_clear_namespace(buf,ns,0,-1)
for _,h in ipairs(hints) do
  local ok=pcall(vim.api.nvim_buf_set_extmark,buf,ns,h.Row,h.Col,{virt_text={{h.Text..' ','Comment'}},virt_text_pos='inline'})
  if not ok then pcall(vim.api.nvim_buf_set_extmark,buf,ns,h.Row,h.Col,{virt_text={{h.Text,'Comment'}},virt_text_pos='eol'}) end
end
vim.api.nvim_create_autocmd({'TextChanged','TextChangedI'},{buffer=buf,once=true,callback=function() vim.api.nvim_buf_clear_namespace(buf,ns,0,-1) end})
return #hints`

// databaseInsertHints shows the target column before each INSERT value.
func (m *Model) databaseInsertHints() tea.Cmd {
	lines, c, cmd, ok := m.dbSQLBuffer()
	if !ok {
		return cmd
	}
	pos := dbHintPositions(lines, dbclient.InsertValueHints(strings.Join(lines, "\n"), c.Driver))
	if len(pos) == 0 {
		return m.pushToastDetail(toast.Info, "No INSERT column hints", "Hints need INSERT INTO t (columns…) VALUES (…).")
	}
	var n int
	_ = m.nvim.ExecLuaResult(dbInsertHintsLua, &n, pos)
	return m.pushToast(toast.Info, "INSERT column hints shown (they clear when you edit)")
}
