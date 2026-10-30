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

	OpGitRebasePlan  = "git.rebase_plan"
	OpGitRebase      = "git.rebase"
	OpGitRebaseState = "git.rebase_state"
	OpGitRebaseCtl   = "git.rebase_ctl"

	OpGitClone = "git.clone"
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

// MaxRebaseSteps caps the commits of one interactive rebase from the web.
const MaxRebaseSteps = 300

// GitRebasePlanArgs starts an interactive rebase at commit From: it and
// every newer commit of HEAD are listed.
type GitRebasePlanArgs struct {
	From string `json:"from"`
}

// GitRebasePlan is the result of git.rebase_plan: the commits from Base
// (not included; "" : from the root) to HEAD, oldest first, all picks.
// Pushed of them are on Upstream already (rewriting them means a force
// push); Dirty: there are uncommitted changes (the rebase stashes them).
type GitRebasePlan struct {
	Base     string          `json:"base"` // full hash
	Branch   string          `json:"branch"`
	Steps    []GitRebaseStep `json:"steps"`
	Upstream string          `json:"upstream,omitempty"`
	Pushed   int             `json:"pushed,omitempty"`
	Dirty    bool            `json:"dirty,omitempty"`
}

// GitRebaseStep is one row of an interactive rebase: its commit, what to
// do with it, and a reword's new message.
type GitRebaseStep struct {
	Hash    string `json:"hash"`
	Action  string `json:"action" enum:"pick|reword|edit|squash|fixup|drop"`
	Message string `json:"message,omitempty"`
	Subject string `json:"subject,omitempty"` // git.rebase_plan only
	Body    string `json:"body,omitempty"`    // git.rebase_plan only
}

// GitRebaseArgs runs the rows of the last git.rebase_plan in this order
// with these actions, onto Base (as the plan gave it).
type GitRebaseArgs struct {
	Base  string          `json:"base"`
	Steps []GitRebaseStep `json:"steps"`
}

// GitRebaseState is the result of git.rebase_state: where a stopped
// rebase is (InProgress false: none).
type GitRebaseState struct {
	InProgress bool     `json:"inProgress"`
	Step       int      `json:"step,omitempty"`
	Total      int      `json:"total,omitempty"`
	Action     string   `json:"action,omitempty"`
	Hash       string   `json:"hash,omitempty"`
	Subject    string   `json:"subject,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	Conflicts  []string `json:"conflicts,omitempty"`
}

// GitRebaseCtlArgs goes on with a stopped rebase, skips its commit or
// puts the branch back as it was.
type GitRebaseCtlArgs struct {
	Do string `json:"do" enum:"continue|skip|abort"`
}

// MaxCloneURL caps a clone URL.
const MaxCloneURL = 2048

// GitCloneArgs clones a remote repository next to (or into) the open
// folder, as the laptop's Git: Clone does; the laptop then opens it.
type GitCloneArgs struct {
	URL string `json:"url"`
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
	{Op: OpGitRebasePlan, New: newOf[GitRebasePlanArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitRebaseState, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitRebase, New: newOf[GitRebaseArgs](), Role: RoleController, Rate: slow, MaxBytes: 256 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitRebaseCtl, New: newOf[GitRebaseCtlArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitClone, New: newOf[GitCloneArgs](), Role: RoleController, Rate: slow, MaxBytes: 4 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
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

// validHash: 4 to 64 lowercase hex digits.
func validHash(h string) bool {
	return len(h) >= 4 && len(h) <= 64 && strings.Trim(h, "0123456789abcdef") == ""
}

func (a *GitRebasePlanArgs) validate(int) error {
	if !validHash(a.From) {
		return invalid("bad commit")
	}
	return nil
}

var rebaseActions = map[string]bool{"pick": true, "reword": true, "edit": true, "squash": true, "fixup": true, "drop": true}

func (a *GitRebaseArgs) validate(n int) error {
	if a.Base != "" && !validHash(a.Base) {
		return invalid("bad base")
	}
	if len(a.Steps) == 0 || len(a.Steps) > MaxRebaseSteps {
		return invalid("%d steps", len(a.Steps))
	}
	for _, s := range a.Steps {
		if !validHash(s.Hash) || !rebaseActions[s.Action] || s.Subject != "" || s.Body != "" {
			return invalid("bad step")
		}
		if s.Message != "" && s.Action != "reword" {
			return invalid("a message on a %s step", s.Action)
		}
		if s.Message != "" {
			if err := (&GitCommitArgs{Message: s.Message}).validate(n); err != nil {
				return err
			}
		} else if s.Action == "reword" {
			return invalid("a reword without a message")
		}
	}
	return nil
}

func (a *GitRebaseCtlArgs) validate(int) error {
	if a.Do != "continue" && a.Do != "skip" && a.Do != "abort" {
		return invalid("bad do")
	}
	return nil
}

// cloneHost: a host name (letters, digits, dots, dashes; not starting
// with a dash) with an optional port.
func cloneHost(h string) bool {
	h, port, _ := strings.Cut(h, ":")
	if h == "" || h[0] == '-' || len(h) > 253 || strings.Trim(port, "0123456789") != "" {
		return false
	}
	for _, c := range h {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

// clonePath: a repository path with at least one name in it.
func clonePath(p string) bool {
	p = strings.Trim(p, "/")
	return p != "" && !strings.HasPrefix(p, "-") && strings.Trim(p, "./") != ""
}

func cloneUser(u string) bool {
	if u == "" || u[0] == '-' {
		return false
	}
	for _, c := range u {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (a *GitCloneArgs) validate(int) error {
	u := a.URL
	if u == "" || len(u) > MaxCloneURL || hasControl(u) || strings.ContainsAny(u, " \t\\") {
		return invalid("bad url")
	}
	switch {
	case strings.HasPrefix(u, "https://"):
		host, path, _ := strings.Cut(strings.TrimPrefix(u, "https://"), "/")
		if strings.Contains(host, "@") || !cloneHost(host) || !clonePath(path) {
			return invalid("bad url")
		}
	case strings.HasPrefix(u, "ssh://"):
		rest := strings.TrimPrefix(u, "ssh://")
		host, path, _ := strings.Cut(rest, "/")
		if user, h, ok := strings.Cut(host, "@"); ok {
			if !cloneUser(user) {
				return invalid("bad url")
			}
			host = h
		}
		if !cloneHost(host) || !clonePath(path) {
			return invalid("bad url")
		}
	default: // scp-like: user@host:path
		user, rest, ok := strings.Cut(u, "@")
		host, path, ok2 := strings.Cut(rest, ":")
		if !ok || !ok2 || !cloneUser(user) || strings.Contains(host, ":") || !cloneHost(host) || !clonePath(path) {
			return invalid("bad url")
		}
	}
	return nil
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
	registerResult(OpGitRebasePlan, GitRebasePlan{})
	registerResult(OpGitRebaseState, GitRebaseState{})
	registerResult(OpGitRebase, AskRef{})
	registerResult(OpGitRebaseCtl, AskRef{})
	registerResult(OpGitClone, AskRef{})
}
