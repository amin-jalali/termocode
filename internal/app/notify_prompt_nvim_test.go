package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// A vim.notify warning during startup (for example "rainbow-delimiters
// bootstrap failed (offline?)") must not open a hit-enter prompt: with
// cmdheight=0 nvim then blocks every RPC call and the whole app freezes.
func TestNotifyDoesNotBlockNvim(t *testing.T) {
	c, err := nvim.New()
	if err != nil {
		t.Skipf("nvim not available: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = c.Command("set cmdheight=0")
	if err := c.ExecLua(notifyLua); err != nil {
		t.Fatalf("notifyLua: %v", err)
	}
	long := strings.Repeat("offline? ", 40)
	done := make(chan string, 1)
	go func() {
		_ = c.ExecLua(`vim.notify('termocode: bootstrap failed: ` + long + `', vim.log.levels.WARN)`)
		_ = c.ExecLua(`vim.notify('second line\nthird line', vim.log.levels.ERROR)`)
		out, _ := c.EvalLuaString(`return vim.api.nvim_get_mode().mode`)
		done <- out
	}()
	select {
	case mode := <-done:
		if mode == "r" || mode == "rm" {
			t.Fatalf("nvim is in a hit-enter prompt (mode %q)", mode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nvim blocked after vim.notify (hit-enter prompt)")
	}
}
