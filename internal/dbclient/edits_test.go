package dbclient

import (
	"context"
	"database/sql"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

func TestEditStatementsBindEveryValue(t *testing.T) {
	evil := `x'); DROP TABLE users; --`
	edits := []RowEdit{
		{Kind: EditUpdate, Key: []EditValue{{Column: "id", Value: Cell{Text: "42"}}}, Values: []EditValue{
			{Column: "plan", Value: Cell{Text: evil}, Old: Cell{Text: "free"}, Check: true},
			{Column: `we"ird`, Value: Cell{Null: true}, Old: Cell{Null: true}, Check: true},
		}},
		{Kind: EditDelete, Key: []EditValue{{Column: "id", Value: Cell{Text: "51"}}}},
		{Kind: EditInsert, Values: []EditValue{{Column: "email", Value: Cell{Text: "g@ex.com"}}, {Column: "plan", Value: Cell{Null: true}}}},
	}
	cases := []struct {
		d    Dialect
		want []string
	}{
		{PostgresDialect{}, []string{
			`DELETE FROM "public"."users" WHERE "id" = $1`,
			`UPDATE "public"."users" SET "plan" = $1, "we""ird" = $2 WHERE "id" = $3 AND "plan" = $4 AND "we""ird" IS NULL`,
			`INSERT INTO "public"."users" ("email", "plan") VALUES ($1, $2)`,
		}},
		{MySQLDialect{}, []string{
			"DELETE FROM `public`.`users` WHERE `id` = ?",
			"UPDATE `public`.`users` SET `plan` = ?, `we\"ird` = ? WHERE `id` = ? AND `plan` = ? AND `we\"ird` IS NULL",
			"INSERT INTO `public`.`users` (`email`, `plan`) VALUES (?, ?)",
		}},
		{SQLiteDialect{}, []string{
			`DELETE FROM "public"."users" WHERE "id" = ?`,
			`UPDATE "public"."users" SET "plan" = ?, "we""ird" = ? WHERE "id" = ? AND "plan" = ? AND "we""ird" IS NULL`,
			`INSERT INTO "public"."users" ("email", "plan") VALUES (?, ?)`,
		}},
	}
	for _, c := range cases {
		stmts, err := EditStatements(c.d, "public", "users", edits)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, st := range stmts {
			got = append(got, st.SQL)
			if strings.Contains(st.SQL, "DROP") || strings.Contains(st.SQL, "g@ex.com") || strings.Contains(st.SQL, "42") || strings.Contains(st.SQL, "free") {
				t.Fatalf("a value was built into the SQL: %s", st.SQL)
			}
			if st.Expect != 1 {
				t.Fatalf("%s expects %d rows", st.SQL, st.Expect)
			}
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%T:\n got %q\nwant %q", c.d, got, c.want)
		}
		wantArgs := [][]any{{"51"}, {evil, nil, "42", "free"}, {"g@ex.com", nil}}
		for i, st := range stmts {
			if !reflect.DeepEqual(st.Args, wantArgs[i]) {
				t.Fatalf("%T args %d = %#v, want %#v", c.d, i, st.Args, wantArgs[i])
			}
		}
	}
}

func TestEditStatementsPreviewShowsLiterals(t *testing.T) {
	edits := []RowEdit{{Kind: EditUpdate, Key: []EditValue{{Column: "id", Value: Cell{Text: "42", Kind: "number"}}}, Values: []EditValue{{Column: "plan", Value: Cell{Text: `it's \ ok`}}}}}
	pg, _ := EditStatements(PostgresDialect{}, "", "users", edits)
	if want := `UPDATE "users" SET "plan" = 'it''s \ ok' WHERE "id" = 42;`; pg[0].Preview != want {
		t.Fatalf("pg preview = %s, want %s", pg[0].Preview, want)
	}
	my, _ := EditStatements(MySQLDialect{}, "", "users", edits)
	if want := "UPDATE `users` SET `plan` = 'it''s \\\\ ok' WHERE `id` = 42;"; my[0].Preview != want {
		t.Fatalf("mysql preview = %s, want %s", my[0].Preview, want)
	}
	// A "number" that is not a number is quoted.
	edits[0].Key[0].Value = Cell{Text: "1 OR 1=1", Kind: "number"}
	pg, _ = EditStatements(PostgresDialect{}, "", "users", edits)
	if !strings.HasSuffix(pg[0].Preview, `WHERE "id" = '1 OR 1=1';`) {
		t.Fatalf("preview = %s", pg[0].Preview)
	}
}

func TestEditStatementsInsertDefaultsAndRefusals(t *testing.T) {
	ins := []RowEdit{{Kind: EditInsert}}
	pg, err := EditStatements(PostgresDialect{}, "s", "t", ins)
	if err != nil || pg[0].SQL != `INSERT INTO "s"."t" DEFAULT VALUES` {
		t.Fatalf("pg = %+v %v", pg, err)
	}
	my, err := EditStatements(MySQLDialect{}, "", "t", ins)
	if err != nil || my[0].SQL != "INSERT INTO `t` () VALUES ()" {
		t.Fatalf("mysql = %+v %v", my, err)
	}
	for _, bad := range [][]RowEdit{
		{{Kind: EditUpdate, Values: []EditValue{{Column: "a", Value: Cell{Text: "1"}}}}},                                                         // no key
		{{Kind: EditDelete, Key: []EditValue{{Column: "id", Value: Cell{Null: true}}}}},                                                          // empty key
		{{Kind: EditUpdate, Key: []EditValue{{Column: "id", Value: Cell{Text: "1"}}}}},                                                           // nothing to set
		{{Kind: EditUpdate, Key: []EditValue{{Column: "", Value: Cell{Text: "1"}}}, Values: []EditValue{{Column: "a", Value: Cell{Text: "1"}}}}}, // no column name
	} {
		if _, err := EditStatements(PostgresDialect{}, "", "t", bad); err == nil {
			t.Fatalf("%+v was accepted", bad)
		}
	}
	if _, err := EditStatements(PostgresDialect{}, "", "", []RowEdit{{Kind: EditInsert}}); err == nil {
		t.Fatal("an empty table name was accepted")
	}
}

func editStub(t *testing.T, name string, rows int64) (*SQLSession, *dbtest.Stub) {
	t.Helper()
	stub := &dbtest.Stub{ExecRows: rows}
	db := dbtest.Open(name, stub)
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, false)
	s.newDB = func() (*sql.DB, error) { return dbtest.Open(name+"-ro", stub), nil }
	t.Cleanup(func() { s.Close(); db.Close() })
	return s, stub
}

