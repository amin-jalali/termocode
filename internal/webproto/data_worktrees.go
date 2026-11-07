package webproto

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
	OpWorktreesLink    = "worktrees.link"
)

var opWorktreesRows = []Policy{
	{Op: OpWorktreesRefresh, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpWorktreesPin, New: newOf[WorktreeOnArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpWorktreesSleep, New: newOf[WorktreeOnArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorktreesNote, New: newOf[WorktreeNoteArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 2 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorktreesArchive, New: newOf[WorktreeArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorktreesDelete, New: newOf[WorktreeArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorktreesLink, New: newOf[WorktreeLinkArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opWorktreesRows...); return true }()

func init() {
	registerTopic("worktrees", WorktreesSnap{}, WorktreesSnap{})
	registerResult(OpWorktreesPin, WorktreeDone{})
	registerResult(OpWorktreesSleep, WorktreeDone{})
	registerResult(OpWorktreesNote, WorktreeDone{})
	registerResult(OpWorktreesArchive, AskRef{})
	registerResult(OpWorktreesDelete, AskRef{})
	registerResult(OpWorktreesLink, WorktreeDone{})
}

// Worktree limits.
const (
	MaxWorktrees    = 500
	MaxWorktreeNote = 500
	WorktreeIDLen   = 12
	// MaxWorktreeLinkNumber caps an issue or PR number of worktrees.link.
	MaxWorktreeLinkNumber = 999_999_999
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
// are known. LastSeen is Unix ms. Port is the worktree's dev server
// port; Running and URL say it runs now and where.
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
	Port       int    `json:"port,omitempty"`    // the worktree's dev server port (CMP-5)
	Running    bool   `json:"running,omitempty"` // its dev server runs now
	URL        string `json:"url,omitempty"`     // the dev server's address while it runs
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

// WorktreeLinkArgs links an issue or PR of the repository to a worktree
// (MOB-17, "Link to a lane" on the phone): only a kind and a number,
// never a URL or a repository, so the laptop reads it with gh on the
// repository it knows. Kind "none" with number 0 unlinks.
type WorktreeLinkArgs struct {
	ID     string `json:"id"`
	Kind   string `json:"kind" enum:"issue|pr|none"`
	Number int    `json:"number"`
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

func (a *WorktreeLinkArgs) validate(int) error {
	if !validWorktreeHexID(a.ID) {
		return invalid("bad worktree id")
	}
	switch a.Kind {
	case "none":
		if a.Number != 0 {
			return invalid("bad link: none takes number 0")
		}
	case "issue", "pr":
		if a.Number < 1 || a.Number > MaxWorktreeLinkNumber {
			return invalid("bad link: number 1 to 999999999")
		}
	default:
		return invalid("bad link kind: issue, pr or none")
	}
	return nil
}
