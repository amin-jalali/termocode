package specui

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// Spec: Given nvim just became ready,
// when files are opened one right after the other (faster than the
// editor-state throttle),
// then the editor shows the last file — not the Welcome page or the
// previous file — once nvim is quiet.
func TestFastOpenShowsLastFile(t *testing.T) {
	slow(t)
	files := map[string]string{"a.txt": "alpha body\n", "b.txt": "bravo body\n", "c.txt": "charlie body\n"}
	for i := range 3 {
		d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Files: files})
		d.Action("open:a.txt")
		d.Action("open:b.txt")
		d.Action("open:c.txt")
		if !d.WaitForText("charlie body", 3*time.Second) {
			t.Fatalf("run %d: c.txt not shown after a fast open:\n%s", i, screen(d))
		}
	}
}
