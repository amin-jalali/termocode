package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/lspmirror"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// Plan RC, phase 5: language servers for a folder opened in a container.
// Order: the server in the image (`exec -i`, same paths) → a local
// server through `termocode lsp-proxy`, which reads the bind mount or a
// read-only local mirror of the code → none. Owner decision Q5: the
// server runs in the container when it can, and a local server's
// diagnostics are muted in a container workspace (its view of the
// dependencies is not the container's); completion and navigation stay
// on. "LSP: Toggle Local Diagnostics…" shows them. Nothing is installed
// or written in the container.

const (
	pickerKindContainerLSPWhere     pickerKindEnum  = 7731 // LSP: Choose Where the Language Server Runs…
	confirmKindContainerMirrorClear confirmKindEnum = 7732 // LSP: Clear Container Mirror…
)

// Setting values of container_lsp ("" = auto).
const (
	containerLSPAuto  = "auto"  // in the image, else local
	containerLSPImage = "image" // only in the image
	containerLSPLocal = "local" // only local (mount or mirror)
	containerLSPOff   = "off"
)

// lspPlace is where one server of a container workspace runs.
type lspPlace string

const (
	lspInImage lspPlace = "image"
	lspLocal   lspPlace = "local"
)

// containerLSPSettings are the two config.json keys (file + palette).
type containerLSPSettings struct {
	// Where: container_lsp, auto|image|local|off ("" = auto).
	Where string `json:"container_lsp,omitempty"`
	// LocalDiagnostics: container_local_lsp_diagnostics, on|off ("" =
	// off: owner decision Q5, a local server's diagnostics are muted).
	LocalDiagnostics string `json:"container_local_lsp_diagnostics,omitempty"`
}

func (s containerLSPSettings) where() string {
	switch s.Where {
	case containerLSPImage, containerLSPLocal, containerLSPOff:
		return s.Where
	}
	return containerLSPAuto
}

func (s containerLSPSettings) showLocalDiagnostics() bool { return s.LocalDiagnostics == "on" }

