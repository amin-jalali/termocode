package webproto

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Saved and pinned views (MOB-71, mobile plan 3.11.7): a view spec the
// user named and kept, stored on the laptop (views.json, at most
// MaxSavedViews, shared by every paired phone), and the per-device pins
// of the phone's Home (devices.json, at most MaxViewPins per device, so
// each phone has its own Home). Specs only, never data: a pinned view
// re-runs live on each connect (view.run, MOB-9).

// Saved view op names.
const (
	OpViewList   = "view.list"
	OpViewSave   = "view.save"
	OpViewDelete = "view.delete"
	OpViewPin    = "view.pin"
)

// Caps of saved views.
const (
	MaxSavedViews = 32
	MaxViewPins   = 8
	MaxViewName   = 60 // runes
	MaxViewSpec   = 4 * kib
	maxViewID     = 96
)

// viewIDRE is a registry view id ("git.status", "pr.list",
// "mcp.github.list_issues").
var viewIDRE = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-zA-Z0-9_-]+)*$`)

// savedIDRE is a saved view's id (the laptop makes it: "v-" + hex).
var savedIDRE = regexp.MustCompile(`^v-[0-9a-f]{8,32}$`)

// ValidSavedID reports a well-formed saved view id.
func ValidSavedID(s string) bool { return savedIDRE.MatchString(s) }

// ViewSpecCheck is the registry's full check of a spec (ViewSpec.Validate
// against its ViewDef, MOB-9); nil until the registry sets it. Its error
// text names the allowed ids.
var ViewSpecCheck func(spec json.RawMessage) error

// CheckViewSpec checks a spec's shape: one JSON object of at most
// MaxViewSpec bytes whose "view" is a registry view id, then the
// registry's own check when there is one.
func CheckViewSpec(spec json.RawMessage) error {
	b := bytes.TrimSpace(spec)
	if len(b) == 0 || len(b) > MaxViewSpec || b[0] != '{' || !json.Valid(b) {
		return invalid("bad view spec")
	}
	var head struct {
		View string `json:"view"`
	}
	if json.Unmarshal(b, &head) != nil || len(head.View) > maxViewID || !viewIDRE.MatchString(head.View) {
		return invalid("bad view spec: no view id")
	}
	if ViewSpecCheck != nil {
		if err := ViewSpecCheck(b); err != nil {
			return invalid("bad view spec: %v", err)
		}
	}
	return nil
}

// CleanViewName trims a saved view's name; ok=false: empty, too long or
// with a control character.
func CleanViewName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxViewName || hasControl(s) {
		return "", false
	}
	return s, true
}

// SavedView is one saved view: its id, the user's name and the spec.
// Updated is a Unix time in milliseconds.
type SavedView struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Spec    json.RawMessage `json:"spec"`
	Updated int64           `json:"updated"`
}

// ViewList is the answer of view.list and view.pin: every saved view,
// newest first, and this device's pins in Home order.
type ViewList struct {
	Views []SavedView `json:"views"`
	Pins  []string    `json:"pins"`
}

// ViewSaveArgs saves spec as name. With ID it renames or changes that
// saved view; without, a view of the same name (any case) is replaced,
// else a new one is made. Pin also pins it to this device's Home.
type ViewSaveArgs struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Spec json.RawMessage `json:"spec"`
	Pin  bool            `json:"pin,omitempty"`
}

func (a *ViewSaveArgs) validate(int) error {
	if a.ID != "" && !ValidSavedID(a.ID) {
		return invalid("bad view id")
	}
	if _, ok := CleanViewName(a.Name); !ok {
		return invalid("bad view name")
	}
	return CheckViewSpec(a.Spec)
}

// ViewIDArgs names one saved view (view.delete).
type ViewIDArgs struct {
	ID string `json:"id"`
}

func (a *ViewIDArgs) validate(int) error {
	if !ValidSavedID(a.ID) {
		return invalid("bad view id")
	}
	return nil
}

// ViewPinArgs pins (On) or unpins saved view ID on this device's Home;
// Order is its place among the pins (0 first; past the end: last).
type ViewPinArgs struct {
	ID    string `json:"id"`
	On    bool   `json:"on"`
	Order int    `json:"order,omitempty"`
}

func (a *ViewPinArgs) validate(int) error {
	if !ValidSavedID(a.ID) {
		return invalid("bad view id")
	}
	if a.Order < 0 || a.Order >= MaxViewPins {
		return invalid("bad pin order")
	}
	return nil
}

var opViewRows = []Policy{
	{Op: OpViewList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpViewSave, New: newOf[ViewSaveArgs](), Role: RoleController, Rate: slow, MaxBytes: 6 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpViewDelete, New: newOf[ViewIDArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpViewPin, New: newOf[ViewPinArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opViewRows...); return true }()

func init() {
	registerResult(OpViewList, ViewList{})
	registerResult(OpViewSave, SavedView{})
	registerResult(OpViewPin, ViewList{})
}
