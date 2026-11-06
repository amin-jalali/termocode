package webproto

// Quick fixes and inlay hints of the desktop editor (WEBP-9, Editor and
// files), from the laptop's language servers. The laptop keeps the list
// lsp.code_actions gave; lsp.code_action_apply applies one of it on the
// laptop (its edits reach the page as the laptop's changes), only while
// the buffer is still at the tick the list was asked at.

// Op names.
const (
	OpLspCodeActions     = "lsp.code_actions"
	OpLspCodeActionApply = "lsp.code_action_apply"
	OpLspInlayHints      = "lsp.inlay_hints"
)

// Limits.
const (
	MaxCodeActions = 100  // actions of one list
	MaxHintRows    = 2000 // rows one lsp.inlay_hints may cover
	MaxHints       = 2000 // hints of one answer
	MaxHintLabel   = 256  // bytes of one hint's label
)

// LspRangeArgs names a range of buffer Buf at changedtick Tick: rows
// 0-based, columns in UTF-16 units, the end not before the start.
type LspRangeArgs struct {
	Buf     int `json:"buf"`
	Tick    int `json:"tick"`
	Line    int `json:"line"`
	Col     int `json:"col"`
	EndLine int `json:"end_line"`
	EndCol  int `json:"end_col"`
}

func (a *LspRangeArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Tick < 0 || a.Tick > MaxLspRow || a.Line < 0 || a.Col < 0 || a.EndLine > MaxLspRow || a.EndCol > MaxLspRow ||
		a.EndLine < a.Line || (a.EndLine == a.Line && a.EndCol < a.Col) {
		return invalid("bad buffer or range")
	}
	return nil
}

// LspApplyArgs applies action Index of the list asked at Tick.
type LspApplyArgs struct {
	Buf   int `json:"buf"`
	Tick  int `json:"tick"`
	Index int `json:"index"`
}

func (a *LspApplyArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Tick <= 0 || a.Tick > MaxLspRow || a.Index < 0 || a.Index >= MaxCodeActions {
		return invalid("bad buffer, tick or action")
	}
	return nil
}

// LspRowsArgs names rows First..Last (0-based, both in) of buffer Buf.
type LspRowsArgs struct {
	Buf   int `json:"buf"`
	Tick  int `json:"tick"`
	First int `json:"first"`
	Last  int `json:"last"`
}

func (a *LspRowsArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Tick < 0 || a.Tick > MaxLspRow || a.First < 0 || a.Last < a.First || a.Last-a.First >= MaxHintRows {
		return invalid("bad buffer or rows")
	}
	return nil
}

// LspAction is one code action: its title, the protocol's kind
// ("quickfix", "refactor.extract" …) and whether it is the preferred one.
type LspAction struct {
	Title     string `json:"title"`
	Kind      string `json:"kind,omitempty"`
	Preferred bool   `json:"preferred,omitempty"`
}

// LspActions answers lsp.code_actions, in the servers' order.
type LspActions struct {
	Items []LspAction `json:"items"`
}

// LspHint is one inlay hint: where it shows (0-based row, UTF-16
// column), its text and its padding.
type LspHint struct {
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Label    string `json:"label"`
	PadLeft  bool   `json:"padLeft,omitempty"`
	PadRight bool   `json:"padRight,omitempty"`
}

// LspHints answers lsp.inlay_hints.
type LspHints struct {
	Items []LspHint `json:"items"`
	Cut   bool      `json:"cut,omitempty"`
}

var opActionRows = []Policy{
	{Op: OpLspCodeActions, New: newOf[LspRangeArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpLspCodeActionApply, New: newOf[LspApplyArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpLspInlayHints, New: newOf[LspRowsArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opActionRows...); return true }()

func init() {
	registerResult(OpLspCodeActions, LspActions{})
	registerResult(OpLspCodeActionApply, EditRes{})
	registerResult(OpLspInlayHints, LspHints{})
}