// loadContainerLSPSettings reads the keys from config.json (missing or
// broken = defaults).
func loadContainerLSPSettings() containerLSPSettings {
	var s containerLSPSettings
	p, err := settingsConfigPath()
	if err != nil {
		return s
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

// saveContainerLSPSetting writes one key ("" removes it); other keys stay.
func saveContainerLSPSetting(key, val string) error {
	p, err := settingsConfigPath()
	if err != nil {
		return err
	}
	return cfgfile.Update(p, func(m map[string]any) error {
		if val == "" {
			delete(m, key)
		} else {
			m[key] = val
		}
		return nil
	})
}

// lspServerSpec is one language server of the registry.
type lspServerSpec struct {
	Name string
	Cmd  []string
}

// lspServerSpecs are the registry's language servers (the table
// lspSetupLua starts from).
func lspServerSpecs() []lspServerSpec {
	var out []lspServerSpec
	for _, t := range lspinstall.ByCategory(lspinstall.CategoryLSP) {
		if len(t.Cmd) == 0 {
			continue
		}
		out = append(out, lspServerSpec{Name: t.Name, Cmd: append([]string(nil), t.Cmd...)})
	}
	return out
}

// lspServerBins are the distinct executables of specs, sorted.
func lspServerBins(specs []lspServerSpec) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range specs {
		if b := s.Cmd[0]; !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	sort.Strings(out)
	return out
}

// lspServerPlan is how one server starts.
type lspServerPlan struct {
	Place lspPlace `json:"place"`
	Cmd   []string `json:"cmd"`
	// Mute drops the server's diagnostics (a local server, Q5).
	Mute bool `json:"mute,omitempty"`
}

// containerLSPPlan is what the Lua side starts servers from
// (_G._termocode_lsp_container).
type containerLSPPlan struct {
	Root    string                   `json:"root"`
	Label   string                   `json:"label"`
	Servers map[string]lspServerPlan `json:"servers"`
	// Gen counts the plans: a client is reused only within one plan, so
	// a client the new plan stopped is never picked up again.
	Gen int `json:"gen"`
}

// containerLSPInput is everything planContainerLSP decides from.
type containerLSPInput struct {
	Settings containerLSPSettings
	Root     string // the workspace root inside the container
	Label    string
	// InImage: executable → its absolute path in the image.
	InImage map[string]string
	// LocalDir is a local folder with the same files as Root (the bind
	// mount or the mirror); "" = none.
	LocalDir string
	// LocalCmd finds a server on this machine.
	LocalCmd func(bin string) (string, bool)
	// ImageArgv wraps argv to run in the container (Host.LSPArgv).
	ImageArgv func(argv []string) []string
	// Proxy is the termocode executable (`termocode lsp-proxy`).
	Proxy string
}

// planContainerLSP decides, per server, where it runs.
func planContainerLSP(in containerLSPInput, specs []lspServerSpec) containerLSPPlan {
	p := containerLSPPlan{Root: in.Root, Label: in.Label, Servers: map[string]lspServerPlan{}}
	where := in.Settings.where()
	if where == containerLSPOff {
		return p
	}
	for _, s := range specs {
		bin, args := s.Cmd[0], s.Cmd[1:]
		if where != containerLSPLocal && in.ImageArgv != nil {
			if abs := in.InImage[bin]; abs != "" {
				argv := append([]string{abs}, args...)
				p.Servers[s.Name] = lspServerPlan{Place: lspInImage, Cmd: in.ImageArgv(argv)}
				continue
			}
		}
		if where == containerLSPImage || in.LocalDir == "" || in.Proxy == "" || in.LocalCmd == nil {
			continue
		}
		local, ok := in.LocalCmd(bin)
		if !ok {
			continue
		}
		argv := []string{in.Proxy, "lsp-proxy", "--map", in.Root + "=" + in.LocalDir, "--", local}
		p.Servers[s.Name] = lspServerPlan{
			Place: lspLocal,
			Cmd:   append(argv, args...),
			Mute:  !in.Settings.showLocalDiagnostics(),
		}
	}
	return p
}

// needsMirror reports whether a local server could run if the code were
// mirrored: the workspace has no bind mount, the setting allows local
// servers, and some server is missing in the image but found here.
func needsMirror(in containerLSPInput, specs []lspServerSpec) bool {
	where := in.Settings.where()
	if in.LocalDir != "" || where == containerLSPOff || where == containerLSPImage || in.LocalCmd == nil {
		return false
	}
	for _, s := range specs {
		bin := s.Cmd[0]
		if where == containerLSPAuto && in.InImage[bin] != "" {
			continue
		}
		if _, ok := in.LocalCmd(bin); ok {
			return true
		}
	}
	return false
}

// localLSPCmd finds bin like lspSetupLua's resolve_cmd: PATH, then the
// managed tools/bin.
func localLSPCmd(bin string) (string, bool) {
	if p, err := exec.LookPath(bin); err == nil {
		return p, true
	}
	if dir, err := lspinstall.BinDir(); err == nil && dir != "" {
		p := filepath.Join(dir, bin)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// chipLabel is the status chip text of one attached client:
// "gopls · in api-1", "gopls · local".
func (p containerLSPPlan) chipLabel(name string) string {
	s, ok := p.Servers[name]
	if !ok {
		return name
	}
	if s.Place == lspInImage {
		return name + " · in " + p.Label
	}
	return name + " · local"
}

// ─── the Model side ───────────────────────────────────────────────────

// containerLSPHost is what phase 5 needs from a container host
// (containerhost.Host).
type containerLSPHost interface {
	FindCommands(ctx context.Context, names []string) (map[string]string, error)
	LSPArgv(argv []string) []string
	TreeTar(ctx context.Context) (io.ReadCloser, func(), error)
	Caps() workspace.Caps
	Label() string
}

// containerLSPState is the Model's phase-5 state (nil = local workspace
// or a test Model).
type containerLSPState struct {
	probed   bool
	inImage  map[string]string
	probeErr error
	mirror   *lspmirror.Mirror
	// mirrored: the mirror holds a fill (LocalDir may use it).
	mirrored bool
	filling  bool
	plan     containerLSPPlan
}

// containerMirrorBase is the folder of all mirrors (tests point it to
// a temp folder).
var containerMirrorBase = lspmirror.BaseDir

// activeLSPMirror is the mirror saves update (remote_buffers.go writes
// run on the RPC goroutine, away from the Model).
var activeLSPMirror atomic.Pointer[lspmirror.Mirror]

// containerLSPTimeout bounds the probe; containerMirrorTimeout the fill.
var (
	containerLSPTimeout    = 20 * time.Second
	containerMirrorTimeout = 3 * time.Minute
)

type containerLSPProbedMsg struct {
	found map[string]string
	err   error
}

type containerMirrorFilledMsg struct {
	stats lspmirror.Stats
	err   error
}

// containerLSPLua installs a plan (a JSON string) or marks it pending
// ("pending": servers of a container workspace wait for the plan, so no
// local server is started on container paths). A new plan stops the
// clients of the old one and starts servers for the open buffers.
const containerLSPLua = `
local spec = ...
if spec == 'pending' then
  if _G._termocode_lsp_container == nil then _G._termocode_lsp_container = 'pending' end
  return
end
local ok, t = pcall(vim.json.decode, spec)
if not ok or type(t) ~= 'table' then return end
local get = vim.lsp.get_clients or vim.lsp.get_active_clients
for _, c in ipairs(get()) do
  if c.config and c.config._termocode_where then pcall(c.stop, c) end
end
_G._termocode_lsp_container = t
if type(_G._termocode_lsp_start) ~= 'function' then return end
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == '' and vim.api.nvim_buf_get_name(b) ~= '' then
    pcall(_G._termocode_lsp_start, b)
  end
end
`

// containerLSPHostOf is the container host of m's workspace.
func (m Model) containerLSPHostOf() (containerLSPHost, bool) {
	ws := m.workspace()
	if ws.Kind != workspace.KindContainer || ws.Host == nil {
		return nil, false
	}
	h, ok := ws.Host.(containerLSPHost)
	return h, ok
}

// containerLSPPending marks the plan pending through run (nvim's
// ExecLuaArgs, at attach) for a container workspace.
func (m Model) containerLSPPending(run func(code string, args ...any) error) {
	if _, ok := m.containerLSPHostOf(); !ok {
		return
	}
	_ = run(containerLSPLua, "pending")
}

// containerLSPInitCmd probes the image for language servers (nil for a
// local workspace).
func (m Model) containerLSPInitCmd() tea.Cmd {
	h, ok := m.containerLSPHostOf()
	if !ok || m.containerLSP == nil {
		return nil
	}
	ws := m.workspace()
	if m.containerLSP.mirror == nil {
		if base, err := containerMirrorBase(); err == nil {
			m.containerLSP.mirror = lspmirror.New(base, workspace.Key(ws.ID), ws.Root)
		}
	}
	bins := lspServerBins(lspServerSpecs())
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), containerLSPTimeout)
		defer cancel()
		found, err := h.FindCommands(ctx, bins)
		return containerLSPProbedMsg{found: found, err: err}
	}
}

// containerLSPInput is the planner's input for the Model's workspace now.
func (m Model) containerLSPInputNow() (containerLSPInput, bool) {
	h, ok := m.containerLSPHostOf()
	st := m.containerLSP
	if !ok || st == nil {
		return containerLSPInput{}, false
	}
	ws := m.workspace()
	in := containerLSPInput{
		Settings:  loadContainerLSPSettings(),
		Root:      ws.Root,
		Label:     h.Label(),
		InImage:   st.inImage,
		LocalCmd:  localLSPCmd,
		ImageArgv: h.LSPArgv,
	}
	if exe, err := spawn.Executable(); err == nil {
		in.Proxy = exe
	}
	switch {
	case h.Caps().MountOf != "":
		in.LocalDir = h.Caps().MountOf
	case st.mirrored && st.mirror != nil:
		in.LocalDir = st.mirror.Dir
	}
	return in, true
}

// applyContainerLSP plans again and hands the plan to nvim. With
// fill=true a mirror that would help is filled first (one tar stream).
func (m *Model) applyContainerLSP(fill bool) tea.Cmd {
	in, ok := m.containerLSPInputNow()
	if !ok {
		return nil
	}
	specs := lspServerSpecs()
	st := m.containerLSP
	gen := st.plan.Gen + 1
	st.plan = planContainerLSP(in, specs)
	st.plan.Gen = gen
	if m.nvim != nil {
		if b, err := json.Marshal(st.plan); err == nil {
			_ = m.nvim.ExecLuaArgs(containerLSPLua, string(b))
		}
	}
	if fill && !st.mirrored && !st.filling && st.mirror != nil && needsMirror(in, specs) {
		return m.fillContainerMirrorCmd()
	}
	return nil
}

// fillContainerMirrorCmd copies the workspace code into the mirror.
func (m *Model) fillContainerMirrorCmd() tea.Cmd {
	h, ok := m.containerLSPHostOf()
	st := m.containerLSP
	if !ok || st == nil || st.mirror == nil || st.filling {
		return nil
	}
	st.filling = true
	mr := st.mirror
	keep := mr.Dir
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), containerMirrorTimeout)
		defer cancel()
		r, stop, err := h.TreeTar(ctx)
		if err != nil {
			return containerMirrorFilledMsg{err: err}
		}
		defer stop()
		stats, err := mr.Fill(r)
		if err == nil || errors.Is(err, lspmirror.ErrCap) {
			_ = lspmirror.Prune(filepath.Dir(keep), lspmirror.DefaultTotalCap, keep)
		}
		return containerMirrorFilledMsg{stats: stats, err: err}
	}
}

