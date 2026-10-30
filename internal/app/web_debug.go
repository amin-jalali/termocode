package app

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Run and Debug from the page (WEBP-4). Everything is the laptop's RUN
// AND DEBUG view's: the nvim-dap session in m.debug, its launch
// configurations, its persisted breakpoints, watches and Debug Console.
// The page names a configuration by name, a breakpoint by workspace path
// and line, a variable by its tree key; the hub runs the laptop's own
// functions (dapStart, dapStepOver, aiDebugSetBreakpoint …). The rows of
// the variables tree the page opened are the hub's (shared by the
// browsers, not the laptop's view): their children are fetched like the
// laptop's, again after each stop.

func init() {
	webTopics["debug"] = webTopicDef{snapshot: webDebugSnapshot, apply: webApplyReplace[webproto.DebugSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishDebug)
	webOps[webproto.OpDebugStart] = (*Model).webDebugStart
	webOps[webproto.OpDebugRun] = (*Model).webDebugRun
	webOps[webproto.OpDebugSelect] = (*Model).webDebugSelect
	webOps[webproto.OpDebugControl] = (*Model).webDebugControl
	webOps[webproto.OpDebugBp] = (*Model).webDebugBp
	webOps[webproto.OpDebugExpand] = (*Model).webDebugExpand
	webOps[webproto.OpDebugWatch] = (*Model).webDebugWatch
	webOps[webproto.OpDebugEval] = (*Model).webDebugEval
	webOps[webproto.OpDebugFrame] = (*Model).webDebugFrame
	webOps[webproto.OpDebugSources] = (*Model).webDebugSources
}

// webDebugMaxOpen caps the rows the page may keep open.
const webDebugMaxOpen = 500

// Tests swap these: run a configuration in a laptop terminal, one step of
// the session, a breakpoint change in nvim, the loaded scripts.
var (
	webDebugRunCmd  = func(m *Model, cwd, cmdline string) { m.debugRunInTerminal(cwd, cmdline) }
	webDebugStep    = func(m *Model, op string) tea.Cmd { return webDebugSteps[op](m) }
	webDebugSetBp   = (*Model).webDebugSetBpNvim
	webDebugAsk     = (*Model).debugRequestChildren
	webDebugSources = func(c *nvim.Client) (string, error) {
		return c.EvalLuaStringArgs(luaWebDebugSources, 3000)
	}
)

// The Lua the debug ops send nvim: constant chunks, their values as RPC
// arguments (WEB-21).
const (
	// luaWebDebugSources asks nvim-dap for the loaded scripts (the
	// argument is the wait in ms).
	luaWebDebugSources = `local t = ...; return _G._termocode_dap and _G._termocode_dap.loaded_sources and _G._termocode_dap.loaded_sources(t) or ''`
	// luaWebDebugSetBp sets a breakpoint: path, line, condition, log
	// message, hit count.
	luaWebDebugSetBp = `local p, l, c, g, h = ...
		if not (_G._termocode_dap and _G._termocode_dap.set_bp) then return 'the debugger is not set up yet' end
		return _G._termocode_dap.set_bp(p, l, c, g, h)`
)

// errWebNoEditor: the laptop's editor is not running.
var errWebNoEditor = webErr(webproto.CodeConflict, "The editor is not running.")

var webDebugSteps = map[string]func(m *Model) tea.Cmd{
	"continue": (*Model).dapContinue, "pause": (*Model).dapPause, "step_over": (*Model).dapStepOver,
	"step_into": (*Model).dapStepInto, "step_out": (*Model).dapStepOut, "restart": (*Model).dapRestart, "stop": (*Model).dapStop,
}

// webDebugPub is what the debug topic sent last, and the rows the page
// opened (Update only).
type webDebugPub struct {
	key  string
	last *webproto.DebugSnap
	open map[string]bool
}

type webDebugDelta webproto.DebugSnap

func (webDebugDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webDebugDelta)
	return n, ok
}

