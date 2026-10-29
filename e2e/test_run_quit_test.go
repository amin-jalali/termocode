//go:build e2e

package e2e

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spec (SCN-3): quitting while a test run is going ends the run: no
// `go test` or test binary outlives termocode.
func TestTestRunEndsOnQuit(t *testing.T) {
	ws := NewRepo(t)
	writeFile(t, filepath.Join(ws, "go.mod"), "module scn\n\ngo 1.21\n")
	writeFile(t, filepath.Join(ws, "gate_test.go"), scnGateTest)
	tm := Start(t, StartOpts{Workspace: ws, Env: goTestEnv(t), Args: []string{"--no-session"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.Palette("Testing: Run All Tests")
	waitFileHas(t, filepath.Join(ws, "test-runs.log"), "start", 90*time.Second)
	tm.Quit()
	if left := waitNoProcs(tm.home, []string{tm.home, ws}, 5*time.Second); len(left) > 0 {
		t.Fatalf("still running 5s after quit:\n%s", strings.Join(left, "\n"))
	}
}
