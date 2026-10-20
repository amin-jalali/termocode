package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	tea "github.com/charmbracelet/bubbletea"
)

// databaseResultDemo fills the SQL Result page with a canned result for the
// shotkit goldens (dbdemo result / messages / error).
func (m *Model) databaseResultDemo(variant string) tea.Cmd {
	m.databasesDemo("")
	names := []string{"ada", "grace", "linus", "mary", "ken", "ravi", "sue", "olu", "ying", "ben", "zoe", "ivan", "nia", "tom", "amy", "raj", "eve", "max", "lee", "kim", "dan", "ana", "sam", "joy", "uma", "ali", "bea", "cy", "dee", "fay"}
	plans := []string{"free", "pro", "team"}
	res := dbclient.Result{
		Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}, {Name: "plan"}, {Name: "created_at"}},
		Elapsed: 118 * time.Millisecond,
	}
	for i, n := range names {
		plan := dbclient.Cell{Text: plans[i%3]}
		email := dbclient.Cell{Text: n + "@example.com"}
		if i == 2 {
			email = dbclient.Cell{Null: true}
		}
		res.Rows = append(res.Rows, []dbclient.Cell{
			{Text: fmt.Sprint(48213 - i), Kind: "number"}, email, plan,
			{Text: fmt.Sprintf("2026-10-18 09:%02d:%02d+00", 59-i, (i*37)%60)},
		})
	}
	r := &dbRun{conn: "local-pg", sql: "SELECT id, email, plan, created_at\nFROM users\nORDER BY id DESC;", by: "user", line: 3, result: res}
	s := m.dbConsole()
	s.tabs, s.tab = nil, 0
	s.placeRun(r)
	s.cache, s.size, s.page, s.sortCol, s.filter = nil, 100, 0, -1, ""
	s.view = dbViewGrid
	s.trans, s.trow = false, 0
	switch variant {
	case "table", "transposed", "edits", "submit", "import", "export":
		// The data view of public.users: the second page, more to come.
		tv := newDBTableView(dbTable{conn: "local-pg", driver: "postgres", schema: "public", name: "users"})
		tv.keys, tv.cols, tv.offset, tv.more, tv.size = []string{"id"}, []string{"id", "email", "plan", "created_at"}, 100, true, 100
		r.sql, _ = tv.selectSQL(101)
		r.tv, r.line = tv, 0
		if variant == "transposed" {
			s.trans, s.trow = true, 1
		}
	case "describe":
		tbl := dbTable{conn: "local-pg", driver: "postgres", schema: "public", name: "users"}
		r.sql, r.describe, r.line = "Describe "+tbl.label(), &tbl, 0
		r.result = dbDescribeResult(tbl, dbDemoMeta())
	case "ddl":
		tbl := dbTable{conn: "local-pg", driver: "postgres", schema: "public", name: "users"}
		return m.databaseShowDDL(tbl, dbBuildDDL(tbl, dbDemoMeta()))
	}
	if variant == "edits" || variant == "submit" {
		return m.databaseEditsDemo(r, variant == "submit")
	}
	switch variant {
	case "export":
		page := m.databaseShowPage()
		return tea.Batch(page, m.databaseExportStart())
	case "import":
		page := m.databaseShowPage()
		return tea.Batch(page, m.databaseImportDemo())
	}
	switch variant {
	case "messages":
		r.sql = "UPDATE users SET plan = 'pro' WHERE id = 48212;"
		r.result = dbclient.Result{Affected: 1, Elapsed: 9 * time.Millisecond, Notices: []string{"NOTICE: trigger users_audit wrote 1 row"}}
		s.view = dbViewMessages
	case "error":
		r.sql = "SELECT * FROM userz;"
		r.result = dbclient.Result{Err: errors.New(`ERROR: relation "userz" does not exist (SQLSTATE 42P01)`), Elapsed: 3 * time.Millisecond}
		s.view = dbViewMessages
	}
	return m.databaseShowPage()
}

