package webproto

// The laptop's Lua extension panels and status items on the web
// (WEBP-7). The status topic names the panels and carries the status
// items' text (StatusExt); ext.panel renders one panel's lines as styled
// spans (the extension markup's style names, never raw escapes). A row
// picked and a status item clicked run the extension's own code on the
// laptop, as a click there does.

// Op names.
const (
	OpExtPanel     = "ext.panel"
	OpExtSelect    = "ext.select"
	OpExtStatusRun = "ext.status_run"
)

// MaxExtID caps a panel or status item id ("ext.name").
const MaxExtID = 128

// MaxExtRows caps the rows of a panel the page asks for.
const MaxExtRows = 2000

func validExtID(s string) bool {
	if s == "" || len(s) > MaxExtID {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' || r == ':') {
			return false
		}
	}
	return true
}

// ExtIDArgs names a panel or a status item.
type ExtIDArgs struct {
	ID string `json:"id"`
}

func (a *ExtIDArgs) validate(int) error {
	if !validExtID(a.ID) {
		return invalid("bad id")
	}
	return nil
}

// ExtSelectArgs picks row (0-based) of panel ID.
type ExtSelectArgs struct {
	ID  string `json:"id"`
	Row int    `json:"row"`
}

func (a *ExtSelectArgs) validate(int) error {
	if !validExtID(a.ID) || a.Row < 0 || a.Row >= MaxExtRows {
		return invalid("bad row")
	}
	return nil
}

// ExtSpan is a run of a panel line: its text and the style names over it
// ("accent bold"; empty: plain).
type ExtSpan struct {
	T string `json:"t"`
	S string `json:"s,omitempty"`
}

// ExtPanelView answers ext.panel: the panel's lines.
type ExtPanelView struct {
	Lines [][]ExtSpan `json:"lines"`
}

// StatusExt is the laptop's extension panels and status items, in
// registration order (status topic; absent: no extension has any).
type StatusExt struct {
	Panels []StatusExtPanel `json:"panels"`
	Items  []StatusExtItem  `json:"items"`
}

// StatusExtPanel is one extension panel.
type StatusExtPanel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Ext   string `json:"ext"`
}

// StatusExtItem is one status item: its text now (plain) and whether a
// click runs its command.
type StatusExtItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Run  bool   `json:"run,omitempty"`
}

var opExtRows = []Policy{
	{Op: OpExtPanel, New: newOf[ExtIDArgs](), Role: RoleViewer, Rate: perSec(4), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpExtSelect, New: newOf[ExtSelectArgs](), Role: RoleController, Rate: perSec(4), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpExtStatusRun, New: newOf[ExtIDArgs](), Role: RoleController, Rate: perSec(4), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opExtRows...); return true }()

func init() {
	registerResult(OpExtPanel, ExtPanelView{})
}
