package app

import (
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// Read-only views of the model for behaviour tests (internal/testkit).
// Users never call these.

// nvimSetupGate, when set, runs in the nvim setup (attachCmd) after the
// base options, before the slower rest. Tests set it to hold setup
// half-way.
var nvimSetupGate atomic.Pointer[func()]

func runNvimSetupGate() {
	if g := nvimSetupGate.Load(); g != nil {
		(*g)()
	}
}

// SetNvimSetupGate makes the next nvim setups call gate half-way through
// (after the base options and the always-on autocmds, before the LSP,
// theme and plugin setup), so a test can act while setup still runs. It
// returns a func that restores the default. For tests.
func SetNvimSetupGate(gate func()) (restore func()) {
	old := nvimSetupGate.Swap(&gate)
	return func() { nvimSetupGate.Store(old) }
}

// errNoNvim is returned by NvimLua when the app runs without nvim.
var errNoNvim = errors.New("no embedded nvim")

// NvimLua runs a Lua chunk in the embedded nvim and returns its result as
// a string (tostring of the chunk's return value). For tests.
func (m Model) NvimLua(code string, args ...any) (string, error) {
	if m.nvim == nil {
		return "", errNoNvim
	}
	return m.nvim.EvalLuaStringArgs(code, args...)
}

// OpenBuffers returns the editor tabs as the tab bar shows them (pinned
// first) and the id of the active one. For tests.
func (m Model) OpenBuffers() ([]nvim.BufferInfo, int) {
	return m.tabs.Buffers()
}

// ExplorerCursor returns the absolute path of the explorer row under the
// cursor and the paths in the multi-selection. For tests.
func (m Model) ExplorerCursor() (string, []string) {
	return m.explorer.CursorPath(), m.explorer.Selection()
}

// FocusedEditor reports whether keys go to the editor (else the sidebar).
// For tests.
func (m Model) FocusedEditor() bool { return m.focus == FocusEditor }

// EditorPaneSize is the nvim grid size ("COLSxROWS") the layout asked for
// last. For tests.
func (m Model) EditorPaneSize() string {
	return strconv.Itoa(m.resizeT.w) + "x" + strconv.Itoa(m.resizeT.h)
}
