package webproto

import (
	"strings"
	"unicode/utf8"
)

// Coordinator runs from the phone (MOB-64, mobile plan §0 "Workflows and
// agents", §3.9): the phone asks the laptop to start a planned run (a
// profile with a planner step: "Plan, then run in waves" or "Work until
// done") on a task, and may send a running step a message.
//
// ai.run_start never starts anything by itself: it is a laptop card
// (Cancel first) that names the profile, its steps and the task; only
// the laptop's yes sends the task with that profile, exactly as if the
// user had typed it there. The planner's plan then comes to the phone as
// the cards topic's "plan" card (Reject first); merge, discard and new
// processes stay laptop cards. A project profile the laptop has not
// trusted is refused.
//
// runs.send hands a running step of a run a message (agentrun.Send): it
// reads it after its current tool call. The audit line keeps the run
// key and the step, never the text.

// Coordinator op names.
const (
	OpAIRunStart = "ai.run_start"
	OpRunsSend   = "runs.send"
)

// Coordinator limits.
const (
	MaxRunTask    = 8000 // runes of a task (the agent_run_start cap)
	MaxRunTaskRef = 256  // bytes of the task's reference ("TASKS.md#MOB-64", "#412")
	MaxRunSend    = 4000 // runes of a message to a step (agentrun.SendMax)
	MaxRunStepID  = 64   // bytes of a step id
	MaxRunLive    = 8    // running steps listed on a run
)

var opCoordRows = []Policy{
	{Op: OpAIRunStart, New: newOf[AIRunStartArgs](), Role: RoleController, Rate: slow, MaxBytes: 40 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpRunsSend, New: newOf[RunsSendArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 20 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opCoordRows...); return true }()

func init() {
	registerResult(OpAIRunStart, AskRef{})
}

// AIRunStartArgs asks for a coordinator run: the task and the profile
// ("" = the built-in "plan"). Ref names where the task comes from, for
// the card and the audit line; it is never read as a path.
type AIRunStartArgs struct {
	Task    string `json:"task"`
	Profile string `json:"profile,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

func (a *AIRunStartArgs) validate(int) error {
	task := strings.TrimSpace(a.Task)
	if task == "" || !utf8.ValidString(a.Task) || utf8.RuneCountInString(a.Task) > MaxRunTask || strings.ContainsRune(a.Task, 0) {
		return invalid("bad task")
	}
	if a.Profile != "" && !validName(a.Profile, MaxAIName) {
		return invalid("bad profile")
	}
	if a.Ref != "" && !validName(a.Ref, MaxRunTaskRef) {
		return invalid("bad task reference")
	}
	return nil
}

// RunsSendArgs sends a running step of run Key a message; Step "" means
// the step the run is on (refused when more than one runs).
type RunsSendArgs struct {
	Key  string `json:"key"`
	Step string `json:"step,omitempty"`
	Text string `json:"text"`
}

func (a *RunsSendArgs) validate(int) error {
	if !validName(a.Key, MaxID) {
		return invalid("bad run key")
	}
	if a.Step != "" && !validName(a.Step, MaxRunStepID) {
		return invalid("bad step")
	}
	if strings.TrimSpace(a.Text) == "" || !utf8.ValidString(a.Text) || utf8.RuneCountInString(a.Text) > MaxRunSend || strings.ContainsRune(a.Text, 0) {
		return invalid("bad message")
	}
	return nil
}

// RunStepRef is a step of a run that runs now (runs.send's target).
type RunStepRef struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}
