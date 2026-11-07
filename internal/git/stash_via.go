package git

import (
	"fmt"
	"strings"
)

// StashRepo runs the stash commands of one repository where it lives:
// on this machine (no Runner) or through a Runner, the Host Exec of a
// container or remote workspace (ADR 0018). The local path keeps the
// local functions (StashList, StashPush …) as they were.
type StashRepo struct {
	dir string
	x   Runner // nil: this machine
}

// StashAt is the stash commands of the repository at dir, run by x
// (nil: on this machine).
func StashAt(dir string, x Runner) StashRepo { return StashRepo{dir: dir, x: x} }

func (r StashRepo) via(args ...string) (string, error) { return viaGit(r.x, r.dir)(args...) }

// List is StashList.
func (r StashRepo) List() ([]StashEntry, error) {
	if r.x == nil {
		return StashList(r.dir)
	}
	out, err := r.via("stash", "list", "--pretty=%gd|%s")
	if err != nil {
		return nil, err
	}
	return parseStashList(out), nil
}

func parseStashList(out string) []StashEntry {
	var entries []StashEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		ref, msg, ok := strings.Cut(line, "|")
		if line == "" || !ok {
			continue
		}
		entries = append(entries, StashEntry{Ref: ref, Message: msg})
	}
	return entries
}

// Hash is StashHash.
func (r StashRepo) Hash(ref string) string {
	if r.x == nil {
		return StashHash(r.dir, ref)
	}
	out, err := r.via("rev-parse", "--verify", "-q", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Push is StashPush.
func (r StashRepo) Push(o StashOptions) error {
	if r.x == nil {
		return StashPush(r.dir, o)
	}
	before := r.Hash("stash@{0}")
	if _, err := r.via(stashPushArgs(o)...); err != nil {
		return err
	}
	if r.Hash("stash@{0}") == before {
		return fmt.Errorf("no local changes to save")
	}
	return nil
}

// Apply is StashApply.
func (r StashRepo) Apply(ref string) error {
	if r.x == nil {
		return StashApply(r.dir, ref)
	}
	_, err := r.via("stash", "apply", ref)
	return err
}

// Drop is StashDrop.
func (r StashRepo) Drop(ref string) error {
	if r.x == nil {
		return StashDrop(r.dir, ref)
	}
	_, err := r.via("stash", "drop", ref)
	return err
}

// Pop applies ref, then drops it. ref "" is `git stash pop` (the latest).
func (r StashRepo) Pop(ref string) error {
	if ref == "" {
		if r.x == nil {
			return StashPop(r.dir)
		}
		_, err := r.via("stash", "pop")
		return err
	}
	if err := r.Apply(ref); err != nil {
		return err
	}
	return r.Drop(ref)
}

// Diff is StashDiff.
func (r StashRepo) Diff(ref string) (string, error) {
	if r.x == nil {
		return StashDiff(r.dir, ref)
	}
	out, err := r.via("stash", "show", "--no-color", "-p", "--include-untracked", ref)
	if err != nil {
		return r.via("stash", "show", "--no-color", "-p", ref)
	}
	return out, nil
}

// Branch is StashBranch.
func (r StashRepo) Branch(name, ref string) error {
	if r.x == nil {
		return StashBranch(r.dir, name, ref)
	}
	_, err := r.via("stash", "branch", name, ref)
	return err
}
