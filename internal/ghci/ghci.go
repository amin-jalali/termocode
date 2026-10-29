// Package ghci reads the CI state of a branch from the GitHub CLI (gh):
// the workflow runs of its newest pushed commit, its pull request with
// the checks and review state, and the log of a failed run, trimmed to
// the failing part for the AI chat (G-CI).
//
// gh is always run with a fixed argument list (no shell), in the repo
// folder, with a timeout and the user's environment; it uses its own
// login. termocode never reads or prints a token. Tests pass a fake
// Runner and never start the real gh.
package ghci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Runner runs gh with args in dir and returns its stdout and stderr.
type Runner func(ctx context.Context, dir string, args ...string) (stdout, stderr string, err error)

// ErrNoGH: gh is not installed (not on PATH).
var ErrNoGH = errors.New("the gh command is not installed")

// ErrNotLoggedIn: gh is installed but not signed in.
var ErrNotLoggedIn = errors.New("gh is not signed in")

// ErrNoGitHub: the repository has no GitHub remote.
var ErrNoGitHub = errors.New("no GitHub remote for this repository")

// Quiet reports an error that means "CI is not available here" (no gh,
// no login, no GitHub remote): the UI shows one quiet hint, no error.
func Quiet(err error) bool {
	return errors.Is(err, ErrNoGH) || errors.Is(err, ErrNotLoggedIn) || errors.Is(err, ErrNoGitHub)
}

// DefaultRunner runs the gh found on PATH, at most timeout long.
func DefaultRunner(timeout time.Duration) Runner {
	return func(ctx context.Context, dir string, args ...string) (string, string, error) {
		path, err := exec.LookPath("gh")
		if err != nil {
			return "", "", ErrNoGH
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := spawn.CommandContext(ctx, path, args...)
		cmd.Dir = dir
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "GH_NO_UPDATE_NOTIFIER=1", "GH_PAGER=", "PAGER=cat")
		cmd.WaitDelay = 200 * time.Millisecond
		err = cmd.Run()
		if ctx.Err() == context.DeadlineExceeded {
			return "", "", fmt.Errorf("gh did not answer in %v", timeout)
		}
		return out.String(), errb.String(), err
	}
}

// classify turns a failed gh run into one of the errors above, or a
// short error with gh's first stderr line.
func classify(err error, stderr string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNoGH) {
		return ErrNoGH
	}
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "not logged in") || strings.Contains(s, "not logged into") || strings.Contains(s, "gh auth login"):
		return ErrNotLoggedIn
	case strings.Contains(s, "none of the git remotes") || strings.Contains(s, "no git remotes") ||
		strings.Contains(s, "not a git repository") || strings.Contains(s, "unable to determine base repository"):
		return ErrNoGitHub
	}
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(stderr), "\n", 2)[0])
	if line == "" {
		return fmt.Errorf("gh: %w", err)
	}
	return errors.New("gh: " + line)
}

// State is the CI state of a branch.
type State int

const (
	StateNone    State = iota // no run for the branch
	StatePending              // a run is queued or in progress
	StatePass                 // all runs passed
	StateFail                 // at least one run failed
)

// Run is one workflow run.
type Run struct {
	ID         int64  `json:"databaseId"`
	Workflow   string `json:"workflowName"`
	Title      string `json:"displayTitle"`
	Status     string `json:"status"`     // queued, in_progress, completed, …
	Conclusion string `json:"conclusion"` // success, failure, cancelled, …
	HeadSHA    string `json:"headSha"`
	URL        string `json:"url"`
}

// Pending reports a run that has not finished.
func (r Run) Pending() bool { return r.Status != "" && r.Status != "completed" }

// Failed reports a finished run that failed.
func (r Run) Failed() bool { return !r.Pending() && failedConclusion(r.Conclusion) }

func failedConclusion(c string) bool {
	switch strings.ToLower(c) {
	case "failure", "timed_out", "startup_failure", "action_required", "error":
		return true
	}
	return false
}

// Check is one check of a pull request (a job or a commit status).
type Check struct {
	Name  string
	State State // StatePending, StatePass or StateFail; StateNone = skipped / neutral
	URL   string
}

// PR is the pull request of a branch.
type PR struct {
	Number int
	Title  string
	State  string // OPEN, CLOSED, MERGED
	Draft  bool
	URL    string
	Review string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED or ""
	Checks []Check
}

// Snapshot is the CI state of one branch.
type Snapshot struct {
	Branch  string
	HeadSHA string // the commit the runs are for
	Runs    []Run  // the newest run of each workflow for HeadSHA
	PR      *PR    // nil: no pull request
}

