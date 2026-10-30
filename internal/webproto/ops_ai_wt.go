package webproto

import "strings"

// The agent worktrees view of the desktop page (WEBP-3): the laptop's
// "AI: Agent Worktrees…" list. Steps of an agent run that work at the
// same time each get their own git worktree; the page lists them with
// the files each one changed and shows a change as a diff. Merge and
// Discard change the user's files or remove the agent's work, so they
// run only after a laptop card (Cancel first); a merge conflict asks on
// the laptop again, as there.

// Agent worktree op names.
const (
	OpAIWorktrees       = "ai.worktrees"
	OpAIWorktreeText    = "ai.worktree_text"
	OpAIWorktreeMerge   = "ai.worktree_merge"
	OpAIWorktreeDiscard = "ai.worktree_discard"
)

// Agent worktree limits.
const (
	MaxAIWorktrees     = 64
	MaxAIWorktreeFiles = 200
	MaxAIWorktreeID    = 64
)

// AIWorktreeArgs names an agent worktree by its id.
type AIWorktreeArgs struct {
	ID string `json:"id"`
}

func (a *AIWorktreeArgs) validate(int) error {
	if !validWorktreeID(a.ID) {
		return invalid("bad worktree id")
	}
	return nil
}

// AIWorktreeTextArgs asks for one changed file of a worktree.
type AIWorktreeTextArgs struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

func (a *AIWorktreeTextArgs) validate(int) error {
	if !validWorktreeID(a.ID) {
		return invalid("bad worktree id")
	}
	if !validPath(a.Path, false) || strings.HasSuffix(a.Path, "/") {
		return invalid("bad path")
	}
	return nil
}

// validWorktreeID: letters, digits, '.', '_' and '-', not first a '.' or
// a '-' (never an option or a path).
func validWorktreeID(id string) bool {
	if id == "" || len(id) > MaxAIWorktreeID || id[0] == '.' || id[0] == '-' {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// AIWorktrees is the result of ai.worktrees. Off: the laptop's
// ai_worktrees setting is off (steps work in the user's tree); NoRepo:
// the workspace is not a git repository with a commit.
type AIWorktrees struct {
	Off    bool         `json:"off,omitempty"`
	NoRepo bool         `json:"noRepo,omitempty"`
	Items  []AIWorktree `json:"items"`
}

// AIWorktree is one agent worktree: its branch, the step that owns it
// ("" when it is left over from a stopped run or another chat), whether
// that step still runs (its files are not counted then) and the files it
// changed (Cut: more than MaxAIWorktreeFiles).
type AIWorktree struct {
	ID      string             `json:"id"`
	Branch  string             `json:"branch"`
	Owner   string             `json:"owner,omitempty"`
	Running bool               `json:"running,omitempty"`
	Files   []AIWorktreeChange `json:"files"`
	Cut     bool               `json:"cut,omitempty"`
}

// AIWorktreeChange is one file the agent changed (repository path).
type AIWorktreeChange struct {
	Path   string `json:"path"`
	Status string `json:"status" enum:"A|M|D"`
}

// AIWorktreeText is the result of ai.worktree_text: the file before the
// agent and after it (MaxGitText each). Secret: the file may hold
// secrets and stays on the laptop.
type AIWorktreeText struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Before    string `json:"before"`
	After     string `json:"after"`
	Binary    bool   `json:"binary,omitempty"`
	Secret    bool   `json:"secret,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

var opAIWorktreeRows = []Policy{
	{Op: OpAIWorktrees, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpAIWorktreeText, New: newOf[AIWorktreeTextArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 8 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpAIWorktreeMerge, New: newOf[AIWorktreeArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpAIWorktreeDiscard, New: newOf[AIWorktreeArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opAIWorktreeRows...); return true }()

func init() {
	registerResult(OpAIWorktrees, AIWorktrees{})
	registerResult(OpAIWorktreeText, AIWorktreeText{})
	registerResult(OpAIWorktreeMerge, AskRef{})
	registerResult(OpAIWorktreeDiscard, AskRef{})
}
