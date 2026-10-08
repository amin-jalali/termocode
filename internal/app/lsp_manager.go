package app

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/activity"
	"termocode/internal/confirm"
	"termocode/internal/lspinstall"
	"termocode/internal/picker"
	"termocode/internal/toast"
)

// lspInstallState is the installer's slice of the app model. Held by
// pointer so the value-receiver Model copies share it; only mutated from
// Update.
type lspInstallState struct {
	installing map[string]bool // tool name → background install running
	asked      map[string]bool // filetypes already suggested (persisted)
	lastLang   string          // last filetype seen by the suggest check
	clients    []string        // LSP clients attached to the active buffer
	pending    string          // tool the open confirm dialog is about
	category   lspinstall.Category
}

func newLSPInstallState() *lspInstallState {
	return &lspInstallState{
		installing: map[string]bool{},
		asked:      lspinstall.LoadAsked(),
	}
}

// lspInstallEventMsg is one event from a background install: a progress
// line, or (done=true) the final result.
type lspInstallEventMsg struct {
	name   string
	line   string
	done   bool
	output string
	err    error
	ch     <-chan lspInstallEventMsg
}

// lspUninstallDoneMsg reports a background uninstall.
type lspUninstallDoneMsg struct {
	name string
	err  error
}

func waitLSPInstall(ch <-chan lspInstallEventMsg) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return ev
	}
}

// toolStateLabel is the picker glyph + hint for a tool.
func toolStateLabel(t lspinstall.Tool, st lspinstall.Status, installing bool) (glyph, hint string) {
	switch {
	case installing:
		return "…", "installing…"
	case st.State == lspinstall.Managed:
		v := st.Version
		if v == "" {
			v = "managed"
		} else {
			v = "managed · " + v
		}
		return "✓", v
	case st.State == lspinstall.System:
		return "✓", "on PATH"
	}
	return "✗", "missing · " + t.InstallKind.String()
}

// toolPickerItems builds the manager rows for one category. Pure apart
// from the status probe (injected for tests).
func toolPickerItems(cat lspinstall.Category, installing map[string]bool, status func(lspinstall.Tool) lspinstall.Status) []picker.Item {
	tools := lspinstall.ByCategory(cat)
	items := make([]picker.Item, 0, len(tools))
	for _, t := range tools {
		glyph, hint := toolStateLabel(t, status(t), installing[t.Name])
		items = append(items, picker.Item{
			ID:    t.Name,
			Title: glyph + " " + t.DisplayName(),
			Hint:  hint,
		})
	}
	return items
}

// openToolManager opens "LSP: Manage Language Servers…" (CategoryLSP) or
// "DAP: Install Adapter…" (CategoryDAP).
func (m *Model) openToolManager(cat lspinstall.Category) tea.Cmd {
	m.ensureLSPMgr()
	title := " Language Servers "
	if cat == lspinstall.CategoryDAP {
		title = " Debug Adapters "
	}
	m.lspMgr.category = cat
	// Snapshot: the loader runs off the UI goroutine.
	installing := make(map[string]bool, len(m.lspMgr.installing))
	for k, v := range m.lspMgr.installing {
		installing[k] = v
	}
	m.picker = picker.NewWith(title, func() []picker.Item {
		return toolPickerItems(cat, installing, lspinstall.StatusOf)
	})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindToolManager
	return m.picker.Init()
}

