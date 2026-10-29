package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// perfTree writes a workspace of 20 000 small source files (200 folders
// of 100), 1 in 100 of them with the word "needle" on one line.
func perfTree(tb testing.TB) string {
	tb.Helper()
	dir := tb.TempDir()
	var body strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&body, "func handler%02d(w http.ResponseWriter, r *http.Request) { log.Printf(\"served %%s\", r.URL) }\n", i)
	}
	plain := body.String()
	for d := 0; d < 200; d++ {
		sub := filepath.Join(dir, fmt.Sprintf("pkg%03d", d))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			tb.Fatal(err)
		}
		for f := 0; f < 100; f++ {
			text := plain
			if (d*100+f)%100 == 37 {
				text = plain[:len(plain)/2] + "// the needle is here\n" + plain[len(plain)/2:]
			}
			if err := os.WriteFile(filepath.Join(sub, fmt.Sprintf("file%03d.go", f)), []byte(text), 0o644); err != nil {
				tb.Fatal(err)
			}
		}
	}
	return dir
}

// timeStream runs one streamed search and returns the time to the first
// batch with results and to the Done batch, and the result count.
func timeStream(tb testing.TB, dir, query string) (first, done time.Duration, n int) {
	tb.Helper()
	t0 := time.Now()
	for b := range Stream(context.Background(), []string{dir}, query, Options{MaxResults: DefaultMaxResults}) {
		if len(b.Results) > 0 && first == 0 {
			first = time.Since(t0)
		}
		n += len(b.Results)
		if b.Done {
			if b.Err != nil {
				tb.Fatal(b.Err)
			}
			done = time.Since(t0)
		}
	}
	return first, done, n
}

// Find in files on a 20 000-file workspace with the built-in search (rg
// not on PATH, so the number does not depend on the machine having rg):
// the median of three runs (after one warm-up) of the time to the first
// results and to the end.
func TestPerfSearch20kFiles(t *testing.T) {
	perfbudget.Require(t)
	dir := perfTree(t)
	t.Setenv("PATH", t.TempDir()) // no rg: the built-in walker
	if Available() {
		t.Fatal("rg still found")
	}
	timeStream(t, dir, "needle") // warm the page cache
	var firsts, dones []time.Duration
	for i := 0; i < 3; i++ {
		first, done, n := timeStream(t, dir, "needle")
		if n != 200 {
			t.Fatalf("%d results, want 200", n)
		}
		firsts, dones = append(firsts, first), append(dones, done)
	}
	slices.Sort(firsts)
	slices.Sort(dones)
	t.Logf("first results %v, done %v", firsts, dones)
	perfbudget.Check(t, "search_20k_first_result", float64(firsts[1].Microseconds()/100)/10)
	perfbudget.Check(t, "search_20k_done", float64(dones[1].Microseconds()/100)/10)
}

// BenchmarkRunFallback20k is the built-in search over perfTree.
func BenchmarkRunFallback20k(b *testing.B) {
	dir := perfTree(b)
	b.ReportAllocs()
	for b.Loop() {
		n := 0
		if err := runFallback(context.Background(), dir, "needle", Options{}, func(rs []Result) { n += len(rs) }); err != nil || n != 200 {
			b.Fatalf("%d results, %v", n, err)
		}
	}
}
