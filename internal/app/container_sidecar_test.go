package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// fakeSidecar records what the app asks of a running tools sidecar.
type fakeSidecar struct{ stops int }

func (f *fakeSidecar) ExecArgv(argv ...string) []string {
	return append([]string{"docker", "exec", "-i", "termocode-sc-k"}, argv...)
}
func (f *fakeSidecar) Stop(context.Context) error { f.stops++; return nil }

// swapSidecarStart replaces the real start; it returns sc or err.
func swapSidecarStart(t *testing.T, sc *fakeSidecar, err error) *[]workspace.Workspace {
	t.Helper()
	var asked []workspace.Workspace
	old := startToolsSidecar
	startToolsSidecar = func(_ context.Context, ws workspace.Workspace) (toolsSidecar, error) {
		asked = append(asked, ws)
		if err != nil {
			return nil, err
		}
		return sc, nil
	}
	t.Cleanup(func() { startToolsSidecar = old })
	return &asked
}

func runSidecarCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			if c != nil {
				m.handleSidecarMsg(c())
			}
		}
		return
	}
	m.handleSidecarMsg(msg)
}

func TestSidecarPaletteIDs(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range sidecarPaletteItems() {
		ids[it.ID] = true
		if _, ok := aiActionPolicies[it.ID]; !ok {
			t.Errorf("%s has no AI policy", it.ID)
		}
	}
	for _, id := range []string{"debug-sidecar-start", "debug-sidecar-stop"} {
		if !ids[id] {
			t.Errorf("palette has no %s", id)
		}
	}
}

func TestSidecarOnlyForAContainerWorkspace(t *testing.T) {
	m := newTestModel(t)
	cmd, ok := m.dispatchSidecarPalette("debug-sidecar-start")
	if !ok || m.confirmOpen {
		t.Fatalf("ok %v, card %v", ok, m.confirmOpen)
	}
	_ = cmd
	if !strings.Contains(regToast(&m), "Only for a folder in a container") {
		t.Fatalf("toast %q", regToast(&m))
	}
}

// RC-8: Debug: Start Tools Sidecar asks first on a Cancel-first card
// that says what runs; Start runs it, and the debugger of the workspace
// then goes through it (dlv behind tc-dap-bridge in the sidecar).
func TestSidecarStartAsksThenRunsAndRoutesTheDebugger(t *testing.T) {
	m, _, _, _ := newContainerModel(t)
	sc := &fakeSidecar{}
	asked := swapSidecarStart(t, sc, nil)
	m.dispatchSidecarPalette("debug-sidecar-start")
	if !m.confirmOpen || m.confirmKind != confirmKindSidecarStart || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	if v := m.confirm.Title() + " " + m.confirm.Message(); !strings.Contains(v, "api-1") || !strings.Contains(v, "removed when termocode quits") {
		t.Errorf("card text %q", v)
	}
	if cmd := m.answerSidecarStart("cancel"); cmd != nil || len(*asked) != 0 {
		t.Fatal("Cancel started something")
	}
	m.dispatchSidecarPalette("debug-sidecar-start")
	runSidecarCmd(t, &m, m.answerSidecarStart("start"))
	if len(*asked) != 1 || m.sidecar == nil {
		t.Fatalf("started %d, sidecar %v", len(*asked), m.sidecar)
	}
	argv := m.sidecarDebugArgv()
	if got := strings.Join(argv, " "); got != "docker exec -i termocode-sc-k /opt/termocode/bin/tc-dap-bridge -- /opt/termocode/bin/dlv dap -l 127.0.0.1:0" {
		t.Fatalf("debug argv %q", got)
	}
	lua := sidecarDapLua
	for _, want := range []string{"type = 'executable'", "dap.adapters.delve", "add_config"} {
		if !strings.Contains(lua, want) {
			t.Errorf("sidecarDapLua has no %s", want)
		}
	}
	// A second start while one runs says so and starts nothing.
	m.dispatchSidecarPalette("debug-sidecar-start")
	if m.confirmOpen || len(*asked) != 1 {
		t.Fatal("a second sidecar was offered")
	}
}

// A failed start says why in one line; a missing tools volume says how
// to build it.
func TestSidecarStartFailureSaysHowToBuildTheTools(t *testing.T) {
	m, _, _, _ := newContainerModel(t)
	swapSidecarStart(t, nil, fmt.Errorf("%w (termocode-tools-amd64-musl): no such volume", containerhost.ErrNoToolsVolume))
	m.dispatchSidecarPalette("debug-sidecar-start")
	runSidecarCmd(t, &m, m.answerSidecarStart("start"))
	if m.sidecar != nil {
		t.Fatal("a failed start left a sidecar")
	}
	if got := regToast(&m); !strings.Contains(got, "scripts/sidecar-tools/build.sh") {
		t.Fatalf("toast %q", got)
	}
	swapSidecarStart(t, nil, errors.New("boom"))
	m.dispatchSidecarPalette("debug-sidecar-start")
	runSidecarCmd(t, &m, m.answerSidecarStart("start"))
	if got := regToast(&m); !strings.Contains(got, "boom") {
		t.Fatalf("toast %q", got)
	}
}

// Stop removes it; quitting termocode removes it too.
func TestSidecarStopAndQuitRemoveIt(t *testing.T) {
	m, _, _, _ := newContainerModel(t)
	sc := &fakeSidecar{}
	swapSidecarStart(t, sc, nil)
	m.dispatchSidecarPalette("debug-sidecar-start")
	runSidecarCmd(t, &m, m.answerSidecarStart("start"))
	cmd, _ := m.dispatchSidecarPalette("debug-sidecar-stop")
	runSidecarCmd(t, &m, cmd)
	if sc.stops != 1 || m.sidecar != nil {
		t.Fatalf("stops %d, sidecar %v", sc.stops, m.sidecar)
	}
	m.dispatchSidecarPalette("debug-sidecar-start")
	runSidecarCmd(t, &m, m.answerSidecarStart("start"))
	m.stopSidecarNow()
	if sc.stops != 2 || m.sidecar != nil {
		t.Fatalf("quit: stops %d", sc.stops)
	}
}

// Starting the debugger in a container workspace without a sidecar
// offers to start one instead of a local adapter on container paths.
func TestDebugStartInAContainerOffersTheSidecar(t *testing.T) {
	m, _, _, _ := newContainerModel(t)
	swapSidecarStart(t, &fakeSidecar{}, nil)
	m.dapStart()
	if !m.confirmOpen || m.confirmKind != confirmKindSidecarStart {
		t.Fatalf("card open %v kind %v", m.confirmOpen, m.confirmKind)
	}
}
