package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// cdbOpenFile writes name below the working folder and opens it in the editor.
func cdbOpenFile(t *testing.T, m *Model, name, text string) string {
	t.Helper()
	wd, _ := os.Getwd()
	file := filepath.Join(wd, name)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	m.openFileAtLine(file, 1)
	return file
}

func TestCovDBSQLOutlineAndSwitchFile(t *testing.T) {
	m := cdbRealModel(t,
		dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432},
		dbclient.Conn{ID: "mongo", Driver: "redis", Host: "h"})
	toastHas := func(w string) {
		t.Helper()
		if !strings.Contains(covToastText(m), w) {
			t.Fatalf("toast lacks %q: %q", w, covToastText(m))
		}
	}
	m.databaseSQLOutline()
	m.databaseSQLSwitchPicker()
	toastHas("Open a .sql file")
	file := cdbOpenFile(t, m, "q/report.sql", "-- @conn pg\nSELECT 1;\n\nSELECT 2;\n")
	m.databaseSQLOutline()
	if !m.pickerOpen || m.pickerKind != pickerKindDBSQLOutline {
		t.Fatalf("no outline: %q", covToastText(m))
	}
	cdbPick(t, m, "4")
	cdbPick2sql := func(kind pickerKindEnum, id string) {
		m.pickerKind, m.pickerOpen = kind, true
		cdbPick(t, m, id)
	}
	cdbPick2sql(pickerKindDBSQLOutline, "zz")
	// an empty file has no statements
	cdbOpenFile(t, m, "empty.sql", "")
	m.databaseSQLOutline()
	toastHas("No statements")
	// the switch picker
	m.openFileAtLine(file, 1)
	m.databaseSQLSwitchPicker()
	if !m.pickerOpen || m.pickerKind != pickerKindDBSQLSwitch {
		t.Fatal("no switch picker")
	}
	cdbPick(t, m, "pg")
	toastHas("runs on")
	if m.databases.console.attached[file] != "pg" {
		t.Fatal("not attached")
	}
	saved := dbLoadSQLAttach(m.databaseWorkspaceRoot())
	if saved[file] != "pg" {
		t.Fatalf("not saved: %v", saved)
	}
	// again: the file has a connection, so Detach is offered
	m.databaseSQLSwitchPicker()
	cdbPick(t, m, dbSQLDetach)
	toastHas("Detached")
	// no file asked for: nothing happens
	cdbPick2sql(pickerKindDBSQLSwitch, "pg")
	// an unwritable project: session only
	m.databaseSQLSwitchPicker()
	_ = os.RemoveAll(filepath.Join(m.databaseWorkspaceRoot(), ".termocode"))
	if err := os.WriteFile(filepath.Join(m.databaseWorkspaceRoot(), ".termocode"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cdbPick(t, m, "pg")
	toastHas("this session only")
	// no SQL connections at all
	m.databases.conns = []dbclient.Conn{{ID: "mongo", Driver: "redis"}}
	m.databaseSQLSwitchPicker()
	toastHas("No SQL connections")
	m.pickerKind = 0
	if _, ok := m.databaseSQLPicked("x"); ok {
		t.Fatal("foreign picker")
	}
	m.pickerKind = pickerKindDBSQLIntent
	m.databaseSQLPicked("x")
}

func TestCovDBSQLAttachFiles(t *testing.T) {
	if got := dbLoadSQLAttach(""); len(got) != 0 {
		t.Fatal("no root")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := dbSQLAttachPath(root)
	_ = os.WriteFile(p, []byte("{broken"), 0o644)
	if got := dbLoadSQLAttach(root); len(got) != 0 {
		t.Fatal("broken file")
	}
	_ = os.WriteFile(p, []byte(`{"a.sql":"pg","../up.sql":"pg","/abs.sql":"pg","empty.sql":""}`), 0o644)
	if got := dbLoadSQLAttach(root); len(got) != 1 || got[filepath.Join(root, "a.sql")] != "pg" {
		t.Fatalf("%v", got)
	}
	if err := dbSaveSQLAttach("", "x", "pg"); err == nil {
		t.Fatal("no root")
	}
	if err := dbSaveSQLAttach(root, filepath.Join(root, "..", "x.sql"), "pg"); err == nil {
		t.Fatal("outside")
	}
	if err := dbSaveSQLAttach(root, filepath.Join(root, "a.sql"), ""); err != nil {
		t.Fatal(err)
	}
	if got := dbLoadSQLAttach(root); len(got) != 0 {
		t.Fatalf("not removed: %v", got)
	}
	// merge: a choice made in this session wins
	s := &dbConsoleState{attached: map[string]string{filepath.Join(root, "b.sql"): "mine"}}
	_ = dbSaveSQLAttach(root, filepath.Join(root, "b.sql"), "saved")
	_ = dbSaveSQLAttach(root, filepath.Join(root, "c.sql"), "saved")
	s.mergeSQLAttach(root)
	if s.attached[filepath.Join(root, "b.sql")] != "mine" || s.attached[filepath.Join(root, "c.sql")] != "saved" {
		t.Fatalf("%v", s.attached)
	}
	// the directory is a file: cannot save
	bad := t.TempDir()
	_ = os.WriteFile(filepath.Join(bad, ".termocode"), []byte("x"), 0o644)
	if err := dbSaveSQLAttach(bad, filepath.Join(bad, "a.sql"), "pg"); err == nil {
		t.Fatal("unwritable")
	}
	if err := dbSaveSQLFolderDialect(bad, bad, "mysql"); err == nil {
		t.Fatal("unwritable")
	}
}

func TestCovDBSQLFolderDialects(t *testing.T) {
	root := t.TempDir()
	if got := dbLoadSQLFolderDialects(""); len(got) != 0 {
		t.Fatal("no root")
	}
	if dbSQLFolderDialect("", "x") != "" || dbSQLFolderDialect(root, "") != "" {
		t.Fatal("empty args")
	}
	if dbSQLFolderDialect(root, filepath.Join(root, "..", "x", "f.sql")) != "" {
		t.Fatal("outside")
	}
	if dbSQLFolderDialect(root, filepath.Join(root, "a", "f.sql")) != "" {
		t.Fatal("none saved")
	}
	if err := dbSaveSQLFolderDialect("", root, "mysql"); err == nil {
		t.Fatal("no root")
	}
	if err := dbSaveSQLFolderDialect(root, filepath.Join(root, ".."), "mysql"); err == nil {
		t.Fatal("outside")
	}
	if err := dbSaveSQLFolderDialect(root, filepath.Join(root, "a"), "oracle"); err == nil {
		t.Fatal("unknown dialect")
	}
	if err := dbSaveSQLFolderDialect(root, filepath.Join(root, "a"), "mysql"); err != nil {
		t.Fatal(err)
	}
	if err := dbSaveSQLFolderDialect(root, root, "sqlite"); err != nil {
		t.Fatal(err)
	}
	if got := dbSQLFolderDialect(root, filepath.Join(root, "a", "b", "f.sql")); got != "mysql" {
		t.Fatalf("nearest folder: %q", got)
	}
	if got := dbSQLFolderDialect(root, filepath.Join(root, "z", "f.sql")); got != "sqlite" {
		t.Fatalf("workspace folder: %q", got)
	}
	// without the "." entry a stray folder has none
	if err := dbSaveSQLFolderDialect(root, root, ""); err != nil {
		t.Fatal(err)
	}
	if got := dbSQLFolderDialect(root, filepath.Join(root, "z", "f.sql")); got != "" {
		t.Fatalf("%q", got)
	}
	_ = os.WriteFile(dbSQLFolderDialectsPath(root), []byte("{broken"), 0o644)
	if got := dbLoadSQLFolderDialects(root); len(got) != 0 {
		t.Fatal("broken")
	}
	_ = os.WriteFile(dbSQLFolderDialectsPath(root), []byte(`{"../up":"mysql","/abs":"mysql","ok":"nosuch","a":"postgres"}`), 0o644)
	if got := dbLoadSQLFolderDialects(root); len(got) != 1 || got["a"] != "postgres" {
		t.Fatalf("%v", got)
	}
	// the connection wins, then @dialect, the folder, the text
	for _, tc := range []struct{ src, drv, folder, want, how string }{
		{"select 1", "mysql", "", "mysql", "connection"},
		{"-- @dialect postgres\nx", "", "mysql", "postgres", "detected"},
		{"select 1", "", "sqlite", "sqlite", "folder"},
		{"select 1", "", "", "", "generic"},
	} {
		if d, how := dbSQLDialectFor(tc.src, tc.drv, tc.folder); d != tc.want || how != tc.how {
			t.Errorf("%+v: %q %q", tc, d, how)
		}
	}
}

func TestCovDBSQLFolderDialectPicker(t *testing.T) {
	m := cdbRealModel(t)
	m.databaseSQLFolderDialectPicker()
	if !strings.Contains(covToastText(m), "Open a .sql file") {
		t.Fatalf("toast %q", covToastText(m))
	}
	file := cdbOpenFile(t, m, "etl/load.sql", "select 1;\n")
	m.databaseSQLFolderDialectPicker()
	if !m.pickerOpen || m.pickerKind != pickerKindDBSQLDialect {
		t.Fatal("no dialect picker")
	}
	folder := filepath.Dir(file)
	cdbPick(t, m, "mysql\x00"+folder)
	if !strings.Contains(covToastText(m), "uses mysql") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.databaseSQLFolderDialectPicker() // now with a current dialect: Clear is offered
	cdbPick(t, m, dbSQLDialectClear+"\x00"+folder)
	if !strings.Contains(covToastText(m), "Cleared the dialect") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.pickerKind, m.pickerOpen = pickerKindDBSQLDialect, true
	cdbPick(t, m, "nonsense")
	m.pickerKind, m.pickerOpen = pickerKindDBSQLDialect, true
	cdbPick(t, m, "oracle\x00"+folder)
	if !strings.Contains(covToastText(m), "Dialect not saved") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// a file outside the workspace
	outside := filepath.Join(t.TempDir(), "o.sql")
	_ = os.WriteFile(outside, []byte("select 1;\n"), 0o644)
	m.openFileAtLine(outside, 1)
	m.databaseSQLFolderDialectPicker()
	if !strings.Contains(covToastText(m), "Not in the workspace") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.pickerKind = 0
	if _, ok := m.databaseSQLMorePicked("x"); ok {
		t.Fatal("foreign picker")
	}
}

func TestCovDBSQLUsagesAndEmbeddedSQL(t *testing.T) {
	m := cdbRealModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432})
	toastHas := func(w string) {
		t.Helper()
		if !strings.Contains(covToastText(m), w) {
			t.Fatalf("toast lacks %q: %q", w, covToastText(m))
		}
	}
	// the name under the cursor: a table used twice in the file
	cdbOpenFile(t, m, "u.sql", "-- @conn pg\nSELECT * FROM orders;\nSELECT id FROM orders o WHERE o.id = 1;\nSELECT 1;\n")
	cdbCursor := func(row, col int) {
		t.Helper()
		if err := m.nvim.ExecLuaArgs(`local r, c = ...; vim.api.nvim_win_set_cursor(0, {r, c})`, row, col); err != nil {
			t.Fatal(err)
		}
	}
	cdbCursor(2, 16)
	m.databaseHighlightUsages()
	toastHas("usages of orders")
	// an alias resolves in its statement
	cdbCursor(3, 21)
	m.databaseHighlightUsages()
	// a name nobody uses elsewhere / no name
	cdbCursor(4, 0)
	m.databaseHighlightUsages()
	cdbCursor(1, 0)
	m.databaseHighlightUsages()
	// no connection in the file: the dialect comes from the text
	cdbOpenFile(t, m, "v.sql", "SELECT * FROM t JOIN t2 ON 1=1;\n")
	cdbCursor(1, 15)
	m.databaseHighlightUsages()
	// not a SQL file
	cdbOpenFile(t, m, "x.txt", "orders\n")
	m.databaseHighlightUsages()
	// embedded SQL
	m.databaseEmbeddedSQL()
	toastHas("Open a code file")
	cdbOpenFile(t, m, "main.go", "package main\n\nfunc q() string {\n\treturn \"SELECT id, name FROM users WHERE id = 1\"\n}\n")
	m.databaseEmbeddedSQL()
	if !m.pickerOpen || m.pickerKind != pickerKindDBSQLEmbedded {
		t.Fatalf("no picker: %q", covToastText(m))
	}
	cdbPick(t, m, "4")
	m.pickerKind, m.pickerOpen = pickerKindDBSQLEmbedded, true
	cdbPick(t, m, "x")
	cdbOpenFile(t, m, "plain.go", "package main\n")
	m.databaseEmbeddedSQL()
	toastHas("No SQL in strings")
	items := dbEmbeddedSQLItems([]dbclient.EmbeddedSQL{{Line: 7, Kind: "go", Text: strings.Repeat("select a ", 20)}})
	if len([]rune(items[0].Title)) > dbEmbeddedLabelMax || items[0].ID != "7" {
		t.Fatalf("%+v", items)
	}
}

