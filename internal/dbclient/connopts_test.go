package dbclient

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConnOptionsStartSQLPostgres(t *testing.T) {
	o := ConnOptions{
		Schema:    "app, public",
		TimeZone:  "Europe/Berlin",
		Isolation: "repeatable read",
		InitSQL:   "SET work_mem = '64MB'; SET statement_timeout = 5000;",
	}
	got := o.StartSQL("postgres")
	want := []string{
		`SET search_path TO "app", "public"`,
		`SET TIME ZONE 'Europe/Berlin'`,
		`SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL REPEATABLE READ`,
		`SET work_mem = '64MB'`,
		`SET statement_timeout = 5000`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StartSQL(postgres) =\n%q\nwant\n%q", got, want)
	}
}

func TestConnOptionsStartSQLMySQL(t *testing.T) {
	o := ConnOptions{Schema: "shop", TimeZone: "+00:00", Isolation: "read committed"}
	got := o.StartSQL("mariadb")
	want := []string{
		"USE `shop`",
		"SET time_zone = '+00:00'",
		"SET SESSION TRANSACTION ISOLATION LEVEL READ COMMITTED",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StartSQL(mysql) = %q, want %q", got, want)
	}
}

func TestConnOptionsStartSQLSQLiteAndRedis(t *testing.T) {
	o := ConnOptions{Schema: "main", TimeZone: "UTC", Isolation: "serializable", InitSQL: "PRAGMA foreign_keys = ON"}
	if got := o.StartSQL("sqlite"); !reflect.DeepEqual(got, []string{"PRAGMA foreign_keys = ON"}) {
		t.Fatalf("sqlite StartSQL = %q, want only the init SQL", got)
	}
	if got := o.StartSQL("redis"); len(got) != 0 {
		t.Fatalf("redis StartSQL = %q, want none", got)
	}
	if got := (ConnOptions{}).StartSQL("postgres"); len(got) != 0 {
		t.Fatalf("empty options StartSQL = %q, want none", got)
	}
}

func TestConnOptionsCheckRefusesBadValues(t *testing.T) {
	bad := []struct {
		name   string
		driver string
		o      ConnOptions
	}{
		{"isolation", "postgres", ConnOptions{Isolation: "chaos"}},
		{"tx mode", "postgres", ConnOptions{TxMode: "sometimes"}},
		{"idle", "postgres", ConnOptions{IdleMinutes: -1}},
		{"time zone quote", "postgres", ConnOptions{TimeZone: "UTC'; DROP TABLE x; --"}},
		{"empty schema item", "postgres", ConnOptions{Schema: "a,,b"}},
		{"schema nul", "mysql", ConnOptions{Schema: "a\x00b"}},
		{"socket relative", "postgres", ConnOptions{Socket: "run/pg"}},
		{"socket on sqlite", "sqlite", ConnOptions{Socket: "/run/x.sock"}},
		{"bad pattern", "postgres", ConnOptions{Objects: []string{"[abc"}}},
		{"bad read-only pattern", "postgres", ConnOptions{ReadOnlyTables: []string{"public.[x"}}},
		{"init sql unterminated", "postgres", ConnOptions{InitSQL: "SET a = 'x"}},
	}
	for _, tc := range bad {
		if err := tc.o.Check(tc.driver); err == nil {
			t.Errorf("%s: Check accepted %+v", tc.name, tc.o)
		}
	}
	good := ConnOptions{
		Schema: "app", TimeZone: "America/New_York", Isolation: "Serializable", TxMode: "manual",
		IdleMinutes: 30, Socket: "/var/run/postgresql", Databases: []string{"app*"},
		Schemas: []string{"public"}, Objects: []string{"user_*", "!*_tmp"}, ReadOnlyTables: []string{"audit.*"},
	}
	if err := good.Check("postgres"); err != nil {
		t.Fatalf("Check refused good options: %v", err)
	}
}

func TestConnOptionsIdleTimeout(t *testing.T) {
	if got := (&ConnOptions{}).IdleTimeout(); got != 0 {
		t.Fatalf("no idle minutes: %v, want 0 (never)", got)
	}
	if got := (&ConnOptions{IdleMinutes: 15}).IdleTimeout(); got != 15*time.Minute {
		t.Fatalf("IdleTimeout = %v, want 15m", got)
	}
	var nilOpts *ConnOptions
	if got := nilOpts.IdleTimeout(); got != 0 {
		t.Fatalf("nil options IdleTimeout = %v", got)
	}
}

