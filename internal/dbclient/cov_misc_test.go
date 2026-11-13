package dbclient

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestReachErrorTextsAndHelpers(t *testing.T) {
	for _, c := range []struct {
		e    ReachError
		want string
	}{
		{ReachError{Addr: "db:5432"}, "Nothing answers at db:5432. Is the database running there?"},
		{ReachError{Addr: "localhost:5432"}, "Is the container's port published?"},
		{ReachError{Addr: "[::1]:5432"}, "Is the container's port published?"},
		{ReachError{Addr: "db:5432", Timeout: true}, "No answer from db:5432 in time."},
		{ReachError{Addr: "db:5432", Via: "me@jump"}, "SSH to me@jump works, but nothing answers at db:5432 on that server (connection refused)."},
		{ReachError{Addr: "db:5432", Via: "me@jump", Timeout: true}, "did not answer in time"},
		{ReachError{Addr: "db:5432", Via: "me@jump", Prohibited: true}, "does not allow tunnels"},
		{ReachError{Addr: "db:5432", Via: "me@jump", Reason: "no route."}, "could not be reached (no route)."},
		{ReachError{Addr: "db:5432", Hint: "Try the container x."}, "Try the container x."},
	} {
		if got := c.e.Error(); !strings.Contains(got, c.want) {
			t.Errorf("%+v = %q, want %q", c.e, got, c.want)
		}
	}
	cause := errors.New("deadline")
	if !errors.Is(&ReachError{Err: cause}, cause) {
		t.Error("Unwrap gives the deadline")
	}
	for _, c := range []struct {
		user, host string
		port       int
		want       string
	}{{"me", "h", 0, "me@h"}, {"", "h", 22, "h"}, {"me", "h", 2222, "me@h:2222"}} {
		if got := SSHLabel(c.user, c.host, c.port); got != c.want {
			t.Errorf("SSHLabel = %q, want %q", got, c.want)
		}
	}
	if ReachText(nil) != "" || ReachText(errors.New("plain")) != "" {
		t.Error("ReachText of nothing")
	}
	if got := ReachText(&ReachError{Addr: "a:1"}); !strings.HasPrefix(got, "Nothing answers") {
		t.Errorf("ReachText of a ReachError = %q", got)
	}
	flat := errors.New("driver: dial failed: Nothing answers at a:1. Is the database running there?")
	if got := ReachText(flat); !strings.HasPrefix(got, "Nothing answers at a:1") {
		t.Errorf("flattened = %q", got)
	}
	if got := ReachText(errors.New("x: SSH to u@h works, but nothing answers")); !strings.HasPrefix(got, "SSH to u@h") {
		t.Errorf("ssh line = %q", got)
	}
	if got := ReachText(errors.New("SSH to u@h only")); got != "" {
		t.Errorf("an SSH line without the rest = %q", got)
	}
	for in, want := range map[string]bool{"localhost": true, "LOCALHOST": true, "127.0.0.1": true, "[::1]": true, "db.example": false} {
		if loopbackHost(in) != want {
			t.Errorf("loopbackHost(%s)", in)
		}
	}
	refused := tunnelReachError("a:1", "u@h", &ssh.OpenChannelError{Reason: ssh.ConnectionFailed})
	if r := refused.(*ReachError); r.Prohibited || r.Reason != "" {
		t.Errorf("refused = %+v", r)
	}
	if r := tunnelReachError("a:1", "u@h", &ssh.OpenChannelError{Reason: ssh.Prohibited}).(*ReachError); !r.Prohibited {
		t.Errorf("prohibited = %+v", r)
	}
	if r := tunnelReachError("a:1", "u@h", errors.New("ssh: something else")).(*ReachError); r.Reason != "something else" {
		t.Errorf("other = %+v", r)
	}
	if r := tunnelReachError("a:1", "u@h", &ssh.OpenChannelError{Reason: ssh.ResourceShortage, Message: "full"}).(*ReachError); r.Reason == "" {
		t.Errorf("another channel reason = %+v", r)
	}
}

func TestEngineLabels(t *testing.T) {
	for c, want := range map[string]Conn{
		"PostgreSQL": {Driver: "postgres", Host: "db.example"}, "MySQL": {Driver: "mysql"}, "SQLite": {Driver: "sqlite"}, "Redis": {Driver: "redis"},
		"SQL Server": {Driver: "sqlserver", Host: "db.example"}, "Azure SQL": {Driver: "sqlserver", Host: "x.database.windows.net"},
		"Oracle": {Driver: "oracle"}, "mongodb": {Driver: "mongodb"},
	} {
		if got := want.EngineLabel(); got != c {
			t.Errorf("%+v label = %q, want %q", want, got, c)
		}
	}
	if e := (Conn{Driver: "firebird"}).EngineLabel(); e == "" || e == "firebird" {
		t.Errorf("an extra engine uses its title: %q", e)
	}
}

