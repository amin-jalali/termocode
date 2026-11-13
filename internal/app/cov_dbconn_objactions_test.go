package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestCovDBObjWordsAndCardText(t *testing.T) {
	for k, w := range map[dbNodeKind]string{dbNodeTable: "table", dbNodeView: "view", dbNodeRoutine: "function", dbNodeSequence: "sequence", dbNodeSchema: "schema", dbNodeDatabase: "schema", dbNodeColumn: "object"} {
		if got := dbObjWord(k); got != w {
			t.Errorf("%v: %q", k, got)
		}
	}
	got := dbObjectActionCardText([]string{"DROP TABLE a"}, []string{"view v"}, true)
	for _, w := range []string{"DROP TABLE a;", "Also removed or broken (1)", "view v", "cannot be undone"} {
		if !strings.Contains(got, w) {
			t.Errorf("card lacks %q: %s", w, got)
		}
	}
	for id, want := range map[string]dbclient.ObjectActionKind{"truncate": dbclient.ActTruncate} {
		if a := dbObjectActionFor(id, dbObjTarget{kind: dbNodeTable, name: "t"}, ""); a.Kind != want {
			t.Errorf("%s: %+v", id, a)
		}
	}
	tg := dbObjTarget{kind: dbNodeColumn, schema: "s", name: "t", column: "id"}
	if a := dbObjectActionFor("make-pk", tg, ""); a.Kind != dbclient.ActMakePK || a.Columns[0] != "id" {
		t.Errorf("%+v", a)
	}
	if a := dbObjectActionFor("create-schema", tg, "n"); a.Kind != dbclient.ActCreateSchema || a.Name != "n" {
		t.Errorf("%+v", a)
	}
	if a := dbObjectActionFor("create-database", tg, "n"); a.Kind != dbclient.ActCreateDatabase {
		t.Errorf("%+v", a)
	}
	if a := dbObjectActionFor("drop", dbObjTarget{kind: dbNodeSchema, schema: "s", name: "s"}, ""); a.Schema != "" {
		t.Errorf("schema drop keeps its schema: %+v", a)
	}
	if got := dbActionIDs(dbNodeSequence, "postgres"); got != "drop" {
		t.Errorf("sequence %q", got)
	}
}

func cdbObjModel(t *testing.T) (*Model, *cdbSession) {
	t.Helper()
	m := cdbModel(t,
		dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432},
		dbclient.Conn{ID: "my", Driver: "mysql", Host: "h", Port: 3306},
		dbclient.Conn{ID: "mongo", Driver: "redis", Host: "h"})
	cdbPGTree(m.databases)
	cdbAddNode(m.databases, "my", "my/db/shop", dbNodeDatabase, "shop")
	sess := &cdbSession{}
	m.databases.sessions["pg"] = sess
	m.databases.sessions["my"] = sess
	return m, sess
}

func TestCovDBObjectTargets(t *testing.T) {
	m, _ := cdbObjModel(t)
	s := m.databases
	tg, ok := s.dbObjTargetOf(dbTreeRow{kind: dbNodeColumn, path: "pg/db/app/schema/public/table/orders/col/id", name: "id", conn: "pg"})
	if !ok || tg.name != "orders" || tg.column != "id" || tg.schema != "public" || tg.parent != "pg/db/app/schema/public/table/orders" {
		t.Fatalf("%+v %v", tg, ok)
	}
	if _, ok := s.dbObjTargetOf(dbTreeRow{kind: dbNodeColumn, path: "pg/odd", conn: "pg"}); ok {
		t.Fatal("a column path without /col/")
	}
	if _, ok := s.dbObjTargetOf(dbTreeRow{kind: dbNodeTable, path: "x", conn: "zzz"}); ok {
		t.Fatal("unknown connection")
	}
	if _, ok := s.dbObjTargetOf(dbTreeRow{kind: dbNodeTable, path: "x", conn: "mongo"}); ok {
		t.Fatal("redis has no SQL dialect")
	}
	tg, _ = s.dbObjTargetOf(dbTreeRow{kind: dbNodeDatabase, path: "pg/db/app", name: "app", conn: "pg"})
	if tg.parent != "pg/db/app" || tg.schema != "" {
		t.Fatalf("%+v", tg)
	}
	tg, _ = s.dbObjTargetOf(dbTreeRow{kind: dbNodeTable, path: "my/db/shop/table/o", name: "o", conn: "my"})
	if tg.schema != "shop" {
		t.Fatalf("mysql schema %+v", tg)
	}
	tg, _ = s.dbObjTargetOf(dbTreeRow{kind: dbNodeConnection, path: "pg", name: "pg", conn: "pg"})
	if tg.parent != "pg" {
		t.Fatalf("%+v", tg)
	}
	if got := s.dbReloadAncestor("zzz/a/b"); got == "" {
		t.Fatal("no ancestor")
	}
}

