package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Every kind of quick doc reference finds its tree node or says why not.
func TestCovDBQuickDocRefs(t *testing.T) {
	s := dbJoinTestState()
	g := "pg/db/shop/schema/sales/Tables"
	s.nodes["pg/db/shop"] = dbNode{kind: dbNodeDatabase, name: "shop", conn: "pg", children: []string{"pg/db/shop/schema/sales"}}
	s.nodes["pg/db/shop/schema/sales"] = dbNode{kind: dbNodeSchema, name: "sales", conn: "pg", children: []string{g}}
	s.nodes[g+"/cold"] = dbNode{kind: dbNodeTable, name: "cold", conn: "pg"}
	s.nodes[g+"/v1"] = dbNode{kind: dbNodeView, name: "v1", conn: "pg"}
	gn := s.nodes[g]
	gn.children = append(gn.children, g+"/cold", g+"/v1")
	s.nodes[g] = gn
	s.nodes["pg/db/empty"] = dbNode{kind: dbNodeDatabase, name: "empty", conn: "pg"}
	s.nodes["pg/x"] = dbNode{kind: dbNodeNote, name: "note", conn: "pg", detail: "a note"}
	s.nodes["pg/y"] = dbNode{kind: dbNodeNote, name: "bare", conn: "pg"}

	for _, c := range []struct {
		ref   dbclient.SQLRef
		title string
		line  string
	}{
		{dbclient.SQLRef{Kind: "table", Table: "nope"}, "Table nope", "Not in the Databases tree"},
		{dbclient.SQLRef{Kind: "table", Table: "v1"}, "View sales.v1", "Columns not loaded yet"},
		{dbclient.SQLRef{Kind: "column", Schema: "sales", Table: "nope", Column: "x"}, "Column sales.nope.x", "Not in the Databases tree"},
		{dbclient.SQLRef{Kind: "column", Table: "cold", Column: "x"}, "Column cold.x", "are not loaded"},
		{dbclient.SQLRef{Kind: "column", Table: "users", Column: "zz"}, "Column users.zz", "has no column zz"},
		{dbclient.SQLRef{Kind: "column", Table: "users", Column: "email"}, "Column sales.users.email", "Type: int"},
		{dbclient.SQLRef{Kind: "column", Column: "user_id"}, "Column user_id", "sales.lines  int"},
		{dbclient.SQLRef{Kind: "column", Column: "ghost"}, "Column ghost", "No loaded table has this column"},
		{dbclient.SQLRef{Kind: "routine", Schema: "other", Routine: "total_of"}, "Routine other.total_of", "Not a routine of the Databases tree"},
		{dbclient.SQLRef{Kind: "routine", Schema: "sales", Routine: "total_of"}, "Routine sales.total_of", "Its parameters show in its DDL"},
		{dbclient.SQLRef{Kind: "schema", Schema: "sales"}, "Schema sales", "Tables: 6"},
		{dbclient.SQLRef{Kind: "schema", Schema: "shop"}, "Database shop", "Not loaded yet"},
		{dbclient.SQLRef{Kind: "schema", Schema: "gone"}, "Schema gone", "Not in the Databases tree"},
	} {
		title, lines := dbQuickDoc(s, "pg", c.ref)
		if title != c.title || !strings.Contains(strings.Join(lines, "\n"), c.line) {
			t.Errorf("%+v: %q %q", c.ref, title, lines)
		}
	}
	if title, lines := dbQuickDoc(s, "pg", dbclient.SQLRef{Kind: "other"}); title != "" || lines != nil {
		t.Fatal("an unknown kind")
	}
	if title, lines := dbQuickDocNode(s, "pg/x"); title != "note" || len(lines) != 1 || lines[0] != "a note" {
		t.Fatalf("%q %q", title, lines)
	}
	if title, lines := dbQuickDocNode(s, "pg/y"); title != "bare" || len(lines) != 0 {
		t.Fatalf("%q %q", title, lines)
	}
	if title, _ := dbQuickDocNode(s, "missing"); title != "" {
		t.Fatal("a missing node")
	}
	if got := dbQualName("my/db/app/Tables/t", "t"); got != "app.t" {
		t.Fatalf("mysql name %q", got)
	}
	if got := dbQualName("lite", "t"); got != "t" {
		t.Fatalf("plain name %q", got)
	}
	if got := dbOffsetPositions([]string{"ab"}, []int{1, 50}, []string{"x", "y"}); len(got) != 1 {
		t.Fatalf("an offset past the text: %+v", got)
	}
}

