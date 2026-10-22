package app_test

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// editSession is a fake MySQL server with one table `app`.`users` (id,
// email, plan) held in memory: page reads, counts, and the UPDATE /
// DELETE / INSERT statements of a submit change it, so a reload shows
// what the server holds. Every statement, its bound values and the
// transaction calls are recorded.
type editSession struct {
	mu       sync.Mutex
	rows     [][3]string
	snapshot [][3]string // at BEGIN (Rollback restores it)
	events   []string    // BEGIN / COMMIT / ROLLBACK and every statement
	args     [][]any
	ro       []string // statements in a read-only session
	keys     []string
	unique   [][]string
	affected int // -1: the real count
	failOn   string
	inTx     bool
	closed   int
	intro    int
}

func newEditSession(n int) *editSession {
	s := &editSession{keys: []string{"id"}, affected: -1}
	for i := 1; i <= n; i++ {
		s.rows = append(s.rows, [3]string{strconv.Itoa(i), fmt.Sprintf("user-%03d@example.com", i), []string{"free", "pro", "team"}[i%3]})
	}
	return s
}

func (s *editSession) Ping(context.Context) error { return nil }
func (s *editSession) Cancel()                    {}
func (s *editSession) Close() error               { s.mu.Lock(); s.closed++; s.mu.Unlock(); return nil }
func (s *editSession) ReadOnlySession() (dbclient.Session, error) {
	return roEdit{s}, nil
}

type roEdit struct{ s *editSession }

func (r roEdit) Ping(context.Context) error { return nil }
func (r roEdit) Cancel()                    {}
func (r roEdit) Close() error               { return nil }
func (r roEdit) Begin(context.Context) error {
	return fmt.Errorf("read-only")
}
func (r roEdit) Commit(context.Context) error   { return fmt.Errorf("read-only") }
func (r roEdit) Rollback(context.Context) error { return fmt.Errorf("read-only") }
func (r roEdit) ReadOnlySession() (dbclient.Session, error) {
	return r, nil
}
func (r roEdit) Introspect(ctx context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	return r.s.Introspect(ctx, req)
}
func (r roEdit) Run(ctx context.Context, sql string, o dbclient.RunOptions) dbclient.Result {
	r.s.mu.Lock()
	r.s.ro = append(r.s.ro, sql)
	r.s.mu.Unlock()
	if !strings.HasPrefix(sql, "SELECT") {
		return dbclient.Result{Err: fmt.Errorf("ERROR: read-only transaction")}
	}
	return r.s.Run(ctx, sql, o)
}

func (s *editSession) Introspect(_ context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intro++
	switch {
	case req.Object != "":
		ir := dbclient.IntrospectionResult{Keys: append([]string(nil), s.keys...), Columns: []dbclient.Column{{Name: "id", Type: "bigint"}, {Name: "email", Type: "varchar(80)"}, {Name: "plan", Type: "varchar(10)"}}}
		for _, cols := range s.unique {
			ir.Indexes = append(ir.Indexes, dbclient.SchemaIndex{Name: "uq", Columns: cols, Unique: true})
		}
		return ir, nil
	case req.Schema != "":
		return dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}, {Name: "active_users", Kind: "VIEW"}}}, nil
	case req.Database != "":
		return dbclient.IntrospectionResult{Schemas: []string{"app"}}, nil
	default:
		return dbclient.IntrospectionResult{Databases: []string{"app"}}, nil
	}
}

func (s *editSession) Begin(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inTx {
		return fmt.Errorf("transaction already active")
	}
	s.inTx = true
	s.snapshot = append([][3]string(nil), s.rows...)
	s.events = append(s.events, "BEGIN")
	return nil
}
func (s *editSession) Commit(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inTx = false
	s.events = append(s.events, "COMMIT")
	return nil
}
func (s *editSession) Rollback(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inTx {
		s.rows = s.snapshot
	}
	s.inTx = false
	s.events = append(s.events, "ROLLBACK")
	return nil
}

var (
	editLimit  = regexp.MustCompile(`LIMIT (\d+)(?: OFFSET (\d+))?`)
	editSetCol = regexp.MustCompile("`(\\w+)` = \\?")
	editInsCol = regexp.MustCompile("`(\\w+)`")
)

