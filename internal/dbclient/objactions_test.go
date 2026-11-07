package dbclient

import (
	"reflect"
	"strings"
	"testing"
)

func TestObjectActionSQLPerDialect(t *testing.T) {
	pg, my, lite := PostgresDialect{}, MySQLDialect{}, SQLiteDialect{}
	cases := []struct {
		name string
		d    Dialect
		a    ObjectAction
		want []string
	}{
		{"pg drop cascade", pg, ObjectAction{Kind: ActDrop, Object: "table", Schema: "public", Name: "users", Cascade: true},
			[]string{`DROP TABLE "public"."users" CASCADE`}},
		{"pg drop view", pg, ObjectAction{Kind: ActDrop, Object: "view", Name: "v"}, []string{`DROP VIEW "v"`}},
		{"mysql drop ignores cascade", my, ObjectAction{Kind: ActDrop, Object: "table", Name: "t", Cascade: true}, []string{"DROP TABLE `t`"}},
		{"pg truncate", pg, ObjectAction{Kind: ActTruncate, Name: "t", Cascade: true}, []string{`TRUNCATE TABLE "t" CASCADE`}},
		{"mysql truncate", my, ObjectAction{Kind: ActTruncate, Schema: "app", Name: "t"}, []string{"TRUNCATE TABLE `app`.`t`"}},
		{"sqlite truncate is delete", lite, ObjectAction{Kind: ActTruncate, Name: "t"}, []string{`DELETE FROM "t"`}},
		{"pg create schema", pg, ObjectAction{Kind: ActCreateSchema, Name: "s"}, []string{`CREATE SCHEMA "s"`}},
		{"mysql create schema", my, ObjectAction{Kind: ActCreateSchema, Name: "s"}, []string{"CREATE DATABASE `s`"}},
		{"pg create db", pg, ObjectAction{Kind: ActCreateDatabase, Name: "d"}, []string{`CREATE DATABASE "d"`}},
		{"pg copy", pg, ObjectAction{Kind: ActCopyTable, Name: "t", NewName: "t_bak"},
			[]string{`CREATE TABLE "t_bak" (LIKE "t" INCLUDING ALL)`, `INSERT INTO "t_bak" SELECT * FROM "t"`}},
		{"mysql copy", my, ObjectAction{Kind: ActCopyTable, Name: "t", NewName: "t_bak"},
			[]string{"CREATE TABLE `t_bak` LIKE `t`", "INSERT INTO `t_bak` SELECT * FROM `t`"}},
		{"sqlite copy", lite, ObjectAction{Kind: ActCopyTable, Name: "t", NewName: "t_bak"},
			[]string{`CREATE TABLE "t_bak" AS SELECT * FROM "t"`}},
		{"pg make pk", pg, ObjectAction{Kind: ActMakePK, Name: "t", Columns: []string{"id", "org"}},
			[]string{`ALTER TABLE "t" ADD PRIMARY KEY ("id", "org")`}},
	}
	for _, c := range cases {
		got, err := ObjectActionSQL(c.d, c.a)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestObjectActionSQLRefusesWhatTheDialectCannotDo(t *testing.T) {
	lite := SQLiteDialect{}
	for _, a := range []ObjectAction{
		{Kind: ActCreateSchema, Name: "s"},
		{Kind: ActCreateDatabase, Name: "d"},
		{Kind: ActMakePK, Name: "t", Columns: []string{"id"}},
	} {
		if _, err := ObjectActionSQL(lite, a); err == nil {
			t.Fatalf("sqlite %v: want an error", a.Kind)
		}
	}
	bad := []ObjectAction{
		{Kind: ActDrop, Object: "table"},
		{Kind: ActDrop, Object: "table; DROP", Name: "t"},
		{Kind: ActCopyTable, Name: "t"},
		{Kind: ActCopyTable, Name: "t", NewName: "t"},
		{Kind: ActMakePK, Name: "t"},
	}
	for _, a := range bad {
		if _, err := ObjectActionSQL(PostgresDialect{}, a); err == nil {
			t.Fatalf("%+v: want an error", a)
		}
	}
	if _, err := ObjectActionSQL(nil, ObjectAction{Kind: ActDrop, Object: "table", Name: "t"}); err == nil {
		t.Fatal("nil dialect: want an error")
	}
}

func TestObjectActionSQLQuotesNames(t *testing.T) {
	got, err := ObjectActionSQL(PostgresDialect{}, ObjectAction{Kind: ActDrop, Object: "table", Name: `x"; DROP TABLE users; --`})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != `DROP TABLE "x""; DROP TABLE users; --"` {
		t.Fatalf("got %q", got[0])
	}
}

func TestDropDependentsQuery(t *testing.T) {
	for _, d := range []Dialect{PostgresDialect{}, MySQLDialect{}, SQLiteDialect{}} {
		q, args, err := DropDependentsQuery(d, "public", "users")
		if err != nil {
			t.Fatalf("%T: %v", d, err)
		}
		if strings.Contains(q, "users") {
			t.Fatalf("%T: the name must be an argument, not in the SQL: %s", d, q)
		}
		found := false
		for _, a := range args {
			if a == "users" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%T: args %v miss the table", d, args)
		}
	}
}

func TestBackupTableName(t *testing.T) {
	if got := BackupTableName("orders", "20261107"); got != "orders_backup_20261107" {
		t.Fatalf("got %q", got)
	}
}
