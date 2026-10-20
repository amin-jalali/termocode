package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Editing a result (plan F6). Changes stay pending on the page, marked in
// the left margin (* changed, + new, - deleted), until w shows the exact
// SQL in a card and Submit runs it as one transaction: values bound as
// parameters, rows found by their full key, and each statement must change
// exactly one row or everything is rolled back. Only a data view of a
// table or a plain SELECT from one table with its key can be edited; any
// other result says why in row 2.

const (
	promptKindDBEditCell promptKindEnum = 1300 + iota
)

// dbCellRef is a cell of the loaded result (row: index in result.Rows).
type dbCellRef struct{ row, col int }

// dbNewRow is a row added on the page; a column not in set gets its
// default.
type dbNewRow struct{ set map[int]dbclient.Cell }

// dbEdits are the pending changes of one result tab.
type dbEdits struct {
	cells   map[dbCellRef]dbclient.Cell
	deleted map[int]bool
	added   []*dbNewRow
	note    string // the last submit failure, shown in row 2
}

func newDBEdits() *dbEdits {
	return &dbEdits{cells: map[dbCellRef]dbclient.Cell{}, deleted: map[int]bool{}}
}

// count is the number of statements a submit runs: one per changed,
// deleted or added row.
func (e *dbEdits) count() int {
	if e == nil {
		return 0
	}
	rows := map[int]bool{}
	for ref := range e.cells {
		if !e.deleted[ref.row] {
			rows[ref.row] = true
		}
	}
	return len(rows) + len(e.deleted) + len(e.added)
}

func (e *dbEdits) rowChanged(row int) bool {
	if e == nil {
		return false
	}
	for ref := range e.cells {
		if ref.row == row {
			return true
		}
	}
	return false
}

// dbEditTarget is where the changes of a tab go, or why it is read-only.
type dbEditTarget struct {
	loading       bool
	why           string // not "": read-only, in a few words for row 2
	detail        string // why, as a sentence for the toast
	conn, driver  string
	sessKey       string
	schema, table string
	key           []int        // result columns of the key
	editable      map[int]bool // result columns that are table columns
}

// dbEditPos is the row and column under the cursor: a loaded row (orig)
// or a new row (added); the other is -1.
type dbEditPos struct{ orig, added, col int }

// dbResolveKey finds the key columns in the result: the primary key, else
// the first unique index whose columns are all there.
func dbResolveKey(cols []dbclient.Column, keys []string, unique [][]string) ([]int, string, string) {
	candidates := [][]string{}
	if len(keys) > 0 {
		candidates = append(candidates, keys)
	}
	candidates = append(candidates, unique...)
	if len(candidates) == 0 {
		return nil, "read-only: no key", "The table has no primary key or unique key."
	}
	for _, cand := range candidates {
		var idx []int
		for _, name := range cand {
			i := dbColumnIndex(cols, name)
			if i < 0 {
				idx = nil
				break
			}
			idx = append(idx, i)
		}
		if len(idx) > 0 {
			return idx, "", ""
		}
	}
	return nil, "read-only: no key column", "Add the key column (" + sanitizeDBText(strings.Join(candidates[0], ", ")) + ") to the query to edit its rows."
}

// dbColumnIndex is the one result column called name (exact, else the
// only one that matches without case); -1 when absent or twice there.
func dbColumnIndex(cols []dbclient.Column, name string) int {
	found := -1
	for i, c := range cols {
		if c.Name == name {
			if found >= 0 {
				return -1
			}
			found = i
		}
	}
	if found >= 0 {
		return found
	}
	for i, c := range cols {
		if strings.EqualFold(c.Name, name) {
			if found >= 0 {
				return -1
			}
			found = i
		}
	}
	return found
}

// dbUniqueSets are the column lists of the unique indexes.
func dbUniqueSets(ir dbclient.IntrospectionResult) [][]string {
	var out [][]string
	for _, idx := range ir.Indexes {
		if idx.Unique && len(idx.Columns) > 0 {
			out = append(out, idx.Columns)
		}
	}
	return out
}

