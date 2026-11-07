package webproto

// The live board across projects (MOB-54, mobile plan 2.5 and 3.8). The
// board shows every project of the laptop, not only the home one: each
// termocode window that runs agents writes a small roll-up (internal/
// agentroll), and the home hub turns those into one row per project
// (working / waiting counts and the agents' names). The page puts the
// home project's own board on top, then these rows; a running project's
// full board comes over the door with ws set (MOB-50) when the user
// opens its section. Read only: the phone gets names, states and counts,
// never a path outside ~ and never what an agent wrote.

// Board op names.
const (
	OpBoardProjects = "board.projects"
)

var opBoardProjectRows = []Policy{
	{Op: OpBoardProjects, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opBoardProjectRows...); return true }()

func init() {
	registerResult(OpBoardProjects, BoardProjects{})
}

// Board limits.
const (
	MaxBoardProjects = 50 // project rows, needs-you first
	MaxBoardAgents   = 8  // agents named per project row
)

// Board project states. The page draws each with a shape too:
// ● needs, ◐ working, ○ idle.
// BoardWorking ("working") and BoardIdle ("idle") are shared with the
// board.get columns and states (data_board.go, MOB-18).
const (
	BoardNeeds = "needs"
)

// BoardProjects answers board.projects: the home project first, then the
// others that need the user, then the working ones, then by name. More
// counts the rows left out at MaxBoardProjects.
type BoardProjects struct {
	Home     string         `json:"home"` // the home workspace's key
	Projects []BoardProject `json:"projects"`
	More     int            `json:"more,omitempty"`
}

// BoardProject is one project row. WS is its workspace key (16 hex
// digits, what MOB-50 frames carry); Where is its folder with ~ for home.
// Seen is when its window last wrote its roll-up (Unix ms; 0 for home).
type BoardProject struct {
	WS      string       `json:"ws"`
	Name    string       `json:"name"`
	Where   string       `json:"where"`
	State   string       `json:"state" enum:"needs|working|idle"`
	Home    bool         `json:"home,omitempty"`
	Working int          `json:"working,omitempty"`
	Waiting int          `json:"waiting,omitempty"`
	Agents  []BoardAgent `json:"agents,omitempty"`
	More    int          `json:"more,omitempty"` // agents left out at MaxBoardAgents
	Seen    int64        `json:"seen,omitempty"`
}

// BoardAgent is one agent of a project: an AI run or a coding CLI.
type BoardAgent struct {
	Name  string `json:"name"`
	State string `json:"state" enum:"working|waiting"`
}

// BoardState is a project's state from its counts.
func BoardState(working, waiting int) string {
	switch {
	case waiting > 0:
		return BoardNeeds
	case working > 0:
		return BoardWorking
	}
	return BoardIdle
}