func TestValidateEveryDriverFamily(t *testing.T) {
	for name, c := range map[string]struct {
		conn Conn
		want string
	}{
		"bad id":         {Conn{ID: "bad id!", Driver: "postgres", Host: "h", Port: 1}, "invalid connection id"},
		"bad driver":     {Conn{ID: "a", Driver: "nope"}, "unsupported database driver"},
		"mongo no host":  {Conn{ID: "a", Driver: "mongodb"}, "host or URL"},
		"mongo bad port": {Conn{ID: "a", Driver: "mongodb", Host: "h", Port: 70000}, "invalid port"},
		"mongo password": {Conn{ID: "a", Driver: "mongodb", Host: "h", Source: SourceProject, Password: "literal"}, "password must be a reference"},
		"mongo url pass": {Conn{ID: "a", Driver: "mongodb", URL: "mongodb://u:p@h", Source: SourceProject}, "password must be a reference"},
		"libsql no url":  {Conn{ID: "a", Driver: "libsql"}, "server URL"},
		"libsql token":   {Conn{ID: "a", Driver: "libsql", URL: "libsql://h?authToken=abc", Source: SourceProject}, "password must be a reference"},
		"http no host":   {Conn{ID: "a", Driver: "etcd"}, "server address"},
		"http bad port":  {Conn{ID: "a", Driver: "etcd", Host: "h", Port: -1}, "valid port"},
		"http password":  {Conn{ID: "a", Driver: "etcd", Host: "h", Source: SourceProject, Password: "literal"}, "password must be a reference"},
		"sqlite no path": {Conn{ID: "a", Driver: "sqlite"}, "sqlite path"},
		"duckdb no path": {Conn{ID: "a", Driver: "duckdb"}, "duckdb path"},
		"bad options":    {Conn{ID: "a", Driver: "postgres", Host: "h", Port: 1, Options: &ConnOptions{TxMode: "weird"}}, "invalid options"},
		"mongo ok":       {Conn{ID: "a", Driver: "mongodb", Host: "h"}, ""},
		"libsql ok":      {Conn{ID: "a", Driver: "libsql", URL: "libsql://h"}, ""},
		"http ok":        {Conn{ID: "a", Driver: "etcd", Host: "h"}, ""},
	} {
		err := Validate(c.conn)
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s = %v, want %q", name, err, c.want)
		}
	}
}

func TestObjectActionSQLRefusalsAndForms(t *testing.T) {
	pg, my, lite := PostgresDialect{}, MySQLDialect{}, SQLiteDialect{}
	if _, err := ObjectActionSQL(nil, ObjectAction{Name: "t"}); err == nil {
		t.Error("no dialect: want error")
	}
	if _, err := ObjectActionSQL(pg, ObjectAction{}); err == nil {
		t.Error("no name: want error")
	}
	for _, c := range []struct {
		d    Dialect
		a    ObjectAction
		want string
		bad  string
	}{
		{pg, ObjectAction{Kind: ActDrop, Object: "materialized view", Name: "mv", Schema: "s"}, `DROP MATERIALIZED VIEW "s"."mv"`, ""},
		{my, ObjectAction{Kind: ActDrop, Object: "materialized view", Name: "mv"}, "", "no materialized views"},
		{my, ObjectAction{Kind: ActDrop, Object: "schema", Name: "d"}, "DROP DATABASE `d`", ""},
		{pg, ObjectAction{Kind: ActDrop, Object: "schema", Name: "s", Cascade: true}, `DROP SCHEMA "s" CASCADE`, ""},
		{lite, ObjectAction{Kind: ActDrop, Object: "schema", Name: "s"}, "", "no schemas"},
		{pg, ObjectAction{Kind: ActDrop, Object: "gadget", Name: "g"}, "", "cannot drop"},
		{pg, ObjectAction{Kind: ActCopyTable, Name: "t", NewName: "t2"}, "LIKE", ""},
		{my, ObjectAction{Kind: ActCopyTable, Name: "t", NewName: "t2"}, "CREATE TABLE `t2` LIKE `t`", ""},
		{lite, ObjectAction{Kind: ActCopyTable, Name: "t", NewName: "t2"}, "AS SELECT * FROM", ""},
		{pg, ObjectAction{Kind: ActCopyTable, Name: "t", NewName: "t"}, "", "new name"},
		{pg, ObjectAction{Kind: ActMakePK, Name: "t", Columns: []string{"a", "b"}}, `ADD PRIMARY KEY ("a", "b")`, ""},
		{pg, ObjectAction{Kind: ActMakePK, Name: "t"}, "", "at least one column"},
		{lite, ObjectAction{Kind: ActMakePK, Name: "t", Columns: []string{"a"}}, "", "SQLite cannot"},
		{pg, ObjectAction{Kind: ActMakePK, Name: "t", Columns: []string{" "}}, "", "no name"},
		{pg, ObjectAction{Kind: 99, Name: "t"}, "", "unknown object action"},
	} {
		got, err := ObjectActionSQL(c.d, c.a)
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("%+v = %v, want %q", c.a, err, c.bad)
			}
			continue
		}
		if err != nil || len(got) == 0 || !strings.Contains(strings.Join(got, ";"), c.want) {
			t.Errorf("%+v = %v, %v, want %q", c.a, got, err, c.want)
		}
	}
	for _, d := range []Dialect{pg, my, lite} {
		if q, args, err := DropDependentsQuery(d, "", "t"); err != nil || q == "" || len(args) == 0 {
			t.Errorf("DropDependentsQuery(%T) = %v %v", d, args, err)
		}
	}
	if _, _, err := DropDependentsQuery(SQLServerDialect{}, "", "t"); err == nil {
		t.Error("another dialect: want error")
	}
}