// webDebugKey fingerprints the debug state: what the laptop's view draws.
func (m *Model) webDebugKey() string {
	d := m.ensureDebug()
	s := d.snap
	var b strings.Builder
	b.WriteString(strconv.FormatBool(s.Available) + strconv.FormatBool(s.Active) + strconv.FormatBool(s.Stopped) + strconv.FormatBool(m.dapSessionActive))
	b.WriteString("|" + s.Name + "|" + d.status + "|" + d.hint + "|" + d.selected + "|" + d.configs.err)
	b.WriteString("|" + strconv.Itoa(s.Thread) + "," + strconv.Itoa(s.Frame) + "," + strconv.Itoa(len(s.Threads)) + "," + strconv.Itoa(len(s.Frames)) + "," + strconv.Itoa(len(s.Scopes)))
	ptr := func(v any) string { return strconv.FormatUint(uint64(reflect.ValueOf(v).Pointer()), 16) }
	d.runConfigs() // reads launch.json again when it changed (stat-throttled)
	b.WriteString("|" + ptr(s.Frames) + ptr(s.Scopes) + ptr(s.Threads) + ptr(d.bps) + ptr(d.liveBps) + ptr(d.configs.list))
	b.WriteString("|" + strconv.Itoa(len(d.children)) + "," + strconv.Itoa(len(d.childErr)) + "," + strconv.FormatUint(d.console.Seq(), 10))
	for _, w := range d.watches {
		b.WriteString("|" + w.Expr + "=" + w.Value + strconv.FormatBool(w.Err))
	}
	if m.web != nil {
		b.WriteString("|o" + strconv.Itoa(len(m.web.run.debug.open)))
	}
	return b.String()
}

func webDebugSnapshot(m *Model, _ string) (any, error) {
	snap := m.webDebugBuild()
	if m.web != nil {
		p := &m.web.run.debug
		p.key, p.last = m.webDebugKey(), &snap
	}
	return snap, nil
}

// webPublishDebug sends the state when it changed, and asks for the
// children of opened rows that have none yet (after a stop they are gone).
func (m *Model) webPublishDebug(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.run.debug
	if !h.webListens("debug") {
		p.last = nil
		return nil
	}
	m.webDebugFetchOpen()
	key := m.webDebugKey()
	if p.last != nil && key == p.key {
		return nil
	}
	p.key = key
	cur := m.webDebugBuild()
	if p.last != nil && reflect.DeepEqual(*p.last, cur) {
		return nil
	}
	p.last = &cur
	h.publish("debug", webDebugDelta(cur))
	return nil
}

// webDebugOpen says whether the page has row key open (scopes are open
// unless the adapter marked them expensive).
func (m *Model) webDebugOpen(key string, expensive bool) bool {
	if m.web == nil {
		return false
	}
	if v, ok := m.web.run.debug.open[key]; ok {
		return v
	}
	return !strings.Contains(key, "/") && !expensive
}

// webDebugVarsOf is the variables under key: a loaded scope's, else the
// fetched children; ok is false when they are not here.
func webDebugVarsOf(d *debugState, key string) ([]dbgVar, bool) {
	if !strings.Contains(key, "/") {
		for _, sc := range d.snap.Scopes {
			if scopeKey(sc.Name) == key && sc.Loaded {
				return sc.Variables, true
			}
		}
	}
	kids, ok := d.children[key]
	return kids, ok
}

// webDebugRef is the variables reference of row key (0: none).
func webDebugRef(d *debugState, key string) int {
	i := strings.LastIndexByte(key, '/')
	if i < 0 {
		for _, sc := range d.snap.Scopes {
			if scopeKey(sc.Name) == key && !sc.Loaded {
				return sc.Ref
			}
		}
		return 0
	}
	vars, ok := webDebugVarsOf(d, key[:i])
	if !ok {
		return 0
	}
	for _, v := range vars {
		if v.Name == key[i+1:] {
			return v.Ref
		}
	}
	return 0
}

// webDebugFetchOpen asks for the children of the opened rows that have
// none yet (parents first: a child's ref is known once its parent came).
func (m *Model) webDebugFetchOpen() {
	d := m.debug
	if d == nil || !d.snap.Stopped || m.web == nil {
		return
	}
	for key, open := range m.web.run.debug.open {
		if !open || d.inflight[key] {
			continue
		}
		if _, ok := webDebugVarsOf(d, key); ok {
			continue
		}
		if _, failed := d.childErr[key]; failed {
			continue
		}
		if ref := webDebugRef(d, key); ref > 0 {
			webDebugAsk(m, key, ref)
		}
	}
	for _, sc := range d.snap.Scopes {
		key := scopeKey(sc.Name)
		if sc.Loaded || d.inflight[key] || !m.webDebugOpen(key, sc.Expensive) {
			continue
		}
		if _, ok := d.children[key]; ok {
			continue
		}
		if _, failed := d.childErr[key]; !failed {
			webDebugAsk(m, key, sc.Ref)
		}
	}
}

