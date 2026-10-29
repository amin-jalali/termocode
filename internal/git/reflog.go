package git

import (
	"strconv"
	"strings"
)

// Undo with the reflog (G-UND): git writes a line to HEAD's reflog for
// every action that moves HEAD (commit, amend, reset, checkout, merge,
// rebase, cherry-pick, pull …). termocode reads it to say what the last
// action was in plain words and to move HEAD back to before it.

// ReflogKind is the kind of git action a reflog line records.
type ReflogKind string

const (
	ReflogCommit     ReflogKind = "commit"
	ReflogAmend      ReflogKind = "amend"
	ReflogInitial    ReflogKind = "initial" // the first commit of the repo
	ReflogMerge      ReflogKind = "merge"
	ReflogCheckout   ReflogKind = "checkout"
	ReflogReset      ReflogKind = "reset"
	ReflogRebase     ReflogKind = "rebase"
	ReflogCherryPick ReflogKind = "cherry-pick"
	ReflogRevert     ReflogKind = "revert"
	ReflogPull       ReflogKind = "pull"
	ReflogOther      ReflogKind = "other"
)

// ReflogEntry is one git action from HEAD's reflog.
type ReflogEntry struct {
	Hash     string // full hash of HEAD after the action
	Prev     string // full hash of HEAD before it ("" when unknown: the oldest line)
	Kind     ReflogKind
	Message  string // git's reflog line ("commit: fix x")
	Subject  string // the subject of the commit at Hash
	From, To string // checkout: the branch (or commit) left and the one entered
	When     int64  // unix time of the action
}

// Short is the entry's short hash (7 letters).
func (e ReflogEntry) Short() string { return shortHash(e.Hash) }

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// Ago is how long ago the action ran ("2 min ago").
func (e ReflogEntry) Ago() string { return relativeTimeFromEpoch(e.When) }

// Summary says the action in plain words: "commit 'fix x'",
// "checkout main → feat/x", "reset to HEAD~1".
func (e ReflogEntry) Summary() string {
	_, rest, _ := strings.Cut(e.Message, ": ")
	q := "'" + e.Subject + "'"
	switch e.Kind {
	case ReflogCommit:
		return "commit " + q
	case ReflogAmend:
		return "amend " + q
	case ReflogInitial:
		return "first commit " + q
	case ReflogCheckout:
		return "checkout " + e.From + " → " + e.To
	case ReflogReset:
		return "reset to " + strings.TrimPrefix(rest, "moving to ")
	case ReflogMerge:
		action, _, _ := strings.Cut(e.Message, ": ")
		if action == "commit (merge)" {
			return "merge commit " + q
		}
		return action
	case ReflogCherryPick:
		return "cherry-pick " + q
	case ReflogRevert:
		return "revert " + q
	case ReflogPull:
		if step, _ := rebaseStep(e.Message); step {
			return "pull (rebase)"
		}
		return "pull"
	case ReflogRebase:
		action, _, _ := strings.Cut(e.Message, ": ")
		switch {
		case strings.Contains(action, "(finish)"):
			return "rebase finished"
		case strings.Contains(action, "(abort)"):
			return "rebase aborted"
		}
	}
	return e.Message
}

// reflogFormat asks for: full hash, selector with the unix time
// (HEAD@{1700000000} under --date=unix), reflog line, commit subject.
const reflogFormat = "--format=%H%x1f%gd%x1f%gs%x1f%s"

// Reflog lists the last limit actions of HEAD's reflog, newest first. A
// repo without commits has none.
func Reflog(dir string, limit int) ([]ReflogEntry, error) {
	if !hasCommits(dir) {
		return nil, nil
	}
	// A rebase folds many lines into one entry: read more lines until
	// limit entries (and the one before, for Prev) are there or the
	// reflog ends.
	for n := limit*4 + 16; ; n *= 4 {
		out, err := runOut(dir, "git", "log", "-g", "--date=unix", reflogFormat,
			"-n", strconv.Itoa(n), "HEAD", "--")
		if err != nil {
			return nil, err
		}
		lines := strings.Count(out, "\n")
		entries := parseReflog(out)
		if len(entries) > limit || lines < n {
			if len(entries) > limit {
				entries = entries[:limit]
			}
			return entries, nil
		}
	}
}

// parseReflog reads Reflog's records; each entry's Prev is the next
// (older) record's hash.
func parseReflog(out string) []ReflogEntry {
	var entries []ReflogEntry
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) < 4 {
			continue
		}
		e := ReflogEntry{Hash: f[0], Message: f[2], Subject: f[3]}
		if i := strings.LastIndex(f[1], "@{"); i >= 0 {
			e.When, _ = strconv.ParseInt(strings.TrimSuffix(f[1][i+2:], "}"), 10, 64)
		}
		e.Kind = reflogKind(e.Message)
		if e.Kind == ReflogCheckout {
			_, rest, _ := strings.Cut(e.Message, "moving from ")
			e.From, e.To, _ = strings.Cut(rest, " to ")
		}
		entries = append(entries, e)
	}
	for i := 0; i+1 < len(entries); i++ {
		entries[i].Prev = entries[i+1].Hash
	}
	return foldRebases(entries)
}

// rebaseStep reports whether a reflog line is a step of a rebase (git's
// own, or the one of `git pull --rebase`), and whether it is its start.
func rebaseStep(msg string) (step, start bool) {
	action, _, _ := strings.Cut(msg, ": ")
	if !strings.HasPrefix(action, "rebase") && !(strings.HasPrefix(action, "pull") && strings.Contains(action, "(")) {
		return false, false
	}
	return true, strings.Contains(action, "(start)")
}

// foldRebases makes the lines of one rebase (finish or abort back to its
// start) one entry: the newest line's hash, the HEAD before the start as
// Prev. A rebase whose start is cut off (an incomplete read) is dropped
// with everything after it, so a Prev is never wrong.
func foldRebases(entries []ReflogEntry) []ReflogEntry {
	var out []ReflogEntry
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if step, start := rebaseStep(e.Message); !step || start {
			out = append(out, e)
			continue
		}
		j := i
		for ; j < len(entries); j++ {
			if step, start := rebaseStep(entries[j].Message); !step || start {
				break
			}
		}
		if j == len(entries) {
			break // the start is not in what was read
		}
		if step, _ := rebaseStep(entries[j].Message); step {
			e.Prev = entries[j].Prev
			i = j
		} else {
			e.Prev = entries[j].Hash // a start-less run (an old git): stop at the first other line
			i = j - 1
		}
		out = append(out, e)
	}
	return out
}

// reflogKind is the kind of action a reflog line records, from its
// first part ("commit (amend)", "checkout", "merge feat", …).
func reflogKind(msg string) ReflogKind {
	action, _, _ := strings.Cut(msg, ": ")
	switch {
	case action == "commit (amend)":
		return ReflogAmend
	case action == "commit (initial)":
		return ReflogInitial
	case action == "commit (merge)", action == "merge" || strings.HasPrefix(action, "merge "):
		return ReflogMerge
	case action == "commit" || strings.HasPrefix(action, "commit "):
		return ReflogCommit
	case action == "checkout":
		return ReflogCheckout
	case action == "reset":
		return ReflogReset
	case strings.HasPrefix(action, "rebase"):
		return ReflogRebase
	case strings.HasPrefix(action, "cherry-pick"):
		return ReflogCherryPick
	case strings.HasPrefix(action, "revert"):
		return ReflogRevert
	case strings.HasPrefix(action, "pull"):
		return ReflogPull
	}
	return ReflogOther
}
