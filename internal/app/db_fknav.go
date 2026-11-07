package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Foreign key navigation on the result page (DBX-7). In the data view of
// a table: gf on an FK cell opens the row it points to, gb goes back to
// where the jump started, gr lists the tables whose rows point at the row
// under the cursor and opens them, and gl picks a value for an FK cell
// from the referenced column (a pending change, like e). The SQL comes
// from dbclient (fknav.go); a jump opens the other table's data view with
// a WHERE built like the quick filter, so it pages, sorts and edits as a
// view opened from the tree.

const (
	pickerKindDBFKRefs   pickerKindEnum = 5137
	pickerKindDBFKLookup pickerKindEnum = 5138
)

const (
	dbFKBackMax     = 64  // the oldest jump is dropped
	dbFKLookupLimit = 500 // values gl offers
)

// dbFKBack is where a jump started: the data view and its row.
type dbFKBack struct {
	tv  *dbTableView
	row int
}

// dbFKState is the jump history and the open pickers of FK navigation.
type dbFKState struct {
	back    []dbFKBack
	refs    []dbFKRef // the gr picker's choices
	refFrom *dbTableView
	refRow  int
	lookup  *dbFKLookup
}

// dbFKRef is one choice of the gr picker: rows of table where cols match.
type dbFKRef struct {
	table string
	where string
	label string
}

// dbFKLookup is an open gl picker: the cell it fills and its values.
type dbFKLookup struct {
	tab    *dbTab
	pos    dbEditPos
	values []dbclient.Cell
}

func init() {
	registerNotifyHandler("termocode_db_fk", func(m *Model, args []any) tea.Cmd {
		return m.databaseFKKey(notifyArgString(args, 0), notifyArgInt(args, 1), notifyArgInt(args, 2))
	})
}

// dbFKPushBack records a jump; the trail keeps the last dbFKBackMax.
func (st *dbFKState) pushBack(b dbFKBack) {
	st.back = append(st.back, b)
	if len(st.back) > dbFKBackMax {
		st.back = append(st.back[:0], st.back[len(st.back)-dbFKBackMax:]...)
	}
}

// dbFKWhere is the WHERE that finds the rows whose cols hold vals (each
// value as the quick filter writes it; NULL never matches a key).
func dbFKWhere(driver string, cols []string, vals []dbclient.Cell) (string, error) {
	if len(cols) == 0 || len(cols) != len(vals) {
		return "", fmt.Errorf("no foreign key to follow")
	}
	where := ""
	for i, c := range cols {
		if vals[i].Null {
			return "", fmt.Errorf("%s is NULL: it points to no row", c)
		}
		w, err := dbQuickWhere(driver, c, vals[i], "")
		if err != nil {
			return "", err
		}
		if where != "" {
			where += " AND "
		}
		where += w
	}
	return where, nil
}

// dbSiblingTable is the table name in the same schema as t (the tree path
// of a sibling under Tables).
func dbSiblingTable(t dbTable, name string) dbTable {
	out := dbTable{conn: t.conn, driver: t.driver, sessKey: t.sessKey, schema: t.schema, name: name}
	if i := strings.LastIndex(t.path, "/"); i >= 0 {
		if j := strings.LastIndex(t.path[:i], "/"); j >= 0 {
			out.path = t.path[:j] + "/Tables/" + name
		}
	}
	return out
}

// dbCanonFKs lists the foreign keys of every table in cs, keyed by table,
// in the form IncomingFKs reads (an unnamed key gets a name so its
// columns stay one key).
func dbCanonFKs(cs dbclient.CanonSchema) map[string][]dbclient.SchemaForeignKey {
	out := map[string][]dbclient.SchemaForeignKey{}
	for _, t := range cs.Tables {
		for n, c := range t.Constraints {
			if c.Kind != "FOREIGN KEY" || len(c.Columns) != len(c.RefColumns) {
				continue
			}
			name := c.Name
			if name == "" {
				name = fmt.Sprintf("%s#%d", t.Name, n)
			}
			for i, col := range c.Columns {
				out[t.Name] = append(out[t.Name], dbclient.SchemaForeignKey{Name: name, Column: col, RefTable: c.RefTable, RefColumn: c.RefColumns[i], Virtual: c.Virtual})
			}
		}
	}
	return out
}

// dbRowCells is the row's cells by column name.
func dbRowCells(cols []dbclient.Column, row []dbclient.Cell) map[string]dbclient.Cell {
	out := make(map[string]dbclient.Cell, len(cols))
	for i, c := range cols {
		if i < len(row) {
			out[c.Name] = row[i]
		}
	}
	return out
}

