package git

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/clock"
)

// Reset is the package Reset where the repository lives.
func (r StashRepo) Reset(mode, rev string) error {
	if r.x == nil {
		return Reset(r.dir, mode, rev)
	}
	switch mode {
	case ResetSoft, ResetMixed, ResetKeep:
	default:
		return fmt.Errorf("unknown reset mode %q", mode)
	}
	_, err := r.via("reset", "-q", "--"+mode, rev, "--")
	return err
}

// ResetHardSafe is the package ResetHardSafe where the repository
// lives: the backup ref and the stash are made there first.
func (r StashRepo) ResetHardSafe(rev string) (ResetBackup, error) {
	if r.x == nil {
		return ResetHardSafe(r.dir, rev)
	}
	old := r.Hash("HEAD")
	if len(old) < 7 {
		return ResetBackup{}, fmt.Errorf("no commit to reset")
	}
	b := ResetBackup{Ref: "refs/termocode/backup/" + clock.Now().UTC().Format("20060102-150405.000"), Old: old[:7]}
	b.Ref = strings.ReplaceAll(b.Ref, ".", "-")
	if _, err := r.via("update-ref", b.Ref, old); err != nil {
		return ResetBackup{}, err
	}
	st, err := r.via("status", "--porcelain")
	if err != nil {
		return b, fmt.Errorf("could not read the changes, nothing was reset: %w", err)
	}
	if strings.TrimSpace(st) != "" {
		if err := r.Push(StashOptions{Message: "termocode: before reset --hard to " + rev, Untracked: true}); err != nil {
			return b, fmt.Errorf("could not save the changes first, nothing was reset: %w", err)
		}
		b.Stashed = true
	}
	_, err = r.via("reset", "-q", "--hard", rev, "--")
	return b, err
}

// Uncommit is the package Uncommit where the repository lives.
func (r StashRepo) Uncommit() error {
	if r.x == nil {
		return Uncommit(r.dir)
	}
	if r.Hash("HEAD~1") == "" {
		return fmt.Errorf("the first commit has no commit before it")
	}
	return r.Reset(ResetSoft, "HEAD~1")
}

// CountCommits counts the commits of a range ("a..b"); -1 when unknown.
func (r StashRepo) CountCommits(rng string) int {
	run := localGit(r.dir)
	if r.x != nil {
		run = r.via
	}
	out, err := run("rev-list", "--count", rng)
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return -1
	}
	return n
}

// Subject is the subject of the last commit ("" when unknown).
func (r StashRepo) Subject() string {
	run := localGit(r.dir)
	if r.x != nil {
		run = r.via
	}
	out, err := run("log", "-1", "--format=%s")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
