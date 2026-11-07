package webproto

// Projects (MOB-8): every project the laptop knows, one door for the
// phone. A project is a workspace folder from the recent workspaces, a
// running session, an installed service or another window's agent
// roll-up; one row per folder, with what runs there and how many agents
// work or wait for the user. The topic is read only (opening, closing and
// tasks come later, each with its own op row); a project is named by an
// id the laptop makes from its folder (a hash: the page never sends a
// path). Where is ~-relative, or only the folder's name outside home.
// The topic sends the whole list.

func init() {
	registerTopic("projects", ProjectsSnap{}, ProjectsSnap{})
}

// Project limits.
const (
	MaxProjects  = 50
	ProjectIDLen = 12
)

// ProjectsSnap is the projects topic. State: "loading" before the laptop
// read its files (only this window's project is in the list), "ready".
// Working and Waiting add up every project's agents, More counts the
// projects past MaxProjects.
type ProjectsSnap struct {
	State    string        `json:"state" enum:"loading|ready"`
	Projects []ProjectItem `json:"projects"`
	Working  int           `json:"working,omitempty"`
	Waiting  int           `json:"waiting,omitempty"`
	More     int           `json:"more,omitempty"`
}

// ProjectItem is one project. State: "here" is this window's workspace,
// "running" another termocode runs there (a session or a window),
// "asleep" nothing runs. Attached: a terminal shows its session; Service:
// a user service starts it; Recent: it is in the recent workspaces.
// OpenedAt is Unix ms.
type ProjectItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Where    string `json:"where"`
	State    string `json:"state" enum:"here|running|asleep"`
	Attached bool   `json:"attached,omitempty"`
	Service  bool   `json:"service,omitempty"`
	Recent   bool   `json:"recent,omitempty"`
	Working  int    `json:"working,omitempty"`
	Waiting  int    `json:"waiting,omitempty"`
	OpenedAt int64  `json:"opened_at,omitempty"`
}
