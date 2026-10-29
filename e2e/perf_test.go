//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Perf guards (STAB-5) on the real binary. Budgets live in
// internal/perfbudget/budgets.txt (read here: e2e is its own module);
// see that package for TERMOCODE_PERF and TERMOCODE_PERF_OUT.

// perfRequire skips unless perf guards are on.
func perfRequire(t *testing.T) {
	t.Helper()
	if testing.Short() || os.Getenv("TERMOCODE_PERF") != "1" {
		t.Skip("perf guard: set TERMOCODE_PERF=1 (and no -short) to run")
	}
}

// perfCheck is perfbudget.Check for the e2e module.
func perfCheck(t *testing.T, name string, got float64) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "internal", "perfbudget", "budgets.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var limit float64
	var unit string
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if f := strings.Fields(line); len(f) == 3 && f[0] == name {
			if limit, err = strconv.ParseFloat(f[1], 64); err != nil {
				t.Fatalf("budgets.txt: bad limit for %s: %q", name, f[1])
			}
			unit, found = f[2], true
		}
	}
	if !found {
		t.Fatalf("perf: no budget %q in internal/perfbudget/budgets.txt", name)
	}
	status := "ok"
	if got > limit {
		status = "OVER"
	}
	g := strconv.FormatFloat(got, 'f', -1, 64)
	l := strconv.FormatFloat(limit, 'f', -1, 64)
	t.Logf("perf: %s = %s %s (budget %s %s)", name, g, unit, l, unit)
	if p := os.Getenv("TERMOCODE_PERF_OUT"); p != "" {
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "%s\t%s\t%s\t%s\t%s\n", name, g, l, unit, status)
			_ = f.Close()
		}
	}
	if status != "ok" {
		t.Errorf("perf: %s = %s %s is over its budget %s %s (internal/perfbudget/budgets.txt)", name, g, unit, l, unit)
	}
}

// Startup to the first full frame (tab bar and status bar drawn): the
// median of three starts.
func TestPerfStartupFirstFrame(t *testing.T) {
	perfRequire(t)
	var took []time.Duration
	for i := 0; i < 3; i++ {
		tm := Start(t, StartOpts{})
		tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
			return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
		})
		took = append(took, time.Since(tm.started))
		tm.mu.Lock()
		alt := tm.altAt.Sub(tm.started)
		tm.mu.Unlock()
		t.Logf("start %d: alt screen after %s, first frame after %s", i+1, alt, took[i])
		tm.settle()
		tm.Quit()
	}
	slices.Sort(took)
	perfCheck(t, "startup_first_frame", float64(took[1].Milliseconds()))
}

// Memory of one idle session: the client, the session server and all
// under it (UI, nvim, shell), with a plain text file open (no language
// server, so the number does not depend on what is installed).
func TestPerfSessionIdleRSS(t *testing.T) {
	perfRequire(t)
	r := newSessionRig(t)
	tm := r.start()
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.settle()
	sockDir := filepath.Join(r.runtime, "termocode")
	b, err := os.ReadFile(filepath.Join(sockDir, hashKey(r.ws)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(b, &info); err != nil || info.PID == 0 {
		t.Fatalf("session info %q: %v", b, err)
	}
	total := func() (int, string) {
		var parts []string
		kb := 0
		for _, pid := range append([]int{tm.cmd.Process.Pid}, procTree(info.PID)...) {
			n := rssKB(pid)
			kb += n
			parts = append(parts, fmt.Sprintf("%s %d MB", procName(pid), n/1024))
		}
		return kb, strings.Join(parts, ", ")
	}
	// Idle: wait until the total stops growing (within 2 %).
	kb, parts := total()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tm.WaitStable(500*time.Millisecond, uiTimeout)
		next, p := total()
		stable := next <= kb+kb/50
		kb, parts = max(kb, next), p
		if stable {
			break
		}
	}
	t.Logf("idle RSS of one session: %d MB (%s)", kb/1024, parts)
	perfCheck(t, "session_idle_rss", float64(kb/1024))
	tm.Quit()
}
