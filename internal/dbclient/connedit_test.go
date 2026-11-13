package dbclient

import (
	"strings"
	"testing"
)

func editConns() []Conn {
	return []Conn{
		{ID: "prod", Name: "Prod", Driver: "postgres", Host: "db", Port: 5432, Password: "${secret:prod}", Comment: "main db",
			SSL: &SSLConfig{Mode: "verify-full", CA: "~/ca.pem"}},
		{ID: "dev", Driver: "mysql", Host: "localhost", Port: 3306},
	}
}

// Duplicate puts a copy right after the original with a free id and
// a "(copy)" name; the original is not changed.
func TestDuplicateConn(t *testing.T) {
	cs := editConns()
	out, id, err := DuplicateConn(cs, "prod")
	if err != nil || id != "prod-copy" || len(out) != 3 || out[1].ID != "prod-copy" || out[1].Name != "Prod (copy)" {
		t.Fatalf("got %v %q %v", err, id, out)
	}
	out[1].SSL.Mode = "disable"
	if cs[0].SSL.Mode != "verify-full" {
		t.Fatal("copy shares the SSL settings with the original")
	}
	out2, id2, _ := DuplicateConn(out, "prod")
	if id2 != "prod-copy-2" || len(out2) != 4 {
		t.Fatalf("second copy id %q", id2)
	}
	if _, _, err := DuplicateConn(cs, "nope"); err == nil {
		t.Fatal("unknown id must fail")
	}
}

// Rename checks the new id and refuses one that is taken.
func TestRenameConn(t *testing.T) {
	cs := editConns()
	if _, err := RenameConn(cs, "dev", "prod"); err == nil {
		t.Fatal("taken id must fail")
	}
	if _, err := RenameConn(cs, "dev", "Bad Id"); err == nil {
		t.Fatal("invalid id must fail")
	}
	out, err := RenameConn(cs, "dev", "local")
	if err != nil || out[1].ID != "local" || cs[1].ID != "dev" {
		t.Fatalf("got %v %v", err, out)
	}
}

func TestDeleteConn(t *testing.T) {
	cs := editConns()
	out, err := DeleteConn(cs, "prod")
	if err != nil || len(out) != 1 || out[0].ID != "dev" || len(cs) != 2 {
		t.Fatalf("got %v %v", err, out)
	}
	if _, err := DeleteConn(cs, "nope"); err == nil {
		t.Fatal("unknown id must fail")
	}
}

func TestCopySSL(t *testing.T) {
	cs := editConns()
	out, err := CopySSL(cs, "prod", "dev")
	if err != nil || out[1].SSL == nil || out[1].SSL.CA != "~/ca.pem" || cs[1].SSL != nil {
		t.Fatalf("got %v %v", err, out)
	}
	out[1].SSL.CA = "x"
	if out[0].SSL.CA != "~/ca.pem" {
		t.Fatal("SSL settings are shared")
	}
	if _, err := CopySSL(cs, "dev", "prod"); err == nil {
		t.Fatal("source without SSL must fail")
	}
}

// Copied JSON keeps secret references but never a literal password,
// also not one inside the URL.
func TestConnJSONDropsLiteralPasswords(t *testing.T) {
	pw := "hun" + "ter2"
	c := Conn{ID: "x", Driver: "postgres", Host: "h", Port: 5432, Password: pw, URL: "postgres://u:" + pw + "@h/db"}
	s, err := ConnJSON(c)
	if err != nil || strings.Contains(s, pw) {
		t.Fatalf("leak: %v %s", err, s)
	}
	ref, _ := ConnJSON(editConns()[0])
	if !strings.Contains(ref, "${secret:prod}") || !strings.Contains(ref, `"comment": "main db"`) {
		t.Fatalf("ref or comment lost: %s", ref)
	}
}

// Pasted JSON gets a free id, loses a literal password and must be valid.
func TestParseConnJSON(t *testing.T) {
	cs := editConns()
	pw := "hun" + "ter2"
	c, err := ParseConnJSON(`{"id":"dev","driver":"mariadb","host":"h","password":"`+pw+`"}`, cs)
	if err != nil || c.ID != "dev-copy" || c.Password != "" || c.Driver != "mysql" || c.Port != 3306 {
		t.Fatalf("got %v %+v", err, c)
	}
	if _, err := ParseConnJSON(`{"id":"z","driver":"informix","host":"h"}`, cs); err == nil {
		t.Fatal("unsupported driver must fail")
	}
	if _, err := ParseConnJSON(`not json`, cs); err == nil {
		t.Fatal("bad json must fail")
	}
}
