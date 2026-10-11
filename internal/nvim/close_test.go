package nvim

import (
	"os/exec"
	"testing"
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
	_ = c.Close()
	_ = c.Close()
}
