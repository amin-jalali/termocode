package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dbeKey runs an edit key of the page on row, col and feeds its answer back.
func dbeKey(m *Model, key string, row, col int) {
	dbePump(m, m.databaseEditKey(key, row, col))
}

// The edit keys of a data view: e with its prompt, D, U, - and + on loaded
// and new rows, and what they refuse.
func TestCovDBEditKeys(t *testing.T) {
	m, _ := dbeModel(t)
	if m.databaseEditKey("e", 0, 0) != nil {
		t.Fatal("a key with no result")
	}
	dbeOpen(t, m, "files")
	s := m.dbConsole()
	tab := s.curTab()

	dbeKey(m, "e", 0, 1)
	dbeToastHas(t, m, "Binary values cannot be edited here")
	dbeKey(m, "e", 9, 1)
	dbeToastHas(t, m, "Move to a row first")
	// e on a NULL cell offers \N.
	dbeKey(m, "e", 1, 1)
	if !m.promptOpen || m.prompt.Value() != `\N` {
		t.Fatalf("NULL prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.promptOpen = false
	dbeKey(m, "e", 0, 3)
	dbePrompt(t, m, "x")
	dbeToastHas(t, m, "n is int4: type a whole number")
	dbeKey(m, "e", 0, 3)
	dbePrompt(t, m, "7")
	if c, _ := dbCellNow(tab, dbEditPos{orig: 0, added: -1, col: 3}); c.Text != "7" {
		t.Fatalf("cell %+v", c)
	}
	// E with no editor does nothing.
	if m.databaseEditKey("E", 0, 2) != nil {
		t.Fatal("E with no editor")
	}

	dbeKey(m, "D", 0, 2)
	dbeToastHas(t, m, "Set to DEFAULT")
	if c, _ := dbCellNow(tab, dbEditPos{orig: 0, added: -1, col: 2}); c.Kind != dbDefaultKind {
		t.Fatalf("default %+v", c)
	}
	dbeKey(m, "U", 1, 2)
	dbeToastHas(t, m, "New UUID")

	// - marks a row deleted, - again keeps it; a deleted row takes no e,
	// D or U.
	dbeKey(m, "-", 1, 0)
	if !tab.edits.deleted[1] {
		t.Fatal("not deleted")
	}
	dbeKey(m, "e", 1, 2)
	dbeToastHas(t, m, "This row will be deleted")
	if m.databaseSpecialKey("D", tab, tab.target, []dbEditPos{{orig: 1, added: -1, col: 2}}) != nil {
		t.Fatal("D on a deleted row")
	}
	dbeKey(m, "-", 1, 0)
	if tab.edits.deleted[1] {
		t.Fatal("still deleted")
	}

	// + adds a row; D on it unsets the cell, - drops it.
	dbeKey(m, "+", 0, 0)
	if len(tab.edits.added) != 1 {
		t.Fatal("no new row")
	}
	dbeKey(m, "e", 2, 2)
	if !m.promptOpen || !strings.Contains(m.prompt.View(), "empty: default") {
		t.Fatal("no new-row prompt")
	}
	dbePrompt(t, m, "fresh")
	dbeKey(m, "D", 2, 2)
	if _, ok := tab.edits.added[0].set[2]; ok {
		t.Fatal("DEFAULT on a new row kept a value")
	}
	dbeKey(m, "-", 2, 0)
	if len(tab.edits.added) != 0 {
		t.Fatal("the new row stayed")
	}

	// A column that is not the table's.
	tab.target.editable[2] = false
	dbeKey(m, "e", 0, 2)
	dbeToastHas(t, m, "This column cannot be edited")
	dbeKey(m, "U", 0, 2)
	dbeToastHas(t, m, "This column cannot be edited")
	tab.target.editable[2] = true
	if m.databaseSpecialKey("D", tab, tab.target, nil) == nil {
		t.Fatal("no cells")
	}
	dbeToastHas(t, m, "Move to a row first")
	// Many rows at once.
	m.databaseSpecialKey("U", tab, tab.target, []dbEditPos{{orig: 0, added: -1, col: 2}, {orig: 1, added: -1, col: 2}})
	dbeToastHas(t, m, "New UUID in 2 rows")

	// The record view: + shows the new row, P is for the grid.
	s.trans, s.trow = true, 0
	dbeKey(m, "+", 0, 0)
	if s.trow != 2 {
		t.Fatalf("record view row %d", s.trow)
	}
	dbeToastHas(t, m, "e on a line sets that value")
	dbeKey(m, "P", 0, 0)
	dbeToastHas(t, m, "Paste rows in the grid")
	if pos, ok := m.databaseEditPos(tab, 2, 0); !ok || pos.added != 0 || pos.col != 2 {
		t.Fatalf("record view pos %+v %v", pos, ok)
	}
	if _, ok := m.databaseEditPos(tab, 99, 0); ok {
		t.Fatal("a line past the columns")
	}
	s.trow = 9
	if _, ok := m.databaseEditPos(tab, 1, 0); ok {
		t.Fatal("a record past the rows")
	}
	s.trans = false
	s.view = dbViewMessages
	if _, ok := m.databaseEditPos(tab, 0, 0); ok {
		t.Fatal("a cell of the Messages view")
	}
	s.view = dbViewGrid
}

// D on a SQLite row cannot set the DEFAULT back.
func TestCovDBEditDefaultOnSQLite(t *testing.T) {
	m, _ := dbeModel(t)
	dbeOpen(t, m, "orders")
	tab := m.dbConsole().curTab()
	tg, _ := m.databaseEditTarget(tab)
	tg.driver = "sqlite"
	dbeKey(m, "D", 0, 2)
	dbeToastHas(t, m, "SQLite cannot set a value back to its DEFAULT")
}

// u reverts the cell, then the row, then (a card) everything.
func TestCovDBEditRevert(t *testing.T) {
	m, _ := dbeModel(t)
	dbeOpen(t, m, "orders")
	s := m.dbConsole()
	tab := s.curTab()
	dbeKey(m, "u", 0, 0)
	dbeToastHas(t, m, "No pending changes")
	m.databaseRevertAll()

	set := func(row, col int, v string) {
		dbSetCellValue(tab, dbEditPos{orig: row, added: -1, col: col}, v, false)
	}
	set(0, 2, "a")
	set(0, 1, "6")
	dbeKey(m, "u", 0, 2)
	if _, ok := tab.edits.cells[dbCellRef{0, 2}]; ok {
		t.Fatal("the cell stayed")
	}
	set(0, 2, "b")
	dbeKey(m, "u", 0, 0) // not a changed cell: the row's changes go
	if tab.edits.rowChanged(0) {
		t.Fatal("the row's changes stayed")
	}
	tab.edits.deleted[1] = true
	dbeKey(m, "u", 1, 0)
	if tab.edits.deleted[1] {
		t.Fatal("the delete stayed")
	}
	tab.edits.added = append(tab.edits.added, &dbNewRow{set: map[int]dbclient.Cell{}})
	dbeKey(m, "u", 2, 0)
	if len(tab.edits.added) != 0 {
		t.Fatal("the new row stayed")
	}
	set(1, 2, "c")
	dbeKey(m, "u", 0, 0)
	if !m.confirmOpen || m.confirmKind != confirmKindDBRevertAll {
		t.Fatal("no revert-all card")
	}
	m.confirmOpen = false
	cmd, ok := m.databaseEditConfirm("cancel")
	if !ok || cmd != nil || !tab.dirty() {
		t.Fatal("cancel reverted")
	}
	m.confirmKind = confirmKindDBRevertAll
	m.databaseEditConfirm("revert")
	if tab.dirty() {
		t.Fatal("not reverted")
	}
	dbeToastHas(t, m, "Changes reverted")
}

// w: the card, Submit, the refresh; and the ways it is refused.
func TestCovDBEditSubmit(t *testing.T) {
	m, sess := dbeModel(t)
	dbeOpen(t, m, "orders")
	s := m.dbConsole()
	tab := s.curTab()
	m.databaseEditTarget(tab) // the page drawn in the editor looks it up
	dbeKey(m, "w", 0, 0)
	dbeToastHas(t, m, "No pending changes")

	dbSetCellValue(tab, dbEditPos{orig: 0, added: -1, col: 2}, "changed", false)
	dbeKey(m, "w", 0, 0)
	if !m.confirmOpen || m.confirmKind != confirmKindDBSubmit || !strings.Contains(m.confirm.Message(), `UPDATE "public"."orders"`) {
		t.Fatalf("no submit card (toast %q)", cdbToast(m))
	}
	m.confirmOpen = false
	clip := dbeClip(t)
	m.databaseEditConfirm("copy")
	if !strings.HasPrefix(clip(), `UPDATE "public"."orders"`) || !m.confirmOpen {
		t.Fatalf("copy %q", clip())
	}
	m.confirmOpen = false
	cmd, _ := m.databaseEditConfirm("submit")
	dbePump(m, cmd)
	if !strings.Contains(sess.ran(), `UPDATE "public"."orders" SET "note" = $1 WHERE "id" = $2`) {
		t.Fatalf("no UPDATE ran:\n%s", sess.ran())
	}
	if tab.dirty() {
		t.Fatal("the change stayed pending")
	}

	// Refused: a read-only connection, a running query, no session.
	dbSetCellValue(tab, dbEditPos{orig: 0, added: -1, col: 2}, "again", false)
	m.databases.conns[0].ReadOnly = true
	dbeKey(m, "w", 0, 0)
	dbeToastHas(t, m, "The connection is read-only")
	m.databases.conns[0].ReadOnly = false
	s.running = &dbRun{}
	m.databaseSubmit(tab, nil)
	dbeToastHas(t, m, "A query is running")
	s.running = nil
	delete(m.databases.sessions, "pg")
	m.databaseSubmit(tab, nil)
	dbeToastHas(t, m, "Not connected")
	m.databases.sessions["pg"] = sess
	if m.databaseSubmit(&dbTab{}, nil) != nil {
		t.Fatal("a tab not on the page")
	}
	tg := tab.target
	tab.target = &dbEditTarget{why: "read-only"}
	dbeKey(m, "w", 0, 0)
	dbeToastHas(t, m, "this result cannot be edited")
	tab.target = tg

	// Manual mode: the transaction cannot begin.
	m.databases.manual = map[string]bool{"pg": true}
	sess.beginErr = errors.New("no begin")
	msgs := dbeMsgs(m.databaseSubmit(tab, []dbclient.EditStatement{{SQL: "UPDATE x"}}))
	if len(msgs) != 1 || msgs[0].(dbSubmitMsg).out.Err == nil {
		t.Fatalf("%v", msgs)
	}
	s.submitting = nil
	m.databases.manual = nil
	sess.beginErr = nil

	// A card answer with nothing to submit, a foreign card.
	m.confirmKind = confirmKindDBSubmit
	if cmd, ok := m.databaseEditConfirm("cancel"); !ok || cmd != nil {
		t.Fatal("cancel")
	}
	m.confirmKind = 0
	if _, ok := m.databaseEditConfirm("x"); ok {
		t.Fatal("a foreign card")
	}
}

// The refresh after a submit: a failed read, a replaced result, a console
// result whose columns changed.
func TestCovDBEditRefresh(t *testing.T) {
	m, sess := dbeModel(t)
	dbeOpen(t, m, "orders")
	tab := m.dbConsole().curTab()
	m.onDBRefresh(dbRefreshMsg{tab: tab, run: tab.run, result: dbclient.Result{Err: errors.New("gone")}})
	dbeToastHas(t, m, "Could not reload the rows")
	if m.onDBRefresh(dbRefreshMsg{tab: tab, run: &dbRun{}}) != nil {
		t.Fatal("a replaced result")
	}
	tab.run.tv = nil
	// The console SQL is no read, the connection is gone, no session.
	tab.run.sql = "DELETE FROM x"
	if m.databaseRefreshTab(tab) != nil {
		t.Fatal("a write was run again")
	}
	tab.run.sql = "SELECT 1"
	msgs := dbeMsgs(m.databaseRefreshTab(tab))
	if len(msgs) != 1 {
		t.Fatalf("%v", msgs)
	}
	tab.run.sessKey = "nope"
	if m.databaseRefreshTab(tab) != nil {
		t.Fatal("no session")
	}
	tab.run.sessKey = ""
	tab.run.conn = "gone"
	m.databases.sessions["gone"] = sess
	if m.databaseRefreshTab(tab) != nil {
		t.Fatal("no connection")
	}
	tab.run.result.Err = errors.New("x")
	if m.databaseRefreshTab(tab) != nil {
		t.Fatal("a failed result")
	}
	m.databases = nil
	if m.onDBRefresh(dbRefreshMsg{}) != nil || m.onDBSubmit(dbSubmitMsg{}) != nil {
		t.Fatal("no databases")
	}
}

// Why a result is read-only, before any lookup.
func TestCovDBEditTargets(t *testing.T) {
	m, sess := dbeModel(t)
	ro := func(r *dbRun) string {
		tg, _ := m.databaseEditTargetOf(&dbTab{run: r})
		return tg.why + ": " + tg.detail
	}
	cols := dbclient.Result{Columns: []dbclient.Column{{Name: "id"}}}
	for _, c := range []struct {
		r    *dbRun
		want string
	}{
		{&dbRun{conn: "pg"}, "There are no rows to edit"},
		{&dbRun{conn: "nope", result: cols}, "does not use SQL"},
		{&dbRun{conn: "pg", by: "ai", result: cols}, "read-only: AI result"},
		{&dbRun{conn: "pg", describe: &dbTable{}, result: cols}, "A table description cannot be edited"},
		{&dbRun{conn: "pg", sql: "SELECT 1 FROM a UNION SELECT 2 FROM b", result: cols}, "read-only query"},
		{&dbRun{conn: "pg", sql: "SELECT id FROM orders", sessKey: "other", result: cols}, "Connect first"},
	} {
		if got := ro(c.r); !strings.Contains(got, c.want) {
			t.Errorf("%+v: %q", c.r, got)
		}
	}

	// A console SELECT looks the key up; the answers say why not.
	r := &dbRun{conn: "pg", sql: "SELECT id, note FROM orders", result: dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "note"}, {Name: "note"}}}}
	tab := &dbTab{run: r}
	tg, cmd := m.databaseEditTargetOf(tab)
	if !tg.loading || cmd == nil {
		t.Fatal("no lookup")
	}
	keys := cdbMsgs(cmd)[0].(dbEditKeysMsg)
	reset := func() { tab.target = &dbEditTarget{loading: true} }
	m.onDBEditKeys(keys)
	if tab.target.why != "" || !tab.target.editable[0] || tab.target.editable[2] {
		t.Fatalf("target %+v", tab.target)
	}
	if m.onDBEditKeys(keys) != nil {
		t.Fatal("a second answer")
	}
	reset()
	k := keys
	k.err = errors.New("denied")
	m.onDBEditKeys(k)
	if tab.target.why != "read-only" {
		t.Fatalf("%+v", tab.target)
	}
	reset()
	k = keys
	k.ir = dbclient.IntrospectionResult{}
	m.onDBEditKeys(k)
	if tab.target.why != "read-only: no table" {
		t.Fatalf("%+v", tab.target)
	}
	reset()
	k = keys
	k.ir = dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id", PK: true}, {Name: "x"}}}
	m.onDBEditKeys(k)
	if tab.target.why != "" {
		t.Fatalf("a PK column: %+v", tab.target)
	}
	reset()
	k = keys
	k.ir = dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "zz"}}, Keys: []string{"zz"}}
	m.onDBEditKeys(k)
	if !strings.HasPrefix(tab.target.why, "read-only") {
		t.Fatalf("a key not in the result: %+v", tab.target)
	}
	_ = sess

	// A JOIN result looks up each table.
	jr := &dbRun{conn: "pg", sql: "SELECT o.id, o.note, c.name FROM orders o JOIN customers c ON c.id = o.customer_id", result: dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "note"}, {Name: "name"}}}}
	jt := &dbTab{run: jr}
	tg, cmd = m.databaseEditTargetOf(jt)
	if !tg.loading || cmd == nil {
		t.Fatalf("no join lookup: %+v", tg)
	}
	jk := cdbMsgs(cmd)[0].(dbEditKeysMsg)
	m.onDBEditKeys(jk)
	if jt.target.loading {
		t.Fatalf("join target %+v", jt.target)
	}
}

