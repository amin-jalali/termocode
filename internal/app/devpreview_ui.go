package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/devpreview"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Preview: the app the user or an agent is building (CMP-66) ──────────
//
// termocode is a terminal app, so the preview is the user's own browser:
// "Preview: Dev Server URLs…" lists every local address seen in the
// terminal tabs (a dev server an agent started in its lane prints one)
// and every running service's published port, checks each one (status
// and page title) and opens the picked one. Over SSH it copies the
// address and shows the port-forward command instead (we never open
// tunnels). Only loopback addresses are listed or probed.

// devPreviewPrefix marks the preview picker rows ("dvp:<url>").
const devPreviewPrefix = "dvp:"

// devPreviewScanLines is how many of the newest lines of each terminal
// tab are read for addresses.
const devPreviewScanLines = 600

// devPreviewMaxRows caps the list (and the probes run at once).
const devPreviewMaxRows = 20

// devPreviewMsg carries the probed targets back to Update.
type devPreviewMsg struct {
	targets []devpreview.Target
	checks  []devpreview.Check
	newest  bool // "open the newest that answers" instead of the list
}

// devPreviewProbeAll is swapped by tests (no real HTTP).
var devPreviewProbeAll = devpreview.ProbeAll

func devPreviewPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "preview-dev-servers", Title: "Preview: Dev Server URLs…", Lead: "preview"},
		{ID: "preview-open-newest", Title: "Preview: Open Newest Dev Server"},
	}
}

// dispatchDevPreviewPalette runs the "Preview: …" palette commands.
func (m *Model) dispatchDevPreviewPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "preview-dev-servers":
		return m.devPreviewScan(false), true
	case "preview-open-newest":
		return m.devPreviewScan(true), true
	}
	return nil, false
}

// devPreviewTermLua returns, per buffer, the newest n lines joined (""
// for a buffer that is gone).
const devPreviewTermLua = `local bufs, n = ...
local out = {}
for i, buf in ipairs(bufs) do
  if vim.api.nvim_buf_is_valid(buf) then
    local count = vim.api.nvim_buf_line_count(buf)
    out[i] = table.concat(vim.api.nvim_buf_get_lines(buf, math.max(count - n, 0), -1, false), "\n")
  else
    out[i] = ""
  end
end
return out`

// devPreviewTargets collects the addresses, oldest source first: the
// services, then the terminal tabs (open, else the hidden panel's).
func (m *Model) devPreviewTargets() []devpreview.Target {
	var all []devpreview.Target
	all = append(all, m.devPreviewServiceTargets()...)
	tabs, _, _ := m.aiTermTabs()
	if m.nvim != nil && len(tabs) > 0 {
		bufs := make([]int, len(tabs))
		for i, t := range tabs {
			bufs[i] = t.BufID
		}
		var texts []string
		if err := m.nvim.ExecLuaResult(devPreviewTermLua, &texts, bufs, devPreviewScanLines); err == nil {
			for i, text := range texts {
				if i < len(tabs) {
					all = append(all, devpreview.FromText(text, "terminal "+aiTermLabel(i, tabs[i].Name))...)
				}
			}
		}
	}
	out := devpreview.Merge(all)
	if len(out) > devPreviewMaxRows {
		out = out[:devPreviewMaxRows]
	}
	return out
}

// devPreviewServiceTargets are the published ports of the services that
// are up (compose and local).
func (m *Model) devPreviewServiceTargets() []devpreview.Target {
	st := m.services
	if st == nil {
		return nil
	}
	var out []devpreview.Target
	add := func(section string, s services.Service) {
		ss := st.statusOf(section, s.Name)
		if !ss.State.Up() {
			return
		}
		list := ss.Ports
		if len(list) == 0 && section == svcSecLocal {
			list = s.Ports
		}
		for _, p := range list {
			if t, ok := devpreview.FromPort(p.Published, "service "+s.Name); ok {
				out = append(out, t)
			}
		}
	}
	for _, s := range st.compose {
		add(svcSecCompose, s)
	}
	for _, s := range st.local {
		add(svcSecLocal, s)
	}
	return out
}

// devPreviewScan finds the targets now (Update) and probes them in the
// background.
func (m *Model) devPreviewScan(newest bool) tea.Cmd {
	targets := m.devPreviewTargets()
	if len(targets) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "No dev server found",
			"Start one in a terminal tab (for example npm run dev) or as a service; termocode looks for http://localhost:PORT in the output.")
		return c
	}
	probe := devPreviewProbeAll
	return func() tea.Msg {
		return devPreviewMsg{targets: targets, checks: probe(context.Background(), targets), newest: newest}
	}
}

// handleDevPreviewMsg routes the preview messages. ok=false: not ours.
func (m *Model) handleDevPreviewMsg(msg tea.Msg) (tea.Cmd, bool) {
	dm, ok := msg.(devPreviewMsg)
	if !ok {
		return nil, false
	}
	if dm.newest {
		for i, t := range dm.targets {
			if i < len(dm.checks) && dm.checks[i].Up {
				return m.devPreviewOpen(t.URL), true
			}
		}
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "No dev server answers",
			fmt.Sprintf("%d address(es) seen, none answers now; see Preview: Dev Server URLs…", len(dm.targets)))
		return c, true
	}
	items := make([]picker.Item, 0, len(dm.targets))
	for i, t := range dm.targets {
		var chk devpreview.Check
		if i < len(dm.checks) {
			chk = dm.checks[i]
		}
		items = append(items, picker.Item{ID: devPreviewPrefix + t.URL, Title: t.URL, Hint: chk.Hint() + " · " + t.From})
	}
	m.openTasksPickerItems(" Dev Servers ", items)
	return nil, true
}

// devPreviewOpen opens url in the user's browser; over SSH it copies it
// and shows the port-forward command.
func (m *Model) devPreviewOpen(url string) tea.Cmd {
	var c tea.Cmd
	if mcpOverSSH() {
		m.copyToClipboard(url)
		detail := "over SSH, forward the port first"
		if _, port, ok := devpreview.Normalize(url); ok {
			detail = fmt.Sprintf("over SSH: ssh -L %d:localhost:%d %s", port, port, mcpSSHHost())
		}
		m.toast, c = m.toast.PushDetail(toast.Info, "Copied "+url, detail)
		return c
	}
	if err := osOpen(url); err != nil {
		m.toast, c = m.toast.PushDetail(toast.Errr, "Open failed", err.Error())
		return c
	}
	m.toast, c = m.toast.PushDetail(toast.Info, "Opened", url)
	return c
}

// handleDevPreviewPick opens a picked "dvp:<url>" row.
func (m *Model) handleDevPreviewPick(id string) tea.Cmd {
	u, _, ok := devpreview.Normalize(strings.TrimPrefix(id, devPreviewPrefix))
	if !ok { // the list only holds local addresses; never open anything else
		return nil
	}
	return m.devPreviewOpen(u)
}
