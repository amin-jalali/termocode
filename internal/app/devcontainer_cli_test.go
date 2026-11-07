package app

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/devcontainer"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// dcCLIExec fakes the engine and the devcontainer CLI: id is the
// container a label query finds ("" = none); up is what "up" prints.
type dcCLIExec struct {
	mu    sync.Mutex
	calls []string
	id    string
	up    string
}

func (x *dcCLIExec) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	line := strings.Join(argv, " ")
	x.mu.Lock()
	x.calls = append(x.calls, line)
	x.mu.Unlock()
	switch {
	case strings.Contains(line, " ps -a -q "):
		if x.id == "" {
			return "", "", 0, nil
		}
		return x.id + "\n", "", 0, nil
	case strings.Contains(line, " up --workspace-folder "):
		if strings.Contains(x.up, `"error"`) {
			return x.up, "feature install failed\n", 1, nil
		}
		return x.up, "", 0, nil
	}
	return "", "", 0, nil
}
func (x *dcCLIExec) CaptureInput(ctx context.Context, dir string, _ []byte, argv ...string) (string, string, int, error) {
	return x.Capture(ctx, dir, argv...)
}
func (x *dcCLIExec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, func() {}, io.EOF
}

const dcCompose = `{"dockerComposeFile": "c.yml", "service": "app", "workspaceFolder": "/src", "features": {"ghcr.io/devcontainers/features/go:1": {}}, "postCreateCommand": "make deps"}`

// dcCLIProject is dcProject with the CLI fake and a found CLI.
func dcCLIProject(t *testing.T, src string, cli string) (*Model, *dcCLIExec, string) {
	t.Helper()
	m, _, root := dcProject(t, src)
	x := &dcCLIExec{up: `{"outcome":"success","containerId":"abc123","remoteWorkspaceFolder":"/src"}`}
	devcontainerExec = x
	old := devcontainerLookCLI
	devcontainerLookCLI = func() string { return cli }
	t.Cleanup(func() { devcontainerLookCLI = old })
	return m, x, root
}

func TestReopenComposeWithoutCLISaysHowToGetIt(t *testing.T) {
	m, x, _ := dcCLIProject(t, dcCompose, "")
	m.openDevContainerReopen()
	if m.confirmOpen || !strings.Contains(regToast(m), "devcontainer CLI") || len(x.calls) != 0 {
		t.Fatalf("card %v toast %q calls %v", m.confirmOpen, regToast(m), x.calls)
	}
}

