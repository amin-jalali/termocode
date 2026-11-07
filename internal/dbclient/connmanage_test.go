package dbclient

import (
	"strings"
	"testing"
)

func manageConns() []Conn {
	return []Conn{
		{ID: "pg", Name: "Shop", Driver: "postgres", Host: "db", Port: 5432, User: "app", Source: SourceUser,
			SSH: &SSHConfig{Host: "bastion", User: "me", Jump: []string{"j1"}}, Kube: nil},
		{ID: "lite", Driver: "sqlite", Path: "/tmp/a.db", Source: SourceUser},
	}
}

// DBX-50: the edit form changes name, comment, host, port, user and
// database; the result must still be a valid connection.
func TestEditConn(t *testing.T) {
	cs := manageConns()
	out, err := EditConn(cs, "pg", ConnEdit{Name: "Shop prod", Comment: "read only please", Host: "db2", Port: 6543, User: "ro", Database: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	if c := out[0]; c.Name != "Shop prod" || c.Comment != "read only please" || c.Host != "db2" || c.Port != 6543 || c.User != "ro" || c.Database != "shop" {
		t.Errorf("edited %+v", c)
	}
	if cs[0].Host != "db" {
		t.Error("the input list changed")
	}
	if _, err := EditConn(cs, "pg", ConnEdit{Host: "", Port: 5432}); err == nil {
		t.Error("an empty host was accepted")
	}
	if _, err := EditConn(cs, "nope", ConnEdit{}); err == nil {
		t.Error("unknown id accepted")
	}
	if _, err := EditConn(cs, "pg", ConnEdit{Host: "db", Port: 5432, Comment: "two\nlines"}); err != nil {
		t.Errorf("comment with a line break: %v", err)
	} else if out, _ := EditConn(cs, "pg", ConnEdit{Host: "db", Port: 5432, Comment: "two\nlines"}); strings.ContainsAny(out[0].Comment, "\n\r") {
		t.Errorf("comment keeps a line break: %q", out[0].Comment)
	}
}

// DBX-50: templates: built-in ones plus the user's own, made from a
// connection without its id or a literal password.
func TestConnTemplates(t *testing.T) {
	if len(BuiltinTemplates) < 4 {
		t.Fatalf("built-in templates %d", len(BuiltinTemplates))
	}
	c := manageConns()[0]
	c.Password = "literal"
	tpl := TemplateOf(c, "Shop over SSH")
	if tpl.ID != "shop-over-ssh" || tpl.Password != "" || tpl.Name != "Shop over SSH" || tpl.SSH == nil || tpl.SSH == c.SSH {
		t.Errorf("template %+v", tpl)
	}
	data, err := TemplatesJSON([]Conn{tpl})
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseTemplates(data)
	if err != nil || len(back) != 1 || back[0].Host != "db" {
		t.Fatalf("round trip %+v %v", back, err)
	}
	n, err := FromTemplate(manageConns(), back[0], "pg")
	if err != nil || n.ID != "pg-copy" || n.Source != SourceUser || n.Host != "db" {
		t.Errorf("from template %+v %v", n, err)
	}
	for _, b := range BuiltinTemplates {
		if _, err := FromTemplate(nil, b, b.ID); err != nil {
			t.Errorf("built-in %s: %v", b.ID, err)
		}
	}
}

// DBX-50: named SSH profiles: saved by name (a new one replaces the old
// one of that name) and put on a connection as a copy.
func TestSSHProfiles(t *testing.T) {
	p := SSHProfile{Name: "bastion", SSH: SSHConfig{Host: "b.example.com", User: "me", Jump: []string{"j"}}}
	ps := SaveSSHProfile(nil, p)
	ps = SaveSSHProfile(ps, SSHProfile{Name: "bastion", SSH: SSHConfig{Host: "b2"}})
	if len(ps) != 1 || ps[0].SSH.Host != "b2" {
		t.Fatalf("profiles %+v", ps)
	}
	ps = SaveSSHProfile(ps, p)
	data, err := SSHProfilesJSON(ps)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseSSHProfiles(data)
	if err != nil || len(back) != 1 || back[0].SSH.Host != "b.example.com" {
		t.Fatalf("round trip %+v %v", back, err)
	}
	cs := manageConns()
	out, err := UseSSHProfile(cs, "pg", back[0])
	if err != nil || out[0].SSH.Host != "b.example.com" || &out[0].SSH.Jump[0] == &back[0].SSH.Jump[0] {
		t.Fatalf("use %+v %v", out, err)
	}
	if _, err := UseSSHProfile(cs, "lite", back[0]); err == nil {
		t.Error("SSH on SQLite accepted")
	}
	if _, err := ParseSSHProfiles([]byte(`{"profiles":[{"name":"","ssh":{"host":"x"}}]}`)); err == nil {
		t.Error("a profile without a name accepted")
	}
}

// DBX-50: the statement that reads the server version.
func TestServerVersionQuery(t *testing.T) {
	for d, want := range map[string]string{"postgres": "SELECT version()", "mysql": "SELECT version()", "sqlite": "SELECT sqlite_version()", "cassandra": "SELECT release_version FROM system.local", "redis": ""} {
		if got := ServerVersionQuery(d); got != want {
			t.Errorf("%s = %q", d, got)
		}
	}
}

// Kube settings are copied, not shared, by Duplicate.
func TestDuplicateConnCopiesKube(t *testing.T) {
	cs := []Conn{{ID: "k", Driver: "postgres", Kube: &KubeConfig{Pod: "pg-0", Port: 5432}, Source: SourceUser}}
	out, id, err := DuplicateConn(cs, "k")
	if err != nil || id != "k-copy" || out[1].Kube == cs[0].Kube {
		t.Fatalf("duplicate %+v %v", out, err)
	}
}
