package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/toast"
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

// dapToggleBreakpoint toggles a breakpoint at the current line via nvim-dap.
// Works even without a running session — nvim-dap just records the line so
// it'll halt there once `dap.continue()` is called.
func (m *Model) dapToggleBreakpoint() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	_ = m.nvim.ExecLua(`local ok, dap = pcall(require, 'dap'); if ok then dap.toggle_breakpoint() end`)
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Info, "DAP", "toggled breakpoint")
	return cmd
}

// dapStart launches the most appropriate configuration for the current file:
//   - `dap.continue()` does the right thing (asks the user to pick when
//     multiple configs match; auto-launches when there's only one).
// We additionally pre-flight that the language has a registered adapter and
// emit a helpful toast otherwise.
func (m *Model) dapStart() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	lang := strings.ToLower(m.editor.Lang())
	if missing := m.dapMissingAdapterToast(lang); missing != "" {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.Push(toast.Errr, missing)
		return cmd
	}
	_ = m.nvim.ExecLua(`local ok, dap = pcall(require, 'dap'); if ok then dap.continue() end`)
	m.dapSessionActive = true
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Info, "DAP", "starting session")
	return cmd
}

// dapStop terminates the current debug session. No-op when no session is
// active; the underlying call is idempotent.
func (m *Model) dapStop() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	_ = m.nvim.ExecLua(`local ok, dap = pcall(require, 'dap'); if ok then dap.terminate(); dap.close() end`)
	m.dapSessionActive = false
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Info, "DAP", "session stopped")
	return cmd
}

// dapStepOver / dapStepInto / dapStepOut / dapContinue are one-line wrappers
// around the matching nvim-dap functions. They report a toast when no
// session is active so the user gets visible feedback rather than silence.
func (m *Model) dapStepOver() tea.Cmd  { return m.dapInvoke("step_over", "step over") }
func (m *Model) dapStepInto() tea.Cmd  { return m.dapInvoke("step_into", "step into") }
func (m *Model) dapStepOut() tea.Cmd   { return m.dapInvoke("step_out", "step out") }
func (m *Model) dapContinue() tea.Cmd  { return m.dapInvoke("continue", "continue") }

func (m *Model) dapInvoke(fn, label string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	if !m.dapSessionActive {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, "DAP", "no active session — start with 'Debug: Start'")
		return cmd
	}
	_ = m.nvim.ExecLua(fmt.Sprintf(`local ok, dap = pcall(require, 'dap'); if ok then dap.%s() end`, fn))
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Info, "DAP", label)
	return cmd
}

// dapShowStack opens the preview overlay with the current call stack. Pulls
// formatted text from the Lua helper so the Go side stays language-agnostic.
func (m *Model) dapShowStack() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		body, err := c.EvalLuaString(`return _G._termocode_dap and _G._termocode_dap.format_stack() or 'DAP not initialized.'`)
		if err != nil {
			return PreviewMsg{Title: " Call Stack ", Body: "Failed to fetch stack: " + err.Error()}
		}
		if body == "" {
			body = "(empty)"
		}
		return PreviewMsg{Title: " Call Stack ", Body: body}
	}
}

// dapShowVars opens the preview overlay with the current frame's variables.
func (m *Model) dapShowVars() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		body, err := c.EvalLuaString(`return _G._termocode_dap and _G._termocode_dap.format_vars() or 'DAP not initialized.'`)
		if err != nil {
			return PreviewMsg{Title: " Variables ", Body: "Failed to fetch variables: " + err.Error()}
		}
		if body == "" {
			body = "(empty)"
		}
		return PreviewMsg{Title: " Variables ", Body: body}
	}
}

// dapMissingAdapterToast returns a non-empty string when the current language
// has no usable adapter on PATH — the caller surfaces it as a toast. Empty
// string means "either supported here, or we don't gate this language".
func (m *Model) dapMissingAdapterToast(lang string) string {
	switch lang {
	case "go":
		if _, err := exec.LookPath("dlv"); err != nil {
			return "DAP: install dlv  (go install github.com/go-delve/delve/cmd/dlv@latest)"
		}
	case "python":
		py := pythonExecutable()
		if py == "" {
			return "DAP: no python on PATH"
		}
		// Cheap import check; identical to the Lua-side gate.
		check := exec.Command(py, "-c", "import debugpy")
		if err := check.Run(); err != nil {
			return "DAP: install debugpy  (" + py + " -m pip install debugpy)"
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
