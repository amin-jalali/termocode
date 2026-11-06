package webproto

import "slices"

// The laptop's AI screens on the web (web parity, AI screens): AI: Usage,
// Project Rules…, Memory…, Skills…, Hooks…, Web Tools…, MCP: Show Log, MCP:
// Server Status…, Copy termocode Server Config, Configure Provider… and
// Help: Build Bug Report…, read as rows or text. ai.info changes nothing:
// trusting rules, editing memory, setting keys and the like stay on the
// laptop (a key never leaves it). Every string passes the registered-secret
// redaction; the provider's base URL shows its host only.

// OpAIInfo reads one AI screen.
const OpAIInfo = "ai.info"

// AI screen kinds.
const (
	AIInfoUsage       = "usage"
	AIInfoRules       = "rules"
	AIInfoMemory      = "memory"
	AIInfoSkills      = "skills"
	AIInfoHooks       = "hooks"
	AIInfoWebTools    = "web-tools"
	AIInfoMCPLog      = "mcp-log"
	AIInfoServeStatus = "serve-status"
	AIInfoServeConfig = "serve-config"
	AIInfoConfigure   = "configure"
	AIInfoBugReport   = "bug-report"
)

// AIInfoKinds are the screens, in the order a menu lists them.
var AIInfoKinds = []string{AIInfoUsage, AIInfoRules, AIInfoMemory, AIInfoSkills, AIInfoHooks, AIInfoWebTools,
	AIInfoMCPLog, AIInfoServeStatus, AIInfoServeConfig, AIInfoConfigure, AIInfoBugReport}

// Limits of a screen.
const (
	MaxAIInfoRows = 500
	MaxAIInfoText = 256 << 10
)

// AIInfoArgs names a screen.
type AIInfoArgs struct {
	Kind string `json:"kind" enum:"usage|rules|memory|skills|hooks|web-tools|mcp-log|serve-status|serve-config|configure|bug-report"`
}

func (a *AIInfoArgs) validate(int) error {
	if !slices.Contains(AIInfoKinds, a.Kind) {
		return invalid("bad kind")
	}
	return nil
}

// AIInfo is a screen: rows (a header row starts a group) and/or plain
// text (monospace). Note is one line under the title. Copy: the page
// offers to copy Text; Save: it offers to save Text as that file name.
type AIInfo struct {
	Title string      `json:"title"`
	Note  string      `json:"note,omitempty"`
	Rows  []AIInfoRow `json:"rows"`
	Text  string      `json:"text,omitempty"`
	Copy  bool        `json:"copy,omitempty"`
	Save  string      `json:"save,omitempty"`
}

// AIInfoRow is one row of a screen.
type AIInfoRow struct {
	Title  string `json:"title"`
	Hint   string `json:"hint,omitempty"`
	Header bool   `json:"header,omitempty"`
}

var opAIInfoRows = []Policy{
	{Op: OpAIInfo, New: newOf[AIInfoArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opAIInfoRows...); return true }()

func init() {
	registerResult(OpAIInfo, AIInfo{})
}
