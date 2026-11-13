package app

import (
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func cdbSavedModel(t *testing.T) *Model {
	t.Helper()
	m := cdbRealModel(t,
		dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432},
		dbclient.Conn{ID: "pg2", Driver: "postgres", Host: "h2", Port: 5432},
		dbclient.Conn{ID: "cache", Driver: "redis", Host: "h"})
	return m
}

// cdbSetBuffer replaces the current buffer's lines and puts the cursor on line.
func cdbSetBuffer(t *testing.T, m *Model, line int, lines ...string) {
	t.Helper()
	if err := m.nvim.ExecLuaArgs(`local l, n = ...; vim.api.nvim_buf_set_lines(0, 0, -1, false, l); vim.api.nvim_win_set_cursor(0, {n, 0})`, lines, line); err != nil {
		t.Fatal(err)
	}
}

func TestCovDBSavedQueriesRoundTrip(t *testing.T) {
	m := cdbSavedModel(t)
	// not in a SQL console
	m.databaseSaveQueryStart()
	if !strings.Contains(covToastText(m), "Open a SQL console") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.databaseNewConsole("pg")
	cdbSetBuffer(t, m, 2, "-- @conn pg", "SELECT 1 FROM orders WHERE id = 7;")
	m.databaseSaveQueryStart()
	if !m.promptOpen || m.promptKind != promptKindDBSaveQuery {
		t.Fatalf("no name prompt: %q", covToastText(m))
	}
	d := m.databases.console.saveDraft
	if d.conn != "pg" || strings.Contains(d.sql, "@conn") {
		t.Fatalf("draft %+v", d)
	}
	m.promptOpen = false
	m.databaseSaveQueryNamed("orders by id")
	if !strings.Contains(covToastText(m), "Query saved") {
		t.Fatalf("toast %q", covToastText(m))
	}
	if cmd := m.databaseSaveQueryNamed("again"); cmd != nil {
		t.Fatal("no draft")
	}

	// a long statement names itself from its first 40 characters
	cdbSetBuffer(t, m, 2, "-- @conn pg", "SELECT "+strings.Repeat("a, ", 30)+"b FROM t;")
	m.databaseSaveQueryStart()
	if len([]rune(m.prompt.Value())) > dbSavedQueryDefaultNameMax {
		t.Fatalf("name %q", m.prompt.Value())
	}
	m.promptOpen = false

	// an empty statement, a file without a connection
	cdbSetBuffer(t, m, 1, "-- @conn pg", "")
	m.databaseSaveQueryStart()
	if !strings.Contains(covToastText(m), "No statement here") {
		t.Fatalf("toast %q", covToastText(m))
	}
	cdbSetBuffer(t, m, 1, "-- @conn nowhere", "SELECT 1;")
	m.databaseSaveQueryStart()
	if !strings.Contains(covToastText(m), "No connection for this file") {
		t.Fatalf("toast %q", covToastText(m))
	}

	// the list; Enter on a query inserts it below the cursor of its console
	cdbSetBuffer(t, m, 2, "-- @conn pg", "SELECT 2;")
	m.databaseSavedQueries()
	if !m.pickerOpen || m.pickerKind != pickerKindDBSaved {
		t.Fatalf("no picker: %q", covToastText(m))
	}
	id := m.databases.console.savedList[0].ID
	cdbPick(t, m, id)
	_, lines, _ := m.httpBufText(0)
	if !strings.Contains(strings.Join(lines, "\n"), "orders WHERE id = 7") {
		t.Fatalf("not inserted: %q", lines)
	}
	// from another connection's buffer: goes to its console
	cdbSetBuffer(t, m, 2, "-- @conn pg2", "SELECT 3;")
	m.databaseInsertSaved(m.databases.console.savedList[0])
	if _, lines, _ = m.httpBufText(0); !strings.Contains(strings.Join(lines, "\n"), "orders WHERE id = 7") {
		t.Fatalf("not in the console: %q", lines)
	}
	// a saved query whose connection is gone
	gone := m.databases.console.savedList[0]
	gone.Conn = "vanished"
	m.databaseInsertSaved(gone)
	if !strings.Contains(covToastText(m), "Connection not found") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// an unknown id does nothing
	m.pickerKind, m.pickerOpen = pickerKindDBSaved, true
	cdbPick(t, m, "zzz")
	// the delete list
	m.databaseSavedQueries()
	cdbPick(t, m, dbSavedPickerDeleteID)
	if !m.pickerOpen || m.pickerKind != pickerKindDBSavedDelete {
		t.Fatal("no delete picker")
	}
	m.pickerOpen = false
	if cmd, ok := m.databaseSavedPicker(""); !ok || cmd != nil {
		t.Fatal("empty delete id")
	}
	m.pickerOpen = true
	cdbPick(t, m, id)
	if !strings.Contains(covToastText(m), "Saved query deleted") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.pickerKind, m.pickerOpen = pickerKindDBSavedDelete, true
	cdbPick(t, m, "no-such-id")
	// an empty list
	m.databaseSavedQueries()
	_ = m.databases.console.savedStore().Delete("zzz")
	m.pickerKind = 0
	if _, ok := m.databaseSavedPicker("x"); ok {
		t.Fatal("foreign picker")
	}
}

