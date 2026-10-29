package nvim

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestLeakTrackingNamesTheTest: a client started by a test is listed with
// that test's name until it is closed and its process is gone.
func TestLeakTrackingNamesTheTest(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	TrackClients()
	before := ChildProcesses()
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if got := LeakedClients(0); !slices.Contains(got, "TestLeakTrackingNamesTheTest") {
		_ = c.Close()
		t.Fatalf("live clients %q", got)
	}
	if before >= 0 && ChildProcesses() != before+1 {
		t.Errorf("nvim children %d, want %d", ChildProcesses(), before+1)
	}
	if r := LeakReport(0); !strings.Contains(r, "  TestLeakTrackingNamesTheTest: 1\n") {
		t.Errorf("report %q", r)
	}
	if err := c.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	if got := LeakedClients(5 * time.Second); slices.Contains(got, "TestLeakTrackingNamesTheTest") {
		t.Fatalf("still live after Close: %q", got)
	}
	if before >= 0 && ChildProcesses() != before {
		t.Errorf("nvim children %d after Close, want %d", ChildProcesses(), before)
	}
}
