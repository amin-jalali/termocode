package webproto

import (
	"time"
	"unicode/utf8"
)

// "What can I do?" on the phone (MOB-13): ai.catalog lists what the AI
// can do on this laptop, grouped by feature (built-in tools, with a few
// prompts to try and the view each feature opens), and every configured
// MCP server with the tools it last listed. It reads cached lists only:
// it never starts a server, so a viewer may ask.
//
// Pinned tracker blocks (MOB-13): the AI may pin a read-only MCP tool
// call ("my open Linear issues") to the phone's Home after a card on the
// laptop (the tracker_pin tool, always asks). The pins are stored on the
// laptop and shared by every paired phone. ai.tracker_run re-runs one on
// the laptop, only while its tool still runs at once (read-only, trusted
// server, not off, in the chat's MCP profile); the phone sees rows, never
// the raw result. Unpin is the phone's own tap (Cancel first on the page).

// Catalog and tracker op names.
const (
	OpAICatalog      = "ai.catalog"
	OpAITrackerPins  = "ai.tracker_pins"
	OpAITrackerRun   = "ai.tracker_run"
	OpAITrackerUnpin = "ai.tracker_unpin"
)

// Catalog limits.
const (
	MaxCatalogQuery      = 64
	MaxCatalogServers    = 64
	MaxCatalogTools      = 1000 // MCP tools in one answer, all servers
	MaxCatalogTry        = 4
	MaxTrackerPins       = 12
	MaxTrackerRows       = 50
	MaxTrackerTitle      = 60
	CatalogFeatureMCP    = "mcp"
	TrackerIDLen         = 12
	TrackerRunTimeout    = 20 * time.Second
	TrackerRefreshMinSec = 60
)

// AICatalogArgs narrows the catalog: Feature names one group ("mcp":
// only the servers, the Home's MCP chip), Query keeps the rows whose
// title, hint or tool name holds it (case-insensitive).
type AICatalogArgs struct {
	Feature string `json:"feature,omitempty"`
	Query   string `json:"query,omitempty"`
}

func (a *AICatalogArgs) validate(int) error {
	switch {
	case a.Feature != "" && !validName(a.Feature, MaxAIName):
		return invalid("bad feature")
	case len(a.Query) > MaxCatalogQuery || hasControl(a.Query) || !utf8.ValidString(a.Query):
		return invalid("bad query")
	}
	return nil
}

// AICatalog is the result of ai.catalog. Note is one line when MCP is
// off or no server is set up; Truncated: more MCP tools than the cap.
type AICatalog struct {
	Features  []CatalogFeature `json:"features"`
	Servers   []CatalogServer  `json:"servers"`
	Note      string           `json:"note,omitempty"`
	Truncated bool             `json:"truncated,omitempty"`
}

// CatalogFeature is one built-in feature: Tools the AI may call for it
// (Asks of them ask first), Try a few prompts that fill the composer,
// View the view id it opens in the Views menu ("" when none yet).
type CatalogFeature struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Hint  string   `json:"hint"`
	Tools int      `json:"tools"`
	Asks  int      `json:"asks"`
	Try   []string `json:"try"`
	View  string   `json:"view,omitempty"`
}

// CatalogServer is one MCP server from the laptop's mcp.json. State is
// the manager's (running, stopped, off, sign-in, …); Cached: the tools
// come from the disk cache (the server did not run yet this session);
// Known: a tool list was ever seen.
type CatalogServer struct {
	Name      string        `json:"name"`
	State     string        `json:"state"`
	Trusted   bool          `json:"trusted"`
	Cached    bool          `json:"cached,omitempty"`
	Known     bool          `json:"known"`
	Prompts   bool          `json:"prompts,omitempty"`
	Resources bool          `json:"resources,omitempty"`
	Tools     []CatalogTool `json:"tools"`
}

// CatalogTool is one MCP tool. Gate is "runs" (runs at once), "asks" or
// "off" (the user turned it off, or the chat's profile leaves it out);
// Text is what a tap puts in the composer (nothing is sent).
type CatalogTool struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	Hint  string `json:"hint,omitempty"`
	Gate  string `json:"gate" enum:"runs|asks|off"`
	Text  string `json:"text"`
}

// TrackerPins is the result of ai.tracker_pins, oldest first.
type TrackerPins struct {
	Pins []TrackerPin `json:"pins"`
}

// TrackerPin is one pinned tracker block: the MCP tool it runs and its
// arguments as short text (redacted). Runs: the tool still runs at once,
// so the phone may refresh it; else Why says what the laptop needs.
type TrackerPin struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Server  string `json:"server"`
	Tool    string `json:"tool"`
	Args    string `json:"args,omitempty"`
	Refresh int    `json:"refresh"` // seconds between re-runs while Home is open
	Runs    bool   `json:"runs"`
	Why     string `json:"why,omitempty"`
}

// TrackerIDArgs names one pin.
type TrackerIDArgs struct {
	ID string `json:"id"`
}

func (a *TrackerIDArgs) validate(int) error {
	if len(a.ID) != TrackerIDLen {
		return invalid("bad pin")
	}
	for _, r := range a.ID {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return invalid("bad pin")
		}
	}
	return nil
}

// TrackerBlock is the result of ai.tracker_run: the rows the tool gave
// (items with a title, a hint line and the item's id as Ref for "↩ to
// chat"), or Text when it gave no list, or Error when it failed. At is
// the run's time (unix ms).
type TrackerBlock struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	Rows      []TrackerRow `json:"rows"`
	Text      string       `json:"text,omitempty"`
	Error     string       `json:"error,omitempty"`
	Truncated bool         `json:"truncated,omitempty"`
	At        int64        `json:"at"`
}

// TrackerRow is one item of a tracker block.
type TrackerRow struct {
	Title string `json:"title"`
	Hint  string `json:"hint,omitempty"`
	Ref   string `json:"ref,omitempty"`
}

var opCatalogRows = []Policy{
	{Op: OpAICatalog, New: newOf[AICatalogArgs](), Role: RoleViewer, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpAITrackerPins, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpAITrackerRun, New: newOf[TrackerIDArgs](), Role: RoleController, Rate: Rate{N: MaxTrackerPins, Per: time.Minute}, MaxBytes: 1 * kib, Live: 2, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpAITrackerUnpin, New: newOf[TrackerIDArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opCatalogRows...); return true }()

func init() {
	registerResult(OpAICatalog, AICatalog{})
	registerResult(OpAITrackerPins, TrackerPins{})
	registerResult(OpAITrackerRun, TrackerBlock{})
	registerResult(OpAITrackerUnpin, TrackerPins{})
}