// State sums up the runs: failed wins over pending, pending over pass.
func (s Snapshot) State() State {
	if len(s.Runs) == 0 {
		return StateNone
	}
	st := StatePass
	for _, r := range s.Runs {
		if r.Failed() {
			return StateFail
		}
		if r.Pending() {
			st = StatePending
		}
	}
	return st
}

// FailedRuns lists the failed runs.
func (s Snapshot) FailedRuns() []Run {
	var out []Run
	for _, r := range s.Runs {
		if r.Failed() {
			out = append(out, r)
		}
	}
	return out
}

// Fetch reads the CI state of branch: `gh run list` and `gh pr view`.
// A branch with no pull request is not an error.
func Fetch(ctx context.Context, run Runner, dir, branch string) (Snapshot, error) {
	snap := Snapshot{Branch: branch}
	if branch == "" || strings.HasPrefix(branch, "(") || strings.HasPrefix(branch, "-") {
		return snap, nil // detached HEAD or no branch: nothing to ask
	}
	out, errOut, err := run(ctx, dir, "run", "list", "--branch", branch, "--limit", "20",
		"--json", "databaseId,workflowName,displayTitle,status,conclusion,headSha,url")
	if err != nil {
		return snap, classify(err, errOut)
	}
	var runs []Run
	if err := json.Unmarshal([]byte(out), &runs); err != nil {
		return snap, fmt.Errorf("gh run list: %w", err)
	}
	snap.HeadSHA, snap.Runs = newestRuns(runs)
	pr, err := fetchPR(ctx, run, dir, branch)
	if err != nil {
		return snap, err
	}
	snap.PR = pr
	return snap, nil
}

// newestRuns keeps the runs of the newest commit (gh lists newest
// first), one per workflow (a re-run replaces the older one).
func newestRuns(runs []Run) (string, []Run) {
	if len(runs) == 0 {
		return "", nil
	}
	sha := runs[0].HeadSHA
	seen := map[string]bool{}
	var out []Run
	for _, r := range runs {
		if r.HeadSHA != sha || seen[r.Workflow] {
			continue
		}
		seen[r.Workflow] = true
		out = append(out, r)
	}
	return sha, out
}

type prJSON struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	State          string `json:"state"`
	IsDraft        bool   `json:"isDraft"`
	URL            string `json:"url"`
	ReviewDecision string `json:"reviewDecision"`
	Rollup         []struct {
		Typename   string `json:"__typename"`
		Name       string `json:"name"`
		Context    string `json:"context"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		State      string `json:"state"`
		DetailsURL string `json:"detailsUrl"`
		TargetURL  string `json:"targetUrl"`
	} `json:"statusCheckRollup"`
}

func fetchPR(ctx context.Context, run Runner, dir, branch string) (*PR, error) {
	out, errOut, err := run(ctx, dir, "pr", "view", branch,
		"--json", "number,title,state,isDraft,url,reviewDecision,statusCheckRollup")
	if err != nil {
		if strings.Contains(strings.ToLower(errOut), "no pull requests found") {
			return nil, nil
		}
		return nil, classify(err, errOut)
	}
	var p prJSON
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return nil, fmt.Errorf("gh pr view: %w", err)
	}
	pr := &PR{Number: p.Number, Title: p.Title, State: p.State, Draft: p.IsDraft, URL: p.URL, Review: p.ReviewDecision}
	for _, c := range p.Rollup {
		ch := Check{Name: c.Name, URL: c.DetailsURL}
		if c.Typename == "StatusContext" {
			ch.Name, ch.URL = c.Context, c.TargetURL
			switch strings.ToUpper(c.State) {
			case "SUCCESS":
				ch.State = StatePass
			case "FAILURE", "ERROR":
				ch.State = StateFail
			default:
				ch.State = StatePending
			}
		} else {
			switch {
			case c.Status != "" && !strings.EqualFold(c.Status, "COMPLETED"):
				ch.State = StatePending
			case failedConclusion(c.Conclusion):
				ch.State = StateFail
			case strings.EqualFold(c.Conclusion, "SUCCESS"):
				ch.State = StatePass
			}
		}
		pr.Checks = append(pr.Checks, ch)
	}
	return pr, nil
}

// SlowPoll is the poll interval while nothing runs; FastPoll while a
// run is pending or right after a push.
const (
	SlowPoll  = 5 * time.Minute
	FastPoll  = 20 * time.Second
	PushGrace = 3 * time.Minute
)

// NextPoll is how long to wait before the next Fetch: fast while a run
// is pending or within PushGrace after a push (the new run may not be
// listed yet), slow otherwise and after an error (offline, no gh).
func NextPoll(s Snapshot, err error, sincePush time.Duration) time.Duration {
	if err != nil {
		return SlowPoll
	}
	if s.State() == StatePending || (sincePush >= 0 && sincePush < PushGrace) {
		return FastPoll
	}
	return SlowPoll
}
