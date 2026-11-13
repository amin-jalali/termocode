package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// cdbSchemaSession answers ListSchemas and the five catalog reads of a
// PostgreSQL schema with customers and orders tables.
func cdbSchemaSession(listErr, canonErr bool, noSchemas bool) *cdbSession {
	var n atomic.Int32
	return &cdbSession{run: func(q string) dbclient.Result {
		switch {
		case strings.Contains(q, "current_database"):
			return dbclient.Result{Rows: [][]dbclient.Cell{{cdbCell("app")}}}
		case strings.HasPrefix(q, "SELECT nspname"):
			if listErr {
				return dbclient.Result{Err: errors.New("no catalog")}
			}
			if noSchemas {
				return dbclient.Result{}
			}
			return dbclient.Result{Rows: [][]dbclient.Cell{{cdbCell("public")}}}
		}
		if canonErr {
			return dbclient.Result{Err: errors.New("catalog denied")}
		}
		switch n.Add(1) % 5 {
		case 1:
			return dbclient.Result{Rows: [][]dbclient.Cell{
				{cdbCell("customers"), cdbCell("id"), cdbCell("integer"), cdbCell("t"), cdbCell(""), cdbCell("")},
				{cdbCell("orders"), cdbCell("id"), cdbCell("integer"), cdbCell("t"), cdbCell(""), cdbCell("")},
				{cdbCell("orders"), cdbCell("customer_id"), cdbCell("integer"), cdbCell("f"), cdbCell(""), cdbCell("")}}}
		case 2:
			return dbclient.Result{Rows: [][]dbclient.Cell{
				{cdbCell("orders"), cdbCell("orders_customer_fk"), cdbCell("f"), cdbCell("customer_id"), cdbCell("customers"), cdbCell("id"), cdbCell("a"), cdbCell("a"), cdbCell("")}}}
		}
		return dbclient.Result{}
	}}
}

func cdbSchemaModel(t *testing.T, sess *cdbSession) *Model {
	t.Helper()
	m := cdbRealModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432}, dbclient.Conn{ID: "mongo", Driver: "redis", Host: "h"})
	cdbPGTree(m.databases)
	m.databases.sessions["pg"] = sess
	return m
}

// cdbSchemaStart picks action on the public schema and submits folder.
func cdbSchemaStart(t *testing.T, m *Model, path, action, folder string) []interface{} {
	t.Helper()
	m.databases.databaseSelectPath(path)
	m.databaseSchemaFilesStart()
	if !m.pickerOpen || m.pickerKind != pickerKindDBSchemaFiles {
		t.Fatalf("no schema picker: %q", covToastText(m))
	}
	cdbPick(t, m, action)
	if !m.promptOpen || m.promptKind != promptKindDBSchemaFolder {
		t.Fatal("no folder prompt")
	}
	m.promptOpen = false
	cmd := m.databaseSchemaFolderEntered(folder)
	var out []interface{}
	for _, msg := range cdbMsgs(cmd) {
		out = append(out, msg)
	}
	return out
}

func cdbSchemaMsg(t *testing.T, msgs []interface{}) dbSchemaFilesMsg {
	t.Helper()
	for _, m := range msgs {
		if sm, ok := m.(dbSchemaFilesMsg); ok {
			return sm
		}
	}
	t.Fatalf("no schema message in %v", msgs)
	return dbSchemaFilesMsg{}
}

func TestCovDBSchemaFolderHelpers(t *testing.T) {
	if got := dbSchemaFolderPath("/w", " out "); got != "/w/out" {
		t.Fatal(got)
	}
	if got := dbSchemaFolderPath("/w", "/abs/../x"); got != "/x" {
		t.Fatal(got)
	}
	if got := dbSchemaFolderDefault(dbCompareSide{conn: "my pg", schema: "pub/lic"}); got != "schema/my_pg-pub_lic" {
		t.Fatal(got)
	}
	if got := dbSchemaFolderDefault(dbCompareSide{conn: ".hid"}); got != "schema/hid" {
		t.Fatal(got)
	}
	items := dbSchemaCompareItems([]dbclient.SchemaFileDiff{
		{Name: "tables/a.sql", State: dbclient.SchemaFileChanged},
		{Name: "tables/b.sql", State: dbclient.SchemaFileIdentical},
	}, map[string]bool{"tables/a.sql": true})
	if len(items) != 4 || !strings.HasPrefix(items[1].Title, "[x] ") || strings.HasPrefix(items[3].Title, "[") {
		t.Fatalf("%+v", items)
	}
}

