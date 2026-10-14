package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// K7: the debugger for the agent, through the same nvim-dap session the
// Run & Debug view shows.
//
//   - debug_breakpoint (list auto; set / remove auto like F9; clear asks,
//     class ide): breakpoints in any workspace file, with condition or log
//     message. They show in the gutter and the Run view and persist in
//     breakpoints.json like the UI's.
//   - debug_state (auto): session, stop reason, thread, top frames, the
//     variables of the top frame (capped, depth-limited) and watch values.
//   - debug_start (confirm, class run): a launch configuration by name, a
//     file with a configuration built from its language, or the selected
//     Run-view configuration; waits for the first stop.
//   - debug_control (confirm): continue / steps / restart (class run),
//     pause / stop (class ide); waits (bounded, Stop cancels) for the next
//     stop and returns the new state.
//   - debug_evaluate (confirm, class run): an expression in the current
//     frame; it may run code in the program.
//
// The editor follows a new stop like the UI: it shows the stopped line
// (the user's focus stays where it was).

const (
	aiDebugValueWidth   = 200
	aiDebugMaxFrames    = 8
	aiDebugMaxVars      = 60
	aiDebugMaxDepth     = 3
	aiDebugStartWait    = 30 * time.Second
	aiDebugControlWait  = 10 * time.Second
	aiDebugPoll         = 100 * time.Millisecond
	aiDebugOutputLines  = 15
	aiDebugStateTimeout = 3000 // ms for all the requests of one debug_state
)

var (
	aiDebugBpOps      = []string{"list", "set", "remove", "clear"}
	aiDebugControlOps = []string{"continue", "pause", "step_over", "step_into", "step_out", "stop", "restart"}
)

// aiDbgVar is a variable with its loaded children (Lua ai_state).
type aiDbgVar struct {
	Name     string     `json:"name"`
	Value    string     `json:"value"`
	Type     string     `json:"type"`
	Ref      int        `json:"ref"`
	Children []aiDbgVar `json:"children"`
	More     int        `json:"more"`
}

type aiDbgScope struct {
	Name      string     `json:"name"`
	Expensive bool       `json:"expensive"`
	Vars      []aiDbgVar `json:"vars"`
	More      int        `json:"more"`
	Error     string     `json:"error"`
}

type aiDbgWatch struct {
	Expr  string `json:"expr"`
	Value string `json:"value"`
	Error string `json:"error"`
}

type aiDbgThread struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// aiDbgState is the decoded _termocode_dap.ai_state().
type aiDbgState struct {
	Available  bool         `json:"available"`
	Active     bool         `json:"active"`
	Stopped    bool         `json:"stopped"`
	Name       string       `json:"name"`
	Reason     string       `json:"reason"`
	Text       string       `json:"text"`
	Exit       *int         `json:"exit"`
	Output     string       `json:"output"` // the session's stdout / stderr
	Threads    int          `json:"threads"`
	Thread     *aiDbgThread `json:"thread"`
	Frames     []dbgFrame   `json:"frames"`
	FrameTotal int          `json:"frame_total"`
	Scopes     []aiDbgScope `json:"scopes"`
	Watches    []aiDbgWatch `json:"watches"`
}

// aiDbgStatus is the decoded _termocode_dap.ai_status(): what the waits
// compare. Seq counts stops, Sessions started sessions, Msgs DAP errors.
type aiDbgStatus struct {
	Available bool   `json:"available"`
	Active    bool   `json:"active"`
	Stopped   bool   `json:"stopped"`
	Seq       int    `json:"seq"`
	Sessions  int    `json:"sessions"`
	Msgs      int    `json:"msgs"`
	Frames    int    `json:"frames"`
	Reason    string `json:"reason"`
	Exit      *int   `json:"exit"`
	Msg       string `json:"msg"`
	Name      string `json:"name"`
}

// aiDbgExtra is what the report adds when no session runs.
type aiDbgExtra struct {
	Breakpoints int
	Configs     []string
	Output      []string // last Debug Console lines
}

// aiDbgLua runs a Lua chunk in nvim and returns its string result. It is
// called on the tool goroutine (the helpers wait for the adapter).
type aiDbgLua func(code string, args ...any) (string, error)

// aiDebugDone says whether a wait after op is over. A stop counts only
// when it is new (Seq grew) and its frames are loaded.
func aiDebugDone(op string, before, now aiDbgStatus) bool {
	newStop := now.Active && now.Stopped && now.Seq > before.Seq && now.Frames > 0
	switch op {
	case "stop":
		return !now.Active
	case "restart":
		return newStop
	case "start":
		if newStop {
			return true
		}
		if now.Active {
			return false
		}
		return now.Sessions > before.Sessions || now.Msgs > before.Msgs
	}
	return newStop || !now.Active
}