// databaseEditTarget is the target of tab t; a console result looks its
// table's key up first (cmd, target.loading).
func (m *Model) databaseEditTarget(t *dbTab) (*dbEditTarget, tea.Cmd) {
	if t.target != nil {
		return t.target, nil
	}
	r := t.run
	ro := func(why, detail string) (*dbEditTarget, tea.Cmd) {
		t.target = &dbEditTarget{why: why, detail: detail}
		return t.target, nil
	}
	if r == nil || r.result.Err != nil || len(r.result.Columns) == 0 {
		return ro("read-only", "There are no rows to edit.")
	}
	c, ok := m.databases.connection(r.conn)
	switch {
	case !ok || dbclient.DialectFor(c.Driver) == nil:
		return ro("read-only", "This connection does not use SQL.")
	case c.ReadOnly:
		return ro("read-only connection", "The connection is set to read-only.")
	case r.by == "ai":
		return ro("read-only: AI result", "Run the query in your console to edit its rows.")
	case r.describe != nil:
		return ro("read-only", "A table description cannot be edited.")
	}
	cols := r.result.Columns
	if tv := r.tv; tv != nil {
		if tv.view {
			return ro("read-only: a view", "Views are edited through their tables.")
		}
		key, why, detail := dbResolveKey(cols, tv.keys, tv.unique)
		if why != "" {
			return ro(why, detail)
		}
		tg := &dbEditTarget{conn: tv.conn, driver: tv.driver, sessKey: tv.sessKey, schema: tv.schema, table: tv.name, key: key, editable: map[int]bool{}}
		for i, col := range cols {
			if dbColumnIndex(cols, col.Name) == i {
				tg.editable[i] = true
			}
		}
		t.target = tg
		return tg, nil
	}
	sel, why := dbclient.SimpleSelect(dbStripComments(r.sql), c.Driver)
	if why != "" {
		return ro("read-only query", strings.ToUpper(why[:1])+why[1:]+".")
	}
	key := r.sessKey
	if key == "" {
		key = r.conn
	}
	sess := m.databases.sessions[key]
	if sess == nil {
		return ro("read-only", "Connect first: Enter on the connection.")
	}
	t.target = &dbEditTarget{loading: true}
	run := r
	req := dbclient.IntrospectionRequest{Schema: sel.Schema, Object: sel.Table, Kind: "table"}
	return t.target, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		ir, err := sess.Introspect(ctx, req)
		return dbEditKeysMsg{tab: t, run: run, conn: c, sel: sel, sessKey: key, ir: ir, err: err}
	}
}

type dbEditKeysMsg struct {
	tab     *dbTab
	run     *dbRun
	conn    dbclient.Conn
	sel     dbclient.SelectTarget
	sessKey string
	ir      dbclient.IntrospectionResult
	err     error
	// the key that asked, done again once the key is known
	key      string
	row, col int
}

func (m *Model) onDBEditKeys(msg dbEditKeysMsg) tea.Cmd {
	if m.databases == nil || msg.tab.run != msg.run || msg.tab.target == nil || !msg.tab.target.loading {
		return nil
	}
	t := msg.tab
	cols := msg.run.result.Columns
	switch {
	case msg.err != nil:
		t.target = &dbEditTarget{why: "read-only", detail: dbErrorText(msg.err)}
	case len(msg.ir.Columns) == 0:
		t.target = &dbEditTarget{why: "read-only: no table", detail: sanitizeDBText(msg.sel.Table) + " is not a table here."}
	default:
		ir := msg.ir
		var keys []string
		keys = append(keys, ir.Keys...)
		for _, col := range ir.Columns {
			if col.PK && !dbContains(keys, col.Name) {
				keys = append(keys, col.Name)
			}
		}
		key, why, detail := dbResolveKey(cols, keys, dbUniqueSets(ir))
		if why != "" {
			t.target = &dbEditTarget{why: why, detail: detail}
			break
		}
		tg := &dbEditTarget{conn: msg.run.conn, driver: msg.conn.Driver, sessKey: msg.sessKey, schema: msg.sel.Schema, table: msg.sel.Table, key: key, editable: map[int]bool{}}
		for i, col := range cols {
			if dbColumnIndex(cols, col.Name) != i {
				continue
			}
			for _, tc := range ir.Columns {
				if tc.Name == col.Name || strings.EqualFold(tc.Name, col.Name) {
					tg.editable[i] = true
				}
			}
		}
		t.target = tg
	}
	s := m.dbConsole()
	if s.curTab() != t {
		return nil
	}
	if s.pendingKey != "" {
		key, row, col := s.pendingKey, s.pendingRow, s.pendingCol
		s.pendingKey = ""
		return m.databaseEditKey(key, row, col)
	}
	s.keepCursor = true
	return m.databaseShowPage()
}

