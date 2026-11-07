package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Stacked worktrees (CMP-17) ───────────────────────────────────────────
//
// A child worktree (c) is stacked on its parent: it starts from the
// parent's branch, worktrees.json keeps that branch as its base, and
// Git: Create Pull Request… and the ship loop target the parent's branch
// from inside the child instead of the default branch. r in WORKTREES
// (Worktree: Restack Selected onto Parent…) replays the child's commits
// onto the parent's branch after the parent moved on: a card asks first,
// Cancel first and focused; Esc cancels. Uncommitted changes stop it
// before anything runs; a conflict leaves the rebase open in the child.

const (
	confirmKindWTRestack confirmKindEnum = 1717
	wtRestackPaletteID                   = "worktree-restack"
)

func init() {
	aiActionPolicies[wtRestackPaletteID] = aiDestructive() // rewrites the child's commits; the card asks
}

// wtRestackPending is the open restack card.
type wtRestackPending struct {
	title, path, branch, onto, parent string
}

// wtRestackedMsg is the end of a restack.
type wtRestackedMsg struct {
	title, branch, onto string
	out                 git.RebaseOutcome
	err                 error
}

// wtRestackRun replays the branch checked out in dir onto onto (a var:
// tests).
var wtRestackRun = func(dir, onto string) (git.RebaseOutcome, error) {
	return git.RebaseOnto(dir, onto, false, git.RebaseOptions{})
}

// stackBaseFor is the branch a pull request from dir targets when dir is
// in a stacked child worktree ("" = not stacked: use the default
// branch). A var: tests.
var stackBaseFor = func(dir string) string {
	if dir == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	at := dir // git runs in the nearest folder that exists
	for {
		if fi, err := os.Stat(at); err == nil && fi.IsDir() {
			break
		}
		up := filepath.Dir(at)
		if up == at {
			return ""
		}
		at = up
	}
	list, common, err := git.ListWorktrees(at)
	if err != nil {
		return ""
	}
	metas, err := worktrees.Load(common)
	if err != nil {
		return ""
	}
	paths := make([]string, 0, len(list))
	branchOf := make(map[string]string, len(list))
	for _, w := range list {
		paths = append(paths, w.Path)
		branchOf[w.Path] = w.Branch
	}
	own := worktrees.Owner(paths, dir)
	if own == "" {
		return ""
	}
	return worktrees.StackBase(metas, own, branchOf)
}

// prBaseFor is the base of a new pull request from dir: the parent's
// branch in a stacked child (stacked), else def (the default branch).
func prBaseFor(dir string, def func() (string, error)) (base string, stacked bool, err error) {
	if b := stackBaseFor(dir); b != "" {
		return b, true, nil
	}
	base, err = def()
	return base, false, err
}

// wtStackOnto is the parent's branch the item is stacked on, from the
// loaded entries ("" = not stacked), and the parent's title.
func (m *Model) wtStackOnto(it wtItem) (onto, parent string) {
	if it.e.Meta.Parent == "" {
		return "", ""
	}
	metas := make(map[string]worktrees.Meta, len(m.wt.entries))
	branchOf := make(map[string]string, len(m.wt.entries))
	for _, e := range m.wt.entries {
		metas[e.Path] = e.Meta
		if !e.Gone {
			branchOf[e.Path] = e.Branch
		}
		if e.Path == it.e.Meta.Parent {
			parent = e.Title()
		}
	}
	if parent == "" {
		parent = wtShortPath(it.e.Meta.Parent)
	}
	return worktrees.StackBase(metas, it.e.Path, branchOf), parent
}

// worktreeRestack is r: ask to replay the cursor's child onto its
// parent's branch.
func (m *Model) worktreeRestack() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	if it.e.Gone {
		return m.pushToastDetail(toast.Info, "Its folder is gone", "F forgets it.")
	}
	onto, parent := m.wtStackOnto(it)
	switch {
	case onto == "":
		return m.pushToastDetail(toast.Info, wtTitle(it)+" is not stacked", "Only a child worktree (made with c) restacks onto its parent's branch.")
	case it.e.Branch == "":
		return m.pushToastDetail(toast.Info, wtTitle(it)+" is not on a branch", "Check out a branch in it first; a detached worktree is not restacked.")
	case it.e.Branch == onto:
		return m.pushToastDetail(toast.Info, wtTitle(it)+" is on its parent's branch", "There is nothing to restack.")
	}
	p := &wtRestackPending{title: wtTitle(it), path: it.e.Path, branch: it.e.Branch, onto: onto, parent: parent}
	var b strings.Builder
	b.WriteString("Replay the commits of " + p.branch + " onto " + p.onto + ", the branch of its parent " + p.parent + ".")
	b.WriteString("\n\nIt runs in " + wtShortPath(p.path) + ". This rewrites the commits of " + p.branch + ": if it was pushed, the next push needs force-with-lease.")
	b.WriteString("\n\nUncommitted changes stop it before anything runs. A conflict leaves the rebase open there: open the worktree to continue or abort it.")
	m.wt.restackAsk = p
	m.confirm = confirm.Risky("Restack "+p.title+"?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "restack", Title: "Restack onto " + p.onto, Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTRestack
	return nil
}

// answerWTRestack answers the card ("" = Esc = Cancel).
func (m *Model) answerWTRestack(id string) tea.Cmd {
	p := m.wt.restackAsk
	m.wt.restackAsk = nil
	if p == nil || id != "restack" {
		return nil
	}
	off := m.wt.off
	return func() tea.Msg {
		msg := wtRestackedMsg{title: p.title, branch: p.branch, onto: p.onto}
		if off {
			return msg
		}
		msg.out, msg.err = wtRestackRun(p.path, p.onto)
		return msg
	}
}

// onWTRestacked says how the restack ended and reads the list again.
func (m *Model) onWTRestacked(msg wtRestackedMsg) tea.Cmd {
	var t tea.Cmd
	switch {
	case errors.Is(msg.err, git.ErrRebaseDirty):
		t = m.pushToastDetail(toast.Warn, "Restack did not start", msg.title+" has uncommitted changes: commit or stash them first.")
	case msg.err != nil:
		t = m.pushToastDetail(toast.Warn, "Restack of "+msg.title+" failed", msg.err.Error())
	case msg.out.Stopped && len(msg.out.Conflicts) > 0:
		t = m.pushToastDetail(toast.Warn, "Restack stopped on a conflict", "In "+msg.title+": "+wtFirstFew(msg.out.Conflicts, 3)+". Open the worktree to resolve and continue, or abort the rebase.")
	case msg.out.Stopped:
		t = m.pushToastDetail(toast.Warn, "Restack stopped", "The rebase in "+msg.title+" waits: open the worktree to continue or abort it.")
	default:
		t = m.pushToastDetail(toast.Info, "Restacked "+msg.branch+" onto "+msg.onto, "If it was pushed before, push it with force-with-lease.")
	}
	return tea.Batch(t, m.worktreesReload())
}

// wtFirstFew is up to n names, then "and N more".
func wtFirstFew(names []string, n int) string {
	if len(names) <= n {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:n], ", ") + " and " + wtPlural(len(names)-n, "more file")
}
