package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The flows of DB-6 on the tree: o data view, d describe, D DDL view,
// s select in the console, # count rows, y copy the name. Each one reads
// the catalogs or one page of rows on the table's own session, off the UI
// goroutine, and drops its answer when the view hid or the connection
// closed in the meantime (tableGen).

// databaseTableAt is the table or view of a tree node.
func (m *Model) databaseTableAt(path string) (dbTable, bool) {
	s := m.databases
	if s == nil {
		return dbTable{}, false
	}
	n, ok := s.nodes[path]
	if !ok || (n.kind != dbNodeTable && n.kind != dbNodeView) {
		return dbTable{}, false
	}
	c, ok := s.connection(n.conn)
	if !ok || dbclient.DialectFor(c.Driver) == nil {
		return dbTable{}, false
	}
	t := dbTable{conn: n.conn, driver: c.Driver, name: n.name, view: n.kind == dbNodeView, path: path, sessKey: s.sessionKeyFor(path, n.conn)}
	if _, pg := dbclient.DialectFor(c.Driver).(dbclient.PostgresDialect); pg {
		t.schema = dbAncestorSchema(path)
	} else {
		t.schema = dbAncestorDatabase(path) // MySQL: the database; SQLite: main
	}
	return t, true
}

// sessionKeyFor is the key of the session that reaches a tree node: the
// nearest ancestor with its own session (a PostgreSQL database), else the
// connection's.
func (s *dbState) sessionKeyFor(path, conn string) string {
	for p := path; ; {
		if s.sessions[p] != nil {
			return p
		}
		i := strings.LastIndex(p, "/")
		if i < 0 {
			break
		}
		p = p[:i]
	}
	return conn
}

func (t dbTable) request() dbclient.IntrospectionRequest {
	kind := "table"
	if t.view {
		kind = "view"
	}
	return dbclient.IntrospectionRequest{Database: dbAncestorDatabase(t.path), Schema: t.schema, Object: t.name, Kind: kind}
}

// databaseTableTarget is the table under the tree cursor, or a toast that
// says what to select.
func (m *Model) databaseTableTarget() (dbTable, tea.Cmd, bool) {
	row, ok := m.databaseCurrentRow()
	if ok {
		if t, ok := m.databaseTableAt(row.path); ok {
			if m.databases.sessions[t.sessKey] == nil {
				return t, m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection."), false
			}
			return t, nil, true
		}
	}
	return dbTable{}, m.pushToastDetail(toast.Info, "Select a table or view", "Move to a table in the tree first."), false
}

func (m *Model) nextTableGen() uint64 {
	s := m.dbConsole()
	s.tableGen++
	return s.tableGen
}

// ── data view ───────────────────────────────────────────────────────────

type dbTableMetaMsg struct {
	gen  uint64
	tv   *dbTableView
	meta dbTableMeta
	err  error
}

// databaseOpenTable opens the data view of a table or view: the keys and
// columns first (they give the default order), then the first page.
func (m *Model) databaseOpenTable() tea.Cmd {
	t, cmd, ok := m.databaseTableTarget()
	if !ok {
		return cmd
	}
	s := m.dbConsole()
	if s.running != nil {
		return m.pushToastDetail(toast.Warn, "A query is running", "Esc cancels it.")
	}
	sess := m.databases.sessions[t.sessKey]
	tv := newDBTableView(t)
	gen := m.nextTableGen()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		ir, err := sess.Introspect(ctx, t.request())
		return dbTableMetaMsg{gen: gen, tv: tv, meta: dbTableMeta{ir: ir}, err: err}
	}
}

func (m *Model) onDBTableMeta(msg dbTableMetaMsg) tea.Cmd {
	if m.databases == nil || m.dbConsole().tableGen != msg.gen {
		return nil
	}
	if msg.err != nil {
		return m.dbConsoleError(msg.err)
	}
	tv := msg.tv
	ir := sanitizeDBIntrospection(msg.meta.ir)
	tv.keys = nil
	for _, k := range ir.Keys {
		tv.keys = append(tv.keys, k)
	}
	for _, c := range ir.Columns {
		tv.cols = append(tv.cols, c.Name)
		if c.PK && !dbContains(tv.keys, c.Name) {
			tv.keys = append(tv.keys, c.Name)
		}
	}
	tv.unique = dbUniqueSets(ir)
	tv.rowid = ""
	if _, lite := dbclient.DialectFor(tv.driver).(dbclient.SQLiteDialect); lite && !tv.view && len(tv.keys) == 0 && len(tv.unique) == 0 {
		tv.rowid = dbSQLiteRowid(tv.cols, ir.DDL) // no key: the rowid finds the row
	}
	return m.databaseRunTable(tv)
}

