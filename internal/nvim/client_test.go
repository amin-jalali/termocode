package nvim

import (
	"os/exec"
	"testing"
	"time"
)

func TestClientHandshake(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}

	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	deadline := time.After(3 * time.Second)
	got := 0
	for got == 0 {
		select {
		case msg := <-c.events:
			if r, ok := msg.(RedrawMsg); ok {
				got = len(r.Events)
			}
		case <-deadline:
			t.Fatal("no redraw events within 3s")
		}
	}
	if got == 0 {
		t.Fatal("got RedrawMsg but no events inside")
	}
}

func TestClientInput(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}

	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	// Drain the initial redraw burst.
	deadline := time.After(2 * time.Second)
drain:
	for {
		select {
		case <-c.events:
		case <-deadline:
			break drain
		case <-time.After(200 * time.Millisecond):
			break drain
		}
	}

	if err := c.Input("ihello<Esc>"); err != nil {
		t.Fatalf("Input: %v", err)
	}

	// Wait for new redraws after input.
	gotPostInput := false
	deadline = time.After(2 * time.Second)
	for !gotPostInput {
		select {
		case msg := <-c.events:
			if _, ok := msg.(RedrawMsg); ok {
				gotPostInput = true
			}
		case <-deadline:
			t.Fatal("no redraw events after Input within 2s")
		}
	}
}