func TestCovDBObjectActionsStartGuards(t *testing.T) {
	m, _ := cdbObjModel(t)
	s := m.databases
	toastHas := func(w string) {
		t.Helper()
		if !strings.Contains(covToastText(m), w) {
			t.Fatalf("toast lacks %q: %q", w, covToastText(m))
		}
	}
	m.databaseObjectActions() // on the connection: create database, but the cursor row is a connection
	if !m.pickerOpen || m.pickerKind != pickerKindDBObjAction {
		t.Fatalf("connection menu: %q", covToastText(m))
	}
	m.pickerOpen = false
	s.databaseSelectPath("mongo")
	m.databaseObjectActions()
	toastHas("Object actions need a SQL database")
	s.databaseSelectPath("pg/db/app/schema/public/table/orders/col/id")
	delete(s.sessions, "pg")
	m.databaseObjectActions()
	toastHas("Not connected")
	s.sessions["pg"] = &cdbSession{}
	s.conns[0].ReadOnly = true
	m.databaseObjectActions()
	toastHas("Read-only connection")
	s.conns[0].ReadOnly = false
	// a row with no actions
	s.nodes["pg/db/app/schema/public/view/v1/x"] = dbNode{kind: dbNodeDetail, name: "d", path: "pg/db/app/schema/public/view/v1/x", conn: "pg"}
	v := s.nodes["pg/db/app/schema/public/view/v1"]
	v.children = []string{"pg/db/app/schema/public/view/v1/x"}
	s.nodes["pg/db/app/schema/public/view/v1"] = v
	s.databaseSelectPath("pg/db/app/schema/public/view/v1/x")
	m.databaseObjectActions()
	toastHas("No object actions here")
	m.databases = nil
	m.databaseObjectActions()
	toastHas("Select an object first")
}

func cdbObjPick(t *testing.T, m *Model, path, id string) {
	t.Helper()
	m.databases.databaseSelectPath(path)
	m.databaseObjectActions()
	if !m.pickerOpen {
		t.Fatalf("no picker on %s: %q", path, covToastText(m))
	}
	cdbPick(t, m, id)
}

