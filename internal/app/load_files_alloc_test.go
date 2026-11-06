package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/amin-jalali/termocode/internal/picker"
)

// Spec (FIX-42): opening Go to file again on the same workspace sizes
// the file list from the last one, so the list is not copied again and
// again while it grows (on a 50 000-file tree that copying was about a
// fifth of the listing time). The list itself (one Item per file) is
// allowed once; the growth copies are what this test keeps out.
func TestLoadFilesInReusesTheLastListSize(t *testing.T) {
	dir := t.TempDir()
	const n = 5000
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("d%02d", i%50), fmt.Sprintf("f%04d.go", i))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(loadFilesIn([]string{dir})); got != n {
		t.Fatalf("%d files, want %d", got, n)
	}
	allocated := func() uint64 {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.TotalAlloc
	}
	runtime.GC()
	before := allocated()
	items := loadFilesIn([]string{dir})
	used := allocated() - before
	if len(items) != n {
		t.Fatalf("%d files, want %d", len(items), n)
	}
	// The walk's own paths and entries take about two lists' worth; a
	// list grown by appends adds about three more (measured: 1.5 MB with
	// the size reused, 2.8 MB without, for a 0.48 MB list).
	list := uint64(n) * uint64(unsafe.Sizeof(picker.Item{}))
	if used > 4*list {
		t.Errorf("second listing allocated %d bytes, want at most %d (4x the %d-byte list): the list grew by copies", used, 4*list, list)
	}
}
