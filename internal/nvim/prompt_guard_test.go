package nvim

import (
	"os/exec"
	"testing"
	"time"
)

// An error from user input (Ctrl+] with no tags file, a bad :command)
// opens a hit-enter prompt with cmdheight=0; nvim then answers no RPC
// call and the app froze. The prompt guard must answer it.
func TestPromptGuardAnswersHitEnter(t *testing.T) {
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
	for _, keys := range []string{"<Esc><C-]>", "<Esc>:nosuchcommand<CR>", "<Esc>:echo \"a\\nb\\nc\"<CR>"} {
		_ = c.Input(keys)
		done := make(chan string, 1)
		go func() {
			out, _ := c.EvalLuaString(`return vim.api.nvim_get_mode().mode`)
			done <- out
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: nvim stayed in a prompt (RPC hung)", keys)
		}
	}
	// The app is told, so it can show the message.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-c.events:
			if _, ok := ev.(PromptMsg); ok {
				return
			}
		case <-deadline:
			t.Fatal("no PromptMsg event")
		}
	}
}

// After Close, a late mouse event is dropped instead of panicking
// (send on closed channel).
func TestMouseAsyncAfterClose(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = c.Attach(80, 24) // an attached nvim exits at once on Close
	_ = c.Close()
	c.MouseAsync("left", "press", "", 1, 1)
}

// The guard asked nvim for its mode 10 times a second forever, which
// woke both processes while the editor sat idle. With nvim quiet it
// should hardly poll; a prompt still always comes with a redraw, which
// wakes the guard, so a late error (from a timer here) is still answered.
func TestPromptGuardSleepsWhileIdle(t *testing.T) {
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
	time.Sleep(promptGuardQuiet + 500*time.Millisecond)
	before := c.guardPolls.Load()
	time.Sleep(2 * time.Second)
	if n := c.guardPolls.Load() - before; n > 2 {
		t.Errorf("guard polled nvim %d times in 2 s while idle, want ≤ 2", n)
	}

	// A message from a timer opens a hit-enter prompt with no key pressed.
	_ = c.Command(`lua vim.defer_fn(function() vim.api.nvim_echo({{"a\nb\nc"}}, true, {}) end, 300)`)
	time.Sleep(time.Second)
	done := make(chan struct{})
	go func() {
		_, _ = c.EvalLuaString(`return vim.api.nvim_get_mode().mode`)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("nvim stayed in the prompt from the timer (RPC hung)")
	}
}
