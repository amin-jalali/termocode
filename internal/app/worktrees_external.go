package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── External and gone worktrees (WTX-10, W2 leftover) ────────────────────
//
// A worktree made outside termocode (`git worktree add` by hand) shows in
// EXTERNAL; its card says "no meta" while termocode knows nothing about
// it. Open, Add folder, Pin and Link work on it as on any other; A
// (Worktree: Adopt Selected) gives it metadata, so its notes and links
// are kept. It stays in EXTERNAL: termocode did not make it.
//
// A worktree whose folder is gone stays on the list until F (Worktree:
// Forget Gone Selected) drops its metadata and git's record of it. F
// refuses one whose folder is still there: it never deletes files.

const (
	wtAdoptPaletteID  = "worktree-adopt"
	wtForgetPaletteID = "worktree-forget"
)

// wtForgetRun drops git's record of a gone worktree (a var: tests).
var wtForgetRun = git.ForgetWorktree

// wtNoMeta: the card says "no meta".
func wtNoMeta(it wtItem) bool {
	return it.e.Kind == worktrees.KindExternal && !it.e.Gone && it.e.Meta.Empty()
}

// worktreeAdopt gives the cursor's external worktree metadata (A).
func (m *Model) worktreeAdopt() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	switch {
	case it.e.Kind != worktrees.KindExternal:
		return m.pushToastDetail(toast.Info, "Nothing to adopt", "Adopt is for worktrees made outside termocode; this one already has its notes.")
	case it.e.Gone:
		return m.pushToastDetail(toast.Info, "Nothing to adopt", "Its folder is gone: F forgets it.")
	case it.e.Meta.Adopted:
		return m.pushToastDetail(toast.Info, "Already adopted", wtShortPath(it.e.Path))
	}
	if err := m.wtTouchMeta(it.e.Path, func(md *worktrees.Meta) { md.Adopted, md.LastSeen = true, clock.Now() }); err != nil {
		return m.pushToastDetail(toast.Warn, "The worktree could not be adopted", err.Error())
	}
	m.wt.cursorPath = it.e.Path
	return m.pushToastDetail(toast.Info, "Adopted "+wtTitle(it), "termocode keeps its pins, links and notes now.")
}

// wtForgotMsg is the end of a Forget.
type wtForgotMsg struct {
	path string
	err  error
}

// worktreeForget forgets the cursor's gone worktree (F).
func (m *Model) worktreeForget() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	if !it.e.Gone {
		return m.pushToastDetail(toast.Info, "Nothing to forget", "Its folder is still there; Forget is only for worktrees whose folder is gone.")
	}
	root := m.wt.root
	if root == "" {
		root, _ = m.rootDir()
	}
	path, off := it.e.Path, m.wt.off
	return func() tea.Msg {
		if off {
			return wtForgotMsg{path: path}
		}
		return wtForgotMsg{path: path, err: wtForgetRun(root, path)}
	}
}

func (m *Model) onWTForgot(msg wtForgotMsg) tea.Cmd {
	st := m.wt
	if msg.err != nil {
		return m.pushToastDetail(toast.Warn, "The worktree could not be forgotten", msg.err.Error())
	}
	if !st.off && st.common != "" {
		if err := worktrees.Update(st.common, func(mm map[string]worktrees.Meta) { delete(mm, msg.path) }); err != nil {
			return m.pushToastDetail(toast.Warn, "The worktree's notes could not be dropped", err.Error())
		}
	}
	kept := st.entries[:0]
	for _, e := range st.entries {
		if e.Path != msg.path {
			kept = append(kept, e)
		}
	}
	st.entries = kept
	st.touch()
	return m.pushToastDetail(toast.Info, "Forgot "+wtShortPath(msg.path), "Its metadata and git's record of it are gone; no file was touched.")
}

func init() {
	aiActionPolicies[wtAdoptPaletteID] = aiNever("acts on the worktree the user selected in the WORKTREES view")
	aiActionPolicies[wtForgetPaletteID] = aiNever("acts on the worktree the user selected in the WORKTREES view")
}
