package webproto

import "strings"

// Agent races from the phone (MOB-62, mobile plan 2.6 and 3.9): start a
// race of 2–4 lanes, summarise the finished lanes, run the race's verify
// command in them, and stop the lanes that still work. Starting makes
// worktrees and opens a terminal tab per lane on the laptop, so it runs
// only after a laptop card (Cancel first). Summarise spends the user's
// tokens on the quick model and only runs when the user taps it. Verify
// runs the command the race was started with (it was on the start card);
// the phone cannot change it. Stop sends Ctrl+C into the lanes; the work
// stays in the worktrees. Merge and Discard stay laptop cards (the races
// view). The phone follows the lanes in the races topic.

// Agent race op names.
const (
	OpAIRaceStart   = "ai.race_start"
	OpAIRaceSummary = "ai.race_summary"
	OpAIRaceVerify  = "ai.race_verify"
	OpAIRaceStop    = "ai.race_stop"
)

// Agent race limits (the laptop's agent_race_start tool has the same).
const (
	MaxAIRaceTask   = 2000
	MaxAIRaceVerify = 300
	MaxAIRaceAgent  = 32
	MinAIRaceLanes  = 2
	MaxAIRaceLanes  = 4
	MaxAIRaceID     = 32
)

// AIRaceStartArgs asks the laptop to start a race: the task every agent
// gets, one agent id per lane (an agent may run twice) and an optional
// one-line verify command.
type AIRaceStartArgs struct {
	Task   string   `json:"task"`
	Agents []string `json:"agents"`
	Verify string   `json:"verify,omitempty"`
}

func (a *AIRaceStartArgs) validate(int) error {
	task := strings.TrimSpace(a.Task)
	if task == "" || len(a.Task) > MaxAIRaceTask {
		return invalid("bad task")
	}
	if len(a.Verify) > MaxAIRaceVerify || hasControl(a.Verify) {
		return invalid("bad verify command")
	}
	if len(a.Agents) < MinAIRaceLanes || len(a.Agents) > MaxAIRaceLanes {
		return invalid("a race has %d to %d lanes", MinAIRaceLanes, MaxAIRaceLanes)
	}
	for _, id := range a.Agents {
		if !validAgentID(id) {
			return invalid("bad agent id")
		}
	}
	return nil
}

// validAgentID: lower case letters, digits and dashes, not first a dash.
func validAgentID(id string) bool {
	if id == "" || len(id) > MaxAIRaceAgent || id[0] == '-' {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// AIRaceArgs names a race by its id ("race-<n>").
type AIRaceArgs struct {
	Race string `json:"race"`
}

func (a *AIRaceArgs) validate(int) error {
	if !validRaceID(a.Race) {
		return invalid("bad race id")
	}
	return nil
}

// validRaceID is "race-" and a number from 1, no leading zero.
func validRaceID(id string) bool {
	n, ok := strings.CutPrefix(id, "race-")
	if !ok || n == "" || len(id) > MaxAIRaceID || n[0] == '0' {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// AIRaceSummary is the result of ai.race_summary: the quick model's
// difference summary of the finished lanes (at most 3 lines, redacted on
// the laptop) and its model and cost. It is stored in the race record.
type AIRaceSummary struct {
	Race  string   `json:"race"`
	Lines []string `json:"lines"`
	Cost  string   `json:"cost,omitempty"`
}

// AIRaceDone is the result of ai.race_verify and ai.race_stop: how many
// lanes it acted on and the laptop's short line about it.
type AIRaceDone struct {
	Race    string `json:"race"`
	Lanes   int    `json:"lanes"`
	Message string `json:"message"`
}

var opAIRaceRows = []Policy{
	{Op: OpAIRaceStart, New: newOf[AIRaceStartArgs](), Role: RoleController, Rate: slow, MaxBytes: 4 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpAIRaceSummary, New: newOf[AIRaceArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpAIRaceVerify, New: newOf[AIRaceArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpAIRaceStop, New: newOf[AIRaceArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opAIRaceRows...); return true }()

func init() {
	registerResult(OpAIRaceStart, AskRef{})
	registerResult(OpAIRaceSummary, AIRaceSummary{})
	registerResult(OpAIRaceVerify, AIRaceDone{})
	registerResult(OpAIRaceStop, AIRaceDone{})
}