// databaseRunTable asks the server for the page the view stands on.
func (m *Model) databaseRunTable(tv *dbTableView) tea.Cmd {
	s := m.dbConsole()
	if s.running != nil {
		return nil
	}
	if m.databases.sessions[tv.sessKey] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	size := loadDBConsoleSettings().pageRows()
	sql, err := tv.selectSQL(size + 1)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot show this table", sanitizeDBText(err.Error()))
	}
	r := &dbRun{conn: tv.conn, sql: sql, by: "user", statements: []string{sql}, sessKey: tv.sessKey, maxRows: size + 1, tv: tv, readOnly: true}
	if t := s.curTab(); t != nil && t.run != nil && t.run.tv == tv {
		r.tab = t // a page, a sort or a filter of the data view on the page
	}
	return m.databaseStartRun(r)
}

// databaseTablePage moves a data view by pages (delta ±1) when the server
// has them; false when there is nothing to move to.
func (m *Model) databaseTablePage(tv *dbTableView, delta int) (tea.Cmd, bool) {
	size := max(tv.size, 1)
	switch {
	case delta > 0 && tv.more:
		tv.offset += size
	case delta < 0 && tv.offset > 0:
		tv.offset = max(tv.offset-size, 0)
	default:
		return nil, false
	}
	return m.databaseRunTable(tv), true
}

// databaseTableWhere applies the WHERE the user typed. A refused text
// changes nothing and sends nothing.
func (m *Model) databaseTableWhere(tv *dbTableView, text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if err := dbWhereCheck(tv.driver, text); err != nil {
		return m.pushToastDetail(toast.Warn, "Filter refused", sanitizeDBText(err.Error()))
	}
	old := tv.where
	tv.where, tv.offset, tv.total = text, 0, -1
	if _, err := tv.selectSQL(2); err != nil {
		tv.where = old
		return m.pushToastDetail(toast.Warn, "Filter refused", sanitizeDBText(err.Error()))
	}
	return m.databaseRunTable(tv)
}

// databaseTableSort sorts the data view by a column on the server; the
// same column again flips the order.
func (m *Model) databaseTableSort(tv *dbTableView, col string) tea.Cmd {
	if tv.orderCol == col {
		tv.desc = !tv.desc
	} else {
		tv.orderCol, tv.desc = col, false
	}
	tv.offset = 0
	return m.databaseRunTable(tv)
}

// ── count ───────────────────────────────────────────────────────────────

type dbCountMsg struct {
	gen   uint64
	tv    *dbTableView // nil: counted from the tree
	label string
	n     int
	err   error
}

// databaseCount counts the rows of tv (the WHERE of a data view included)
// on demand; it is never automatic.
func (m *Model) databaseCount(tv *dbTableView) tea.Cmd {
	sql, err := tv.countSQL()
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot count this table", sanitizeDBText(err.Error()))
	}
	sess := m.databases.sessions[tv.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	gen := m.nextTableGen()
	label := tv.label()
	inTx := m.databases.txOpen(tv.sessKey)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadDBConsoleSettings().timeout())
		defer cancel()
		var r dbclient.Result
		if inTx {
			r = dbReadInTx(ctx, sess, sql, dbclient.RunOptions{MaxRows: 1})
		} else {
			r = dbRunReadOnly(ctx, sess, sql, dbclient.RunOptions{MaxRows: 1})
		}
		msg := dbCountMsg{gen: gen, tv: tv, label: label, err: r.Err}
		if r.Err == nil && len(r.Rows) > 0 && len(r.Rows[0]) > 0 {
			if _, e := fmt.Sscan(r.Rows[0][0].Text, &msg.n); e != nil {
				msg.err = e
			}
		}
		return msg
	}
}

func (m *Model) databaseCountFromTree() tea.Cmd {
	t, cmd, ok := m.databaseTableTarget()
	if !ok {
		return cmd
	}
	return m.databaseCount(newDBTableView(t))
}

func (m *Model) onDBCount(msg dbCountMsg) tea.Cmd {
	if m.databases == nil || m.dbConsole().tableGen != msg.gen {
		return nil
	}
	if msg.err != nil {
		return m.dbConsoleError(msg.err)
	}
	if s := m.dbConsole(); msg.tv != nil && s.shown != nil && s.shown.tv == msg.tv {
		msg.tv.total = msg.n
		return m.databaseShowPage()
	}
	return m.pushToastDetail(toast.Info, dbPlural(msg.n, "row"), msg.label)
}

// ── describe and DDL ────────────────────────────────────────────────────

type dbMetaMsg struct {
	gen  uint64
	tbl  dbTable
	meta dbTableMeta
	ddl  bool
	err  error
}

func (m *Model) databaseTableMeta(ddl bool) tea.Cmd {
	t, cmd, ok := m.databaseTableTarget()
	if !ok {
		return cmd
	}
	return m.databaseLoadMeta(t, ddl)
}

func (m *Model) databaseLoadMeta(t dbTable, ddl bool) tea.Cmd {
	sess := m.databases.sessions[t.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	gen := m.nextTableGen()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		meta, err := dbLoadTableMeta(ctx, sess, t, t.request())
		return dbMetaMsg{gen: gen, tbl: t, meta: meta, ddl: ddl, err: err}
	}
}

