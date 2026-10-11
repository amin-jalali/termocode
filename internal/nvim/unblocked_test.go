package nvim

import (
	"errors"
	"os/exec"
	"testing"
	"time"
)

// A key that makes nvim wait for more input ("f" in Normal mode, Ctrl+K
// in Insert mode) must not make the next call hang: that froze the app
// (Update waited for nvim, nvim waited for a key that only Update sends).
func TestCallWhileNvimWaitsForAKey(t *testing.T) {
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
	_ = c.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'abcxdef'})`)
	for _, keys := range []string{"<Esc>0f", "<Esc>i<C-k>"} {
		_ = c.Input(keys)
		time.Sleep(50 * time.Millisecond)
		done := make(chan error, 1)
		go func() {
			_, err := c.EvalLuaString(`return 'ok'`)
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, ErrBlocked) {
				t.Errorf("%s: err = %v, want ErrBlocked", keys, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: call hung while nvim waited for a key", keys)
		}
		_ = c.Input("x<Esc>") // finish the key sequence
		var s string
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if s, err = c.EvalLuaString(`return 'ok'`); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if s != "ok" {
			t.Fatalf("%s: nvim did not recover: %q %v", keys, s, err)
		}
	}
	// "f" then "x" moved the cursor onto the x.
	if col, _ := c.EvalLuaString(`return tostring(vim.api.nvim_win_get_cursor(0)[2])`); col != "3" && col != "4" {
		t.Logf("cursor col after fx: %s", col)
	}
}
