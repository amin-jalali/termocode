package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBTableQuotesEveryIdentifier(t *testing.T) {
	for _, tc := range []struct {
		driver, schema, name, want string
	}{
		{"postgres", "public", `us"ers`, `"public"."us""ers"`},
		{"postgres", "", "users", `"users"`},
		{"mysql", "app", "or`ders", "`app`.`or``ders`"},
		{"sqlite", "main", "users", `"main"."users"`},
	} {
		got := dbTable{driver: tc.driver, schema: tc.schema, name: tc.name}.qualified()
		if got != tc.want {
			t.Errorf("%s: qualified = %s, want %s", tc.driver, got, tc.want)
		}
	}
}

func TestDBTableSelectSQL(t *testing.T) {
	tv := &dbTableView{dbTable: dbTable{driver: "postgres", schema: "public", name: "users"}, keys: []string{"id"}, cols: []string{"id", "plan"}}
	got, err := tv.selectSQL(101)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT * FROM "public"."users" ORDER BY "id" LIMIT 101 OFFSET 0`; got != want {
		t.Fatalf("default = %s, want %s", got, want)
	}
	tv.where, tv.orderCol, tv.desc, tv.offset = "plan = 'pro'", "plan", true, 200
	got, err = tv.selectSQL(101)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT * FROM "public"."users" WHERE (plan = 'pro') ORDER BY "plan" DESC, "id" LIMIT 101 OFFSET 200`; got != want {
		t.Fatalf("filtered = %s, want %s", got, want)
	}
	m := &dbTableView{dbTable: dbTable{driver: "mysql", schema: "app", name: "users"}, cols: []string{"id"}}
	m.offset = 100
	if got, _ = m.selectSQL(101); got != "SELECT * FROM `app`.`users` LIMIT 101 OFFSET 100" {
		t.Fatalf("mysql = %s", got)
	}
	tv.orderCol = `plan"; DROP TABLE users; --`
	if _, err = tv.selectSQL(101); err == nil {
		t.Fatal("an ORDER BY column that is not in the table was accepted")
	}
}

