package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveFailTextIsPlain(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	pre := "nvim:nvim_command exception: Vim(write):"
	cases := []struct{ err, want string }{
		{pre + "E45: 'readonly' option is set (add ! to override)", "a.txt is read-only."},
		{pre + `E505: "a.txt" is read-only (add ! to override)`, "a.txt is read-only."},
		{pre + "E514: Write error (file system full?)", "The disk is full: a.txt was not saved."},
		{pre + "E212: Can't open file for writing: permission denied", "No permission to write a.txt."},
		{pre + "E13: File exists (add ! to override)", "E13: File exists (add ! to override)."},
	}
	for _, c := range cases {
		got := saveFailText(p, errors.New(c.err))
		if !strings.HasPrefix(got, c.want) || !strings.HasSuffix(got, "Your changes are still in the editor.") {
			t.Errorf("%q → %q, want %q…", c.err, got, c.want)
		}
		if strings.Contains(got, "nvim") || strings.Contains(got, "Vim(") {
			t.Errorf("%q → %q still shows nvim internals", c.err, got)
		}
	}
}

func TestSaveFailTextNamesReadOnlyFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("runs as root: file modes do not stop root from writing")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	got := saveFailText(filepath.Join(dir, "new.txt"), errors.New("Vim(write):E212: Can't open file for writing: permission denied"))
	if !strings.HasPrefix(got, "No permission to write in folder locked.") {
		t.Errorf("got %q", got)
	}
}

// A remote buffer's save error already is our own sentence: it is kept
// as is, without nvim's trace.
func TestSaveFailTextKeepsOurRemoteSentence(t *testing.T) {
	raw := "nvim:nvim_command exception: BufWriteCmd Autocommands for \"/app/*\": Vim(append):Lua callback: termocode: cannot save main.go: api-1 is not running. Your text is kept.\nstack traceback:\n\t[C]: in function 'error'"
	if got := saveFailText("/app/main.go", errors.New(raw)); got != "cannot save main.go: api-1 is not running. Your text is kept." {
		t.Fatalf("saveFailText = %q", got)
	}
}
