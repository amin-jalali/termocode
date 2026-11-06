package webproto

// The laptop's saved layouts from the web (WEBP-7; the laptop's Layout:
// Open…, Save As…, Delete… and Reset to Default). The page lists the
// layouts by name, where each comes from and what it changes, never a
// path. Opening, saving and resetting act at once, as the laptop's own
// commands do; a taken name is replaced only with Replace (the first
// answer says it exists, so the page can ask). Deleting a saved layout
// file waits for a laptop card.

// Op names.
const (
	OpLayoutList   = "layout.list"
	OpLayoutOpen   = "layout.open"
	OpLayoutSave   = "layout.save"
	OpLayoutDelete = "layout.delete"
	OpLayoutReset  = "layout.reset"
)

// MaxLayoutName caps a layout name (it is a file name on the laptop).
const MaxLayoutName = 40

// validLayoutName: letters, digits, space, - _ . (up to 40), starting
// with a letter or digit, not ending in a dot or space.
func validLayoutName(s string) bool {
	if s == "" || len(s) > MaxLayoutName || s[len(s)-1] == '.' || s[len(s)-1] == ' ' {
		return false
	}
	for i, r := range s {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !alnum && (i == 0 || !(r == ' ' || r == '_' || r == '.' || r == '-')) {
			return false
		}
	}
	return true
}

// LayoutNameArgs names a layout.
type LayoutNameArgs struct {
	Name string `json:"name"`
}

func (a *LayoutNameArgs) validate(int) error {
	if !validLayoutName(a.Name) {
		return invalid("bad layout name")
	}
	return nil
}

// LayoutSaveArgs saves the laptop's screen as Name: Scope user (every
// project of the user) or project (.termocode/layouts, shared).
type LayoutSaveArgs struct {
	Name    string `json:"name"`
	Scope   string `json:"scope" enum:"user|project"`
	Replace bool   `json:"replace,omitempty"`
}

func (a *LayoutSaveArgs) validate(int) error {
	if !validLayoutName(a.Name) {
		return invalid("bad layout name")
	}
	if a.Scope != "user" && a.Scope != "project" {
		return invalid("bad scope")
	}
	return nil
}

// LayoutEntry is one layout: where it comes from (built-in, user or
// project), its one-line description and the lines of what it changes.
type LayoutEntry struct {
	Name        string   `json:"name"`
	Source      string   `json:"source" enum:"built-in|user|project"`
	Description string   `json:"description,omitempty"`
	Preview     []string `json:"preview"`
}

// LayoutList answers layout.list, in the laptop picker's order.
type LayoutList struct {
	Items []LayoutEntry `json:"items"`
}

// LayoutSaved answers layout.save: Saved, or Exists (nothing written;
// save again with Replace).
type LayoutSaved struct {
	Saved  bool `json:"saved,omitempty"`
	Exists bool `json:"exists,omitempty"`
}

// LayoutOpened answers layout.open and layout.reset: the layout the
// laptop shows now ("default" after a reset).
type LayoutOpened struct {
	Name string `json:"name"`
}

var opLayoutRows = []Policy{
	{Op: OpLayoutList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpLayoutOpen, New: newOf[LayoutNameArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpLayoutSave, New: newOf[LayoutSaveArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpLayoutDelete, New: newOf[LayoutNameArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpLayoutReset, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opLayoutRows...); return true }()

func init() {
	registerResult(OpLayoutList, LayoutList{})
	registerResult(OpLayoutOpen, LayoutOpened{})
	registerResult(OpLayoutSave, LayoutSaved{})
	registerResult(OpLayoutDelete, AskRef{})
	registerResult(OpLayoutReset, LayoutOpened{})
}
