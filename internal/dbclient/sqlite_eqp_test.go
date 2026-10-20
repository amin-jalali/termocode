package dbclient

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sqlite3 prints EXPLAIN QUERY PLAN as a text tree even in -json mode;
// the session turns it into rows of one "plan" column (the console's
// EXPLAIN and the AI's db_explain read it).
func TestSQLiteExplainQueryPlanRows(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	path := filepath.Join(t.TempDir(), "eqp.db")
	if out, err := exec.Command("sqlite3", path, "CREATE TABLE u(id INTEGER PRIMARY KEY, e TEXT); CREATE INDEX u_e ON u(e);").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	for _, ro := range []bool{false, true} {
		s, err := NewSQLiteSession(context.Background(), Conn{ID: "x", Driver: "sqlite", Path: path, ReadOnly: ro})
		if err != nil {
			t.Fatal(err)
		}
		r := s.Run(context.Background(), (SQLiteDialect{}).Explain("SELECT * FROM u WHERE e = 'a'", false), RunOptions{})
		_ = s.Close()
		if r.Err != nil {
			t.Fatalf("read-only=%v: %v", ro, r.Err)
		}
		if len(r.Columns) != 1 || r.Columns[0].Name != "plan" || len(r.Rows) == 0 || !strings.Contains(r.Rows[len(r.Rows)-1][0].Text, "u_e") {
			t.Fatalf("read-only=%v: columns %v rows %v", ro, r.Columns, r.Rows)
		}
	}
}
