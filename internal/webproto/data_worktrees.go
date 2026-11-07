package webproto

import "strings"

// Worktrees (WTX-16): the laptop's WORKTREES view — every git worktree
// of the repository (the main checkout, the user's worktrees, agent
// lanes, ones made by hand, archived ones) with its group, agent state,
// branch status, last note and link. The page names a worktree by its
// id (a hash of its path that the laptop made; the page never sends a
// path). Pin, sleep / wake and a note run on the laptop as there (sleep
// with agent tabs or children asks on the laptop first: Keep running
// first). Archive and delete wait for a laptop card; the laptop's own
// steps follow (changed files, the branch name). Filtering is the
// page's. The topic sends the whole list.

// Worktree op names.
const (
	OpWorktreesRefresh = "worktrees.refresh"
	OpWorktreesPin     = "worktrees.pin"
	OpWorktreesSleep   = "worktrees.sleep"
	OpWorktreesNote    = "worktrees.note"
	OpWorktreesArchive = "worktrees.archive"
	OpWorktreesDelete  = "worktrees.delete"
	// A new task from the page (CMP-49): the form's choices, then a new
	// worktree with an agent that gets the task.
	OpWorktreesTaskForm = "worktrees.task_form"
	OpWorktreesNewTask  = "worktrees.new_task"
)

