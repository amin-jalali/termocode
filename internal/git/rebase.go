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
	return parseRebaseLog(out, base)
}

// parseRebaseLog reads the records of LoadRebaseTodo's git log format.
func parseRebaseLog(out, base string) (RebaseTodo, error) {
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

// Move swaps row i with the row dir (-1 up, +1 down) away and returns
// the row's new index; at either end (or out of range) nothing moves.
func (t *RebaseTodo) Move(i, dir int) int {
	j := i + dir
	if i < 0 || i >= len(t.Items) || j < 0 || j >= len(t.Items) {
		return i
	}
	t.Items[i], t.Items[j] = t.Items[j], t.Items[i]
	return j
}

// SetAction sets row i's action (out of range: ignored).
func (t *RebaseTodo) SetAction(i int, a RebaseAction) {
	if i >= 0 && i < len(t.Items) {
		t.Items[i].Action = a
	}
}

// Result is how many commits the todo leaves: squash and fixup meld into
// the row above, drop removes the commit.
func (t RebaseTodo) Result() int {
	n := 0
	for _, it := range t.Items {
		switch it.Action {
		case RebaseSquash, RebaseFixup, RebaseDrop:
		default:
			n++
		}
	}
	return n
}

// Summary is the preview line: "5 commits → 3 commits".
func (t RebaseTodo) Summary() string {
	return commitsWord(len(t.Items)) + " → " + commitsWord(t.Result())
}

func commitsWord(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

// Validate says why git could not run the todo (or should not): a squash
// or fixup needs a kept commit above it, and at least one commit must be
// left.
func (t RebaseTodo) Validate() error {
	kept := false
	for _, it := range t.Items {
		switch it.Action {
		case RebaseDrop:
		case RebaseSquash, RebaseFixup:
			if !kept {
				return fmt.Errorf("%s %s has nothing above it to meld into", it.Action, it.Hash)
			}
		default:
			kept = true
		}
	}
	if !kept {
		return errors.New("every commit is dropped; nothing would be left")
	}
	return nil
}

// Format is the todo as git reads it: "<action> <full hash> <subject>"
// per row. git only reads the first two words; the subject helps a human
// reading .git/rebase-merge/done.
func (t RebaseTodo) Format() string {
	var b strings.Builder
	for _, it := range t.Items {
		subject := strings.ReplaceAll(it.Subject, "\n", " ")
		fmt.Fprintf(&b, "%s %s %s\n", it.Action, it.FullHash, subject)
	}
	return b.String()
}

// ParentOf is the full hash of commit's first parent (an error for a root
// commit).
func ParentOf(dir, commit string) (string, error) {
	out, err := runOut(dir, "git", "rev-parse", "--verify", "-q", commit+"^")
	if err != nil {
		return "", fmt.Errorf("%s has no parent", commit)
	}
	return strings.TrimSpace(out), nil
}
