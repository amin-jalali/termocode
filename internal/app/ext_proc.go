package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ext"
	"github.com/amin-jalali/termocode/internal/extproc"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/preview"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Tier 2: process extensions (N6b, ADR 0009) ───────────────────────────
//
// A process extension is a program in any language that speaks JSON-RPC
// over stdio (internal/extproc). This file maps what it registers onto
// the same registry the Lua extensions fill (extState.reg), so the
// palette, activity bar, status bar, hit map and hover labels treat both
// kinds the same:
//
//   - extState.luaReg is what the Lua side reported; procRegs holds one
//     entry per process extension; extRebuild merges them into reg.
//   - The Host runs every process call on its own goroutines. Its events
//     come in through extProcListen (a Cmd that waits on the channel), so
//     Update never waits on an extension.
//   - Starting, reloading (Host.Set) and stopping run in Cmds too.

// termocodeVersion is sent to process extensions in "initialize";
// cmd/termocode sets it (SetVersion).
var termocodeVersion = "dev"

// SetVersion records the version string for extensions.
func SetVersion(v string) {
	if v != "" {
		termocodeVersion = v
	}
}

// extHostTarget is the newest Host, for StopExtensions.
var extHostTarget atomic.Pointer[extproc.Host]

// StopExtensions stops every process extension (and its children).
// cmd/termocode calls it when Bubble Tea returns, however it returns.
func StopExtensions() {
	if h := extHostTarget.Load(); h != nil {
		h.Close()
	}
}

// procReg is what one process extension registered.
type procReg struct {
	name        string
	version     string
	gen         int    // the run its registrations belong to
	state       string // "", "running", "restarting", "failed"
	err         string
	contributed []ext.Command // from extension.json (lazy start)
	commands    []ext.Command
	panels      []ext.Panel
	status      []ext.StatusItem
}

// procPending is a prompt or pick a process extension waits for.
type procPending struct {
	ext  string
	gen  int
	id   json.RawMessage
	kind string // "prompt" | "pick"
}

type extProcEventsMsg struct{ evs []extproc.Event }

type extProcSetMsg struct {
	exts    []ext.Extension
	skipped []string
	base    int // Host.Set's base: older events are dropped
}

type extProcDoneMsg struct {
	what string
	err  error
}

func newExtHost() *extproc.Host {
	root, _ := os.Getwd()
	h := extproc.NewHost(extproc.Config{Root: root, Version: termocodeVersion})
	extHostTarget.Store(h)
	return h
}

// extProcInitCmd discovers the process extensions, starts the ones that
// start at startup and starts listening. Called from Init.
func (m Model) extProcInitCmd() tea.Cmd {
	if m.ext == nil || m.ext.proc == nil {
		return nil
	}
	return tea.Batch(m.extProcListen(), m.extProcSetCmd())
}

// extProcListen waits for the next host events (it takes what is queued,
// up to 64, so a burst is one Update).
func (m Model) extProcListen() tea.Cmd {
	if m.ext == nil || m.ext.proc == nil {
		return nil
	}
	h := m.ext.proc
	return func() tea.Msg {
		var evs []extproc.Event
		select {
		case ev := <-h.Events():
			evs = append(evs, ev)
		case <-h.Done():
			return nil
		}
		for len(evs) < 64 {
			select {
			case ev := <-h.Events():
				evs = append(evs, ev)
				continue
			default:
			}
			break
		}
		return extProcEventsMsg{evs: evs}
	}
}

// extProcSetCmd (re)reads the extensions folder and hands the process
// extensions to the host, which stops the old processes first.
func (m Model) extProcSetCmd() tea.Cmd {
	h := m.ext.proc
	return func() tea.Msg {
		dir, err := ext.Dir()
		if err != nil {
			return extProcSetMsg{}
		}
		all, skipped := ext.Discover(dir)
		var procs []ext.Extension
		for _, x := range all {
			if x.IsProcess() {
				procs = append(procs, x)
			}
		}
		base := h.Set(procs)
		return extProcSetMsg{exts: procs, skipped: skipped, base: base}
	}
}

