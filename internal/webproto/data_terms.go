package webproto

// Topic data of the terminal views (web-ui-design §2.5). A terminal is
// an nvim terminal buffer: its rows are the buffer's lines, already
// wrapped at the laptop's width. Rows are plain text (no colours in v1).

// TermsSnap is the "terms" snapshot and its only delta (the whole list):
// every shell, tab by tab; a split pane names its tab's buffer in PaneOf.
type TermsSnap struct {
	Terms []TermItem `json:"terms"`
}

// TermItem is one shell. Cwd is workspace-relative ("" the root), or the
// folder's name when it is outside the workspace.
type TermItem struct {
	Buf     int    `json:"buf"`
	Title   string `json:"title"`
	Cwd     string `json:"cwd"`
	Busy    bool   `json:"busy"`
	LastCmd string `json:"last_cmd,omitempty"`
	Exited  bool   `json:"exited"`
	PaneOf  int    `json:"pane_of,omitempty"`
	Task    string `json:"task,omitempty"`
	Active  bool   `json:"active"`
}

// TermSnap is the "term:<buf>" snapshot: the last rows of the terminal.
// First is the buffer row of Rows[0]; More says older rows exist
// (term.history). Screen: a full-screen program seems to run; Cols and
// Height are the laptop's terminal size (0 when unknown).
type TermSnap struct {
	Buf    int      `json:"buf"`
	First  int      `json:"first"`
	Rows   []string `json:"rows"`
	More   bool     `json:"more"`
	Tick   int      `json:"tick"`
	Screen bool     `json:"screen"`
	Cols   int      `json:"cols"`
	Height int      `json:"height"`
}

// TermResetDelta replaces the rows (the hub lost track, or the window
// needs rows it did not have).
type TermResetDelta struct {
	Kind  string   `json:"kind" const:"reset"`
	First int      `json:"first"`
	Rows  []string `json:"rows"`
	More  bool     `json:"more"`
	Tick  int      `json:"tick"`
}

// TermModeDelta: screen mode turned on or off.
type TermModeDelta struct {
	Kind   string `json:"kind" const:"mode"`
	Screen bool   `json:"screen"`
	Cols   int    `json:"cols"`
	Height int    `json:"height"`
}

// TermHistoryRes is the result of term.history: rows [First, Before).
type TermHistoryRes struct {
	First int      `json:"first"`
	Rows  []string `json:"rows"`
	More  bool     `json:"more"`
}

// TermNewRes is the result of term.new.
type TermNewRes struct {
	Buf int `json:"buf"`
}

// Terminal limits: a view keeps the last MaxTermRows rows (a lines delta
// is applied, then the window keeps its tail); term.history pages
// MaxTermRows older rows at a time.
const MaxTermRows = 2000

func init() {
	registerTopic("terms", TermsSnap{}, TermsSnap{})
	registerTopic("term", TermSnap{}, LinesDelta{}, TickDelta{}, TermResetDelta{}, TermModeDelta{}, GoneDelta{})
	registerResult(OpTermHistory, TermHistoryRes{})
	registerResult(OpTermNew, TermNewRes{})
}