// clipValue cuts a value to one line of aiDebugValueWidth runes.
func clipValue(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if r := []rune(s); len(r) > aiDebugValueWidth {
		s = string(r[:aiDebugValueWidth]) + "…"
	}
	return s
}

// frameLoc is "rel/path.go:12" (or the frame's source name).
func frameLoc(f dbgFrame, root string) string {
	p := f.Path
	if p == "" {
		p = f.Source
	}
	if p == "" {
		return "no source"
	}
	return fmt.Sprintf("%s:%d", aiRootRel(root, p), f.Line)
}

func writeDbgVars(b *strings.Builder, vars []aiDbgVar, more int, indent string) {
	for _, v := range vars {
		b.WriteString(indent + v.Name)
		if v.Type != "" {
			b.WriteString(" " + v.Type)
		}
		b.WriteString(" = " + clipValue(v.Value) + "\n")
		writeDbgVars(b, v.Children, v.More, indent+"  ")
	}
	if more > 0 {
		fmt.Fprintf(b, "%s… %d more\n", indent, more)
	}
}

// aiDebugStateReport is the debug_state text.
func aiDebugStateReport(st aiDbgState, root string, x aiDbgExtra) string {
	var b strings.Builder
	output := func() {
		if len(x.Output) == 0 {
			return
		}
		b.WriteString("Program output (last lines):\n" + strings.Join(x.Output, "\n") + "\n")
	}
	switch {
	case !st.Available:
		return "The debugger is not ready: nvim-dap is not downloaded yet (termocode downloads it at start; " +
			"it needs git and the network)."
	case !st.Active:
		b.WriteString("No debug session is running.")
		if st.Exit != nil {
			fmt.Fprintf(&b, " The last session ended with exit code %d.", *st.Exit)
		}
		b.WriteString("\n")
		output()
		fmt.Fprintf(&b, "%d breakpoints (debug_breakpoint list).\n", x.Breakpoints)
		if len(x.Configs) > 0 {
			q := make([]string, len(x.Configs))
			for i, c := range x.Configs {
				q[i] = fmt.Sprintf("%q", c)
			}
			b.WriteString("Configurations: " + strings.Join(q, ", ") + "\n")
		}
		b.WriteString("Start one with debug_start(config) or debug_start(path).")
		return b.String()
	case !st.Stopped:
		fmt.Fprintf(&b, "Debug session %q is running (not paused). A breakpoint stops it; debug_control(pause) pauses it.", st.Name)
		return b.String()
	}
	fmt.Fprintf(&b, "Debug session %q is paused", st.Name)
	if st.Reason != "" {
		fmt.Fprintf(&b, " (%s", st.Reason)
		if st.Text != "" {
			b.WriteString(": " + clipValue(st.Text))
		}
		b.WriteString(")")
	}
	if st.Thread != nil {
		fmt.Fprintf(&b, " in thread %d %q", st.Thread.ID, st.Thread.Name)
	}
	b.WriteString(".\n")
	if len(st.Frames) > 0 {
		top := st.Frames[0]
		fmt.Fprintf(&b, "Stopped at %s in %s.\n", frameLoc(top, root), top.Name)
		total := st.FrameTotal
		if total < len(st.Frames) {
			total = len(st.Frames)
		}
		fmt.Fprintf(&b, "Call stack (%d of %d frames):\n", len(st.Frames), total)
		for i, f := range st.Frames {
			fmt.Fprintf(&b, "  #%d %s (%s)\n", i, f.Name, frameLoc(f, root))
		}
		fmt.Fprintf(&b, "Variables of %s:\n", top.Name)
	}
	for _, sc := range st.Scopes {
		switch {
		case sc.Expensive:
			fmt.Fprintf(&b, "  %s: not loaded (expensive scope)\n", sc.Name)
		case sc.Error != "":
			fmt.Fprintf(&b, "  %s: error: %s\n", sc.Name, clipValue(sc.Error))
		case len(sc.Vars) == 0 && sc.More == 0:
			fmt.Fprintf(&b, "  %s: none\n", sc.Name)
		default:
			fmt.Fprintf(&b, "  %s:\n", sc.Name)
			writeDbgVars(&b, sc.Vars, sc.More, "    ")
		}
	}
	if len(st.Watches) > 0 {
		b.WriteString("Watch:\n")
		for _, w := range st.Watches {
			if w.Error != "" {
				fmt.Fprintf(&b, "  %s: error: %s\n", w.Expr, clipValue(w.Error))
			} else {
				fmt.Fprintf(&b, "  %s = %s\n", w.Expr, clipValue(w.Value))
			}
		}
	}
	b.WriteString("Next: debug_control(step_over | step_into | step_out | continue | stop), debug_evaluate(expr).")
	return b.String()
}

