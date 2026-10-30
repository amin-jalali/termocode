package webproto

// The ops of the desktop's git views (WEBP-5): the commit graph, blame,
// the merge editor, interactive rebase, amend, clone, pull requests and
// CI checks. They join the op table below; every rule of ops.go holds
// for them. Reads are open to viewers. Whatever rewrites history or
// talks to a remote waits for a laptop card, and runs there as the
// laptop's own command (its toasts, its credentials).

// Git view op names.
const (
	OpGitGraph = "git.graph"
)

// Graph limits.
const (
	MaxGraphPage = 200  // commits in one page of git.graph
	MaxGraphSkip = 5000 // git.graph reads at most this far down
)

// GitGraphArgs asks for one page of the commit graph: Skip commits in,
// of every branch and tag, or of HEAD's history only.
type GitGraphArgs struct {
	Skip int  `json:"skip,omitempty"`
	Head bool `json:"head,omitempty"`
}

// GitGraph is the result of git.graph.
type GitGraph struct {
	Skip int           `json:"skip"`
	Rows []GitGraphRow `json:"rows"`
	More bool          `json:"more,omitempty"` // older commits follow
}

// GitGraphRow is one commit of the graph and how to draw its row: the
// node's column and lane colour, the lines of the row's top half (from
// the row above) and of its bottom half (to the row below).
type GitGraphRow struct {
	Hash     string         `json:"hash"` // short
	Parents  []string       `json:"parents"`
	Subject  string         `json:"subject"`
	Author   string         `json:"author"`
	When     int64          `json:"when"` // Unix ms (committer date)
	Refs     []GitRef       `json:"refs,omitempty"`
	Head     bool           `json:"head,omitempty"`     // HEAD points here
	Unpushed bool           `json:"unpushed,omitempty"` // on HEAD, not on its upstream
	Col      int            `json:"col"`
	Color    int            `json:"color"`
	Up       []GitGraphEdge `json:"up"`
	Down     []GitGraphEdge `json:"down"`
}

// GitRef is a branch or tag on a commit.
type GitRef struct {
	Name string `json:"name"`
	Kind string `json:"kind" enum:"local|remote|tag"`
}

// GitGraphEdge is one line of half a row: from column F to column T, in
// lane colour C (the page takes it modulo its palette).
type GitGraphEdge struct {
	F int `json:"f"`
	T int `json:"t"`
	C int `json:"c"`
}

var opGitViewRows = []Policy{
	{Op: OpGitGraph, New: newOf[GitGraphArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
}

func (a *GitGraphArgs) validate(int) error {
	if a.Skip < 0 || a.Skip > MaxGraphSkip {
		return invalid("bad skip")
	}
	return nil
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opGitViewRows...); return true }()

func init() {
	registerResult(OpGitGraph, GitGraph{})
}
