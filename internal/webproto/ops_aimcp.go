package webproto

// The chat's MCP profile from the web (web parity: AI: MCP: Switch
// Profile…): the named sets of servers and tools the chat may use. The
// list is a read; using one, or all servers again, changes what the AI
// can call, so it waits for a laptop card (Cancel first).

// MCP profile op names.
const (
	OpAIMCPProfiles   = "ai.mcp_profiles"
	OpAIUseMCPProfile = "ai.use_mcp_profile"
)

// MaxMCPProfiles caps the list.
const MaxMCPProfiles = 64

// MCPProfiles is the result of ai.mcp_profiles: the chat's profile (""
// for all servers and tools) and the profiles of the user and project.
type MCPProfiles struct {
	Current  string           `json:"current"`
	Profiles []MCPProfileItem `json:"profiles"`
}

// MCPProfileItem is one profile: its name and what it keeps.
type MCPProfileItem struct {
	Name string `json:"name"`
	Hint string `json:"hint,omitempty"`
}

// AIMCPProfileArgs names the profile to use, or All for every server.
type AIMCPProfileArgs struct {
	Name string `json:"name,omitempty"`
	All  bool   `json:"all,omitempty"`
}

func (a *AIMCPProfileArgs) validate(int) error {
	if a.All == (a.Name != "") || a.Name != "" && !validName(a.Name, MaxAIName) {
		return invalid("name a profile or all")
	}
	return nil
}

var opAIMCPRows = []Policy{
	{Op: OpAIMCPProfiles, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpAIUseMCPProfile, New: newOf[AIMCPProfileArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opAIMCPRows...); return true }()

func init() {
	registerResult(OpAIMCPProfiles, MCPProfiles{})
	registerResult(OpAIUseMCPProfile, AskRef{})
}
