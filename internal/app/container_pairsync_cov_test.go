package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/pairsync"
)

// ctrPairModel is the Containers view whose engine is the in-process
// fake engine with one running container api-1 (files under /app), and
// a local workspace folder as the process folder.
func ctrPairModel(t *testing.T) (*Model, *fakedocker.Container, string) {
	t.Helper()
	m, _ := ctrModel(t)
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.WriteFile(t, "/app/main.go", []byte("package main\n"), 0o644)
	m.services.containerEngine.X = d
	local, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(local)
	return m, c, local
}

var ctrPairC = containers.Container{ID: "api-1", Names: []string{"/api-1"}, State: "running", WorkingDir: "/app"}

// ctrApply runs cmd and applies the containerUIDoneMsg it ends with.
func ctrApply(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	done := ctrFind[containerUIDoneMsg](t, ctrMsgs(cmd))
	return done.apply(m)
}

// The prompt: a stopped container, a remote workspace or a sync that is
// running say why; the default folder is the last one, the working dir,
// then /workspace.
func TestCovContainerPairSyncPrompt(t *testing.T) {
	m, _, _ := ctrPairModel(t)
	_ = m.askPairSync(containers.Container{ID: "x", State: "exited"})
	if m.promptOpen || !strings.Contains(ctrToast(m), "Start the container first") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.pair.busy = true
	_ = m.askPairSync(ctrPairC)
	if !strings.Contains(ctrToast(m), "A pair sync is running") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.pair.busy = false
	_ = m.askPairSync(containers.Container{ID: "r", State: "running", WorkingDir: "/"})
	if !m.promptOpen || m.prompt.Value() != "/workspace" {
		t.Fatalf("default %q", m.prompt.Value())
	}
	_ = m.askPairSync(ctrPairC)
	if m.prompt.Value() != "/app" {
		t.Fatalf("default %q", m.prompt.Value())
	}
	m.services.pair.lastRoot = map[string]string{"api-1": "/srv"}
	_ = m.askPairSync(ctrPairC)
	if m.prompt.Value() != "/srv" {
		t.Fatalf("default %q", m.prompt.Value())
	}
	_ = m.onPairSyncFolderPrompt("relative")
	if !strings.Contains(ctrToast(m), "type a full folder") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ws, _, _, _ := fakeContainerWS(t)
	m.ws = ws
	m.promptOpen = false
	_ = m.askPairSync(ctrPairC)
	if m.promptOpen || !strings.Contains(ctrToast(m), "Open a local folder first") {
		t.Fatalf("remote: toast %s", ctrToast(m))
	}
}