// aiDebugBreakpointReport is the debug_breakpoint list text.
func aiDebugBreakpointReport(store map[string][]storedBreakpoint, live map[string][]liveBreakpoint, root string) string {
	rows := sortedBreakpointRows(store, live)
	if len(rows) == 0 {
		return "No breakpoints. Set one with debug_breakpoint(op: \"set\", path, line)."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d breakpoints:\n", len(rows))
	for _, r := range rows {
		fmt.Fprintf(&b, "- %s:%d", aiRootRel(root, r.Path), r.BP.Line)
		if r.BP.Text != "" {
			b.WriteString(": " + clipValue(r.BP.Text))
		}
		if r.BP.Condition != "" {
			b.WriteString(" if " + r.BP.Condition)
		}
		if r.BP.HitCondition != "" {
			b.WriteString(" hit " + r.BP.HitCondition)
		}
		if r.BP.LogMessage != "" {
			fmt.Fprintf(&b, " log %q", r.BP.LogMessage)
		}
		if r.Verified != nil && !*r.Verified {
			b.WriteString(" (not confirmed by the debugger)")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// aiDebugFileConfig builds a debug configuration for one file from its
// language (debug_start(path)). Go debugs the file's package folder.
func aiDebugFileConfig(root, abs string) (launchConfig, error) {
	rel := aiRootRel(root, abs)
	name := "Debug " + rel
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".go":
		// outputMode remote: the program's output comes as DAP output
		// events (Debug Console, debug_state), not on dlv's stdout.
		extra := map[string]any{"outputMode": "remote"}
		if strings.HasSuffix(abs, "_test.go") {
			extra["mode"] = "test"
		}
		return dapConfig(name, "delve", filepath.Dir(abs), extra), nil
	case ".py":
		return dapConfig(name, "python", abs, map[string]any{"console": "internalConsole", "cwd": root}), nil
	case ".js", ".mjs", ".cjs", ".ts":
		return dapConfig(name, "pwa-node", abs, map[string]any{"cwd": root}), nil
	}
	return launchConfig{}, fmt.Errorf("no debugger for %s files; add a configuration to .termocode/launch.json and use debug_start(config)", filepath.Ext(abs))
}

// debugConfigNames lists the names of the debuggable configurations.
func debugConfigNames(list []launchConfig) []string {
	var out []string
	for _, c := range list {
		if c.Debuggable() {
			out = append(out, c.Name)
		}
	}
	return out
}

// aiDebugFindConfig finds a configuration by name (exact, then any case).
func aiDebugFindConfig(list []launchConfig, name string) (launchConfig, error) {
	found, ok := launchConfig{}, false
	for _, c := range list {
		if c.Name == name {
			found, ok = c, true
			break
		}
	}
	if !ok {
		for _, c := range list {
			if strings.EqualFold(c.Name, name) {
				found, ok = c, true
				break
			}
		}
	}
	if ok && !found.Debuggable() {
		return launchConfig{}, fmt.Errorf("%q is a run command, not a debug configuration; use tasks_run or terminal_send to run it", found.Name)
	}
	if !ok {
		names := debugConfigNames(list)
		q := make([]string, len(names))
		for i, n := range names {
			q[i] = fmt.Sprintf("%q", n)
		}
		if len(q) == 0 {
			return launchConfig{}, fmt.Errorf("no configuration %q and no debug configurations; use debug_start(path)", name)
		}
		return launchConfig{}, fmt.Errorf("no configuration %q; debug configurations: %s", name, strings.Join(q, ", "))
	}
	return found, nil
}

// ── Model side (Update goroutine) ────────────────────────────────────────

// aiDebugLuaFn hands the nvim client to the tool goroutine.
func aiDebugLuaFn(ctx context.Context, call aiIDECaller) (aiDbgLua, error) {
	var lua aiDbgLua
	_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if m.nvim == nil {
			return "", nil, errors.New("the editor is not running")
		}
		c := m.nvim
		lua = func(code string, args ...any) (string, error) { return c.EvalLuaStringArgs(code, args...) }
		return "", nil, nil
	})
	return lua, err
}

// aiDebugStatus reads _termocode_dap.ai_status().
func aiDebugStatus(lua aiDbgLua) (aiDbgStatus, error) {
	raw, err := lua(`return _G._termocode_dap and _G._termocode_dap.ai_status and _G._termocode_dap.ai_status() or ''`)
	if err != nil {
		return aiDbgStatus{}, err
	}
	var st aiDbgStatus
	if raw == "" || json.Unmarshal([]byte(raw), &st) != nil {
		return aiDbgStatus{}, errors.New("the debugger is not set up yet")
	}
	return st, nil
}

// aiDebugExtraNow runs on Update: watches, breakpoint count, configs and
// the last program output.
func (m *Model) aiDebugExtraNow() ([]string, aiDbgExtra) {
	d := m.ensureDebug()
	var watches []string
	for _, w := range d.watches {
		watches = append(watches, w.Expr)
	}
	x := aiDbgExtra{Configs: debugConfigNames(d.runConfigs())}
	for _, l := range d.bps {
		x.Breakpoints += len(l)
	}
	for _, l := range d.console.Lines() {
		switch {
		case l.Category == "console" && strings.HasPrefix(l.Text, "Starting "):
			x.Output = nil // only the newest session's output
		case l.Category == "stdout" || l.Category == "stderr": // the program and adapter errors, not status lines
			x.Output = append(x.Output, l.Text)
		}
	}
	if len(x.Output) > aiDebugOutputLines {
		x.Output = x.Output[len(x.Output)-aiDebugOutputLines:]
	}
	return watches, x
}

// aiTailLines is the last n lines of s.
func aiTailLines(s string, n int) []string {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// aiDebugReport is debug_state on the tool goroutine.
func aiDebugReport(ctx context.Context, call aiIDECaller, lua aiDbgLua, root string, depth int) (aiDbgState, string, error) {
	var watches []string
	var extra aiDbgExtra
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		watches, extra = m.aiDebugExtraNow()
		return "", nil, nil
	}); err != nil {
		return aiDbgState{}, "", err
	}
	if depth < 0 {
		depth = 0
	}
	if depth > aiDebugMaxDepth {
		depth = aiDebugMaxDepth
	}
	if watches == nil {
		watches = []string{}
	}
	opts, _ := json.Marshal(map[string]any{"frames": aiDebugMaxFrames, "depth": depth, "max_vars": aiDebugMaxVars,
		"watches": watches, "timeout_ms": aiDebugStateTimeout})
	raw, err := lua(`local j = ...; return _G._termocode_dap and _G._termocode_dap.ai_state and _G._termocode_dap.ai_state(j) or ''`, string(opts))
	if err != nil {
		return aiDbgState{}, "", err
	}
	var st aiDbgState
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &st); err != nil {
			return aiDbgState{}, "", fmt.Errorf("bad debugger state: %v", err)
		}
	}
	switch {
	case st.Active:
		extra.Output = nil
	case strings.TrimSpace(st.Output) != "":
		// The adapter's own copy: complete even when the Debug Console
		// has not seen the last output events yet.
		extra.Output = aiTailLines(st.Output, aiDebugOutputLines)
	}
	return st, aiDebugStateReport(st, root, extra), nil
}

