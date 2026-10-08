package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// nvimDapRepo is the upstream URL for mfussenegger/nvim-dap, the de-facto
// DAP client for nvim. We bootstrap-clone it on first launch so users don't
// need to maintain a plugin manager separately.
const nvimDapRepo = "https://github.com/mfussenegger/nvim-dap.git"

// ensureNvimDap clones mfussenegger/nvim-dap into the plugins dir if it's not
// already present. Mirrors ensureVimVisualMulti's idempotent / non-fatal
// contract: failures (no git, no network) return an empty path with a
// human-readable warn string so the editor still launches without DAP.
func ensureNvimDap() (path string, warn string) {
	root, err := pluginsDir()
	if err != nil {
		return "", fmt.Sprintf("dap: locate plugins dir: %v", err)
	}
	dest := filepath.Join(root, "nvim-dap")

	if st, err := os.Stat(filepath.Join(dest, ".git")); err == nil && st.IsDir() {
		return dest, ""
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Sprintf("dap: mkdir %s: %v", root, err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", "dap: git not on PATH; skipping nvim-dap clone"
	}

	cmd := exec.Command("git", "clone", "--depth", "1", "--quiet", nvimDapRepo, dest)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dest)
		return "", fmt.Sprintf("dap: clone failed (%v); editor will run without DAP", err)
	}
	return dest, ""
}

// dapToggleBreakpoint toggles a breakpoint at the cursor line via nvim-dap
// (works without a session — nvim-dap records the line and sends it once a
// session starts), then persists the new set (debug_breakpoints.go).
func (m *Model) dapToggleBreakpoint() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLua(`if _G._termocode_dap and _G._termocode_dap.toggle then _G._termocode_dap.toggle() end`)
	return m.debugSyncBreakpointsCmd()
}

// dapSetSpecialBreakpoint places a conditional breakpoint (cond) or a
// logpoint (log) on the cursor line, replacing any breakpoint there.
func (m *Model) dapSetSpecialBreakpoint(cond, log string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`local c, l = ...; if _G._termocode_dap and _G._termocode_dap.toggle then _G._termocode_dap.toggle(c, l) end`, cond, log)
	return m.debugSyncBreakpointsCmd()
}

// dapStart is Start / Continue (Alt+F5, Run view ▶, palette):
//   - paused session → continue;
//   - no session → the selected Run-view configuration (launch.json or
//     auto-detected); without one, nvim-dap's configurations for the
//     current filetype (a picker when several match).
func (m *Model) dapStart() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	d := m.ensureDebug()
	if m.dapSessionActive {
		if d.snap.Stopped {
			return m.dapContinue()
		}
		return m.debugToast("session is running — Shift+Alt+F5 stops it")
	}
	if cfg, _, ok := d.selectedConfig(); ok && cfg.Debuggable() {
		return m.debugStartConfig(cfg)
	}
	return m.debugStartForFiletype()
}

// debugStartConfig starts a launch.json-style config through nvim-dap.
func (m *Model) debugStartConfig(cfg launchConfig) tea.Cmd {
	d := m.ensureDebug()
	if msg := missingAdapterMsg(dapTypeLang(cfg.Type)); msg != "" {
		d.hint = msg
		return m.debugErrorToast(msg)
	}
	raw := map[string]any{}
	for k, v := range cfg.Raw {
		raw[k] = v
	}
	if _, ok := raw["name"]; !ok {
		raw["name"] = cfg.Name
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return m.debugErrorToast(err.Error())
	}
	m.ensureEditorWindowCurrent() // ${file} resolves against the current buffer
	res, err := m.nvim.EvalLuaStringArgs(`local j = ...
		if not (_G._termocode_dap and _G._termocode_dap.run) then return 'DAP not initialized' end
		return _G._termocode_dap.run(j)`, string(data))
	if err != nil {
		return m.debugErrorToast(err.Error())
	}
	if res != "" {
		if strings.Contains(res, "no debug adapter") {
			d.hint = res + " — Install Adapter…"
		}
		d.console.AppendOutput("stderr", res+"\n")
		return m.debugErrorToast(res)
	}
	return m.debugStarted(cfg.Name)
}

// debugStarted is the shared bookkeeping after a successful dap.run.
func (m *Model) debugStarted(name string) tea.Cmd {
	d := m.ensureDebug()
	d.hint = ""
	d.status = "Starting…"
	d.console.AppendOutput("console", "Starting "+name+"…\n")
	m.showDebugConsole(false)
	if m.showExp {
		m.activity.SetActive(activity.ViewRun)
	}
	return tea.Batch(m.debugToast("starting "+name), m.debugRefresh())
}

