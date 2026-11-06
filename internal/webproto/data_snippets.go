package webproto

// The user's own snippets (WEBP-9, Editor and files; the laptop's
// Snippets: Manage…): the page lists them, saves one and deletes one
// (the page asks first). They live in the laptop's snippets.json; the
// laptop's editor reads them again after each change.

// Op names.
const (
	OpSnippetsUser   = "snippets.user"
	OpSnippetsSave   = "snippets.save"
	OpSnippetsDelete = "snippets.delete"
)

// MaxSnippetScope caps a scope (a file type, or "*" for every one).
const MaxSnippetScope = 64

// SnippetRefArgs names one snippet: its scope and trigger.
type SnippetRefArgs struct {
	Scope   string `json:"scope"`
	Trigger string `json:"trigger"`
}

func (a *SnippetRefArgs) validate(int) error {
	if !validScopeTrigger(a.Scope, a.Trigger) {
		return invalid("bad scope or trigger")
	}
	return nil
}

// SnippetSaveArgs saves one snippet; Body may hold new lines and tabs.
type SnippetSaveArgs struct {
	Scope   string `json:"scope"`
	Trigger string `json:"trigger"`
	Body    string `json:"body"`
}

func (a *SnippetSaveArgs) validate(int) error {
	if !validScopeTrigger(a.Scope, a.Trigger) || a.Body == "" || len(a.Body) > MaxSnippetBody {
		return invalid("bad snippet")
	}
	for _, r := range a.Body {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return invalid("bad snippet body")
		}
	}
	return nil
}

func validScopeTrigger(scope, trigger string) bool {
	if len(scope) > MaxSnippetScope || hasControl(scope) || trigger == "" || len(trigger) > 64 {
		return false
	}
	for _, r := range trigger {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// UserSnippet is one of the user's snippets ("*": every file type).
type UserSnippet struct {
	Scope   string `json:"scope"`
	Trigger string `json:"trigger"`
	Body    string `json:"body"`
}

// UserSnippetList answers snippets.user: "*" first, then by scope and trigger.
type UserSnippetList struct {
	Items []UserSnippet `json:"items"`
}

// SnippetSaved answers snippets.save: an older snippet of that name was replaced.
type SnippetSaved struct {
	Replaced bool `json:"replaced,omitempty"`
}

var opSnippetRows = []Policy{
	{Op: OpSnippetsUser, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpSnippetsSave, New: newOf[SnippetSaveArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 16 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpSnippetsDelete, New: newOf[SnippetRefArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opSnippetRows...); return true }()

func init() {
	registerResult(OpSnippetsUser, UserSnippetList{})
	registerResult(OpSnippetsSave, SnippetSaved{})
	registerResult(OpSnippetsDelete, SnippetSaved{})
}