// aiDebugFollow shows the stopped line in the editor, like the UI, and
// keeps the user's focus where it was.
func (m *Model) aiDebugFollow(path string, line int) tea.Cmd {
	if m.nvim == nil || path == "" {
		return m.debugRefresh()
	}
	focus, aiFocused, panel, inTerm := m.focus, m.aiS().focused, m.panelFocused, m.inTerminal
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`local p, l = ...
		if vim.fn.fnamemodify(vim.api.nvim_buf_get_name(0), ':p') ~= p then
			vim.cmd('edit ' .. vim.fn.fnameescape(p))
		end
		pcall(vim.api.nvim_win_set_cursor, 0, { math.max(l, 1), 0 })
		pcall(vim.cmd, 'normal! zz')`, path, line)
	m.focus, m.aiS().focused, m.panelFocused, m.inTerminal = focus, aiFocused, panel, inTerm
	return m.debugRefresh()
}

// aiDebugAfter waits after op, makes the editor follow a new stop and
// returns the state report with a first line about what happened.
func aiDebugAfter(ctx context.Context, call aiIDECaller, lua aiDbgLua, root, op string, before aiDbgStatus, wait time.Duration) (string, error) {
	var now aiDbgStatus
	timedOut, err := aiPollUntil(ctx, wait, aiDebugPoll, func() (bool, error) {
		st, err := aiDebugStatus(lua)
		if err != nil {
			return false, err
		}
		now = st
		return aiDebugDone(op, before, st), nil
	})
	if err != nil {
		return "", err
	}
	st, report, err := aiDebugReport(ctx, call, lua, root, 1)
	if err != nil {
		return "", err
	}
	var head string
	newStop := st.Active && st.Stopped && now.Seq > before.Seq
	switch {
	case newStop:
		head = "Paused."
		if len(st.Frames) > 0 {
			top := st.Frames[0]
			head = fmt.Sprintf("Paused at %s; the editor shows the line.", frameLoc(top, root))
			if top.Path != "" {
				if _, err := os.Stat(top.Path); err == nil {
					path, line := top.Path, top.Line
					_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.aiDebugFollow(path, line), nil })
				}
			}
		}
	case !st.Active && op == "start" && now.Msgs > before.Msgs:
		head = "The debug session did not start: " + clipValue(now.Msg) + " (the program output below has the details)"
	case !st.Active && op == "start" && now.Sessions == before.Sessions:
		head = "The debug session did not start; see the Debug Console."
	case !st.Active && op == "start":
		head = "The program ran to its end without stopping at a breakpoint."
	case !st.Active:
		head = "The debug session ended."
	case timedOut:
		head = fmt.Sprintf("Still running after %s (no breakpoint hit). Call debug_state later, or debug_control(pause).", wait)
	default:
		head = "Running."
	}
	_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.debugRefresh(), nil })
	return head + "\n" + report, nil
}

