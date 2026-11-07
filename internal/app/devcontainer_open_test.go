package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/devcontainer"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// dcExec records the setup commands; fail names a command (by a part of
// it) that exits 1; exists says the container is there already.
type dcExec struct {
	mu     sync.Mutex
	calls  []string
	fail   string
	exists bool
}

func (x *dcExec) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	line := strings.Join(argv, " ")
	x.mu.Lock()
	x.calls = append(x.calls, line)
	x.mu.Unlock()
	if strings.Contains(line, " inspect ") {
		if x.exists {
			return "[]", "", 0, nil
		}
		return "", "Error: No such container", 1, nil
	}
	if x.fail != "" && strings.Contains(line, x.fail) {
		return "", "step one\nboom happened\n", 1, nil
	}
	return "", "", 0, nil
}
func (x *dcExec) CaptureInput(ctx context.Context, dir string, _ []byte, argv ...string) (string, string, int, error) {
	return x.Capture(ctx, dir, argv...)
}
func (x *dcExec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, func() {}, io.EOF
}

// dcProject is a test model on a folder with devcontainer.json src.
func dcProject(t *testing.T, src string) (*Model, *dcExec, string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".devcontainer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".devcontainer", "devcontainer.json"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	x := &dcExec{}
	old := devcontainerExec
	devcontainerExec = x
	t.Cleanup(func() { devcontainerExec = old })
	mm := newTestModel(t)
	ws, err := localhost.New(root)
	if err != nil {
		t.Fatal(err)
	}
	mm.ws = ws
	return &mm, x, root
}

// dcDrive runs the setup the way the update loop would: the inspect,
// then each step, until it ends.
func dcDrive(m *Model) {
	cmd := m.devcontainerInspectCmd()
	for i := 0; cmd != nil && i < 50; i++ {
		if _, ok := m.handleDevContainerMsg(cmd()); !ok {
			return
		}
		cmd = m.devcontainerStepCmd()
	}
}

const dcSample = `{"image": "golang:1.23", "initializeCommand": "echo local", "postCreateCommand": "go mod download", "containerEnv": {"TOKEN": "s3cr3t"}}`

func TestReopenInDevContainerCardListsCommandsCancelFirst(t *testing.T) {
	m, x, _ := dcProject(t, dcSample)
	m.openDevContainerReopen()
	if !m.confirmOpen || m.confirmKind != confirmKindDevContainerTrust {
		t.Fatalf("card open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || b[1].ID != "create" {
		t.Fatalf("buttons = %+v", b)
	}
	text := devcontainerCardText(m.devcontainerPending.cfg, m.devcontainerPending.setup, false)
	for _, want := range []string{"initializeCommand on this computer: echo local", "postCreateCommand on the new container: go mod download", "trust this project"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card text misses %q:\n%s", want, text)
		}
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if len(x.calls) != 0 || m.devcontainer != nil {
		t.Fatalf("Cancel ran %v", x.calls)
	}
}

func TestReopenInDevContainerCreatesRunsAndOpens(t *testing.T) {
	got := swapReexec(t)
	m, x, root := dcProject(t, dcSample)
	m.openDevContainerReopen()
	m.confirmOpen = false
	m.handleConfirmSelect("create")
	dcDrive(m)
	name := devcontainer.ContainerName(root)
	joined := strings.Join(x.calls, "\n")
	for _, want := range []string{"inspect --type container " + name, "sh -c echo local", "run -d --name " + name, "exec -w /workspaces/" + filepath.Base(root) + " " + name + " sh -c go mod download"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("calls miss %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "s3cr3t") {
		t.Fatal("a containerEnv value is in argv")
	}
	env, err := os.ReadFile(devcontainerEnvPath(name))
	if err != nil || string(env) != "TOKEN=s3cr3t\n" {
		t.Fatalf("env file %q %v", env, err)
	}
	if fi, _ := os.Stat(devcontainerEnvPath(name)); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Fatal("the env file must be 0600")
	}
	if len(*got) != 1 || strings.Join((*got)[0], " ") != "--container "+name+" /workspaces/"+filepath.Base(root) {
		t.Fatalf("re-exec = %v", *got)
	}
	if !devcontainer.Trusted(root, m.devcontainerPendingCfgForTest(t, root)) {
		t.Fatal("Create did not record the trust")
	}
}

func TestReopenInDevContainerExistingIsOnlyStarted(t *testing.T) {
	swapReexec(t)
	m, x, root := dcProject(t, dcSample)
	x.exists = true
	m.openDevContainerReopen()
	m.confirmOpen = false
	m.handleConfirmSelect("create")
	dcDrive(m)
	joined := strings.Join(x.calls, "\n")
	if !strings.Contains(joined, "start "+devcontainer.ContainerName(root)) || strings.Contains(joined, "echo local") || strings.Contains(joined, " exec ") {
		t.Fatalf("an existing container ran the file:\n%s", joined)
	}
}

func TestReopenInDevContainerStepFailureStops(t *testing.T) {
	got := swapReexec(t)
	m, x, _ := dcProject(t, dcSample)
	x.fail = "go mod download"
	m.openDevContainerReopen()
	m.confirmOpen = false
	m.handleConfirmSelect("create")
	dcDrive(m)
	if len(*got) != 0 || m.devcontainer != nil || !strings.Contains(regToast(m), "failed") {
		t.Fatalf("re-exec %v run %v toast %q", *got, m.devcontainer, regToast(m))
	}
}

func TestReopenInDevContainerWithoutFileOrWithCompose(t *testing.T) {
	m, _, root := dcProject(t, `{"dockerComposeFile": "c.yml", "service": "app"}`)
	m.openDevContainerReopen()
	if m.confirmOpen || !strings.Contains(regToast(m), "Cannot make this dev container") {
		t.Fatalf("compose: card %v toast %q", m.confirmOpen, regToast(m))
	}
	_ = os.RemoveAll(filepath.Join(root, ".devcontainer"))
	m.openDevContainerReopen()
	if !strings.Contains(regToast(m), "No devcontainer.json") {
		t.Fatalf("toast = %q", regToast(m))
	}
}

// devcontainerPendingCfgForTest parses the project's file again.
func (m *Model) devcontainerPendingCfgForTest(t *testing.T, root string) devcontainer.Config {
	t.Helper()
	src, err := os.ReadFile(devcontainer.Find(root))
	if err != nil {
		t.Fatal(err)
	}
	c, err := devcontainer.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
