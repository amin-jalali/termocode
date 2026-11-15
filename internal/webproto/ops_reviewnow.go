package webproto

import "time"

// Review (Now + Review): one pull request read on the laptop in one fixed
// order: the question it answers (and its task), the solution in one
// line, the changes grouped with the main change first, one proof row per
// check bound to the commit it ran on (a check of an older commit is
// stale), the risk with its reasons and one short suggestion. review.list
// is the queue: the pull requests that ask for the user's review (they
// join the inbox as the user's turn). review.act approves or comments on
// exactly the commit the user saw: when new code came in it is refused
// and nothing is sent. Merging stays the user's own step elsewhere.

// Review ops.
const (
	OpReviewList = "review.list"
	OpReviewGet  = "review.get"
	OpReviewAct  = "review.act"
)

// MaxReviewHead caps a commit id.
const MaxReviewHead = 64

// ReviewActArgs approves or comments on pull request Number at commit
// Head (the head the user saw; a moved head is refused).
type ReviewActArgs struct {
	Number int    `json:"number"`
	Head   string `json:"head"`
	Do     string `json:"do" enum:"approve|comment"`
	Text   string `json:"text,omitempty"`
}

func (a *ReviewActArgs) validate(n int) error {
	if err := (&PRNumberArgs{Number: a.Number}).validate(n); err != nil {
		return err
	}
	if len(a.Head) < 7 || len(a.Head) > MaxReviewHead {
		return invalid("bad head")
	}
	for _, r := range a.Head {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return invalid("bad head")
		}
	}
	switch a.Do {
	case "approve":
	case "comment":
		if a.Text == "" {
			return invalid("a comment needs text")
		}
	default:
		return invalid("do is approve or comment")
	}
	if len(a.Text) > MaxReviewText || hasControlExceptNL(a.Text) {
		return invalid("bad text")
	}
	return nil
}

func hasControlExceptNL(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return true
		}
	}
	return false
}

// ReviewQueue is the result of review.list: the pull requests that wait
// for the user's review, oldest ask first.
type ReviewQueue struct {
	Items []ReviewQueued `json:"items"`
}

// ReviewQueued is one pull request in the queue.
type ReviewQueued struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Bot    bool   `json:"bot,omitempty"`
	Draft  bool   `json:"draft,omitempty"`
}

// ReviewSnap is the result of review.get.
type ReviewSnap struct {
	Number     int              `json:"number"`
	Title      string           `json:"title"`
	Author     string           `json:"author"`
	URL        string           `json:"url"`
	Head       string           `json:"head"`
	Branch     string           `json:"branch"`
	Add        int              `json:"add"`
	Del        int              `json:"del"`
	Files      int              `json:"files"`
	Question   string           `json:"question,omitempty"`
	Task       string           `json:"task,omitempty"`
	Solution   string           `json:"solution"`
	Groups     []ReviewGroup    `json:"groups"`
	Proof      []ReviewProof    `json:"proof"`
	Risk       ReviewRisk       `json:"risk"`
	Suggestion ReviewSuggestion `json:"suggestion"`
	Agent      string           `json:"agent,omitempty"` // the agent lane that made it, when one did
	State      string           `json:"state" enum:"OPEN|CLOSED|MERGED"`
	Approved   bool             `json:"approved,omitempty"` // GitHub says it is approved
	Diff       []ReviewDiffLine `json:"diff,omitempty"`     // the main change's first lines, when the laptop has them
}

// ReviewGroup is one group of changes; Main is the main change.
type ReviewGroup struct {
	Title string   `json:"title"`
	Files []string `json:"files"`
	Add   int      `json:"add"`
	Del   int      `json:"del"`
	Main  bool     `json:"main,omitempty"`
}

// ReviewProof is one proof row: a check and the commit (short) it ran on.
type ReviewProof struct {
	Name  string `json:"name"`
	State string `json:"state" enum:"ok|fail|running|stale|notrun"`
	SHA   string `json:"sha,omitempty"`
	URL   string `json:"url,omitempty"`
	Fix   bool   `json:"fix,omitempty"`
	Again bool   `json:"again,omitempty"`
}

// ReviewRisk is the risk band with its reasons, the highest first.
type ReviewRisk struct {
	Level   string   `json:"level" enum:"low|medium|high"`
	Reasons []string `json:"reasons"`
}

// ReviewSuggestion is one short suggestion; Notes counts what stands
// before an approval.
type ReviewSuggestion struct {
	Text  string `json:"text"`
	Notes int    `json:"notes,omitempty"`
}

// ReviewDiffLine is one line of the main change.
type ReviewDiffLine struct {
	N    int    `json:"n"`
	Kind string `json:"kind" enum:"ctx|add|del"`
	Text string `json:"text"`
}

// ReviewActResult names the commit the review was sent on (short).
type ReviewActResult struct {
	Number int    `json:"number"`
	Head   string `json:"head"`
}

var opReviewNowRows = []Policy{
	{Op: OpReviewList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpReviewGet, New: newOf[PRNumberArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpReviewAct, New: newOf[ReviewActArgs](), Role: RoleController, Rate: Rate{N: 2, Per: time.Second}, MaxBytes: 24 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opReviewNowRows...); return true }()

func init() {
	registerResult(OpReviewList, ReviewQueue{})
	registerResult(OpReviewGet, ReviewSnap{})
	registerResult(OpReviewAct, ReviewActResult{})
}