// databaseEditPos maps the cursor (row, col from the page) to a cell.
func (m *Model) databaseEditPos(t *dbTab, row, col int) (dbEditPos, bool) {
	s := m.dbConsole()
	if s.view != dbViewGrid {
		return dbEditPos{}, false
	}
	page := s.pageResult()
	idx := s.pageIdx()
	if s.trans {
		if s.trow >= len(page.Rows) || row < 0 || row >= len(page.Columns) {
			return dbEditPos{}, false
		}
		return dbEditPos{orig: idx[s.trow], added: -1, col: row}, true
	}
	if row < 0 || col < 0 || col >= len(page.Columns) {
		return dbEditPos{}, false
	}
	if row < len(page.Rows) {
		return dbEditPos{orig: idx[row], added: -1, col: col}, true
	}
	if a := row - len(page.Rows); t.edits != nil && a < len(t.edits.added) {
		return dbEditPos{orig: -1, added: a, col: col}, true
	}
	return dbEditPos{}, false
}

// dbCellNow is the value a cell shows: the pending one, else the loaded.
func dbCellNow(t *dbTab, p dbEditPos) (dbclient.Cell, bool) {
	if p.added >= 0 {
		c, ok := t.edits.added[p.added].set[p.col]
		return c, ok
	}
	if t.edits != nil {
		if c, ok := t.edits.cells[dbCellRef{p.orig, p.col}]; ok {
			return c, true
		}
	}
	row := t.run.result.Rows[p.orig]
	if p.col < len(row) {
		return row[p.col], true
	}
	return dbclient.Cell{Null: true}, true
}

