package app

import (
	"errors"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// Read-only views of the model for behaviour tests (internal/testkit).
// Users never call these.

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