// databaseFKCell is the data view, the loaded row and the column under the
// cursor (row and col as the page sends them, col a result column).
func (m *Model) databaseFKCell(row, col int) (*dbTableView, *dbTab, dbEditPos, tea.Cmd, bool) {
	s := m.dbConsole()
	r := s.shown
	if r == nil || r.tv == nil || r.plan != nil || r.redis != nil || r.result.Err != nil {
		return nil, nil, dbEditPos{}, m.pushToast(toast.Info, "Foreign keys work in table data: open a table with o"), false
	}
	t := s.curTab()
	if t == nil || t.run != r {
		return nil, nil, dbEditPos{}, nil, false
	}
	pos, ok := m.databaseEditPos(t, row, col)
	if !ok || pos.col < 0 || pos.col >= len(r.result.Columns) {
		return nil, nil, dbEditPos{}, m.pushToastDetail(toast.Info, "Move to a cell first", "The cursor is not on a cell of the grid."), false
	}
	return r.tv, t, pos, nil, true
}

// databaseFKKey runs gf, gb, gr and gl.
func (m *Model) databaseFKKey(key string, row, col int) tea.Cmd {
	s := m.dbConsole()
	if s.running != nil || s.submitting != nil {
		return nil
	}
	if s.shown != nil && s.shown.redis == nil && s.shown.plan == nil && s.view == dbViewGrid && !s.trans {
		col = s.gridColumn(col) // the grid shows the columns in the layout's order
	}
	switch key {
	case "gb":
		return m.databaseFKBack()
	case "gf":
		return m.databaseFKFollow(row, col)
	case "gr":
		return m.databaseFKReferencing(row, col)
	case "gl":
		return m.databaseFKLookupStart(row, col)
	}
	return nil
}

func (m *Model) databaseFKFollow(row, col int) tea.Cmd {
	tv, _, pos, cmd, ok := m.databaseFKCell(row, col)
	if !ok {
		return cmd
	}
	if pos.orig < 0 {
		return m.pushToastDetail(toast.Info, "Submit the new row first", "A row that is not saved points nowhere yet.")
	}
	r := m.dbConsole().shown
	name := r.result.Columns[pos.col].Name
	key, found := dbclient.FKForColumn(tv.fks, name)
	if !found {
		return m.pushToastDetail(toast.Info, "Not a foreign key", sanitizeDBText(name)+" points to no other table. gr lists the rows that point here.")
	}
	cells := dbRowCells(r.result.Columns, r.result.Rows[pos.orig])
	cols, vals := make([]string, len(key)), make([]dbclient.Cell, len(key))
	for i, fk := range key {
		c, ok := cells[fk.Column]
		if !ok {
			return m.pushToastDetail(toast.Info, "The key is not all here", "Show the column "+sanitizeDBText(fk.Column)+" to follow this key.")
		}
		cols[i], vals[i] = fk.RefColumn, c
	}
	where, err := dbFKWhere(tv.driver, cols, vals)
	if err != nil {
		return m.pushToastDetail(toast.Info, "Nothing to follow", sanitizeDBText(err.Error()))
	}
	to := dbSiblingTable(tv.dbTable, key[0].RefTable)
	return m.databaseUnlessDirty(func() tea.Cmd { return m.databaseFKOpen(tv, pos.orig, to, where) })
}

// databaseFKOpen opens the data view of to with where, and remembers
// where the jump started.
func (m *Model) databaseFKOpen(from *dbTableView, row int, to dbTable, where string) tea.Cmd {
	s := m.dbConsole()
	if m.databases.sessions[to.sessKey] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	if err := dbWhereCheck(to.driver, where); err != nil {
		return m.pushToastDetail(toast.Warn, "Filter refused", sanitizeDBText(err.Error()))
	}
	s.fk.pushBack(dbFKBack{tv: from, row: row})
	tv := newDBTableView(to)
	tv.where = where
	sess := m.databases.sessions[to.sessKey]
	gen := m.nextTableGen()
	return tea.Batch(m.pushToastDetail(toast.Info, "Opened "+to.label(), "gb goes back."), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		ir, err := sess.Introspect(ctx, to.request())
		return dbTableMetaMsg{gen: gen, tv: tv, meta: dbTableMeta{ir: ir}, err: err}
	})
}

