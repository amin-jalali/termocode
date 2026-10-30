package webproto

// The command palette of the desktop workbench (Ctrl/Cmd+Shift+P) lists,
// beside the page's own commands, the laptop palette's commands that are
// safe from the web: a fixed allow-list on the laptop, checked against
// the AI policy (a command the AI may never run is never offered). The
// page names a command only by its palette id. A risky one (it pushes,
// pulls or changes the stash) runs only after a laptop card.

// Palette op names.
const (
	OpPaletteList = "palette.list"
	OpPaletteRun  = "palette.run"
	OpPaletteAsk  = "palette.ask"
)

// MaxPaletteID caps a palette id.
const MaxPaletteID = 64

// PaletteArgs names a laptop palette command by its id.
type PaletteArgs struct {
	ID string `json:"id"`
}

func (a *PaletteArgs) validate(int) error {
	if a.ID == "" || len(a.ID) > MaxPaletteID || a.ID[0] == '-' {
		return invalid("bad command id")
	}
	for _, r := range a.ID {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return invalid("bad command id")
		}
	}
	return nil
}

// PaletteItem is one laptop command the page may offer.
type PaletteItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Keys is the laptop's shortcut ("Ctrl+Shift+E"), if it has one.
	Keys string `json:"keys,omitempty"`
	// Ask: it runs only after the laptop confirms (palette.ask).
	Ask bool `json:"ask,omitempty"`
}

// PaletteList is the result of palette.list, in the laptop palette's order.
type PaletteList struct {
	Items []PaletteItem `json:"items"`
}

var opPaletteRows = []Policy{
	{Op: OpPaletteList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpPaletteRun, New: newOf[PaletteArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpPaletteAsk, New: newOf[PaletteArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opPaletteRows...); return true }()

func init() {
	registerResult(OpPaletteList, PaletteList{})
	registerResult(OpPaletteAsk, AskRef{})
}
