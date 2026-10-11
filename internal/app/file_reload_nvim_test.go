package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// A file that changes on disk while its buffer has unsaved edits must not
// open the blocking W12 prompt (it froze the app on Git: Stash Changes).
// Unmodified buffers reload; modified ones keep their text.
func TestFileChangedOnDiskDoesNotBlockNvim(t *testing.T) {
	c, err := nvim.New()
	if err != nil {
		t.Skipf("nvim not available: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	for _, cmd := range []string{"set cmdheight=0", "set autoread"} {
		_ = c.Command(cmd)
	}
	if err := c.ExecLua(notifyLua); err != nil {
		t.Fatal(err)
	}
	if err := c.ExecLua(fileReloadLua); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	clean, dirty := filepath.Join(dir, "clean.txt"), filepath.Join(dir, "dirty.txt")
	_ = os.WriteFile(clean, []byte("old\n"), 0o644)
	_ = os.WriteFile(dirty, []byte("old\n"), 0o644)
	_ = c.Command("edit " + clean)
	_ = c.Command("split " + dirty)
	_ = c.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'mine'})`)
	time.Sleep(1100 * time.Millisecond) // a new mtime second
	_ = os.WriteFile(clean, []byte("new\n"), 0o644)
	_ = os.WriteFile(dirty, []byte("new\n"), 0o644)

	done := make(chan [2]string, 1)
	go func() {
		_ = c.Command("silent! checktime")
		a, _ := c.EvalLuaString(`return vim.fn.getbufline(vim.fn.bufnr('clean.txt'), 1)[1]`)
		b, _ := c.EvalLuaString(`return vim.fn.getbufline(vim.fn.bufnr('dirty.txt'), 1)[1]`)
		done <- [2]string{a, b}
	}()
	select {
	case got := <-done:
		if got[0] != "new" {
			t.Errorf("unmodified buffer not reloaded: %q", got[0])
		}
		if got[1] != "mine" {
			t.Errorf("modified buffer lost its edits: %q", got[1])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nvim blocked on checktime (W12 prompt)")
	}
}
