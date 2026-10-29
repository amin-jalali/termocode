package dbclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Database panel perf (PERF-6): a schema of 500 tables × 20 columns and a
// table of 1 000 000 rows in a real sqlite3 file. The benchmarks and the
// perf guards (perf_guard_test.go) share these fixtures.

const (
	perfTables  = 500
	perfColumns = 20
	perfBigRows = 1_000_000
)

var (
	perfDBOnce sync.Once
	perfDBDir  string
	perfDBPath string
	perfDBErr  error
)

// perfSQLite is the path of the sqlite3 CLI, or a skip.
func perfSQLite(tb testing.TB) string {
	tb.Helper()
	bin, err := lookupSQLite()
	if err != nil {
		tb.Skip("sqlite3 is not installed")
	}
	return bin
}

// perfDB builds the fixture database once per test binary: big (1M rows,
// id, name, amount, note) and t000…t499 with 20 columns each.
func perfDB(tb testing.TB) string {
	tb.Helper()
	bin := perfSQLite(tb)
	perfDBOnce.Do(func() {
		perfDBDir, perfDBErr = os.MkdirTemp("", "tc-perfdb-")
		if perfDBErr != nil {
			return
		}
		perfDBPath = filepath.Join(perfDBDir, "perf.db")
		var b strings.Builder
		b.WriteString("PRAGMA journal_mode=OFF;\nPRAGMA synchronous=OFF;\nBEGIN;\n")
		for i := 0; i < perfTables; i++ {
			fmt.Fprintf(&b, "CREATE TABLE t%03d (id INTEGER PRIMARY KEY", i)
			for c := 1; c < perfColumns; c++ {
				fmt.Fprintf(&b, ", col_%02d TEXT", c)
			}
			b.WriteString(");\n")
		}
		b.WriteString("CREATE TABLE big (id INTEGER PRIMARY KEY, name TEXT, amount REAL, note TEXT);\n")
		fmt.Fprintf(&b, "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < %d) "+
			"INSERT INTO big SELECT i, 'name ' || i, i * 1.5, CASE WHEN i %% 7 = 0 THEN NULL ELSE 'note for row ' || i END FROM n;\n", perfBigRows)
		b.WriteString("COMMIT;\n")
		cmd := spawn.Command(bin, "-batch", perfDBPath)
		cmd.Stdin = strings.NewReader(b.String())
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			perfDBErr = fmt.Errorf("build perf db: %v: %s", err, out.String())
		}
	})
	if perfDBErr != nil {
		tb.Fatal(perfDBErr)
	}
	return perfDBPath
}

func TestMain(m *testing.M) {
	code := m.Run()
	if perfDBDir != "" {
		_ = os.RemoveAll(perfDBDir)
	}
	os.Exit(code)
}

func perfSession(tb testing.TB) *SQLiteSession {
	tb.Helper()
	path := perfDB(tb)
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: path})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = s.Close() })
	return s
}

// Connect to the first tree: open a session and read the object list.
func BenchmarkPerfDBConnectToTree(b *testing.B) {
	path := perfDB(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: path})
		if err != nil {
			b.Fatal(err)
		}
		r, err := s.Introspect(context.Background(), IntrospectionRequest{})
		if err != nil || len(r.Objects) != perfTables+1 {
			b.Fatalf("introspect: %v, %d objects", err, len(r.Objects))
		}
		_ = s.Close()
	}
}

// Expand one table of 20 columns.
func BenchmarkPerfDBExpandTable(b *testing.B) {
	s := perfSession(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := s.Introspect(context.Background(), IntrospectionRequest{Object: fmt.Sprintf("t%03d", i%perfTables)})
		if err != nil || len(r.Columns) != perfColumns {
			b.Fatalf("introspect: %v, %d columns", err, len(r.Columns))
		}
	}
}

// A console query of the 1M-row table to its first rows (the console's
// default limit of 10 000 rows).
func BenchmarkPerfDBQueryFirstRows(b *testing.B) {
	s := perfSession(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := s.Run(context.Background(), "SELECT * FROM big", RunOptions{MaxRows: 10000})
		if r.Err != nil || len(r.Rows) != 10000 || !r.Truncated {
			b.Fatalf("run: %v, %d rows, truncated %v", r.Err, len(r.Rows), r.Truncated)
		}
	}
}

// One page of the data view (100 rows) deep in the table.
func BenchmarkPerfDBTablePage(b *testing.B) {
	s := perfSession(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := s.Run(context.Background(), fmt.Sprintf("SELECT * FROM big LIMIT 101 OFFSET %d", (i%1000)*100), RunOptions{MaxRows: 101})
		if r.Err != nil || len(r.Rows) != 101 {
			b.Fatalf("page: %v, %d rows", r.Err, len(r.Rows))
		}
	}
}

func benchExport(b *testing.B, f ExportFormat) {
	s := perfSession(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w, err := NewRowWriter(f, io.Discard, ExportOptions{Driver: "sqlite"})
		if err != nil {
			b.Fatal(err)
		}
		if err := StreamRows(context.Background(), s, "sqlite", "SELECT * FROM big WHERE id <= 100000", w); err != nil {
			b.Fatal(err)
		}
		if err := w.End(); err != nil {
			b.Fatal(err)
		}
	}
}

// Export 100 000 rows.
func BenchmarkPerfDBExportCSV(b *testing.B)  { benchExport(b, FormatCSV) }
func BenchmarkPerfDBExportJSON(b *testing.B) { benchExport(b, FormatJSON) }

// Query history load: a full history of 500 entries.
func BenchmarkPerfDBHistoryList(b *testing.B) {
	h := &History{Path: filepath.Join(b.TempDir(), "history.jsonl")}
	for i := 0; i < 500; i++ {
		if err := h.Append(HistoryEntry{ID: fmt.Sprint(i), Conn: "c", SQL: fmt.Sprintf("SELECT * FROM t%03d WHERE id = %d", i%perfTables, i), Rows: i}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a, err := h.List()
		if err != nil || len(a) != 500 {
			b.Fatalf("list: %v, %d", err, len(a))
		}
	}
}