// webDebugVar is a variable for the page, its value and type capped.
func webDebugVar(v dbgVar) webproto.DebugVar {
	return webproto.DebugVar{Name: webName(v.Name), Value: webClip(webName(v.Value), webproto.MaxDebugValue),
		Type: webClip(webName(v.Type), webproto.MaxDebugValue), Ref: v.Ref}
}

// webDebugPlace is a frame's or script's file for the page.
func (m *Model) webDebugPlace(abs string) (rel, file string, outside bool) {
	if abs == "" {
		return "", "", false
	}
	if r, ok := m.webRel(abs); ok {
		return r, "", false
	}
	return "", webName(filepath.Base(abs)), true
}

// webDebugBuild is the debug topic now.
func (m *Model) webDebugBuild() webproto.DebugSnap {
	d := m.ensureDebug()
	sn := d.snap
	s := webproto.DebugSnap{Available: sn.Available || m.nvim != nil, Active: sn.Active, Stopped: sn.Stopped, Name: webName(sn.Name),
		Status: webName(d.status), Thread: sn.Thread, Frame: sn.Frame, Threads: []webproto.DebugThread{}, Frames: []webproto.DebugFrame{},
		Scopes: []webproto.DebugScope{}, Children: []webproto.DebugChildren{}, Open: []string{}, Watches: []webproto.DebugWatch{},
		Breakpoints: []webproto.DebugBreakpoint{}, Configs: []webproto.DebugConfig{}, Console: []webproto.DebugLine{},
		Hint: webName(d.hint), ConfigError: webName(d.configs.err)}
	for i, t := range sn.Threads {
		if i == webproto.MaxDebugThreads {
			break
		}
		s.Threads = append(s.Threads, webproto.DebugThread{ID: t.ID, Name: webName(t.Name), Stopped: t.Stopped})
	}
	for i, f := range sn.Frames {
		if i == webproto.MaxDebugFrames {
			break
		}
		rel, file, out := m.webDebugPlace(f.Path)
		if f.Path == "" && f.Source != "" {
			file = webName(f.Source)
		}
		s.Frames = append(s.Frames, webproto.DebugFrame{ID: f.ID, Name: webName(f.Name), Path: rel, File: file, Outside: out, Line: f.Line, Col: f.Col})
	}
	budget := webproto.MaxDebugVars
	vars := func(list []dbgVar) []webproto.DebugVar {
		out := []webproto.DebugVar{}
		for _, v := range list {
			if budget == 0 {
				s.More++
				continue
			}
			budget--
			out = append(out, webDebugVar(v))
		}
		return out
	}
	for _, sc := range sn.Scopes {
		ds := webproto.DebugScope{Key: scopeKey(sc.Name), Name: webName(sc.Name), Expensive: sc.Expensive, Loaded: sc.Loaded, Vars: []webproto.DebugVar{}}
		if sc.Loaded {
			ds.Vars = vars(sc.Variables)
		}
		s.Scopes = append(s.Scopes, ds)
	}
	if m.web != nil && sn.Stopped {
		var keys []string
		for _, sc := range sn.Scopes {
			if k := scopeKey(sc.Name); !sc.Loaded && m.webDebugOpen(k, sc.Expensive) {
				keys = append(keys, k)
			}
		}
		for k, open := range m.web.run.debug.open {
			if open && strings.Contains(k, "/") {
				keys = append(keys, k)
			}
		}
		webSortStrings(keys)
		for _, k := range keys {
			if kids, ok := d.children[k]; ok {
				s.Children = append(s.Children, webproto.DebugChildren{Key: webName(k), Vars: vars(kids)})
			} else if e, ok := d.childErr[k]; ok {
				s.Children = append(s.Children, webproto.DebugChildren{Key: webName(k), Vars: []webproto.DebugVar{}, Error: webName(e)})
			}
		}
	}
	if m.web != nil {
		for k, open := range m.web.run.debug.open {
			if open {
				s.Open = append(s.Open, webName(k))
			}
		}
		webSortStrings(s.Open)
	}
	for i, w := range d.watches {
		if i == webproto.MaxDebugWatches {
			break
		}
		dw := webproto.DebugWatch{Expr: webName(w.Expr)}
		if sn.Stopped {
			dw.Value, dw.Error, dw.Ref = webClip(webName(w.Value), webproto.MaxDebugValue), w.Err, w.Ref
		}
		s.Watches = append(s.Watches, dw)
	}
	for _, r := range sortedBreakpointRows(d.bps, d.liveBps) {
		rel, ok := m.webRel(r.Path)
		if !ok {
			continue // a breakpoint of another project
		}
		if len(s.Breakpoints) == webproto.MaxDebugBreakpoints {
			break
		}
		b := webproto.DebugBreakpoint{Path: rel, Line: r.BP.Line, Text: webClip(webName(r.BP.Text), webproto.MaxDebugValue),
			Condition: webName(r.BP.Condition), Hit: webName(r.BP.HitCondition), Log: webName(r.BP.LogMessage)}
		if r.Verified != nil {
			b.Verified = map[bool]string{true: "yes", false: "no"}[*r.Verified]
		}
		s.Breakpoints = append(s.Breakpoints, b)
	}
	cfgs := d.runConfigs()
	sel, _, hasSel := d.selectedConfig()
	if hasSel {
		s.Selected = webName(sel.Name)
	}
	for i, c := range cfgs {
		if i == webproto.MaxDebugConfigs {
			break
		}
		s.Configs = append(s.Configs, webproto.DebugConfig{Name: webName(c.Name), Type: webName(c.Type), Debug: c.Debuggable(), Source: webName(c.Source)})
	}
	lines, cut := d.console.Tail(webproto.MaxDebugConsole)
	s.ConsoleCut = cut
	for _, l := range lines {
		cat := l.Category
		switch cat {
		case "stdout", "stderr", "console", "important", "input", "result":
		default:
			cat = "console"
		}
		s.Console = append(s.Console, webproto.DebugLine{Cat: cat, Text: webClip(l.Text, webproto.MaxDebugConsoleLine)})
	}
	return s
}