func (m *Model) onDBMeta(msg dbMetaMsg) tea.Cmd {
	if m.databases == nil || m.dbConsole().tableGen != msg.gen {
		return nil
	}
	if msg.err != nil {
		return m.dbConsoleError(msg.err)
	}
	if msg.ddl {
		return m.databaseShowDDL(msg.tbl, dbBuildDDL(msg.tbl, msg.meta))
	}
	s := m.dbConsole()
	if s.running != nil {
		return nil
	}
	tbl := msg.tbl
	res := dbDescribeResult(tbl, msg.meta)
	r := &dbRun{conn: tbl.conn, sql: "Describe " + tbl.label(), by: "user", result: res, describe: &tbl}
	if t := s.curTab(); t != nil && t.run != nil && t.run.describe != nil && *t.run.describe == tbl {
		r.tab = t // r on a description reads the catalogs again in its tab
	}
	s.placeRun(r)
	s.cache, s.page, s.size, s.view = nil, 0, loadDBConsoleSettings().pageRows(), dbViewGrid
	s.filter, s.sortCol, s.desc, s.trans, s.trow = "", -1, false, false, 0
	return m.databaseShowPage()
}

func (m *Model) databaseShowDDL(t dbTable, text string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.ensureDBLua()
	m.ensureEditorWindowCurrent()
	name := "SQL DDL: " + t.label()
	_ = m.nvim.ExecLuaArgs(`_G._termocode_db_ddl(...)`, name, strings.Split(text, "\n"))
	m.focus = FocusEditor
	return m.fetchStateCmd()
}

// ── select in the console, copy the name ────────────────────────────────

// databaseConsoleFile makes sure the connection's console file exists and
// returns its path.
func (m *Model) databaseConsoleFile(id string) (string, error) {
	path := m.databaseConsolePath(id)
	if path == "" {
		return "", os.ErrInvalid
	}
	if err := os.MkdirAll(dirOf(path), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, err = f.WriteString("-- @conn " + id + "\n\n")
		_ = f.Close()
	} else if os.IsExist(err) {
		err = nil
	}
	return path, err
}

func dirOf(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[:i]
	}
	return "."
}

// databaseSelectInConsole writes SELECT * FROM <table> LIMIT n; at the end
// of the connection's console and runs it there.
func (m *Model) databaseSelectInConsole() tea.Cmd {
	t, cmd, ok := m.databaseTableTarget()
	if !ok {
		return cmd
	}
	if m.nvim == nil {
		return nil
	}
	s := m.dbConsole()
	if s.running != nil {
		return m.pushToastDetail(toast.Warn, "A query is running", "Esc cancels it.")
	}
	path, err := m.databaseConsoleFile(t.conn)
	if err != nil {
		return m.dbConsoleError(err)
	}
	stmt := fmt.Sprintf("SELECT * FROM %s LIMIT %d;", t.qualified(), loadDBConsoleSettings().pageRows())
	if err := dbReadOnlyCheck(t.driver, stmt); err != nil {
		return m.dbConsoleError(err)
	}
	m.ensureDBLua()
	m.ensureEditorWindowCurrent()
	var line int
	_ = m.nvim.ExecLuaResult(`local f,stmt=...
vim.cmd('edit '..vim.fn.fnameescape(f)); vim.bo.filetype='sql'
local all=vim.api.nvim_buf_get_lines(0,0,-1,false)
local n=#all
while n>0 and all[n]=='' do n=n-1 end
if n>0 and all[n]==stmt then -- pressed twice: use the line that is there
 vim.api.nvim_win_set_cursor(0,{n,0})
 return n
end
local at
if n>0 then vim.api.nvim_buf_set_lines(0,n,-1,false,{'',stmt,''}); at=n+2
else vim.api.nvim_buf_set_lines(0,0,-1,false,{stmt,''}); at=1 end
pcall(vim.cmd,'silent write')
vim.api.nvim_win_set_cursor(0,{at,0})
return at`, &line, path, stmt)
	m.focus = FocusEditor
	r := &dbRun{conn: t.conn, file: path, sql: stmt, line: max(line, 1), by: "user", statements: []string{stmt}, sessKey: t.sessKey}
	return tea.Batch(m.fetchStateCmd(), m.databaseReviewRun(r))
}

func (m *Model) databaseCopyTableName() tea.Cmd {
	t, cmd, ok := m.databaseTableTarget()
	if !ok {
		return cmd
	}
	return m.copyWithToast(t.qualified(), "Copied name")
}

// dbRunReadOnly runs sql in a session the server holds read-only, so a
// function that writes fails there whatever the text said. The session is
// closed again at once.
func dbRunReadOnly(ctx context.Context, sess dbclient.Session, sql string, opt dbclient.RunOptions) dbclient.Result {
	ro, err := sess.ReadOnlySession()
	if err != nil {
		return dbclient.Result{Err: err}
	}
	defer ro.Close()
	return ro.Run(ctx, sql, opt)
}