func TestDBTableCountSQL(t *testing.T) {
	tv := &dbTableView{dbTable: dbTable{driver: "sqlite", schema: "main", name: "users"}, where: "id > 3"}
	got, err := tv.countSQL()
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT COUNT(*) FROM "main"."users" WHERE (id > 3)`; got != want {
		t.Fatalf("count = %s, want %s", got, want)
	}
}

func TestDBWhereCheckRefusesAnythingButAPlainRead(t *testing.T) {
	ok := []string{
		"", "  ", "id = 1", "plan = 'pro' AND id > 3", "name LIKE '%;%'", "name = 'it''s -- fine'", `"a b" = 1`,
		"id IN (SELECT id FROM t)", "email = 'a/*b*/c'",
	}
	for _, w := range ok {
		for _, driver := range []string{"postgres", "mysql", "sqlite"} {
			if err := dbWhereCheck(driver, w); err != nil {
				t.Errorf("%s %q refused: %v", driver, w, err)
			}
		}
	}
	bad := []string{
		"1=1; DROP TABLE users",
		"1=1;DELETE FROM users",
		"1=1 -- the rest",
		"1=1 /* x */",
		"id = 1) OR (1=1",
		"id = 1) UNION SELECT 1 --",
		"id = 'open",
		"((id = 1)",
		"1=1 INTO OUTFILE '/tmp/x'",
		"id = (DELETE FROM users)",
		"1=1 UNION SELECT 1; SELECT 2",
	}
	for _, w := range bad {
		for _, driver := range []string{"postgres", "mysql", "sqlite"} {
			if err := dbWhereCheck(driver, w); err == nil {
				t.Errorf("%s %q was accepted", driver, w)
			}
		}
	}
	// In MySQL a backslash escapes the quote, so this is one string.
	escaped := `id = '\'; DROP TABLE t; -- '`
	if err := dbWhereCheck("mysql", escaped); err != nil {
		t.Errorf("mysql escaped quote refused: %v", err)
	}
	// Everywhere else the string ends at the second quote, then ";" follows.
	for _, driver := range []string{"postgres", "sqlite"} {
		if err := dbWhereCheck(driver, escaped); err == nil {
			t.Errorf("%s accepted a string that ends early", driver)
		}
	}
	for _, w := range []string{"1=1 # comment", "1=1 /*! DROP TABLE t */"} {
		if err := dbWhereCheck("mysql", w); err == nil {
			t.Errorf("mysql %q was accepted", w)
		}
	}
	for _, w := range []string{"$$ x $$", "$a$ ; $a$", "1=1 $$"} {
		if err := dbWhereCheck("postgres", w); err == nil {
			t.Errorf("postgres %q was accepted", w)
		}
	}
	if err := dbWhereCheck("postgres", "1=1; DROP"); err == nil || strings.Contains(err.Error(), "\n") {
		t.Errorf("the refusal must be one short line, got %v", err)
	}
}

func TestDBConsoleStopDropsPendingTableAnswers(t *testing.T) {
	for _, conn := range []string{"", "local", "other"} {
		s := &dbConsoleState{tableGen: 4}
		s.stop(conn)
		if s.tableGen == 4 {
			t.Fatalf("stop(%q) must invalidate answers that are still on their way", conn)
		}
	}
}

func TestDBTableSQLBuildersRefuseABadWhereThemselves(t *testing.T) {
	tv := &dbTableView{dbTable: dbTable{driver: "postgres", schema: "public", name: "users"}, total: -1}
	for _, w := range []string{"1=1; DROP TABLE users", "x -- y", "a = 1) OR (b = 2"} {
		tv.where = w
		if _, err := tv.selectSQL(101); err == nil {
			t.Errorf("selectSQL accepted %q", w)
		}
		if _, err := tv.countSQL(); err == nil {
			t.Errorf("countSQL accepted %q", w)
		}
	}
}

func TestDBTableFailedPageGoesBackToTheLastGoodOffset(t *testing.T) {
	tv := newDBTableView(dbTable{driver: "postgres", name: "t"})
	ok := dbclient.Result{Columns: []dbclient.Column{{Name: "a"}}, Rows: [][]dbclient.Cell{{{Text: "1"}}, {{Text: "2"}}, {{Text: "3"}}}}
	tv.offset = 100
	tv.finish(&ok, 2)
	if tv.good != 100 || !tv.more || len(ok.Rows) != 2 {
		t.Fatalf("good page: good=%d more=%v rows=%d", tv.good, tv.more, len(ok.Rows))
	}
	tv.offset = 200 // ] asked for the next page ...
	failed := dbclient.Result{Err: context.Canceled}
	tv.finish(&failed, 2) // ... and the user pressed Esc
	if tv.offset != 100 {
		t.Fatalf("offset after a cancelled page = %d, want 100", tv.offset)
	}
}

func TestDBTableEmptyPageKeepsItsColumns(t *testing.T) {
	tv := newDBTableView(dbTable{driver: "sqlite", name: "t"})
	tv.cols = []string{"id", "name"}
	res := dbclient.Result{}
	tv.finish(&res, 100)
	if len(res.Columns) != 2 || res.Columns[1].Name != "name" || tv.more {
		t.Fatalf("empty page: %+v more=%v", res.Columns, tv.more)
	}
}

func TestDBTablePostgresEStringsCannotHideAFunctionCall(t *testing.T) {
	for _, w := range []string{
		`x = E'\'' or lo_export(16400,'/tmp/pwn') > 0 or x = E'\''`,
		`x = e'\'' or lo_export(16400,'/tmp/pwn') > 0 or x = e'\''`,
		`x = E'\'' UNION SELECT setval('s', 1) --`,
		`x = E'\'' UNION SELECT pg_sleep(100) WHERE E'\'' = ''`,
	} {
		tv := &dbTableView{dbTable: dbTable{driver: "postgres", schema: "public", name: "t"}, total: -1, where: w}
		if sql, err := tv.selectSQL(101); err == nil {
			t.Errorf("selectSQL accepted %q as %s", w, sql)
		}
		if sql, err := tv.countSQL(); err == nil {
			t.Errorf("countSQL accepted %q as %s", w, sql)
		}
	}
	// An E string is one string: a ; or a comment inside it is only text.
	for _, w := range []string{`name = E'it\'s; -- fine'`, `name = E'a\\'`} {
		if err := dbWhereCheck("postgres", w); err != nil {
			t.Errorf("legit E string %q refused: %v", w, err)
		}
	}
	if err := dbWhereCheck("postgres", `x = E'\''; DROP TABLE t`); err == nil {
		t.Error("a ; after an E string was accepted")
	}
}
