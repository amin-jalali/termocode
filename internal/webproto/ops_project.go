package webproto

// Opening and closing a project from the phone (MOB-51, mobile plan
// §3.7). The phone names a project only by its id from the projects list
// (its ws: the workspace key, 16 hex digits, see ws.go), never by a path.
//
// project.open goes to the home hub (the door's own workspace): a project
// that does not run starts there only after a laptop card (Cancel first),
// headless (no window), up to web_projects_max at once. A project that
// runs already answers conflict: the door joins it at once.
//
// project.close goes to the project's own hub (ws set, the door routes
// it): only a project the door started headless closes, and never while
// an agent, a workflow run or a card is going there; one opened on the
// laptop is closed on the laptop.
//
// The AI can never open or close a project (no AI tool, no palette id).

// Project op names.
const (
	OpProjectOpen  = "project.open"
	OpProjectClose = "project.close"
)

// Project states as the projects topic shows them (MOB-8 / MOB-51).
const (
	ProjectOpening = "opening"
	ProjectRunning = "running"
	ProjectAsleep  = "asleep"
)

// ProjectArgs names one project by its id.
type ProjectArgs struct {
	WS string `json:"ws"`
}

func (a *ProjectArgs) validate(int) error {
	if !ValidWS(a.WS) {
		return invalid("bad project id")
	}
	return nil
}

// ProjectClosed is the result of project.close: the project stops right
// after this answer (its hub then goes, close code "project closed").
type ProjectClosed struct {
	WS string `json:"ws"`
}

var opProjectRows = []Policy{
	{Op: OpProjectOpen, New: newOf[ProjectArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpProjectClose, New: newOf[ProjectArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

// init, not a var: the rows come after every row the vars add, so the
// generated files list them last.
func init() {
	opTable = append(opTable, opProjectRows...)
	registerResult(OpProjectOpen, AskRef{})
	registerResult(OpProjectClose, ProjectClosed{})
}