// onToolRowSelected opens the right confirm dialog for a manager row.
func (m *Model) onToolRowSelected(name string) tea.Cmd {
	m.ensureLSPMgr()
	t, ok := lspinstall.Lookup(name)
	if !ok {
		return nil
	}
	if m.lspMgr.installing[name] {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, t.Name+" is installing…", "progress shows in toasts")
		return cmd
	}
	dir, _ := lspinstall.ToolDir(t.Name)
	st := lspinstall.StatusOf(t)

	var title, body string
	var buttons []confirm.Button
	switch st.State {
	case lspinstall.Managed:
		title = t.Name + " is installed"
		body = "Managed copy in " + dir + "."
		buttons = []confirm.Button{
			{ID: "install", Title: "Reinstall", Style: confirm.StylePrimary},
			{ID: "uninstall", Title: "Uninstall", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		}
	default:
		plan, err := lspinstall.BuildPlan(t, dir, runtime.GOOS, runtime.GOARCH)
		if err == nil {
			err = lspinstall.CheckRequires(plan)
		}
		if err != nil {
			// Missing host toolchain / unsupported platform: say exactly
			// what to do instead of opening a dialog that would fail.
			return m.toolErrorToast(t, err, "")
		}
		title = "Install " + t.Name + "?"
		body = "Runs: " + plan.HumanCommand() + "\nInto: " + dir
		if st.State == lspinstall.System {
			title = t.Name + " found on PATH"
			body = "Using " + st.Path + ".\nInstall a managed copy? It takes precedence inside termocode.\n" + body
		}
		buttons = []confirm.Button{
			{ID: "install", Title: "Install", Style: confirm.StylePrimary},
			{ID: "cancel", Title: "Cancel"},
		}
	}
	m.lspMgr.pending = name
	m.confirm = confirm.New(title, body, buttons)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindToolInstall
	return nil
}

// onToolConfirm handles the manager's confirm buttons.
func (m *Model) onToolConfirm(id string) tea.Cmd {
	m.ensureLSPMgr()
	t, ok := lspinstall.Lookup(m.lspMgr.pending)
	m.lspMgr.pending = ""
	if !ok {
		return nil
	}
	switch id {
	case "install":
		return m.startToolInstall(t)
	case "uninstall":
		return func() tea.Msg {
			return lspUninstallDoneMsg{name: t.Name, err: lspinstall.Uninstall(t)}
		}
	}
	return nil
}

// startToolInstall runs the install in a goroutine and streams its events
// back as lspInstallEventMsg. Never blocks the UI.
func (m *Model) startToolInstall(t lspinstall.Tool) tea.Cmd {
	m.ensureLSPMgr()
	if m.lspMgr.installing[t.Name] {
		return nil
	}
	m.lspMgr.installing[t.Name] = true
	ch := make(chan lspInstallEventMsg, 32)
	go func() {
		defer close(ch)
		out, err := lspinstall.Install(context.Background(), t, func(line string) {
			if line == "done" {
				return
			}
			select {
			case ch <- lspInstallEventMsg{name: t.Name, line: line, ch: ch}:
			default: // drop progress when the UI lags; the result is never dropped
			}
		})
		ch <- lspInstallEventMsg{name: t.Name, done: true, output: out, err: err, ch: ch}
	}()
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Info, "Installing "+t.Name+"…", "runs in the background")
	return tea.Batch(cmd, waitLSPInstall(ch))
}

// onToolInstallEvent handles progress + completion of a background install.
func (m *Model) onToolInstallEvent(ev lspInstallEventMsg) tea.Cmd {
	m.ensureLSPMgr()
	if !ev.done {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Installing "+ev.name+"…", ev.line)
		return tea.Batch(cmd, waitLSPInstall(ev.ch))
	}
	delete(m.lspMgr.installing, ev.name)
	t, _ := lspinstall.Lookup(ev.name)
	if ev.err != nil {
		return m.toolErrorToast(t, ev.err, ev.output)
	}
	detail := "language server started for open files"
	if m.nvim != nil {
		if t.Category == lspinstall.CategoryDAP {
			detail = "debug adapter registered"
			_ = m.nvim.ExecLua(`if _G._termocode_dap_register_managed then _G._termocode_dap_register_managed() end`)
		} else {
			_ = m.nvim.ExecLua(`if _G._termocode_lsp_start then
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buflisted then pcall(_G._termocode_lsp_start, b) end
  end
end`)
		}
	}
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Info, "✓ "+t.Name+" installed", detail)
	return cmd
}