// The summary on row 2 of the page and databaseEditable.
func TestCovDBEditSummaryAndEditable(t *testing.T) {
	m, _ := dbeModel(t)
	if m.databaseEditable(nil) {
		t.Fatal("nil tab")
	}
	dbeOpen(t, m, "orders")
	s := m.dbConsole()
	tab := s.curTab()
	tab.target = nil
	if !m.databaseEditable(tab) {
		t.Fatal("a data view with a key")
	}
	dbSetCellValue(tab, dbEditPos{orig: 0, added: -1, col: 2}, "x", false)
	tab.edits.note = "it failed"
	extra, brief := m.databaseEditSummary(tab.run)
	if !strings.Contains(extra, "1 change") || !strings.Contains(extra, "it failed") || brief != " · 1 change" {
		t.Fatalf("%q %q", extra, brief)
	}
	if extra, _ := m.databaseEditSummary(&dbRun{conn: "pg"}); extra != "" {
		t.Fatalf("another run %q", extra)
	}
	tab.edits = nil
	tab.target = &dbEditTarget{why: "read-only: a view"}
	if extra, _ := m.databaseEditSummary(tab.run); !strings.Contains(extra, "read-only: a view") {
		t.Fatalf("%q", extra)
	}
	var e *dbEdits
	if e.rowChanged(0) {
		t.Fatal("nil edits")
	}
	dbApplyOptimistic(&dbTab{})
}