// dapFtConfig is one entry of _termocode_dap.configs(ft).
type dapFtConfig struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Type  string `json:"type"`
}

// debugStartForFiletype runs nvim-dap's own configuration for the current
// buffer's filetype, asking with a picker when there are several.
func (m *Model) debugStartForFiletype() tea.Cmd {
	m.ensureEditorWindowCurrent()
	ft, _ := m.nvim.EvalLuaString(`return vim.bo.filetype`)
	lang := strings.ToLower(ft)
	if lang == "" {
		lang = strings.ToLower(m.editor.Lang())
	}
	if msg := m.dapMissingAdapterToast(lang); msg != "" {
		m.ensureDebug().hint = msg
		return m.debugErrorToast(msg)
	}
	raw, err := m.nvim.EvalLuaStringArgs(`local ft = ...; return _G._termocode_dap and _G._termocode_dap.configs(ft) or '[]'`, ft)
	if err != nil {
		return m.debugErrorToast(err.Error())
	}
	var cfgs []dapFtConfig
	_ = json.Unmarshal([]byte(raw), &cfgs)
	switch len(cfgs) {
	case 0:
		label := ft
		if label == "" {
			label = "this file"
		}
		return m.debugErrorToast("no debug configuration for " + label + " — palette → Debug: Open launch.json")
	case 1:
		return m.debugStartFt(ft, cfgs[0].Index, cfgs[0].Name)
	}
	items := make([]picker.Item, 0, len(cfgs))
	for _, c := range cfgs {
		items = append(items, picker.Item{ID: fmt.Sprintf("ft:%d:%s", c.Index, ft), Title: c.Name, Hint: c.Type})
	}
	m.picker = picker.NewItems(" Start Debugging ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDebugConfig
	return m.picker.Init()
}

// debugStartFt starts dap.configurations[ft][index].
func (m *Model) debugStartFt(ft string, index int, name string) tea.Cmd {
	m.ensureEditorWindowCurrent()
	res, err := m.nvim.EvalLuaStringArgs(`local ft, i = ...; return _G._termocode_dap.run_ft(ft, i)`, ft, index)
	if err != nil {
		return m.debugErrorToast(err.Error())
	}
	if res != "" {
		return m.debugErrorToast(res)
	}
	return m.debugStarted(name)
}

// openDebugConfigPicker lists the Run view configurations; picking one
// selects it (Start runs it).
func (m *Model) openDebugConfigPicker() tea.Cmd {
	d := m.ensureDebug()
	d.configs.invalidate()
	cfgs := d.runConfigs()
	if len(cfgs) == 0 {
		return m.debugOpenLaunchJSON()
	}
	items := make([]picker.Item, 0, len(cfgs))
	for i, c := range cfgs {
		hint := c.Type
		if c.Source == "launch.json" {
			hint += " · launch.json"
		}
		items = append(items, picker.Item{ID: fmt.Sprintf("cfg:%d", i), Title: c.Name, Hint: hint})
	}
	m.picker = picker.NewItems(" Select Configuration ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDebugConfig
	return m.picker.Init()
}

// onDebugConfigPicked handles both config pickers.
func (m *Model) onDebugConfigPicked(id string) tea.Cmd {
	d := m.ensureDebug()
	switch {
	case strings.HasPrefix(id, "cfg:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(id, "cfg:"))
		cfgs := d.runConfigs()
		if i < 0 || i >= len(cfgs) {
			return nil
		}
		d.selected = cfgs[i].Name
		d.hint = adapterHintFor(cfgs[i])
		return m.debugToast("configuration: " + cfgs[i].Name)
	case strings.HasPrefix(id, "ft:"):
		rest := strings.TrimPrefix(id, "ft:")
		sep := strings.IndexByte(rest, ':')
		if sep < 0 {
			return nil
		}
		i, _ := strconv.Atoi(rest[:sep])
		return m.debugStartFt(rest[sep+1:], i, "debug session")
	}
	return nil
}

// dapStop terminates the session (Shift+Alt+F5, toolbar ■). When the
// adapter does not answer within 3s the session is force-closed.
func (m *Model) dapStop() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	if !m.dapSessionActive {
		return m.debugToast("no active session")
	}
	_ = m.nvim.ExecLua(`local ok, dap = pcall(require, 'dap'); if ok and dap.session() then dap.terminate() end`)
	m.ensureDebug().status = "Stopping…"
	force := tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return debugMsg{apply: func(m *Model) tea.Cmd {
			if !m.dapSessionActive || m.nvim == nil {
				return nil
			}
			_ = m.nvim.ExecLua(`local ok, dap = pcall(require, 'dap'); if ok and dap.session() then dap.close() end`)
			return m.debugRefresh()
		}}
	})
	return tea.Batch(m.debugToast("stopping session"), m.debugRefresh(), force)
}