// databaseFKBack shows the data view the last jump started from again.
func (m *Model) databaseFKBack() tea.Cmd {
	s := m.dbConsole()
	n := len(s.fk.back)
	if n == 0 {
		return m.pushToastDetail(toast.Info, "No jump to go back from", "gf on a foreign key cell jumps to the row it points to.")
	}
	b := s.fk.back[n-1]
	return m.databaseUnlessDirty(func() tea.Cmd {
		s.fk.back = s.fk.back[:n-1]
		if m.databases.sessions[b.tv.sessKey] == nil {
			return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
		}
		return m.databaseRunTable(b.tv)
	})
}

// databaseFKReferencing reads the schema's foreign keys (off the UI
// goroutine) and lists the tables that point at the row under the cursor.
func (m *Model) databaseFKReferencing(row, col int) tea.Cmd {
	tv, _, pos, cmd, ok := m.databaseFKCell(row, col)
	if !ok {
		return cmd
	}
	if pos.orig < 0 {
		return m.pushToastDetail(toast.Info, "Submit the new row first", "No row points at a row that is not saved.")
	}
	sess := m.databases.sessions[tv.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	r := m.dbConsole().shown
	cells := dbRowCells(r.result.Columns, r.result.Rows[pos.orig])
	schema := dbERSchemaOf(m.databases.nodes, tv.path, tv.driver)
	if schema == "" {
		schema = tv.schema
	}
	gen := m.nextTableGen()
	from, orig := tv, pos.orig
	return tea.Batch(m.pushToast(toast.Info, "Looking for rows that point here…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		var cs dbclient.CanonSchema
		ro, err := sess.ReadOnlySession()
		if err == nil {
			cs, err = dbclient.ReadCanonSchema(ctx, ro, from.driver, schema)
			ro.Close()
		}
		return applyMsg(func(m *Model) tea.Cmd { return m.onDBFKReferencing(gen, from, orig, cells, cs, err) })
	})
}

// dbFKRefChoices are the gr choices: each key of another table that points
// at table, with the WHERE that finds the rows of this row.
func dbFKRefChoices(driver, table string, all map[string][]dbclient.SchemaForeignKey, cells map[string]dbclient.Cell) []dbFKRef {
	var out []dbFKRef
	for _, in := range dbclient.IncomingFKs(all, table) {
		cols, vals, refs := make([]string, len(in.Key)), make([]dbclient.Cell, len(in.Key)), make([]string, len(in.Key))
		ok := true
		for i, fk := range in.Key {
			c, has := cells[fk.RefColumn]
			if !has {
				ok = false
				break
			}
			cols[i], vals[i], refs[i] = fk.Column, c, fk.RefColumn
		}
		if !ok {
			continue
		}
		where, err := dbFKWhere(driver, cols, vals)
		if err != nil {
			continue
		}
		label := sanitizeDBText(in.Table) + " (" + sanitizeDBText(strings.Join(cols, ", ")) + " → " + sanitizeDBText(strings.Join(refs, ", ")) + ")"
		if in.Key[0].Virtual {
			label += " · virtual" // from the virtual objects file (DBX-31)
		}
		out = append(out, dbFKRef{table: in.Table, where: where, label: label})
	}
	return out
}

func (m *Model) onDBFKReferencing(gen uint64, from *dbTableView, orig int, cells map[string]dbclient.Cell, cs dbclient.CanonSchema, err error) tea.Cmd {
	if m.databases == nil || m.dbConsole().tableGen != gen {
		return nil
	}
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Could not read the foreign keys", dbclient.Redact(sanitizeDBText(err.Error())))
	}
	v, _ := m.databases.dbVirtual(from.conn) // virtual keys point here too (db_virtual.go)
	refs := dbFKRefChoices(from.driver, from.name, dbCanonFKs(v.WithForeignKeys(cs)), cells)
	if len(refs) == 0 {
		return m.pushToastDetail(toast.Info, "No table points here", "No foreign key of this schema references "+from.label()+".")
	}
	s := m.dbConsole()
	s.fk.refs, s.fk.refFrom, s.fk.refRow = refs, from, orig
	items := make([]picker.Item, len(refs))
	for i, r := range refs {
		items[i] = picker.Item{ID: fmt.Sprint(i), Title: r.label}
	}
	m.picker = picker.NewItems("Rows that point here (select: open)", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBFKRefs
	m.pickerOpen = true
	return nil
}

// databaseFKLookupStart lists the values the FK cell under the cursor may
// take (gl).
func (m *Model) databaseFKLookupStart(row, col int) tea.Cmd {
	tv, t, pos, cmd, ok := m.databaseFKCell(row, col)
	if !ok {
		return cmd
	}
	tg, cmd := m.databaseEditTarget(t)
	if tg.loading {
		return tea.Batch(cmd, m.pushToastDetail(toast.Info, "Looking up the table's key…", "Press gl again in a moment."))
	}
	if tg.why != "" {
		return m.pushToastDetail(toast.Info, "This result cannot be edited", tg.detail)
	}
	name := t.run.result.Columns[pos.col].Name
	key, found := dbclient.FKForColumn(tv.fks, name)
	if !found {
		return m.pushToastDetail(toast.Info, "Not a foreign key", sanitizeDBText(name)+" takes any value: e edits it.")
	}
	q, err := dbclient.FKLookupQuery(dbclient.DialectFor(tv.driver), tv.schema, key, dbFKLookupLimit)
	if err != nil {
		return m.pushToastDetail(toast.Info, "No values to pick", sanitizeDBText(err.Error()))
	}
	sess := m.databases.sessions[tv.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	gen := m.nextTableGen()
	inTx := m.databases.txOpen(tv.sessKey)
	lk := &dbFKLookup{tab: t, pos: pos}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadDBConsoleSettings().timeout())
		defer cancel()
		opt := dbclient.RunOptions{MaxRows: dbFKLookupLimit, Args: q.Args}
		var res dbclient.Result
		if inTx {
			res = dbReadInTx(ctx, sess, q.SQL, opt)
		} else {
			res = dbRunReadOnly(ctx, sess, q.SQL, opt)
		}
		return applyMsg(func(m *Model) tea.Cmd { return m.onDBFKLookup(gen, lk, res) })
	}
}

