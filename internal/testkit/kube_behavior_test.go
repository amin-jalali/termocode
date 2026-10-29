package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/kube/kubetest"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// The Kubernetes view in the whole app on a fake kubectl: it loads the
// current context's namespace, the restart card does nothing on Enter
// (Cancel has the focus), a port-forward shows its local port and ends
// with termocode.
func TestKubernetesViewEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	wd, _ := os.Getwd()
	f := kubetest.New(t)
	f.Fixtures(t, filepath.Join(wd, "..", "kube", "testdata"))
	f.Set(t, "pf-port.txt", "41777")
	t.Setenv("PATH", f.PathEnv())
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "kubeconfig"))
	// Runs after the driver's own cleanup (Model.Close): the quit path.
	t.Cleanup(func() {
		if pid := f.PID("pf.pid"); pid > 0 && !servicestest.WaitGone(pid, 10*time.Second) {
			t.Errorf("kubectl port-forward (pid %d) outlived termocode", pid)
		}
	})
	d := New(t, Options{W: 120, H: 34, Nvim: true})
	d.Palette("kubernetes-show")
	if !d.WaitForText("77k2p", 5*time.Second) {
		t.Fatalf("the pods did not load:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "ctx dev · ns shop") {
		t.Errorf("no scope line:\n%s", screen(d))
	}
	// The restart card: Enter picks Cancel.
	d.Key("tab") // DEPLOYMENTS
	d.Key("j")
	d.Key("r")
	if !d.WaitForText("Restart deployment?", 3*time.Second) {
		t.Fatalf("no restart card:\n%s", screen(d))
	}
	d.Key("enter")
	d.Settle(300 * time.Millisecond)
	if f.Count("rollout") != 0 {
		t.Fatalf("Enter restarted: %v", f.Calls())
	}
	// Port-forward web-0.
	d.Key("g")
	for i := 0; i < 10 && !strings.Contains(cursorLine(screen(d)), "web-0"); i++ {
		d.Key("j")
	}
	d.Key("f")
	if !d.WaitForText("41777", 5*time.Second) {
		t.Fatalf("the forward's port is not shown:\n%s", screen(d))
	}
	if pid := f.PID("pf.pid"); !servicestest.Alive(pid) {
		t.Fatalf("no running port-forward")
	}
}
