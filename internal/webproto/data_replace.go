package webproto

// search.replace (WEBP-9, Editor and files): Search: Replace in
// Workspace… from the web. The text and its replacement are literal;
// the laptop counts the matches for its card and writes only after the
// laptop's yes (the laptop's own replace, open buffers read again).

// OpSearchReplace is the op name.
const OpSearchReplace = "search.replace"

// MaxReplaceText caps the bytes of the text and of its replacement.
const MaxReplaceText = 4096

// SearchReplaceArgs replaces Find with Replace in every workspace file.
type SearchReplaceArgs struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

func (a *SearchReplaceArgs) validate(int) error {
	if a.Find == "" || len(a.Find) > MaxReplaceText || len(a.Replace) > MaxReplaceText || hasControl(a.Find) || hasControl(a.Replace) {
		return invalid("bad text or replacement")
	}
	return nil
}

var opReplaceRows = []Policy{
	{Op: OpSearchReplace, New: newOf[SearchReplaceArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 12 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opReplaceRows...); return true }()

func init() {
	registerResult(OpSearchReplace, AskRef{})
}