// webSortStrings sorts keys in place (a small insertion sort: few keys).
func webSortStrings(keys []string) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
}

// webDebugConfig finds a configuration by name ("": the selected one).
func (m *Model) webDebugConfig(call *webCall, name string) (launchConfig, bool) {
	d := m.ensureDebug()
	if name == "" {
		cfg, _, ok := d.selectedConfig()
		if !ok {
			call.fail(webproto.CodeNotFound, "There is no launch configuration. Add one on the laptop.")
		}
		return cfg, ok
	}
	for _, c := range d.runConfigs() {
		if c.Name == name {
			return c, true
		}
	}
	call.fail(webproto.CodeNotFound, "That configuration is not in the list.")
	return launchConfig{}, false
}

// webDebugStart starts a debug session for a configuration (it becomes
// the selected one, as a start from the laptop's list does).
func (m *Model) webDebugStart(call *webCall) tea.Cmd {
	cfg, ok := m.webDebugConfig(call, call.args.(*webproto.DebugConfigArgs).Config)
	if !ok {
		return nil
	}
	if !cfg.Debuggable() {
		call.fail(webproto.CodeConflict, "That configuration runs a command. Use Run.")
		return nil
	}
	if m.dapSessionActive {
		call.fail(webproto.CodeConflict, "A debug session runs. Stop it first.")
		return nil
	}
	d := m.ensureDebug()
	d.selected = cfg.Name
	cmd, err := webDebugStartCfg(m, cfg)
	if err != nil {
		m.webAuditOp(call, cfg.Name, "failed")
		call.fail(webproto.CodeConflict, webSentence(err.Error()))
		return cmd
	}
	m.webAuditOp(call, cfg.Name, "ok")
	call.reply(nil)
	return cmd
}

// webDebugRun runs a configuration without the debugger, in a new laptop
// terminal (the command is the configuration's, never the page's).
func (m *Model) webDebugRun(call *webCall) tea.Cmd {
	cfg, ok := m.webDebugConfig(call, call.args.(*webproto.DebugConfigArgs).Config)
	if !ok {
		return nil
	}
	cmdline := cfg.RunCommand()
	if cmdline == "" {
		call.fail(webproto.CodeConflict, "That configuration has nothing to run.")
		return nil
	}
	root, file := workspaceRoot(), m.editor.Path()
	cmdline = expandLaunchVars(cmdline, root, file)
	cwd := expandLaunchVars(orDefault(cfg.Cwd, "${workspaceFolder}"), root, file)
	webDebugRunCmd(m, cwd, cmdline)
	m.webAuditOp(call, cfg.Name, "ok")
	call.reply(nil)
	return nil
}

