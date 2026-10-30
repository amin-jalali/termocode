package webproto

import "strings"

// Pull requests and CI checks on the desktop page (WEBP-5), read through
// the laptop's gh (its sign-in stays there). Merging, reviewing and
// checking out wait for a laptop card.

// Pull request and CI op names.
const (
	OpPRList   = "pr.list"
	OpPRView   = "pr.view"
	OpCIStatus = "ci.status"

	OpPRMerge    = "pr.merge"
	OpPRReview   = "pr.review"
	OpPRCheckout = "pr.checkout"
)

// MaxReviewText caps a review's message.
const MaxReviewText = 16 << 10

// PRMergeArgs merges a pull request: "squash", "merge" (a merge commit)
// or "rebase". Never with an admin override.
type PRMergeArgs struct {
	Number int    `json:"number"`
	Method string `json:"method" enum:"squash|merge|rebase"`
}

// PRReviewArgs sends a review: approve (the message may be empty),
// request_changes or comment (both need one).
type PRReviewArgs struct {
	Number int    `json:"number"`
	Kind   string `json:"kind" enum:"approve|request_changes|comment"`
	Body   string `json:"body,omitempty"`
}

// MaxPRNumber caps a pull request number.
const MaxPRNumber = 1_000_000_000

// PRNumberArgs names a pull request.
type PRNumberArgs struct {
	Number int `json:"number"`
}

// PRList is the result of pr.list: the repository's open pull requests.
type PRList struct {
	Items []PRItem `json:"items"`
}

// PRItem is one pull request in a list.
type PRItem struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Branch string `json:"branch"` // head
	Base   string `json:"base"`
	URL    string `json:"url"`
	Draft  bool   `json:"draft,omitempty"`
	Review string `json:"review,omitempty" enum:"APPROVED|CHANGES_REQUESTED|REVIEW_REQUIRED"`
	Checks string `json:"checks" enum:"none|pending|pass|fail"` // all checks summed up
}

// PRView is the result of pr.view.
type PRView struct {
	Item      PRItem     `json:"item"`
	State     string     `json:"state" enum:"OPEN|CLOSED|MERGED"`
	Body      string     `json:"body"` // Markdown
	Mergeable string     `json:"mergeable,omitempty"`
	Files     []PRFile   `json:"files"`
	Reviews   []PRReview `json:"reviews"` // oldest first
	Checks    []PRCheck  `json:"checks"`
}

// PRFile is one changed file of a pull request.
type PRFile struct {
	Path string `json:"path"`
	Add  int    `json:"add"`
	Del  int    `json:"del"`
}

// PRReview is one review or comment.
type PRReview struct {
	Author string `json:"author"`
	State  string `json:"state,omitempty"` // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED; "" a plain comment
	Body   string `json:"body,omitempty"`
	When   string `json:"when,omitempty"` // ISO time
}

// PRCheck is one check (a job or a commit status).
type PRCheck struct {
	Name  string `json:"name"`
	State string `json:"state" enum:"none|pending|pass|fail"` // none: skipped or neutral
	URL   string `json:"url,omitempty"`
}

// CIStatus is the result of ci.status: the laptop's CI watch of the
// checked-out branch (it asks gh itself; Loading: the first answer is
// on its way).
type CIStatus struct {
	Branch  string  `json:"branch"`
	Loading bool    `json:"loading,omitempty"`
	Error   string  `json:"error,omitempty"` // why there is nothing (no gh, no GitHub remote)
	State   string  `json:"state" enum:"none|pending|pass|fail"`
	Head    string  `json:"head,omitempty"` // the commit the runs are for (short)
	Runs    []CIRun `json:"runs"`
	PR      *PRItem `json:"pr,omitempty"`
}

// CIRun is one workflow run.
type CIRun struct {
	Workflow   string `json:"workflow"`
	Title      string `json:"title"`
	Status     string `json:"status"`               // queued, in_progress, completed, …
	Conclusion string `json:"conclusion,omitempty"` // success, failure, cancelled, …
	State      string `json:"state" enum:"pending|pass|fail|none"`
	URL        string `json:"url,omitempty"`
}

var opPRRows = []Policy{
	{Op: OpPRList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpPRView, New: newOf[PRNumberArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpCIStatus, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpPRMerge, New: newOf[PRMergeArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpPRReview, New: newOf[PRReviewArgs](), Role: RoleController, Rate: slow, MaxBytes: 24 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpPRCheckout, New: newOf[PRNumberArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

func (a *PRNumberArgs) validate(int) error {
	if a.Number < 1 || a.Number > MaxPRNumber {
		return invalid("bad number")
	}
	return nil
}

func (a *PRMergeArgs) validate(n int) error {
	if err := (&PRNumberArgs{Number: a.Number}).validate(n); err != nil {
		return err
	}
	if a.Method != "squash" && a.Method != "merge" && a.Method != "rebase" {
		return invalid("bad method")
	}
	return nil
}

func (a *PRReviewArgs) validate(n int) error {
	if err := (&PRNumberArgs{Number: a.Number}).validate(n); err != nil {
		return err
	}
	if a.Kind != "approve" && a.Kind != "request_changes" && a.Kind != "comment" {
		return invalid("bad kind")
	}
	if len(a.Body) > MaxReviewText || hasControl(strings.NewReplacer("\n", "", "\t", "").Replace(a.Body)) {
		return invalid("bad message")
	}
	if a.Kind != "approve" && strings.TrimSpace(a.Body) == "" {
		return invalid("a message is needed")
	}
	return nil
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opPRRows...); return true }()

func init() {
	registerResult(OpPRList, PRList{})
	registerResult(OpPRView, PRView{})
	registerResult(OpCIStatus, CIStatus{})
	for _, op := range []string{OpPRMerge, OpPRReview, OpPRCheckout} {
		registerResult(op, AskRef{})
	}
}
