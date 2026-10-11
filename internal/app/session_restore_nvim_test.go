package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

func newRestoreNvim(t *testing.T) *nvim.Client {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	withConfigDir(t)
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = c.Command("set hidden")
	return c
}

func restoreFiles(t *testing.T) (a, b, c string) {
	dir := t.TempDir()
	a, b, c = filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt"), filepath.Join(dir, "c.txt")
	for _, p := range []string{a, b, c} {
		_ = os.WriteFile(p, []byte("1\n2\n3\n4\n"), 0o644)
	}
	return a, b, c
}

func currentBufName(t *testing.T, c *nvim.Client) string {
	t.Helper()
	s, err := c.EvalLuaString(`return vim.api.nvim_buf_get_name(0)`)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The usual case: nothing open yet, the last active file comes back.
func TestSessionRestoreOpensActiveFileNvim(t *testing.T) {
	c := newRestoreNvim(t)
	a, b, _ := restoreFiles(t)
	if err := c.ExecLua(sessionRestoreLua([]string{a, b}, b, map[string][2]int{b: {3, 1}})); err != nil {
		t.Fatal(err)
	}
	if got := currentBufName(t, c); got != b {
		t.Fatalf("current buffer %q, want %q", got, b)
	}
	if row, _ := c.EvalLuaString(`return tostring(vim.fn.line('.'))`); row != "3" {
		t.Fatalf("cursor row %s, want 3", row)
	}
	if ok, _ := c.EvalLuaStringArgs(`return tostring(vim.fn.bufexists(...))`, a); ok != "1" {
		t.Fatalf("%s not restored", a)
	}
}

// nvim finishes setup (ReadyMsg) after the UI is already usable. When the
// user opened a file in that gap, the restore must not pull them back to
// the session's old active file, nor move their cursor.
func TestSessionRestoreKeepsUserFileNvim(t *testing.T) {
	c := newRestoreNvim(t)
	a, b, user := restoreFiles(t)
	if err := c.Command("edit " + user); err != nil {
		t.Fatal(err)
	}
	_ = c.Command("call cursor(2, 1)")
	lua := sessionRestoreLua([]string{a, b, user}, b, map[string][2]int{b: {3, 1}, user: {4, 1}})
	if err := c.ExecLua(lua); err != nil {
		t.Fatal(err)
	}
	if got := currentBufName(t, c); got != user {
		t.Fatalf("current buffer %q, want the user's %q", got, user)
	}
	if row, _ := c.EvalLuaString(`return tostring(vim.fn.line('.'))`); row != "2" {
		t.Fatalf("cursor row %s, want 2 (user's position)", row)
	}
	for _, p := range []string{a, b} {
		if ok, _ := c.EvalLuaStringArgs(`return tostring(vim.fn.bufexists(...))`, p); ok != "1" {
			t.Fatalf("%s not restored", p)
		}
	}
}

// Same gap, but the user opened the terminal panel: the session's active
// file must not be loaded into the terminal window.
func TestSessionRestoreLeavesTerminalWindowNvim(t *testing.T) {
	c := newRestoreNvim(t)
	a, b, _ := restoreFiles(t)
	if err := c.Command("botright 5new | terminal"); err != nil {
		t.Fatal(err)
	}
	if err := c.ExecLua(sessionRestoreLua([]string{a, b}, b, nil)); err != nil {
		t.Fatal(err)
	}
	bt, _ := c.EvalLuaString(`return vim.bo.buftype`)
	if bt != "terminal" {
		t.Fatalf("current window now shows buftype %q (%s), want the terminal", bt, currentBufName(t, c))
	}
	if ok, _ := c.EvalLuaStringArgs(`return tostring(vim.fn.buflisted(...))`, b); ok != "1" {
		t.Fatalf("%s not added to the buffer list", b)
	}
}