// handleContainerLSPMsg routes the phase-5 messages. ok=false: not ours.
func (m *Model) handleContainerLSPMsg(msg tea.Msg) (tea.Cmd, bool) {
	st := m.containerLSP
	switch msg := msg.(type) {
	case containerLSPProbedMsg:
		if st == nil {
			return nil, true
		}
		st.probed, st.inImage, st.probeErr = true, msg.found, msg.err
		cmd := m.applyContainerLSP(true)
		if msg.err != nil {
			return tea.Batch(cmd, m.pushToastDetail(toast.Warn, "Language servers in "+m.workspaceHostLabel()+" not checked",
				remoteErrText(m.workspaceHostLabel(), msg.err))), true
		}
		return cmd, true
	case containerMirrorFilledMsg:
		if st == nil {
			return nil, true
		}
		st.filling = false
		label := m.workspaceHostLabel()
		switch {
		case msg.err == nil:
			st.mirrored = true
			activeLSPMirror.Store(st.mirror)
			cmd := m.applyContainerLSP(false)
			return tea.Batch(cmd, m.pushToastDetail(toast.Info, "Local language server ready",
				"Copied "+strconv.Itoa(msg.stats.Files)+" files of "+label+" into a read-only local mirror.")), true
		case errors.Is(msg.err, lspmirror.ErrCap):
			st.mirrored = true
			activeLSPMirror.Store(st.mirror)
			cmd := m.applyContainerLSP(false)
			return tea.Batch(cmd, m.pushToastDetail(toast.Warn, "Mirror is only partial",
				"The code of "+label+" is larger than 512 MB; the local language server sees part of it.")), true
		}
		return m.pushToastDetail(toast.Warn, "No local language server", "Could not copy the code of "+label+": "+remoteErrText(label, msg.err)), true
	}
	return nil, false
}

