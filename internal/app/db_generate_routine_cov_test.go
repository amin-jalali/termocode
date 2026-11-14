package app

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
)

// dbeClip catches what the model copies (the real clipboard is never
// touched).
func dbeClip(t *testing.T) func() string {
	t.Helper()
	var mu sync.Mutex
	var got string
	t.Cleanup(SetClipboardSink(func(s string) { mu.Lock(); got = s; mu.Unlock() }))
	return func() string { mu.Lock(); defer mu.Unlock(); return got }
}

const dbeRoutinePath = "pg/db/app/schema/public/Routines/add_item"

// c on a routine: the picker, the signature read, one prompt per input
// argument, then the call is copied; DROP needs no argument.
func TestCovDBRoutineGenerateFlow(t *testing.T) {
	clip := dbeClip(t)
	m, sess := dbeModel(t)
	s := m.databases
	cdbAddNode(s, "pg/db/app/schema/public", "pg/db/app/schema/public/Routines", dbNodeGroup, "Routines")
	cdbAddNode(s, "pg/db/app/schema/public/Routines", dbeRoutinePath, dbNodeRoutine, "add_item")
	s.databaseSelectPath(dbeRoutinePath)

	delete(s.sessions, "pg")
	m.databaseCodeGenStart()
	dbeToastHas(t, m, "Not connected")
	s.sessions["pg"] = sess
	m.databaseCodeGenStart()
	if !m.pickerOpen || m.pickerKind != pickerKindDBGenerate || s.genRoutine.name != "add_item" {
		t.Fatalf("no routine picker: %v %+v", m.pickerOpen, s.genRoutine)
	}
	m.pickerOpen = false
	cmd, ok := m.databaseGeneratePicker("call")
	if !ok {
		t.Fatal("not routed")
	}
	dbePump(m, cmd) // the fake's catalog has no routines
	dbeToastHas(t, m, "Routine not found")

	tgt, _ := m.databaseRoutineAt(dbeRoutinePath)
	if tgt.schema != "public" || tgt.qualified() != `"public"."add_item"` {
		t.Fatalf("%+v %s", tgt, tgt.qualified())
	}
	gen := m.dbConsole().tableGen
	rt := dbclient.CanonRoutine{Name: "add_item", Kind: "FUNCTION", Args: "qty integer, note text, OUT id bigint"}
	if m.onDBRoutine(dbRoutineMsg{gen: gen + 1, target: tgt, routine: rt, found: true, code: "call"}) != nil {
		t.Fatal("stale")
	}
	m.onDBRoutine(dbRoutineMsg{gen: gen, target: tgt, routine: rt, found: true, code: "call"})
	if !m.promptOpen || m.promptKind != promptKindDBRoutineArg || !strings.Contains(m.prompt.View(), "argument 1 of 2") {
		t.Fatalf("no argument prompt %v", m.promptOpen)
	}
	dbePrompt(t, m, "3")
	if !m.promptOpen || !strings.Contains(m.prompt.View(), "argument 2 of 2") {
		t.Fatal("no second prompt")
	}
	dbePrompt(t, m, "it's")
	if got := clip(); got != `SELECT * FROM "public"."add_item"(3 /* qty */, 'it''s' /* note */, NULL /* id */);` {
		t.Fatalf("call %q", got)
	}
	dbeToastHas(t, m, "Copied the call of add_item")
	if m.databaseRoutineArgEntered("x") != nil {
		t.Fatal("an answer with no form")
	}

	m.onDBRoutine(dbRoutineMsg{gen: gen, target: tgt, routine: rt, found: true, code: "rdrop"})
	if got := clip(); got != `DROP FUNCTION "public"."add_item"(qty integer, note text, OUT id bigint);` {
		t.Fatalf("drop %q", got)
	}
	// No input argument: the call at once; a procedure is CALLed.
	m.onDBRoutine(dbRoutineMsg{gen: gen, target: tgt, routine: dbclient.CanonRoutine{Name: "add_item", Kind: "PROCEDURE"}, found: true, code: "call"})
	if got := clip(); got != `CALL "public"."add_item"();` {
		t.Fatalf("procedure %q", got)
	}
	m.onDBRoutine(dbRoutineMsg{gen: gen, err: errors.New("catalog denied")})

	// The read fails.
	sess.roErr = errors.New("ro")
	msgs := cdbMsgs(m.databaseRoutineLoad(tgt, "call", false))
	if len(msgs) != 1 || msgs[0].(dbRoutineMsg).err == nil {
		t.Fatalf("%v", msgs)
	}
	delete(s.sessions, "pg")
	m.databaseRoutineLoad(tgt, "call", false)
	dbeToastHas(t, m, "Not connected")
	m.databases = nil
	if _, ok := m.databaseRoutineAt(dbeRoutinePath); ok {
		t.Fatal("no databases")
	}
	if m.databaseGeneratePick("x", false) != nil {
		t.Fatal("no databases")
	}
}

