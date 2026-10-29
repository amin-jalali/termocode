//go:build unix

package servicestest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The guard's stand-ins fail closed: each call exits 1 with a message,
// runs nothing, and is logged with the test that made it.
func TestGuardBlocksAndRecords(t *testing.T) {
	g, err := InstallGuard(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range GuardedCLIs {
		cmd := exec.Command(filepath.Join(g.Bin, name), "ps", "-a")
		cmd.Env = append(os.Environ(), GuardTestEnv+"=TestX/sub")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("%s stand-in exited 0", name)
		}
		if !strings.Contains(string(out), "blocked in tests") {
			t.Errorf("%s stand-in said %q", name, out)
		}
	}
	calls := g.Calls("TestX/sub")
	if len(calls) != len(GuardedCLIs) || calls[0] != "TestX/sub|docker ps -a" {
		t.Fatalf("calls = %q", calls)
	}
	if len(g.Calls("TestX")) != 0 {
		t.Error("a parent test name matched its subtest's calls")
	}
	if g.Report("TestY") != "" || !strings.Contains(g.Report(""), "kubectl ps -a") {
		t.Errorf("report: %q", g.Report(""))
	}
}
