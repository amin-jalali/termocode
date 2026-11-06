package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// BranchInfo is one local branch for the branch picker (GITX-32).
type BranchInfo struct {
	Name     string
	Upstream string // "origin/main", "" without one
	Ahead    int    // commits the branch has that its upstream lacks
	Behind   int    // commits the upstream has that the branch lacks
	Gone     bool   // the upstream was deleted
	Current  bool
}

// BranchList lists the local branches with their upstream and how far
// ahead / behind it each one is (one git call).
func BranchList(dir string) ([]BranchInfo, error) {
	out, err := runOut(dir, "git", "for-each-ref",
		"--format=%(refname:short)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(HEAD)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var list []BranchInfo
	for _, l := range strings.Split(out, "\n") {
		f := strings.Split(l, "\x00")
		if len(f) < 4 || f[0] == "" {
			continue
		}
		b := BranchInfo{Name: f[0], Upstream: f[1], Current: strings.TrimSpace(f[3]) == "*"}
		for _, part := range strings.Split(f[2], ",") {
			k, v, _ := strings.Cut(strings.TrimSpace(part), " ")
			n, _ := strconv.Atoi(v)
			switch k {
			case "ahead":
				b.Ahead = n
			case "behind":
				b.Behind = n
			case "gone":
				b.Gone = true
			}
		}
		list = append(list, b)
	}
	return list, nil
}

// RecentBranches is up to n local branches, most recently checked out
// first, read from the HEAD reflog; deleted branches are left out.
func RecentBranches(dir string, n int) []string {
	out, err := runOut(dir, "git", "reflog", "show", "--format=%gs", "-n", "1000", "HEAD", "--")
	if err != nil || n <= 0 {
		return nil
	}
	local := map[string]bool{}
	if names, err := Branches(dir); err == nil {
		for _, b := range names {
			local[b] = true
		}
	}
	var recent []string
	seen := map[string]bool{}
	add := func(b string) {
		if b != "" && local[b] && !seen[b] && len(recent) < n {
			seen[b] = true
			recent = append(recent, b)
		}
	}
	if cur, err := runOut(dir, "git", "branch", "--show-current"); err == nil {
		add(strings.TrimSpace(cur))
	}
	for _, l := range strings.Split(out, "\n") {
		// "checkout: moving from <old> to <new>"
		rest, ok := strings.CutPrefix(strings.TrimSpace(l), "checkout: moving from ")
		if !ok {
			continue
		}
		from, to, ok := strings.Cut(rest, " to ")
		if !ok {
			continue
		}
		add(to)
		add(from)
	}
	return recent
}

// CheckoutTracking checks out the remote branch remoteRef ("origin/x") as
// a new local branch x that tracks it.
func CheckoutTracking(dir, remoteRef string) error {
	if strings.HasPrefix(remoteRef, "-") || !strings.Contains(remoteRef, "/") {
		return fmt.Errorf("not a remote branch: %s", remoteRef)
	}
	return runGit(dir, "checkout", "--track", remoteRef)
}

// IsBlockedByChanges reports whether a checkout failed because local
// changes (or untracked files) would be overwritten.
func IsBlockedByChanges(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	var ce *CmdError
	if errors.As(err, &ce) {
		text = ce.Output
	}
	return strings.Contains(text, "would be overwritten by checkout") ||
		strings.Contains(text, "commit your changes or stash them")
}

// StashAndSwitch saves every local change (untracked files too) in the
// stash, then switches to branch. If the switch fails, the stash is
// applied back so nothing is lost.
func StashAndSwitch(dir, branch string) error {
	before := stashTop(dir)
	if err := runGit(dir, "stash", "push", "--include-untracked", "-m", "termocode: before switching to "+branch); err != nil {
		return err
	}
	stashed := stashTop(dir) != before // "No local changes to save" adds none
	if err := Checkout(dir, branch); err != nil {
		if !stashed {
			return err
		}
		if perr := StashPop(dir); perr != nil {
			return fmt.Errorf("%v; your changes stay in the stash: %v", err, perr)
		}
		return err
	}
	return nil
}