func TestCovDBSchemaFilesStartGuards(t *testing.T) {
	m := cdbSchemaModel(t, cdbSchemaSession(false, false, false))
	s := m.databases
	toastHas := func(w string) {
		t.Helper()
		if !strings.Contains(covToastText(m), w) {
			t.Fatalf("toast lacks %q: %q", w, covToastText(m))
		}
	}
	s.cursor = 99
	m.databaseSchemaFilesStart()
	toastHas("Select a schema")
	s.databaseSelectPath("mongo")
	m.databaseSchemaFilesStart()
	toastHas("Not a SQL database")
	s.databaseSelectPath("pg/db/app/schema/public")
	delete(s.sessions, "pg")
	m.databaseSchemaFilesStart()
	toastHas("Not connected")
	s.sessions["pg"] = cdbSchemaSession(false, false, false)
	// a migration tool in the project adds a note
	if err := os.WriteFile(filepath.Join(s.root, "flyway.conf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.databaseSchemaFilesStart()
	if !m.pickerOpen {
		t.Fatal("no picker")
	}
	m.pickerOpen = false
	// routing
	m.pickerKind = 0
	if _, ok := m.databaseSchemaFilesPicker("x"); ok {
		t.Fatal("foreign picker")
	}
	m.pickerKind = pickerKindDBSchemaFiles
	s.schemaFiles = nil
	if cmd, ok := m.databaseSchemaFilesPicker("dump"); !ok || cmd != nil {
		t.Fatal("no flow")
	}
	if cmd := m.databaseSchemaFolderEntered("x"); cmd != nil {
		t.Fatal("no flow")
	}
	m.confirmKind = confirmKindDBSchemaFiles
	if cmd, ok := m.databaseSchemaFilesConfirm("dump"); !ok || cmd != nil {
		t.Fatal("no flow")
	}
	m.confirmKind = 0
	if _, ok := m.databaseSchemaFilesConfirm("dump"); ok {
		t.Fatal("foreign card")
	}
	if cmd := m.onDBSchemaFiles(dbSchemaFilesMsg{}); cmd != nil {
		t.Fatal("no flow")
	}
	// MySQL: the database is the schema
	m2 := cdbModel(t, dbclient.Conn{ID: "my", Driver: "mysql", Host: "h", Port: 3306})
	cdbAddNode(m2.databases, "my", "my/db/shop", dbNodeDatabase, "shop")
	m2.databases.sessions["my"] = &cdbSession{}
	m2.databases.databaseSelectPath("my/db/shop")
	m2.databaseSchemaFilesStart()
	if m2.databases.schemaFiles.side.schema != "shop" {
		t.Fatalf("%+v", m2.databases.schemaFiles.side)
	}
}

func TestCovDBSchemaDumpAndOverwrite(t *testing.T) {
	m := cdbSchemaModel(t, cdbSchemaSession(false, false, false))
	s := m.databases
	msgs := cdbSchemaStart(t, m, "pg/db/app/schema/public", "dump", "out")
	sm := cdbSchemaMsg(t, msgs)
	if sm.err != nil || len(sm.files) == 0 {
		t.Fatalf("%+v", sm)
	}
	m.onDBSchemaFiles(sm)
	if !strings.Contains(covToastText(m), "Wrote") {
		t.Fatalf("toast %q", covToastText(m))
	}
	dir := filepath.Join(s.root, "out")
	if _, err := os.Stat(filepath.Join(dir, sm.files[0].Name)); err != nil {
		t.Fatal(err)
	}
	// stale answers are dropped
	old := sm
	old.gen--
	if cmd := m.onDBSchemaFiles(old); cmd != nil {
		t.Fatal("stale")
	}
	// another dump over changed files asks first
	if err := os.WriteFile(filepath.Join(dir, sm.files[0].Name), []byte("-- edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sm = cdbSchemaMsg(t, cdbSchemaStart(t, m, "pg/db/app/schema/public", "dump", "out"))
	m.onDBSchemaFiles(sm)
	if !m.confirmOpen || m.confirmKind != confirmKindDBSchemaFiles {
		t.Fatal("no overwrite card")
	}
	cdbConfirm(t, m, "dump")
	if b, _ := os.ReadFile(filepath.Join(dir, sm.files[0].Name)); strings.Contains(string(b), "edited") {
		t.Fatal("not overwritten")
	}
	// an unwritable target
	m.databases.schemaFiles.dir = filepath.Join(dir, sm.files[0].Name) // a file
	m.databases.schemaFiles.files = sm.files
	m.databaseSchemaDump()
	// errors in the message
	m.onDBSchemaFiles(dbSchemaFilesMsg{gen: m.databases.schemaFiles.gen, err: errors.New("denied")})
	// an unreadable folder at dump time
	m.databases.schemaFiles.action = "dump"
	m.databases.schemaFiles.dir = filepath.Join(dir, "tables", "nofolder")
	_ = os.WriteFile(filepath.Join(dir, "tables", "nofolder"), []byte("x"), 0o644)
	m.onDBSchemaFiles(dbSchemaFilesMsg{gen: m.databases.schemaFiles.gen, schema: "public", files: sm.files})
}

func TestCovDBSchemaFolderEnteredErrors(t *testing.T) {
	// schema unknown: listed from the server; no schema; list error; canon error
	for _, tc := range []struct {
		name string
		sess *cdbSession
		want string
	}{
		{"none", cdbSchemaSession(false, false, true), "lists no schema"},
		{"list", cdbSchemaSession(true, false, false), "no catalog"},
		{"canon", cdbSchemaSession(false, true, false), "catalog denied"},
	} {
		m := cdbSchemaModel(t, tc.sess)
		sm := cdbSchemaMsg(t, cdbSchemaStart(t, m, "pg", "dump", "out"))
		if sm.err == nil || !strings.Contains(sm.err.Error(), tc.want) {
			t.Fatalf("%s: %v", tc.name, sm.err)
		}
		m.onDBSchemaFiles(sm)
		// the session says the read-only session fails
		tc.sess.roErr = errors.New("ro")
		sm = cdbSchemaMsg(t, cdbSchemaStart(t, m, "pg", "dump", "out"))
		if sm.err == nil {
			t.Fatal("ro error")
		}
	}
	// the schema is listed when the cursor is on the connection
	m := cdbSchemaModel(t, cdbSchemaSession(false, false, false))
	sm := cdbSchemaMsg(t, cdbSchemaStart(t, m, "pg", "dump", "out"))
	if sm.err != nil || sm.schema != "public" {
		t.Fatalf("%+v", sm)
	}
	// empty answer / session gone
	m.databases.schemaFiles.action = "dump"
	if cmd := m.databaseSchemaFolderEntered("  "); cmd != nil {
		t.Fatal("empty folder")
	}
	delete(m.databases.sessions, "pg")
	m.databaseSchemaFolderEntered("out")
	if !strings.Contains(covToastText(m), "Not connected") {
		t.Fatalf("toast %q", covToastText(m))
	}
}

func TestCovDBSchemaCompareFlow(t *testing.T) {
	m := cdbSchemaModel(t, cdbSchemaSession(false, false, false))
	s := m.databases
	dir := filepath.Join(s.root, "snap")
	// the folder has a changed file, a folder-only file and the rest missing
	if err := os.MkdirAll(filepath.Join(dir, "tables"), 0o755); err != nil {
		t.Fatal(err)
	}
	sm := cdbSchemaMsg(t, cdbSchemaStart(t, m, "pg/db/app/schema/public", "compare", "snap"))
	if sm.err != nil || len(sm.files) < 2 {
		t.Fatalf("%+v", sm)
	}
	// write one identical, one changed, one folder-only file
	if _, err := dbclient.WriteSchemaFolder(dir, sm.files[:1]); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, sm.files[1].Name)), 0o755)
	if err := os.WriteFile(filepath.Join(dir, sm.files[1].Name), []byte("-- other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tables", "ghost.sql"), []byte("CREATE TABLE ghost (id int);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sm = cdbSchemaMsg(t, cdbSchemaStart(t, m, "pg/db/app/schema/public", "compare", "snap"))
	m.onDBSchemaFiles(sm)
	if !m.pickerOpen || m.pickerKind != pickerKindDBSchemaCompare {
		t.Fatalf("no compare picker: %q", covToastText(m))
	}
	st := s.schemaFiles
	if len(st.diffs) < 3 {
		t.Fatalf("diffs %+v", st.diffs)
	}
	// toggle a row, an identical row does not toggle, an unknown id too
	var changed, identical, ghost string
	for _, d := range st.diffs {
		switch d.State {
		case dbclient.SchemaFileChanged:
			changed = d.Name
		case dbclient.SchemaFileIdentical:
			identical = d.Name
		case dbclient.SchemaFileFolderOnly:
			ghost = d.Name
		}
	}
	m.databaseSchemaCompareToggle(changed)
	m.databaseSchemaCompareToggle(identical)
	m.databaseSchemaCompareToggle("nope")
	if !st.selected[changed] || st.selected[identical] {
		t.Fatalf("selected %v", st.selected)
	}
	// Enter with a selection: the card; Update the folder
	cdbPick(t, m, changed)
	if !m.confirmOpen {
		t.Fatal("no card")
	}
	cdbConfirm(t, m, "to-folder")
	if !strings.Contains(covToastText(m), "Updated") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// Enter on the identical row: nothing to change
	m.databaseSchemaCompareOpen("")
	st.selected = map[string]bool{}
	m.pickerKind, m.pickerOpen = pickerKindDBSchemaCompare, true
	cdbPick(t, m, identical)
	if !strings.Contains(covToastText(m), "Nothing to change") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// the cursor row (a folder-only file) opens the migration script
	m.pickerKind, m.pickerOpen = pickerKindDBSchemaCompare, true
	cdbPick(t, m, ghost)
	cdbConfirm(t, m, "from-folder")
	_, lines, _ := m.httpBufText(0)
	if !strings.Contains(strings.Join(lines, "\n"), "Review before you run it") {
		t.Fatalf("no script: %q", lines)
	}
	// cancel and an unknown button
	m.pickerKind, m.pickerOpen = pickerKindDBSchemaCompare, true
	cdbPick(t, m, ghost)
	cdbConfirm(t, m, "cancel")
	// no editor: the script is copied
	var copied string
	t.Cleanup(SetClipboardSink(func(x string) { copied = x }))
	m.pickerKind, m.pickerOpen = pickerKindDBSchemaCompare, true
	cdbPick(t, m, ghost)
	nv := m.nvim
	m.nvim = nil
	cdbConfirm(t, m, "from-folder")
	m.nvim = nv
	if !strings.Contains(copied, "Review before you run it") {
		t.Fatalf("copied %q", copied)
	}
	// a folder that cannot be updated
	m.pickerKind, m.pickerOpen = pickerKindDBSchemaCompare, true
	cdbPick(t, m, ghost)
	st.dir = filepath.Join(dir, "tables", "ghost.sql", "x")
	st.apply = append(st.apply, dbclient.SchemaFileDiff{Name: "../outside.sql", State: dbclient.SchemaFileLiveOnly})
	cdbConfirm(t, m, "to-folder")
	_ = identical
}

func TestCovDBSchemaFolderForCompletion(t *testing.T) {
	m := cdbSchemaModel(t, cdbSchemaSession(false, false, false))
	s := m.databases
	dir := filepath.Join(s.root, "ddl")
	if err := os.MkdirAll(filepath.Join(dir, "tables"), 0o755); err != nil {
		t.Fatal(err)
	}
	// an empty folder
	sm := cdbSchemaMsg(t, cdbSchemaStart(t, m, "pg/db/app/schema/public", "complete", "ddl"))
	if sm.err == nil || !strings.Contains(sm.err.Error(), "no tables/") {
		t.Fatalf("%+v", sm)
	}
	m.onDBSchemaFiles(sm)
	// a folder with a table and an unreadable statement
	if err := os.WriteFile(filepath.Join(dir, "tables", "users.sql"), []byte("CREATE TABLE users (id int, name text);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tables", "bad.sql"), []byte("CREATE TABLE ((((;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sm = cdbSchemaMsg(t, cdbSchemaStart(t, m, "pg/db/app/schema/public", "complete", "ddl"))
	if sm.err != nil || sm.ddl == nil || sm.tables < 1 {
		t.Fatalf("%+v", sm)
	}
	m.onDBSchemaFiles(sm)
	if !strings.Contains(covToastText(m), "Completion reads") {
		t.Fatalf("toast %q", covToastText(m))
	}
	if s.ddl["pg"] == nil {
		t.Fatal("no completion source")
	}
	sm.warns = nil
	m.onDBSchemaFiles(sm)
	// a missing folder reads as empty; a file in the way is an error
	_ = os.WriteFile(filepath.Join(dir, "blocked"), []byte("x"), 0o644)
	sm = cdbSchemaMsg(t, cdbSchemaStart(t, m, "pg/db/app/schema/public", "complete", "ddl/blocked"))
	if sm.err == nil {
		t.Fatalf("%+v", sm)
	}
	// the path helper
	if got := m.dbShortPath("/elsewhere"); got != "/elsewhere" {
		t.Fatal(got)
	}
	m.databases = nil
	if got := m.dbShortPath("/x"); got != "/x" {
		t.Fatal(got)
	}
}