// databaseEditKey runs e, E, +, -, u and w on the result page.
func (m *Model) databaseEditKey(key string, row, col int) tea.Cmd {
	s := m.dbConsole()
	t := s.curTab()
	if t == nil || t.run == nil {
		return nil
	}
	if key == "u" {
		return m.databaseRevertKey(t, row, col)
	}
	if key == "w" {
		return m.databaseSubmitCard()
	}
	tg, cmd := m.databaseEditTarget(t)
	if tg.loading {
		s.pendingKey, s.pendingRow, s.pendingCol = key, row, col
		if cmd != nil {
			return tea.Batch(cmd, m.pushToast(toast.Info, "Looking up the table's key…"))
		}
		return nil
	}
	if tg.why != "" {
		s.keepCursor = true
		return tea.Batch(m.databaseShowPage(), m.pushToastDetail(toast.Info, "This result cannot be edited", tg.detail))
	}
	if key == "+" {
		if s.trans {
			return m.pushToastDetail(toast.Info, "Add rows in the grid", "t goes back to the grid.")
		}
		if t.edits == nil {
			t.edits = newDBEdits()
		}
		t.edits.added = append(t.edits.added, &dbNewRow{set: map[int]dbclient.Cell{}})
		t.edits.note = ""
		s.cursorNewRow = true
		return tea.Batch(m.databaseShowPage(), m.pushToastDetail(toast.Info, "New row added", "e sets a value; empty cells get their default."))
	}
	pos, ok := m.databaseEditPos(t, row, col)
	if !ok {
		return m.pushToastDetail(toast.Info, "Move to a row first", "The cursor is not on a cell of the grid.")
	}
	switch key {
	case "-":
		if t.edits == nil {
			t.edits = newDBEdits()
		}
		t.edits.note = ""
		if pos.added >= 0 {
			t.edits.added = append(t.edits.added[:pos.added], t.edits.added[pos.added+1:]...)
		} else if t.edits.deleted[pos.orig] {
			delete(t.edits.deleted, pos.orig)
		} else {
			t.edits.deleted[pos.orig] = true
		}
		s.keepCursor = true
		return m.databaseShowPage()
	case "e", "E":
		if pos.orig >= 0 && t.edits != nil && t.edits.deleted[pos.orig] {
			return m.pushToastDetail(toast.Info, "This row will be deleted", "- again keeps it; then e edits it.")
		}
		if !tg.editable[pos.col] {
			return m.pushToastDetail(toast.Info, "This column cannot be edited", "It is not a column of "+sanitizeDBText(tg.table)+" (or it is in the result twice).")
		}
		cur, has := dbCellNow(t, pos)
		if has && cur.Kind == "binary" {
			return m.pushToastDetail(toast.Info, "Binary values cannot be edited here", "Use an UPDATE in the console.")
		}
		s.editAt, s.editTab = pos, t
		name := dbOneLine(t.run.result.Columns[pos.col].Name)
		text := cur.Text
		if !has {
			text = ""
		} else if cur.Null {
			text = `\N`
		}
		// A long or multi-line value, or one with control bytes (the prompt
		// would show them raw), opens in a buffer instead.
		if key == "E" || strings.IndexFunc(text, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			if m.nvim == nil {
				return nil
			}
			label := "SQL Edit: " + sanitizeDBText(tg.table) + "." + name
			_ = m.nvim.ExecLuaArgs(`_G._termocode_db_edit(...)`, label, strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n"))
			return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Edit the value, then :w", `:w keeps it as a pending change · q leaves without a change · \N alone is NULL`))
		}
		label := name + ` (\N for NULL):`
		if !has {
			label = name + ` (empty: default · \N: NULL):`
		}
		cmd := m.openHTTPPrompt(promptKindDBEditCell, "Edit cell", label, text)
		m.prompt.SetAllowEmpty(true)
		return cmd
	}
	return nil
}

func init() {
	registerNotifyHandler("termocode_db_edit_done", func(m *Model, args []any) tea.Cmd {
		return m.databaseSetCell(notifyArgString(args, 0), true)
	})
}

// databaseSetCell keeps value as the pending value of the cell being
// edited (\N is NULL; on a new row an empty value means the default).
func (m *Model) databaseSetCell(value string, fromBuffer bool) tea.Cmd {
	s := m.dbConsole()
	t, pos := s.editTab, s.editAt
	s.editTab = nil
	if fromBuffer && m.nvim != nil {
		_ = m.nvim.ExecLua(`_G._termocode_db_edit_close()`)
	}
	if t == nil || t.run == nil || s.tabIndex(t) < 0 {
		return nil
	}
	if t.edits == nil {
		t.edits = newDBEdits()
	}
	t.edits.note = ""
	cell := dbclient.Cell{Text: value}
	if value == `\N` {
		cell = dbclient.Cell{Null: true}
	}
	if pos.added >= 0 {
		if pos.added >= len(t.edits.added) {
			return nil
		}
		if value == "" && !fromBuffer {
			delete(t.edits.added[pos.added].set, pos.col)
		} else {
			t.edits.added[pos.added].set[pos.col] = cell
		}
	} else {
		if pos.orig >= len(t.run.result.Rows) {
			return nil
		}
		ref := dbCellRef{pos.orig, pos.col}
		old := dbclient.Cell{Null: true}
		if row := t.run.result.Rows[pos.orig]; pos.col < len(row) {
			old = row[pos.col]
		}
		cell.Kind = old.Kind
		if old.Null == cell.Null && old.Text == cell.Text {
			delete(t.edits.cells, ref) // back to the loaded value
		} else {
			t.edits.cells[ref] = cell
		}
	}
	s.keepCursor = true
	if idx := s.tabIndex(t); idx != s.tab {
		s.useTab(idx)
	}
	return m.databaseShowPage()
}

// databaseRevertKey is u: the changed cell under the cursor, else the
// row's changes, else (a card) all changes of the tab.
func (m *Model) databaseRevertKey(t *dbTab, row, col int) tea.Cmd {
	s := m.dbConsole()
	if !t.dirty() {
		return m.pushToast(toast.Info, "No pending changes")
	}
	t.edits.note = ""
	if pos, ok := m.databaseEditPos(t, row, col); ok {
		s.keepCursor = true
		switch {
		case pos.added >= 0:
			t.edits.added = append(t.edits.added[:pos.added], t.edits.added[pos.added+1:]...)
			return m.databaseShowPage()
		case t.edits.deleted[pos.orig]:
			delete(t.edits.deleted, pos.orig)
			return m.databaseShowPage()
		}
		if _, ok := t.edits.cells[dbCellRef{pos.orig, pos.col}]; ok {
			delete(t.edits.cells, dbCellRef{pos.orig, pos.col})
			return m.databaseShowPage()
		}
		if t.edits.rowChanged(pos.orig) {
			for ref := range t.edits.cells {
				if ref.row == pos.orig {
					delete(t.edits.cells, ref)
				}
			}
			return m.databaseShowPage()
		}
	}
	m.confirm = confirm.New("Revert "+dbPlural(t.edits.count(), "change")+"?", "Every pending change of this result goes back to the loaded value.", []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "revert", Title: "Revert all", Style: confirm.StyleDestructive},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBRevertAll
	return nil
}

// databaseRevertAll drops every pending change of the current tab.
func (m *Model) databaseRevertAll() tea.Cmd {
	s := m.dbConsole()
	t := s.curTab()
	if !t.dirty() {
		return m.pushToast(toast.Info, "No pending changes")
	}
	t.edits = nil
	s.keepCursor = true
	return tea.Batch(m.databaseShowPage(), m.pushToast(toast.Info, "Changes reverted"))
}

// dbComparable reports whether "column = old" finds the row reliably for
// the optimistic check (not for binary, JSON, floating point or, on
// MySQL, date-time text).
func dbComparable(driver string, col dbclient.Column, old dbclient.Cell) bool {
	if old.Null {
		return true
	}
	switch old.Kind {
	case "binary", "json":
		return false
	case "time":
		if driver == "mysql" {
			return false
		}
	}
	t := strings.ToUpper(col.Type)
	for _, bad := range []string{"JSON", "BLOB", "BYTEA", "BINARY", "XML", "FLOAT", "REAL", "DOUBLE", "GEOMETRY", "POINT", "POLYGON", "LINE", "CIRCLE", "BOX", "PATH", "TSVECTOR", "MONEY"} {
		if strings.Contains(t, bad) {
			return false
		}
	}
	return true
}

// databaseEditStatements turns the tab's changes into statements.
func (m *Model) databaseEditStatements(t *dbTab) ([]dbclient.EditStatement, error) {
	tg := t.target
	if tg == nil || tg.why != "" || tg.loading {
		return nil, errors.New("this result cannot be edited")
	}
	res := t.run.result
	keyOf := func(orig int) []dbclient.EditValue {
		var out []dbclient.EditValue
		for _, k := range tg.key {
			c := dbclient.Cell{Null: true}
			if k < len(res.Rows[orig]) {
				c = res.Rows[orig][k]
			}
			out = append(out, dbclient.EditValue{Column: res.Columns[k].Name, Value: c})
		}
		return out
	}
	var edits []dbclient.RowEdit
	var rows []int
	for orig := range t.edits.deleted {
		rows = append(rows, orig)
	}
	sort.Ints(rows)
	for _, orig := range rows {
		edits = append(edits, dbclient.RowEdit{Kind: dbclient.EditDelete, Key: keyOf(orig)})
	}
	changed := map[int][]int{}
	for ref := range t.edits.cells {
		if !t.edits.deleted[ref.row] {
			changed[ref.row] = append(changed[ref.row], ref.col)
		}
	}
	rows = rows[:0]
	for orig := range changed {
		rows = append(rows, orig)
	}
	sort.Ints(rows)
	for _, orig := range rows {
		cols := changed[orig]
		sort.Ints(cols)
		e := dbclient.RowEdit{Kind: dbclient.EditUpdate, Key: keyOf(orig)}
		for _, col := range cols {
			old := dbclient.Cell{Null: true}
			if col < len(res.Rows[orig]) {
				old = res.Rows[orig][col]
			}
			e.Values = append(e.Values, dbclient.EditValue{Column: res.Columns[col].Name, Value: t.edits.cells[dbCellRef{orig, col}], Old: old, Check: dbComparable(tg.driver, res.Columns[col], old)})
		}
		edits = append(edits, e)
	}
	for _, nr := range t.edits.added {
		e := dbclient.RowEdit{Kind: dbclient.EditInsert}
		var cols []int
		for col := range nr.set {
			cols = append(cols, col)
		}
		sort.Ints(cols)
		for _, col := range cols {
			e.Values = append(e.Values, dbclient.EditValue{Column: res.Columns[col].Name, Value: nr.set[col]})
		}
		edits = append(edits, e)
	}
	return dbclient.EditStatements(dbclient.DialectFor(tg.driver), tg.schema, tg.table, edits)
}

// dbPreviewSQL is the statements as people read them (literals inline).
func dbPreviewSQL(stmts []dbclient.EditStatement) string {
	lines := make([]string, len(stmts))
	for i, st := range stmts {
		lines[i] = st.Preview
	}
	return strings.Join(lines, "\n")
}

// databaseSubmitCard is w: the card with the exact SQL. Production gets
// the strong card; a read-only connection never gets this far.
func (m *Model) databaseSubmitCard() tea.Cmd {
	s := m.dbConsole()
	t := s.curTab()
	if !t.dirty() {
		return m.pushToastDetail(toast.Info, "No pending changes", "e edits a cell · + adds a row · - deletes a row.")
	}
	stmts, err := m.databaseEditStatements(t)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot submit", sanitizeDBText(err.Error()))
	}
	tg := t.target
	c, _ := m.databases.connection(tg.conn)
	if c.ReadOnly {
		return m.pushToastDetail(toast.Warn, "Cannot submit", "The connection is read-only.")
	}
	name := sanitizeDBText(dbConnName(c))
	n := len(stmts)
	manual := m.databases.manualMode(tg.conn)
	title := "Submit " + dbPlural(n, "change") + " to " + name + " (one transaction)"
	note := dbPlural(n, "row") + " will change. Rolled back if any statement fails."
	if manual {
		title = "Submit " + dbPlural(n, "change") + " to " + name + " (open transaction)"
		note = dbPlural(n, "row") + " will change inside the open transaction; Commit (K) saves them. Undone if any statement fails."
	}
	buttons := []confirm.Button{
		{ID: "submit", Title: "Submit", Style: confirm.StylePrimary},
		{ID: "copy", Title: "Copy SQL"},
		{ID: "cancel", Title: "Cancel"},
	}
	if c.Environment == "production" {
		title = "Write to production (" + name + ")? " + dbPlural(n, "change")
		buttons = []confirm.Button{
			{ID: "cancel", Title: "Cancel"},
			{ID: "copy", Title: "Copy SQL"},
			{ID: "submit", Title: "Submit", Style: confirm.StyleDestructive},
		}
	}
	s.submitTab, s.submitStmts = t, stmts
	m.confirm = confirm.New(title, dbSafeMultiline(dbPreviewSQL(stmts))+"\n\n"+note, buttons).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBSubmit
	return nil
}

