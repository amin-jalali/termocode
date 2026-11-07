package webproto

import "unicode/utf8"

// Saved views (MOBUX-18, mobile plan 3.11.7): the phone's "Save as view"
// and "📌 Pin to Home". A saved view is a spec and a name only, never
// data: every open runs the spec again, live, on the laptop (view.run).
// They live on the laptop in the user's config folder (views.json) and
// are shared by every paired phone (owner 2026-11-07), pins and their
// order too. Rename, delete and pin need a controller; the page asks
// before a delete (Cancel first). Every change goes to every browser as
// ev views_saved with the whole new list.

// Op names.
const (
	OpViewList   = "view.list"
	OpViewSave   = "view.save"
	OpViewRename = "view.rename"
	OpViewDelete = "view.delete"
	OpViewPin    = "view.pin"
)

// EventViewsSaved is the ev kind that carries the saved views after a change.
const EventViewsSaved = "views_saved"

// Caps of the saved views.
const (
	MaxSavedViews  = 32 // saved views on the laptop
	MaxPinnedViews = 8  // of them pinned to Home
	MaxViewName    = 60 // runes of a saved view's name
	MaxSavedViewID = 16 // bytes of a saved view's id
)

// ViewSaveArgs saves a spec under a name; a view of the same name (any
// case) is replaced and keeps its id and pin. Pin also pins it to Home.
type ViewSaveArgs struct {
	Spec ViewSpec `json:"spec"`
	Name string   `json:"name"`
	Pin  bool     `json:"pin,omitempty"`
}

func (a *ViewSaveArgs) validate(int) error {
	if !validViewName(a.Name) {
		return invalid("a view name is 1 to %d characters, on one line", MaxViewName)
	}
	return a.Spec.Validate()
}

// ViewRefArgs names one saved view.
type ViewRefArgs struct {
	ID string `json:"id"`
}

func (a *ViewRefArgs) validate(int) error {
	if !validSavedViewID(a.ID) {
		return invalid("bad view id")
	}
	return nil
}

// ViewRenameArgs gives a saved view a new name.
type ViewRenameArgs struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (a *ViewRenameArgs) validate(int) error {
	if !validSavedViewID(a.ID) {
		return invalid("bad view id")
	}
	if !validViewName(a.Name) {
		return invalid("a view name is 1 to %d characters, on one line", MaxViewName)
	}
	return nil
}

// ViewPinArgs pins a saved view to Home (On) or unpins it. Order is its
// place among the pinned views, 1 = first; 0 keeps its place, or puts a
// newly pinned view last.
type ViewPinArgs struct {
	ID    string `json:"id"`
	On    bool   `json:"on"`
	Order int    `json:"order,omitempty"`
}

func (a *ViewPinArgs) validate(int) error {
	if !validSavedViewID(a.ID) {
		return invalid("bad view id")
	}
	if a.Order < 0 || a.Order > MaxPinnedViews || (!a.On && a.Order != 0) {
		return invalid("order is 0 to %d, and only when pinning", MaxPinnedViews)
	}
	return nil
}

// SavedView is one saved view: its name and spec, never its data.
// Order is its place on Home (1 = first) when Pinned.
type SavedView struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Spec   ViewSpec `json:"spec"`
	Pinned bool     `json:"pinned,omitempty"`
	Order  int      `json:"order,omitempty"`
	Saved  int64    `json:"saved"`        // unix ms of the last save or rename
	By     string   `json:"by,omitempty"` // the device that saved it last
}

// SavedViewList answers view.list (and rename, delete, pin): pinned
// views first in their order, then the others by name.
type SavedViewList struct {
	Items     []SavedView `json:"items"`
	Max       int         `json:"max"`
	MaxPinned int         `json:"max_pinned"`
}

// ViewSaved answers view.save: the saved view's id, and whether a view
// of that name was replaced.
type ViewSaved struct {
	ID       string `json:"id"`
	Replaced bool   `json:"replaced,omitempty"`
}

func validViewName(s string) bool {
	n := utf8.RuneCountInString(s)
	return n > 0 && n <= MaxViewName && utf8.ValidString(s) && !hasControl(s) && s[0] != ' ' && s[len(s)-1] != ' '
}

func validSavedViewID(s string) bool {
	if s == "" || len(s) > MaxSavedViewID {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// ValidSavedView says whether a saved view read back from views.json is
// still one the phone may run: a good id and name, a spec of the right
// shape, a pin order in range.
func ValidSavedView(v *SavedView) bool {
	if v == nil || !validSavedViewID(v.ID) || !validViewName(v.Name) || v.Spec.Validate() != nil {
		return false
	}
	return v.Order >= 0 && v.Order <= MaxPinnedViews && (v.Pinned || v.Order == 0)
}

var opViewSavedRows = []Policy{
	{Op: OpViewList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpViewSave, New: newOf[ViewSaveArgs](), Role: RoleController, Rate: slow, MaxBytes: 16 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpViewRename, New: newOf[ViewRenameArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpViewDelete, New: newOf[ViewRefArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpViewPin, New: newOf[ViewPinArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opViewSavedRows...); return true }()

func init() {
	registerResult(OpViewList, SavedViewList{})
	registerResult(OpViewSave, ViewSaved{})
	registerResult(OpViewRename, SavedViewList{})
	registerResult(OpViewDelete, SavedViewList{})
	registerResult(OpViewPin, SavedViewList{})
	registerEvent(EventViewsSaved, SavedViewList{})
}
