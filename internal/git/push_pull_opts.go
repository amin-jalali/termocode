package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Push and pull options (GITX-12): what a push sends, what a pull
// brings, push to any remote branch, pull with rebase or from any
// branch, fetch all remotes.

// ErrNoUpstream: the current branch tracks no remote branch.
var ErrNoUpstream = errors.New("the current branch has no upstream branch")

// Upstream is the remote branch HEAD tracks ("origin/main").
func Upstream(dir string) (string, error) {
	out, err := runOut(dir, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", ErrNoUpstream
	}
	return strings.TrimSpace(out), nil
}

// outgoingRange is the log arguments of the commits on HEAD that base
// lacks; base "" is the upstream, or every remote when there is none.
func outgoingRange(dir, base string) ([]string, error) {
	if base == "" {
		if up, err := Upstream(dir); err == nil {
			base = up
		} else {
			return []string{"HEAD", "--not", "--remotes"}, nil
		}
	}
	if strings.HasPrefix(base, "-") {
		return nil, fmt.Errorf("%q is not a branch", base)
	}
	if _, err := ResolveCommit(dir, base); err != nil {
		return []string{"HEAD", "--not", "--remotes"}, nil // a new remote branch
	}
	return []string{"HEAD", "--not", base}, nil
}

func logRange(dir string, rng []string, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	args := append([]string{"log", "--pretty=format:%h%x1f%s%x1f%an%x1f%at%x1f%d%x1e", fmt.Sprintf("-n%d", limit)}, rng...)
	out, err := runCombined(dir, "git", append(args, "--")...)
	if err != nil {
		if !hasCommits(dir) {
			return nil, nil
		}
		return nil, err
	}
	return parseLogEntries(out), nil
}

// Outgoing lists the commits a push to base sends (base "": the
// upstream, or the commits no remote has yet).
func Outgoing(dir, base string, limit int) ([]LogEntry, error) {
	rng, err := outgoingRange(dir, base)
	if err != nil {
		return nil, err
	}
	return logRange(dir, rng, limit)
}

// OutgoingFiles counts the files the outgoing commits change.
func OutgoingFiles(dir, base string) (int, error) {
	rng, err := outgoingRange(dir, base)
	if err != nil {
		return 0, err
	}
	out, err := runCombined(dir, "git", append(append([]string{"log", "--format=", "--name-only"}, rng...), "--")...)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			seen[l] = true
		}
	}
	return len(seen), nil
}

// Incoming lists the commits of the upstream that HEAD lacks, as of the
// last fetch.
func Incoming(dir string, limit int) ([]LogEntry, error) {
	up, err := Upstream(dir)
	if err != nil {
		return nil, err
	}
	return logRange(dir, []string{up, "--not", "HEAD"}, limit)
}

// checkName refuses an empty remote or branch name, or one git would
// read as an option or a revision range.
func checkName(what, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.Contains(name, "..") || strings.ContainsAny(name, " \t\n~^:?*[\\") {
		return fmt.Errorf("%s %s is not valid", what, strconv.Quote(name))
	}
	return nil
}

// PushTo pushes HEAD to branch on remote (made when it is new); the
// upstream is not changed.
func PushTo(dir, remote, branch string) error {
	if err := checkName("remote", remote); err != nil {
		return err
	}
	if err := checkName("branch", branch); err != nil {
		return err
	}
	out, err := remoteCmd(dir, "push", remote, "HEAD:refs/heads/"+branch).CombinedOutput()
	if err != nil {
		return cmdError(err, out)
	}
	return nil
}

// PullFrom pulls with a rebase or a merge; remote and branch "" pull the
// upstream.
func PullFrom(dir, remote, branch string, rebase bool) error {
	args := []string{"pull", "--no-edit"}
	if rebase {
		args = append(args, "--rebase")
	} else {
		args = append(args, "--no-rebase")
	}
	if remote != "" || branch != "" {
		if err := checkName("remote", remote); err != nil {
			return err
		}
		if err := checkName("branch", branch); err != nil {
			return err
		}
		args = append(args, remote, branch)
	}
	out, err := remoteCmd(dir, args...).CombinedOutput()
	if err != nil {
		return cmdError(err, out)
	}
	return nil
}

// FetchAll fetches every remote and drops remote branches that are gone.
func FetchAll(dir string) error {
	out, err := remoteCmd(dir, "fetch", "--all", "--prune").CombinedOutput()
	if err != nil {
		return cmdError(err, out)
	}
	return nil
}

// RemoteNames lists the remotes, sorted.
func RemoteNames(dir string) ([]string, error) {
	out, err := runCombined(dir, "git", "remote")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return names, nil
}
