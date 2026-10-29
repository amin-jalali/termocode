//go:build unix

package testkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/kube/kubetest"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// Guard for TestMain: with no fake installed, every container / cluster
// tool on PATH is the guard's stand-in, never the real one.
func TestRealContainerCLIsAreBlocked(t *testing.T) {
	New(t, Options{})
	guardBin := ""
	if cliGuard != nil {
		guardBin = cliGuard.Bin
	}
	for _, name := range servicestest.GuardedCLIs {
		p, err := exec.LookPath(name)
		if err != nil {
			continue // not installed at all: nothing to reach
		}
		if filepath.Dir(p) != guardBin {
			t.Errorf("a test would run the real %s at %s", name, p)
		}
	}
}

// The leak this guards against: the monkey and the palette walk open the
// Containers view without a fake docker. That view must hit the guard
// (and show an error), never the developer's real Docker daemon.
func TestContainersViewWithoutFakeHitsGuard(t *testing.T) {
	d := New(t, Options{W: 120, H: 34})
	if cliGuard == nil {
		t.Fatal("TestMain installed no CLI guard")
	}
	const tag = "expected-block"
	t.Setenv(servicestest.GuardTestEnv, tag) // these calls are expected
	d.Palette("containers-focus")
	if !d.WaitFor(func() bool { return len(cliGuard.Calls(tag)) > 0 }, 10*time.Second) {
		t.Fatalf("the Containers view ran no docker through the guard (a real one?):\n%s", screen(d))
	}
}

// fakeEngine puts a working fake docker first on PATH, answering from the
// docker27 fixtures, and a fake kubectl (an empty cluster, the
// Kubernetes view may open too). Tests that may open a container view on their own
// (the monkey, the palette and key walks) call it before New.
func fakeEngine(t *testing.T) *servicestest.Fake {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixtures := filepath.Join(wd, "..", "containers", "testdata", "docker27")
	f := servicestest.New(t)
	for _, name := range []string{"version.json", "ps.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(fixtures, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	k := kubetest.New(t)
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "kubeconfig"))
	t.Setenv("PATH", k.Bin+string(os.PathListSeparator)+f.PathEnv())
	return f
}