// dbDemoMeta is the canned catalog of public.users.
func dbDemoMeta() dbTableMeta {
	return dbTableMeta{
		cols: []dbColInfo{
			{name: "id", typ: "bigint", notNull: true, def: "nextval('users_id_seq'::regclass)", pk: true},
			{name: "email", typ: "text", notNull: true},
			{name: "plan", typ: "text", def: "'free'::text"},
			{name: "created_at", typ: "timestamp with time zone", notNull: true, def: "now()"},
		},
		ir: dbclient.IntrospectionResult{
			Keys: []string{"id"},
			Indexes: []dbclient.SchemaIndex{
				{Name: "users_pkey", Columns: []string{"id"}, Unique: true},
				{Name: "users_email_key", Columns: []string{"email"}, Unique: true},
				{Name: "users_plan_idx", Columns: []string{"plan", "created_at"}},
			},
			ForeignKeys: []dbclient.SchemaForeignKey{{Name: "users_org_fk", Column: "org_id", RefTable: "orgs", RefColumn: "id"}},
		},
		triggers: [][3]string{{"users_audit", "AFTER", "INSERT OR UPDATE"}},
	}
}

// databaseEditsDemo puts pending changes on the data view (one changed
// cell, a deleted row, a new row), a pinned console result in tab 1 and a
// manual transaction with uncommitted changes; submit opens the card.
func (m *Model) databaseEditsDemo(r *dbRun, submit bool) tea.Cmd {
	s := m.dbConsole()
	pinned := &dbRun{conn: "local-pg", sql: "SELECT count(*) FROM orders;", by: "user", line: 1,
		result: dbclient.Result{Columns: []dbclient.Column{{Name: "count"}}, Rows: [][]dbclient.Cell{{{Text: "1204", Kind: "number"}}}}}
	s.tabs, s.tab = nil, 0
	s.placeRun(pinned)
	s.curTab().pinned = true
	s.placeRun(r)
	s.cache, s.size, s.page, s.sortCol, s.filter, s.view = nil, 100, 0, -1, "", dbViewGrid
	t := s.curTab()
	t.edits = newDBEdits()
	t.edits.cells[dbCellRef{row: 1, col: 2}] = dbclient.Cell{Text: "team"}
	t.edits.cells[dbCellRef{row: 2, col: 1}] = dbclient.Cell{Text: "linus@example.com"}
	t.edits.deleted[4] = true
	t.edits.added = append(t.edits.added, &dbNewRow{set: map[int]dbclient.Cell{1: {Text: "new@example.com"}}})
	d := m.databases
	d.manual = map[string]bool{"local-pg": true}
	d.txs = map[string]*dbTxState{"local-pg": {open: true, writes: 2}}
	d.activeID = "local-pg"
	page := m.databaseShowPage()
	if submit {
		return tea.Batch(page, m.databaseSubmitCard())
	}
	return page
}

// databaseImportDemo opens the import preview card on a canned file: 1 200
// rows of users.csv for public.users (created_at has no file column).
func (m *Model) databaseImportDemo() tea.Cmd {
	s := m.dbConsole()
	t := dbTable{conn: "local-pg", driver: "postgres", schema: "public", name: "users"}
	sn := dbclient.CSVSniff{
		Delimiter: ',', Header: true, Encoding: "UTF-8", Fields: 3,
		Columns: []string{"id", "email", "plan"},
		Sample: [][]string{
			{"48214", "nia@example.com", "free"},
			{"48215", "tom@example.com", "pro"},
			{"48216", "amy@example.com", "team"},
			{"48217", "raj@example.com", "free"},
			{"48218", "eve@example.com", "pro"},
		},
	}
	imp := &dbImport{t: t, conn: dbDemoConnection(), path: "users.csv", display: "users.csv", sniff: sn, records: 1201,
		cols: []string{"id", "email", "plan", "created_at"}, mapping: []int{0, 1, 2, -1}, sessKey: "local-pg"}
	s.imp = imp
	return m.databaseImportCard()
}