func (s *editSession) Run(_ context.Context, sql string, o dbclient.RunOptions) dbclient.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, sql)
	s.args = append(s.args, o.Args)
	if s.failOn != "" && strings.Contains(sql, s.failOn) {
		return dbclient.Result{Err: fmt.Errorf("ERROR: duplicate key value violates unique constraint")}
	}
	sql = strings.TrimSpace(dbLastLines(sql))
	col := map[string]int{"id": 0, "email": 1, "plan": 2}
	find := func(id any) int {
		for i, r := range s.rows {
			if r[0] == fmt.Sprint(id) {
				return i
			}
		}
		return -1
	}
	count := func(n int) int64 {
		if s.affected >= 0 {
			return int64(s.affected)
		}
		return int64(n)
	}
	switch {
	case strings.HasPrefix(sql, "SELECT COUNT(*)"):
		return dbclient.Result{Columns: []dbclient.Column{{Name: "n"}}, Rows: [][]dbclient.Cell{{{Text: strconv.Itoa(len(s.rows)), Kind: "number"}}}}
	case strings.HasPrefix(sql, "SELECT"):
		limit, offset := 1000, 0
		if mt := editLimit.FindStringSubmatch(sql); mt != nil {
			limit, _ = strconv.Atoi(mt[1])
			offset, _ = strconv.Atoi(mt[2])
		}
		r := dbclient.Result{Columns: []dbclient.Column{{Name: "id", Type: "BIGINT"}, {Name: "email", Type: "VARCHAR"}, {Name: "plan", Type: "VARCHAR"}}}
		if strings.Contains(sql, "count(") {
			return dbclient.Result{Columns: []dbclient.Column{{Name: "count(*)"}}, Rows: [][]dbclient.Cell{{{Text: "3"}}}}
		}
		for i := offset; i < offset+limit && i < len(s.rows); i++ {
			row := s.rows[i]
			r.Rows = append(r.Rows, []dbclient.Cell{{Text: row[0], Kind: "number"}, {Text: row[1]}, {Text: row[2]}})
		}
		return r
	case strings.HasPrefix(sql, "UPDATE `app`.`users`"):
		set := editSetCol.FindAllStringSubmatch(sql[:strings.Index(sql, " WHERE ")], -1)
		i := find(o.Args[len(set)])
		if i < 0 || s.affected == 0 {
			return dbclient.Result{Affected: count(0)}
		}
		for k, m := range set {
			s.rows[i][col[m[1]]] = fmt.Sprint(o.Args[k])
		}
		return dbclient.Result{Affected: count(1)}
	case strings.HasPrefix(sql, "DELETE FROM `app`.`users`"):
		i := find(o.Args[0])
		if i < 0 || s.affected == 0 {
			return dbclient.Result{Affected: count(0)}
		}
		s.rows = append(s.rows[:i], s.rows[i+1:]...)
		return dbclient.Result{Affected: count(1)}
	case strings.HasPrefix(sql, "INSERT INTO `app`.`users`"):
		row := [3]string{strconv.Itoa(len(s.rows) + 1000), "", "free"}
		if open := strings.Index(sql, "("); open >= 0 {
			names := editInsCol.FindAllStringSubmatch(sql[open:strings.Index(sql, ")")], -1)
			for k, m := range names {
				row[col[m[1]]] = fmt.Sprint(o.Args[k])
			}
		}
		s.rows = append(s.rows, row)
		return dbclient.Result{Affected: count(1)}
	}
	return dbclient.Result{Affected: 1}
}

func (s *editSession) log() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

// writes are the recorded statements that are not reads, with their
// bound values.
func (s *editSession) writes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for i, e := range s.events {
		if e == "BEGIN" || e == "COMMIT" || e == "ROLLBACK" || strings.HasPrefix(e, "SELECT") {
			out = append(out, e)
			continue
		}
		args := s.argsFor(i)
		out = append(out, fmt.Sprintf("%s %q", e, args))
	}
	return out
}

func (s *editSession) argsFor(event int) []any {
	n := -1
	for i := 0; i <= event; i++ {
		if e := s.events[i]; e != "BEGIN" && e != "COMMIT" && e != "ROLLBACK" {
			n++
		}
	}
	if n < 0 || n >= len(s.args) {
		return nil
	}
	return s.args[n]
}

