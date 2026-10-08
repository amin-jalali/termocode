package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/toast"
)

// ── Debug session state (Group D) ────────────────────────────────────────
//
// nvim-dap stays the DAP client. Lua listeners (dap_lua.go) push every
// session event to Go as
//
//	termocode_dap(kind, json)          session / initialized / stopped /
//	                                   continued / terminated / exited /
//	                                   output / message / refresh / breakpoints
//	termocode_dap_result(reqid, json)  answer to an async variables /
//	                                   evaluate request
//
// Go keeps the UI model in m.debug (a pointer so value copies of Model
// share it): the last session snapshot, the variable tree's open nodes and
// lazily fetched children, watches, the Debug Console and the persisted
// breakpoints. dapSessionActive (status-bar badge) is derived from events,
// never guessed.

// dbgFrame is one stack frame of the stopped thread.
type dbgFrame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Source string `json:"source"`
	Line   int    `json:"line"`
	Col    int    `json:"col"`
}

type dbgThread struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Stopped bool   `json:"stopped"`
}

type dbgVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"type"`
	Ref   int    `json:"ref"`
}

type dbgScope struct {
	Name      string   `json:"name"`
	Ref       int      `json:"ref"`
	Expensive bool     `json:"expensive"`
	Loaded    bool     `json:"loaded"`
	Variables []dbgVar `json:"variables"`
}

// dbgSnapshot is the decoded _termocode_dap.state().
type dbgSnapshot struct {
	Available bool        `json:"available"`
	Active    bool        `json:"active"`
	Stopped   bool        `json:"stopped"`
	Name      string      `json:"name"`
	Thread    int         `json:"thread"`
	Frame     int         `json:"frame"`
	Threads   []dbgThread `json:"threads"`
	Frames    []dbgFrame  `json:"frames"`
	Scopes    []dbgScope  `json:"scopes"`
}

// dbgResult is the decoded termocode_dap_result payload.
type dbgResult struct {
	Error     string   `json:"error"`
	Variables []dbgVar `json:"variables"`
	Result    string   `json:"result"`
	Type      string   `json:"type"`
	Ref       int      `json:"ref"`
}

// dbgWatch is one WATCH expression and its last value.
type dbgWatch struct {
	Expr  string
	Value string
	Err   bool
	Ref   int
}

// dbgPending remembers what an async request was for.
type dbgPending struct {
	kind string // "vars" | "watch" | "repl"
	key  string // vars: tree key; watch: expression
}

// debugState is everything the Run view and Debug Console render.
type debugState struct {
	snap       dbgSnapshot
	status     string // toolbar status ("Paused on breakpoint", "Running", …)
	stopReason string

	// Variable tree: open[key] overrides the default (scopes open unless
	// expensive, variables closed). children[key] holds a fetched child
	// list; refs change on every stop, so children are dropped then.
	open     map[string]bool
	children map[string][]dbgVar
	childErr map[string]string
	pending  map[int]dbgPending
	inflight map[string]bool
	nextReq  int

	watches []dbgWatch

	console *debugConsole

	// Launch configurations.
	configs  configCache
	selected string // selected config name ("" = first)
	hint     string // missing-adapter hint shown in the Run view

	// Breakpoints.
	bpPath  string
	bps     map[string][]storedBreakpoint
	liveBps map[string][]liveBreakpoint

	// Run view accordion.
	collapsed map[runSection]bool
	cursor    int
}

func newDebugState() *debugState {
	return &debugState{
		open:      map[string]bool{},
		children:  map[string][]dbgVar{},
		childErr:  map[string]string{},
		pending:   map[int]dbgPending{},
		inflight:  map[string]bool{},
		console:   newDebugConsole(),
		bps:       map[string][]storedBreakpoint{},
		collapsed: map[runSection]bool{},
	}
}

// ensureDebug returns m.debug, creating it on first use.
func (m *Model) ensureDebug() *debugState {
	if m.debug == nil {
		m.debug = newDebugState()
	}
	return m.debug
}

// debugMsg applies a closure to the Model inside Update — the result of a
// background fetch. Routed by routeDebugMsg (update.go).
type debugMsg struct {
	apply func(m *Model) tea.Cmd
}

// shiftAltF5CSI is the xterm sequence for Shift+Alt+F5 (CSI 15;4~), which
// Bubble Tea v1 does not decode — it arrives as an unknown-CSI message
// whose String() is "?CSI[49 53 59 52 126]?".
const shiftAltF5CSI = "?CSI[49 53 59 52 126]?"

// routeDebugMsg handles Group D messages at the top of Update. Returns
// handled=false for anything else.
func (m *Model) routeDebugMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case debugMsg:
		if msg.apply == nil {
			return nil, true
		}
		return msg.apply(m), true
	case fmt.Stringer:
		if _, isKey := msg.(tea.KeyMsg); !isKey && msg.String() == shiftAltF5CSI {
			return m.dapStop(), true
		}
	}
	return nil, false
}

// debugToast is a small "DAP" info toast.
func (m *Model) debugToast(detail string) tea.Cmd {
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Info, "DAP", detail)
	return cmd
}

func (m *Model) debugErrorToast(detail string) tea.Cmd {
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Errr, "DAP", detail)
	return cmd
}

// ── Events from Lua ──────────────────────────────────────────────────────

func init() {
	registerNotifyHandler("termocode_dap", func(m *Model, args []any) tea.Cmd {
		return m.onDebugEvent(notifyArgString(args, 0), notifyArgString(args, 1))
	})
	registerNotifyHandler("termocode_dap_result", func(m *Model, args []any) tea.Cmd {
		var res dbgResult
		_ = json.Unmarshal([]byte(notifyArgString(args, 1)), &res)
		return m.onDebugResult(notifyArgInt(args, 0), res)
	})
}

// debugEvent is the union of every termocode_dap payload.
type debugEvent struct {
	Active   bool   `json:"active"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Text     string `json:"text"`
	Code     int    `json:"code"`
	Category string `json:"category"`
	Output   string `json:"output"`
	Level    int    `json:"level"`
}