// aiDebugStartNow runs on Update: resolve the configuration and start it.
func (m *Model) aiDebugStartNow(lua aiDbgLua, root, name, path string) (string, tea.Cmd, aiDbgStatus, error) {
	before, err := aiDebugStatus(lua)
	if err != nil {
		return "", nil, before, err
	}
	if !before.Available {
		return "", nil, before, errors.New("the debugger (nvim-dap) is not downloaded yet")
	}
	if before.Active {
		return "", nil, before, fmt.Errorf("a debug session (%q) is already running; use debug_control, or debug_control(stop) first", before.Name)
	}
	d := m.ensureDebug()
	d.configs.invalidate()
	var cfg launchConfig
	switch {
	case name != "":
		if cfg, err = aiDebugFindConfig(d.runConfigs(), name); err != nil {
			return "", nil, before, err
		}
	case path != "":
		if cfg, err = aiDebugFileConfig(root, path); err != nil {
			return "", nil, before, err
		}
	default:
		if sel, _, ok := d.selectedConfig(); ok && sel.Debuggable() {
			cfg = sel
		} else if p := m.editor.Path(); p != "" {
			if cfg, err = aiDebugFileConfig(root, p); err != nil {
				return "", nil, before, err
			}
		} else {
			return "", nil, before, errors.New("no debug configuration and no open file; pass config or path")
		}
	}
	cmd, err := m.tryDebugStartConfig(cfg)
	if err != nil {
		return "", cmd, before, err
	}
	return cfg.Name, cmd, before, nil
}

