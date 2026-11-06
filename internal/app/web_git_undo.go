package app

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Undo, redo and the reflog from the web (WEBP-9, Git): the laptop's own
// undo (git_undo.go) behind a laptop card. git.undo_plan reads the plan
// off the UI thread and says it in the laptop card's words; git.undo
// runs that plan once (git checks it is still where HEAD is: a HEAD that
// moved stops it), git.redo takes the last undo back. Local changes in
// the way are stashed and put back, as the laptop's Stash and Undo does.

// webUndoPlan is a plan the page was shown.
type webUndoPlan struct {
	plan git.UndoPlan
	move bool
}

func init() {
	webOps[webproto.OpGitReflog] = (*Model).webGitReflog
	webOps[webproto.OpGitUndoPlan] = (*Model).webGitUndoPlan
	webAskOps[webproto.OpGitUndo] = (*Model).webGitUndo
	webAskOps[webproto.OpGitRedo] = (*Model).webGitRedo
	webOps[webproto.OpGitFixupPlan] = (*Model).webGitFixupPlan
	webAskOps[webproto.OpGitFixup] = (*Model).webGitFixup
}

// webFixupPlan is a fixup the page was shown.
type webFixupPlan struct {
	check fixupCheckMsg
	text  string
	stash bool
}

func (m *Model) webGitReflog(call *webCall) tea.Cmd {
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		entries, err := git.Reflog(root, webproto.MaxGitReflog)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if m.web != nil {
				m.web.more.gitLists.reflog = entries
			}
			out := webproto.GitReflog{Entries: []webproto.GitReflogEntry{}}
			for _, e := range entries {
				out.Entries = append(out.Entries, webproto.GitReflogEntry{Hash: e.Short(), Summary: webName(e.Summary()), When: e.When * 1000})
			}
			call.reply(out)
			return nil
		})
	}
}

// webUndoTexts are the laptop card's title and action for a plan.
func webUndoTexts(p git.UndoPlan, move bool) (title, action string) {
	title, action = "Undo the last git action?", "Undo"
	if move {
		title, action = "Move HEAD back to here?", "Move HEAD"
	}
	if len(p.Blocked) > 0 {
		action = "Stash and " + action
	}
	return title, action
}

// webUndoErr says why there is no plan, in one line.
func webUndoErr(call *webCall, err error) {
	var busy *git.BusyError
	switch {
	case errors.As(err, &busy):
		call.fail(webproto.CodeConflict, "A "+busy.Op+" is in progress. Finish or stop it first.")
	case errors.Is(err, git.ErrNothingToUndo), errors.Is(err, git.ErrUndoFirstCommit):
		call.fail(webproto.CodeConflict, "Nothing to undo: "+err.Error())
	default:
		webGitFail(call, err)
	}
}

func (m *Model) webGitUndoPlan(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitUndoPlanArgs)
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	var line *git.ReflogEntry
	if a.Reflog {
		list := m.web.more.gitLists.reflog
		if a.Index >= len(list) || list[a.Index].Short() != a.Hash[:min(len(a.Hash), 7)] {
			call.fail(webproto.CodeNotFound, "That reflog line is not in the list. Open the list again.")
			return nil
		}
		e := list[a.Index]
		line = &e
	}
	call.later()
	return func() tea.Msg {
		var p git.UndoPlan
		var err error
		if line != nil {
			p, err = git.PlanMoveTo(root, *line)
		} else {
			p, err = git.PlanUndo(root)
		}
		return applyMsg(func(m *Model) tea.Cmd {
			switch {
			case err != nil:
				webUndoErr(call, err)
				return nil
			case p.Noop():
				call.fail(webproto.CodeConflict, "Nothing to do: HEAD is there already.")
				return nil
			}
			move := line != nil
			if m.web != nil {
				m.web.more.gitLists.undo = &webUndoPlan{plan: p, move: move}
			}
			title, action := webUndoTexts(p, move)
			call.reply(webproto.GitUndoPlan{Title: title, Text: undoConfirmText(p, move), Action: action, Move: move})
			return nil
		})
	}
}

func (m *Model) webGitUndo(call *webCall) *webAsk {
	move := call.args.(*webproto.GitUndoArgs).Move
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	u := m.web.more.gitLists.undo
	if u == nil || u.move != move {
		call.fail(webproto.CodeConflict, "Read what the undo does first.")
		return nil
	}
	m.web.more.gitLists.undo = nil
	p := u.plan
	title, action := webUndoTexts(p, move)
	return &webAsk{summary: p.Entry.Short() + " " + webName(p.Entry.Summary()), title: title, body: undoConfirmText(p, move), action: action,
		run: func(m *Model) tea.Cmd {
			cwd, err := m.rootDir()
			if err != nil {
				return nil
			}
			opt := git.UndoOptions{Stash: len(p.Blocked) > 0}
			return func() tea.Msg {
				res, err := git.RunUndo(cwd, p, opt)
				return applyMsg(func(m *Model) tea.Cmd {
					return m.onUndoDone(undoDoneMsg{plan: p, move: move, res: res, err: err})
				})
			}
		}}
}