// webDebugSelect makes a configuration the one Start runs.
func (m *Model) webDebugSelect(call *webCall) tea.Cmd {
	cfg, ok := m.webDebugConfig(call, call.args.(*webproto.DebugConfigArgs).Config)
	if !ok {
		return nil
	}
	d := m.ensureDebug()
	d.selected = cfg.Name
	d.setHint(adapterHintFor(cfg), dapTypeLang(cfg.Type))
	call.reply(nil)
	return nil
}

// webDebugControl is one step of the live session.
func (m *Model) webDebugControl(call *webCall) tea.Cmd {
	op := call.args.(*webproto.DebugControlArgs).Op
	if !m.dapSessionActive {
		call.fail(webproto.CodeConflict, "No debug session runs.")
		return nil
	}
	stopped := m.ensureDebug().snap.Stopped
	switch op {
	case "continue", "step_over", "step_into", "step_out":
		if !stopped {
			call.fail(webproto.CodeConflict, "The program runs. Pause it first.")
			return nil
		}
	case "pause":
		if stopped {
			call.reply(nil)
			return nil
		}
	}
	m.webAuditOp(call, op, "ok")
	call.reply(nil)
	return webDebugStep(m, op)
}

// webDebugSetBpNvim sets a breakpoint through nvim-dap (loading a file
// that is not open, as the AI's tool does).
func (m *Model) webDebugSetBpNvim(abs string, line int, cond, hit, logMsg string) error {
	if m.nvim == nil {
		return errWebNoEditor
	}
	res, err := m.nvim.EvalLuaStringArgs(luaWebDebugSetBp, abs, line, cond, logMsg, hit)
	if err != nil {
		return err
	}
	if res != "" {
		return webErr(webproto.CodeConflict, webSentence(res))
	}
	m.debugSyncBreakpointsNow()
	return nil
}

// webDebugStored finds the stored breakpoint file for workspace path rel
// (nvim may name it by its resolved path).
func (m *Model) webDebugStored(rel string) (string, bool) {
	for p := range m.ensureDebug().bps {
		if r, ok := m.webRel(p); ok && r == rel {
			return p, true
		}
	}
	return "", false
}

// webDebugBp toggles, sets, removes or clears breakpoints.
func (m *Model) webDebugBp(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DebugBpArgs)
	d := m.ensureDebug()
	if a.Op == "clear" && a.Path == "" {
		n := len(sortedBreakpointRows(d.bps, nil))
		cmd := m.debugClearBreakpoints()
		m.webAuditOp(call, "all ("+strconv.Itoa(n)+")", "ok")
		call.reply(nil)
		return cmd
	}
	abs, err := m.webResolve(a.Path)
	if err != nil {
		webFail(call, err)
		return nil
	}
	stored, has := m.webDebugStored(a.Path)
	if !has {
		stored = abs
	}
	summary := a.Path
	if a.Op != "clear" {
		summary += ":" + strconv.Itoa(a.Line)
	}
	op := a.Op
	if op == "toggle" {
		op = "set"
		if has && d.hasBreakpoint(stored, a.Line) {
			op = "remove"
		}
	}
	var cmd tea.Cmd
	switch op {
	case "set":
		if has && d.hasBreakpoint(stored, a.Line) {
			// A new condition replaces the breakpoint on the line.
			cmd = m.debugRemoveBreakpoint(stored, a.Line)
		}
		if err := webDebugSetBp(m, abs, a.Line, a.Condition, a.Hit, a.Log); err != nil {
			m.webAuditOp(call, summary, "failed")
			webFailErr(call, err, "The breakpoint could not be set.")
			return cmd
		}
	case "remove":
		if !has || !d.hasBreakpoint(stored, a.Line) {
			call.fail(webproto.CodeNotFound, "There is no breakpoint on that line.")
			return nil
		}
		cmd = m.debugRemoveBreakpoint(stored, a.Line)
		m.debugSyncBreakpointsNow()
	case "clear":
		if has {
			var lines []int
			for _, bp := range d.bps[stored] {
				lines = append(lines, bp.Line)
			}
			var cmds []tea.Cmd
			for _, l := range lines {
				cmds = append(cmds, m.debugRemoveBreakpoint(stored, l))
			}
			m.debugSyncBreakpointsNow()
			cmd = tea.Batch(cmds...)
		}
	}
	m.webAuditOp(call, op+" "+summary, "ok")
	call.reply(nil)
	return cmd
}