// dapStepOver / dapStepInto / dapStepOut / dapContinue / dapPause /
// dapRestart wrap the matching nvim-dap functions. Steps need a paused
// session; the toolbar status and the ▶ sign show the result.
func (m *Model) dapStepOver() tea.Cmd { return m.dapInvoke("step_over", true) }
func (m *Model) dapStepInto() tea.Cmd { return m.dapInvoke("step_into", true) }
func (m *Model) dapStepOut() tea.Cmd  { return m.dapInvoke("step_out", true) }
func (m *Model) dapContinue() tea.Cmd {
	if !m.dapSessionActive {
		return m.dapStart()
	}
	return m.dapInvoke("continue", true)
}
func (m *Model) dapPause() tea.Cmd   { return m.dapInvoke("pause", false) }
func (m *Model) dapRestart() tea.Cmd { return m.dapInvoke("restart", false) }

func (m *Model) dapInvoke(fn string, needPaused bool) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	if !m.dapSessionActive {
		return m.debugToast("no active session — start with Alt+F5")
	}
	if needPaused && !m.ensureDebug().snap.Stopped {
		return m.debugToast("program is running — pause it first")
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLua(fmt.Sprintf(`local ok, dap = pcall(require, 'dap'); if ok then dap.%s() end`, fn))
	return m.debugRefresh()
}

// dapShowStack / dapShowVars (palette) open the Run view on that section.
func (m *Model) dapShowStack() tea.Cmd {
	m.openRunView(runSecStack, true)
	return nil
}

func (m *Model) dapShowVars() tea.Cmd {
	m.openRunView(runSecVariables, true)
	return nil
}

// debugRunWithoutDebugging runs the selected configuration as a plain
// command in a new terminal tab.
func (m *Model) debugRunWithoutDebugging() tea.Cmd {
	d := m.ensureDebug()
	cfg, _, ok := d.selectedConfig()
	cmdline := ""
	if ok {
		cmdline = cfg.RunCommand()
	}
	if cmdline == "" {
		return m.debugToast("nothing to run — select a configuration in the Run view")
	}
	root, file := workspaceRoot(), m.editor.Path()
	cmdline = expandLaunchVars(cmdline, root, file)
	cwd := expandLaunchVars(orDefault(cfg.Cwd, "${workspaceFolder}"), root, file)
	m.debugRunInTerminal(cwd, cmdline)
	return nil
}

// debugRunInTerminal opens a fresh terminal tab and types `cd cwd && cmd`.
func (m *Model) debugRunInTerminal(cwd, cmdline string) {
	if m.nvim == nil {
		return
	}
	if m.termOpen {
		m.newTerminalTab()
	} else {
		m.openTerminalPanel()
	}
	m.terminalMinimized = false
	m.applyLayout()
	m.resizeTerminalSplit()
	tab := m.activeTerminalTab()
	if tab == nil {
		return
	}
	line := "cd " + shellQuote(cwd) + " && " + cmdline + "\n"
	_ = m.nvim.ExecLuaArgs(`local buf, text = ...
		if vim.api.nvim_buf_is_loaded(buf) and vim.bo[buf].buftype == 'terminal' then
			local ok, chan = pcall(function() return vim.bo[buf].channel end)
			if ok and chan and chan > 0 then pcall(vim.fn.chansend, chan, text) end
		end`, tab.BufID, line)
}

// debugOpenLaunchJSON opens .termocode/launch.json, creating it from the
// auto-detected configurations when missing.
func (m *Model) debugOpenLaunchJSON() tea.Cmd {
	root := workspaceRoot()
	path := filepath.Join(root, launchJSONRel)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return m.debugErrorToast(err.Error())
		}
		if err := os.WriteFile(path, []byte(launchJSONTemplate(detectLaunchConfigs(root))), 0o644); err != nil {
			return m.debugErrorToast(err.Error())
		}
	}
	m.ensureDebug().configs.invalidate()
	m.openFileAtLine(path, 1)
	return nil
}