// updateExtProc handles this file's messages (called from updateExt).
func (m *Model) updateExtProc(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case extProcSetMsg:
		return m.onExtProcSet(msg), true
	case extProcEventsMsg:
		var cmds []tea.Cmd
		for _, ev := range msg.evs {
			cmds = append(cmds, m.onExtProcEvent(ev))
		}
		m.extRebuild()
		cmds = append(cmds, m.extProcListen())
		return tea.Batch(cmds...), true
	case extProcDoneMsg:
		if msg.err == nil || errors.Is(msg.err, extproc.ErrClosed) {
			return nil, true // a crash is reported by its exit event
		}
		recordError(fmt.Sprintf("[ext] %s: %v", msg.what, msg.err))
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr, "Extension "+msg.what+" failed", msg.err.Error())
		return c, true
	}
	return nil, false
}

func (m *Model) onExtProcSet(msg extProcSetMsg) tea.Cmd {
	s := m.ext
	s.procRegs = map[string]*procReg{}
	s.procBase = msg.base
	// Start only now, so every event of the new processes finds procRegs.
	s.proc.StartAtStartup()
	for _, x := range msg.exts {
		r := &procReg{name: x.Name, version: x.Manifest.Version}
		for _, c := range x.Manifest.Contributes.Commands {
			c.ID = qualifyExtID(x.Name, c.ID)
			c.Ext = x.Name
			if c.Title == "" {
				c.Title = c.ID
			}
			r.contributed = append(r.contributed, c)
		}
		s.procRegs[x.Name] = r
		if x.Problem != "" {
			recordError("[ext] " + x.Name + ": " + x.Problem)
		}
	}
	m.extRebuild()
	if m.nvim != nil {
		return nil // the Lua load reports skipped folders
	}
	var cmds []tea.Cmd
	for _, sk := range msg.skipped {
		recordError("[ext] skipped " + sk)
		var c tea.Cmd
		m.toast, c = m.toast.PushKeyed("ext-skip", toast.Warn, "Extension skipped", sk)
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

// qualifyExtID prefixes an id without a dot with the extension name, as
// the Lua API does.
func qualifyExtID(name, id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, ".") {
		return id
	}
	return name + "." + id
}

func (m *Model) onExtProcEvent(ev extproc.Event) tea.Cmd {
	s := m.ext
	r := s.procRegs[ev.Ext]
	if r == nil {
		return nil // from before a reload
	}
	if ev.Gen < r.gen || ev.Gen <= s.procBase {
		return nil // from a run that is gone
	}
	if ev.Gen > r.gen {
		r.gen = ev.Gen
	}
	switch ev.Kind {
	case extproc.EvStarted:
		r.state, r.err = "running", ""
	case extproc.EvExited:
		m.dropProcRegs(r)
		m.cancelProcPending(ev.Ext)
		if !ev.Restarting {
			if r.state != "failed" {
				r.state = ""
			}
			return nil
		}
		r.state, r.err = "restarting", ev.Err
		recordError(fmt.Sprintf("[ext] %s crashed: %s", ev.Ext, ev.Err))
		var c tea.Cmd
		m.toast, c = m.toast.PushKeyed("ext-crash-"+ev.Ext, toast.Warn,
			"Extension "+ev.Ext+" crashed, restarting", ev.Err)
		return c
	case extproc.EvFailed:
		m.dropProcRegs(r)
		m.cancelProcPending(ev.Ext)
		r.state, r.err = "failed", ev.Err
		recordError(fmt.Sprintf("[ext] %s: %s", ev.Ext, ev.Err))
		var c tea.Cmd
		m.toast, c = m.toast.PushKeyed("ext-err-"+ev.Ext, toast.Errr, "Extension "+ev.Ext+" stopped", ev.Err)
		return c
	case extproc.EvMessage:
		return m.onExtProcMessage(r, ev)
	}
	return nil
}

// dropProcRegs forgets what a run registered (a restart registers again).
func (m *Model) dropProcRegs(r *procReg) {
	for _, p := range r.panels {
		delete(m.ext.lines, p.ID)
	}
	for _, st := range r.status {
		delete(m.ext.status, st.ID)
	}
	r.commands, r.panels, r.status = nil, nil, nil
}

// procParams is the union of the params of the UI methods.
type procParams struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Hint    string   `json:"hint"`
	Icon    string   `json:"icon"`
	Text    string   `json:"text"`
	Command string   `json:"command"`
	Lines   []string `json:"lines"`
	Message string   `json:"message"`
	Detail  string   `json:"detail"`
	Level   string   `json:"level"`
	Body    string   `json:"body"`
	Label   string   `json:"label"`
	Value   string   `json:"value"`
	Path    string   `json:"path"`
	Line    int      `json:"line"`
	Col     int      `json:"col"`
	Items   []struct {
		Title string `json:"title"`
		Hint  string `json:"hint"`
	} `json:"items"`
}