func twoEdits(t *testing.T) []EditStatement {
	stmts, err := EditStatements(PostgresDialect{}, "public", "users", []RowEdit{
		{Kind: EditUpdate, Key: []EditValue{{Column: "id", Value: Cell{Text: "1"}}}, Values: []EditValue{{Column: "plan", Value: Cell{Text: "pro"}}}},
		{Kind: EditDelete, Key: []EditValue{{Column: "id", Value: Cell{Text: "2"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return stmts
}

func TestApplyEditsOneTransaction(t *testing.T) {
	s, stub := editStub(t, "edits-ok", 1)
	out := ApplyEdits(context.Background(), s, twoEdits(t), false)
	if out.Err != nil || out.Failed != -1 || !reflect.DeepEqual(out.Affected, []int64{1, 1}) {
		t.Fatalf("outcome = %+v", out)
	}
	if b, c, r := stub.TransactionCounts(); b != 1 || c != 1 || r != 0 {
		t.Fatalf("begins %d commits %d rollbacks %d", b, c, r)
	}
	args := stub.RecordedArgs()
	if len(args) != 2 || !reflect.DeepEqual(args[0], []any{"2"}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestApplyEditsRollsBackWhenACountIsWrong(t *testing.T) {
	s, stub := editStub(t, "edits-count", 0)
	out := ApplyEdits(context.Background(), s, twoEdits(t), false)
	var ce *EditCountError
	if !errors.As(out.Err, &ce) || out.Failed != 0 || ce.Got != 0 || ce.Want != 1 {
		t.Fatalf("outcome = %+v", out)
	}
	if !strings.Contains(out.Err.Error(), "changed on the server") {
		t.Fatalf("message = %q", out.Err)
	}
	if b, c, r := stub.TransactionCounts(); b != 1 || c != 0 || r != 1 {
		t.Fatalf("begins %d commits %d rollbacks %d", b, c, r)
	}
	if len(stub.Recorded()) != 1 {
		t.Fatalf("ran after the failure: %v", stub.Recorded())
	}
	// Too many rows is just as wrong as none.
	s2, stub2 := editStub(t, "edits-many", 3)
	out = ApplyEdits(context.Background(), s2, twoEdits(t), false)
	if !errors.As(out.Err, &ce) || ce.Got != 3 {
		t.Fatalf("outcome = %+v", out)
	}
	if _, c, r := stub2.TransactionCounts(); c != 0 || r != 1 {
		t.Fatalf("commits %d rollbacks %d", c, r)
	}
}

func TestApplyEditsRollsBackOnAServerError(t *testing.T) {
	s, stub := editStub(t, "edits-err", 1)
	stub.ExecError = errors.New(`ERROR: duplicate key value violates unique constraint "users_email_key"`)
	out := ApplyEdits(context.Background(), s, twoEdits(t), false)
	if out.Err == nil || out.Failed != 0 || !strings.Contains(out.Err.Error(), "duplicate key") {
		t.Fatalf("outcome = %+v", out)
	}
	if _, c, r := stub.TransactionCounts(); c != 0 || r != 1 {
		t.Fatalf("commits %d rollbacks %d", c, r)
	}
}

func TestApplyEditsInsideAnOpenTransactionUsesASavepoint(t *testing.T) {
	s, stub := editStub(t, "edits-tx", 1)
	if err := s.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := ApplyEdits(context.Background(), s, twoEdits(t), true)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	got := stub.Recorded()
	if len(got) != 4 || got[0] != "SAVEPOINT termocode_edit" || got[3] != "RELEASE SAVEPOINT termocode_edit" {
		t.Fatalf("statements = %q", got)
	}
	if b, c, r := stub.TransactionCounts(); b != 1 || c != 0 || r != 0 {
		t.Fatalf("the user's transaction was ended: begins %d commits %d rollbacks %d", b, c, r)
	}
	// A failure goes back to the savepoint and leaves the transaction open.
	stub.ExecRows = 0
	out = ApplyEdits(context.Background(), s, twoEdits(t), true)
	if out.Err == nil {
		t.Fatal("a wrong count was accepted")
	}
	got = stub.Recorded()[4:]
	if len(got) != 4 || got[2] != "ROLLBACK TO SAVEPOINT termocode_edit" || got[3] != "RELEASE SAVEPOINT termocode_edit" {
		t.Fatalf("statements = %q", got)
	}
	if _, c, r := stub.TransactionCounts(); c != 0 || r != 0 {
		t.Fatalf("commits %d rollbacks %d", c, r)
	}
}

// The sqlite3 CLI binds values from its parameter table: a value is data,
// never SQL, and the parameter table is gone again afterwards.
func TestSQLiteRunBindsArguments(t *testing.T) {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed")
	}
	file := filepath.Join(t.TempDir(), "bind.db")
	if out, err := exec.Command(bin, file, `CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, n INTEGER); INSERT INTO t VALUES (1,'a',5),(2,'b',6);`).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: file})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	evil := "x'); DROP TABLE t; --\nline two"
	r := s.Run(context.Background(), `UPDATE "t" SET "name" = ?, "n" = ? WHERE "id" = ? AND "n" = ?`, RunOptions{Args: []any{evil, "7", "1", "5"}})
	if r.Err != nil || r.Affected != 1 {
		t.Fatalf("update = %+v", r)
	}
	r = s.Run(context.Background(), `SELECT name, typeof(n) AS ty, n FROM t WHERE id = ?`, RunOptions{Args: []any{"1"}})
	if r.Err != nil || len(r.Rows) != 1 || r.Rows[0][0].Text != evil || r.Rows[0][1].Text != "integer" || r.Rows[0][2].Text != "7" {
		t.Fatalf("row = %+v", r)
	}
	r = s.Run(context.Background(), `UPDATE t SET name = ? WHERE id = ?`, RunOptions{Args: []any{nil, "2"}})
	if r.Err != nil || r.Affected != 1 {
		t.Fatalf("null update = %+v", r)
	}
	r = s.Run(context.Background(), `SELECT name IS NULL AS gone, (SELECT count(*) FROM sqlite_temp_master WHERE name = 'sqlite_parameters') AS left FROM t WHERE id = 2`, RunOptions{})
	if r.Err != nil || len(r.Rows) != 1 || r.Rows[0][0].Text != "1" || r.Rows[0][1].Text != "0" {
		t.Fatalf("after = %+v", r)
	}
	// A wrong count rolls the whole edit back on the real CLI too.
	stmts, err := EditStatements(SQLiteDialect{}, "", "t", []RowEdit{
		{Kind: EditUpdate, Key: []EditValue{{Column: "id", Value: Cell{Text: "1"}}}, Values: []EditValue{{Column: "name", Value: Cell{Text: "kept?"}}}},
		{Kind: EditDelete, Key: []EditValue{{Column: "id", Value: Cell{Text: "99"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyEdits(context.Background(), s, stmts, false)
	if out.Err == nil || out.Failed != 0 {
		t.Fatalf("outcome = %+v", out)
	}
	r = s.Run(context.Background(), `SELECT name FROM t WHERE id = 1`, RunOptions{})
	if r.Err != nil || r.Rows[0][0].Text != evil {
		t.Fatalf("the failed edit was kept: %+v", r)
	}
}

// A savepoint released inside BEGIN does not end the transaction: a later
// restart still says that the open transaction was rolled back.
func TestSQLiteReleaseInsideBeginKeepsTheTransaction(t *testing.T) {
	s := &SQLiteSession{}
	s.trackTransaction([]string{"BEGIN"})
	s.trackTransaction([]string{"SAVEPOINT", "TERMOCODE_READ"})
	s.trackTransaction([]string{"RELEASE", "SAVEPOINT", "TERMOCODE_READ"})
	if !s.transaction {
		t.Fatal("RELEASE ended the BEGIN transaction")
	}
	s.trackTransaction([]string{"COMMIT"})
	if s.transaction {
		t.Fatal("COMMIT did not end it")
	}
	// A transaction a savepoint began ends with its RELEASE.
	s.trackTransaction([]string{"SAVEPOINT", "A"})
	s.trackTransaction([]string{"RELEASE", "A"})
	if s.transaction {
		t.Fatal("RELEASE of the outer savepoint did not end it")
	}
}
