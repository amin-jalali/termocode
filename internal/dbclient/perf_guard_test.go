package dbclient

import (
	"context"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// Perf guards of the Database panel (PERF-6) on the fixture of
// perf_bench_test.go (real sqlite3, 500 tables, 1M rows). They run only
// with TERMOCODE_PERF=1; each takes the median of 7 runs.

func perfMedianMS(t *testing.T, runs int, f func()) float64 {
	t.Helper()
	var took []time.Duration
	for i := 0; i < runs; i++ {
		t0 := time.Now()
		f()
		took = append(took, time.Since(t0))
	}
	slices.Sort(took)
	return float64(took[len(took)/2].Microseconds()/100) / 10
}

// Connect to the first tree: open a session and list the 500 tables.
func TestPerfDBConnectToTree(t *testing.T) {
	perfbudget.Require(t)
	path := perfDB(t)
	got := perfMedianMS(t, 7, func() {
		s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: path})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if r, err := s.Introspect(context.Background(), IntrospectionRequest{}); err != nil || len(r.Objects) != perfTables+1 {
			t.Fatalf("introspect: %v, %d objects", err, len(r.Objects))
		}
	})
	perfbudget.Check(t, "db_connect_to_tree", got)
}

// A console query of the 1M-row table to its first 10 000 rows.
func TestPerfDBQueryFirstRows(t *testing.T) {
	perfbudget.Require(t)
	s := perfSession(t)
	got := perfMedianMS(t, 7, func() {
		r := s.Run(context.Background(), "SELECT * FROM big", RunOptions{MaxRows: 10000})
		if r.Err != nil || len(r.Rows) != 10000 {
			t.Fatalf("run: %v, %d rows", r.Err, len(r.Rows))
		}
	})
	perfbudget.Check(t, "db_query_first_rows", got)
}

func perfExport(t *testing.T, f ExportFormat, name string) {
	perfbudget.Require(t)
	s := perfSession(t)
	got := perfMedianMS(t, 7, func() {
		w, err := NewRowWriter(f, io.Discard, ExportOptions{Driver: "sqlite"})
		if err != nil {
			t.Fatal(err)
		}
		if err := StreamRows(context.Background(), s, "sqlite", "SELECT * FROM big WHERE id <= 100000", w); err != nil {
			t.Fatal(err)
		}
		if err := w.End(); err != nil {
			t.Fatal(err)
		}
	})
	perfbudget.Check(t, name, got)
}

// Export 100 000 rows.
func TestPerfDBExportCSV(t *testing.T)  { perfExport(t, FormatCSV, "db_export_100k_csv") }
func TestPerfDBExportJSON(t *testing.T) { perfExport(t, FormatJSON, "db_export_100k_json") }
