package git

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Restack (CMP-56): an agent lane's branch is a pile of work in progress
// commits ("termocode agent <id>" snapshots, the agent's own "wip"s) on
// the commit the lane started from. Restack replays that work on top of
// the user's current commit (or another one) and, with Squash, makes it
// one clean commit with a real message, ready to merge, push or open a
// pull request from. It runs only in the lane's own worktree: the user's
// branch, index and files never change. A clash stops before anything
// changes (the rebase is aborted) and names the files. The old commits
// stay in the lane branch's reflog.

// AgentRestackOptions tune RestackAgentWorktree.
type AgentRestackOptions struct {
	Onto    string // the commit to put the work on; "" is the user's HEAD
	Squash  bool   // make the work one commit with Message
	Message string // the clean commit's message (Squash only; required)
}

// AgentRestack is what a restack did.
type AgentRestack struct {
	Onto      string   // full hash the work now sits on
	Before    int      // the lane's commits before
	After     int      // the lane's commits after (0: the work was in Onto already)
	Conflicts []string // files that clash with Onto; nothing changed
}

// ErrRestackNothing: the lane has no commit after its start.
var ErrRestackNothing = errors.New("the lane has no work to restack")

// RestackAgentWorktree replays the work of agent worktree id on top of
// opt.Onto (its uncommitted files are committed first, as for a merge),
// then moves the lane's base there, so its diff and a later merge are
// only the agent's work.
func RestackAgentWorktree(dir, id string, opt AgentRestackOptions) (AgentRestack, error) {
	msg := strings.TrimSpace(opt.Message)
	if opt.Squash && msg == "" {
		return AgentRestack{}, errors.New("a squashed commit needs a message")
	}
	top, w, base, tip, err := snapshotAgent(dir, id)
	if err != nil {
		return AgentRestack{}, err
	}
	if _, err := os.Stat(w.Path); err != nil {
		return AgentRestack{}, fmt.Errorf("agent worktree %s is gone; its work is on branch %s", id, w.Branch)
	}
	onto := opt.Onto
	if onto == "" {
		onto = "HEAD"
	}
	if strings.HasPrefix(onto, "-") {
		return AgentRestack{}, fmt.Errorf("%q is not a commit", onto)
	}
	o, err := gitOut(top, "rev-parse", "--verify", "-q", "--end-of-options", onto+"^{commit}")
	if err != nil {
		return AgentRestack{}, fmt.Errorf("%q is not a commit of this repository", onto)
	}
	res := AgentRestack{Onto: strings.TrimSpace(o)}
	if res.Before, err = revCount(top, base+".."+tip); err != nil {
		return AgentRestack{}, err
	}
	if res.Before == 0 {
		return AgentRestack{}, ErrRestackNothing
	}
	// commit.gpgsign=false: no pinentry under the TUI; termocode's identity
	// only when the repository has none (as the snapshot commit).
	cfg := []string{"-c", "commit.gpgsign=false", "-c", "rebase.autoSquash=false", "-c", "rebase.updateRefs=false"}
	if e, _ := gitOut(w.Path, "config", "user.email"); strings.TrimSpace(e) == "" {
		cfg = append(cfg, "-c", "user.name=termocode", "-c", "user.email=termocode@localhost")
	}
	run := func(args ...string) (string, error) {
		return gitOut(w.Path, append(append([]string{}, cfg...), args...)...)
	}
	if _, err := run("rebase", "-q", "--onto", res.Onto, base, w.Branch); err != nil {
		u, _ := gitOut(w.Path, "diff", "--name-only", "--diff-filter=U")
		res.Conflicts = strings.Fields(u)
		if _, aerr := gitOut(w.Path, "rebase", "--abort"); aerr != nil {
			return res, fmt.Errorf("restack stopped (%v) and could not be undone: %w; run git rebase --abort in %s", err, aerr, w.Path)
		}
		if len(res.Conflicts) == 0 {
			return res, fmt.Errorf("restack stopped, nothing changed: %w", err)
		}
		return res, nil
	}
	if opt.Squash {
		if _, err := run("reset", "-q", "--soft", res.Onto); err != nil {
			return res, err
		}
		if _, err := gitOut(w.Path, "diff", "--cached", "--quiet"); err != nil {
			if _, err := run("commit", "-q", "--no-verify", "-m", msg); err != nil {
				return res, err
			}
		}
	}
	if res.After, err = revCount(top, res.Onto+".."+"refs/heads/"+w.Branch); err != nil {
		return res, err
	}
	if _, err := gitRetry(top, "update-ref", agentBaseRef+id, res.Onto); err != nil {
		return res, err
	}
	return res, nil
}

// revCount is how many commits rng (a..b) has.
func revCount(dir, rng string) (int, error) {
	out, err := gitOut(dir, "rev-list", "--count", rng)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}
