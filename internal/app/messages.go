package app

import "termocode/internal/nvim"

// stickyContextMsg carries the freshly-fetched "function/class signature"
// line that should sit in the sticky-scroll strip above the editor. Empty
// signature means "no enclosing symbol; hide the strip".
type stickyContextMsg struct {
	Signature string
}

// scrollbarMarkersMsg carries diagnostic positions + total line count for
// the editor scrollbar's overview ruler.
type scrollbarMarkersMsg struct {
	Marks      []nvim.DiagnosticMark
	TotalLines int
}

type FileOpenRequestedMsg struct{ Path string }
type FileOpenedMsg struct {
	Path    string
	Content string
}
type FileSavedMsg struct{ Path string }
type ErrMsg struct{ Err error }

// StateMsg carries a snapshot of nvim state used to drive the UI.
type StateMsg struct {
	Path       string
	Dirty      bool
	Lang       string
	Bufs       []nvimBuffer
	Active     int
	Errors     int
	Warnings   int
	CursorLine int               // 1-based; 0 when unknown
	Cursors    map[string][2]int // path → [line, col] (1-based) for every listed buffer

	// LastReloadPath / LastReloadSeq mirror the Lua side's "file just
	// reloaded from disk" globals. Update.go compares the seq against the
	// previously-seen one to fire a single toast per reload event.
	LastReloadPath string
	LastReloadSeq  int

	// CurrentWin / Mode mirror nvim's current window-id and the first
	// byte of nvim_get_mode().mode. The StateMsg handler combines them
	// with m.terminalWinID to recompute m.inTerminal, which gates the
	// terminal-shortcut pass-through path in handleGlobalKey.
	CurrentWin int
	Mode       string
}

// nvimBuffer is a thin alias to avoid an import cycle in messages.go callers.
type nvimBuffer struct {
	ID       int
	Path     string
	Modified bool
	Filetype string
}
