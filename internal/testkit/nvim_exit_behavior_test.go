package testkit

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// killNvim kills the embedded nvim with SIGKILL, as a crash or the OOM
// killer would.
func killNvim(d *Driver) {
	d.T.Helper()
	pid, err := strconv.Atoi(lua(d, `return tostring(vim.fn.getpid())`))
	if err != nil {
		d.T.Fatalf("nvim pid: %v", err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		d.T.Fatalf("kill nvim: %v", err)
	}
}

// hotExitBackups counts the hot-exit backup files of the sandbox.
func hotExitBackups() int {
	n := 0
	root := filepath.Join(os.Getenv("XDG_STATE_HOME"), "termocode", "backup")
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".bak") {
			n++
		}
		return nil
	})
	return n
}

// Spec: Given a.txt edited (not saved) and b.txt open, when the nvim
// process dies, then a dialog says the editor stopped. Restart starts a
// new nvim, reopens both tabs and offers the unsaved text back; Restore
// brings it back.
func TestNvimExitRestart(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "b.txt")
	open(d, "a.txt")
	d.Type("Hi ")
	waitTabs(d, "b.txt", "a.txt*")
	if !d.WaitFor(func() bool { return hotExitBackups() > 0 }, 5*time.Second) {
		t.Fatalf("no hot-exit backup of a.txt")
	}

	killNvim(d)
	if !d.WaitForText("Editor process stopped", 5*time.Second) {
		t.Fatalf("no dialog after nvim died:\n%s", screen(d))
	}
	d.Key("enter") // Restart (the first button)
	if !d.WaitForText("Restore unsaved changes?", 10*time.Second) {
		t.Fatalf("restart did not offer the unsaved text:\n%s", screen(d))
	}
	d.Key("enter") // Restore
	if !waitTabs(d, "b.txt", "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("tabs after restart %v, want [b.txt a.txt*]\n%s", got, screen(d))
	}
	open(d, "a.txt")
	if got := bufLines(d); got != "Hi alpha\nbravo\ncharlie" {
		t.Errorf("a.txt after restart: %q", got)
	}
	// The new nvim works: typing reaches it.
	d.Type("X")
	if !d.WaitFor(func() bool { return strings.Count(bufLines(d), "X") == 1 }, 3*time.Second) {
		t.Errorf("typing after restart: %q", bufLines(d))
	}
}

// Spec: Given nvim died, when the user picks Quit, then the app quits and
// the unsaved text stays in the backup for the next start.
func TestNvimExitQuit(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Type("Hi ")
	waitTabs(d, "a.txt*")
	if !d.WaitFor(func() bool { return hotExitBackups() > 0 }, 5*time.Second) {
		t.Fatalf("no hot-exit backup of a.txt")
	}
	killNvim(d)
	if !d.WaitForText("Editor process stopped", 5*time.Second) {
		t.Fatalf("no dialog after nvim died:\n%s", screen(d))
	}
	d.Key("esc") // the editor is gone: the question stays
	if !strings.Contains(screen(d), "Editor process stopped") {
		t.Fatalf("Esc closed the dialog with a dead editor:\n%s", screen(d))
	}
	d.Key("right", "enter") // Quit
	if !d.Quit() {
		t.Fatalf("Quit did not quit:\n%s", screen(d))
	}
	if hotExitBackups() == 0 {
		t.Errorf("Quit dropped the backup of the unsaved text")
	}
}

// Spec: a normal quit closes nvim without the "Editor process stopped"
// dialog.
func TestNormalQuitNoNvimExitDialog(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Key("ctrl+q")
	if !d.Quit() {
		t.Fatalf("ctrl+q did not quit:\n%s", screen(d))
	}
	d.Settle(500 * time.Millisecond)
	if strings.Contains(screen(d), "Editor process stopped") {
		t.Errorf("normal quit showed the nvim-exit dialog:\n%s", screen(d))
	}
}
