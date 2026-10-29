package dbclient

import (
	"strings"
	"testing"
)

// Each problem of a databases.json entry is marked on that entry, so the
// tree can show it instead of failing the whole file.
func TestParseConfigMarksEachProblem(t *testing.T) {
	body := `{"connections":[
	  {"id":"Bad Id!","driver":"postgres","host":"h","port":5432},
	  {"id":"dup","driver":"postgres","host":"h","port":5432},
	  {"id":"dup","driver":"postgres","host":"h","port":5432},
	  {"id":"url-pw","driver":"postgres","url":"postgres://u:hunter2@h/db"},
	  {"id":"oracle","driver":"oracle","host":"h","port":1521},
	  {"id":"ref","driver":"postgres","host":"h","port":5432,"password":"${vault:x}"}
	]}`
	cs, err := ParseConfig([]byte(body), SourceProject, "p", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"invalid connection id", "", "duplicate connection id", "literal password in project config", "unsupported database driver", "literal password in project config"}
	for i, w := range want {
		if cs[i].Problem != w {
			t.Errorf("%s: problem %q, want %q", cs[i].ID, cs[i].Problem, w)
		}
	}
	user, err := ParseConfig([]byte(`{"connections":[{"id":"u","driver":"postgres","url":"postgres://u:pw@h/db"}]}`), SourceUser, "u", t.TempDir())
	if err != nil || !strings.Contains(user[0].Warning, "use the secret store") || user[0].Problem != "" {
		t.Errorf("user url password: %+v %v", user, err)
	}
	if _, err := ParseConfig([]byte(`{"connections": [`), SourceUser, "u", t.TempDir()); err == nil {
		t.Error("broken JSON was accepted")
	}
}

// Validate is the add-connection form's check.
func TestValidateConnection(t *testing.T) {
	for _, tc := range []struct {
		c    Conn
		want string
	}{
		{Conn{ID: "a", Driver: "sqlite"}, "sqlite path is required"},
		{Conn{ID: "a", Driver: "postgres", Host: "h"}, "host and valid port are required"},
		{Conn{ID: "a", Driver: "mysql", Host: "h", Port: 70000}, "host and valid port are required"},
		{Conn{ID: "a", Driver: "postgres", Host: "h", Port: 5432, Source: SourceProject, Password: "pw"}, "password must be a reference"},
		{Conn{ID: "a", Driver: "postgres", Host: "h", Port: 5432, Source: SourceProject, URL: "postgres://u:pw@h/db"}, "password must be a reference"},
		{Conn{ID: "a", Driver: "postgres", Host: "h", Port: 5432, Source: SourceProject, Password: "${env:PGPASS}"}, ""},
		{Conn{ID: "a", Driver: "sqlite", Path: "x.db"}, ""},
		{Conn{ID: "a", Driver: "mariadb", Host: "h", Port: 3306}, ""},
	} {
		err := Validate(tc.c)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || err.Error() != tc.want) {
			t.Errorf("Validate(%+v) = %v, want %q", tc.c, err, tc.want)
		}
	}
}

// The PostgreSQL URL keeps the user without a password, and a password
// given at connect time goes into a URL connection.
func TestPostgresDSNUserAndPassword(t *testing.T) {
	if got := (Conn{Driver: "postgres", Host: "h", User: "amy", Database: "db"}).DSN(""); got != "postgres://amy@h/db" {
		t.Errorf("no password = %q", got)
	}
	if got := (Conn{Driver: "postgres", URL: "postgres://amy@h:5433/db?sslmode=disable"}).DSN("p@ss"); got != "postgres://amy:p%40ss@h:5433/db?sslmode=disable" {
		t.Errorf("url + password = %q", got)
	}
	if got := (Conn{Driver: "mysql", URL: "mysql://amy@h:3307/shop?charset=utf8mb4"}).DSN("pw"); !strings.HasPrefix(got, "amy:pw@tcp(h:3307)/shop?") || !strings.Contains(got, "charset=utf8mb4") {
		t.Errorf("mysql url = %q", got)
	}
}

// More cases the edit check must refuse or take.
func TestSimpleSelectMoreShapes(t *testing.T) {
	for _, c := range []struct{ sql, dialect, table string }{
		{"SELECT [id] FROM [my table]", "sqlite", "my table"},
		{`SELECT "a""b" FROM t WHERE x = 'it''s'`, "postgres", "t"},
		{`SELECT * FROM t WHERE x = "a\"b"`, "mysql", "t"},
		{"SELECT * FROM t WHERE x IN (SELECT y FROM u)", "postgres", "t"},
	} {
		if got, why := SimpleSelect(c.sql, c.dialect); why != "" || got.Table != c.table {
			t.Errorf("%q = %+v (%s)", c.sql, got, why)
		}
	}
	for _, sql := range []string{
		"SELECT t. FROM t",
		"SELECT * FROM s.",
		"SELECT * FROM t AS",
		"SELECT * FROM t WHERE a = 1 UNION SELECT * FROM u",
		"SELECT * FROM t WHERE (a = 1; DELETE FROM t)",
		"SELECT * FROM t /* open",
		"SELECT * FROM t WHERE a = 'open",
		`SELECT * FROM "open`,
		"SELECT * FROM [open",
		"SELECT",
	} {
		if got, why := SimpleSelect(sql, "sqlite"); why == "" {
			t.Errorf("%q was taken: %+v", sql, got)
		}
	}
}