// onDebugEvent updates the state for one Lua event and schedules a
// snapshot refresh where needed.
func (m *Model) onDebugEvent(kind, payload string) tea.Cmd {
	d := m.ensureDebug()
	var ev debugEvent
	if payload != "" {
		_ = json.Unmarshal([]byte(payload), &ev)
	}
	switch kind {
	case "output":
		d.console.AppendOutput(ev.Category, ev.Output)
		return nil
	case "message":
		cat := "console"
		if ev.Level >= 3 { // vim.log.levels.WARN
			cat = "stderr"
		}
		d.console.AppendOutput(cat, ev.Text+"\n")
		if ev.Level >= 3 {
			return m.debugErrorToast(dbgFirstLine(ev.Text))
		}
		return nil
	case "breakpoints":
		return m.debounce.Do("dap-bps", 150*time.Millisecond, func() tea.Msg {
			return debugMsg{apply: func(m *Model) tea.Cmd { return m.debugSyncBreakpointsCmd() }}
		})
	case "session":
		if ev.Active {
			m.debugSessionStarted(ev.Name)
		}
	case "initialized":
		m.debugSessionStarted(ev.Name)
	case "stopped":
		d.snap.Stopped = true
		d.stopReason = ev.Reason
		d.status = stopStatus(ev.Reason, ev.Text)
		d.dropChildren()
		cmd := m.debugRefresh()
		return tea.Batch(cmd, m.debugEvaluateWatchesLater())
	case "continued":
		if d.snap.Stopped {
			d.snap.Stopped = false
			d.snap.Frames = nil
			d.snap.Scopes = nil
			d.dropChildren()
		}
		d.status = "Running"
	case "terminated", "exited":
		if kind == "exited" {
			d.console.AppendOutput("console", fmt.Sprintf("Process exited with code %d\n", ev.Code))
		}
	}
	return m.debugRefresh()
}

// debugSessionStarted marks a session live (idempotent).
func (m *Model) debugSessionStarted(name string) {
	d := m.ensureDebug()
	if !m.dapSessionActive {
		d.console.AppendOutput("console", "── "+orDefault(name, "debug session")+" ──\n")
	}
	m.dapSessionActive = true
	d.snap.Active = true
	if name != "" {
		d.snap.Name = name
	}
	if d.status == "" || d.status == "Starting…" {
		d.status = "Running"
	}
	d.hint = ""
}