// aiDebugControlNow runs on Update: one control op on the live session.
func (m *Model) aiDebugControlNow(lua aiDbgLua, op string) (tea.Cmd, aiDbgStatus, bool, error) {
	st, err := aiDebugStatus(lua)
	if err != nil {
		return nil, st, false, err
	}
	if !st.Active {
		return nil, st, false, errors.New("no debug session is running; start one with debug_start")
	}
	switch op {
	case "continue", "step_over", "step_into", "step_out":
		if !st.Stopped {
			return nil, st, false, errors.New("the program is running, not paused; use debug_control(pause) first")
		}
	case "pause":
		if st.Stopped {
			return nil, st, true, nil // already paused
		}
	case "stop":
		return m.dapStop(), st, false, nil
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLua(fmt.Sprintf(`local ok, dap = pcall(require, 'dap'); if ok then dap.%s() end`, op))
	return m.debugRefresh(), st, false, nil
}

// aiDebugSetBreakpoint runs on Update: debug_breakpoint set.
func (m *Model) aiDebugSetBreakpoint(lua aiDbgLua, abs string, line int, cond, logMsg string) error {
	res, err := lua(`local p, l, c, g = ...
		if not (_G._termocode_dap and _G._termocode_dap.set_bp) then return 'the debugger is not set up yet' end
		return _G._termocode_dap.set_bp(p, l, c, g)`, abs, line, cond, logMsg)
	if err != nil {
		return err
	}
	if res != "" {
		return errors.New(res)
	}
	m.debugSyncBreakpointsNow()
	return nil
}

// debugSyncBreakpointsNow is debugSyncBreakpointsCmd done at once (the
// AI tool's answer must see the new store).
func (m *Model) debugSyncBreakpointsNow() {
	if m.nvim == nil {
		return
	}
	raw, err := m.nvim.EvalLuaString(`return _G._termocode_dap and _G._termocode_dap.breakpoints and _G._termocode_dap.breakpoints() or ''`)
	if err != nil || raw == "" {
		return
	}
	var live liveBreakpoints
	if json.Unmarshal([]byte(raw), &live) == nil {
		m.debugApplyLiveBreakpoints(live)
	}
}

// hasBreakpoint reports whether the store has a breakpoint at path:line.
func (d *debugState) hasBreakpoint(path string, line int) bool {
	for _, bp := range d.bps[path] {
		if bp.Line == line {
			return true
		}
	}
	return false
}

// aiDebugBreakpoint runs on Update: debug_breakpoint.
func (m *Model) aiDebugBreakpoint(lua aiDbgLua, root, op, abs string, line int, cond, logMsg string) (string, tea.Cmd, error) {
	d := m.ensureDebug()
	m.debugSyncBreakpointsNow()
	rel := aiRootRel(root, abs)
	switch op {
	case "list":
		return aiDebugBreakpointReport(d.bps, d.liveBps, root), nil, nil
	case "set":
		if err := m.aiDebugSetBreakpoint(lua, abs, line, cond, logMsg); err != nil {
			return "", nil, err
		}
		what := "breakpoint"
		switch {
		case logMsg != "":
			what = "logpoint (" + logMsg + ")"
		case cond != "":
			what = "conditional breakpoint (if " + cond + ")"
		}
		text := ""
		for _, bp := range d.bps[abs] {
			if bp.Line == line && bp.Text != "" {
				text = ": " + clipValue(bp.Text)
			}
		}
		return fmt.Sprintf("set a %s at %s:%d%s. It shows in the gutter and the Run & Debug view.", what, rel, line, text), nil, nil
	case "remove":
		if !d.hasBreakpoint(abs, line) {
			return "", nil, fmt.Errorf("no breakpoint at %s:%d; debug_breakpoint(op: \"list\") lists them", rel, line)
		}
		cmd := m.debugRemoveBreakpoint(abs, line)
		m.debugSyncBreakpointsNow()
		return fmt.Sprintf("removed the breakpoint at %s:%d", rel, line), cmd, nil
	case "clear":
		if abs == "" {
			n := 0
			for _, l := range d.bps {
				n += len(l)
			}
			cmd := m.debugClearBreakpoints()
			return fmt.Sprintf("removed all %d breakpoints", n), cmd, nil
		}
		var lines []int
		for _, bp := range d.bps[abs] {
			lines = append(lines, bp.Line)
		}
		var cmds []tea.Cmd
		for _, l := range lines {
			cmds = append(cmds, m.debugRemoveBreakpoint(abs, l))
		}
		m.debugSyncBreakpointsNow()
		return fmt.Sprintf("removed %d breakpoints in %s", len(lines), rel), tea.Batch(cmds...), nil
	}
	return "", nil, fmt.Errorf("unknown op %q", op)
}

// ── Tool definitions ─────────────────────────────────────────────────────

func aiDebugWaitArg(a map[string]any, def time.Duration) time.Duration {
	if _, ok := a["wait_ms"]; !ok {
		return def
	}
	return aiWaitArg(a)
}

// aiDebugTools are debug_breakpoint, debug_state, debug_start,
// debug_control and debug_evaluate.
func aiDebugTools(root string, call aiIDECaller) []ai.Tool {
	ws := ai.Workspace{Root: root}
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	// file resolves a workspace file that must exist.
	file := func(p string) (string, error) {
		abs, err := ws.Resolve(p)
		if err != nil {
			return "", err
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("%s does not exist", p)
		}
		if fi.IsDir() {
			return "", fmt.Errorf("%s is a folder, not a file", p)
		}
		return abs, nil
	}
	return []ai.Tool{
		{
			Name: "debug_breakpoint",
			Description: "Breakpoints of the debugger (also used by a running session). op: list; set (path, line, " +
				"optional condition or log_message — a logpoint prints instead of stopping); remove (path, line); " +
				"clear (all, or only path). They show in the editor gutter and the Run & Debug view and are saved.",
			Params: map[string]any{
				"op":          map[string]any{"type": "string", "enum": aiDebugBpOps, "description": "what to do"},
				"path":        str("file (set: default the open file; clear: only this file)"),
				"line":        num("1-based line (set, remove)"),
				"condition":   str("set: stop only when this expression is true"),
				"log_message": str("set: print this message instead of stopping; {expr} is replaced by its value"),
			},
			Required: []string{"op"},
			Gate: func(a map[string]any) (bool, string) {
				if ai.ArgString(a, "op") == "clear" {
					return true, ai.ClassIDE // removes many breakpoints the user placed
				}
				return false, ""
			},
			Validate: func(a map[string]any) error {
				op := ai.ArgString(a, "op")
				if !contains(aiDebugBpOps, op) {
					return fmt.Errorf("unknown op %q; use one of %s", op, strings.Join(aiDebugBpOps, ", "))
				}
				if (op == "set" || op == "remove") && ai.ArgInt(a, "line") < 1 {
					return fmt.Errorf("op %s needs line (1-based)", op)
				}
				if op == "remove" && ai.ArgString(a, "path") == "" {
					return errors.New("op remove needs path and line")
				}
				return nil
			},
			Describe: func(a map[string]any) string {
				op, p, l := ai.ArgString(a, "op"), ai.ArgString(a, "path"), ai.ArgInt(a, "line")
				switch op {
				case "set":
					return fmt.Sprintf("Set breakpoint %s:%d", p, l)
				case "remove":
					return fmt.Sprintf("Remove breakpoint %s:%d", p, l)
				case "clear":
					if p != "" {
						return "Remove all breakpoints in " + p
					}
					return "Remove all breakpoints"
				}
				return "List breakpoints"
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				op, line := ai.ArgString(a, "op"), ai.ArgInt(a, "line")
				cond, logMsg := ai.ArgString(a, "condition"), ai.ArgString(a, "log_message")
				lua, err := aiDebugLuaFn(ctx, call)
				if err != nil {
					return "", err
				}
				p := ai.ArgString(a, "path")
				abs := ""
				if p != "" {
					if abs, err = file(p); err != nil {
						return "", err
					}
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					if abs == "" && op == "set" {
						if abs = m.editor.Path(); abs == "" {
							return "", nil, errors.New("no file is open; pass path")
						}
					}
					return m.aiDebugBreakpoint(lua, root, op, abs, line, cond, logMsg)
				})
			},
		},
		{
			Name: "debug_state",
			Description: "The debug session: running or paused, stop reason, thread, top stack frames (file:line, function), " +
				"the variables of the top frame (depth: child levels, default 1, max 3; capped) and the watch values. " +
				"Without a session: the last exit code, breakpoints and the debug configurations.",
			Params:   map[string]any{"depth": num("child levels under each variable (default 1, max 3)")},
			Describe: func(map[string]any) string { return "Read the debug state" },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				lua, err := aiDebugLuaFn(ctx, call)
				if err != nil {
					return "", err
				}
				depth := 1
				if _, ok := a["depth"]; ok {
					depth = ai.ArgInt(a, "depth")
				}
				_, report, err := aiDebugReport(ctx, call, lua, root, depth)
				return report, err
			},
		},
		{
			Name: "debug_start",
			Description: "Start debugging (Run & Debug). config: a launch configuration by name (.termocode/launch.json or " +
				"auto-detected; debug_state lists them); or path: debug this file (Go: its package) with a configuration " +
				"for its language; neither: the selected configuration or the open file. Set breakpoints first. " +
				"Waits (wait_ms, default 30000) for the first stop and returns the state.",
			Params: map[string]any{
				"config":  str("launch configuration name"),
				"path":    str("file to debug"),
				"wait_ms": num("wait up to this long for the first stop (default 30000, max 120000)"),
			},
			Mutating: true, Class: ai.ClassRun,
			Validate: func(a map[string]any) error {
				if ai.ArgString(a, "config") != "" && ai.ArgString(a, "path") != "" {
					return errors.New("pass config or path, not both")
				}
				return nil
			},
			Describe: func(a map[string]any) string {
				if c := ai.ArgString(a, "config"); c != "" {
					return "Start debugging: " + c
				}
				if p := ai.ArgString(a, "path"); p != "" {
					return "Start debugging " + p
				}
				return "Start debugging"
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				lua, err := aiDebugLuaFn(ctx, call)
				if err != nil {
					return "", err
				}
				abs := ""
				if p := ai.ArgString(a, "path"); p != "" {
					if abs, err = file(p); err != nil {
						return "", err
					}
				}
				name := ai.ArgString(a, "config")
				var before aiDbgStatus
				started, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
					n, cmd, b, err := m.aiDebugStartNow(lua, root, name, abs)
					before = b
					return n, cmd, err
				})
				if err != nil {
					return "", err
				}
				out, err := aiDebugAfter(ctx, call, lua, root, "start", before, aiDebugWaitArg(a, aiDebugStartWait))
				return "Started " + fmt.Sprintf("%q", started) + ".\n" + out, err
			},
		},
		{
			Name: "debug_control",
			Description: "Control the paused / running debug session: continue, pause, step_over, step_into, step_out, " +
				"stop, restart. Waits (wait_ms, default 10000) for the next stop or the end and returns the new state.",
			Params: map[string]any{
				"op":      map[string]any{"type": "string", "enum": aiDebugControlOps, "description": "what to do"},
				"wait_ms": num("wait up to this long for the next stop (default 10000, max 120000)"),
			},
			Required: []string{"op"},
			Gate: func(a map[string]any) (bool, string) {
				switch ai.ArgString(a, "op") {
				case "pause", "stop":
					return true, ai.ClassIDE // like the palette: they run no program code
				}
				return true, ai.ClassRun
			},
			Validate: func(a map[string]any) error {
				if op := ai.ArgString(a, "op"); !contains(aiDebugControlOps, op) {
					return fmt.Errorf("unknown op %q; use one of %s", op, strings.Join(aiDebugControlOps, ", "))
				}
				return nil
			},
			Describe: func(a map[string]any) string {
				return "Debug: " + strings.ReplaceAll(ai.ArgString(a, "op"), "_", " ")
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				op := ai.ArgString(a, "op")
				lua, err := aiDebugLuaFn(ctx, call)
				if err != nil {
					return "", err
				}
				var before aiDbgStatus
				var already bool
				if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
					cmd, b, al, err := m.aiDebugControlNow(lua, op)
					before, already = b, al
					return "", cmd, err
				}); err != nil {
					return "", err
				}
				if already {
					_, report, err := aiDebugReport(ctx, call, lua, root, 1)
					return "Already paused.\n" + report, err
				}
				return aiDebugAfter(ctx, call, lua, root, op, before, aiDebugWaitArg(a, aiDebugControlWait))
			},
		},
		{
			Name: "debug_evaluate",
			Description: "Evaluate an expression in the paused program's current frame (like the Debug Console). It can " +
				"call functions and change the program's state. Returns the value, its type and its first fields.",
			Params:   map[string]any{"expr": str("expression in the program's language")},
			Required: []string{"expr"},
			Mutating: true, Class: ai.ClassRun,
			Validate: func(a map[string]any) error {
				if strings.TrimSpace(ai.ArgString(a, "expr")) == "" {
					return errors.New("expr is required")
				}
				return nil
			},
			Describe: func(a map[string]any) string { return "Evaluate in debugger: " + ai.ArgString(a, "expr") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				expr := strings.TrimSpace(ai.ArgString(a, "expr"))
				lua, err := aiDebugLuaFn(ctx, call)
				if err != nil {
					return "", err
				}
				return aiDebugEvaluate(ctx, call, lua, expr)
			},
		},
	}
}

