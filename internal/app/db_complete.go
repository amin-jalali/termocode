package app

import (
	"context"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// SQL completion (plan F4): Go pushes the schema of a console's connection
// to the buffer (a table per buffer, read by the Lua omnifunc in
// db_console_lua.go); nothing is asked of the server while typing. Columns
// of a table nobody expanded are loaded on the first "alias." and pushed
// when they arrive.

func (s *dbState) setColumns(conn, table string, names []string) {
	if s.cols == nil {
		s.cols = map[string]map[string][]string{}
	}
	if s.cols[conn] == nil {
		s.cols[conn] = map[string][]string{}
	}
	s.cols[conn][strings.ToLower(table)] = names
	s.schemaVer++
}

type dbSchemaTable struct{ N, K string }

// schemaPayload is what the omnifunc reads for one connection: object
// names with their kind, schema names, the known columns, the quote.
func (s *dbState) schemaPayload(conn dbclient.Conn) map[string]any {
	seen := map[string]bool{}
	var tables []dbSchemaTable
	schemaSet := map[string]bool{}
	for _, n := range s.nodes {
		if n.conn != conn.ID {
			continue
		}
		kind := ""
		switch n.kind {
		case dbNodeTable:
			kind = "table"
		case dbNodeView:
			kind = "view"
		case dbNodeRoutine:
			kind = "func"
		case dbNodeSequence:
			kind = "seq"
		case dbNodeSchema, dbNodeDatabase:
			if n.name != "" {
				schemaSet[n.name] = true
			}
			continue
		default:
			continue
		}
		key := kind + "\x00" + n.name
		if n.name == "" || seen[key] {
			continue
		}
		seen[key] = true
		tables = append(tables, dbSchemaTable{N: n.name, K: kind})
	}
	sort.Slice(tables, func(i, j int) bool {
		if tables[i].N != tables[j].N {
			return tables[i].N < tables[j].N
		}
		return tables[i].K < tables[j].K
	})
	schemas := make([]string, 0, len(schemaSet))
	for name := range schemaSet {
		schemas = append(schemas, name)
	}
	sort.Strings(schemas)
	cols := map[string][]string{}
	for table, names := range s.cols[conn.ID] {
		cols[table] = names
	}
	quote := `"`
	if _, my := dbclient.DialectFor(conn.Driver).(dbclient.MySQLDialect); my {
		quote = "`"
	}
	// Plain maps and slices: the RPC encoder ignores struct tags.
	list := make([]map[string]any, 0, len(tables))
	for _, t := range tables {
		list = append(list, map[string]any{"n": t.N, "k": t.K})
	}
	return map[string]any{"quote": quote, "tables": list, "schemas": schemas, "cols": cols}
}

// databasePushSchema sends the connection's schema to a SQL buffer when it
// changed since the last push.
func (m *Model) databasePushSchema(buf int, c dbclient.Conn) {
	s := m.dbConsole()
	stamp := c.ID + "@" + utoa(m.databases.schemaVer)
	if s.pushed[buf] == stamp {
		return
	}
	if s.pushed == nil {
		s.pushed = map[int]string{}
	}
	s.pushed[buf] = stamp
	_ = m.nvim.ExecLuaArgs(`local b,p=...;_G._termocode_sql.schema[b]=p`, buf, m.databases.schemaPayload(c))
}

func utoa(n uint64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{digits[n%10]}, b...)
	}
	return string(b)
}

// databaseSchemaChanged refreshes the schema of every open SQL buffer
// (the tree loaded something).
func (m *Model) databaseSchemaChanged() {
	if m.nvim == nil {
		return
	}
	var bufs []int
	_ = m.nvim.ExecLuaResult(`local out={};for _,b in ipairs(vim.api.nvim_list_bufs()) do if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].filetype=='sql' and _G._termocode_sql.rows[b] then out[#out+1]=b end end;return out`, &bufs)
	for _, b := range bufs {
		m.databaseBufferLens(b)
	}
}

type dbColsMsg struct {
	buf   int
	conn  string
	table string
	names []string
	err   error
	gen   uint64
}

// databaseLoadColumns reads the columns of one table for the omnifunc
// (asked by Lua on the first "alias."); the tree is not touched.
func (m *Model) databaseLoadColumns(buf int, table string) tea.Cmd {
	if m.nvim == nil || m.databases == nil {
		return nil
	}
	file, lines, ok := m.httpBufText(buf)
	if !ok {
		return nil
	}
	s := m.dbConsole()
	id := s.consoleConn(file, lines)
	c, ok := m.databases.connection(id)
	if !ok {
		return nil
	}
	var paths []string
	for path, n := range m.databases.nodes {
		if n.conn == id && (n.kind == dbNodeTable || n.kind == dbNodeView) && strings.EqualFold(n.name, table) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		t, ok := m.databaseTableAt(path)
		if !ok {
			continue
		}
		sess := m.databases.sessions[t.sessKey]
		if sess == nil {
			continue
		}
		gen := s.tableGen
		name := t.name
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
			defer cancel()
			ir, err := sess.Introspect(ctx, t.request())
			msg := dbColsMsg{buf: buf, conn: c.ID, table: name, err: err, gen: gen}
			for _, col := range ir.Columns {
				msg.names = append(msg.names, sanitizeDBText(col.Name))
			}
			return msg
		}
	}
	return nil
}

func (m *Model) onDBCols(msg dbColsMsg) tea.Cmd {
	if m.databases == nil || msg.err != nil || m.dbConsole().tableGen != msg.gen {
		return nil
	}
	m.databases.setColumns(msg.conn, msg.table, msg.names)
	if m.nvim == nil {
		return nil
	}
	m.databaseBufferLens(msg.buf)
	_ = m.nvim.ExecLuaArgs(`_G._termocode_sql.retrigger(...)`, msg.buf, strings.ToLower(msg.table))
	return m.fetchStateCmd()
}

func init() {
	registerNotifyHandler("termocode_db_cols", func(m *Model, args []any) tea.Cmd {
		return m.databaseLoadColumns(notifyArgInt(args, 0), notifyArgString(args, 1))
	})
}