func (s *editSession) row(id string) ([3]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r[0] == id {
			return r, true
		}
	}
	return [3]string{}, false
}

func (s *editSession) has(event string) bool {
	for _, e := range s.log() {
		if e == event || strings.HasPrefix(e, event) {
			return true
		}
	}
	return false
}

// editDriver opens the Databases view with conns and the session s, on the
// table users.
func editDriver(t *testing.T, w, h int, s *editSession, conn string) *testkit.Driver {
	t.Helper()
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	if conn == "" {
		conn = `{"id":"local","driver":"mysql","host":"localhost","port":3306}`
	}
	d := testkit.New(t, testkit.Options{W: w, H: h, Nvim: true, Files: map[string]string{
		"README.md":                 "x\n",
		".termocode/databases.json": `{"connections":[` + conn + `]}`,
	}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	if !d.WaitForText("DATABASES", time.Second) {
		t.Fatalf("view did not open:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("enter", "down", "enter")
	for _, name := range []string{"Tables", "users"} {
		if !d.WaitForText(name, 2*time.Second) {
			t.Fatalf("tree lacks %s:\n%s", name, ansi.Strip(d.Frame()))
		}
	}
	d.Key("down", "down")
	return d
}

// dbMarked reports a grid line with mark in the left margin that holds text.
func dbMarked(page, mark, text string) bool {
	for _, l := range strings.Split(page, "\n") {
		if strings.HasPrefix(l, mark) && strings.Contains(l, text) {
			return true
		}
	}
	return false
}

// openUsers opens the data view of users and waits for its first page.
func openUsers(t *testing.T, d *testkit.Driver) {
	t.Helper()
	d.Key("o")
	dbWaitPage(t, d, "user-001@example.com")
}

// cursorTo puts the page cursor on the first match of text.
func cursorTo(t *testing.T, d *testkit.Driver, text string) {
	t.Helper()
	dbLua(t, d, `local w=vim.fn.bufwinid(vim.g.termocode_db_buf); vim.api.nvim_set_current_win(w); vim.fn.cursor(1,1); local l=vim.fn.search(vim.fn.escape(...,'\\/.*$^~[]'),'cW'); return tostring(l)`, text)
}

// setCell edits the cell under the cursor through the e prompt.
func setCell(t *testing.T, d *testkit.Driver, value string) {
	t.Helper()
	d.Key("e")
	if !d.WaitForText("Edit cell", time.Second) {
		t.Fatalf("no edit prompt:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("ctrl+u")
	for i := 0; i < 40; i++ {
		d.Key("backspace")
	}
	d.Type(value)
	d.Key("enter")
}

func TestDBEditCellSubmitsOneBoundUpdate(t *testing.T) {
	s := newEditSession(250)
	d := editDriver(t, 100, 30, s, "")
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	setCell(t, d, "x'); DROP TABLE users; --")
	dbWaitPage(t, d, "1 change")
	text := dbText(t, d)
	if !dbMarked(text, "*", "x'); DROP TABLE users; --") {
		t.Fatalf("the pending cell is not marked:\n%s", text)
	}
	if s.has("UPDATE") {
		t.Fatal("an edit reached the server before w")
	}
	d.Key("w")
	want := "UPDATE `app`.`users` SET `email` = 'x''); DROP TABLE users; --' WHERE `id` = 2 AND `email` = 'user-002@example.com';"
	if !d.WaitForText("Submit 1 change to local (one transaction)", time.Second) || !strings.Contains(ansi.Strip(d.Frame()), "UPDATE `app`.`users` SET `email` =") {
		t.Fatalf("no submit card:\n%s", ansi.Strip(d.Frame()))
	}
	for _, label := range []string{"Submit", "Copy SQL", "Cancel"} {
		if !strings.Contains(ansi.Strip(d.Frame()), label) {
			t.Fatalf("card lacks %s:\n%s", label, ansi.Strip(d.Frame()))
		}
	}
	// Copy SQL keeps the card and copies the exact text.
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { return dbLua(t, d, `return vim.fn.getreg('"')`) == want }, 2*time.Second) {
		t.Fatalf("copied %q, want %q", dbLua(t, d, `return vim.fn.getreg('"')`), want)
	}
	if !strings.Contains(ansi.Strip(d.Frame()), "Submit 1 change") {
		t.Fatalf("Copy SQL closed the card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "enter") // from Copy SQL to Submit
	if !d.WaitFor(func() bool { return s.has("COMMIT") }, 2*time.Second) {
		t.Fatalf("no commit: %q", s.writes())
	}
	got := s.writes()
	var tx []string
	for _, e := range got {
		if !strings.HasPrefix(e, "SELECT") {
			tx = append(tx, e)
		}
	}
	wantTx := []string{"BEGIN", "UPDATE `app`.`users` SET `email` = ? WHERE `id` = ? AND `email` = ? [\"x'); DROP TABLE users; --\" \"2\" \"user-002@example.com\"]", "COMMIT"}
	if strings.Join(tx, "\n") != strings.Join(wantTx, "\n") {
		t.Fatalf("server saw:\n%s\nwant:\n%s", strings.Join(tx, "\n"), strings.Join(wantTx, "\n"))
	}
	if r, _ := s.row("2"); r[1] != "x'); DROP TABLE users; --" {
		t.Fatalf("row 2 = %v", r)
	}
	// The page reloads in place: no mark, the new value from the server.
	if !d.WaitFor(func() bool {
		t := dbText(t, d)
		return !strings.Contains(t, "1 change") && strings.Contains(t, "DROP TABLE users")
	}, 2*time.Second) {
		t.Fatalf("page after submit:\n%s", dbText(t, d))
	}
	if dbMarked(dbText(t, d), "*", "DROP TABLE") {
		t.Fatalf("mark stayed after the submit:\n%s", dbText(t, d))
	}
	d.Key("tab", "tab")
	if text := dbText(t, d); !strings.Contains(text, "Saved 1 change") || !strings.Contains(text, "1 row") {
		t.Fatalf("Messages do not say what ran:\n%s", text)
	}
}

func TestDBEditDeleteAndNewRowInOneSubmit(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 100, 30, s, "")
	openUsers(t, d)
	cursorTo(t, d, "user-003@example.com")
	d.Key("-")
	dbWaitPage(t, d, "1 change")
	if !dbMarked(dbText(t, d), "-", "user-003@example.com") {
		t.Fatalf("deleted row is not marked:\n%s", dbText(t, d))
	}
	d.Key("+")
	dbWaitPage(t, d, "2 changes")
	if !dbMarked(dbText(t, d), "+", "<default>") {
		t.Fatalf("new row missing:\n%s", dbText(t, d))
	}
	// The cursor is on the new row: set its email.
	dbLua(t, d, `vim.fn.search('<default>','cw'); vim.fn.search('<default>','W'); return ''`)
	setCell(t, d, "new@example.com")
	dbWaitPage(t, d, "new@example.com")
	d.Key("w")
	if !d.WaitForText("Submit 2 changes to local", time.Second) {
		t.Fatalf("no submit card:\n%s", ansi.Strip(d.Frame()))
	}
	frame := ansi.Strip(d.Frame())
	for _, want := range []string{"DELETE FROM `app`.`users` WHERE `id` = 3;", "INSERT INTO `app`.`users` (`email`) VALUES ('new@example.com');", "2 rows will change"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("card lacks %q:\n%s", want, frame)
		}
	}
	d.Key("right", "right", "enter") // a DELETE: Submit is the last button
	if !d.WaitFor(func() bool { return s.has("COMMIT") }, 2*time.Second) {
		t.Fatalf("no commit: %q", s.writes())
	}
	if _, ok := s.row("3"); ok {
		t.Fatal("row 3 is still there")
	}
	dbWaitPage(t, d, "new@example.com")
	if strings.Contains(dbText(t, d), "change") {
		t.Fatalf("marks after the submit:\n%s", dbText(t, d))
	}
}

func TestDBEditRevertCellRowAndAll(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 100, 30, s, "")
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	setCell(t, d, "a@example.com")
	dbWaitPage(t, d, "1 change")
	d.Key("u")
	if !d.WaitFor(func() bool { return !strings.Contains(dbText(t, d), "1 change") }, time.Second) {
		t.Fatalf("u did not revert the cell:\n%s", dbText(t, d))
	}
	if !strings.Contains(dbText(t, d), "user-002@example.com") {
		t.Fatalf("old value not back:\n%s", dbText(t, d))
	}
	// Two changes, cursor elsewhere: u asks before it reverts all.
	setCell(t, d, "a@example.com")
	cursorTo(t, d, "user-004@example.com")
	d.Key("-")
	dbWaitPage(t, d, "2 changes")
	cursorTo(t, d, "user-005@example.com")
	d.Key("u")
	if !d.WaitForText("Revert 2 changes?", time.Second) {
		t.Fatalf("no revert card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { return !strings.Contains(dbText(t, d), "change") }, time.Second) {
		t.Fatalf("revert all left changes:\n%s", dbText(t, d))
	}
	if s.has("UPDATE") || s.has("DELETE") || s.has("BEGIN") {
		t.Fatalf("revert reached the server: %q", s.writes())
	}
}

func TestDBEditWrongCountRollsBackAndKeepsTheMarks(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 100, 30, s, "")
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	setCell(t, d, "b@example.com")
	dbWaitPage(t, d, "1 change")
	s.mu.Lock()
	s.affected = 0 // someone else changed the row
	s.mu.Unlock()
	d.Key("w")
	if !d.WaitForText("Submit 1 change", time.Second) {
		t.Fatalf("no card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "right", "enter") // Submit is the last button
	if !d.WaitForText("The row changed on the server", 2*time.Second) {
		t.Fatalf("no stale card:\n%s", ansi.Strip(d.Frame()))
	}
	if !s.has("ROLLBACK") || s.has("COMMIT") {
		t.Fatalf("not rolled back: %q", s.writes())
	}
	d.Key("enter") // Keep my changes
	text := dbText(t, d)
	if !strings.Contains(text, "1 change") || !strings.Contains(text, "not saved") || !strings.Contains(text, "b@example.com") {
		t.Fatalf("marks lost after a failed submit:\n%s", text)
	}
	// Reload drops them.
	d.Key("w")
	d.WaitForText("Submit 1 change", time.Second)
	d.Key("right", "right", "enter") // Submit is the last button
	if !d.WaitForText("The row changed on the server", 2*time.Second) {
		t.Fatalf("no stale card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "enter")
	if !d.WaitFor(func() bool {
		t := dbText(t, d)
		return !strings.Contains(t, "change") && strings.Contains(t, "user-002@example.com")
	}, 2*time.Second) {
		t.Fatalf("Reload kept the changes:\n%s", dbText(t, d))
	}
}

func TestDBEditServerErrorNamesTheStatement(t *testing.T) {
	s := newEditSession(5)
	s.failOn = "UPDATE"
	d := editDriver(t, 100, 30, s, "")
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	setCell(t, d, "dup@example.com")
	dbWaitPage(t, d, "1 change")
	d.Key("w")
	d.WaitForText("Submit 1 change", time.Second)
	d.Key("right", "right", "enter") // Submit is the last button
	if !d.WaitForText("statement 1 of 1 failed", 2*time.Second) {
		t.Fatalf("no failure toast:\n%s", ansi.Strip(d.Frame()))
	}
	if !s.has("ROLLBACK") || s.has("COMMIT") {
		t.Fatalf("not rolled back: %q", s.writes())
	}
	if !strings.Contains(dbText(t, d), "1 change") {
		t.Fatalf("marks lost:\n%s", dbText(t, d))
	}
}

func TestDBEditWithoutAKeyIsReadOnly(t *testing.T) {
	s := newEditSession(5)
	s.keys = nil
	d := editDriver(t, 100, 30, s, "")
	openUsers(t, d)
	if !strings.Contains(dbText(t, d), "read-only: no key") {
		t.Fatalf("row 2 does not say why:\n%s", dbText(t, d))
	}
	cursorTo(t, d, "user-002@example.com")
	d.Key("e")
	if !d.WaitForText("This result cannot be edited", time.Second) {
		t.Fatalf("no toast:\n%s", ansi.Strip(d.Frame()))
	}
	if strings.Contains(ansi.Strip(d.Frame()), "Edit cell") {
		t.Fatal("an edit prompt opened without a key")
	}
	// A unique index is a key too.
	s.unique = [][]string{{"email"}}
	d.Key("escape")
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	openUsers(t, d)
	if strings.Contains(dbText(t, d), "read-only") {
		t.Fatalf("a unique key was not used:\n%s", dbText(t, d))
	}
	cursorTo(t, d, "free")
	setCell(t, d, "team")
	d.Key("w")
	if !d.WaitForText("WHERE `email` = 'user-", time.Second) {
		t.Fatalf("card does not find the row by the unique key:\n%s", ansi.Strip(d.Frame()))
	}
}

func TestDBEditReadOnlyConnectionAndViewRefuse(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 100, 30, s, `{"id":"local","driver":"mysql","host":"localhost","port":3306,"readOnly":true}`)
	openUsers(t, d)
	if !strings.Contains(dbText(t, d), "read-only connection") {
		t.Fatalf("row 2:\n%s", dbText(t, d))
	}
	cursorTo(t, d, "user-002@example.com")
	for _, k := range []string{"e", "-", "+"} {
		d.Key(k)
		if !d.WaitForText("This result cannot be edited", time.Second) {
			t.Fatalf("%s was not refused:\n%s", k, ansi.Strip(d.Frame()))
		}
		d.Settle(100 * time.Millisecond)
	}
	if strings.Contains(dbText(t, d), "change") {
		t.Fatalf("a change was kept:\n%s", dbText(t, d))
	}
}

func TestDBEditProductionGetsTheStrongCard(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 100, 30, s, `{"id":"local","driver":"mysql","host":"localhost","port":3306,"environment":"production"}`)
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	setCell(t, d, "p@example.com")
	d.Key("w")
	if !d.WaitForText("Write to production (local)? 1 change", time.Second) {
		t.Fatalf("no production card:\n%s", ansi.Strip(d.Frame()))
	}
	if strings.Contains(ansi.Strip(d.Frame()), "Always") {
		t.Fatal("an always-allow choice on a production write")
	}
	d.Key("enter") // the first button is Cancel
	d.Settle(200 * time.Millisecond)
	if s.has("UPDATE") {
		t.Fatal("Enter on the production card wrote")
	}
}

func TestDBEditPagingWithChangesAsksFirst(t *testing.T) {
	s := newEditSession(250)
	d := editDriver(t, 100, 30, s, "")
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	setCell(t, d, "c@example.com")
	dbWaitPage(t, d, "1 change")
	n := len(s.log())
	d.Key("]")
	if !d.WaitForText("Discard 1 pending change?", time.Second) {
		t.Fatalf("no discard card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("enter") // Keep
	d.Settle(200 * time.Millisecond)
	if len(s.log()) != n || !strings.Contains(dbText(t, d), "1 change") {
		t.Fatalf("Keep moved the page: %q", s.log()[n:])
	}
	d.Key("]")
	d.WaitForText("Discard 1 pending change?", time.Second)
	d.Key("right", "enter")
	dbWaitPage(t, d, "user-101@example.com")
	if strings.Contains(dbText(t, d), "change") {
		t.Fatalf("changes kept after Discard:\n%s", dbText(t, d))
	}
	if s.has("UPDATE") {
		t.Fatal("Discard wrote")
	}
}

func TestDBEditConsoleResultLooksUpItsKey(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 100, 30, s, "")
	d.Key("s") // SELECT * FROM `app`.`users` LIMIT 100; in the console
	dbWaitPage(t, d, "user-001@example.com")
	before := s.intro
	cursorTo(t, d, "user-002@example.com")
	setCell(t, d, "d@example.com")
	dbWaitPage(t, d, "1 change")
	if s.intro <= before {
		t.Fatal("the key was not looked up")
	}
	d.Key("w")
	if !d.WaitForText("WHERE `id` = 2", time.Second) {
		t.Fatalf("no card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "right", "enter") // Submit is the last button
	if !d.WaitFor(func() bool { r, _ := s.row("2"); return r[1] == "d@example.com" }, 2*time.Second) {
		t.Fatalf("not saved: %q", s.writes())
	}
	dbWaitPage(t, d, "d@example.com")
}

func TestDBEditComputedResultIsReadOnly(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 100, 30, s, "")
	d.Key("n") // the console
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "consoles") }, 2*time.Second) {
		t.Fatalf("no console: %s", dbBufName(t, d))
	}
	dbLua(t, d, `vim.api.nvim_buf_set_lines(0,0,-1,false,{'-- @conn local','SELECT id, lower(email) FROM users;'}); vim.cmd('stopinsert'); vim.api.nvim_win_set_cursor(0,{2,0}); return ''`)
	d.Key("esc")
	dbSend(d)
	dbWaitPage(t, d, "lower")
	cursorTo(t, d, "user-001")
	d.Key("e")
	if !d.WaitForText("This result cannot be edited", time.Second) {
		t.Fatalf("no toast:\n%s", ansi.Strip(d.Frame()))
	}
	if !strings.Contains(dbText(t, d), "read-only query") {
		t.Fatalf("row 2:\n%s", dbText(t, d))
	}
}

// The whole flow on a real SQLite file through the sqlite3 CLI (skipped
// when it is missing): edit, delete and add rows in the data view, submit,
// and check the file; then a manual transaction rolled back with Z.
func TestDBEditSQLiteEndToEnd(t *testing.T) {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed")
	}
	file := filepath.Join(t.TempDir(), "shop.db")
	script := `CREATE TABLE "order items" (id INTEGER PRIMARY KEY, name TEXT NOT NULL, qty INTEGER DEFAULT 5);
INSERT INTO "order items"(id,name,qty) VALUES (1,'apple',1),(2,'pear',2),(3,'plum',3);
CREATE TABLE logs (msg TEXT, rowid TEXT, _rowid_ TEXT, oid TEXT);
INSERT INTO logs(msg) VALUES ('a');`
	if out, err := exec.Command(bin, file, script).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v\n%s", err, out)
	}
	query := func(sql string) string {
		out, err := exec.Command(bin, file, sql).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v %s", sql, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	d := testkit.New(t, testkit.Options{W: 120, H: 30, Nvim: true, Files: map[string]string{
		"README.md":                 "x\n",
		".termocode/databases.json": fmt.Sprintf(`{"connections":[{"id":"shop","driver":"sqlite","path":%q}]}`, file),
	}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("enter")
	if !d.WaitForText("main", 5*time.Second) {
		t.Fatalf("no database row:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("down", "enter")
	if !d.WaitForText("order items", 5*time.Second) {
		t.Fatalf("no table row:\n%s", ansi.Strip(d.Frame()))
	}
	// logs has no key and hides every rowid name with a column: read-only.
	d.Key("down", "down", "o")
	dbWaitPage(t, d, "read-only: no key")
	d.Key("escape", "f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("down", "o")
	dbWaitPage(t, d, "apple")
	cursorTo(t, d, "pear")
	setCell(t, d, "it's a \"pear\"; DROP TABLE logs; --")
	dbWaitPage(t, d, "1 change")
	cursorTo(t, d, "plum")
	d.Key("-")
	d.Key("+")
	dbWaitPage(t, d, "3 changes")
	dbLua(t, d, `vim.fn.search('<default>','cw'); vim.fn.search('<default>','W'); return ''`)
	setCell(t, d, "fig")
	d.Key("w")
	if !d.WaitForText("Submit 3 changes to shop", 2*time.Second) {
		t.Fatalf("no card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "right", "enter") // Submit is the last button
	if !d.WaitFor(func() bool {
		return query(`SELECT count(*) FROM "order items"`) == "3" && strings.Contains(query(`SELECT name FROM "order items" WHERE name = 'fig'`), "fig")
	}, 3*time.Second) {
		t.Fatalf("file after submit: %s", query(`SELECT * FROM "order items"`))
	}
	if got := query(`SELECT name FROM "order items" WHERE id = 2`); got != `it's a "pear"; DROP TABLE logs; --` {
		t.Fatalf("row 2 = %q", got)
	}
	if got := query(`SELECT count(*) FROM "order items" WHERE name = 'plum'`); got != "0" {
		t.Fatal("row 3 was not deleted")
	}
	if got := query(`SELECT qty FROM "order items" WHERE name = 'fig'`); got != "5" {
		t.Fatalf("the new row did not get its default: %q", got)
	}
	if got := query(`SELECT count(*) FROM logs`); got != "1" {
		t.Fatal("the logs table changed")
	}
	dbWaitPage(t, d, "fig")
	// Manual mode: a submit waits for Commit; Z rolls it back.
	d.Key("f1")
	d.Type("Databases: Transaction Mode")
	d.Key("enter")
	d.WaitForText("Transaction mode for shop", time.Second)
	d.Type("Manual")
	d.Key("enter")
	d.WaitForText("Manual transactions on shop", time.Second)
	cursorTo(t, d, "apple")
	setCell(t, d, "banana")
	d.Key("w")
	if !d.WaitForText("(open transaction)", 2*time.Second) {
		t.Fatalf("no card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("enter")
	dbWaitPage(t, d, "manual commit · 1 not committed")
	dbWaitPage(t, d, "banana")
	d.Key("Z")
	dbWaitPage(t, d, "apple")
	if got := query(`SELECT name FROM "order items" WHERE id = 1`); got != "apple" {
		t.Fatalf("rollback left %q", got)
	}
}

// E edits a long value in a buffer: :w keeps it as a pending change, q
// leaves without one.
func TestDBEditMultiLineValueInABuffer(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 100, 30, s, "")
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	d.Key("E")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "SQL Edit: users.email") }, 2*time.Second) {
		t.Fatalf("no edit buffer: %s", dbBufName(t, d))
	}
	if got := dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`); got != "user-002@example.com" {
		t.Fatalf("buffer = %q", got)
	}
	d.Key("q")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "Table · ") }, time.Second) {
		t.Fatalf("q did not go back: %s", dbBufName(t, d))
	}
	if strings.Contains(dbText(t, d), "change") {
		t.Fatal("q kept a change")
	}
	d.Key("E")
	d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "SQL Edit") }, 2*time.Second)
	dbLua(t, d, `vim.api.nvim_buf_set_lines(0,0,-1,false,{'line one','line two'}); return ''`)
	d.Key(":")
	d.Type("w")
	d.Key("enter")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "Table · ") }, 2*time.Second) {
		t.Fatalf(":w did not go back: %s", dbBufName(t, d))
	}
	dbWaitPage(t, d, "1 change")
	d.Key("w")
	if !d.WaitForText("SET `email` = 'line one", time.Second) {
		t.Fatalf("card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "right", "enter") // Submit is the last button
	if !d.WaitFor(func() bool { r, _ := s.row("2"); return r[1] == "line one\nline two" }, 2*time.Second) {
		t.Fatalf("saved %q", s.writes())
	}
}

// DB-3: with auto-commit or a DELETE the card opens on Cancel and Submit is
// the danger button; a manual transaction without a DELETE keeps Submit first.
func TestDBEditSubmitCardCancelFirstForAutoCommitAndDelete(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 100, 30, s, "")
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	setCell(t, d, "c@example.com")
	dbWaitPage(t, d, "1 change")
	d.Key("w")
	if !d.WaitForText("Submit 1 change to local (one transaction)", time.Second) {
		t.Fatalf("no submit card:\n%s", ansi.Strip(d.Frame()))
	}
	for _, l := range strings.Split(ansi.Strip(d.Frame()), "\n") {
		if strings.Contains(l, "Copy SQL") && strings.Index(l, "Cancel") > strings.Index(l, "Submit") {
			t.Fatalf("Cancel is not before Submit: %q", l)
		}
	}
	d.Key("enter") // the focused button is Cancel
	d.Settle(200 * time.Millisecond)
	if s.has("UPDATE") || s.has("COMMIT") {
		t.Fatalf("Enter on the auto-commit card wrote: %q", s.writes())
	}
	// A DELETE gets the same card.
	d.Key("-")
	d.Key("w")
	d.WaitForText("Submit 2 changes", time.Second)
	d.Key("enter")
	d.Settle(200 * time.Millisecond)
	if s.has("DELETE") {
		t.Fatalf("Enter on the DELETE card wrote: %q", s.writes())
	}
}

// DB-9: a table view is named by its table, and the name shows once.
func TestDBPageTabIsNamedByItsTableAndShownOnce(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 100, 30, s, "")
	openUsers(t, d)
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "Table · app.users") }, 2*time.Second) {
		t.Fatalf("the page is not named by its table: %s", dbBufName(t, d))
	}
	d.Settle(200 * time.Millisecond)
	if n := strings.Count(ansi.Strip(d.Frame()), "Table · app.users"); n != 1 {
		t.Fatalf("the title shows %d times:\n%s", n, ansi.Strip(d.Frame()))
	}
}
