package nvim

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// :w of a buffer whose file another program saved meanwhile asks "The
// file has been changed since reading it!!! Do you really want to write
// to it (y/n)?". Inside an RPC call nvim reports that prompt as mode "r?"
// with blocking=false, so neither the call guard nor the prompt guard saw
// it: the call never returned and the app froze for good. The guard must
// answer it (no), and the file on disk must stay as the other program
// left it.
func TestWriteOfFileChangedOnDiskDoesNotHang(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = c.Command("set cmdheight=0")
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Command("edit " + p); err != nil {
		t.Fatal(err)
	}
	_ = c.Input("<Esc>otwo<Esc>")
	// Another program saves the file: a newer mtime than nvim read.
	if err := os.WriteFile(p, []byte("one\nother\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Command("silent w") }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal(":w hung on the changed-on-disk question")
	}
	if b, _ := os.ReadFile(p); string(b) != "one\nother\n" {
		t.Errorf("disk = %q: the other program's text was overwritten", b)
	}
	if s, err := c.EvalLuaString(`return 'ok'`); err != nil || s != "ok" {
		t.Errorf("nvim did not recover: %q %v", s, err)
	}
}
