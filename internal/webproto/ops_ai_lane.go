package webproto

import (
	"strings"
	"unicode/utf8"
)

// Start an agent in a new lane from the phone (MOB-65, mobile plan 3.9):
// the laptop makes a new git worktree (Worktree: New…, WTX) and starts
// an outside agent in it with the phone's task (Agent: Start… in a
// folder, AGX), typed once the agent shows its input box. It makes a
// folder, a branch and a process on the laptop, so it runs only after a
// laptop card (Cancel first) that names the agent and the folder. The
// new lane shows in the worktrees topic. ai.lane_agents lists the agents
// the laptop can start this way: names and a sign-in word only; signing
// in stays on the laptop and the phone never sees accounts or tokens.

// Lane op names.
const (
	OpAILaneAgents = "ai.lane_agents"
	OpAILaneStart  = "ai.lane_start"
)

// Lane limits.
const (
	MaxAILaneTask    = 2000
	MaxAILaneAgentID = 32
	MaxAILaneFrom    = 200
	MaxAILaneAgents  = 32
)

// AILaneStartArgs asks the laptop for a new lane: the agent's id (from
// ai.lane_agents), the task typed into it and the start point of the new
// worktree ("" = the laptop's current branch).
type AILaneStartArgs struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
	From  string `json:"from,omitempty"`
}

func (a *AILaneStartArgs) validate(int) error {
	if !validLaneAgentID(a.Agent) {
		return invalid("bad agent id")
	}
	// The task is typed into a terminal: no control character but a
	// line end or a tab (the laptop joins the lines).
	if strings.TrimSpace(a.Task) == "" || len(a.Task) > MaxAILaneTask || !utf8.ValidString(a.Task) ||
		hasControl(strings.NewReplacer("\n", " ", "\t", " ").Replace(a.Task)) {
		return invalid("bad task")
	}
	if a.From != "" && (len(a.From) > MaxAILaneFrom || !validRefName(a.From)) {
		return invalid("bad start point")
	}
	return nil
}

// validLaneAgentID: lower case letters, digits and dashes, not first a
// dash (an agents.Registry id).
func validLaneAgentID(id string) bool {
	if id == "" || len(id) > MaxAILaneAgentID || id[0] == '-' {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// AILaneAgents is the result of ai.lane_agents: the outside agents
// installed on the laptop whose screen termocode can read (so the task
// can be typed safely). NoRepo: the workspace is not a git repository,
// so no lane can be made.
type AILaneAgents struct {
	NoRepo bool          `json:"noRepo,omitempty"`
	Items  []AILaneAgent `json:"items"`
}

// AILaneAgent is one agent: its id and title, and its sign-in as a word
// ("ready" signed in, "signin" sign in on the laptop first, "unknown"
// termocode cannot tell).
type AILaneAgent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state" enum:"ready|signin|unknown"`
}

// The rows sit right after the agent worktree rows (ops_ai_wt.go), so
// the generated files keep one order.
var opAILaneRows = []Policy{
	{Op: OpAILaneAgents, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpAILaneStart, New: newOf[AILaneStartArgs](), Role: RoleController, Rate: slow, MaxBytes: 4 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

func init() {
	registerResult(OpAILaneAgents, AILaneAgents{})
	registerResult(OpAILaneStart, AskRef{})
}