// dbeDocModel is a real-editor Model with the pg connection and the
// quick doc test tree, editing a console file of pg.
func dbeDocModel(t *testing.T, text string) *Model {
	t.Helper()
	m := cdbRealModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432})
	for p, n := range dbJoinTestState().nodes {
		m.databases.nodes[p] = n
	}
	m.databases.uniques = dbJoinTestState().uniques
	if text != "" {
		cdbOpenFile(t, m, "q.sql", "-- @conn pg\n"+text+"\n")
	}
	return m
}

// dbeCursor puts the editor cursor on row (1-based) and byte col.
func dbeCursor(t *testing.T, m *Model, row, col int) {
	t.Helper()
	if err := m.nvim.ExecLuaArgs(`local r, c = ...; vim.api.nvim_win_set_cursor(0, {r, c})`, row, col); err != nil {
		t.Fatal(err)
	}
}

// Quick Documentation, INSERT hints, JOIN hints and parameter info on a
// SQL buffer.
func TestCovDBQuickDocOnTheBuffer(t *testing.T) {
	m := dbeDocModel(t, "")
	m.databaseQuickDoc()
	dbeToastHas(t, m, "Open a SQL file")
	cdbOpenFile(t, m, "notes.txt", "x")
	m.databaseInsertHints()
	dbeToastHas(t, m, "Open a SQL file")
	cdbOpenFile(t, m, "free.sql", "SELECT 1;\n")
	m.databaseJoinHints()
	dbeToastHas(t, m, "No connection for this file")

	cdbOpenFile(t, m, "q.sql", "-- @conn pg\nSELECT u.email FROM sales.users u JOIN sales.lines l ON l.user_id = u.id;\nINSERT INTO sales.users (id, email) VALUES (1, 'a');\nSELECT lower('A'), total_of(1, 2), mystery(1);\n")
	dbeCursor(t, m, 2, 28)
	m.databaseQuickDoc()
	if !m.confirmOpen || m.confirmKind != confirmKindDBInfo || m.confirm.Title() != "Table sales.users" {
		t.Fatalf("card %v %q", m.confirmOpen, m.confirm.Title())
	}
	m.confirmOpen = false
	dbeCursor(t, m, 2, 6)
	m.databaseQuickDoc()
	dbeToastHas(t, m, "No name under the cursor")

	m.databaseInsertHints()
	dbeToastHas(t, m, "INSERT column hints shown")
	m.databaseJoinHints()
	dbeToastHas(t, m, "JOIN cardinality hints shown")

	dbeCursor(t, m, 4, 14)
	m.databaseParamInfo()
	if !m.confirmOpen || !strings.HasPrefix(m.confirm.Title(), "Parameters of lower") {
		t.Fatalf("built-in card %q (toast %q)", m.confirm.Title(), cdbToast(m))
	}
	m.confirmOpen = false
	dbeCursor(t, m, 4, 43)
	m.databaseParamInfo()
	if !m.confirmOpen || !strings.Contains(m.confirm.Message(), "not a known built-in function") {
		t.Fatalf("unknown call card %q", m.confirm.Message())
	}
	m.confirmOpen = false
	dbeCursor(t, m, 4, 0)
	m.databaseParamInfo()
	dbeToastHas(t, m, "Not inside a call")

	// A tree routine, not connected: the card says so.
	dbeCursor(t, m, 4, 28)
	m.databaseParamInfo()
	if !m.confirmOpen || !strings.Contains(m.confirm.Message(), "Not connected") {
		t.Fatalf("routine card %q %q", m.confirm.Title(), m.confirm.Message())
	}
	m.confirmOpen = false

	// Connected: the signature is read off the UI goroutine.
	sess := &cdbSession{}
	m.databases.sessions["pg"] = sess
	cmd := m.databaseParamInfo()
	msgs := cdbMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("%v", msgs)
	}
	got := msgs[0].(dbParamInfoMsg)
	if got.found || got.err != nil || got.name != "sales.total_of" || got.arg != 0 {
		t.Fatalf("%+v", got)
	}
	m.onDBParamInfo(got)
	dbeToastHas(t, m, "Routine not found")
	m.onDBParamInfo(dbParamInfoMsg{gen: got.gen, err: errors.New("catalog denied")})
	m.onDBParamInfo(dbParamInfoMsg{gen: got.gen, name: "sales.total_of", arg: 2, found: true, routine: dbclient.CanonRoutine{Name: "total_of", Args: "a integer, OUT b text", Returns: "integer"}})
	body := m.confirm.Message()
	for _, want := range []string{"sales.total_of(a integer, OUT b text)", "past the last parameter", "Returns: integer"} {
		if !strings.Contains(body, want) {
			t.Fatalf("card lacks %q:\n%s", want, body)
		}
	}
	if m.onDBParamInfo(dbParamInfoMsg{gen: got.gen + 5}) != nil {
		t.Fatal("stale")
	}
	sess.roErr = errors.New("no read-only session")
	got = cdbMsgs(m.databaseParamInfo())[0].(dbParamInfoMsg)
	if got.err == nil {
		t.Fatal("the read-only error was lost")
	}
	if m.databaseParamInfoLoad("missing", dbclient.SQLCall{}) != nil {
		t.Fatal("not a routine")
	}
	// A routine of another schema than the call names is skipped.
	cdbOpenFile(t, m, "q2.sql", "-- @conn pg\nSELECT other.total_of(1);\n")
	dbeCursor(t, m, 2, 22)
	m.databaseParamInfo()
	if !strings.Contains(m.confirm.Message(), "not a known built-in function") {
		t.Fatalf("%q", m.confirm.Message())
	}

	// Nothing to hint.
	cdbOpenFile(t, m, "q3.sql", "-- @conn pg\nSELECT 1;\n")
	m.databaseInsertHints()
	dbeToastHas(t, m, "No INSERT column hints")
	m.databaseJoinHints()
	dbeToastHas(t, m, "No JOIN cardinality hints")
}