// aiDbgEval is the decoded _termocode_dap.ai_eval().
type aiDbgEval struct {
	Error    string     `json:"error"`
	Result   string     `json:"result"`
	Type     string     `json:"type"`
	Children []aiDbgVar `json:"children"`
	More     int        `json:"more"`
}

// aiDebugEvaluate is debug_evaluate on the tool goroutine. The line and
// its result also go to the Debug Console, as if the user typed it.
func aiDebugEvaluate(ctx context.Context, call aiIDECaller, lua aiDbgLua, expr string) (string, error) {
	st, err := aiDebugStatus(lua)
	if err != nil {
		return "", err
	}
	switch {
	case !st.Active:
		return "", errors.New("no debug session is running; start one with debug_start")
	case !st.Stopped:
		return "", errors.New("the program is running; pause it first (debug_control pause) or wait for a breakpoint")
	}
	raw, err := lua(`local e = ...; return _G._termocode_dap.ai_eval(e, 5000)`, expr)
	if err != nil {
		return "", err
	}
	var r aiDbgEval
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return "", fmt.Errorf("bad answer from the debugger: %v", err)
	}
	_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
		c := m.ensureDebug().console
		c.AppendOutput("input", "› "+expr+"\n")
		if r.Error != "" {
			c.AppendOutput("stderr", r.Error+"\n")
		} else {
			c.AppendOutput("result", r.Result+"\n")
		}
		return "", m.debugRefresh(), nil // the expression may have changed variables
	})
	if r.Error != "" {
		return "", errors.New("evaluate failed: " + r.Error)
	}
	var b strings.Builder
	b.WriteString(expr + " = " + clipValue(r.Result))
	if r.Type != "" {
		b.WriteString(" (" + r.Type + ")")
	}
	b.WriteString("\n")
	writeDbgVars(&b, r.Children, r.More, "  ")
	return strings.TrimRight(b.String(), "\n"), nil
}
