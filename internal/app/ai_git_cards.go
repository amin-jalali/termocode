package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The git and worktree tools ask exactly where the GUI asks. Where the
// GUI asks every time (a push, a checkout, an amend, a force push, reset
// --hard, a discard, a branch or worktree delete, a rebase, a pull
// request merge …) the tool's gate gives no class: the card has Cancel
// first and focused, the action button is red, and there is no "Always
// allow". Where the GUI does not ask (stage, a plain commit, a new
// branch …) the card keeps "Always allow git". A typed confirmation of
// the GUI (the branch name of a force push or of an unmerged worktree)
// is never taken from the model: the tool opens the GUI's own card and
// the user types it there.

// aiGitAsksEveryTime: t is a git, worktree or pull request call whose
// card the GUI shows every time (mutating, no class).
func aiGitAsksEveryTime(t ai.Tool) bool {
	if !t.Mutating || t.Class != "" {
		return false
	}
	return strings.HasPrefix(t.Name, "git_") || t.Name == "worktree"
}
