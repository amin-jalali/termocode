package app

import (
	"errors"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Line and hunk staging from the desktop's diff editor (WEBP-9, Git):
// the laptop's Git: Stage Line, Unstage Line, Discard Line (or Selected
// Lines) and Discard Hunk (at cursor). The rows are those of the file as
// saved: a file with unsaved edits on the laptop is refused, as the
// laptop's own command refuses it. Discarding waits for a laptop card.

func init() {
	webOps[webproto.OpGitStageLines] = (*Model).webGitStageLines
	webOps[webproto.OpGitUnstageLines] = (*Model).webGitUnstageLines
	webAskOps[webproto.OpGitDiscardLines] = (*Model).webGitDiscardLines
	webAskOps[webproto.OpGitDiscardHunk] = (*Model).webGitDiscardHunk
}

// webGitUnsaved: the laptop has path open with unsaved edits.
func (m *Model) webGitUnsaved(path string) bool {
	for _, b := range m.bufs {
		if rel, ok := m.webRel(b.Path); ok && rel == path && b.Modified {
			return true
		}
	}
	return false
}

// webGitLinesPick checks a line op's file: listed, a change on the side
// it leaves, tracked, saved.
func (m *Model) webGitLinesPick(call *webCall, a *webproto.GitLinesArgs, unstage bool) (string, bool) {
	root, ok := m.webGitRoot(call)
	if !ok || !m.webGitPick(call, root, []string{a.Path}, unstage) {
		return "", false
	}
	if f, _ := m.webGitFind(root, a.Path); f.Untracked() {
		call.fail(webproto.CodeConflict, "A new file is staged whole. Use Stage.")
		return "", false
	}
	if m.webGitUnsaved(a.Path) {
		call.fail(webproto.CodeConflict, "Save the file first: the rows are cut from the saved file.")
		return "", false
	}
	return root, true
}

// webGitRows names rows for the cards and the audit: "line 4", "lines 4-6".
func webGitRowsName(a *webproto.GitLinesArgs) string {
	if a.From == a.To {
		return "line " + strconv.Itoa(a.From)
	}
	return "lines " + strconv.Itoa(a.From) + "-" + strconv.Itoa(a.To)
}

func (m *Model) webGitStageLines(call *webCall) tea.Cmd   { return m.webGitLinesOp(call, false) }
func (m *Model) webGitUnstageLines(call *webCall) tea.Cmd { return m.webGitLinesOp(call, true) }

func (m *Model) webGitLinesOp(call *webCall, unstage bool) tea.Cmd {
	a := call.args.(*webproto.GitLinesArgs)
	if a.Index && !unstage {
		call.fail(webproto.CodeBadRequest, "Index rows are for unstaging only.")
		return nil
	}
	root, ok := m.webGitLinesPick(call, a, unstage)
	if !ok {
		return nil
	}
	op := git.StageLines
	switch {
	case unstage && a.Index:
		op = git.UnstageIndexLines
	case unstage:
		op = git.UnstageLines
	}
	summary := a.Path + " " + strconv.Itoa(a.From) + "-" + strconv.Itoa(a.To)
	return m.webGitWrite(call, summary, func() error {
		err := op(root, a.Path, a.From, a.To)
		if errors.Is(err, git.ErrNoLineChange) {
			return git.ErrHunkGone // "That change moved."
		}
		return err
	})
}

// webGitDiscardDone ends a discard card's run: the toast, the laptop's
// buffers read again, git's state.
func webGitDiscardDone(path string, err error) tea.Msg {
	return applyMsg(func(m *Model) tea.Cmd {
		var t tea.Cmd
		switch {
		case errors.Is(err, git.ErrHunkGone), errors.Is(err, git.ErrNoLineChange):
			m.toast, t = m.toast.PushDetail(toast.Warn, "Nothing discarded: that change moved", path)
		case err != nil:
			m.toast, t = m.toast.PushDetail(toast.Errr, "Discard failed", err.Error())
		default:
			m.toast, t = m.toast.PushDetail(toast.Info, "Discarded changes", path)
		}
		if m.nvim != nil {
			_ = m.nvim.ExecLua(webLuaCheckTime)
		}
		return tea.Batch(m.pollGitCmd(), t)
	})
}

func (m *Model) webGitDiscardLines(call *webCall) *webAsk {
	a := *call.args.(*webproto.GitLinesArgs)
	if a.Index {
		call.fail(webproto.CodeBadRequest, "Index rows are for unstaging only.")
		return nil
	}
	root, ok := m.webGitLinesPick(call, &a, false)
	if !ok {
		return nil
	}
	rows := webGitRowsName(&a)
	return &webAsk{
		summary: a.Path + " " + strconv.Itoa(a.From) + "-" + strconv.Itoa(a.To),
		title:   "Discard lines?",
		body:    "Discard the changes on " + rows + " of " + a.Path + "? This cannot be undone.",
		action:  "Discard",
		run: func(m *Model) tea.Cmd {
			return func() tea.Msg {
				return webGitDiscardDone(a.Path, git.DiscardLines(root, a.Path, a.From, a.To))
			}
		},
	}
}

func (m *Model) webGitDiscardHunk(call *webCall) *webAsk {
	a := *call.args.(*webproto.GitHunkArgs)
	root, ok := m.webGitRoot(call)
	if !ok || !m.webGitPick(call, root, []string{a.Path}, false) {
		return nil
	}
	if f, _ := m.webGitFind(root, a.Path); f.Untracked() {
		call.fail(webproto.CodeConflict, "A new file has no hunks. Use Discard Changes.")
		return nil
	}
	if !webGitInside(root, a.Path) {
		call.fail(webproto.CodeForbidden, "That path leaves the workspace.")
		return nil
	}
	return &webAsk{
		summary: a.Path + " " + a.Hunk,
		title:   "Discard this change?",
		body:    "Discard the change " + a.Hunk + " in " + a.Path + "? This cannot be undone.",
		action:  "Discard",
		run: func(m *Model) tea.Cmd {
			return func() tea.Msg {
				return webGitDiscardDone(a.Path, git.DiscardHunkHeader(root, a.Path, a.Hunk))
			}
		},
	}
}