func TestCovDBObjectActionsEveryFlow(t *testing.T) {
	m, sess := cdbObjModel(t)
	s := m.databases
	var copied string
	t.Cleanup(SetClipboardSink(func(x string) { copied = x }))
	tbl := "pg/db/app/schema/public/table/orders"

	// copy: a name prompt, then a card; the card runs
	cdbObjPick(t, m, tbl, "copy")
	if !m.promptOpen || m.promptKind != promptKindDBObjName {
		t.Fatal("no name prompt")
	}
	cdbSubmit0 := func(v string) {
		m.promptOpen = false
		cmd, ok := m.databaseObjActionPrompt(promptKindDBObjName, v)
		if !ok {
			t.Fatal("not routed")
		}
		cdbRun(cmd)
	}
	cdbSubmit0("  ")
	if !strings.Contains(covToastText(m), "No name") {
		t.Fatalf("toast %q", covToastText(m))
	}
	cdbObjPick(t, m, tbl, "copy")
	cdbSubmit0("orders_bak")
	if !m.confirmOpen || m.confirmKind != confirmKindDBObjAction {
		t.Fatal("no card")
	}
	m.confirmOpen = false
	cmd, ok := m.databaseObjActionConfirm("copy")
	if !ok || !strings.Contains(copied, "orders_bak") {
		t.Fatalf("copied %q", copied)
	}
	_ = cmd
	cdbObjPick(t, m, tbl, "copy")
	cdbSubmit0("orders_bak")
	cmd, _ = m.databaseObjActionConfirm("run")
	msgs := cdbMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs %v", msgs)
	}
	done := msgs[0].(dbObjDoneMsg)
	if done.err != nil || !strings.Contains(sess.ran(), "orders_bak") {
		t.Fatalf("%+v ran %q", done, sess.ran())
	}
	m.onDBObjDone(done) // the parent reloads
	if !strings.Contains(covToastText(m), "Done") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.onDBObjDone(dbObjDoneMsg{err: errors.New("denied")})
	if !strings.Contains(covToastText(m), "Object action failed") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.onDBObjDone(dbObjDoneMsg{p: dbObjPending{t: dbObjTarget{parent: "zzz"}}})
	saved := m.databases
	m.databases = nil
	m.onDBObjDone(dbObjDoneMsg{})
	m.databases = saved

	// drop reads the dependents first
	sess.run = func(q string) dbclient.Result {
		return dbclient.Result{Rows: [][]dbclient.Cell{{{Text: "view"}, {Text: "public"}, {Text: "v_orders"}}, {{Text: "short"}}}}
	}
	cdbObjPick(t, m, tbl, "drop")
	dm := cdbMsgs(m.databaseObjActionPrepare(""))
	if len(dm) != 1 {
		t.Fatalf("deps %v", dm)
	}
	deps := dm[0].(dbObjDepsMsg)
	if len(deps.deps) != 1 || deps.err != nil {
		t.Fatalf("%+v", deps)
	}
	m.onDBObjDeps(deps)
	if !m.confirmOpen {
		t.Fatal("no card")
	}
	// dependents unreadable
	m.onDBObjDeps(dbObjDepsMsg{p: *s.objAction, err: errors.New("no access")})
	// manual transaction + production
	s.manual = map[string]bool{"pg": true}
	s.conns[0].Environment = "production"
	m.onDBObjDeps(dbObjDepsMsg{p: *s.objAction})
	s.manual = nil
	s.conns[0].Environment = ""
	// the dependents query fails / ro fails
	sess.run = func(string) dbclient.Result { return dbclient.Result{Err: errors.New("x")} }
	cdbObjPick(t, m, tbl, "drop-cascade")
	if got := cdbMsgs(m.databaseObjActionPrepare("")); len(got) != 1 || got[0].(dbObjDepsMsg).err == nil {
		t.Fatalf("%v", got)
	}
	sess.roErr = errors.New("ro")
	if got := cdbMsgs(m.databaseObjActionPrepare("")); len(got) != 1 || got[0].(dbObjDepsMsg).err == nil {
		t.Fatalf("%v", got)
	}
	sess.roErr = nil
	// a drop with no connection to read from
	delete(s.sessions, "pg")
	m.databaseObjActionPrepare("")
	s.sessions["pg"] = sess

	// create schema / database prompts
	cdbObjPick(t, m, "pg/db/app/schema/public", "create-schema")
	if !m.promptOpen {
		t.Fatal("no prompt")
	}
	cdbSubmit0("new_schema")
	cdbObjPick(t, m, "my/db/shop", "create-schema")
	cdbObjPick(t, m, "pg", "create-database")
	cdbSubmit0("newdb")
	// make-pk and truncate and view drop go straight to the card
	cdbObjPick(t, m, "pg/db/app/schema/public/table/orders/col/id", "make-pk")
	cdbObjPick(t, m, tbl, "truncate")
	cdbObjPick(t, m, "pg/db/app/schema/public/view/v1", "drop")
	if !m.confirmOpen {
		t.Fatal("no card for a view drop")
	}
	// an unusable name (empty after a prompt-less call)
	m.databaseObjActionPrepare("")
	// buttons: other, no pending, not connected
	m.confirmKind = confirmKindDBObjAction
	if cmd, ok := m.databaseObjActionConfirm("cancel"); !ok || cmd != nil {
		t.Fatal("cancel")
	}
	if cmd, ok := m.databaseObjActionConfirm("run"); !ok || cmd != nil {
		t.Fatal("no pending")
	}
	s.objAction = &dbObjPending{t: dbObjTarget{sessKey: "gone"}, stmts: []string{"x"}}
	m.databaseObjActionConfirm("run")
	if !strings.Contains(covToastText(m), "Not connected") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// the failing statement
	s.objAction = &dbObjPending{t: dbObjTarget{sessKey: "pg"}, stmts: []string{"x"}}
	sess.run = func(string) dbclient.Result { return dbclient.Result{Err: errors.New("fail")} }
	cmd, _ = m.databaseObjActionConfirm("run")
	if got := cdbMsgs(cmd); len(got) != 1 || got[0].(dbObjDoneMsg).err == nil {
		t.Fatalf("%v", got)
	}
}

func TestCovDBObjectActionRouting(t *testing.T) {
	m, _ := cdbObjModel(t)
	m.pickerKind = 0
	if _, ok := m.databaseObjActionPicker("x"); ok {
		t.Fatal("foreign picker")
	}
	m.pickerKind = pickerKindDBObjAction
	if cmd, ok := m.databaseObjActionPicker("x"); !ok || cmd != nil {
		t.Fatal("no pending action")
	}
	if _, ok := m.databaseObjActionPrompt(0, "x"); ok {
		t.Fatal("foreign prompt")
	}
	m.confirmKind = 0
	if _, ok := m.databaseObjActionConfirm("run"); ok {
		t.Fatal("foreign card")
	}
	m.confirmKind = confirmKindDBObjAction
	m.databases.objAction = nil
	if cmd := m.databaseObjActionPrepare(""); cmd != nil {
		t.Fatal("no pending")
	}
	m.databases.objAction = &dbObjPending{id: "create-schema", t: dbObjTarget{driver: "postgres"}}
	m.databaseObjActionPrepare("") // an empty name cannot be planned
	if !strings.Contains(covToastText(m), "Cannot do that") {
		t.Fatalf("toast %q", covToastText(m))
	}
	saved := m.databases
	m.databases = nil
	if cmd := m.databaseObjActionCard(dbObjPending{}, nil, nil); cmd != nil {
		t.Fatal("no databases")
	}
	if cmd, ok := m.databaseObjActionConfirm("run"); !ok || cmd != nil {
		t.Fatal("no databases")
	}
	m.databases = saved
}