func TestConnOptionsVisibilityFilters(t *testing.T) {
	o := &ConnOptions{
		Databases: []string{"app", "app_*"},
		Schemas:   []string{"public", "billing"},
		Objects:   []string{"user*", "order*", "!*_tmp"},
	}
	for name, want := range map[string]bool{"app": true, "app_test": true, "postgres": false} {
		if got := o.ShowDatabase(name); got != want {
			t.Errorf("ShowDatabase(%q) = %v, want %v", name, got, want)
		}
	}
	for name, want := range map[string]bool{"public": true, "billing": true, "pg_catalog": false} {
		if got := o.ShowSchema(name); got != want {
			t.Errorf("ShowSchema(%q) = %v, want %v", name, got, want)
		}
	}
	for name, want := range map[string]bool{"users": true, "orders": true, "users_tmp": false, "invoices": false} {
		if got := o.ShowObject(name); got != want {
			t.Errorf("ShowObject(%q) = %v, want %v", name, got, want)
		}
	}
	onlyHide := &ConnOptions{Objects: []string{"!*_bak"}}
	if !onlyHide.ShowObject("users") || onlyHide.ShowObject("users_bak") {
		t.Fatalf("hide-only patterns must show all but the hidden ones")
	}
	var none *ConnOptions
	if !none.ShowDatabase("x") || !none.ShowSchema("x") || !none.ShowObject("x") {
		t.Fatalf("nil options must show everything")
	}
}

func TestConnOptionsTableReadOnly(t *testing.T) {
	o := &ConnOptions{ReadOnlyTables: []string{"audit.*", "settings", "public.ledger"}}
	cases := []struct {
		schema, table string
		want          bool
	}{
		{"audit", "events", true},
		{"public", "settings", true},
		{"billing", "settings", true},
		{"public", "ledger", true},
		{"billing", "ledger", false},
		{"public", "users", false},
	}
	for _, c := range cases {
		if got := o.TableReadOnly(c.schema, c.table); got != c.want {
			t.Errorf("TableReadOnly(%q, %q) = %v, want %v", c.schema, c.table, got, c.want)
		}
	}
	var none *ConnOptions
	if none.TableReadOnly("a", "b") {
		t.Fatalf("nil options make no table read-only")
	}
}

func TestConnUnixSocketDSN(t *testing.T) {
	pg := Conn{ID: "pg", Driver: "postgres", User: "me", Database: "app", Options: &ConnOptions{Socket: "/var/run/postgresql"}}
	dsn := pg.DSN("")
	if !strings.Contains(dsn, "host=%2Fvar%2Frun%2Fpostgresql") || strings.Contains(dsn, "localhost") {
		t.Fatalf("postgres socket DSN = %q, want the socket dir as host param", dsn)
	}
	my := Conn{ID: "my", Driver: "mysql", User: "me", Database: "shop", Options: &ConnOptions{Socket: "/run/mysqld/mysqld.sock"}}
	if dsn := my.DSN(""); !strings.Contains(dsn, "@unix(/run/mysqld/mysqld.sock)/shop") {
		t.Fatalf("mysql socket DSN = %q, want unix(...)", dsn)
	}
	if err := Validate(Conn{ID: "pg", Driver: "postgres", Options: &ConnOptions{Socket: "/var/run/postgresql"}}); err != nil {
		t.Fatalf("socket connection needs no host: %v", err)
	}
}

func TestParseConfigConnOptions(t *testing.T) {
	data := []byte(`{"connections": [
	  {"id": "ok", "driver": "postgres", "host": "h", "options": {"schema": "app", "idleMinutes": 10, "readOnlyTables": ["audit.*"]}},
	  {"id": "bad", "driver": "postgres", "host": "h", "options": {"isolation": "chaos"}}
	]}`)
	conns, err := ParseConfig(data, SourceUser, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if conns[0].Options == nil || conns[0].Options.Schema != "app" || conns[0].Options.IdleMinutes != 10 || conns[0].Problem != "" {
		t.Fatalf("ok connection = %+v (options %+v)", conns[0], conns[0].Options)
	}
	if !strings.Contains(conns[1].Problem, "isolation") {
		t.Fatalf("bad options problem = %q, want an isolation problem", conns[1].Problem)
	}
}
