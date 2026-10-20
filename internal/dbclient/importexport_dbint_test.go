//go:build dbint

package dbclient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Import and export against a real PostgreSQL: text cells bind into integer,
// boolean, timestamp and jsonb columns; a bad row rolls the file back and is
// named; Export all streams a 25 000-row table through the cursor.
func TestDBIntPostgresImportAndStreamExport(t *testing.T) {
	dsn := os.Getenv("DBINT_POSTGRES_DSN")
	if dsn == "" && dbintEnabled() {
		dsn = "postgres://termocode:termocode@localhost:54329/termocode"
	}
	if dsn == "" {
		t.Skip("DBINT_POSTGRES_DSN is not set")
	}
	conn, err := ParseURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := NewPostgresSession(ctx, conn, conn.Password)
	if err != nil {
		unavailableDBInt(t, err)
	}
	defer s.Close()
	tbl := dbintObject()
	for _, q := range []string{
		fmt.Sprintf(`CREATE TABLE "%s" (id bigint PRIMARY KEY, n int, ok boolean, at timestamptz, doc jsonb, note text)`, tbl),
	} {
		if r := s.Run(ctx, q, RunOptions{}); r.Err != nil {
			t.Fatalf("%s: %v", q, r.Err)
		}
	}
	defer s.Run(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, tbl), RunOptions{})

	var sb strings.Builder
	sb.WriteString("id,n,ok,at,doc,note\n")
	const rows = 25000
	for i := 1; i <= rows; i++ {
		note := fmt.Sprintf(`"it's ""q"" \ %d"`, i)
		if i%7 == 0 {
			note = ""
		}
		fmt.Fprintf(&sb, "%d,%d,%t,2026-10-19T10:00:00Z,\"{\"\"a\"\": %d}\",%s\n", i, i*2, i%2 == 0, i, note)
	}
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "in.csv")
	if err := os.WriteFile(csvPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	sn, err := SniffCSV(csvPath)
	if err != nil || sn.Delimiter != ',' || !sn.Header {
		t.Fatalf("%+v %v", sn, err)
	}
	spec := ImportSpec{Path: csvPath, Sniff: sn, Dialect: PostgresDialect{}, Table: tbl, Targets: sn.Columns, Sources: []int{0, 1, 2, 3, 4, 5}}
	var last int64
	res := ImportCSV(ctx, s, spec, func(n int64) { last = n })
	if res.Err != nil || res.Rows != rows || last != rows {
		t.Fatalf("%+v last=%d", res, last)
	}
	r := s.Run(ctx, fmt.Sprintf(`SELECT count(*), count(note), min(n), bool_or(ok), max(doc->>'a') FROM "%s"`, tbl), RunOptions{})
	if r.Err != nil || r.Rows[0][0].Text != "25000" || r.Rows[0][1].Text != fmt.Sprint(rows-rows/7) {
		t.Fatalf("%+v", r)
	}

	// A bad value in the last batch: the whole file is rolled back, the row is named.
	bad := filepath.Join(dir, "bad.csv")
	body := "id,n\n90001,1\n90002,2\n90003,notanumber\n"
	if err := os.WriteFile(bad, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sn, _ = SniffCSV(bad)
	spec = ImportSpec{Path: bad, Sniff: sn, Dialect: PostgresDialect{}, Table: tbl, Targets: []string{"id", "n"}, Sources: []int{0, 1}}
	res = ImportCSV(ctx, s, spec, nil)
	if res.Err == nil || res.FailedRow != 3 || res.FailedLine != 4 || res.Rows != 0 {
		t.Fatalf("%+v", res)
	}
	r = s.Run(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s" WHERE id > 90000`, tbl), RunOptions{})
	if r.Err != nil || r.Rows[0][0].Text != "0" {
		t.Fatalf("rows left after the failed import: %+v", r)
	}
	if r := s.Run(ctx, `SELECT 1`, RunOptions{}); r.Err != nil {
		t.Fatalf("the session is unusable after the failed import: %v", r.Err)
	}

	// Export all through the cursor of a read-only session.
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	out := filepath.Join(dir, "out.csv")
	n, err := ExportToFile(ctx, out, FormatCSV, ExportOptions{}, false, func(ctx context.Context, sink RowSink) error {
		return StreamRows(ctx, ro, "postgres", fmt.Sprintf(`SELECT id, n, ok, note FROM "%s" ORDER BY id`, tbl), sink)
	}, nil)
	if err != nil || n != rows {
		t.Fatalf("n=%d err=%v", n, err)
	}
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), "\r\n1,2,false,\"it's \"\"q\"\" \\ 1\"\r\n") {
		t.Fatalf("export content: %.300s", b)
	}
	// SQL INSERT export of the same rows reloads into a copy of the table.
	sqlOut := filepath.Join(dir, "out.sql")
	n, err = ExportToFile(ctx, sqlOut, FormatSQL, ExportOptions{Driver: "postgres", Table: tbl}, false, func(ctx context.Context, sink RowSink) error {
		return StreamRows(ctx, ro, "postgres", fmt.Sprintf(`SELECT id, n, ok, at, note FROM "%s" WHERE id <= 50 ORDER BY id`, tbl), sink)
	}, nil)
	if err != nil || n != 50 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if r := s.Run(ctx, fmt.Sprintf(`DELETE FROM "%s" WHERE id <= 50`, tbl), RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	script, _ := os.ReadFile(sqlOut)
	if err := s.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(script)), "\n") {
		if r := s.Run(ctx, line, RunOptions{}); r.Err != nil {
			t.Fatalf("%s: %v", line, r.Err)
		}
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r = s.Run(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s" WHERE id <= 50 AND note LIKE 'it''s%%'`, tbl), RunOptions{})
	if r.Err != nil || r.Rows[0][0].Text == "0" {
		t.Fatalf("reloaded INSERT export: %+v", r)
	}
}