func (m *Model) onExtProcMessage(r *procReg, ev extproc.Event) tea.Cmd {
	var p procParams
	_ = json.Unmarshal(ev.Msg.Params, &p)
	name := r.name
	id := qualifyExtID(name, p.ID)
	switch ev.Msg.Method {
	case "registerCommand":
		if id == "" {
			return nil
		}
		if p.Title == "" {
			p.Title = id
		}
		c := ext.Command{ID: id, Title: p.Title, Ext: name, Hint: p.Hint}
		r.commands = upsert(r.commands, c, func(x ext.Command) string { return x.ID })
	case "registerStatusItem":
		if id == "" {
			return nil
		}
		cmd := ""
		if p.Command != "" {
			cmd = qualifyExtID(name, p.Command)
		}
		r.status = upsert(r.status, ext.StatusItem{ID: id, Ext: name, Command: cmd}, func(x ext.StatusItem) string { return x.ID })
		m.ext.status[id] = p.Text
	case "statusItem/update":
		for _, st := range r.status {
			if st.ID == id {
				m.ext.status[id] = p.Text
			}
		}
	case "registerPanel":
		if id == "" {
			return nil
		}
		if p.Title == "" {
			p.Title = name
		}
		r.panels = upsert(r.panels, ext.Panel{ID: id, Title: p.Title, Icon: p.Icon, Ext: name}, func(x ext.Panel) string { return x.ID })
	case "panel/setContent":
		for _, pn := range r.panels {
			if pn.ID == id {
				m.ext.lines[id] = p.Lines
				m.clampExtCursor(id)
			}
		}
	case "notify":
		sev := toast.Info
		switch p.Level {
		case "warn", "warning":
			sev = toast.Warn
		case "error":
			sev = toast.Errr
		}
		msg := sanitizeExtText(p.Message)
		var c tea.Cmd
		if p.Detail != "" {
			m.toast, c = m.toast.PushDetail(sev, msg, sanitizeExtText(p.Detail))
		} else {
			m.toast, c = m.toast.Push(sev, msg)
		}
		return c
	case "preview":
		m.preview = preview.New(" "+sanitizeExtText(p.Title)+" ", sanitizeExtText(p.Body))
		m.preview.SetSize(m.w, m.h)
		m.previewOpen = true
	case "openFile":
		return m.extOpenFile(p.Path, p.Line, p.Col) // the host checked the workspace
	case "prompt":
		cancel := m.cancelExtRequests()
		title := p.Title
		if title == "" {
			title = "Input"
		}
		m.prompt = prompt.New(sanitizeExtText(title), sanitizeExtText(p.Label), p.Value)
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindExt
		m.ext.pendingProc = &procPending{ext: name, gen: ev.Gen, id: ev.Msg.ID, kind: "prompt"}
		return cancel
	case "pick":
		cancel := m.cancelExtRequests()
		items := make([]picker.Item, 0, len(p.Items))
		for i, it := range p.Items {
			items = append(items, picker.Item{ID: strconv.Itoa(i + 1), Title: sanitizeExtText(it.Title), Hint: sanitizeExtText(it.Hint)})
		}
		title := p.Title
		if title == "" {
			title = "Pick"
		}
		m.picker = picker.NewItems(" "+sanitizeExtText(title)+" ", items)
		m.picker.SetSize(m.w, m.h)
		m.pickerOpen = true
		m.pickerKind = pickerKindExt
		m.ext.pendingProc = &procPending{ext: name, gen: ev.Gen, id: ev.Msg.ID, kind: "pick"}
		return tea.Batch(cancel, m.picker.Init())
	}
	return nil
}

func upsert[T any](list []T, v T, key func(T) string) []T {
	for i := range list {
		if key(list[i]) == key(v) {
			list[i] = v
			return list
		}
	}
	return append(list, v)
}

