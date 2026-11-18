package checkpoint

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Spec (racy git): a change in the same timestamp tick as the user's
// last index write, with the same size, is still in the checkpoint. The
// checkpoint index is a copy of the user's; the copy must keep its mtime,
// or git takes the old stat as clean and keeps the old text.
func TestSaveSeesSameTickSameSizeEdit(t *testing.T) {
	dir := repo(t)
	run(t, dir, "config", "core.trustctime", "false") // the tick is what matters, not ctime
	a := filepath.Join(dir, "a.txt")
	// The index holds a clean (not smudged) stat of a.txt from that tick.
	tick := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(a, tick, tick); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "update-index", "--really-refresh")
	write(t, dir, "a.txt", "a2\n") // same size as "a1\n", same inode
	idx := filepath.Join(dir, ".git", "index")
	for _, p := range []string{a, idx} { // the edit and the index write share a tick
		if err := os.Chtimes(p, tick, tick); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Save(dir, "racy")
	if err != nil {
		t.Fatal(err)
	}
	if got := run(t, dir, "show", p.Commit+":a.txt"); got != "a2\n" {
		t.Errorf("the checkpoint has a.txt = %q, want the edit", got)
	}
}