// ── Prompts (watch / condition / logpoint) ───────────────────────────────

func (m *Model) openDebugPrompt(kind promptKindEnum, title, label string) {
	m.prompt = prompt.New(title, label, "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = kind
}

func (m *Model) openDebugWatchPrompt() {
	m.openDebugPrompt(promptKindDebugWatch, "Add Watch", "Expression:")
}

// handleDebugPromptSubmit finishes the Group D prompts.
func (m *Model) handleDebugPromptSubmit(value string) tea.Cmd {
	switch m.promptKind {
	case promptKindDebugWatch:
		return m.debugAddWatch(value)
	case promptKindDebugCondition:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return m.dapSetSpecialBreakpoint(value, "")
	case promptKindDebugLogpoint:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return m.dapSetSpecialBreakpoint("", value)
	}
	return nil
}

// runDebugPaletteCmd dispatches the Group D palette entries.
func (m *Model) runDebugPaletteCmd(id string) tea.Cmd {
	switch id {
	case "debug-run-nodebug":
		return m.debugRunWithoutDebugging()
	case "debug-select-config":
		return m.openDebugConfigPicker()
	case "debug-open-launch":
		return m.debugOpenLaunchJSON()
	case "debug-pause":
		return m.dapPause()
	case "debug-restart":
		return m.dapRestart()
	case "debug-conditional-bp":
		m.openDebugPrompt(promptKindDebugCondition, "Conditional Breakpoint", "Condition:")
	case "debug-logpoint":
		m.openDebugPrompt(promptKindDebugLogpoint, "Logpoint", "Message ({expr} is interpolated):")
	case "debug-clear-bps":
		return m.debugClearBreakpoints()
	case "debug-add-watch":
		m.openDebugWatchPrompt()
	case "debug-console":
		m.showDebugConsole(true)
	case "debug-view":
		m.openRunView(runSecConfigs, true)
	}
	return nil
}

// debugPaletteIDs lists the IDs runDebugPaletteCmd handles.
var debugPaletteIDs = map[string]bool{
	"debug-run-nodebug": true, "debug-select-config": true, "debug-open-launch": true,
	"debug-pause": true, "debug-restart": true, "debug-conditional-bp": true,
	"debug-logpoint": true, "debug-clear-bps": true, "debug-add-watch": true,
	"debug-console": true, "debug-view": true,
}

// dapTypeLang maps a DAP adapter type to the language the adapter gate in
// dapMissingAdapterToast understands.
func dapTypeLang(t string) string {
	switch t {
	case "go", "delve":
		return "go"
	case "python", "debugpy":
		return "python"
	case "node", "pwa-node":
		return "javascript"
	}
	return ""
}

// adapterHintFor is the Run view hint for a config whose adapter is
// missing ("" when fine or not a debug config).
func adapterHintFor(c launchConfig) string {
	if !c.Debuggable() {
		return ""
	}
	return missingAdapterMsg(dapTypeLang(c.Type))
}

// dapMissingAdapterToast returns a non-empty string when the current language
// has no usable adapter on PATH — the caller surfaces it as a toast. Empty
// string means "either supported here, or we don't gate this language".
func (m *Model) dapMissingAdapterToast(lang string) string {
	return missingAdapterMsg(lang)
}

// missingAdapterMsg is dapMissingAdapterToast without the Model.
func missingAdapterMsg(lang string) string {
	// Adapters from the managed installer (tools/bin) count as present.
	if lspinstall.AnyAvailable(lspinstall.ForFiletype(lspinstall.CategoryDAP, lang)) {
		return ""
	}
	switch lang {
	case "go":
		if _, err := exec.LookPath("dlv"); err != nil {
			return "DAP: dlv missing — palette → DAP: Install Adapter..."
		}
	case "python":
		py := pythonExecutable()
		if py == "" {
			return "DAP: no python on PATH"
		}
		// Cheap import check; identical to the Lua-side gate.
		check := exec.Command(py, "-c", "import debugpy")
		if err := check.Run(); err != nil {
			return "DAP: debugpy missing — palette → DAP: Install Adapter..."
		}
	case "javascript", "typescript":
		if _, err := exec.LookPath("node"); err != nil {
			return "DAP: install node (https://nodejs.org)"
		}
	default:
		// Languages we don't preconfigure: let nvim-dap surface its own error
		// rather than spam the user with a "not supported" toast for every
		// random filetype.
	}
	return ""
}

func pythonExecutable() string {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}
