package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/perfbudget"
	"github.com/amin-jalali/termocode/internal/picker"
)

// manyFilesTree writes 50 000 empty files: 500 folders of 100.
func manyFilesTree(tb testing.TB) string {
	tb.Helper()
	dir := tb.TempDir()
	for d := 0; d < 500; d++ {
		sub := filepath.Join(dir, fmt.Sprintf("src/mod%03d", d))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			tb.Fatal(err)
		}
		for f := 0; f < 100; f++ {
			if err := os.WriteFile(filepath.Join(sub, fmt.Sprintf("handler_%03d.go", f)), nil, 0o644); err != nil {
				tb.Fatal(err)
			}
		}
	}
	return dir
}

// Quick open (Go to file) on a 50 000-file workspace: the time to list
// the files (median of three after a warm-up), and the time the list
// takes to filter after each typed key (median over the keys of
// "mod42handler7").
func TestPerfQuickOpen50k(t *testing.T) {
	perfbudget.Require(t)
	dir := manyFilesTree(t)
	loadFilesIn([]string{dir})
	var lists []time.Duration
	var items []picker.Item
	for i := 0; i < 3; i++ {
		t0 := time.Now()
		items = loadFilesIn([]string{dir})
		lists = append(lists, time.Since(t0))
	}
	if len(items) != 50000 {
		t.Fatalf("%d files, want 50000", len(items))
	}
	slices.Sort(lists)
	p := picker.NewItems(" Go to file ", items)
	var keys []time.Duration
	for _, r := range "mod42handler7" {
		t0 := time.Now()
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		keys = append(keys, time.Since(t0))
	}
	if _, ok := p.Current(); !ok {
		t.Fatal("no match after typing")
	}
	slices.Sort(keys)
	t.Logf("list %v; per key %v", lists, keys)
	perfbudget.Check(t, "quick_open_50k_list", float64(lists[1].Microseconds()/100)/10)
	perfbudget.Check(t, "quick_open_50k_key", float64(keys[len(keys)/2].Microseconds()/100)/10)
}