// extRebuild merges the Lua and process registrations into reg and
// updates the activity bar.
func (m *Model) extRebuild() {
	s := m.ext
	if s == nil {
		return
	}
	reg := ext.Registry{
		Commands:   append([]ext.Command(nil), s.luaReg.Commands...),
		Panels:     append([]ext.Panel(nil), s.luaReg.Panels...),
		Status:     append([]ext.StatusItem(nil), s.luaReg.Status...),
		Extensions: append([]ext.Loaded(nil), s.luaReg.Extensions...),
	}
	s.procCmd = map[string]string{}
	s.procPanel = map[string]string{}
	seen := map[string]bool{}
	for _, c := range reg.Commands {
		seen[c.ID] = true
	}
	names := make([]string, 0, len(s.procRegs))
	for n := range s.procRegs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r := s.procRegs[n]
		cmds := append(append([]ext.Command(nil), r.commands...), r.contributed...)
		for _, c := range cmds {
			if seen[c.ID] {
				continue // a Lua command or the runtime one wins
			}
			seen[c.ID] = true
			reg.Commands = append(reg.Commands, c)
			s.procCmd[c.ID] = n
		}
		for _, p := range r.panels {
			reg.Panels = append(reg.Panels, p)
			s.procPanel[p.ID] = n
		}
		reg.Status = append(reg.Status, r.status...)
		l := ext.Loaded{Name: n, Version: r.version, OK: r.state != "failed", Error: r.err}
		reg.Extensions = append(reg.Extensions, l)
	}
	s.reg = reg
	items := make([]activity.ExtraItem, 0, len(reg.Panels))
	for _, p := range reg.Panels {
		items = append(items, activity.ExtraItem{Label: p.Title, Glyph: p.Icon})
	}
	if s.barItems == nil || !slices.Equal(items, s.barItems) { // most rebuilds only change texts
		s.barItems = items
		activity.SetExtraItems(items)
	}
	if v := m.activity.Active(); v >= activity.ViewExtBase {
		if _, ok := activity.ExtIndex(v); !ok {
			m.activity.SetActive(activity.ViewFiles)
		}
	}
}

// extProcExecute runs a process extension's command off the UI loop.
func (m *Model) extProcExecute(name, id string) tea.Cmd {
	h := m.ext.proc
	return func() tea.Msg {
		err := h.Execute(context.Background(), name, id, nil)
		return extProcDoneMsg{what: id, err: err}
	}
}

// extProcSelect tells a process extension a panel row was chosen.
func (m *Model) extProcSelect(name, panel string, row int, text string) {
	m.ext.proc.Notify(name, "panel/select", map[string]any{"panel": panel, "row": row, "text": text})
}

// procReply answers a pending prompt / pick (value nil = cancelled).
func (m *Model) procReply(p *procPending, value any) tea.Cmd {
	if p == nil || m.ext == nil || m.ext.proc == nil {
		return nil
	}
	m.ext.proc.Reply(p.ext, p.gen, p.id, value, nil)
	return nil
}

// cancelProcPending drops a prompt / pick of an extension that exited.
func (m *Model) cancelProcPending(name string) {
	if p := m.ext.pendingProc; p != nil && p.ext == name {
		m.ext.pendingProc = nil
	}
}

// extProcEventLua forwards editor events to process extensions
// ("termocode_ext_proc_event", event, path).
const extProcEventLua = `
local g = vim.api.nvim_create_augroup('termocode_ext_proc', { clear = true })
local function fwd(name)
  return function(ev)
    if vim.bo[ev.buf].buftype ~= '' or not ev.file or ev.file == '' then return end
    if _G.termocode_notify then
      pcall(_G.termocode_notify, 'termocode_ext_proc_event', name, vim.fn.fnamemodify(ev.file, ':p'))
    end
  end
end
vim.api.nvim_create_autocmd('BufWritePost', { group = g, callback = fwd('save') })
vim.api.nvim_create_autocmd('BufReadPost', { group = g, callback = fwd('open') })
`

func init() {
	registerNotifyHandler("termocode_ext_proc_event", func(m *Model, args []any) tea.Cmd {
		if m.ext != nil && m.ext.proc != nil {
			m.ext.proc.Event(notifyArgString(args, 0), notifyArgString(args, 1))
		}
		return nil
	})
}

// procLogText is the host log for "Extensions: Show Log".
func (m Model) procLogText() string {
	if m.ext == nil || m.ext.proc == nil {
		return ""
	}
	var b strings.Builder
	for _, st := range m.ext.proc.Status() {
		pid := ""
		if st.Pid > 0 {
			pid = fmt.Sprintf(" pid %d", st.Pid)
		}
		fmt.Fprintf(&b, "  %s  process: %s%s", st.Name, st.State, pid)
		if st.Restarts > 0 {
			fmt.Fprintf(&b, ", %d restarts", st.Restarts)
		}
		if st.Err != "" {
			b.WriteString(" (" + st.Err + ")")
		}
		b.WriteString("\n")
	}
	if lines := m.ext.proc.Log(); len(lines) > 0 {
		b.WriteString("\n── process extensions log ──\n")
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}
