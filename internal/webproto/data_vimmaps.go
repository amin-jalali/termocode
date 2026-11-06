package webproto

// The user's own mappings for the web editor's Vim mode (WEBP-1): the
// laptop editor's global mappings the web can run (a key sequence on the
// right side), so the keys the user set up work the same way in the
// browser. The page asks once, when Vim mode is first on.

// OpEditorVimMaps lists them.
const OpEditorVimMaps = "editor.vim_maps"

// Caps of the list.
const (
	MaxVimMaps   = 300
	MaxVimMapLHS = 64
	MaxVimMapRHS = 512
)

// VimMap is one mapping: Mode is "n" (normal), "x" (visual), "o"
// (operator pending) or "i" (insert); LHS and RHS are in the editor's
// key notation ("<Space>w", ":w<CR>"); Noremap: the right side's keys
// are not mapped again.
type VimMap struct {
	Mode    string `json:"mode"`
	LHS     string `json:"lhs"`
	RHS     string `json:"rhs"`
	Noremap bool   `json:"noremap,omitempty"`
}

// VimMapList answers editor.vim_maps, by mode and keys; Timeout is the
// editor's timeoutlen in ms (0: it waits for the next key forever).
type VimMapList struct {
	Items   []VimMap `json:"items"`
	Timeout int      `json:"timeout,omitempty"`
}

var opVimMapRows = []Policy{
	{Op: OpEditorVimMaps, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opVimMapRows...); return true }()

func init() {
	registerResult(OpEditorVimMaps, VimMapList{})
}