// webDebugExpand opens or closes a row of the variables tree for the
// page (not the laptop's view).
func (m *Model) webDebugExpand(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DebugExpandArgs)
	p := &m.web.run.debug
	if p.open == nil {
		p.open = map[string]bool{}
	}
	if _, ok := p.open[a.Key]; !ok && len(p.open) >= webDebugMaxOpen {
		call.fail(webproto.CodeLimit, "Too many open rows. Close some.")
		return nil
	}
	if a.Open {
		p.open[a.Key] = true
	} else if strings.Contains(a.Key, "/") {
		// A closed variable forgets its open children too.
		for k := range p.open {
			if k == a.Key || strings.HasPrefix(k, a.Key+"/") {
				delete(p.open, k)
			}
		}
	} else {
		p.open[a.Key] = false // a scope stays closed
	}
	m.webDebugFetchOpen()
	call.reply(nil)
	return nil
}

// webDebugWatch adds or removes a WATCH expression.
func (m *Model) webDebugWatch(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DebugWatchArgs)
	expr := strings.TrimSpace(a.Expr)
	d := m.ensureDebug()
	summary := strconv.Itoa(len(expr)) + " chars"
	if a.Op == "remove" {
		for i, w := range d.watches {
			if w.Expr == expr {
				m.debugRemoveWatch(i)
				m.webAuditOp(call, "remove "+summary, "ok")
				call.reply(nil)
				return nil
			}
		}
		call.fail(webproto.CodeNotFound, "That expression is not watched.")
		return nil
	}
	if len(d.watches) >= webproto.MaxDebugWatches {
		call.fail(webproto.CodeLimit, "Too many watch expressions.")
		return nil
	}
	cmd := m.debugAddWatch(expr)
	m.webAuditOp(call, "add "+summary, "ok")
	call.reply(nil)
	return cmd
}

// webDebugEval evaluates a Debug Console line in the paused frame; the
// line and its answer come in the topic's console.
func (m *Model) webDebugEval(call *webCall) tea.Cmd {
	expr := strings.TrimSpace(call.args.(*webproto.DebugEvalArgs).Expr)
	if !m.dapSessionActive {
		call.fail(webproto.CodeConflict, "No debug session runs.")
		return nil
	}
	cmd := m.debugEvalRepl(expr)
	m.webAuditOp(call, strconv.Itoa(len(expr))+" chars", "ok")
	call.reply(nil)
	return cmd
}

// webDebugFrame selects a frame of the stopped thread's stack.
func (m *Model) webDebugFrame(call *webCall) tea.Cmd {
	id := call.args.(*webproto.DebugFrameArgs).ID
	d := m.ensureDebug()
	for _, f := range d.snap.Frames {
		if f.ID == id {
			m.debugSelectFrame(id)
			call.reply(nil)
			return m.debugRefresh()
		}
	}
	call.fail(webproto.CodeNotFound, "That frame is not on the stack now.")
	return nil
}

// webDebugSourcesRaw is the decoded _termocode_dap.loaded_sources().
type webDebugSourcesRaw struct {
	Error       string `json:"error"`
	Unsupported bool   `json:"unsupported"`
	Sources     []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	} `json:"sources"`
}

// webDebugSources answers the scripts the debugger loaded, read off the
// UI thread (the adapter answers in its own time).
func (m *Model) webDebugSources(call *webCall) tea.Cmd {
	if !m.dapSessionActive {
		call.fail(webproto.CodeConflict, "No debug session runs.")
		return nil
	}
	if m.nvim == nil {
		webFail(call, errWebNoEditor)
		return nil
	}
	c := m.nvim
	call.later()
	return func() tea.Msg {
		raw, err := webDebugSources(c)
		var r webDebugSourcesRaw
		if err == nil && raw != "" {
			err = json.Unmarshal([]byte(raw), &r)
		}
		if err != nil || r.Error != "" || raw == "" {
			msg := r.Error
			if msg == "" {
				msg = "the debugger did not answer"
			}
			call.fail(webproto.CodeConflict, webSentence(msg))
			return nil
		}
		return applyMsg(func(m *Model) tea.Cmd {
			out := webproto.DebugSources{Sources: []webproto.DebugSource{}, Unsupported: r.Unsupported}
			for _, src := range r.Sources {
				if len(out.Sources) == webproto.MaxDebugSources {
					out.More++
					continue
				}
				rel, file, outside := m.webDebugPlace(src.Path)
				name := webName(src.Name)
				if name == "" {
					name = file
					if rel != "" {
						name = filepath.Base(rel)
					}
				}
				out.Sources = append(out.Sources, webproto.DebugSource{Name: name, Path: rel, Outside: outside})
			}
			call.reply(out)
			return nil
		})
	}
}
