package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/kube/kubetest"
)

// kubeFake is a fake kubectl on PATH with the kube package's fixtures:
// context dev, namespace shop. KUBECONFIG points into a temp dir.
func kubeFake(t *testing.T) *kubetest.Fake {
	t.Helper()
	f := kubetest.New(t)
	f.Fixtures(t, filepath.Join("..", "kube", "testdata"))
	t.Setenv("PATH", f.PathEnv())
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "kubeconfig"))
	return f
}

// kubeTestModel is a model with the Kubernetes view on screen.
func kubeTestModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := &Model{services: newServicesState(), kube: newKubeState(), w: w, h: h, explorerWidth: 30, showExp: true, hoverX: -1, hoverY: -1}
	m.activity.SetActive(activity.ViewKubernetes)
	m.focus = FocusExplorer
	t.Cleanup(m.kube.shutdown)
	return m
}

// kubeRunLoad runs a load command and applies its result; it returns the
// command the result asks for (the next tick; never run in a test).
func kubeRunLoad(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no load command")
	}
	msg, ok := cmd().(kubeLoadMsg)
	if !ok {
		t.Fatal("not a load result")
	}
	return m.onKubeLoad(msg)
}

// kubeLoaded is a model whose view showed and loaded once.
func kubeLoaded(t *testing.T, w, h int) (*kubetest.Fake, *Model) {
	t.Helper()
	f := kubeFake(t)
	m := kubeTestModel(t, w, h)
	kubeRunLoad(t, m, m.kubeSync())
	return f, m
}

func TestKubeShowLoadsTheFourListsInTheCurrentScope(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	st := m.kube
	if st.context != "dev" || st.namespace != "shop" {
		t.Errorf("scope = %q/%q", st.context, st.namespace)
	}
	if len(st.pods) != 5 || len(st.deploys) != 3 || len(st.services) != 3 || len(st.jobs) != 3 {
		t.Errorf("rows = %d pods, %d deployments, %d services, %d jobs", len(st.pods), len(st.deploys), len(st.services), len(st.jobs))
	}
	for _, kind := range []string{"pods", "deployments", "services", "jobs"} {
		if f.Count("get "+kind+" -o json --context dev --namespace shop") != 1 {
			t.Errorf("no scoped get %s in %v", kind, f.Calls())
		}
	}
	if st.err != "" || len(st.errs) != 0 {
		t.Errorf("errors = %q %v", st.err, st.errs)
	}
	if again := m.kubeSync(); again != nil {
		t.Error("a second sync started a second chain")
	}
}

func TestKubeNotLoadedWhenNotWanted(t *testing.T) {
	f := kubeFake(t)
	for name, tweak := range map[string]func(m *Model){
		"other view":     func(m *Model) { m.activity.SetActive(activity.ViewServices) },
		"sidebar hidden": func(m *Model) { m.showExp = false },
		"window blurred": func(m *Model) { m.termBlurred = true },
	} {
		t.Run(name, func(t *testing.T) {
			m := kubeTestModel(t, 150, 42)
			tweak(m)
			if cmd := m.kubeSync(); cmd != nil {
				t.Error("a load started")
			}
		})
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("%d kubectl calls", n)
	}
}

func TestKubeTicksOnlyWhileVisible(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	st := m.kube
	if st.delay != kubeInterval {
		t.Errorf("delay = %v, want %v", st.delay, kubeInterval)
	}
	// A tick while visible loads again.
	kubeRunLoad(t, m, m.onKubeTick(kubeTickMsg{gen: st.gen}))
	if n := f.Count("get pods"); n != 2 {
		t.Errorf("pod lists = %d, want 2", n)
	}
	// Hidden: the chain ends; the old tick is dropped.
	gen := st.gen
	m.activity.SetActive(activity.ViewFiles)
	if cmd := m.kubeSync(); cmd != nil || st.polling {
		t.Error("still polling after the view hid")
	}
	if cmd := m.onKubeTick(kubeTickMsg{gen: gen}); cmd != nil {
		t.Error("an old tick loaded")
	}
	// Back on screen: a load at once.
	m.activity.SetActive(activity.ViewKubernetes)
	kubeRunLoad(t, m, m.kubeSync())
	if n := f.Count("get pods"); n != 3 {
		t.Errorf("pod lists = %d, want 3", n)
	}
}

