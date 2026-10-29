package testkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
)

// Spec (SCN-12): Given a folder with 10 000 files,
// when the user opens it in the explorer, scrolls to its end (End) and
// with the wheel, and finds one file with Quick Open (Ctrl+P),
// then each step stays within its bound, no key blocks the UI loop for
// long and the Go heap stays bounded.
//
// Local (race off): open 30–50 ms, worst key or wheel 5–11 ms, filter
// 0.14 s, heap +3 MB. Quick: it also runs with -short.
func TestScaleExplorer10kFolder(t *testing.T) {
	d := New(t, Options{W: 120, H: 34, Files: map[string]string{"a.txt": "a\n"},
		Setup: func(t testing.TB, dir string) {
			big := filepath.Join(dir, "big")
			if err := os.MkdirAll(big, 0o755); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 10_000; i++ {
				if err := os.WriteFile(filepath.Join(big, fmt.Sprintf("file%05d.txt", i)), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}})
	d.Action("view:files")
	base := heapMB()

	t0 := time.Now()
	clickExplorer(d, "big")
	if !d.WaitForText("file00000.txt", 3*time.Second*raceFactor()) {
		t.Fatalf("folder did not open:\n%s", screen(d))
	}
	within(t, "open the folder", time.Since(t0), time.Second)

	key := keyTime(d, "end")
	if !d.WaitForText("file09999.txt", time.Second*raceFactor()) {
		t.Fatalf("End did not reach the last file:\n%s", screen(d))
	}
	key = max(key, worstKey(d, "home", "pgdown", "pgdown", "down", "up"))
	for i := 0; i < 20; i++ {
		key = max(key, d.Timed(wheel(activity.Width+5, d.H/2, false)))
	}
	d.Settle(100 * time.Millisecond)
	within(t, "worst key or wheel", key, 100*time.Millisecond)

	d.Key("ctrl+p")
	t0 = time.Now()
	d.Type("file09999")
	if !d.WaitFor(func() bool {
		d.render()
		s := screen(d)
		return strings.Contains(s, "file09999.txt") && !strings.Contains(s, "file09998.txt")
	}, 5*time.Second*raceFactor()) {
		t.Fatalf("Quick Open did not narrow to file09999.txt:\n%s", screen(d))
	}
	within(t, "filter to one file", time.Since(t0), 2*time.Second)
	d.Key("esc")
	heapWithin(t, "10k folder open", base, 40)
}
