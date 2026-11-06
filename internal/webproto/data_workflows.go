package webproto

// Workflows (WFX-19, plan §5.7): the phone sees the workflows of the
// laptop's workspace, their recent runs and a short live tail, and may
// stop a run. It never gets a file's contents, an item's data or a
// secret (plan §6.10). A run's write gate comes as a cards topic card of
// kind "workflow": Deny first, Allow once; "Allow for this workflow"
// stays on the laptop (it changes what runs unattended).

// Workflow op names.
const (
	OpWorkflowsStop = "workflows.stop"
)

var opWorkflowsRows = []Policy{
	{Op: OpWorkflowsStop, New: newOf[WorkflowRunArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opWorkflowsRows...); return true }()

func init() {
	registerTopic("workflows", WorkflowsSnap{}, WorkflowsSnap{})
}

// Workflows limits.
const (
	MaxWorkflows     = 100 // workflows listed
	MaxWorkflowRuns  = 30  // runs listed, newest first
	MaxWorkflowTail  = 20  // live lines of a running run
	MaxWorkflowLine  = 300 // bytes of one live line
	MaxWorkflowRunID = 64
)

// WorkflowsSnap is the workflows topic; it sends the whole list.
type WorkflowsSnap struct {
	Workflows []WorkflowItem `json:"workflows"`
	Runs      []WorkflowRun  `json:"runs"`
}

// WorkflowItem is one workflow.
type WorkflowItem struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	State string `json:"state" enum:"running|on|off|untrusted|broken"`
	When  string `json:"when,omitempty"` // "weekdays 09:00 Europe/Berlin", "" by hand
	Next  int64  `json:"next,omitempty"` // Unix ms of the next scheduled run
	Last  string `json:"last,omitempty"` // the newest run's state
}

// WorkflowRun is one run.
type WorkflowRun struct {
	ID       string   `json:"id"`
	Workflow string   `json:"workflow"`
	State    string   `json:"state"` // running, done, failed, cancelled, paused, interrupted
	Dry      bool     `json:"dry,omitempty"`
	Started  int64    `json:"started"`
	Ended    int64    `json:"ended,omitempty"`
	Items    int      `json:"items,omitempty"`
	Failed   int      `json:"failed,omitempty"`
	Wrote    int      `json:"wrote,omitempty"`
	Resumed  int      `json:"resumed,omitempty"`
	Waiting  string   `json:"waiting,omitempty"` // the step whose write card waits
	Tail     []string `json:"tail,omitempty"`    // a running run's last lines, redacted
	Stopping bool     `json:"stopping,omitempty"`
}

// WorkflowRunArgs names a run.
type WorkflowRunArgs struct {
	Run string `json:"run"`
}

func (a *WorkflowRunArgs) validate(int) error {
	if !validName(a.Run, MaxWorkflowRunID) || !validFileName(a.Run) {
		return invalid("bad run id")
	}
	return nil
}
