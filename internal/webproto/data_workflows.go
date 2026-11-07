package webproto

import (
	"strings"
	"unicode/utf8"
)

// Workflows (WFX-19, plan §5.7): the phone sees the workflows of the
// laptop's workspace, their recent runs and a short live tail, and may
// stop a run. It never gets a file's contents, an item's data or a
// secret (plan §6.10). A run's write gate comes as a cards topic card of
// kind "workflow": Deny first, Allow once; "Allow for this workflow"
// stays on the laptop (it changes what runs unattended).

//
// From the phone (MOB-60, mobile plan §3.9): workflows.dry_run starts a
// dry run at once (it reads sources read-only and never writes; the page
// says first that plan steps cost AI money) and answers with its run id.
// workflows.run and workflows.save are laptop cards: a real run may write
// to outside systems, and a save writes a file in the repo and trusts its
// exact bytes, so only the laptop confirms them (Cancel first). An
// untrusted or broken file is refused before any card.

// Workflow op names.
const (
	OpWorkflowsStop   = "workflows.stop"
	OpWorkflowsDryRun = "workflows.dry_run"
	OpWorkflowsRun    = "workflows.run"
	OpWorkflowsSave   = "workflows.save"
)

var opWorkflowsRows = []Policy{
	{Op: OpWorkflowsStop, New: newOf[WorkflowRunArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorkflowsDryRun, New: newOf[WorkflowNameArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorkflowsRun, New: newOf[WorkflowNameArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorkflowsSave, New: newOf[WorkflowSaveArgs](), Role: RoleController, Rate: slow, MaxBytes: 2*MaxWorkflowSave + 1*kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opWorkflowsRows...); return true }()

func init() {
	registerTopic("workflows", WorkflowsSnap{}, WorkflowsSnap{})
	registerResult(OpWorkflowsDryRun, WorkflowRunArgs{})
	registerResult(OpWorkflowsRun, AskRef{})
	registerResult(OpWorkflowsSave, AskRef{})
}

// Workflows limits.
const (
	MaxWorkflows     = 100 // workflows listed
	MaxWorkflowRuns  = 30  // runs listed, newest first
	MaxWorkflowTail  = 20  // live lines of a running run
	MaxWorkflowLine  = 300 // bytes of one live line
	MaxWorkflowRunID = 64
	MaxWorkflowName  = 40       // a workflow name: its file name
	MaxWorkflowSave  = 64 << 10 // bytes of a file saved from the phone
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

// WorkflowNameArgs names a workflow (dry run, run).
type WorkflowNameArgs struct {
	Name string `json:"name"`
}

// WorkflowSaveArgs is a whole workflow file to save as
// .termocode/workflows/<name>.jsonc (new, or a change of one).
type WorkflowSaveArgs struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// validWorkflowName: a lower-case letter first, then letters, digits, -
// or _ (the file name rule of workflow_draft).
func validWorkflowName(n string) bool {
	if n == "" || len(n) > MaxWorkflowName || n[0] < 'a' || n[0] > 'z' {
		return false
	}
	for _, c := range n {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (a *WorkflowNameArgs) validate(int) error {
	if !validWorkflowName(a.Name) {
		return invalid("bad workflow name")
	}
	return nil
}

func (a *WorkflowSaveArgs) validate(int) error {
	if !validWorkflowName(a.Name) {
		return invalid("bad workflow name")
	}
	switch {
	case strings.TrimSpace(a.Content) == "":
		return invalid("empty workflow file")
	case len(a.Content) > MaxWorkflowSave:
		return invalid("workflow file over %d bytes", MaxWorkflowSave)
	case !utf8.ValidString(a.Content) || strings.ContainsRune(a.Content, 0):
		return invalid("workflow file is not text")
	}
	return nil
}