func TestCovDBSQLSyntaxOffAndDiags(t *testing.T) {
	s := dbConsoleSettings{SyntaxOff: []string{"", " PostgreSQL ", "mariadb", "weird"}}
	for d, want := range map[string]bool{"postgres": true, "postgresql": true, "mysql": true, "sqlite": false, "weird": true, "other": false} {
		if got := s.syntaxOff(d); got != want {
			t.Errorf("%s: %v", d, got)
		}
	}
	diags := []map[string]any{{"severity": dbDiagError}, {"severity": 2}, {"x": 1}}
	if got := dbDropSyntaxDiags(diags); len(got) != 2 {
		t.Fatalf("%v", got)
	}
	if got := dbDropSyntaxDiags(nil); got == nil {
		t.Fatal("nil result")
	}
	spans, note := dbSQLUsageSpans("WITH c AS (SELECT 1) SELECT * FROM c;", "postgres", "c", 30)
	if len(spans) == 0 || !strings.Contains(note, "CTE") {
		t.Fatalf("%v %q", spans, note)
	}
	spans, note = dbSQLUsageSpans("SELECT * FROM orders o;", "postgres", "o", 20)
	if len(spans) == 0 || !strings.Contains(note, "alias") {
		t.Fatalf("%v %q", spans, note)
	}
	if _, note = dbSQLUsageSpans("SELECT * FROM orders;", "postgres", "orders", 999); note != "in this file" {
		t.Fatalf("%q", note)
	}
}
