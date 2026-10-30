package webproto

import (
	"strings"
	"unicode/utf8"
)

// The ops of the desktop's git views (WEBP-5): the commit graph, blame,
// the merge editor, interactive rebase, amend, clone, pull requests and
// CI checks. They join the op table below; every rule of ops.go holds
// for them. Reads are open to viewers. Whatever rewrites history or
// talks to a remote waits for a laptop card, and runs there as the
// laptop's own command (its toasts, its credentials).

// Git view op names.
const (
	OpGitGraph = "git.graph"
	OpGitBlame = "git.blame"

	OpGitConflict    = "git.conflict"
	OpGitResolve     = "git.resolve"
	OpGitResolveSide = "git.resolve_side"

	OpGitAmend = "git.amend"
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

// MaxBlameLines caps the lines of one git.blame; the rest is not blamed.
const MaxBlameLines = 20000

// GitBlameArgs names the file to blame (as saved on the laptop).
type GitBlameArgs struct {
	Path string `json:"path"`
}

// GitBlame is the result of git.blame: for line n (1-based) of the saved
// file, Lines[n-1] is the index of its commit in Commits.
type GitBlame struct {
	Path    string           `json:"path"`
	Commits []GitBlameCommit `json:"commits"`
	Lines   []int            `json:"lines"`
	Cut     bool             `json:"cut,omitempty"` // more than MaxBlameLines: the rest is left out
}

// GitBlameCommit is one commit that last changed some lines. A line not
// committed yet has Uncommitted set and no hash.
type GitBlameCommit struct {
	Hash        string `json:"hash,omitempty"` // short
	Author      string `json:"author,omitempty"`
	Subject     string `json:"subject,omitempty"`
	When        int64  `json:"when,omitempty"` // Unix ms (author date)
	Uncommitted bool   `json:"uncommitted,omitempty"`
}

// MaxResolveText caps the merged text git.resolve saves.
const MaxResolveText = 1<<20 - 4<<10

// GitConflictArgs names a file with a merge conflict.
type GitConflictArgs struct {
	Path string `json:"path"`
}

// GitConflict is the result of git.conflict: the file as saved, with
// its conflict markers (line ends as "\n"), and its three sides as git's
// index keeps them. Version names the saved file; git.resolve takes it
// back, so a file changed meanwhile is not overwritten.
type GitConflict struct {
	Path    string            `json:"path"`
	Text    string            `json:"text"`
	Version string            `json:"version"`
	Ours    GitConflictSide   `json:"ours"`   // current: the branch you are on
	Theirs  GitConflictSide   `json:"theirs"` // incoming: what is merged in
	Base    GitConflictSide   `json:"base"`   // the common ancestor
	Blocks  []GitConflictPart `json:"blocks"`
}

// GitConflictSide is one side of the conflict. Present false: that side
// deleted the file. Binary: not shown as text (only git.resolve_side).
type GitConflictSide struct {
	Present bool   `json:"present"`
	Binary  bool   `json:"binary,omitempty"`
	Text    string `json:"text,omitempty"`
}

// GitConflictPart is one conflict of Text. Raw is its exact text, the
// markers included (each line ending in "\n"); each choice is the text
// that replaces Raw for it, as the laptop's merge view resolves it.
type GitConflictPart struct {
	Line        int    `json:"line"` // 1-based line of the "<<<<<<<" marker
	Raw         string `json:"raw"`
	OursLabel   string `json:"oursLabel,omitempty"`
	TheirsLabel string `json:"theirsLabel,omitempty"`
	Ours        string `json:"ours"`
	Theirs      string `json:"theirs"`
	Both        string `json:"both"`
}

// GitResolveArgs saves Text as the merged file and marks it resolved, if
// the saved file is still the one Version names.
type GitResolveArgs struct {
	Path    string `json:"path"`
	Text    string `json:"text"`
	Version string `json:"version"`
}

// GitResolveSideArgs keeps one side of the whole file ("ours": current,
// "theirs": incoming). If that side deleted the file, the file goes.
type GitResolveSideArgs struct {
	Path string `json:"path"`
	Side string `json:"side" enum:"ours|theirs"`
}

// GitResolved is the result of git.resolve.
type GitResolved struct {
	Path string `json:"path"`
	Left int    `json:"left"` // conflicted files left after this one
}

// GitAmendArgs replaces the last commit with one that also has the
// staged changes. An empty Message keeps the commit's own.
type GitAmendArgs struct {
	Message string `json:"message,omitempty"`
}

var opGitViewRows = []Policy{
	{Op: OpGitGraph, New: newOf[GitGraphArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitBlame, New: newOf[GitBlameArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 8 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitConflict, New: newOf[GitConflictArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 8 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	// Resolving writes the working file the page shows merged and stages
	// it (as the laptop's merge view does): the page asks first; the
	// version keeps a changed file from being overwritten.
	{Op: OpGitResolve, New: newOf[GitResolveArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * mib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitResolveSide, New: newOf[GitResolveSideArgs](), Role: RoleController, Rate: slow, MaxBytes: 8 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	// Rewrites history: the laptop says yes.
	{Op: OpGitAmend, New: newOf[GitAmendArgs](), Role: RoleController, Rate: slow, MaxBytes: 40 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

func (a *GitGraphArgs) validate(int) error {
	if a.Skip < 0 || a.Skip > MaxGraphSkip {
		return invalid("bad skip")
	}
	return nil
}

func (a *GitBlameArgs) validate(int) error {
	if !validPath(a.Path, false) || strings.HasSuffix(a.Path, "/") {
		return invalid("bad path")
	}
	return nil
}

func (a *GitConflictArgs) validate(int) error {
	if !validPath(a.Path, false) || strings.HasSuffix(a.Path, "/") {
		return invalid("bad path")
	}
	return nil
}

func (a *GitResolveArgs) validate(int) error {
	if !validPath(a.Path, false) || strings.HasSuffix(a.Path, "/") {
		return invalid("bad path")
	}
	if len(a.Version) != 64 || strings.Trim(a.Version, "0123456789abcdef") != "" {
		return invalid("bad version")
	}
	if len(a.Text) > MaxResolveText || !utf8.ValidString(a.Text) || strings.IndexByte(a.Text, 0) >= 0 {
		return invalid("bad text")
	}
	return nil
}

func (a *GitResolveSideArgs) validate(int) error {
	if !validPath(a.Path, false) || strings.HasSuffix(a.Path, "/") {
		return invalid("bad path")
	}
	if a.Side != "ours" && a.Side != "theirs" {
		return invalid("bad side")
	}
	return nil
}

func (a *GitAmendArgs) validate(n int) error {
	if a.Message == "" {
		return nil
	}
	return (&GitCommitArgs{Message: a.Message}).validate(n)
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opGitViewRows...); return true }()

func init() {
	registerResult(OpGitGraph, GitGraph{})
	registerResult(OpGitBlame, GitBlame{})
	registerResult(OpGitConflict, GitConflict{})
	registerResult(OpGitResolve, GitResolved{})
	registerResult(OpGitResolveSide, AskRef{})
	registerResult(OpGitAmend, AskRef{})
}
