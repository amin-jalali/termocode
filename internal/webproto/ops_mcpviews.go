package webproto

import "strings"

// MCP default views (MOBUX-14, mobile plan §3.11.6): every known tool of
// every configured MCP server has a view the phone can open without the
// AI. mcp.views lists them from what the laptop knows (it never starts a
// server); mcp.view_run runs one: only a read-only, trusted tool inside
// the chat's profile runs, others answer "Ask the AI". The result is the
// shaped data (table / list / card / detail / text / json) and always the
// raw JSON beside it, redacted on the laptop, so the phone shows it in one
// tap. When the view spec (MOBUX-15) lands, view.catalog and view.run
// carry these same views under ids "mcp.<server>.<tool>".

// MCP view op names.
const (
	OpMCPViews   = "mcp.views"
	OpMCPViewRun = "mcp.view_run"
)

// MCP view limits.
const (
	MaxMCPViewServers = 32
	MaxMCPViewsPer    = 128 // views of one server
	MaxMCPViewID      = 160 // "mcp." + server (≤ 40) + "." + tool
	MaxMCPViewArgs    = 12
	MaxMCPViewArg     = 2048 // bytes of one arg value
	MaxMCPViewFields  = 40
	MaxMCPViewRaw     = 16 * kib
	MaxMCPChoices     = 100 // uri picker rows of a resource view
)

// MCPViews is the result of mcp.views: each server with its views.
type MCPViews struct {
	Servers []MCPViewServer `json:"servers"`
}

// MCPViewServer is one server: Known false while its tools were never
// listed (connect it on the laptop first).
type MCPViewServer struct {
	Name  string        `json:"name"`
	Known bool          `json:"known"`
	Off   bool          `json:"off,omitempty"` // turned off in mcp.json
	Views []MCPViewInfo `json:"views"`
}

// MCPViewInfo is one default view: Run says the phone may run it; else
// Why is the one line the phone shows beside "Ask the AI".
type MCPViewInfo struct {
	ID          string          `json:"id"`
	Tool        string          `json:"tool"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Params      []MCPViewParam  `json:"params"`
	Fields      []MCPViewField  `json:"fields"`
	Layout      string          `json:"layout" enum:"table|list|card|detail|text|json"`
	Typed       string          `json:"typed,omitempty" enum:"schema|learned"`
	Run         bool            `json:"run,omitempty"`
	Why         string          `json:"why,omitempty"`
	Resources   []MCPViewChoice `json:"resources,omitempty"`
}

// MCPViewParam is one input of a view.
type MCPViewParam struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind" enum:"string|int|number|bool|enum|uri"`
	Enum     []string `json:"enum,omitempty"`
	Required bool     `json:"required,omitempty"`
	Hint     string   `json:"hint,omitempty"`
}

// MCPViewField is one field (a column, or a row of a detail).
type MCPViewField struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind" enum:"text|num|time|bool|link|json"`
}

// MCPViewChoice is one row of the resource view's uri picker.
type MCPViewChoice struct {
	URI      string   `json:"uri"`
	Name     string   `json:"name"`
	Template bool     `json:"template,omitempty"`
	Vars     []string `json:"vars,omitempty"`
}

// MCPViewRunArgs runs one view with the phone's param values.
type MCPViewRunArgs struct {
	View    string       `json:"view"`
	Args    []MCPViewArg `json:"args,omitempty"`
	Compact bool         `json:"compact,omitempty"` // a table comes as a list
}

// MCPViewArg is one param value.
type MCPViewArg struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

func (a *MCPViewRunArgs) validate(int) error {
	if !validName(a.View, MaxMCPViewID) || !strings.HasPrefix(a.View, "mcp.") {
		return invalid("bad view")
	}
	if len(a.Args) > MaxMCPViewArgs {
		return invalid("%d args", len(a.Args))
	}
	seen := map[string]bool{}
	for _, x := range a.Args {
		if !validName(x.ID, MaxAIName) || seen[x.ID] || !validText(x.Value, MaxMCPViewArg) {
			return invalid("bad arg")
		}
		seen[x.ID] = true
	}
	return nil
}

// MCPViewData is the result of mcp.view_run: the shaped data as one
// typed block (table / list / card / detail / text / json / status,
// drawn by the phone's block renderer) and the raw JSON (or text) of the
// whole result beside it, always set, both redacted on the laptop.
type MCPViewData struct {
	View      string `json:"view"`
	Title     string `json:"title"`
	Layout    string `json:"layout" enum:"table|list|card|detail|text|json|status"`
	Block     *Block `json:"block,omitempty"`
	Raw       string `json:"raw"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     bool   `json:"error,omitempty"`
	// Ask is set when the view may not run from the phone: the one line
	// why, and the page offers "Ask the AI" instead (no block).
	Ask string `json:"ask,omitempty"`
}

var opMCPViewRows = []Policy{
	{Op: OpMCPViews, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	// A run calls a read-only tool on the user's server: controller,
	// audited like the AI's own MCP calls.
	{Op: OpMCPViewRun, New: newOf[MCPViewRunArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 32 * kib, Live: 2, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool {
	opTable = append(opTable, opMCPViewRows...)
	registerResult(OpMCPViews, MCPViews{})
	registerResult(OpMCPViewRun, MCPViewData{})
	return true
}()