func TestReopenComposeCardNamesTheCLIAndFeatures(t *testing.T) {
	m, x, _ := dcCLIProject(t, dcCompose, "/usr/bin/devcontainer")
	m.openDevContainerReopen()
	if !m.confirmOpen || m.confirmKind != confirmKindDevContainerTrust {
		t.Fatalf("card open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	if b := m.confirm.Buttons(); len(b) != 2 || b[0].ID != "cancel" {
		t.Fatalf("buttons = %+v", b)
	}
	text := devcontainerCardText(m.devcontainerPending.cfg, m.devcontainerPending.setup, false)
	for _, want := range []string{"devcontainer CLI", "ghcr.io/devcontainers/features/go:1", "postCreateCommand on the new container: make deps", "trust this project"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card text misses %q:\n%s", want, text)
		}
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if len(x.calls) != 0 {
		t.Fatalf("Cancel ran %v", x.calls)
	}
}

func TestReopenComposeRunsCLIUpAndOpensItsContainer(t *testing.T) {
	got := swapReexec(t)
	m, x, root := dcCLIProject(t, dcCompose, "/usr/bin/devcontainer")
	m.openDevContainerReopen()
	m.confirmOpen = false
	m.handleConfirmSelect("create")
	dcDrive(m)
	joined := strings.Join(x.calls, "\n")
	for _, want := range []string{
		"ps -a -q --filter label=" + devcontainer.Label + "=" + filepath.Clean(root),
		"/usr/bin/devcontainer up --workspace-folder " + filepath.Clean(root),
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("calls miss %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, " exec ") || strings.Contains(joined, "make deps") {
		t.Fatalf("termocode ran the lifecycle itself:\n%s", joined)
	}
	if len(*got) != 1 || strings.Join((*got)[0], " ") != "--container abc123 /src" {
		t.Fatalf("re-exec = %v", *got)
	}
}

func TestReopenComposeExistingIsOnlyStarted(t *testing.T) {
	got := swapReexec(t)
	m, x, _ := dcCLIProject(t, dcCompose, "/usr/bin/devcontainer")
	x.id = "old999"
	m.openDevContainerReopen()
	m.confirmOpen = false
	m.handleConfirmSelect("create")
	dcDrive(m)
	joined := strings.Join(x.calls, "\n")
	if !strings.Contains(joined, "start old999") || strings.Contains(joined, " up ") {
		t.Fatalf("an existing container ran the CLI:\n%s", joined)
	}
	if len(*got) != 1 || strings.Join((*got)[0], " ") != "--container old999 /src" {
		t.Fatalf("re-exec = %v", *got)
	}
}

func TestReopenComposeCLIFailureStops(t *testing.T) {
	got := swapReexec(t)
	m, x, _ := dcCLIProject(t, dcCompose, "/usr/bin/devcontainer")
	x.up = `{"outcome":"error","message":"boom"}`
	m.openDevContainerReopen()
	m.confirmOpen = false
	m.handleConfirmSelect("create")
	dcDrive(m)
	if len(*got) != 0 || m.devcontainer != nil || !strings.Contains(regToast(m), "failed") {
		t.Fatalf("re-exec %v run %v toast %q", *got, m.devcontainer, regToast(m))
	}
}

func TestDevContainerOfferToastAndCard(t *testing.T) {
	m, _, root := dcProject(t, dcSample)
	cmd := m.devcontainerOfferCmd()
	if cmd == nil {
		t.Fatal("no offer for a project with devcontainer.json")
	}
	msg := cmd()
	if _, ok := m.handleDevContainerMsg(msg); !ok || !strings.Contains(regToast(m), "dev container") {
		t.Fatalf("offer toast = %q", regToast(m))
	}
	if again := m.devcontainerOfferCmd(); again != nil {
		t.Fatal("the offer shows once per session")
	}
	m.handleDevContainerMsg(devcontainerOfferClickMsg{root: root})
	if !m.confirmOpen || m.confirmKind != confirmKindDevContainerOffer {
		t.Fatalf("card %v kind %v", m.confirmOpen, m.confirmKind)
	}
	b := m.confirm.Buttons()
	if len(b) != 3 || b[0].ID != "cancel" || b[1].ID != "never" || b[2].ID != "open" {
		t.Fatalf("buttons = %+v", b)
	}
	m.confirmOpen = false
	m.handleConfirmSelect("open")
	if !m.confirmOpen || m.confirmKind != confirmKindDevContainerTrust {
		t.Fatalf("Open must lead to the trust card, kind %v", m.confirmKind)
	}
}

func TestDevContainerOfferNeverMutesTheFolder(t *testing.T) {
	m, _, root := dcProject(t, dcSample)
	m.handleDevContainerMsg(devcontainerOfferClickMsg{root: root})
	m.confirmOpen = false
	m.handleConfirmSelect("never")
	if m.confirmOpen || !devcontainer.OfferMuted(root) {
		t.Fatal("Never did not mute the folder")
	}
	m2 := newTestModel(t)
	m2.ws = m.ws
	if c := m2.devcontainerOfferCmd(); c != nil {
		if msg := c(); msg != nil {
			t.Fatalf("muted folder offered again: %#v", msg)
		}
	}
}

func TestDevContainerOfferNeedsTheFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m2 := newTestModel(t)
	ws, err := localhost.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m2.ws = ws
	if c := m2.devcontainerOfferCmd(); c != nil {
		if msg := c(); msg != nil {
			t.Fatalf("offer without a file: %#v", msg)
		}
	}
}