func stopStatus(reason, text string) string {
	switch reason {
	case "breakpoint", "function breakpoint", "data breakpoint", "instruction breakpoint":
		return "Paused on breakpoint"
	case "step":
		return "Paused on step"
	case "exception":
		if text != "" {
			return "Paused on exception: " + dbgFirstLine(text)
		}
		return "Paused on exception"
	case "pause":
		return "Paused"
	case "entry":
		return "Paused on entry"
	case "goto":
		return "Paused"
	}
	return "Paused"
}

func dbgFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// dropChildren forgets fetched variable children (their refs are stale).
func (d *debugState) dropChildren() {
	d.children = map[string][]dbgVar{}
	d.childErr = map[string]string{}
	d.inflight = map[string]bool{}
	d.pending = map[int]dbgPending{}
}

// debugRefresh fetches a fresh snapshot (debounced: listeners fire in
// bursts — stackTrace, scopes and one variables response per scope).
func (m *Model) debugRefresh() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return m.debounce.Do("dap-state", 40*time.Millisecond, func() tea.Msg {
		raw, err := c.EvalLuaString(`return _G._termocode_dap and _G._termocode_dap.state and _G._termocode_dap.state() or ''`)
		if err != nil || raw == "" {
			return nil
		}
		var snap dbgSnapshot
		if json.Unmarshal([]byte(raw), &snap) != nil {
			return nil
		}
		return debugMsg{apply: func(m *Model) tea.Cmd { return m.debugApplySnapshot(snap) }}
	})
}

// debugApplySnapshot installs a snapshot and re-fetches the children of
// open variables.
func (m *Model) debugApplySnapshot(snap dbgSnapshot) tea.Cmd {
	d := m.ensureDebug()
	wasActive := m.dapSessionActive
	d.snap = snap
	m.dapSessionActive = snap.Active
	switch {
	case !snap.Active:
		if wasActive {
			d.console.AppendOutput("console", "── session ended ──\n")
		}
		d.status = ""
		d.stopReason = ""
		d.dropChildren()
		for i := range d.watches {
			d.watches[i].Value, d.watches[i].Err, d.watches[i].Ref = "", false, 0
		}
	case snap.Stopped:
		if !strings.HasPrefix(d.status, "Paused") {
			d.status = "Paused"
		}
	default:
		d.status = "Running"
	}
	if wasActive != snap.Active && snap.Active {
		d.hint = ""
	}
	m.debugClampCursor()
	return m.debugRequestOpenChildren()
}

// ── Async requests ───────────────────────────────────────────────────────

// debugRequest issues an async variables / evaluate call. The answer comes
// back as termocode_dap_result(reqid, json).
func (m *Model) debugRequest(p dbgPending, lua string, args ...interface{}) {
	if m.nvim == nil {
		return
	}
	d := m.ensureDebug()
	d.nextReq++
	id := d.nextReq
	d.pending[id] = p
	if p.kind == "vars" {
		d.inflight[p.key] = true
	}
	_ = m.nvim.ExecLuaArgs(lua, append([]interface{}{id}, args...)...)
}

const luaDapVariables = `local id, ref = ...; if _G._termocode_dap then _G._termocode_dap.variables(id, ref) end`
const luaDapEvaluate = `local id, expr, ctx = ...; if _G._termocode_dap then _G._termocode_dap.evaluate(id, expr, ctx) end`

// debugRequestChildren fetches the children of a tree node.
func (m *Model) debugRequestChildren(key string, ref int) {
	if ref <= 0 {
		return
	}
	d := m.ensureDebug()
	if d.inflight[key] {
		return
	}
	m.debugRequest(dbgPending{kind: "vars", key: key}, luaDapVariables, ref)
}

// debugRequestOpenChildren walks the visible tree and requests children of
// every open node that has none yet (re-expanding after each stop).
func (m *Model) debugRequestOpenChildren() tea.Cmd {
	d := m.debug
	if d == nil || !d.snap.Stopped {
		return nil
	}
	var walk func(key string, vars []dbgVar)
	walk = func(key string, vars []dbgVar) {
		for _, v := range vars {
			k := key + "/" + v.Name
			if v.Ref > 0 && d.open[k] {
				if kids, ok := d.children[k]; ok {
					walk(k, kids)
				} else if _, failed := d.childErr[k]; !failed {
					m.debugRequestChildren(k, v.Ref)
				}
			}
		}
	}
	for _, sc := range d.snap.Scopes {
		k := scopeKey(sc.Name)
		if !d.scopeOpen(sc) {
			continue
		}
		vars := sc.Variables
		if !sc.Loaded {
			kids, ok := d.children[k]
			if !ok {
				if _, failed := d.childErr[k]; !failed {
					m.debugRequestChildren(k, sc.Ref)
				}
				continue
			}
			vars = kids
		}
		walk(k, vars)
	}
	return nil
}

