package ghci

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// ReviewKind is the verdict of a review.
type ReviewKind string

const (
	ReviewApprove        ReviewKind = "approve"
	ReviewRequestChanges ReviewKind = "request-changes"
	ReviewComment        ReviewKind = "comment"
)

// ErrNeedsText: Request changes and Comment need a message.
var ErrNeedsText = errors.New("a message is needed for this review")

// Review sends a review (`gh pr review N --approve|--request-changes|
// --comment`). Approve may have an empty body; the others need one.
func Review(ctx context.Context, run Runner, dir string, number int, kind ReviewKind, body string) error {
	body = strings.TrimSpace(body)
	args := []string{"pr", "review", strconv.Itoa(number)}
	switch kind {
	case ReviewApprove:
		args = append(args, "--approve")
	case ReviewRequestChanges:
		args = append(args, "--request-changes")
	case ReviewComment:
		args = append(args, "--comment")
	default:
		return errors.New("unknown review kind " + string(kind))
	}
	if body == "" && kind != ReviewApprove {
		return ErrNeedsText
	}
	if body != "" {
		args = append(args, "--body", body)
	}
	_, errOut, err := run(ctx, dir, args...)
	return classify(err, errOut)
}