// A first sync: the card lists the copies both ways, Sync applies them,
// the base is recorded and a second compare finds nothing to do.
func TestCovContainerPairSyncRoundTrip(t *testing.T) {
	m, c, local := ctrPairModel(t)
	if err := os.WriteFile(filepath.Join(local, "notes.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = m.askPairSync(ctrPairC)
	if cmd := ctrApply(t, m, m.onPairSyncFolderPrompt("/app")); cmd != nil {
		t.Fatalf("planned: toast %s", ctrToast(m))
	}
	if !m.confirmOpen || m.confirmKind != confirmKindPairSync {
		t.Fatalf("no card: %s", ctrToast(m))
	}
	view := m.confirm.View()
	for _, want := range []string{"notes.md", "main.go"} {
		if !strings.Contains(view, want) {
			t.Errorf("card lacks %s:\n%s", want, view)
		}
	}
	cmd, ok := m.handlePairSyncConfirm("sync")
	if !ok {
		t.Fatal("not handled")
	}
	_ = ctrApply(t, m, cmd)
	if !strings.Contains(ctrToast(m), "Pair sync: → 1 · ← 1") || m.services.pair.busy {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if b, err := os.ReadFile(filepath.Join(local, "main.go")); err != nil || string(b) != "package main\n" {
		t.Fatalf("pulled %q %v", b, err)
	}
	if b, err := os.ReadFile(c.Local("/app/notes.md")); err != nil || string(b) != "hi\n" {
		t.Fatalf("pushed %q %v", b, err)
	}
	_ = ctrApply(t, m, m.onPairSyncFolderPrompt("/app"))
	if !strings.Contains(ctrToast(m), "Already in sync") {
		t.Fatalf("second: %s", ctrToast(m))
	}
	// A delete on one side is a destructive card; Cancel clears busy.
	if err := os.Remove(filepath.Join(local, "notes.md")); err != nil {
		t.Fatal(err)
	}
	_ = ctrApply(t, m, m.onPairSyncFolderPrompt("/app"))
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "Delete in the container") {
		t.Fatalf("delete card: %s", ctrToast(m))
	}
	if _, ok := m.handlePairSyncConfirm("cancel"); !ok || m.services.pair.busy {
		t.Fatal("cancel")
	}
	m.confirmKind = 0
	if _, ok := m.handlePairSyncConfirm("sync"); ok {
		t.Fatal("handled another card")
	}
}

// A file changed on both sides is a conflict: nothing to write, so it
// runs at once and warns; same new files are only recorded.
func TestCovContainerPairSyncConflictAndAdopt(t *testing.T) {
	m, c, local := ctrPairModel(t)
	if err := os.WriteFile(filepath.Join(local, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = m.askPairSync(ctrPairC)
	cmd := ctrApply(t, m, m.onPairSyncFolderPrompt("/app"))
	if cmd == nil {
		t.Fatalf("same file: no run (%s)", ctrToast(m))
	}
	_ = ctrApply(t, m, cmd)
	if !strings.Contains(ctrToast(m), "= 1") {
		t.Fatalf("adopt toast %s", ctrToast(m))
	}
	_ = os.WriteFile(filepath.Join(local, "main.go"), []byte("package main // local\n"), 0o644)
	c.WriteFile(t, "/app/main.go", []byte("package main // container\n"), 0o644)
	cmd = ctrApply(t, m, m.onPairSyncFolderPrompt("/app"))
	_ = ctrApply(t, m, cmd)
	if !strings.Contains(ctrToast(m), "Fix by hand") || !strings.Contains(ctrToast(m), "main.go") {
		t.Fatalf("conflict toast %s", ctrToast(m))
	}
}

// Failures: a container that is gone, a broken base file, failed
// copies and an unrecorded sync each toast; runPairSync with no host
// does nothing.
func TestCovContainerPairSyncFailures(t *testing.T) {
	m, c, _ := ctrPairModel(t)
	_ = m.askPairSync(ctrPairC)
	c.Stop()
	_ = ctrApply(t, m, m.onPairSyncFolderPrompt("/app"))
	if !strings.Contains(ctrToast(m), "Cannot pair sync") || m.services.pair.busy {
		t.Fatalf("stopped: %s", ctrToast(m))
	}
	c.Start()
	// A base file that is not JSON.
	base, err := pairSyncBaseFile(mustAbs(t), "", "api-1", "/app")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(base), 0o700)
	_ = os.WriteFile(base, []byte("{"), 0o600)
	_ = ctrApply(t, m, m.onPairSyncFolderPrompt("/app"))
	if !strings.Contains(ctrToast(m), "Cannot pair sync") {
		t.Fatalf("bad base: %s", ctrToast(m))
	}
	_ = os.Remove(base)
	m.services.pair = pairSyncState{busy: true}
	if m.runPairSync() != nil || m.services.pair.busy {
		t.Fatal("no host: ran")
	}
	st := pairSyncState{name: "api-1", root: "/app", localRoot: "/w"}
	_ = m.onPairSyncDone(st, pairsync.Result{Pushed: 1, Failed: []string{"a.go: denied"}}, nil)
	if !strings.Contains(ctrToast(m), "a.go: denied") {
		t.Fatalf("failed: %s", ctrToast(m))
	}
	_ = m.onPairSyncDone(st, pairsync.Result{Pushed: 1}, errors.New("disk full"))
	if !strings.Contains(ctrToast(m), "The sync was not recorded") {
		t.Fatalf("save: %s", ctrToast(m))
	}
	st.plan.Actions = []pairsync.Action{{Path: "a", Op: pairsync.OpConflict}, {Path: "b", Op: pairsync.OpConflict}, {Path: "c", Op: pairsync.OpConflict}, {Path: "d", Op: pairsync.OpConflict}}
	_ = m.onPairSyncDone(st, pairsync.Result{Conflicts: 4}, nil)
	if !strings.Contains(ctrToast(m), "a, b, c, …") {
		t.Fatalf("conflicts: %s", ctrToast(m))
	}
}

// A container with no shell is listed by walking its files.
func TestCovContainerPairSyncNoShell(t *testing.T) {
	m, c, _ := ctrPairModel(t)
	c.NoShell()
	_ = m.askPairSync(ctrPairC)
	_ = ctrApply(t, m, m.onPairSyncFolderPrompt("/app"))
	if !m.confirmOpen && !strings.Contains(ctrToast(m), "Cannot pair sync") {
		t.Fatalf("no shell: %s", ctrToast(m))
	}
}

func mustAbs(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(aiWorkspaceRoot())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