// lspChipClients is the status chip list: in a container workspace each
// client says where it runs.
func (m Model) lspChipClients() []string {
	names := m.lspClients()
	st := m.containerLSP
	if st == nil || len(st.plan.Servers) == 0 || len(names) == 0 {
		return names
	}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = st.plan.chipLabel(n)
	}
	return out
}

// noteMirrorWrite updates the mirror after a save of p (a container
// path). Errors only cost freshness, never the save.
func noteMirrorWrite(p string, data []byte) {
	if mr := activeLSPMirror.Load(); mr != nil {
		_ = mr.Put(p, data)
	}
}

// noteMirrorRename and noteMirrorRemove follow the explorer's renames,
// moves and deletes in the container (remote_file_ops.go).
func noteMirrorRename(from, to string) {
	if mr := activeLSPMirror.Load(); mr != nil {
		_ = mr.Rename(from, to)
	}
}

func noteMirrorRemove(p string) {
	if mr := activeLSPMirror.Load(); mr != nil {
		_ = mr.Remove(p)
	}
}

// ─── palette ──────────────────────────────────────────────────────────

func containerLSPPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "lsp-container-where", Title: "LSP: Choose Where the Language Server Runs…"},
		{ID: "lsp-container-diagnostics", Title: "LSP: Toggle Local Diagnostics in Container Workspace"},
		{ID: "lsp-mirror-refresh", Title: "LSP: Refresh Container Mirror"},
		{ID: "lsp-mirror-clear", Title: "LSP: Clear Container Mirror…"},
	}
}

// dispatchContainerLSPPalette runs the phase-5 palette ids.
func (m *Model) dispatchContainerLSPPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "lsp-container-where", "lsp-container-diagnostics", "lsp-mirror-refresh", "lsp-mirror-clear":
	default:
		return nil, false
	}
	if _, ok := m.containerLSPHostOf(); !ok || m.containerLSP == nil {
		return m.pushToastDetail(toast.Info, "Only for a folder in a container",
			"Open one with Workspace: Open Folder in Container…"), true
	}
	switch id {
	case "lsp-container-where":
		return m.openContainerLSPWherePicker(), true
	case "lsp-container-diagnostics":
		return m.toggleContainerLocalDiagnostics(), true
	case "lsp-mirror-refresh":
		return m.refreshContainerMirror(), true
	}
	return m.openMirrorClearCard(), true
}