var opWorktreesRows = []Policy{
	{Op: OpWorktreesRefresh, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpWorktreesPin, New: newOf[WorktreeOnArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpWorktreesSleep, New: newOf[WorktreeOnArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorktreesNote, New: newOf[WorktreeNoteArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 2 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorktreesArchive, New: newOf[WorktreeArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorktreesDelete, New: newOf[WorktreeArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorktreesTaskForm, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpWorktreesNewTask, New: newOf[WorktreeTaskArgs](), Role: RoleController, Rate: slow, MaxBytes: 8 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opWorktreesRows...); return true }()

func init() {
	registerTopic("worktrees", WorktreesSnap{}, WorktreesSnap{})
	registerResult(OpWorktreesPin, WorktreeDone{})
	registerResult(OpWorktreesSleep, WorktreeDone{})
	registerResult(OpWorktreesNote, WorktreeDone{})
	registerResult(OpWorktreesArchive, AskRef{})
	registerResult(OpWorktreesDelete, AskRef{})
	registerResult(OpWorktreesTaskForm, WorktreeTaskForm{})
	registerResult(OpWorktreesNewTask, WorktreeTaskDone{})
}

// Worktree limits.
const (
	MaxWorktrees    = 500
	MaxWorktreeNote = 500
	WorktreeIDLen   = 12
	// A new task (CMP-49).
	MaxTaskName   = 100
	MaxTaskText   = 4 * kib
	MaxTaskRef    = 200
	MaxTaskRefs   = 200
	MaxTaskAgents = 8
	MaxTaskAgent  = 32
)

// WorktreesSnap is the worktrees topic. State: "none" the workspace is
// not a git repository, "loading" before the laptop read it, "ready".
type WorktreesSnap struct {
	State     string         `json:"state" enum:"none|loading|ready"`
	Worktrees []WorktreeItem `json:"worktrees"`
	More      int            `json:"more,omitempty"`
}

// WorktreeItem is one worktree card. Where is its folder for people
// (~ for home); Parent is the id of the worktree it was made from and
// Depth how deep it is drawn under it. Status: Ahead, Behind and Dirty
// are known. LastSeen is Unix ms.
type WorktreeItem struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Branch     string `json:"branch,omitempty"`
	Where      string `json:"where"`
	Kind       string `json:"kind" enum:"main|user|agent|external"`
	Group      string `json:"group" enum:"pinned|active|asleep|external|archived"`
	State      string `json:"state" enum:"working|waiting|done|idle|asleep|gone|archived"`
	Pinned     bool   `json:"pinned,omitempty"`
	Here       bool   `json:"here,omitempty"`
	Locked     bool   `json:"locked,omitempty"`
	Gone       bool   `json:"gone,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Agents     int    `json:"agents,omitempty"`
	Status     bool   `json:"status,omitempty"`
	Ahead      int    `json:"ahead,omitempty"`
	Behind     int    `json:"behind,omitempty"`
	Dirty      int    `json:"dirty,omitempty"`
	Note       string `json:"note,omitempty"`
	Checkpoint string `json:"checkpoint,omitempty"`
	Link       string `json:"link,omitempty"`
	Parent     string `json:"parent,omitempty"`
	Depth      int    `json:"depth,omitempty"`
	LastSeen   int64  `json:"last_seen,omitempty"`
}

// WorktreeDone answers pin, sleep and note. Asked: the laptop asks first
// (sleep would end agent tabs); nothing changed until it says yes.
type WorktreeDone struct {
	Asked bool   `json:"asked,omitempty"`
	Note  string `json:"note,omitempty"`
}

// WorktreeArgs names a worktree.
type WorktreeArgs struct {
	ID string `json:"id"`
}

// WorktreeOnArgs pins / unpins, or sleeps / wakes, a worktree.
type WorktreeOnArgs struct {
	ID string `json:"id"`
	On bool   `json:"on"`
}

// WorktreeNoteArgs leaves a one-line note on a worktree.
type WorktreeNoteArgs struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// validWorktreeHexID: WorktreeIDLen lower-case hex digits.
func validWorktreeHexID(id string) bool {
	if len(id) != WorktreeIDLen {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (a *WorktreeArgs) validate(int) error {
	if !validWorktreeHexID(a.ID) {
		return invalid("bad worktree id")
	}
	return nil
}

func (a *WorktreeOnArgs) validate(int) error {
	if !validWorktreeHexID(a.ID) {
		return invalid("bad worktree id")
	}
	return nil
}

func (a *WorktreeNoteArgs) validate(int) error {
	if !validWorktreeHexID(a.ID) {
		return invalid("bad worktree id")
	}
	if !validName(a.Text, MaxWorktreeNote) {
		return invalid("bad note: one line, at most 500 bytes")
	}
	return nil
}

// WorktreeTaskForm is the result of worktrees.task_form: what the page's
// "New task" form offers. State "none": the workspace is not a git
// repository (or worktrees are off here). From is the start point the
// laptop would use; Refs the branches and tags it can start from (at
// most MaxTaskRefs, the current branch first). Agents are the installed
// agent CLIs (id and title). Setup says what the repository's setup
// commands do: "" none, "trusted" they run, "asks" the laptop asks first.
// Base is the folder new worktrees go to (~ for home).
type WorktreeTaskForm struct {
	State  string              `json:"state" enum:"none|ready"`
	From   string              `json:"from,omitempty"`
	Refs   []string            `json:"refs"`
	Agents []WorktreeTaskAgent `json:"agents"`
	Setup  string              `json:"setup,omitempty" enum:"trusted|asks"`
	Base   string              `json:"base,omitempty"`
}

// WorktreeTaskAgent is one agent CLI the form offers.
type WorktreeTaskAgent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// WorktreeTaskArgs starts a new task: a new worktree named Name (the
// branch is drafted from it; "" drafts it from the task), from the start
// point From ("" = the laptop's current branch), with the agent Agent
// that gets Task typed in once it is ready. One line each.
type WorktreeTaskArgs struct {
	Name  string `json:"name,omitempty"`
	From  string `json:"from,omitempty"`
	Agent string `json:"agent"`
	Task  string `json:"task"`
}

func (a *WorktreeTaskArgs) validate(int) error {
	if a.Name != "" && !validName(a.Name, MaxTaskName) {
		return invalid("bad name: one line, at most 100 bytes")
	}
	if a.From != "" && (!validName(a.From, MaxTaskRef) || strings.HasPrefix(a.From, "-") || strings.ContainsAny(a.From, " \t")) {
		return invalid("bad start point")
	}
	if !validWorktreeID(a.Agent) || len(a.Agent) > MaxTaskAgent {
		return invalid("bad agent")
	}
	if strings.TrimSpace(a.Task) == "" || !validText(a.Task, MaxTaskText) {
		return invalid("bad task: one line, at most 4 KiB")
	}
	return nil
}

// WorktreeTaskDone answers worktrees.new_task: the new worktree's id (as
// in the worktrees topic), its branch and folder (~ for home). Note says
// what else happens (the setup asks on the laptop first).
type WorktreeTaskDone struct {
	ID     string `json:"id"`
	Branch string `json:"branch"`
	Where  string `json:"where"`
	Note   string `json:"note,omitempty"`
}