// K in the tree: the node's doc, then the first rows of a table.
func TestCovDBQuickDocTree(t *testing.T) {
	m, sess := dbeModel(t)
	m.focus = FocusExplorer
	m.activity.SetActive(activity.ViewDatabases)
	s := m.databases
	s.databaseSelectPath("pg/db/app/schema/public/Tables/orders")
	cmd := m.databaseQuickDoc()
	if !m.confirmOpen || m.confirm.Title() != "Table public.orders" {
		t.Fatalf("card %q", m.confirm.Title())
	}
	msgs := cdbMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("%v", msgs)
	}
	m.onDBQuickDocRows(msgs[0].(dbQuickDocRowsMsg))
	if body := m.confirm.Message(); !strings.Contains(body, "First 2 rows:") || !strings.Contains(body, "note=first") {
		t.Fatalf("rows not added:\n%s", body)
	}
	m.onDBQuickDocRows(dbQuickDocRowsMsg{gen: s.docGen, result: dbclient.Result{Err: errors.New("denied")}})
	if body := m.confirm.Message(); !strings.Contains(body, "First rows: denied") {
		t.Fatalf("%s", body)
	}
	if m.onDBQuickDocRows(dbQuickDocRowsMsg{gen: s.docGen + 1}) != nil {
		t.Fatal("stale")
	}
	sess.roErr = errors.New("ro")
	got := cdbMsgs(m.databaseQuickDocRowsLoad("pg/db/app/schema/public/Tables/orders"))
	if len(got) != 1 || got[0].(dbQuickDocRowsMsg).result.Err == nil {
		t.Fatalf("%v", got)
	}
	// Not connected: the card says so and nothing is read.
	delete(s.sessions, "pg")
	if cmd := m.databaseQuickDocTree(); cmd != nil || !strings.Contains(m.confirm.Message(), "Connect to see the first rows.") {
		t.Fatalf("%q", m.confirm.Message())
	}
	if m.databaseQuickDocRowsLoad("pg/db/app") != nil {
		t.Fatal("not a table")
	}
	// A row with no doc / no row.
	s.databaseSelectPath("pg/db/app/schema/public/Tables")
	cdbAddNode(s, "", "pg/blank", dbNodeNote, "x")
	delete(s.nodes, "pg/blank")
	m.databaseQuickDocTree()
	s.cursor = -1
	m.databaseQuickDocTree()
	dbeToastHas(t, m, "Select an item first")
}