func (m *Model) webGitRedo(call *webCall) *webAsk {
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	if m.gitUndo == nil || m.gitUndo.redo == nil {
		call.fail(webproto.CodeConflict, "Nothing to redo: Redo works right after an undo.")
		return nil
	}
	r := m.gitUndo.redo
	what := "Redo: " + webName(r.plan.Entry.Summary())
	if r.move {
		what = "Redo the move of HEAD"
	}
	return &webAsk{summary: shortHashOf(r.plan.From), title: "Redo?", action: "Redo",
		body: what + "? HEAD goes back to " + shortHashOf(r.plan.From) + ".",
		run: func(m *Model) tea.Cmd {
			c := m.gitRedoCmd()
			if c == nil {
				return nil
			}
			return func() tea.Msg {
				msg := c()
				if d, ok := msg.(redoDoneMsg); ok {
					return applyMsg(func(m *Model) tea.Cmd { return m.onRedoDone(d) })
				}
				return msg
			}
		}}
}

// webGitStagedCount counts the staged files the Git tab lists.
func (m *Model) webGitStagedCount() int {
	n := 0
	for _, f := range m.gitFiles {
		if f.Staged() {
			n++
		}
	}
	return n
}

func (m *Model) webGitFixupPlan(call *webCall) tea.Cmd {
	hash := call.args.(*webproto.GitFixupArgs).Hash
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	switch {
	case m.gitRebase.InProgress || m.webGitBuild().Op != "":
		call.fail(webproto.CodeConflict, "A merge or rebase is in progress. Finish it first.")
		return nil
	case m.webGitStagedCount() == 0:
		call.fail(webproto.CodeConflict, "Nothing is staged. Stage the changes that belong in the older commit first.")
		return nil
	}
	call.later()
	return func() tea.Msg {
		msg := fixupCheckMsg{}
		base, _ := git.ParentOf(root, hash)
		todo, err := git.LoadRebaseTodo(root, base)
		if err == nil && (len(todo.Items) == 0 || !strings.HasPrefix(todo.Items[0].FullHash, hash)) {
			err = errors.New("that commit is not in HEAD's history")
		}
		if err == nil {
			msg.target, msg.commits = todo.Items[0].FullHash, len(todo.Items)
			msg.short, msg.subject = todo.Items[0].Hash, todo.Items[0].Subject
			msg.upstream, msg.pushed = git.RebasePushed(root, base)
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if err != nil {
				call.fail(webproto.CodeConflict, "Can't fix up into this commit: "+err.Error())
				return nil
			}
			text, stash := fixupConfirmText(msg, m.gitFiles)
			if m.web != nil {
				m.web.more.gitLists.fixup = &webFixupPlan{check: msg, text: text, stash: stash}
			}
			action := "Fixup"
			if stash {
				action = "Stash and Fixup"
			}
			call.reply(webproto.GitFixupPlan{Text: text, Action: action})
			return nil
		})
	}
}

func (m *Model) webGitFixup(call *webCall) *webAsk {
	hash := call.args.(*webproto.GitFixupArgs).Hash
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	f := m.web.more.gitLists.fixup
	if f == nil || !strings.HasPrefix(f.check.target, hash) {
		call.fail(webproto.CodeConflict, "Read what the fixup does first.")
		return nil
	}
	m.web.more.gitLists.fixup = nil
	action := "Fixup"
	if f.stash {
		action = "Stash and Fixup"
	}
	target := f.check.target
	return &webAsk{summary: f.check.short, title: "Fixup into " + f.check.short + "?", body: f.text, action: action,
		run: func(m *Model) tea.Cmd {
			if m.gitRebase.InProgress {
				return m.rebaseBusyToast()
			}
			cwd, err := m.rootDir()
			if err != nil {
				return nil
			}
			opt := git.RebaseOptions{Autostash: f.stash}
			return func() tea.Msg {
				out, err := git.FixupInto(cwd, target, opt)
				return applyMsg(func(m *Model) tea.Cmd {
					return m.onRebaseDone(rebaseDoneMsg{summary: "staged changes folded into " + target[:min(7, len(target))], out: out, err: err})
				})
			}
		}}
}
