package dbclient

import (
	"os"
	"path/filepath"
	"testing"
)

// DBX-6: saved queries keep a name, a connection and the SQL in a 0600
// file; the same name on the same connection replaces the old one.
func TestSavedQueriesSaveListDelete(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db", "saved.json")
	s := &SavedQueries{Path: p}
	a, err := s.Save(SavedQuery{Name: "Active users", Conn: "local", SQL: "SELECT * FROM users WHERE active"})
	if err != nil || a.ID == "" {
		t.Fatalf("save: %+v %v", a, err)
	}
	if _, err := s.Save(SavedQuery{Name: "Orders", Conn: "prod", SQL: "SELECT * FROM orders"}); err != nil {
		t.Fatal(err)
	}
	b, err := s.Save(SavedQuery{Name: "active users", Conn: "local", SQL: "SELECT id FROM users"})
	if err != nil || b.ID != a.ID {
		t.Fatalf("same name did not replace: %+v %v", b, err)
	}
	all, _ := s.List("")
	if len(all) != 2 || all[0].Name != "active users" || all[0].SQL != "SELECT id FROM users" {
		t.Fatalf("list: %+v", all)
	}
	local, _ := s.List("local")
	if len(local) != 1 || local[0].Conn != "local" {
		t.Fatalf("per connection: %+v", local)
	}
	if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", info, err)
	}
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.List(""); len(all) != 1 || all[0].Name != "Orders" {
		t.Fatalf("after delete: %+v", all)
	}
	if _, err := s.Save(SavedQuery{Name: " ", Conn: "x", SQL: "SELECT 1"}); err == nil {
		t.Fatal("empty name saved")
	}
	if _, err := s.Save(SavedQuery{Name: "x", Conn: "x", SQL: "  "}); err == nil {
		t.Fatal("empty SQL saved")
	}
}

func TestSavedQueriesRedactSecrets(t *testing.T) {
	s := &SavedQueries{Path: filepath.Join(t.TempDir(), "saved.json")}
	q, err := s.Save(SavedQuery{Name: "pw", Conn: "c", SQL: "ALTER USER bob PASSWORD 'hunter2'"})
	if err != nil {
		t.Fatal(err)
	}
	if q.SQL == "ALTER USER bob PASSWORD 'hunter2'" {
		t.Fatalf("secret kept: %s", q.SQL)
	}
}

func TestTemplateList(t *testing.T) {
	list := TemplateList()
	if len(list) != len(Templates()) {
		t.Fatalf("list: %+v", list)
	}
	for _, tp := range list {
		if tp.Abbr == "sel" && (tp.Text != "SELECT * FROM ;" || tp.Cursor != 14) {
			t.Fatalf("sel: %+v", tp)
		}
	}
}
