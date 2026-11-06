package ghci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Review threads of a pull request (GITX-22): the line comments people
// left on the diff, grouped as GitHub groups them, with reply and
// resolve. gh fills {owner} and {repo} from the repository of dir, so
// termocode never builds a repository name itself.

// ThreadComment is one comment of a review thread.
type ThreadComment struct {
	ID     int64 // the REST id (replies go to it)
	Author string
	Body   string
	When   string
	URL    string
}

// Thread is one review thread on a line of the diff.
type Thread struct {
	ID       string // the GraphQL node id (resolve uses it)
	Path     string
	Line     int    // the line now; for an outdated thread the line it was left on
	Side     string // RIGHT (the new file) or LEFT (the old file)
	Resolved bool
	Outdated bool
	Hunk     string // the diff hunk of the first comment
	URL      string // the link of the first comment
	Comments []ThreadComment
}

// ThreadsOpen counts the threads that are not resolved.
func ThreadsOpen(ts []Thread) int {
	n := 0
	for _, t := range ts {
		if !t.Resolved {
			n++
		}
	}
	return n
}

// threadsQuery reads up to 100 threads with up to 50 comments each.
const threadsQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){` +
	`reviewThreads(first:100){nodes{id isResolved isOutdated path line originalLine diffSide ` +
	`comments(first:50){nodes{databaseId author{login} body createdAt diffHunk url}}}}}}}`

// graphqlErrors is the error list of a GraphQL answer.
type graphqlErrors struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (g graphqlErrors) err() error {
	if len(g.Errors) == 0 {
		return nil
	}
	return errors.New("gh: " + g.Errors[0].Message)
}

// Threads reads the review threads of pull request number.
func Threads(ctx context.Context, run Runner, dir string, number int) ([]Thread, error) {
	out, errOut, err := run(ctx, dir, "api", "graphql", "-F", "owner={owner}", "-F", "name={repo}",
		"-F", "number="+strconv.Itoa(number), "-f", "query="+threadsQuery)
	if err != nil {
		return nil, classify(err, errOut)
	}
	var r struct {
		graphqlErrors
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							ID           string `json:"id"`
							IsResolved   bool   `json:"isResolved"`
							IsOutdated   bool   `json:"isOutdated"`
							Path         string `json:"path"`
							Line         *int   `json:"line"`
							OriginalLine *int   `json:"originalLine"`
							DiffSide     string `json:"diffSide"`
							Comments     struct {
								Nodes []struct {
									DatabaseID int64 `json:"databaseId"`
									Author     *struct {
										Login string `json:"login"`
									} `json:"author"`
									Body      string `json:"body"`
									CreatedAt string `json:"createdAt"`
									DiffHunk  string `json:"diffHunk"`
									URL       string `json:"url"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return nil, fmt.Errorf("gh api graphql: %w", err)
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	var ts []Thread
	for _, n := range r.Data.Repository.PullRequest.ReviewThreads.Nodes {
		t := Thread{ID: n.ID, Path: n.Path, Side: n.DiffSide, Resolved: n.IsResolved, Outdated: n.IsOutdated}
		switch {
		case n.Line != nil:
			t.Line = *n.Line
		case n.OriginalLine != nil:
			t.Line = *n.OriginalLine
		}
		for i, c := range n.Comments.Nodes {
			who := "ghost" // a deleted account
			if c.Author != nil && c.Author.Login != "" {
				who = c.Author.Login
			}
			if i == 0 {
				t.Hunk, t.URL = c.DiffHunk, c.URL
			}
			t.Comments = append(t.Comments, ThreadComment{ID: c.DatabaseID, Author: who, Body: c.Body, When: c.CreatedAt, URL: c.URL})
		}
		ts = append(ts, t)
	}
	return ts, nil
}

// Reply answers a review thread: commentID is the thread's first comment.
func Reply(ctx context.Context, run Runner, dir string, number int, commentID int64, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return ErrNeedsText
	}
	if commentID <= 0 || number <= 0 {
		return errors.New("no comment to answer")
	}
	path := fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments/%d/replies", number, commentID)
	_, errOut, err := run(ctx, dir, "api", "--method", "POST", path, "-f", "body="+body)
	return classify(err, errOut)
}

// SetResolved resolves a review thread (resolved true) or opens it again.
func SetResolved(ctx context.Context, run Runner, dir, threadID string, resolved bool) error {
	if threadID == "" || strings.HasPrefix(threadID, "-") {
		return errors.New("no review thread")
	}
	op := "unresolveReviewThread"
	if resolved {
		op = "resolveReviewThread"
	}
	q := "mutation($id:ID!){" + op + "(input:{threadId:$id}){thread{isResolved}}}"
	out, errOut, err := run(ctx, dir, "api", "graphql", "-f", "query="+q, "-f", "id="+threadID)
	if err != nil {
		return classify(err, errOut)
	}
	var g graphqlErrors
	if json.Unmarshal([]byte(out), &g) == nil {
		return g.err()
	}
	return nil
}