// gK on a result shows the cell's whole value and its row.
func TestCovDBCellDocOnThePage(t *testing.T) {
	m, _ := dbeModel(t)
	m.databaseCellDoc(0, 0)
	dbeToastHas(t, m, "No result yet")
	dbeOpen(t, m, "orders")
	m.databaseCellDoc(0, 2)
	if !m.confirmOpen || m.confirm.Title() != "Cell note · row 1" {
		t.Fatalf("card %q", m.confirm.Title())
	}
	m.databaseCellDoc(9, 0)
	dbeToastHas(t, m, "Put the cursor on a cell")
	s := m.dbConsole()
	s.trans, s.trow = true, 1
	m.databaseCellDoc(2, 0)
	if m.confirm.Title() != "Cell note · row 2" {
		t.Fatalf("transposed card %q", m.confirm.Title())
	}
	s.trans = false
	s.shown.tv = nil
	m.databaseCellDoc(1, 1)
	if !strings.HasPrefix(m.confirm.Title(), "Cell customer_id · row 2") || !strings.Contains(m.confirm.Message(), "Value: NULL") {
		t.Fatalf("%q %q", m.confirm.Title(), m.confirm.Message())
	}
	if _, lines := dbCellDoc(nil, nil, 3, 1); !strings.Contains(strings.Join(lines, "\n"), "Value: NULL") {
		t.Fatalf("%q", lines)
	}
}

func TestCovDBUniqueIndexesAndFKTo(t *testing.T) {
	s := dbJoinTestState()
	g := "pg/db/shop/schema/sales/Tables"
	s.setUniqueIndexes(g+"/users", nil)
	if _, ok := s.uniques[g+"/users"]; ok {
		t.Fatal("an empty set stayed")
	}
	if dbFKTo(s, g+"/lines", []string{"a", "b"}, "users", []string{"id"}) {
		t.Fatal("a two-column key")
	}
	s.nodes[g+"/lines/fk/bad"] = dbNode{kind: dbNodeDetail, name: "Foreign key · nonsense", conn: "pg"}
	s.nodes[g+"/lines/fk/q"] = dbNode{kind: dbNodeDetail, name: "Foreign key · order_id → sales.orders.id", conn: "pg"}
	ln := s.nodes[g+"/lines"]
	ln.children = append([]string{g + "/lines/fk/bad", g + "/lines/fk/q"}, ln.children...)
	s.nodes[g+"/lines"] = ln
	if !dbFKTo(s, g+"/lines", []string{"order_id"}, "orders", []string{"id"}) {
		t.Fatal("a schema-qualified reference was missed")
	}
	if !dbFKTo(s, g+"/lines", []string{"user_id"}, "users", []string{"id"}) {
		t.Fatal("the plain reference was missed")
	}
}
