package dbclient

import (
	"path/filepath"
	"testing"
)

// DBX-6: history filtered per connection and searched by text (any case,
// every word must match), newest first.
func TestHistoryFind(t *testing.T) {
	h := &History{Path: filepath.Join(t.TempDir(), "h.jsonl")}
	for _, e := range []HistoryEntry{
		{ID: "1", Conn: "pg", SQL: "SELECT * FROM users"},
		{ID: "2", Conn: "my", SQL: "SELECT * FROM users WHERE id = 1"},
		{ID: "3", Conn: "pg", SQL: "update orders set paid = true"},
		{ID: "4", Conn: "pg", SQL: "select id from Users where active"},
	} {
		if err := h.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(a []HistoryEntry) (s string) {
		for _, e := range a {
			s += e.ID
		}
		return
	}
	cases := []struct{ conn, q, want string }{
		{"", "", "4321"},
		{"pg", "", "431"},
		{"pg", "users", "41"},
		{"", "USERS where", "42"},
		{"my", "orders", ""},
		{"pg", "  paid   orders ", "3"},
	}
	for _, c := range cases {
		got, err := h.Find(c.conn, c.q)
		if err != nil {
			t.Fatal(err)
		}
		if ids(got) != c.want {
			t.Errorf("Find(%q,%q) = %q, want %q", c.conn, c.q, ids(got), c.want)
		}
	}
}
