package nvim

import (
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Close is idempotent: Quit / Reload Window close nvim and main closes it
// again; the second call used to panic (close of closed channel).
func TestCloseTwice(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = c.Attach(80, 24) // an attached nvim exits at once on Close
	_ = c.Close()
	_ = c.Close()
}

// nextMsg runs c.Next (skipping redraws) with a deadline.
func nextMsg(t *testing.T, c *Client) tea.Msg {
	t.Helper()
	ch := make(chan tea.Msg, 1)
	go func() {
		for {
			msg := c.Next()()
			if _, ok := msg.(RedrawMsg); ok {
				continue
			}
			ch <- msg
			return
		}
	}()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("Next did not return within 5s")
		return nil
	}
}

// A killed nvim is reported as ExitedMsg, and calls made after that fail
// at once instead of hanging.
func TestExitedMsgWhenNvimDies(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	s, err := c.EvalLuaString(`return tostring(vim.fn.getpid())`)
	if err != nil {
		t.Fatalf("pid: %v", err)
	}
	pid, _ := strconv.Atoi(s)
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill: %v", err)
	}
	msg, ok := nextMsg(t, c).(ExitedMsg)
	if !ok || msg.Client != c {
		t.Fatalf("got %#v, want ExitedMsg for this client", msg)
	}
	if !c.Exited() {
		t.Error("Exited() is false after nvim died")
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.FetchState()
		if err == nil {
			err = c.Command("echo 1")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("calls after nvim died returned no error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("call after nvim died hangs")
	}
	c.MouseAsync("left", "press", "", 0, 0) // must not panic or block
}

// Close (a normal quit) is not reported as a crash.
func TestNoExitedMsgAfterClose(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = c.Attach(80, 24)
	_ = c.Close()
	if msg := nextMsg(t, c); msg != nil {
		t.Errorf("Next after Close = %#v, want nil", msg)
	}
}
