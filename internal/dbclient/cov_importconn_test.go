package dbclient

import (
	"strings"
	"testing"
)

func TestImportConnectionsFormatsAndIDs(t *testing.T) {
	pgpass := "# comment\n\nhost1:5433:db1:me:s3\\:cret\n*:5432:*:*:skipped\nhost2:*:*:*:pw2\nhost3:99999:d:u:p\nhost4:abc:d:u:p\nshort:line\n"
	got := ImportConnections(".pgpass", []byte(pgpass), []string{"pg-host1-db1"})
	if len(got) != 2 || got[0].Secret != "s3:cret" || got[0].Conn.Port != 5433 || got[1].Conn.Port != 5432 || got[1].Conn.Database != "" {
		t.Fatalf("pgpass = %+v", got)
	}
	if got[0].Conn.ID != "pg-host1-db1-2" {
		t.Errorf("a taken id gets a number, got %q", got[0].Conn.ID)
	}
	cnf := "# c\n; c\n!include x\n[client]\nhost = db.example\nport = 3307\nuser='root'\npassword = \"p # w\" \n[client_prod]\nhost = prod\npassword = x # tail\n[mysqld]\nport = 1\n[mysqldump]\nuser = d\n[mysql_extra]\ndatabase = d2\n[]\nstray = 1\n"
	conns := ImportConnections("my.cnf", []byte(cnf), nil)
	if len(conns) < 2 || conns[0].Conn.Host != "db.example" || conns[0].Conn.Port != 3307 || conns[0].Secret != "p # w" {
		t.Fatalf("my.cnf = %+v", conns)
	}
	for _, c := range conns {
		if c.Conn.Driver != "mysql" || c.From != "my.cnf" {
			t.Errorf("conn = %+v", c)
		}
	}
	if conns[1].Secret != "x" {
		t.Errorf("a trailing comment is cut: %q", conns[1].Secret)
	}
	if ParseMyCnf([]byte("[client]\n")) != nil || ParseMyCnf([]byte("host = x\n")) != nil {
		t.Error("a group without values, or values without a group, make no connection")
	}
	if c := ParseMyCnf([]byte("[client]\nuser = u\nport = 99999\n")); len(c) != 1 || c[0].Conn.Port != 3306 || c[0].Conn.Host != "localhost" {
		t.Errorf("defaults = %+v", c)
	}
	urls := "# list\n\nnot a url\npostgres://u:pw@h1:5432/d1\nmysql://u2@h2/d2\nsqlite:///tmp/x.db\npostgres://:pw@/nohost\nredis://h3:6379/0\n"
	list := ImportConnections("urls.txt", []byte(urls), nil)
	if len(list) != 3 {
		t.Fatalf("url list = %+v", list)
	}
	if list[0].Secret != "pw" || strings.Contains(list[0].Conn.URL, "pw") || list[0].Conn.Environment != "development" {
		t.Errorf("the password moves out of the URL: %+v", list[0])
	}
	// Unknown file names are guessed from the content.
	if got := ImportConnections("whatever", []byte("[client]\nhost = h\n"), nil); len(got) != 1 || got[0].Conn.Driver != "mysql" {
		t.Errorf("content guess = %+v", got)
	}
	if got := ImportConnections("whatever", []byte("h:1:d:u:p\n"), nil); len(got) != 1 || got[0].Conn.Driver != "postgres" {
		t.Errorf("content guess (pgpass) = %+v", got)
	}
	if got := ImportConnections("x.ini", []byte("[client]\nhost = h\n"), nil); len(got) != 1 {
		t.Errorf(".ini = %+v", got)
	}
	// IDs: prefix per driver, slugs, a cap on the length, numbers on a clash.
	used := map[string]bool{}
	long := Conn{Driver: "oracle", Host: strings.Repeat("host-name.", 8), Database: "db"}
	id := importID(long, used)
	if !strings.HasPrefix(id, "db-") || len(id) > 48 || strings.HasSuffix(id, "-") {
		t.Errorf("long id = %q", id)
	}
	used[id] = true
	if again := importID(long, used); again != id+"-2" && !strings.HasSuffix(again, "-2") {
		t.Errorf("clash = %q", again)
	}
	for in, want := range map[string]string{"Host.Example": "host-example", "--a--": "a", "": "", "x_y 9": "x_y-9"} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
	for _, c := range []ImportedConn{{Conn: Conn{ID: "a"}, Secret: "hide", From: "x"}} {
		if strings.Contains(c.String(), "hide") || strings.Contains(c.GoString(), "hide") {
			t.Error("an imported secret never shows in a printed value")
		}
	}
}