// toolErrorToast maps a classified install error to a toast. Generic
// failures also open the installer output in the preview overlay.
func (m *Model) toolErrorToast(t lspinstall.Tool, err error, output string) tea.Cmd {
	var mt *lspinstall.MissingToolchainError
	var off *lspinstall.OfflineError
	var un *lspinstall.UnsupportedError
	var cmd tea.Cmd
	switch {
	case errors.As(err, &mt):
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Can't install "+t.Name+": "+mt.Bin+" not found", mt.Hint)
	case errors.As(err, &off):
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Offline — can't install "+t.Name, "check your network connection and retry")
	case errors.As(err, &un):
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "No automatic install for "+t.Name, un.Error())
	default:
		m.toast, cmd = m.toast.PushDetail(toast.Errr, t.Name+" install failed", "output opened in preview")
		preview := installFailurePreview(t.Name, err, output)
		return tea.Batch(cmd, func() tea.Msg { return preview })
	}
	return cmd
}

// installFailurePreview is the preview overlay shown for a generic install
// failure: the error plus the captured installer output.
func installFailurePreview(name string, err error, output string) PreviewMsg {
	body := "Error: " + err.Error()
	if strings.TrimSpace(output) != "" {
		body += "\n\n" + output
	}
	return PreviewMsg{Title: fmt.Sprintf(" Install failed: %s ", name), Body: body}
}

// onToolUninstalled reports a finished uninstall.
func (m *Model) onToolUninstalled(msg lspUninstallDoneMsg) tea.Cmd {
	var cmd tea.Cmd
	if msg.err != nil {
		m.toast, cmd = m.toast.PushDetail(toast.Errr, msg.name+" uninstall failed", msg.err.Error())
		return cmd
	}
	m.toast, cmd = m.toast.PushDetail(toast.Info, msg.name+" removed", "running servers stop on restart")
	return cmd
}

// maybeSuggestLanguageServer fires the one-time "no language server for
// <ft>" toast the first time a filetype with a known but missing server
// is opened. The "asked" set is persisted so it never repeats.
func (m *Model) maybeSuggestLanguageServer(ft string) tea.Cmd {
	if m.lspMgr == nil || ft == m.lspMgr.lastLang {
		return nil
	}
	m.lspMgr.lastLang = ft
	t, ok := lspinstall.SuggestFor(ft, m.lspMgr.asked, lspinstall.Available)
	if !ok || m.lspMgr.installing[t.Name] {
		return nil
	}
	m.lspMgr.asked[ft] = true
	_ = lspinstall.SaveAsked(m.lspMgr.asked)
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Info,
		"No language server for "+t.Label,
		"Install "+t.Name+": palette → LSP: Manage Language Servers…")
	return cmd
}

// ensureLSPMgr lazily creates the installer state for models built
// without New (tests).
func (m *Model) ensureLSPMgr() {
	if m.lspMgr == nil {
		m.lspMgr = newLSPInstallState()
	}
}

// setLSPClients records the active buffer's attached clients (nil-safe for
// test models built without New).
func (m *Model) setLSPClients(names []string) {
	if m.lspMgr != nil {
		m.lspMgr.clients = names
	}
}

// lspClients is the status-bar chip content.
func (m Model) lspClients() []string {
	if m.lspMgr == nil {
		return nil
	}
	return m.lspMgr.clients
}

// statusBarRect mirrors View's placement of the status bar
// (it lives under the editor column only).
func (m Model) statusBarRect() (x, w int) {
	x = activity.Width
	w = m.w - activity.Width - editorScrollbarWidth - m.actionsColumnWidth()
	if m.showExp {
		x += m.explorerWidth
		w -= m.explorerWidth
	}
	if w < 1 {
		w = 1
	}
	return x, w
}

// hitStatusLSPChip reports whether a click at (x, y) lands on the
// status-bar LSP chip.
func (m Model) hitStatusLSPChip(x, y int) bool {
	if m.zenMode || y != m.h-1 {
		return false
	}
	bx, bw := m.statusBarRect()
	sb := m.status
	sb.SetWidth(bw)
	x0, x1, ok := sb.LSPChipSpan(m.statusState())
	return ok && x >= bx+x0 && x < bx+x1
}
