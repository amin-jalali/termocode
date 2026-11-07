package worktrees

import (
	"errors"
	"fmt"
	"strings"
)

// ── Task dependencies across agent worktrees (CMP-80) ───────────────────
//
// An agent (or the user) records that the work in one worktree waits for
// the work in another: Meta.BlockedBy holds the blockers' paths. A
// blocker is settled when its agent says done, its PR is merged, its
// issue is closed, or the worktree is gone or archived. Waves orders the
// graph so the WORKTREES view can draw "what blocks what".

// MaxBlockers caps the blockers of one worktree.
const MaxBlockers = 16

// Errors of AddBlocker.
var (
	ErrSelfBlock = errors.New("a worktree cannot wait for itself")
	ErrBlockLoop = errors.New("that would make a loop: the other worktree already waits for this one")
)

// AddBlocker records that the worktree at path waits for the one at by.
// Adding it again changes nothing; a self edge and a loop are refused.
func AddBlocker(metas map[string]Meta, path, by string) error {
	if path == by {
		return ErrSelfBlock
	}
	md := metas[path]
	for _, b := range md.BlockedBy {
		if b == by {
			return nil
		}
	}
	if len(md.BlockedBy) >= MaxBlockers {
		return fmt.Errorf("a worktree waits for at most %d others", MaxBlockers)
	}
	// A loop: path is reachable from by over BlockedBy.
	seen := map[string]bool{}
	stack := []string{by}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if p == path {
			return ErrBlockLoop
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		stack = append(stack, metas[p].BlockedBy...)
	}
	md.BlockedBy = append(append([]string(nil), md.BlockedBy...), by)
	metas[path] = md
	return nil
}

// RemoveBlocker drops by from the blockers of path; false when it was
// not there.
func RemoveBlocker(metas map[string]Meta, path, by string) bool {
	md, ok := metas[path]
	if !ok {
		return false
	}
	out := make([]string, 0, len(md.BlockedBy))
	for _, b := range md.BlockedBy {
		if b != by {
			out = append(out, b)
		}
	}
	if len(out) == len(md.BlockedBy) {
		return false
	}
	if len(out) == 0 {
		out = nil
	}
	md.BlockedBy = out
	metas[path] = md
	return true
}

// Settled reports whether the work in e no longer blocks others.
func Settled(e Entry) bool {
	switch {
	case e.Gone, e.Meta.State == StateArchived, e.Meta.Checkpoint == CheckDone:
		return true
	}
	st := e.Meta.Link.State
	return (e.Meta.PR != 0 && st == "merged") || (e.Meta.Issue != 0 && st == "closed")
}

// OpenBlockers are e's blockers in entries that are not settled. A
// blocker that is no longer in entries (forgotten) does not block.
func OpenBlockers(entries []Entry, e Entry) []Entry {
	if len(e.Meta.BlockedBy) == 0 {
		return nil
	}
	byPath := make(map[string]Entry, len(entries))
	for _, x := range entries {
		byPath[x.Path] = x
	}
	var out []Entry
	for _, p := range e.Meta.BlockedBy {
		if b, ok := byPath[p]; ok && !Settled(b) {
			out = append(out, b)
		}
	}
	return out
}

// Blockers are e's blockers in entries, settled or not.
func Blockers(entries []Entry, e Entry) []Entry {
	if len(e.Meta.BlockedBy) == 0 {
		return nil
	}
	byPath := make(map[string]Entry, len(entries))
	for _, x := range entries {
		byPath[x.Path] = x
	}
	var out []Entry
	for _, p := range e.Meta.BlockedBy {
		if b, ok := byPath[p]; ok {
			out = append(out, b)
		}
	}
	return out
}

// Titles are the entries' titles.
func Titles(es []Entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Title())
	}
	return out
}

// WaitsLine says in plain words what e waits for: "waits for a, b",
// "ready: its blockers are done", or "" when it has no blockers.
func WaitsLine(entries []Entry, e Entry) string {
	if len(Blockers(entries, e)) == 0 {
		return ""
	}
	if open := OpenBlockers(entries, e); len(open) > 0 {
		return "waits for " + strings.Join(Titles(open), ", ")
	}
	return "ready: its blockers are done"
}

// Waves orders the worktrees that are in the graph (they wait for one,
// or one waits for them): a worktree is one wave after its latest
// blocker. Worktrees keep the list's order inside a wave. Members of a
// loop (only a hand-edited file has one) go in one last wave.
func Waves(entries []Entry) [][]Entry {
	idx := make(map[string]int, len(entries))
	for i, e := range entries {
		idx[e.Path] = i
	}
	in := make([]bool, len(entries))
	has := false
	for i, e := range entries {
		for _, p := range e.Meta.BlockedBy {
			if j, ok := idx[p]; ok && j != i {
				in[i], in[j], has = true, true, true
			}
		}
	}
	if !has {
		return nil
	}
	const unknown, busy, looped = -1, -2, -3
	level := make([]int, len(entries))
	for i := range level {
		level[i] = unknown
	}
	var walk func(i int) int
	walk = func(i int) int {
		switch level[i] {
		case busy, looped:
			return -1
		case unknown:
		default:
			return level[i]
		}
		level[i] = busy
		lv := 0
		for _, p := range entries[i].Meta.BlockedBy {
			j, ok := idx[p]
			if !ok || j == i {
				continue
			}
			l := walk(j)
			if l < 0 {
				level[i] = looped // in or after a loop
				return -1
			}
			if l+1 > lv {
				lv = l + 1
			}
		}
		level[i] = lv
		return lv
	}
	top := 0
	for i := range entries {
		if in[i] {
			if l := walk(i); l > top {
				top = l
			}
		}
	}
	waves := make([][]Entry, top+1)
	var last []Entry
	for i, e := range entries {
		if !in[i] {
			continue
		}
		if level[i] < 0 {
			last = append(last, e)
			continue
		}
		waves[level[i]] = append(waves[level[i]], e)
	}
	out := waves[:0]
	for _, w := range waves {
		if len(w) > 0 {
			out = append(out, w)
		}
	}
	if len(last) > 0 {
		out = append(out, last)
	}
	return out
}
