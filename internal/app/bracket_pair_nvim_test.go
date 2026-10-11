package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// A half-cloned plugin dir (first start interrupted) or a failed clone used
// to vim.notify a warning. With cmdheight=0 that is a hit-enter prompt that
// blocks every RPC and key: the editor froze at start-up (found by the e2e
// suite with an empty plugin dir).
func TestBracketPairSetupNeverBlocksNvim(t *testing.T) {
	nv, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	withConfigDir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	// PATH with nvim but without git: the clone cannot run.
	bin := t.TempDir()
	if err := os.Symlink(nv, filepath.Join(bin, "nvim")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	plugin := filepath.Join(home, ".local", "share", "termocode", "plugins", "rainbow-delimiters.nvim")
	if err := os.MkdirAll(plugin, 0o755); err != nil { // half-finished clone
		t.Fatal(err)
	}

	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = c.Command("set cmdheight=0")
	// The old code blocked nvim forever; fail instead of hanging.
	done := make(chan string, 1)
	go func() {
		_ = c.ExecLua(bracketPairLua)
		m, _ := c.EvalLuaString(`local m = vim.api.nvim_get_mode() return m.mode .. (m.blocking and ':blocked' or '')`)
		done <- m
	}()
	var mode string
	select {
	case mode = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("nvim blocked by bracket-pair setup (hit-enter prompt?)")
	}
	if mode != "n" {
		t.Fatalf("nvim mode after bracket setup = %q, want plain normal mode", mode)
	}
	why, _ := c.EvalLuaString(`return vim.g.termocode_bracket_pair_error or ''`)
	if why == "" {
		t.Fatalf("setup failure was not recorded")
	}
	if _, err := os.Stat(plugin); !os.IsNotExist(err) {
		t.Fatalf("half-finished plugin dir was not removed (stat err=%v)", err)
	}
}