// MySQL calls name OUT arguments as session variables; an unnamed one
// gets a number. The argument prompt names an unnamed argument by place.
func TestCovDBRoutineCallMySQL(t *testing.T) {
	tgt := dbRoutineTarget{driver: "mysql", schema: "shop", name: "f"}
	got := dbRoutineCall(tgt, dbclient.CanonRoutine{Kind: "FUNCTION", Args: "IN `a` int, OUT `` int, OUT `total` int, flag bool"}, []string{"1", "true"})
	if got != "SELECT `shop`.`f`(1 /* a */, @out2, @total, TRUE /* flag */);" {
		t.Fatalf("%q", got)
	}
	if got := dbRoutineDrop(tgt, dbclient.CanonRoutine{Kind: "PROCEDURE"}); got != "DROP PROCEDURE `shop`.`f`;" {
		t.Fatalf("%q", got)
	}
	if (dbRoutineTarget{driver: "nosuch", name: "x"}).qualified() != "x" || (dbRoutineTarget{driver: "mysql", name: "x"}).qualified() != "`x`" {
		t.Fatal("qualified")
	}
	if got := dbParseRoutineArgs("a int, , b int"); len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	m := cdbModel(t)
	m.ensureDatabasesLoaded()
	m.databases.routineAsk = &dbRoutineAsk{target: tgt, inputs: []dbRoutineArg{{typ: "int"}}}
	m.databaseRoutineAskNext()
	if !strings.Contains(m.prompt.View(), "argument 1 (int)") {
		t.Fatalf("%q", m.prompt.View())
	}
	// A routine of a non-SQL connection, of no connection, a non-routine row.
	m2 := cdbModel(t, dbclient.Conn{ID: "r", Driver: "redis", Host: "h"}, dbclient.Conn{ID: "my", Driver: "mysql", Host: "h"})
	cdbAddNode(m2.databases, "r", "r/routine/x", dbNodeRoutine, "x")
	cdbAddNode(m2.databases, "", "zz/routine/x", dbNodeRoutine, "x")
	cdbAddNode(m2.databases, "my", "my/db/shop", dbNodeDatabase, "shop")
	cdbAddNode(m2.databases, "my/db/shop", "my/db/shop/Routines/f", dbNodeRoutine, "f")
	for _, p := range []string{"r/routine/x", "zz/routine/x", "r"} {
		if _, ok := m2.databaseRoutineAt(p); ok {
			t.Errorf("%s is a routine", p)
		}
	}
	if r, ok := m2.databaseRoutineAt("my/db/shop/Routines/f"); !ok || r.schema != "shop" {
		t.Fatalf("%+v", r)
	}
	m2.databases.cursor = -1
	if _, ok := m2.databaseRoutineGenStart(); ok {
		t.Fatal("no row")
	}
}