func TestKubeLateLoadOfOldScopeIsDropped(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	old := m.kubeLoad()
	m.kubeRescope("prod", "default")
	if msg := old().(kubeLoadMsg); m.onKubeLoad(msg) != nil || len(m.kube.pods) != 0 {
		t.Error("a late load of the old scope was used")
	}
	_ = f
}

func TestKubeBackoffOnErrors(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	st := m.kube
	f.Set(t, "all.err", "Unable to connect to the server: dial tcp 10.0.0.1:6443: connect: connection refused\n")
	var waits []time.Duration
	for i := 0; i < 5; i++ {
		kubeRunLoad(t, m, m.onKubeTick(kubeTickMsg{gen: st.gen}))
		waits = append(waits, st.delay)
	}
	want := []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
	if st.err != "Cannot reach the cluster" {
		t.Errorf("err = %q", st.err)
	}
	if rows := st.rows(); len(rows) != 1 || rows[0].kind != kubeRowNote {
		t.Errorf("rows = %+v, want one note", rows)
	}
	f.Remove("all.err")
	kubeRunLoad(t, m, m.onKubeTick(kubeTickMsg{gen: st.gen}))
	if st.delay != kubeInterval || st.err != "" || len(st.pods) != 5 {
		t.Errorf("after recovery: delay %v err %q pods %d", st.delay, st.err, len(st.pods))
	}
}

func TestKubeForbiddenSectionKeepsTheOthers(t *testing.T) {
	f := kubeFake(t)
	f.Set(t, "jobs.err", "Error from server (Forbidden): jobs.batch is forbidden: User \"dev\" cannot list resource \"jobs\"\n")
	m := kubeTestModel(t, 150, 42)
	kubeRunLoad(t, m, m.kubeSync())
	st := m.kube
	if st.err != "" || len(st.pods) != 5 {
		t.Fatalf("err %q pods %d", st.err, len(st.pods))
	}
	if e := st.errs["jobs"]; !strings.HasPrefix(e, "Not allowed: ") {
		t.Errorf("jobs error = %q", e)
	}
	found := false
	for _, r := range st.rows() {
		if r.kind == kubeRowNote && r.section == "jobs" {
			found = true
		}
	}
	if !found {
		t.Error("no note row under JOBS")
	}
}

func TestKubeWithoutKubectl(t *testing.T) {
	orig := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	m := kubeTestModel(t, 150, 42)
	if next := kubeRunLoad(t, m, m.kubeSync()); next != nil {
		t.Error("polls without kubectl")
	}
	st := m.kube
	if st.err != "kubectl is not installed" || st.polling {
		t.Errorf("err %q polling %v", st.err, st.polling)
	}
	// R looks again: kubectl was installed meanwhile.
	t.Setenv("PATH", orig)
	f := kubeFake(t)
	kubeRunLoad(t, m, m.kubeReload())
	if st.err != "" || len(st.pods) != 5 || st.binary != f.Kubectl() {
		t.Errorf("after R: err %q pods %d binary %q", st.err, len(st.pods), st.binary)
	}
}

func TestKubeNoContext(t *testing.T) {
	f := kubeFake(t)
	f.Remove("current-context.txt")
	m := kubeTestModel(t, 150, 42)
	kubeRunLoad(t, m, m.kubeSync())
	if m.kube.err != "No cluster set up: kubeconfig has no current context" {
		t.Errorf("err = %q", m.kube.err)
	}
}