// containerLSPWhereRows are the picker rows, the current one marked.
func containerLSPWhereRows(cur string) []picker.Item {
	rows := []picker.Item{
		{ID: containerLSPAuto, Title: "Auto", Hint: "in the container when it has the server, else a local one"},
		{ID: containerLSPImage, Title: "In the container only", Hint: "uses what the image has; nothing is installed"},
		{ID: containerLSPLocal, Title: "Local only", Hint: "a local server reads the bind mount or a local mirror"},
		{ID: containerLSPOff, Title: "Off", Hint: "no language server for this kind of workspace"},
	}
	for i := range rows {
		if rows[i].ID == cur {
			rows[i].Title = "● " + rows[i].Title
		} else {
			rows[i].Title = "  " + rows[i].Title
		}
	}
	return rows
}

func (m *Model) openContainerLSPWherePicker() tea.Cmd {
	cur := loadContainerLSPSettings().where()
	m.picker = picker.NewItems(" LSP: Where the Language Server Runs ", containerLSPWhereRows(cur))
	m.picker.SetWidth(84)
	m.picker.SetNote([]string{
		"For folders opened in a container. Nothing is installed in the container.",
		"Local servers show no diagnostics there unless you turn them on.",
	})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindContainerLSPWhere
	return nil
}

// onContainerLSPWherePicked saves the choice and restarts the servers.
func (m *Model) onContainerLSPWherePicked(id string) tea.Cmd {
	switch id {
	case containerLSPAuto, containerLSPImage, containerLSPLocal, containerLSPOff:
	default:
		return nil
	}
	val := id
	if val == containerLSPAuto {
		val = ""
	}
	if err := saveContainerLSPSetting("container_lsp", val); err != nil {
		return m.pushToastDetail(toast.Errr, "Setting not saved", err.Error())
	}
	cmd := m.applyContainerLSP(true)
	return tea.Batch(cmd, m.pushToast(toast.Info, "Language servers: "+containerLSPWhereTitle(id)))
}

func containerLSPWhereTitle(id string) string {
	for _, r := range containerLSPWhereRows("") {
		if r.ID == id {
			return strings.TrimSpace(r.Title)
		}
	}
	return id
}

// toggleContainerLocalDiagnostics flips container_local_lsp_diagnostics.
func (m *Model) toggleContainerLocalDiagnostics() tea.Cmd {
	on := !loadContainerLSPSettings().showLocalDiagnostics()
	val := ""
	if on {
		val = "on"
	}
	if err := saveContainerLSPSetting("container_local_lsp_diagnostics", val); err != nil {
		return m.pushToastDetail(toast.Errr, "Setting not saved", err.Error())
	}
	cmd := m.applyContainerLSP(false)
	if on {
		return tea.Batch(cmd, m.pushToastDetail(toast.Info, "Local diagnostics on",
			"A local server may report errors for packages that only exist in the container."))
	}
	return tea.Batch(cmd, m.pushToast(toast.Info, "Local diagnostics muted in this container workspace"))
}

// refreshContainerMirror copies the code again (a mount needs none).
func (m *Model) refreshContainerMirror() tea.Cmd {
	h, _ := m.containerLSPHostOf()
	if h.Caps().MountOf != "" {
		return m.pushToastDetail(toast.Info, "No mirror needed", "This folder is bind-mounted from "+h.Caps().MountOf+".")
	}
	if m.containerLSP.filling {
		return m.pushToast(toast.Info, "The mirror is being copied")
	}
	return tea.Batch(m.fillContainerMirrorCmd(), m.pushToast(toast.Info, "Copying the code of "+h.Label()+"…"))
}

// openMirrorClearCard asks before the mirror is deleted (Cancel first).
func (m *Model) openMirrorClearCard() tea.Cmd {
	st := m.containerLSP
	if st.mirror == nil || !st.mirrored {
		return m.pushToast(toast.Info, "There is no mirror to clear")
	}
	m.confirm = confirm.Risky("Clear the local mirror?",
		"The local copy of "+m.workspaceHostLabel()+"'s code is deleted and local language servers stop. "+
			"Nothing changes in the container. Refresh Container Mirror copies it again.",
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "clear", Title: "Clear", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindContainerMirrorClear
	return nil
}

// answerMirrorClear is the card's answer (Cancel and Esc keep it).
func (m *Model) answerMirrorClear(id string) tea.Cmd {
	st := m.containerLSP
	if id != "clear" || st == nil || st.mirror == nil {
		return nil
	}
	activeLSPMirror.CompareAndSwap(st.mirror, nil)
	st.mirrored = false
	err := st.mirror.Clear()
	cmd := m.applyContainerLSP(false) // local servers stop: no folder to read
	if err != nil {
		return tea.Batch(cmd, m.pushToastDetail(toast.Errr, "Mirror not fully cleared", err.Error()))
	}
	return tea.Batch(cmd, m.pushToast(toast.Info, "Mirror cleared"))
}
