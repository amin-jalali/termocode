package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dbeBufLines is the text of the editor's current buffer.
func dbeBufLines(t *testing.T, m *Model) string {
	t.Helper()
	_, lines, _ := m.httpBufText(0)
	return strings.Join(lines, "\n")
}

// Rename in SQL: the prompt, the card that counts the places, and the
// writes to the buffer and the other project files; an unsaved buffer
// and a file that cannot be written are named and left alone.
func TestCovDBRenameFlow(t *testing.T) {
	m := cdbRealModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432})
	wd, _ := os.Getwd()

	m.databaseRenameStart()
	dbeToastHas(t, m, "Open a SQL file")

	write := func(name, text string) string {
		p := filepath.Join(wd, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for i := 0; i < 9; i++ {
		write(fmt.Sprintf("more/m%d.sql", i), "SELECT 1 FROM users;\n")
	}
	dirty := write("dirty.sql", "SELECT * FROM users;\n")
	locked := write("locked/l.sql", "DELETE FROM users;\n")
	cur := cdbOpenFile(t, m, "cur.sql", "SELECT * FROM users u WHERE u.note = 'users';\n")

	// The cursor on a blank: no name.
	_ = m.nvim.ExecLua(`vim.api.nvim_win_set_cursor(0, {1, 8})`)
	m.databaseRenameStart()
	dbeToastHas(t, m, "No name under the cursor")

	_ = m.nvim.ExecLua(`vim.api.nvim_win_set_cursor(0, {1, 15})`)
	m.databaseRenameStart()
	if !m.promptOpen || m.promptKind != promptKindDBSQLRename || m.dbConsole().rename == nil || m.dbConsole().rename.old != "users" {
		t.Fatalf("no rename prompt: %v %+v", m.promptOpen, m.dbConsole().rename)
	}
	cdbSubmit(t, m, "users")
	dbeToastHas(t, m, "Rename cancelled")
	if m.dbConsole().rename != nil {
		t.Fatal("the rename stayed")
	}

	m.databaseRenameStart()
	cdbSubmit(t, m, "customers")
	if !m.confirmOpen || m.confirmKind != confirmKindDBRename {
		t.Fatal("no rename card")
	}
	card := m.confirm.Message()
	for _, want := range []string{"1 in this file", "11 in 11 other project .sql files"} {
		if !strings.Contains(strings.Join(strings.Fields(card), " "), want) {
			t.Fatalf("card lacks %q:\n%s", want, card)
		}
	}

	// dirty.sql is open with unsaved changes; locked/ cannot take a file.
	m.openFileAtLine(dirty, 1)
	_ = m.nvim.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'SELECT * FROM users; -- mine'})`)
	m.openFileAtLine(cur, 1)
	if err := os.Chmod(filepath.Dir(locked), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(locked), 0o755) })

	cdbConfirm(t, m, "rename")
	if got := dbeBufLines(t, m); got != "SELECT * FROM customers u WHERE u.note = 'users';" {
		t.Fatalf("buffer %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, "more/m3.sql")); string(b) != "SELECT 1 FROM customers;\n" {
		t.Fatalf("m3.sql %q", b)
	}
	if b, _ := os.ReadFile(dirty); string(b) != "SELECT * FROM users;\n" {
		t.Fatalf("the unsaved file was written: %q", b)
	}
	if b, _ := os.ReadFile(locked); string(b) != "DELETE FROM users;\n" {
		t.Fatalf("locked %q", b)
	}
	toast := cdbToast(m)
	for _, want := range []string{"Renamed users to customers in 10 places (10 files)", "Unsaved, left alone: dirty.sql", "Could not write: l.sql"} {
		if !strings.Contains(toast, want) {
			t.Fatalf("toast lacks %q: %q", want, toast)
		}
	}

	// A clean rename: one toast, no warning.
	_ = os.Chmod(filepath.Dir(locked), 0o755)
	_ = m.nvim.ExecLua(`vim.cmd('silent! bufdo set nomodified')`)
	m.openFileAtLine(cur, 1)
	_ = m.nvim.ExecLua(`vim.api.nvim_win_set_cursor(0, {1, 15})`)
	m.databaseRenameStart()
	cdbSubmit(t, m, "clients")
	cdbConfirm(t, m, "rename")
	if toast := cdbToast(m); !strings.Contains(toast, "Renamed customers to clients") || strings.Contains(toast[strings.Index(toast, "Renamed customers"):], "left alone") {
		t.Fatalf("toast %q", toast)
	}

	// Nothing names it; Cancel on the card; no rename open.
	m.dbConsole().rename = &dbRenameState{old: "nowhere_at_all", file: cur}
	m.databaseRenamePlan("x")
	dbeToastHas(t, m, "Nothing to rename")
	if m.databaseRenamePlan("x") != nil {
		t.Fatal("a plan with no rename")
	}
	m.dbConsole().rename = &dbRenameState{old: "a", new: "b", file: cur}
	if m.databaseRenameApply("cancel") != nil || m.dbConsole().rename != nil {
		t.Fatal("cancel")
	}
}

func TestCovDBRenameWriteKeepsMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.sql")
	if err := os.WriteFile(p, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := dbWriteFileKeepMode(p, "b"); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(p); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	// The target is a folder: the rename over it fails and the temp file goes.
	sub := filepath.Join(dir, "sub")
	_ = os.MkdirAll(filepath.Join(sub, "child"), 0o755)
	if err := dbWriteFileKeepMode(sub, "x"); err == nil {
		t.Fatal("a folder was replaced")
	}
	if _, err := os.Stat(sub + ".termocode-rename"); !os.IsNotExist(err) {
		t.Fatal("the temp file stayed")
	}
	if got := dbRenameFiles(filepath.Join(dir, "missing"), "", "a", "b"); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

// Go to definition of a routine opens its stored source.
func TestCovDBGotoRoutine(t *testing.T) {
	m := cdbRealModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432})
	cdbPGTree(m.databases)
	sess := &cdbSession{run: func(string) dbclient.Result {
		return dbclient.Result{Rows: [][]dbclient.Cell{{cdbCell("CREATE FUNCTION f1() RETURNS int AS 'SELECT 1'")}}}
	}}

	if _, ok := m.databaseGotoRoutine("pg", "nope"); ok {
		t.Fatal("an unknown routine was found")
	}
	cmd, ok := m.databaseGotoRoutine("pg", "F1")
	if !ok || cmd == nil {
		t.Fatal("not found")
	}
	dbeToastHas(t, m, "Not connected")
	m.databases.sessions["pg"] = sess
	cmd, _ = m.databaseGotoRoutine("pg", "f1")
	msgs := cdbMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("%v", msgs)
	}
	src := msgs[0].(dbSourceMsg)
	if src.err != nil || !strings.Contains(src.text, "CREATE FUNCTION f1") || !strings.Contains(sess.ran(), "pg_get_functiondef") {
		t.Fatalf("%+v\nSQL: %s", src, sess.ran())
	}
	m.onDBSource(src)
	if got := dbeBufLines(t, m); !strings.Contains(got, "CREATE FUNCTION f1") {
		t.Fatalf("source not shown: %q", got)
	}
	m.databaseFocusColumn("RETURNS")
	m.databaseFocusColumn("")
	if m.onDBSource(dbSourceMsg{gen: src.gen + 9}) != nil {
		t.Fatal("stale")
	}

	// No source / an error.
	sess.run = func(string) dbclient.Result { return dbclient.Result{} }
	cmd, _ = m.databaseGotoRoutine("pg", "f1")
	src = cdbMsgs(cmd)[0].(dbSourceMsg)
	if src.err == nil || !strings.Contains(src.err.Error(), "no source found") {
		t.Fatalf("%+v", src)
	}
	sess.run = func(string) dbclient.Result { return dbclient.Result{Err: errors.New("denied")} }
	cmd, _ = m.databaseGotoRoutine("pg", "f1")
	src = cdbMsgs(cmd)[0].(dbSourceMsg)
	if src.err == nil || src.err.Error() != "denied" {
		t.Fatalf("%+v", src)
	}
	m.onDBSource(src)
	// A routine of a connection that is gone.
	cdbAddNode(m.databases, "", "ghost/routine/g", dbNodeRoutine, "g")
	if _, ok := m.databaseGotoRoutine("ghost", "g"); ok {
		t.Fatal("a routine of no connection")
	}
}

// The text scanner keeps escaped quotes, block and line comments, other
// quoted names and an open quote as they are.
func TestCovDBRenameTextEdges(t *testing.T) {
	in := "SELECT 'it''s users' /* users */, \"other\", users -- users"
	got, n := dbRenameText(in, "users", "people")
	if n != 1 || got != "SELECT 'it''s users' /* users */, \"other\", people -- users" {
		t.Fatalf("%d %q", n, got)
	}
	if got, n := dbRenameText("SELECT users /* open", "users", "p"); n != 1 || got != "SELECT p /* open" {
		t.Fatalf("%d %q", n, got)
	}
	if got, n := dbRenameText(`SELECT "users`, "users", "p"); n != 0 || got != `SELECT "users` {
		t.Fatalf("%d %q", n, got)
	}
	if got, n := dbRenameText("x", "", "p"); n != 0 || got != "x" {
		t.Fatal("empty old name")
	}
	if a := dbStatementAliases("SELECT * FROM"); len(a) != 0 {
		t.Fatalf("%v", a)
	}
	if a := dbStatementAliases("SELECT * FROM where x"); a["where"] != "" {
		t.Fatalf("%v", a)
	}
	// A file too big to read is skipped.
	root := t.TempDir()
	big := filepath.Join(root, "big.sql")
	if err := os.WriteFile(big, []byte(strings.Repeat("users ", dbUsagesMaxFile/6+10)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := dbRenameFiles(root, "", "users", "p"); len(got) != 0 {
		t.Fatalf("a big file was renamed: %d", len(got))
	}
}
