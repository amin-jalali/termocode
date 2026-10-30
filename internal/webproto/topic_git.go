package webproto

// The git data (web design 2.6): the status list, the branch and the last
// commits as the laptop's Source Control view has them, and the diff of
// one file or one commit in pages. The topic's delta is the whole
// snapshot again, sent when the laptop's own git refresh changed it,
// never on a web timer.

func init() {
	registerTopic("git", GitSnap{}, GitSnap{})
	registerResult(OpGitDiff, GitDiff{})
	registerResult(OpGitShow, GitDiff{})
	registerResult(OpGitCommit, GitCommitted{})
	registerResult(OpGitDiscard, AskRef{})
}

// Git limits.
const (
	MaxGitFiles    = 2000      // files in the git topic; More counts the rest
	MaxGitLog      = 50        // commits in the git topic, newest first
	MaxGitDiff     = 512 << 10 // bytes of git's text read for one diff or show; more is cut
	MaxGitDiffPage = 64 << 10  // bytes of row text in one page of a diff
	MaxGitDiffRows = 2000      // rows in one page of a diff
	MaxHunkHeader  = 1 << 10   // bytes of a hunk header line
)

// GitSnap is the git topic.
type GitSnap struct {
	Repo   bool        `json:"repo"` // false: the workspace is not a git repository
	Branch GitBranch   `json:"branch"`
	Op     string      `json:"op,omitempty" enum:"rebase|merge"` // a rebase or merge stopped half-way: finish it on the laptop
	Files  []GitFile   `json:"files"`                            // as git status lists them
	More   int         `json:"more,omitempty"`                   // changed files past MaxGitFiles, not listed
	Log    []GitCommit `json:"log"`                              // the last commits of HEAD, newest first
}

// GitBranch is the checked-out branch.
type GitBranch struct {
	Name   string `json:"name"` // "(detached 1a2b3c4)" when HEAD is not on a branch
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
}

// GitFile is one changed file. X and Y are git status' two columns (the
// index and the work tree): " ", "M", "A", "D", "R", "C", "U" or "?".
type GitFile struct {
	Path      string `json:"path"`
	X         string `json:"x"`
	Y         string `json:"y"`
	Staged    bool   `json:"staged,omitempty"`    // has a staged change
	Unstaged  bool   `json:"unstaged,omitempty"`  // has a change in the work tree
	Untracked bool   `json:"untracked,omitempty"` // new, unknown to git ("??"); a path ending in "/" is a folder
	Conflict  bool   `json:"conflict,omitempty"`  // a merge conflict: read-only here
}

// GitCommit is one commit of the log.
type GitCommit struct {
	Hash     string `json:"hash"` // short
	Subject  string `json:"subject"`
	Author   string `json:"author"`
	When     int64  `json:"when"`               // Unix ms (committer date)
	Unpushed bool   `json:"unpushed,omitempty"` // on HEAD but not on its upstream
}

// GitDiff is one page of a file's diff (git.diff) or a commit (git.show).
// Rows of kind "file" start a file (Text is its path, git.show only);
// "hunk" rows carry the hunk's header line in Text, which git.stage_hunk
// and git.unstage_hunk take back as their precondition.
type GitDiff struct {
	Path      string         `json:"path,omitempty"`   // git.diff: the file
	Staged    bool           `json:"staged,omitempty"` // git.diff: the staged side
	Commit    *GitCommitInfo `json:"commit,omitempty"` // git.show
	Rows      []DiffRow      `json:"rows"`
	From      int            `json:"from"`           // the index of this page's first row
	Next      int            `json:"next,omitempty"` // the from of the next page; 0: this is the last page
	Added     int            `json:"added"`          // of the whole diff read
	Removed   int            `json:"removed"`
	Binary    bool           `json:"binary,omitempty"`    // a binary file: no rows
	Truncated bool           `json:"truncated,omitempty"` // git's text passed MaxGitDiff: the rest is on the laptop
}

// GitCommitInfo heads a git.show.
type GitCommitInfo struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Body    string `json:"body,omitempty"`
	Author  string `json:"author"`
	When    int64  `json:"when"` // Unix ms
}

// GitCommitted is the result of git.commit.
type GitCommitted struct {
	Hash  string `json:"hash"`  // short
	Files int    `json:"files"` // files the commit changed
}

// AskRef is the result of an op that waits for the laptop (a laptop
// card): the id of the card in the cards topic. The page shows "Waiting
// for the laptop…" until the answered event for that card arrives; its
// choice says what the laptop did.
type AskRef struct {
	Card string `json:"card"`
}
