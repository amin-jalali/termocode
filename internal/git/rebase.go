package git

import (
	"errors"
	"fmt"
	"strings"
)

// Interactive rebase (G-RBS): termocode prepares the todo list itself (the
// user picks an action per commit and the order in a card) and git runs
// it. The list is the commits from Base (not included) to HEAD, oldest
// first, as `git rebase -i` shows them.

// RebaseAction is what a todo row does with its commit.
type RebaseAction string

const (
	RebasePick   RebaseAction = "pick"   // keep the commit
	RebaseReword RebaseAction = "reword" // keep it with a new message
	RebaseEdit   RebaseAction = "edit"   // stop after it so the user can amend
	RebaseSquash RebaseAction = "squash" // meld into the row above, keep both messages
	RebaseFixup  RebaseAction = "fixup"  // meld into the row above, drop its message
	RebaseDrop   RebaseAction = "drop"   // remove the commit
)

// RebaseItem is one todo row.
type RebaseItem struct {
	Action         RebaseAction
	Hash, FullHash string
	Subject, Body  string
	Message        string // reword: the new message ("" keeps the old one)
}

// RebaseTodo is a prepared interactive rebase.
type RebaseTodo struct {
	Base  string // full hash the commits are replayed onto; "" = from the root
	Items []RebaseItem
}

var (
	// ErrRebaseMerge: the range has a merge commit (not supported yet).
	ErrRebaseMerge = errors.New("merge commit in range")
	// ErrRebaseEmpty: there are no commits after the base.
	ErrRebaseEmpty = errors.New("no commits to rebase")
)

// LoadRebaseTodo lists base..HEAD (oldest first) as a todo of picks. base
// "" starts at the root commit. A merge commit in the range is refused.
func LoadRebaseTodo(dir, base string) (RebaseTodo, error) {
	rng := "HEAD"
	if base != "" {
		full, err := runOut(dir, "git", "rev-parse", "--verify", "-q", base+"^{commit}")
		if err != nil {
			return RebaseTodo{}, fmt.Errorf("no commit %s", base)
		}
		base = strings.TrimSpace(full)
		rng = base + "..HEAD"
	}
	const format = "--format=%h%x1f%H%x1f%p%x1f%s%x1f%b%x1e"
	out, err := runOut(dir, "git", "log", "--reverse", format, rng, "--")
	if err != nil {
		return RebaseTodo{}, fmt.Errorf("git log %s: %w", rng, err)
	}
	todo := RebaseTodo{Base: base}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		f := strings.Split(rec, "\x1f")
		if len(f) < 5 {
			continue
		}
		if len(strings.Fields(f[2])) > 1 {
			return RebaseTodo{}, fmt.Errorf("%w: %s %q is a merge commit; rebasing merges is not supported", ErrRebaseMerge, f[0], f[3])
		}
		todo.Items = append(todo.Items, RebaseItem{Action: RebasePick, Hash: f[0], FullHash: f[1],
			Subject: f[3], Body: strings.TrimSpace(f[4])})
	}
	if len(todo.Items) == 0 {
		return RebaseTodo{}, ErrRebaseEmpty
	}
	return todo, nil
}
