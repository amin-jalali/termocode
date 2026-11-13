package app

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// RC-8 (plan RC, phase 6): the debugger of a container workspace runs in
// a tools sidecar (containerhost/sidecar.go): a second container from
// the target's image with termocode's tools volume, sharing its files,
// network and processes. nvim-dap starts dlv there behind tc-dap-bridge
// through `docker exec -i`. The user starts it on a card; it is removed
// on Stop and when termocode quits. Nothing is put in the target.

const confirmKindSidecarStart confirmKindEnum = 7733 // Debug: Start Tools Sidecar…

// toolsSidecar is a running sidecar (*containerhost.Sidecar).
type toolsSidecar interface {
	ExecArgv(argv ...string) []string
	Stop(ctx context.Context) error
}

// startToolsSidecar starts the sidecar of ws (tests swap it).
var startToolsSidecar = func(ctx context.Context, ws workspace.Workspace) (toolsSidecar, error) {
	h, ok := ws.Host.(*containerhost.Host)
	if !ok {
		return nil, errors.New("not a container workspace")
	}
	sc, err := h.StartSidecar(ctx, workspace.Key(ws.ID), h.ContainerEnv())
	if err != nil {
		return nil, err
	}
	return sc, nil
}

// sidecarTimeout bounds a start (the tools volume check, the sweep and
// the run) and a stop.
var sidecarTimeout = 60 * time.Second

type sidecarStartedMsg struct {
	sc  toolsSidecar
	err error
}

type sidecarStoppedMsg struct{ err error }

func sidecarPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "debug-sidecar-start", Title: "Debug: Start Tools Sidecar…", Hint: "debug a folder opened in a container"},
		{ID: "debug-sidecar-stop", Title: "Debug: Stop Tools Sidecar"},
	}
}

// dispatchSidecarPalette runs the sidecar palette ids.
func (m *Model) dispatchSidecarPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "debug-sidecar-start":
		return m.openSidecarStartCard(), true
	case "debug-sidecar-stop":
		return m.stopSidecarCmd(), true
	}
	return nil, false
}

// inContainerWorkspace reports whether the Model's workspace is a folder
// in a container.
func (m Model) inContainerWorkspace() bool {
	ws := m.workspace()
	return ws.Kind == workspace.KindContainer && ws.Host != nil
}