func (m *Model) onDBFKLookup(gen uint64, lk *dbFKLookup, res dbclient.Result) tea.Cmd {
	if m.databases == nil || m.dbConsole().tableGen != gen {
		return nil
	}
	if res.Err != nil {
		return m.pushToastDetail(toast.Errr, "Could not list the values", dbclient.Redact(sanitizeDBText(res.Err.Error())))
	}
	for _, row := range res.Rows {
		if len(row) > 0 && !row[0].Null {
			lk.values = append(lk.values, row[0])
		}
	}
	if len(lk.values) == 0 {
		return m.pushToastDetail(toast.Info, "No values to pick", "The referenced table has no rows.")
	}
	cur, _ := dbCellNow(lk.tab, lk.pos)
	items := make([]picker.Item, len(lk.values))
	for i, v := range lk.values {
		hint := ""
		if !cur.Null && cur.Text == v.Text {
			hint = "current"
		}
		items[i] = picker.Item{ID: fmt.Sprint(i), Title: dbOneLine(v.Text), Hint: hint}
	}
	m.dbConsole().fk.lookup = lk
	title := "Value for " + dbOneLine(lk.tab.run.result.Columns[lk.pos.col].Name)
	if len(lk.values) >= dbFKLookupLimit {
		title += fmt.Sprintf(" (first %d)", dbFKLookupLimit)
	}
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBFKLookup
	m.pickerOpen = true
	return nil
}

// databaseFKPicker handles the gr and gl pickers.
func (m *Model) databaseFKPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBFKRefs && m.pickerKind != pickerKindDBFKLookup {
		return nil, false
	}
	s := m.dbConsole()
	var i int
	if _, err := fmt.Sscan(id, &i); err != nil {
		return nil, true
	}
	if m.pickerKind == pickerKindDBFKLookup {
		lk := s.fk.lookup
		s.fk.lookup = nil
		if lk == nil || i < 0 || i >= len(lk.values) || s.curTab() != lk.tab {
			return nil, true
		}
		v := lk.values[i]
		if !dbSetCellValue(lk.tab, lk.pos, v.Text, false) {
			return nil, true
		}
		s.keepCursor = true
		return tea.Batch(m.databaseShowPage(), m.pushToastDetail(toast.Info, "Value set", "w shows the SQL to save it; u undoes.")), true
	}
	refs, from, row := s.fk.refs, s.fk.refFrom, s.fk.refRow
	s.fk.refs, s.fk.refFrom = nil, nil
	if from == nil || i < 0 || i >= len(refs) {
		return nil, true
	}
	ref := refs[i]
	return m.databaseUnlessDirty(func() tea.Cmd {
		return m.databaseFKOpen(from, row, dbSiblingTable(from.dbTable, ref.table), ref.where)
	}), true
}
