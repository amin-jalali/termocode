package webproto

// Project tasks on the phone (MOB-52): one read that lists a project's
// own checklists (TASKS.md, TODO.md; internal/projtasks), its open
// GitHub issues and pull requests (the laptop's gh) and its open GitLab
// merge requests (the token stays in the laptop's secret store). The
// door (MOB-50) routes the request to the project's hub by the frame's
// ws, so the args never name a project or a path.

// OpProjectTasks lists a project's tasks.
const OpProjectTasks = "project.tasks"

// ProjectTasksArgs narrows project.tasks: Only one kind ("" all), Done
// keeps the checked checklist items too.
type ProjectTasksArgs struct {
	Only string `json:"only,omitempty" enum:"checklist|issues|prs|mrs"`
	Done bool   `json:"done,omitempty"`
}

// Caps of project.tasks.
const (
	MaxProjectTaskItems  = 200 // per section
	MaxProjectTaskLabels = 6
)

// ProjectTasks is the result of project.tasks: one section per source
// that the project has, checklists first.
type ProjectTasks struct {
	Sections []ProjectTaskSection `json:"sections"`
}

// ProjectTaskSection is one source's tasks.
type ProjectTaskSection struct {
	Kind  string        `json:"kind" enum:"checklist|issues|prs|mrs"`
	Title string        `json:"title"` // "TASKS.md", "Issues", "Pull requests", "Merge requests"
	Items []ProjectTask `json:"items"`
	Total int           `json:"total"`          // items before the cap (and, for a checklist, before done ones were left out)
	Open  int           `json:"open"`           // open items in Total
	Note  string        `json:"note,omitempty"` // why it is empty or partial (no gh, sign in on the laptop, …)
}

// ProjectTask is one task row.
type ProjectTask struct {
	ID     string   `json:"id"` // "TASKS.md:12", "issue:41", "pr:14", "mr:3"
	Kind   string   `json:"kind" enum:"ptask|issue|pr|mr"`
	Title  string   `json:"title"`
	State  string   `json:"state" enum:"open|done|draft"`
	Number int      `json:"number,omitempty"` // issue, pull request or merge request number
	Path   string   `json:"path,omitempty"`   // checklist: the file, project-relative
	Line   int      `json:"line,omitempty"`   // checklist: 1-based
	Group  string   `json:"group,omitempty"`  // checklist: the heading above it
	Depth  int      `json:"depth,omitempty"`  // checklist: nesting
	Author string   `json:"author,omitempty"`
	Labels []string `json:"labels,omitempty"` // ≤ MaxProjectTaskLabels
	Checks string   `json:"checks,omitempty" enum:"none|pending|pass|fail"`
	Branch string   `json:"branch,omitempty"` // pull / merge request head
	URL    string   `json:"url,omitempty"`
}

// gh-backed: one call per second at most, as pr.list.
var opProjectTaskRows = []Policy{
	{Op: OpProjectTasks, New: newOf[ProjectTasksArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
}

func (a *ProjectTasksArgs) validate(int) error {
	switch a.Only {
	case "", "checklist", "issues", "prs", "mrs":
		return nil
	}
	return invalid("bad only: use checklist, issues, prs or mrs")
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opProjectTaskRows...); return true }()

func init() {
	registerResult(OpProjectTasks, ProjectTasks{})
}