// openSidecarStartCard asks before a sidecar starts.
func (m *Model) openSidecarStartCard() tea.Cmd {
	if !m.inContainerWorkspace() {
		return m.pushToastDetail(toast.Info, "Only for a folder in a container",
			"Open one with Workspace: Open Folder in Container…")
	}
	if m.sidecar != nil {
		return m.pushToast(toast.Info, "The tools sidecar is running")
	}
	label := m.workspaceHostLabel()
	m.confirm = confirm.Risky("Start the tools sidecar?",
		"To debug, termocode runs a second container next to "+label+", from "+label+"'s own image, "+
			"sharing its files, network and processes, with termocode's debugger tools read-only. "+
			"Nothing is changed in "+label+". The sidecar is removed when termocode quits, or earlier with Debug: Stop Tools Sidecar.",
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "start", Title: "Start", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindSidecarStart
	return nil
}

// answerSidecarStart is the card's answer (Cancel and Esc start nothing).
func (m *Model) answerSidecarStart(id string) tea.Cmd {
	m.confirmOpen = false
	if id != "start" || m.sidecar != nil || m.sidecarStarting {
		return nil
	}
	m.sidecarStarting = true
	ws := m.workspace()
	return tea.Batch(m.pushToast(toast.Info, "Starting the tools sidecar…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sidecarTimeout)
		defer cancel()
		sc, err := startToolsSidecar(ctx, ws)
		return sidecarStartedMsg{sc: sc, err: err}
	})
}

// sidecarDebugArgv is the debug adapter's command: dlv in DAP mode on
// the sidecar's loopback, relayed to stdin/stdout by tc-dap-bridge.
func (m Model) sidecarDebugArgv() []string {
	if m.sidecar == nil {
		return nil
	}
	return m.sidecar.ExecArgv(containerhost.SidecarTools+"/bin/tc-dap-bridge", "--",
		containerhost.SidecarTools+"/bin/dlv", "dap", "-l", "127.0.0.1:0")
}

// sidecarDapLua points nvim-dap's Go adapter at the sidecar (arg: the
// argv) and adds the Go configs; with no argv it does nothing.
const sidecarDapLua = `
local argv = ...
local ok, dap = pcall(require, 'dap')
if not ok or type(argv) ~= 'table' or #argv == 0 then return end
local args = {}
for i = 2, #argv do args[#args + 1] = argv[i] end
dap.adapters.delve = { type = 'executable', command = argv[1], args = args }
local function add_config(ft, cfg)
  dap.configurations[ft] = dap.configurations[ft] or {}
  for _, c in ipairs(dap.configurations[ft]) do
    if c.name == cfg.name then return end
  end
  table.insert(dap.configurations[ft], cfg)
end
add_config('go', { type = 'delve', name = 'Debug current file', request = 'launch', program = '${file}' })
add_config('go', { type = 'delve', name = 'Debug package', request = 'launch', program = '${fileDirname}' })
vim.g.termocode_dap_sidecar = true
`

// handleSidecarMsg routes the sidecar messages. ok=false: not ours.
func (m *Model) handleSidecarMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case sidecarStartedMsg:
		m.sidecarStarting = false
		if msg.err != nil {
			recordError("[sidecar] " + msg.err.Error())
			detail := remoteErrText(m.workspaceHostLabel(), msg.err)
			if errors.Is(msg.err, containerhost.ErrNoToolsVolume) {
				detail = "termocode's debugger tools are not on this engine yet. Build them once with " +
					"scripts/sidecar-tools/build.sh (see the Containers guide), then start again."
			}
			return m.pushToastDetail(toast.Errr, "Tools sidecar not started", detail), true
		}
		m.sidecar = msg.sc
		if m.nvim != nil {
			if err := m.nvim.ExecLuaArgs(sidecarDapLua, m.sidecarDebugArgv()); err != nil {
				recordError("[sidecar] dap adapter: " + err.Error())
			}
		}
		return m.pushToastDetail(toast.Info, "Tools sidecar running",
			"F5 now debugs Go in "+m.workspaceHostLabel()+" through it."), true
	case sidecarStoppedMsg:
		if msg.err != nil {
			return m.pushToastDetail(toast.Warn, "Tools sidecar not removed", msg.err.Error()), true
		}
		return m.pushToast(toast.Info, "Tools sidecar removed"), true
	}
	return nil, false
}

// stopSidecarCmd removes the running sidecar off the UI thread.
func (m *Model) stopSidecarCmd() tea.Cmd {
	sc := m.sidecar
	if sc == nil {
		return m.pushToast(toast.Info, "No tools sidecar is running")
	}
	m.sidecar = nil
	m.resetSidecarDap()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sidecarTimeout)
		defer cancel()
		return sidecarStoppedMsg{err: sc.Stop(ctx)}
	}
}

// stopSidecarNow removes the sidecar while termocode quits (Close).
func (m *Model) stopSidecarNow() {
	sc := m.sidecar
	if sc == nil {
		return
	}
	m.sidecar = nil
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sc.Stop(ctx); err != nil {
		recordError("[sidecar] not removed on quit: " + err.Error())
	}
}

// resetSidecarDap drops the sidecar adapter, so F5 offers the card again.
func (m *Model) resetSidecarDap() {
	if m.nvim != nil {
		_ = m.nvim.ExecLua(`local ok, dap = pcall(require, 'dap'); if ok and vim.g.termocode_dap_sidecar then dap.adapters.delve = nil; vim.g.termocode_dap_sidecar = nil end`)
	}
}