func scopeKey(name string) string { return "s:" + name }

// scopeOpen: scopes default open unless the adapter marked them expensive.
func (d *debugState) scopeOpen(sc dbgScope) bool {
	if v, ok := d.open[scopeKey(sc.Name)]; ok {
		return v
	}
	return !sc.Expensive
}

// onDebugResult routes an async answer to its requester.
func (m *Model) onDebugResult(id int, res dbgResult) tea.Cmd {
	d := m.ensureDebug()
	p, ok := d.pending[id]
	if !ok {
		return nil // stale (dropped on stop / continue)
	}
	delete(d.pending, id)
	switch p.kind {
	case "vars":
		delete(d.inflight, p.key)
		if res.Error != "" {
			d.childErr[p.key] = res.Error
			return nil
		}
		kids := res.Variables
		if kids == nil {
			kids = []dbgVar{}
		}
		d.children[p.key] = kids
		return m.debugRequestOpenChildren()
	case "watch":
		for i := range d.watches {
			if d.watches[i].Expr == p.key {
				if res.Error != "" {
					d.watches[i].Value, d.watches[i].Err, d.watches[i].Ref = res.Error, true, 0
				} else {
					d.watches[i].Value, d.watches[i].Err, d.watches[i].Ref = res.Result, false, res.Ref
				}
			}
		}
	case "repl":
		if res.Error != "" {
			d.console.AppendOutput("stderr", res.Error+"\n")
		} else {
			d.console.AppendOutput("result", res.Result+"\n")
		}
	}
	return nil
}

// ── Watches ──────────────────────────────────────────────────────────────

// debugAddWatch adds a WATCH expression (deduplicated) and evaluates it
// when the session is paused.
func (m *Model) debugAddWatch(expr string) tea.Cmd {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	d := m.ensureDebug()
	for _, w := range d.watches {
		if w.Expr == expr {
			return nil
		}
	}
	d.watches = append(d.watches, dbgWatch{Expr: expr})
	d.collapsed[runSecWatch] = false
	if d.snap.Stopped {
		m.debugRequest(dbgPending{kind: "watch", key: expr}, luaDapEvaluate, expr, "watch")
	}
	return nil
}

func (m *Model) debugRemoveWatch(i int) {
	d := m.ensureDebug()
	if i < 0 || i >= len(d.watches) {
		return
	}
	d.watches = append(d.watches[:i:i], d.watches[i+1:]...)
}

// debugEvaluateWatchesLater re-evaluates watches once the stop settled
// (the frame id must be current).
func (m *Model) debugEvaluateWatchesLater() tea.Cmd {
	d := m.ensureDebug()
	if len(d.watches) == 0 {
		return nil
	}
	return m.debounce.Do("dap-watch", 120*time.Millisecond, func() tea.Msg {
		return debugMsg{apply: func(m *Model) tea.Cmd {
			d := m.ensureDebug()
			if !d.snap.Stopped {
				return nil
			}
			for _, w := range d.watches {
				m.debugRequest(dbgPending{kind: "watch", key: w.Expr}, luaDapEvaluate, w.Expr, "watch")
			}
			return nil
		}}
	})
}

// debugEvalRepl evaluates a Debug Console line.
func (m *Model) debugEvalRepl(expr string) tea.Cmd {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	d := m.ensureDebug()
	d.console.AppendOutput("input", "› "+expr+"\n")
	if !m.dapSessionActive {
		d.console.AppendOutput("stderr", "No active debug session.\n")
		return nil
	}
	m.debugRequest(dbgPending{kind: "repl", key: expr}, luaDapEvaluate, expr, "repl")
	return nil
}

// debugSelectFrame makes a frame current (its variables load via the
// scopes / variables listeners).
func (m *Model) debugSelectFrame(id int) {
	if m.nvim == nil {
		return
	}
	d := m.ensureDebug()
	d.snap.Frame = id
	d.dropChildren()
	_ = m.nvim.ExecLuaArgs(`local id = ...; if _G._termocode_dap and _G._termocode_dap.select_frame then _G._termocode_dap.select_frame(id) end`, id)
}