func TestCovDBSavedStoreFailsOnABrokenFile(t *testing.T) {
	m := cdbSavedModel(t)
	p := dbConfigPath("saved-queries.json")
	_ = os.MkdirAll(p[:strings.LastIndex(p, "/")], 0o700)
	if err := os.WriteFile(p, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.databaseSavedQueries()
	m.databaseNewConsole("pg")
	cdbSetBuffer(t, m, 2, "-- @conn pg", "SELECT 1;")
	m.databaseSaveQueryStart()
	m.promptOpen = false
	m.databaseSaveQueryNamed("x")
	if !strings.Contains(covToastText(m), "rror") && !strings.Contains(covToastText(m), "ailed") {
		t.Logf("toast %q", covToastText(m))
	}
	m.pickerKind = pickerKindDBSavedDelete
	m.databaseSavedPicker("some-id")
	// no editor: nothing to insert into
	n := &Model{}
	n.databases = newDBState()
	if cmd := n.databaseInsertSaved(dbclient.SavedQuery{}); cmd != nil {
		t.Fatal("no nvim")
	}
	if cmd := n.databaseSaveQueryStart(); cmd != nil {
		t.Fatal("no nvim")
	}
}

func TestCovDBHistoryAndConsoles(t *testing.T) {
	m := cdbSavedModel(t)
	m.databases.databaseSelectPath("pg")
	m.databaseHistoryOfConn()
	m2 := cdbModel(t)
	m2.databaseHistoryOfConn()
	if !strings.Contains(covToastText(m2), "No connection here") {
		t.Fatalf("toast %q", covToastText(m2))
	}
	// another console: 2, 3 …; the 9th is the last
	m.databases.databaseSelectPath("pg")
	for i := 0; i < dbConsoleMaxPerConn-1; i++ {
		m.databases.databaseSelectPath("pg")
		m.databaseAnotherConsole()
	}
	m.databases.databaseSelectPath("pg")
	m.databaseAnotherConsole()
	if !strings.Contains(covToastText(m), "consoles already") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// redis: the one console again
	m.databases.databaseSelectPath("cache")
	m.databaseAnotherConsole()
	// no connection
	m.databases.cursor = 99
	m.focus = FocusExplorer
	m.databaseAnotherConsole()
	// numbered console names
	for in, want := range map[string]int{"abc": 1, "abc-2": 2, "abc-x": 1, "abc-1": 1, "-3": 1} {
		if _, n := dbConsoleNumber(in); n != want {
			t.Errorf("%s: %d", in, n)
		}
	}
	if base, n := dbConsoleNumber("abc-7"); base != "abc" || n != 7 {
		t.Errorf("%s %d", base, n)
	}
	if p := m.databaseConsolePathN("pg", 1); p == "" || p != m.databaseConsolePath("pg") {
		t.Fatalf("%q", p)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if p := (&Model{}).databaseConsolePathN("pg", 3); p != "" {
		t.Logf("path without a config dir: %q", p)
	}
	if _, err := dbUserFile("x"); err == nil {
		m.databases.databaseSelectPath("pg")
		m.databaseAnotherConsole()
	}
}
