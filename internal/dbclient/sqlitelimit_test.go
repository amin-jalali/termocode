package dbclient

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestSQLiteRowLimit(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"SELECT * FROM big", true},
		{"select id from big where name like '%limit%'", true},
		{"SELECT * FROM big ORDER BY id DESC", true},
		{"SELECT 1 UNION ALL SELECT 2", true},
		{"WITH x AS (SELECT * FROM big LIMIT 5) SELECT * FROM x", true},
		{"VALUES (1), (2)", false},
		{"WITH x AS (SELECT 1) VALUES (1)", false},
		{"SELECT 1 UNION ALL VALUES (2)", false},
		{"SELECT * FROM (VALUES (1), (2))", true},
		{"SELECT * FROM big -- all of it", true},
		{`SELECT "limit" FROM t`, true},
		{"SELECT [limit] FROM t", true},
		{"SELECT * FROM (SELECT * FROM big LIMIT 3)", true},
		{"SELECT * FROM big LIMIT 10", false},
		{"select * from big limit 10 offset 5", false},
		{"WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x", false},
		{"WITH x AS (SELECT 1) DELETE FROM t", false},
		{"INSERT INTO t VALUES (1)", false},
		{"UPDATE t SET a = 1", false},
		{"PRAGMA table_info(t)", false},
		{"EXPLAIN QUERY PLAN SELECT * FROM t", false},
		{"SELECT * FROM t WHERE id = ?", false},
		{"SELECT * FROM t WHERE id = $id", false},
		{"SELECT 'open", false},
		{"SELECT (1", false},
		{"SELECT 1)", false},
		{"", false},
	} {
		got, ok := sqliteRowLimit(tc.in, 11)
		if ok != tc.want {
			t.Errorf("%q: ok = %v, want %v (%q)", tc.in, ok, tc.want, got)
		}
		if ok && got != tc.in+"\nLIMIT 11" || !ok && got != tc.in {
			t.Errorf("%q: got %q", tc.in, got)
		}
	}
	if _, ok := sqliteRowLimit("SELECT 1", 0); ok {
		t.Error("no limit asked: the statement must stay as it is")
	}
}

// With and without the added LIMIT, a console read shows the same rows,
// columns and "truncated" mark.
func TestSQLiteRunMaxRowsSameRows(t *testing.T) {
	perfSQLite(t)
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	setup := []string{
		"CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, n REAL)",
		"WITH RECURSIVE c(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM c WHERE i < 50) INSERT INTO t SELECT i, 'name ' || i, i * 0.5 FROM c",
	}
	for _, q := range setup {
		if r := s.Run(context.Background(), q, RunOptions{}); r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	for _, q := range []string{
		"SELECT * FROM t",
		"SELECT * FROM t ORDER BY name DESC",
		"SELECT id, id, name AS id FROM t",
		"SELECT name FROM t UNION ALL SELECT name FROM t",
		"WITH x AS (SELECT * FROM t WHERE id > 3) SELECT * FROM x -- comment",
		"VALUES (1), (2), (3)",
		"SELECT * FROM t WHERE id < 5",
		"SELECT * FROM t LIMIT 20",
	} {
		for _, max := range []int{1, 4, 10, 1000} {
			got := s.Run(context.Background(), q, RunOptions{MaxRows: max})
			// The old way: the statement as typed, cut while reading.
			lines, _, err := s.exchangeLocked(context.Background(), q, max)
			if err != nil || got.Err != nil {
				t.Fatalf("%q: %v / %v", q, got.Err, err)
			}
			truncated := len(lines) > 0 && lines[len(lines)-1] == "__TC_TRUNCATED__"
			if truncated {
				lines = lines[:len(lines)-1]
			}
			rows, cols, err := parseSQLiteJSON(lines)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Rows, rows) || !reflect.DeepEqual(got.Columns, cols) || got.Truncated != truncated {
				t.Fatalf("%q max %d:\nrows %v cols %v truncated %v\nwant %v %v %v", q, max, got.Rows, got.Columns, got.Truncated, rows, cols, truncated)
			}
		}
	}
}

// A console read stops at its row limit: an endless query returns its
// first rows instead of running until the timeout.
func TestSQLiteRunMaxRowsStopsEarly(t *testing.T) {
	perfSQLite(t)
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := s.Run(ctx, "WITH RECURSIVE c(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM c) SELECT i FROM c", RunOptions{MaxRows: 10})
	if r.Err != nil || len(r.Rows) != 10 || !r.Truncated || r.Rows[9][0].Text != "10" {
		t.Fatalf("err %v, %d rows, truncated %v", r.Err, len(r.Rows), r.Truncated)
	}
}
