package webproto

import (
	"strconv"
	"strings"
)

// The git lists of the desktop's quick input (WEBP-9, Git): the laptop's
// Git: Show Tags… (pick one to check it out) and Git: Show Stash List…
// (pick one to apply it). The lists are reads; checking out and applying
// change the working files and wait for a laptop card, which takes only
// an entry of the list the page was given.

// Git list op names.
const (
	OpGitTags        = "git.tags"
	OpGitCheckoutTag = "git.checkout_tag"
	OpGitStashList   = "git.stash_list"
	OpGitStashApply  = "git.stash_apply"
)

// MaxGitTags caps the tags of one git.tags (newest first).
const MaxGitTags = 500

// GitTags is the result of git.tags: the tag names, newest first; More
// counts the ones left out.
type GitTags struct {
	Tags []string `json:"tags"`
	More int      `json:"more,omitempty"`
}

// GitTagArgs names a tag of the last git.tags.
type GitTagArgs struct {
	Tag string `json:"tag"`
}

// GitStashList is the result of git.stash_list, newest first.
type GitStashList struct {
	Entries []GitStashEntry `json:"entries"`
}

// GitStashEntry is one stash entry: its ref ("stash@{0}") and message.
type GitStashEntry struct {
	Ref     string `json:"ref"`
	Message string `json:"message"`
}

// GitStashRefArgs names a stash entry of the last git.stash_list.
type GitStashRefArgs struct {
	Ref string `json:"ref"`
}

var opGitListRows = []Policy{
	{Op: OpGitTags, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitCheckoutTag, New: newOf[GitTagArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitStashList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitStashApply, New: newOf[GitStashRefArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

// validRefName is a branch or tag name git takes (check-ref-format) and
// that no git command reads as an option or a range.
func validRefName(n string) bool {
	if n == "" || len(n) > 255 || n[0] == '-' || n[0] == '/' || hasControl(n) || strings.ContainsAny(n, " ~^:?*[\\") ||
		strings.Contains(n, "..") || strings.Contains(n, "@{") || strings.Contains(n, "//") ||
		strings.HasSuffix(n, "/") || strings.HasSuffix(n, ".") || strings.HasSuffix(n, ".lock") || n == "@" {
		return false
	}
	return true
}

func (a *GitTagArgs) validate(int) error {
	if !validRefName(a.Tag) {
		return invalid("bad tag")
	}
	return nil
}

// stashIndex reads n of "stash@{n}" (-1: not one).
func stashIndex(ref string) int {
	s, ok := strings.CutPrefix(ref, "stash@{")
	if !ok {
		return -1
	}
	s, ok = strings.CutSuffix(s, "}")
	n, err := strconv.Atoi(s)
	if !ok || err != nil || n < 0 || n > 9999 || strconv.Itoa(n) != s {
		return -1
	}
	return n
}

func (a *GitStashRefArgs) validate(int) error {
	if stashIndex(a.Ref) < 0 {
		return invalid("bad stash ref")
	}
	return nil
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opGitListRows...); return true }()

func init() {
	registerResult(OpGitTags, GitTags{})
	registerResult(OpGitStashList, GitStashList{})
	registerResult(OpGitCheckoutTag, AskRef{})
	registerResult(OpGitStashApply, AskRef{})
}