// c on a table: the picker, the columns read, then the text is copied;
// → inserts it at the editor cursor.
func TestCovDBTableGenerateFlow(t *testing.T) {
	clip := dbeClip(t)
	m, sess := dbeModel(t)
	sess.over = func(sql string) (dbclient.Result, bool) {
		if strings.Contains(sql, "information_schema.columns") {
			return dbclient.Result{Rows: [][]dbclient.Cell{
				{cdbCell("id"), cdbCell("integer"), cdbCell("NO"), {Null: true}},
				{cdbCell("customer_id"), cdbCell("integer"), cdbCell("YES"), {Null: true}},
				{cdbCell("note"), cdbCell("text"), cdbCell("YES"), {Null: true}}}}, true
		}
		return dbclient.Result{}, false
	}
	s := m.databases
	s.cursor = -1
	m.databaseCodeGenStart()
	dbeToastHas(t, m, "Select a table or view")
	s.databaseSelectPath("pg/db/app/schema/public/Tables/orders")
	m.databaseCodeGenStart()
	if !m.pickerOpen || s.genTable.name != "orders" {
		t.Fatal("no table picker")
	}
	m.pickerOpen = false
	cmd, _ := m.databaseGeneratePicker("select")
	dbePump(m, cmd)
	if got := clip(); got != "SELECT \"id\", \"customer_id\", \"note\"\nFROM \"public\".\"orders\";" {
		t.Fatalf("select %q (toast %q)", got, cdbToast(m))
	}
	dbeToastHas(t, m, "Copied")
	// → with no file open copies instead.
	nm, cmd := m.Update(picker.ActionMsg{ID: "update"})
	*m = nm.(Model)
	dbePump(m, cmd)
	if got := clip(); !strings.HasPrefix(got, "UPDATE \"public\".\"orders\"\nSET ") {
		t.Fatalf("update %q", got)
	}
	dbeToastHas(t, m, "No file is open: copied")
	m.onDBGenerated(dbMetaMsg{code: "select", tbl: dbTable{name: "x"}})
	dbeToastHas(t, m, "Nothing to generate")
	m.pickerKind = 0
	if _, ok := m.databaseGeneratePicker("select"); ok {
		t.Fatal("foreign picker")
	}
	s.genTable, s.genRoutine = dbTable{}, dbRoutineTarget{}
	if m.databaseGeneratePick("select", false) != nil {
		t.Fatal("nothing picked")
	}
}

// → inserts the text at the cursor of the open file.
func TestCovDBGenerateInsertsAtTheCursor(t *testing.T) {
	dbeClip(t)
	m := cdbRealModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432})
	p := cdbOpenFile(t, m, "x.sql", "-- top\n")
	m.editor.SetMeta(p, false, "")
	m.databaseDeliverCode("SELECT 1;", "the thing", true)
	dbeToastHas(t, m, "Inserted the thing")
	if got := dbeBufLines(t, m); !strings.Contains(got, "SELECT 1;") {
		t.Fatalf("buffer %q", got)
	}
}

func TestCovDBGenerateTextKinds(t *testing.T) {
	tbl := dbTable{driver: "postgres", schema: "public", name: "9 rows"}
	meta := dbTableMeta{cols: []dbColInfo{{name: "id", typ: "bigint", pk: true}, {name: "class", typ: "bytea"}, {name: "", typ: "text"}, {name: "2x", typ: "int"}}}
	if got, ok := dbGenerate("update", tbl, dbTableMeta{cols: []dbColInfo{{name: "id", typ: "int", pk: true}}, ir: dbclient.IntrospectionResult{Keys: []string{"id"}}}); !ok || !strings.Contains(got, `SET "id" = `) {
		t.Fatalf("update with only the key: %q", got)
	}
	if got, ok := dbGenerate("create", tbl, meta); !ok || !strings.Contains(got, "CREATE TABLE") {
		t.Fatalf("create %q", got)
	}
	py, _ := dbGenerate("python", tbl, meta)
	for _, want := range []string{"class_: Optional[bytes]", "col3: Optional[str]", "c2x: Optional[int]", "class T9Rows:"} {
		if !strings.Contains(py, want) {
			t.Fatalf("python lacks %q:\n%s", want, py)
		}
	}
	if got := dbGoName("", "Row"); got != "Row" {
		t.Fatalf("%q", got)
	}
}
