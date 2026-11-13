package dbclient

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseURLForEverySchemeFamily(t *testing.T) {
	for _, c := range []struct {
		raw   string
		check func(Conn) bool
	}{
		{"mysql://u:p@tcp(db.host:3307)/app", func(c Conn) bool {
			return c.Driver == "mysql" && c.Host == "db.host" && c.Port == 3307 && c.Database == "app"
		}},
		{"mariadb://u@h/d", func(c Conn) bool { return c.Driver == "mysql" }},
		{"rediss://:pw@h:6380/2", func(c Conn) bool { return c.Driver == "redis" && c.DB == 2 }},
		{"file:/tmp/x.db", func(c Conn) bool { return c.Driver == "sqlite" }},
		{"sqlcipher:///tmp/x.db", func(c Conn) bool { return c.Driver == "sqlite" && c.Preset == SQLCipherPreset && c.URL == "" }},
		{"firebird://u@h:3051/db", func(c Conn) bool { return c.Driver == "firebird" && c.Host == "h" }},
		{"exa://u@h/schema", func(c Conn) bool { return c.Driver == "exasol" }},
		{"libsql://h:8080?authToken=abc&x=1", func(c Conn) bool {
			return c.Driver == "libsql" && c.Password == "abc" && !strings.Contains(c.URL, "abc") && c.Port == 8080
		}},
		{"libsql://h", func(c Conn) bool { return c.Driver == "libsql" && c.Port == 0 }},
		{"mongodb://u:p@h1:27018/db?replicaSet=rs&w=1&w=2", func(c Conn) bool {
			return c.Driver == "mongodb" && c.Password == "p" && c.Port == 27018 && c.Params["replicaSet"] == "rs" && c.Params["w"] == "2" && !strings.Contains(c.URL, ":p@")
		}},
		{"mongodb://h", func(c Conn) bool { return c.Port == 27017 && c.User == "" }},
		{"mongodb://:p@h", func(c Conn) bool { return c.User == "" }},
		{"mongodb+srv://u@cluster.example/db", func(c Conn) bool { return c.Port == 0 && c.Host == "cluster.example" }},
		{"etcds://u:p@h:2379/x", func(c Conn) bool { return c.Driver == "etcd" && c.TLS && c.User == "u" && c.Password == "p" }},
		{"memcached://h", func(c Conn) bool { return c.Driver == "memcached" }},
	} {
		got, err := ParseURL(c.raw)
		if err != nil || !c.check(got) {
			t.Errorf("ParseURL(%q) = %+v, %v", c.raw, got, err)
		}
	}
	for _, bad := range []string{"mongodb://h:notaport", "http://%zz", "etcd://h:notaport", "postgres://h:notaport/d"} {
		if _, err := ParseURL(bad); err == nil {
			t.Errorf("ParseURL(%q) should fail", bad)
		}
	}
}

func TestMongoURIAndScrubForms(t *testing.T) {
	got, err := mongoURI(Conn{Host: "h", Port: 0, Database: "d", User: "u", Params: map[string]string{"tls": "true", "authSource": "own"}}, "p@ss")
	if err != nil || !strings.Contains(got, "h:27017") || !strings.Contains(got, "authSource=own") || strings.Contains(got, "authSource=admin") || !strings.Contains(got, "u:p%40ss@") {
		t.Errorf("mongoURI = %q, %v", got, err)
	}
	got, _ = mongoURI(Conn{Host: "h", User: "u"}, "")
	if !strings.Contains(got, "authSource=admin") || !strings.Contains(got, "u@h") {
		t.Errorf("no password = %q", got)
	}
	got, _ = mongoURI(Conn{URL: "mongodb://me@h/d"}, "pw")
	if !strings.Contains(got, "me:pw@") {
		t.Errorf("the user of the URL = %q", got)
	}
	err = errors.New("auth failed for p@ss / p%40ss / p ss")
	scrubbed := mongoScrub(err, "p@ss")
	if strings.Contains(scrubbed.Error(), "p@ss") || strings.Contains(scrubbed.Error(), "p%40ss") {
		t.Errorf("scrub = %v", scrubbed)
	}
	if mongoScrub(nil, "x") != nil || mongoScrub(err, "") != err {
		t.Error("nothing to scrub")
	}
	for _, src := range []string{`db.c.insertOne({a: 1}).explain()`, `db.c.drop().explain()`} {
		if _, err := MongoServerCommand(mustParse(t, src), 10); err == nil || !strings.Contains(err.Error(), "cannot be explained") {
			t.Errorf("%s = %v", src, err)
		}
	}
	for _, src := range []string{`db.c.find({}, 1)`, `db.c.findOne({}, 1)`} {
		if _, err := MongoServerCommand(mustParse(t, src), 10); err == nil || !strings.Contains(err.Error(), "projection") {
			t.Errorf("%s = %v", src, err)
		}
	}
	if cmd, err := MongoServerCommand(mustParse(t, `db.c.find()`), 0); err != nil || MongoShellJSON(cmd) == "" {
		t.Errorf("a zero page size = %v", err)
	}
}

func TestDuckDBDialectAndNoTransactions(t *testing.T) {
	d := DuckDBDialect{}
	if d.Placeholder(1) != "?" || d.Limit(0, 0) != "" || !strings.Contains(d.Limit(5, 2), "LIMIT 5") || d.ReadOnlyBegin() == "x" {
		t.Error("DuckDBDialect")
	}
	fakeDuckDB(t, `echo '[{"1":1}]'`)
	s, err := NewDuckDBSession(context.Background(), Conn{Driver: "duckdb", Path: "x.duckdb"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, f := range []func(context.Context) error{s.Begin, s.Commit, s.Rollback} {
		if f(context.Background()) == nil {
			t.Error("DuckDB has no transactions here")
		}
	}
	if got := sqlLiteral(uint8(3)); got != "'3'" && got != "3" {
		t.Errorf("sqlLiteral of an odd type = %q", got)
	}
}
