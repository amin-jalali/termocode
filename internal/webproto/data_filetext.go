package webproto

// file.text (WEBP-9, Editor and files): a workspace file as it is on
// disk, for File: Compare Two Files…. A file that may hold secrets is
// refused; binary text is not sent; a long file comes as its start.

// OpFileText is the op name.
const OpFileText = "file.text"

// FileText answers file.text.
type FileText struct {
	Path      string `json:"path"`
	Text      string `json:"text"`
	Binary    bool   `json:"binary,omitempty"`
	Truncated bool   `json:"truncated,omitempty"` // longer than MaxGitText: Text is its start
}

var opFileTextRows = []Policy{
	{Op: OpFileText, New: newOf[PathArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opFileTextRows...); return true }()

func init() {
	registerResult(OpFileText, FileText{})
}
