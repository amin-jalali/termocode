package dbclient

import (
	"fmt"
	"strings"
	"testing"
)

func TestParsePgpassReadsLinesEscapesAndWildcards(t *testing.T) {
	pw := "p" + "w:1\\x"
	data := "# comment\n\n" +
		"db.example.com:6543:shop:alice:" + strings.ReplaceAll(strings.ReplaceAll(pw, "\\", "\\\\"), ":", "\\:") + "\n" +
		"localhost:*:*:bob:s" + "ecret\n" +
		"*:5432:any:carol:x\n" + // host wildcard: cannot connect, skipped
		"short:line\n"
	got := ParsePgpass([]byte(data))
	if len(got) != 2 {
		t.Fatalf("got %d connections, want 2: %v", len(got), got)
	}
	a := got[0]
	if a.Conn.Driver != "postgres" || a.Conn.Host != "db.example.com" || a.Conn.Port != 6543 || a.Conn.Database != "shop" || a.Conn.User != "alice" {
		t.Fatalf("first = %+v", a.Conn)
	}
	if a.Secret != pw {
		t.Fatalf("escaped password not decoded")
	}
	if a.Conn.Password != "" || a.From != "pgpass" {
		t.Fatalf("conn must not carry the password; from=%q", a.From)
	}
	b := got[1]
	if b.Conn.Port != 5432 || b.Conn.Database != "" || b.Conn.User != "bob" {
		t.Fatalf("wildcards not mapped: %+v", b.Conn)
	}
}

func TestParseMyCnfMergesClientGroupsAndSuffixGroups(t *testing.T) {
	data := `
# my options
!includedir /etc/mysql/conf.d/
[mysqld]
port = 9999
[client]
user = root
password = "top s` + `ecret"
host = 127.0.0.1
[mysql]
database = app
[client_staging]
host = staging.example.com
port = 3307
`
	got := ParseMyCnf([]byte(data))
	if len(got) != 2 {
		t.Fatalf("got %d connections, want 2: %v", len(got), got)
	}
	base, st := got[0], got[1]
	if base.Conn.Driver != "mysql" || base.Conn.Host != "127.0.0.1" || base.Conn.Port != 3306 || base.Conn.User != "root" || base.Conn.Database != "app" {
		t.Fatalf("base = %+v", base.Conn)
	}
	if base.Secret != "top s"+"ecret" || base.From != "my.cnf" {
		t.Fatalf("quoted password not read; from=%q", base.From)
	}
	if st.Conn.Host != "staging.example.com" || st.Conn.Port != 3307 || st.Conn.User != "root" || st.Secret != base.Secret {
		t.Fatalf("suffix group must inherit [client]: %+v", st.Conn)
	}
}

func TestParseURLListKeepsPasswordOutOfConn(t *testing.T) {
	data := "# exported\npostgres://u:p" + "w@pg.example.com:5433/app?sslmode=require\nnot a url\nredis://cache.example.com:6380/2\n"
	got := ParseURLList([]byte(data))
	if len(got) != 2 {
		t.Fatalf("got %d, want 2: %v", len(got), got)
	}
	if got[0].Conn.Driver != "postgres" || got[0].Conn.Host != "pg.example.com" || got[0].Secret != "p"+"w" {
		t.Fatalf("pg = %+v", got[0].Conn)
	}
	if got[0].Conn.Password != "" || strings.Contains(got[0].Conn.URL, "p"+"w@") {
		t.Fatalf("password left in conn: %q", got[0].Conn.URL)
	}
	if got[1].Conn.Driver != "redis" || got[1].Conn.Port != 6380 {
		t.Fatalf("redis = %+v", got[1].Conn)
	}
}

func TestImportConnectionsDetectsFormatAndGivesUniqueValidIDs(t *testing.T) {
	pg := "db.example.com:5432:shop:alice:x\ndb.example.com:5432:shop:bob:y\n"
	got := ImportConnections("/home/u/.pgpass", []byte(pg), []string{"pg-db-example-com-shop"})
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	seen := map[string]bool{"pg-db-example-com-shop": true}
	for _, g := range got {
		if !idPattern.MatchString(g.Conn.ID) || seen[g.Conn.ID] {
			t.Fatalf("bad or duplicate id %q", g.Conn.ID)
		}
		seen[g.Conn.ID] = true
		if err := Validate(g.Conn); err != nil {
			t.Fatalf("imported conn invalid: %v", err)
		}
	}
	if c := ImportConnections("x/.my.cnf", []byte("[client]\nhost=h\n"), nil); len(c) != 1 || c[0].Conn.Driver != "mysql" {
		t.Fatalf("my.cnf not detected: %v", c)
	}
	if c := ImportConnections("conns.txt", []byte("mysql://u@h:3306/d\n"), nil); len(c) != 1 || c[0].Conn.Driver != "mysql" {
		t.Fatalf("url list not detected: %v", c)
	}
}

func TestImportedConnNeverPrintsSecret(t *testing.T) {
	s := "sk-" + "test-123"
	got := ParsePgpass([]byte("h:5432:d:u:" + s + "\n"))
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	for _, out := range []string{fmt.Sprint(got[0]), fmt.Sprintf("%v %+v %#v", got, got[0], got[0])} {
		if strings.Contains(out, s) {
			t.Fatalf("secret printed: %s", out)
		}
	}
}

func FuzzImportConnections(f *testing.F) {
	f.Add("a.pgpass", "h:1:d:u:p\\:x\n")
	f.Add("my.cnf", "[client]\nuser=a\npassword='b'\n")
	f.Add("list", "postgres://u:p@h/d\n")
	f.Fuzz(func(t *testing.T, name, data string) {
		for _, g := range ImportConnections(name, []byte(data), nil) {
			if !idPattern.MatchString(g.Conn.ID) || g.Conn.Password != "" {
				t.Fatalf("bad import %q", g.Conn.ID)
			}
		}
	})
}