// A typed value is checked against the column type before it is kept.
func TestCovDBEditCheckCellType(t *testing.T) {
	for _, c := range []struct {
		typ, v string
		ok     bool
	}{
		{"int[]", "{1,2}", true}, {"int[]", "{1,x}", false},
		{"bigint unsigned", "18446744073709551615", true}, {"int", "1.5", false},
		{"numeric(10,2)", "1.5", true}, {"float8", "abc", false},
		{"boolean", "yes", true}, {"bool", "maybe", false},
		{"uuid", "3f2b8c1e-9a4d-4e2f-8b1a-0c9d8e7f6a5b", true}, {"uuid", "nope", false},
		{"jsonb", `{"a":1}`, true}, {"json", "{", false},
		{"date", "2026-01-02", true}, {"date", "02/01/2026", false},
		{"text", "anything", true},
	} {
		if err := dbCheckCellType(c.typ, c.v); (err == nil) != c.ok {
			t.Errorf("%s %q: %v", c.typ, c.v, err)
		}
	}
	tab := &dbTab{run: &dbRun{result: dbclient.Result{Columns: []dbclient.Column{{Name: "n", Type: "int"}}, Rows: [][]dbclient.Cell{{dbeNum("1")}}}}}
	if dbSetCellSpecial(tab, dbEditPos{orig: 5, added: -1}, dbDefaultCell()) || dbSetCellSpecial(tab, dbEditPos{orig: -1, added: 3}, dbDefaultCell()) {
		t.Fatal("a cell that is not there")
	}
	tab.edits.added = append(tab.edits.added, &dbNewRow{set: map[int]dbclient.Cell{}})
	if !dbSetCellSpecial(tab, dbEditPos{orig: -1, added: 0}, dbclient.NewCellUUID()) || len(tab.edits.added[0].set) != 1 {
		t.Fatal("a UUID on a new row")
	}
	var m Model
	if _, ok := m.databaseCheckTyped(tab, dbEditPos{col: 9}, "x"); !ok {
		t.Fatal("a column out of range is not checked")
	}
	if (&dbConsoleState{}).transAdded() != 0 {
		t.Fatal("transAdded")
	}
}
