package testkit

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
)

// Spec: Given the app just started and nvim setup is still running (held
// half-way: base options done, LSP / theme / plugins not yet),
// when the user opens a.txt and types "xy",
// then a.txt is in Insert mode and the buffer reads "xyalpha", the same as
// for a file opened after setup (not Normal mode, where x deletes a char).
func TestOpenBeforeSetupStartsInInsertMode(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	reached := make(chan struct{})
	release := make(chan struct{})
	var once bool
	restore := app.SetNvimSetupGate(func() {
		if once {
			return
		}
		once = true
		close(reached)
		select {
		case <-release:
		case <-time.After(20 * time.Second):
		}
	})
	t.Cleanup(restore)
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: textFiles(),
		BeforeReady: func(d *Driver) {
			defer close(release)
			in := func() bool {
				select {
				case <-reached:
					return true
				default:
					return false
				}
			}
			if !d.WaitFor(in, 10*time.Second) {
				t.Fatalf("nvim setup never reached the gate")
			}
			open(d, "a.txt")
			if d.NvimReady() {
				t.Fatalf("setup finished while held")
			}
			if mode := lua(d, `return vim.api.nvim_get_mode().mode`); mode != "i" {
				t.Errorf("mode of a file opened during setup = %q, want i (Insert)", mode)
			}
			d.Type("xy")
			if got := bufLines(d); got != "xyalpha\nbravo\ncharlie" {
				t.Errorf("typing xy during setup: buffer = %q, want xyalpha…", got)
			}
		}})
	if !d.NvimReady() {
		t.Fatalf("nvim setup did not finish after the gate opened")
	}
}
