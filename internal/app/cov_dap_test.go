package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Coverage of dap.go: the nvim-dap wrappers run against a real headless nvim
// whose `_G._termocode_dap` is replaced by a small recording fake, so no
// adapter or plugin is needed.

// fakeDapLua installs a recording `_termocode_dap` in the model's nvim.
// calls is read back with dapCalls.
func fakeDapLua(t *testing.T, m *Model, runResult, configsJSON, runFtResult string) {
	t.Helper()
	lua := `_G._calls = {}
_G._termocode_dap = {
  toggle = function(c, l) table.insert(_G._calls, 'toggle:' .. tostring(c) .. ':' .. tostring(l)) end,
  run = function(j) table.insert(_G._calls, 'run:' .. j) return ` + luaQuote(runResult) + ` end,
  configs = function(ft) return ` + luaQuote(configsJSON) + ` end,
  run_ft = function(ft, i) table.insert(_G._calls, 'run_ft:' .. ft .. ':' .. i) return ` + luaQuote(runFtResult) + ` end,
}`
	if err := m.nvim.ExecLua(lua); err != nil {
		t.Fatal(err)
	}
}

func consoleText(d *debugState) string {
	var b strings.Builder
	for _, l := range d.console.Lines() {
		b.WriteString(l.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func dapCalls(t *testing.T, m *Model) string {
	t.Helper()
	s, err := m.nvim.EvalLuaString(`return table.concat(_G._calls or {}, '|')`)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// dapModel is a real-nvim model in a temp workspace with a go.mod.
func dapModel(t *testing.T) Model {
	t.Helper()
	t.Chdir(t.TempDir())
	_ = os.WriteFile("go.mod", []byte("module x\n"), 0o644)
	m := newTestModel(t)
	m.w, m.h = 120, 40
	if m.nvim == nil {
		t.Skip("no nvim")
	}
	return m
}

// fakeBin puts a dir with the given executables (name -> script body) alone
// on PATH.
func fakeBin(t *testing.T, scripts map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestCovDapNoNvimIsNoop(t *testing.T) {
	m := Model{}
	if m.dapToggleBreakpoint() != nil || m.dapSetSpecialBreakpoint("a", "") != nil ||
		m.dapStart() != nil || m.dapStop() != nil || m.dapInvoke("pause", false) != nil {
		t.Fatal("no editor: every action must be a no-op")
	}
	if _, err := m.tryDebugStartConfig(launchConfig{}); err == nil {
		t.Fatal("start without editor must fail")
	}
	m.debugRunInTerminal("/x", "ls") // must not panic
}

func TestCovDapToggleAndSpecialBreakpoints(t *testing.T) {
	m := dapModel(t)
	fakeDapLua(t, &m, "", "[]", "")
	m.dapToggleBreakpoint()
	m.dapSetSpecialBreakpoint("x > 1", "")
	m.dapSetSpecialBreakpoint("", "hit {x}")
	got := dapCalls(t, &m)
	for _, want := range []string{"toggle:nil:nil", "toggle:x > 1:", "toggle::hit {x}"} {
		if !strings.Contains(got, want) {
			t.Errorf("calls %q miss %q", got, want)
		}
	}
}

func TestCovDapStartPaths(t *testing.T) {
	m := dapModel(t)
	fakeBin(t, map[string]string{"dlv": "exit 0"})
	fakeDapLua(t, &m, "", "[]", "")
	d := m.ensureDebug()

	// Running (not stopped) session: a hint, no continue.
	m.dapSessionActive = true
	d.snap.Stopped = false
	if m.dapStart() == nil {
		t.Fatal("running session must toast")
	}
	// Stopped session: continue goes through dap.continue (no-op here).
	d.snap.Stopped = true
	if m.dapStart() == nil {
		t.Fatal("paused session continues and refreshes")
	}
	// No session and a debuggable launch.json config: dap.run gets it.
	m.dapSessionActive = false
	_ = os.MkdirAll(".termocode", 0o755)
	_ = os.WriteFile(launchJSONRel, []byte(`{"configurations":[{"name":"Dbg","type":"go","request":"launch","program":"."}]}`), 0o644)
	d.configs.invalidate()
	if m.dapStart() == nil {
		t.Fatal("start must return a command")
	}
	if got := dapCalls(t, &m); !strings.Contains(got, `run:{`) || !strings.Contains(got, `"name":"Dbg"`) {
		t.Errorf("run not called with config: %q", got)
	}
	if d.status != "Starting…" || !strings.Contains(consoleText(d), "Starting Dbg") {
		t.Errorf("status %q", d.status)
	}
	// The Run view is switched when the explorer is shown.
	m.showExp = true
	m.debugStarted("again")
}

func TestCovDapStartConfigErrors(t *testing.T) {
	m := dapModel(t)
	fakeBin(t, map[string]string{"dlv": "exit 0"})
	cfg := launchConfig{Name: "c", Type: "go", Request: "launch", Raw: map[string]any{"program": "."}}

	// Lua returns an error text: toast + console + error result.
	fakeDapLua(t, &m, "boom", "[]", "")
	if _, err := m.tryDebugStartConfig(cfg); err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(consoleText(m.ensureDebug()), "boom") {
		t.Error("error not in the console")
	}
	if m.debugStartConfig(cfg) == nil {
		t.Error("debugStartConfig should toast the error")
	}
	// An adapter-missing answer sets the install hint.
	fakeDapLua(t, &m, "no debug adapter for go", "[]", "")
	_, _ = m.tryDebugStartConfig(cfg)
	if !strings.Contains(m.ensureDebug().hint, "Install Adapter") {
		t.Errorf("hint = %q", m.ensureDebug().hint)
	}
	// DAP chunk missing.
	_ = m.nvim.ExecLua(`_G._termocode_dap = nil`)
	if _, err := m.tryDebugStartConfig(cfg); err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("err = %v", err)
	}
	// Missing adapter on PATH: no dlv.
	fakeBin(t, nil)
	if _, err := m.tryDebugStartConfig(cfg); err == nil || !strings.Contains(err.Error(), "dlv missing") {
		t.Errorf("err = %v", err)
	}
	// A Lua error in dap.run comes back as an error.
	fakeBin(t, map[string]string{"dlv": "exit 0"})
	_ = m.nvim.ExecLua(`_G._termocode_dap = { run = function() error('x') end }`)
	if _, err := m.tryDebugStartConfig(cfg); err == nil {
		t.Error("lua error must fail the start")
	}
	// A config that cannot be marshalled.
	fakeBin(t, map[string]string{"dlv": "exit 0"})
	bad := launchConfig{Name: "c", Type: "go", Raw: map[string]any{"x": make(chan int)}}
	if _, err := m.tryDebugStartConfig(bad); err == nil {
		t.Error("unmarshalable config must fail")
	}
}

func TestCovDapStartForFiletype(t *testing.T) {
	m := dapModel(t)
	fakeBin(t, map[string]string{"dlv": "exit 0"})
	_ = m.nvim.ExecLua(`vim.bo.filetype = 'go'`)

	// no configuration
	fakeDapLua(t, &m, "", "[]", "")
	if m.debugStartForFiletype() == nil {
		t.Fatal("expected an error toast")
	}
	// exactly one: run_ft
	fakeDapLua(t, &m, "", `[{"index":1,"name":"Only","type":"go"}]`, "")
	if m.debugStartForFiletype() == nil {
		t.Fatal("expected a start command")
	}
	if got := dapCalls(t, &m); !strings.Contains(got, "run_ft:go:1") {
		t.Errorf("calls %q", got)
	}
	// several: a picker
	fakeDapLua(t, &m, "", `[{"index":1,"name":"A","type":"go"},{"index":2,"name":"B","type":"go"}]`, "")
	m.debugStartForFiletype()
	if !m.pickerOpen || m.pickerKind != pickerKindDebugConfig {
		t.Fatal("expected the config picker")
	}
	// picking an ft entry starts it
	m.pickerOpen = false
	m.onDebugConfigPicked("ft:2:go")
	if got := dapCalls(t, &m); !strings.HasSuffix(got, "run_ft:go:2") {
		t.Errorf("calls %q", got)
	}
	_ = m.nvim.ExecLua(`_G._termocode_dap = { run_ft = function() error('x') end }`)
	if m.debugStartFt("go", 1, "x") == nil {
		t.Error("run_ft lua error must toast")
	}
	// run_ft failing
	fakeDapLua(t, &m, "", "[]", "no luck")
	if m.debugStartFt("go", 1, "x") == nil {
		t.Error("run_ft error must toast")
	}
	// empty filetype: no config → "this file", falls back to the editor lang
	_ = m.nvim.ExecLua(`vim.bo.filetype = ''`)
	fakeDapLua(t, &m, "", "[]", "")
	if m.debugStartForFiletype() == nil {
		t.Error("expected a toast")
	}
	// missing adapter for the filetype
	_ = m.nvim.ExecLua(`vim.bo.filetype = 'go'`)
	fakeBin(t, nil)
	if m.debugStartForFiletype() == nil || !strings.Contains(m.ensureDebug().hint, "dlv missing") {
		t.Error("missing adapter must set the hint")
	}
	// A dap table without configs() makes the lookup fail.
	fakeBin(t, map[string]string{"dlv": "exit 0"})
	_ = m.nvim.ExecLua(`_G._termocode_dap = { configs = function() error('x') end }`)
	if m.debugStartForFiletype() == nil {
		t.Error("lookup error must toast")
	}
}

func TestCovDapConfigPicker(t *testing.T) {
	m := dapModel(t)
	// No configs at all: opens launch.json (creating it).
	t.Chdir(t.TempDir())
	m.ensureDebug().configs.invalidate()
	if cmd := m.openDebugConfigPicker(); cmd != nil {
		t.Logf("cmd %v", cmd)
	}
	if _, err := os.Stat(launchJSONRel); err != nil {
		t.Fatalf("launch.json not created: %v", err)
	}
	// With configs: a picker listing them.
	_ = os.WriteFile(launchJSONRel, []byte(`{"configurations":[{"name":"A","type":"go","program":"."},{"name":"B","command":"make"}]}`), 0o644)
	m.ensureDebug().configs.invalidate()
	m.openDebugConfigPicker()
	if !m.pickerOpen {
		t.Fatal("picker not opened")
	}
	// Picking one selects it; bad indices and ids are ignored.
	if m.onDebugConfigPicked("cfg:0") == nil || m.ensureDebug().selected != "A" {
		t.Errorf("selected = %q", m.ensureDebug().selected)
	}
	if m.onDebugConfigPicked("cfg:9") != nil || m.onDebugConfigPicked("ft:nocolon") != nil || m.onDebugConfigPicked("other") != nil {
		t.Error("bad ids must be ignored")
	}
}

func TestCovDapStopStepsAndRestart(t *testing.T) {
	m := dapModel(t)
	d := m.ensureDebug()
	if m.dapStop() == nil { // no session
		t.Fatal("no session toast")
	}
	if m.dapInvoke("pause", false) == nil {
		t.Fatal("no session toast")
	}
	if m.dapContinue() == nil { // inactive → start path
		t.Log("continue without session starts")
	}
	m.dapSessionActive = true
	d.snap.Stopped = false
	if m.dapStepOver() == nil || m.dapStepInto() == nil || m.dapStepOut() == nil {
		t.Fatal("steps while running must toast")
	}
	d.snap.Stopped = true
	_ = m.dapStepOver()
	_ = m.dapStepInto()
	_ = m.dapStepOut()
	_ = m.dapContinue()
	_ = m.dapPause()
	_ = m.dapRestart()
	if m.dapStop() == nil || d.status != "Stopping…" {
		t.Fatalf("stop: status %q", d.status)
	}
	// The force-close tick: closes a session that is still active, ignores one that ended.
	m.dapSessionActive = false
	if m.dapForceClose() != nil {
		t.Error("an ended session needs no force close")
	}
	m.dapSessionActive = true
	if m.dapForceClose() == nil {
		t.Error("an active session is force closed and refreshed")
	}
	m.dapSessionActive = false
	if cmd := m.dapStop(); cmd == nil {
		t.Error("inactive stop toasts")
	}
}

func TestCovDapStartNoConfigUsesFiletype(t *testing.T) {
	m := dapModel(t)
	t.Chdir(t.TempDir())
	m.ensureDebug().configs.invalidate()
	fakeDapLua(t, &m, "", "[]", "")
	if m.dapStart() == nil {
		t.Fatal("expected a toast")
	}
}

func TestCovDapShowViews(t *testing.T) {
	m := dapModel(t)
	if m.dapShowStack() != nil || m.dapShowVars() != nil {
		t.Fatal("show views return no command")
	}
	if !m.showExp {
		t.Error("run view not shown")
	}
}

func TestCovDapRunWithoutDebugging(t *testing.T) {
	m := dapModel(t)
	// nothing selectable: auto-detected go.mod configs exist, so use an empty workspace
	empty := t.TempDir()
	t.Chdir(empty)
	m.ensureDebug().configs.invalidate()
	if m.debugRunWithoutDebugging() == nil {
		t.Fatal("nothing to run must toast")
	}
	// A shell config runs in a terminal tab.
	_ = os.MkdirAll(".termocode", 0o755)
	_ = os.WriteFile(launchJSONRel, []byte(`{"configurations":[{"name":"mk","command":"echo hi","cwd":"${workspaceFolder}"}]}`), 0o644)
	m.ensureDebug().configs.invalidate()
	m.ensureDebug().selected = "mk"
	if m.debugRunWithoutDebugging() != nil {
		t.Fatal("run opens a terminal, no command")
	}
	if !m.termOpen {
		t.Error("terminal panel not opened")
	}
	// With the panel already open a new tab is added.
	n := len(m.terminalTabs)
	m.debugRunInTerminal(empty, "echo again")
	if len(m.terminalTabs) <= n {
		t.Errorf("tabs %d -> %d", n, len(m.terminalTabs))
	}
}

func TestCovDapLaunchJSONOpen(t *testing.T) {
	m := dapModel(t)
	m.debugOpenLaunchJSON()
	b, err := os.ReadFile(launchJSONRel)
	if err != nil || !strings.Contains(string(b), "configurations") {
		t.Fatalf("launch.json: %v %q", err, b)
	}
	// An existing file is left alone.
	_ = os.WriteFile(launchJSONRel, []byte("{}"), 0o644)
	if _, err := m.ensureLaunchJSON("."); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(launchJSONRel); string(b) != "{}" {
		t.Errorf("existing launch.json changed: %q", b)
	}
	root := t.TempDir()
	if os.Geteuid() != 0 {
		_ = os.Chmod(root, 0o555)
		if _, err := m.ensureLaunchJSON(root); err == nil {
			t.Error("expected mkdir error")
		}
		_ = os.Chmod(root, 0o755)
	}
	_ = os.MkdirAll(filepath.Join(root, ".termocode"), 0o755)
	_ = os.Chmod(filepath.Join(root, ".termocode"), 0o555)
	defer os.Chmod(filepath.Join(root, ".termocode"), 0o755)
	if os.Geteuid() != 0 {
		if _, err := m.ensureLaunchJSON(root); err == nil {
			t.Error("expected write error")
		}
	}
	// debugOpenLaunchJSON's error toast.
	t.Chdir(root)
	if os.Geteuid() != 0 && m.debugOpenLaunchJSON() == nil {
		t.Error("expected an error toast")
	}
}

func TestCovDapPromptsAndPalette(t *testing.T) {
	m := dapModel(t)
	fakeDapLua(t, &m, "", "[]", "")
	m.promptKind = promptKindDebugWatch
	m.handleDebugPromptSubmit("a+1")
	m.promptKind = promptKindDebugCondition
	if m.handleDebugPromptSubmit("  ") != nil {
		t.Error("blank condition ignored")
	}
	m.handleDebugPromptSubmit("i > 2")
	m.promptKind = promptKindDebugLogpoint
	if m.handleDebugPromptSubmit("") != nil {
		t.Error("blank logpoint ignored")
	}
	m.handleDebugPromptSubmit("msg")
	m.promptKind = promptKindRename
	if m.handleDebugPromptSubmit("x") != nil {
		t.Error("other prompt kinds ignored")
	}
	for id := range debugPaletteIDs {
		m.promptOpen = false
		_ = m.runDebugPaletteCmd(id)
	}
	if m.runDebugPaletteCmd("nope") != nil {
		t.Error("unknown id")
	}
	m.runDebugPaletteCmd("debug-add-watch")
	if !m.promptOpen || m.promptKind != promptKindDebugWatch {
		t.Error("watch prompt not open")
	}
}

func TestCovDapTypeLangAndAdapterTool(t *testing.T) {
	for in, want := range map[string]string{"go": "go", "delve": "go", "python": "python", "debugpy": "python", "node": "javascript", "pwa-node": "javascript", "x": ""} {
		if got := dapTypeLang(in); got != want {
			t.Errorf("dapTypeLang(%q) = %q", in, got)
		}
	}
	if adapterToolFor("") != "" || adapterToolFor("klingon") != "" {
		t.Error("no tool expected")
	}
	if adapterToolFor("go") == "" {
		t.Error("go has a registry adapter")
	}
	var c launchConfig
	if adapterHintFor(c) != "" {
		t.Error("non-debug config has no hint")
	}
}

func TestCovDapMissingAdapterMsg(t *testing.T) {
	fakeBin(t, nil)
	if !strings.Contains(missingAdapterMsg("go"), "dlv missing") {
		t.Error("go")
	}
	if !strings.Contains(missingAdapterMsg("python"), "no python") {
		t.Error("python missing")
	}
	if !strings.Contains(missingAdapterMsg("javascript"), "install node") || !strings.Contains(missingAdapterMsg("typescript"), "install node") {
		t.Error("node")
	}
	if missingAdapterMsg("rust") != "" {
		t.Error("ungated language")
	}
	fakeBin(t, map[string]string{"python3": "exit 1"})
	if !strings.Contains(missingAdapterMsg("python"), "debugpy missing") {
		t.Error("debugpy")
	}
	fakeBin(t, map[string]string{"python": "exit 0", "dlv": "exit 0", "node": "exit 0"})
	if pythonExecutable() == "" {
		t.Error("python fallback name")
	}
	for _, l := range []string{"python", "go", "javascript"} {
		if got := missingAdapterMsg(l); got != "" {
			t.Errorf("%s: %q", l, got)
		}
	}
}

func TestCovDapOpenAdapterInstall(t *testing.T) {
	m := dapModel(t)
	if m.openAdapterInstall("") == nil && !m.pickerOpen {
		t.Log("manager opened as picker or overlay")
	}
	m.pickerOpen = false
	_ = m.openAdapterInstall(adapterToolFor("go"))
	fakeBin(t, nil)
	cfgs, _ := parseLaunchJSON([]byte(`{"configurations":[{"name":"g","type":"go","program":"."}]}`))
	if adapterHintFor(cfgs[0]) == "" {
		t.Error("missing dlv must give a hint")
	}
}
