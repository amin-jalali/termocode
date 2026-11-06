package webproto

// More of the desktop editor's commands (WEBP-9, Editor and files): go
// to type definition and to implementation (read like go to definition,
// lsp.go), format a buffer with the laptop's language server, read a
// file from disk again (unsaved changes dropped: the page asks first,
// Cancel first) and a buffer's text as a Markdown tree for the preview.

// Op names.
const (
	OpLspTypeDefinition = "lsp.type_definition"
	OpLspImplementation = "lsp.implementation"
	OpBufFormat         = "buf.format"
	OpBufReload         = "buf.reload"
	OpBufMarkdown       = "buf.markdown"
)

// MaxMarkdownSrc caps the bytes of a buffer buf.markdown reads; a longer
// buffer is previewed from its start (Cut).
const MaxMarkdownSrc = 512 << 10

// BufTickArgs names buffer Buf as the page knows it at changedtick Tick:
// a buffer that moved on since is a conflict.
type BufTickArgs struct {
	Buf  int `json:"buf"`
	Tick int `json:"tick"`
}

func (a *BufTickArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Tick <= 0 || a.Tick > MaxLspRow {
		return invalid("bad buffer or tick")
	}
	return nil
}

// BufMarkdown answers buf.markdown: the buffer's text as the hub's
// Markdown tree (the page builds no markup from strings).
type BufMarkdown struct {
	MD  *MdNode `json:"md"`
	Cut bool    `json:"cut,omitempty"` // the buffer was longer than MaxMarkdownSrc
}

var opEditorRows = []Policy{
	{Op: OpLspTypeDefinition, New: newOf[LspPosArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpLspImplementation, New: newOf[LspPosArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpBufFormat, New: newOf[BufTickArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpBufReload, New: newOf[BufArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpBufMarkdown, New: newOf[BufArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opEditorRows...); return true }()

func init() {
	registerResult(OpLspTypeDefinition, LspLocations{})
	registerResult(OpLspImplementation, LspLocations{})
	registerResult(OpBufFormat, EditRes{})
	registerResult(OpBufReload, EditRes{})
	registerResult(OpBufMarkdown, BufMarkdown{})
}