type dbSubmitMsg struct {
	tab   *dbTab
	run   *dbRun
	stmts []dbclient.EditStatement
	out   dbclient.EditOutcome
	began bool
	key   string
}

// databaseSubmit runs the statements the card showed, off the UI
// goroutine: one transaction (auto-commit), or inside the open one with a
// savepoint (manual mode; a new one begins when none is open).
func (m *Model) databaseSubmit(t *dbTab, stmts []dbclient.EditStatement) tea.Cmd {
	s := m.dbConsole()
	if s.tabIndex(t) < 0 || t.run == nil || !t.dirty() || t.target == nil {
		return nil
	}
	if s.running != nil || s.submitting != nil || m.dbSessionBusy() {
		return m.pushToastDetail(toast.Warn, "A query is running", "Esc cancels it.")
	}
	tg := t.target
	sess := m.databases.sessions[tg.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	manual := m.databases.manualMode(tg.conn)
	open := m.databases.txOpen(tg.sessKey)
	s.submitting = t
	run := t.run
	timeout := loadDBConsoleSettings().timeout()
	return func() tea.Msg {
		msg := dbSubmitMsg{tab: t, run: run, stmts: stmts, key: tg.sessKey}
		if manual && !open {
			// The transaction outlives this command: never a context that ends.
			if err := sess.Begin(context.Background()); err != nil {
				msg.out = dbclient.EditOutcome{Failed: -1, Err: err}
				return msg
			}
			msg.began = true
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		msg.out = dbclient.ApplyEdits(ctx, sess, stmts, manual)
		return msg
	}
}

func (m *Model) onDBSubmit(msg dbSubmitMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	s := m.dbConsole()
	if s.submitting == msg.tab {
		s.submitting = nil
	}
	if msg.began {
		m.databases.tx(msg.key).open = true
	}
	t := msg.tab
	if msg.out.Err != nil {
		if t.edits != nil {
			t.edits.note = "not saved"
		}
		reason := dbErrorText(msg.out.Err)
		var ce *dbclient.EditCountError
		if errors.As(msg.out.Err, &ce) && s.tabIndex(t) >= 0 {
			s.staleTab = t
			m.confirm = confirm.New("The row changed on the server",
				fmt.Sprintf("Statement %d of %d: %s\n\nReload drops your changes and shows the rows as they are now.", msg.out.Failed+1, len(msg.stmts), sanitizeDBText(msg.out.Err.Error())),
				[]confirm.Button{{ID: "keep", Title: "Keep my changes"}, {ID: "reload", Title: "Reload", Style: confirm.StyleDestructive}})
			m.confirm.SetSize(m.w, m.h)
			m.confirmOpen = true
			m.confirmKind = confirmKindDBStale
			s.keepCursor = true
			return m.databaseShowPageIfTab(t)
		}
		title := "Not saved"
		if msg.out.Failed >= 0 {
			title = fmt.Sprintf("Not saved: statement %d of %d failed", msg.out.Failed+1, len(msg.stmts))
		}
		s.keepCursor = true
		return tea.Batch(m.databaseShowPageIfTab(t), m.pushToastDetail(toast.Errr, title, reason+" · Nothing was saved."))
	}
	manual := m.databases.manualMode(dbTxConn(msg.key))
	if manual {
		m.databases.tx(msg.key).writes += len(msg.stmts)
		m.databases.touch()
	}
	notes := []string{"Saved " + dbPlural(len(msg.stmts), "change") + fmt.Sprintf(" · %d ms", msg.out.Elapsed.Milliseconds())}
	for i, st := range msg.stmts {
		n := int64(0)
		if i < len(msg.out.Affected) {
			n = msg.out.Affected[i]
		}
		notes = append(notes, dbOneLine(st.Preview)+" · "+dbPlural(int(n), "row"))
	}
	if manual {
		notes = append(notes, "In the open transaction: Commit (K) saves, Rollback (Z) undoes.")
	}
	toastTitle := "Saved " + dbPlural(len(msg.stmts), "change")
	toastDetail := ""
	if manual {
		toastTitle = dbPlural(len(msg.stmts), "change") + " in the open transaction"
		toastDetail = "K commits · Z rolls back"
	}
	if t.run == msg.run {
		dbApplyOptimistic(t)
		t.notes = notes
	}
	t.edits = nil
	s.keepCursor = true
	return tea.Batch(m.databaseShowPageIfTab(t), m.databaseRefreshTab(t), m.pushToastDetail(toast.Info, toastTitle, toastDetail))
}

// dbApplyOptimistic shows the submitted changes at once (the refresh
// that follows replaces them with what the server holds).
func dbApplyOptimistic(t *dbTab) {
	e := t.edits
	if e == nil {
		return
	}
	res := &t.run.result
	rows := make([][]dbclient.Cell, 0, len(res.Rows)+len(e.added))
	for i, row := range res.Rows {
		if e.deleted[i] {
			continue
		}
		row = append([]dbclient.Cell(nil), row...)
		for ref, c := range e.cells {
			if ref.row == i && ref.col < len(row) {
				row[ref.col] = c
			}
		}
		rows = append(rows, row)
	}
	for _, nr := range e.added {
		row := make([]dbclient.Cell, len(res.Columns))
		for i := range row {
			row[i] = dbclient.Cell{Text: "<default>", Kind: "default"}
			if c, ok := nr.set[i]; ok {
				row[i] = c
			}
		}
		rows = append(rows, row)
	}
	res.Rows = rows
}

// databaseShowPageIfTab redraws the page when t is on it.
func (m *Model) databaseShowPageIfTab(t *dbTab) tea.Cmd {
	s := m.dbConsole()
	if s.curTab() == t && s.running == nil {
		return m.databaseShowPage()
	}
	return nil
}

type dbRefreshMsg struct {
	tab    *dbTab
	run    *dbRun
	result dbclient.Result
}

// databaseRefreshTab reads tab t's rows again in the background and puts
// them on the page in place (no "Running…"): the data view's page, or
// the console's SELECT. Pending edits made meanwhile win over it.
func (m *Model) databaseRefreshTab(t *dbTab) tea.Cmd {
	r := t.run
	if r == nil || r.result.Err != nil {
		return nil
	}
	key := r.sessKey
	if key == "" {
		key = r.conn
	}
	sess := m.databases.sessions[key]
	if sess == nil {
		return nil
	}
	sql := r.sql
	limit := loadDBConsoleSettings().maxRows()
	if r.maxRows > 0 {
		limit = r.maxRows
	}
	if r.tv != nil {
		var err error
		if sql, err = r.tv.selectSQL(max(r.tv.size, 1) + 1); err != nil {
			return nil
		}
	} else if c, ok := m.databases.connection(r.conn); !ok {
		return nil
	} else if kind, _ := dbclient.Classify(sql, c.Driver); kind != dbclient.KindRead {
		return nil
	}
	inTx := m.databases.txOpen(key)
	readOnly := r.readOnly
	timeout := loadDBConsoleSettings().timeout()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var res dbclient.Result
		switch {
		case inTx:
			res = dbReadInTx(ctx, sess, sql, dbclient.RunOptions{MaxRows: limit})
		case readOnly:
			res = dbRunReadOnly(ctx, sess, sql, dbclient.RunOptions{MaxRows: limit})
		default:
			res = sess.Run(ctx, sql, dbclient.RunOptions{MaxRows: limit})
		}
		return dbRefreshMsg{tab: t, run: r, result: res}
	}
}

func (m *Model) onDBRefresh(msg dbRefreshMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	t := msg.tab
	s := m.dbConsole()
	if t.run != msg.run || t.dirty() || s.tabIndex(t) < 0 {
		return nil // replaced, or edited again: the page keeps what it shows
	}
	if msg.result.Err != nil {
		return m.pushToastDetail(toast.Warn, "Could not reload the rows", dbErrorText(msg.result.Err))
	}
	res := msg.result
	if tv := t.run.tv; tv != nil {
		tv.finish(&res, max(tv.size, 1))
	}
	res.Notices = append(append([]string(nil), t.notes...), res.Notices...)
	t.notes = nil // said once, for this load
	res.Elapsed = t.run.result.Elapsed
	t.run.result = res
	if t.target != nil && t.run.tv == nil {
		// A console result keeps its target: same query, same columns.
		if len(res.Columns) != len(msg.run.result.Columns) {
			t.target = nil
		}
	}
	if s.curTab() == t {
		s.cache = nil
		s.keepCursor = true
		if s.running == nil {
			return m.databaseShowPage()
		}
	}
	return nil
}

// databaseUnlessDirty runs action, after a Keep / Discard card when the
// tab on the page has pending changes it would lose.
func (m *Model) databaseUnlessDirty(action func() tea.Cmd) tea.Cmd {
	s := m.dbConsole()
	t := s.curTab()
	if !t.dirty() {
		return action()
	}
	s.afterDiscard = action
	m.confirm = confirm.New("Discard "+dbPlural(t.edits.count(), "pending change")+"?",
		"The rows load again and the changes are lost. w submits them first.", []confirm.Button{
			{ID: "keep", Title: "Keep"},
			{ID: "discard", Title: "Discard", Style: confirm.StyleDestructive},
		})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBDiscard
	return nil
}

// databaseEditConfirm handles the cards of this file and of db_tx.go.
func (m *Model) databaseEditConfirm(id string) (tea.Cmd, bool) {
	s := m.dbConsole()
	switch m.confirmKind {
	case confirmKindDBSubmit:
		t, stmts := s.submitTab, s.submitStmts
		switch id {
		case "submit":
			s.submitTab, s.submitStmts = nil, nil
			return m.databaseSubmit(t, stmts), true
		case "copy":
			cmd := m.copyWithToast(dbRedact(dbPreviewSQL(stmts)), "Copied the SQL")
			m.confirmOpen = true // the card stays: Submit or Cancel next
			return cmd, true
		}
		s.submitTab, s.submitStmts = nil, nil
		return nil, true
	case confirmKindDBDiscard:
		act := s.afterDiscard
		s.afterDiscard = nil
		if id == "discard" && act != nil {
			if t := s.curTab(); t != nil {
				t.edits = nil
			}
			return act(), true
		}
		return nil, true
	case confirmKindDBRevertAll:
		if id == "revert" {
			return m.databaseRevertAll(), true
		}
		return nil, true
	case confirmKindDBStale:
		t := s.staleTab
		s.staleTab = nil
		if id == "reload" && t != nil {
			t.edits = nil
			s.keepCursor = true
			return tea.Batch(m.databaseShowPageIfTab(t), m.databaseRefreshTab(t)), true
		}
		return nil, true
	case confirmKindDBTxEnd:
		return m.databaseTxConfirm(id), true
	case confirmKindDBContinue:
		r := s.contRun
		s.contRun = nil
		if id == "continue" && r != nil {
			return m.databaseReviewRun(r), true
		}
		return nil, true
	}
	return nil, false
}

// databaseEditSummary is what row 2 adds for r: the pending changes, the
// last failure, the transaction state and why the result is read-only;
// brief is the part that matters most, for a narrow page.
func (m *Model) databaseEditSummary(r *dbRun) (extra, brief string) {
	s := m.dbConsole()
	var parts []string
	t := s.curTab()
	if t != nil && t.run != r {
		t = nil
	}
	grid := r.result.Err == nil && len(r.result.Columns) > 0
	if t != nil && grid {
		if n := t.edits.count(); n > 0 {
			parts = append(parts, dbPlural(n, "change"))
		}
		if t.edits != nil && t.edits.note != "" {
			parts = append(parts, dbGlyphFail()+" "+t.edits.note)
		}
	}
	if mk := m.databases.txMarker(r.conn); mk != "" {
		parts = append(parts, mk)
	}
	if t != nil && grid {
		tg := t.target
		if tg == nil && r.tv != nil {
			tg, _ = m.databaseEditTarget(t) // a data view knows its key: no lookup
		}
		if tg != nil && !tg.loading && tg.why != "" {
			parts = append(parts, tg.why)
		}
	}
	if len(parts) == 0 {
		return "", ""
	}
	return " · " + strings.Join(parts, " · "), " · " + parts[0]
}

// databaseEditable reports a tab whose rows can be edited, as far as is
// known without asking the server.
func (m *Model) databaseEditable(t *dbTab) bool {
	if t == nil || t.run == nil {
		return false
	}
	tg := t.target
	if tg == nil && t.run.tv != nil {
		tg, _ = m.databaseEditTarget(t)
	}
	return tg != nil && !tg.loading && tg.why == ""
}
