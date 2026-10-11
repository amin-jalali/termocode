package testkit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Spec: Given a.txt and b.txt open, b.txt active and unchanged,
// when b.txt is deleted outside the app (rm, git checkout) and the user
// switches to the a.txt tab,
// then b.txt is not written back to disk.
func TestExternalDeleteNotRecreatedOnTabSwitch(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	open(d, "b.txt")
	if err := os.Remove(filepath.Join(d.Dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	d.Key("ctrl+pgup")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "a.txt" }, 3*time.Second) {
		got, a := tabs(d)
		t.Fatalf("Ctrl+PgUp: active %q (tabs %v), want a.txt", a, got)
	}
	if exists(d, "b.txt") {
		t.Errorf("leaving the b.txt tab wrote the deleted file back")
	}
}
