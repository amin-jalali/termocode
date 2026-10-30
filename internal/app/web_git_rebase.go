package app

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Interactive rebase from the desktop page (WEBP-5): the page reads the
// list (git.rebase_plan), the user orders it and sets the actions, and
// the laptop runs it after a card with the laptop's own text, through
// the laptop's own rebase code (git runs termocode as its editor). A
// stopped rebase goes on, skips or aborts the same way.

func init() {
	webOps[webproto.OpGitRebasePlan] = (*Model).webGitRebasePlan
	webOps[webproto.OpGitRebaseState] = (*Model).webGitRebaseState
	webAskOps[webproto.OpGitRebase] = (*Model).webGitRebase
	webAskOps[webproto.OpGitRebaseCtl] = (*Model).webGitRebaseCtl
}

// webRunRebase runs a prepared rebase (tests replace it: git would start
// the test binary as its editor).
var webRunRebase = git.RunRebase

// webRebasePlan is the list git.rebase_plan gave, with what the card
// must say about it.
type webRebasePlan struct {
	check rebaseCheckMsg
}

func (m *Model) webGitRebasePlan(call *webCall) tea.Cmd {
	from := call.args.(*webproto.GitRebasePlanArgs).From
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	if m.gitRebase.InProgress {
		call.fail(webproto.CodeConflict, "A rebase is in progress. Continue or abort it first.")
		return nil
	}
	branch := webName(pushBranch(m.gitBranch))
	call.later()
	return func() tea.Msg {
		base := ""
		if p, err := git.ParentOf(root, from); err == nil {
			base = p
		}
		todo, err := git.LoadRebaseTodo(root, base)
		switch {
		case errors.Is(err, git.ErrRebaseMerge):
			call.fail(webproto.CodeConflict, "There is a merge commit in that range. Rebasing merges is not supported.")
			return nil
		case errors.Is(err, git.ErrRebaseEmpty):
			call.fail(webproto.CodeConflict, "There are no commits to rebase.")
			return nil
		case err != nil:
			webGitFail(call, err)
			return nil
		}
		found := false
		for _, it := range todo.Items {
			found = found || strings.HasPrefix(it.FullHash, from)
		}
		if !found || len(todo.Items) > webproto.MaxRebaseSteps {
			call.fail(webproto.CodeConflict, "That commit is not on this branch, or too far back.")
			return nil
		}
		check := rebaseCheckMsg{todo: todo, dirty: git.WorkTreeDirty(root)}
		check.upstream, check.pushed = git.RebasePushed(root, todo.Base)
		out := webproto.GitRebasePlan{Base: todo.Base, Branch: branch, Steps: []webproto.GitRebaseStep{}, Upstream: webName(check.upstream), Pushed: check.pushed, Dirty: check.dirty}
		for _, it := range todo.Items {
			out.Steps = append(out.Steps, webproto.GitRebaseStep{Hash: it.Hash, Action: string(it.Action), Subject: webName(it.Subject), Body: webName(it.Body)})
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if m.web != nil {
				m.web.more.rebase = &webRebasePlan{check: check}
			}
			call.reply(out)
			return nil
		})
	}
}

func (m *Model) webGitRebaseState(call *webCall) tea.Cmd {
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	st := m.gitRebase
	out := webproto.GitRebaseState{InProgress: st.InProgress}
	if st.InProgress {
		out.Step, out.Total, out.Action, out.Hash = st.Step, st.Total, st.Action, st.Hash
		out.Subject, out.Branch = webName(st.Subject), webName(st.Branch)
		for _, p := range st.Conflicts {
			out.Conflicts = append(out.Conflicts, webName(p))
		}
	}
	call.reply(out)
	return nil
}

// webRebaseTodo builds the todo the page asked for from the plan's rows:
// each row exactly once, in the page's order.
func webRebaseTodo(plan git.RebaseTodo, a *webproto.GitRebaseArgs) (git.RebaseTodo, error) {
	if (a.Base == "") != (plan.Base == "") || !strings.HasPrefix(plan.Base, a.Base) {
		return git.RebaseTodo{}, errors.New("The list is for another base. Open it again.")
	}
	if len(a.Steps) != len(plan.Items) {
		return git.RebaseTodo{}, errors.New("Every commit of the list must be there once.")
	}
	used := make([]bool, len(plan.Items))
	todo := git.RebaseTodo{Base: plan.Base}
	for _, s := range a.Steps {
		at := -1
		for i, it := range plan.Items {
			if !used[i] && (it.Hash == s.Hash || strings.HasPrefix(it.FullHash, s.Hash)) {
				at = i
				break
			}
		}
		if at < 0 {
			return git.RebaseTodo{}, errors.New("Every commit of the list must be there once.")
		}
		used[at] = true
		it := plan.Items[at]
		it.Action, it.Message = git.RebaseAction(s.Action), s.Message
		todo.Items = append(todo.Items, it)
	}
	return todo, todo.Validate()
}

func (m *Model) webGitRebase(call *webCall) *webAsk {
	a := call.args.(*webproto.GitRebaseArgs)
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	if m.gitRebase.InProgress {
		call.fail(webproto.CodeConflict, "A rebase is in progress. Continue or abort it first.")
		return nil
	}
	plan := m.web.more.rebase
	if plan == nil {
		call.fail(webproto.CodeNotFound, "Open the rebase list again.")
		return nil
	}
	todo, err := webRebaseTodo(plan.check.todo, a)
	if err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	check := plan.check
	check.todo = todo
	action := "Start Rebase"
	if check.dirty {
		action = "Stash and Rebase"
	}
	return &webAsk{summary: todo.Summary(), title: "Start the rebase?", body: rebaseConfirmText(check), action: action,
		run: func(m *Model) tea.Cmd {
			if m.gitRebase.InProgress {
				return m.rebaseBusyToast()
			}
			cwd, err := m.rootDir()
			if err != nil {
				return nil
			}
			m.web.more.rebase = nil
			opt := git.RebaseOptions{Autostash: check.dirty}
			return func() tea.Msg {
				out, err := webRunRebase(cwd, todo, opt)
				return rebaseDoneMsg{summary: todo.Summary(), out: out, err: err}
			}
		}}
}

func (m *Model) webGitRebaseCtl(call *webCall) *webAsk {
	do := call.args.(*webproto.GitRebaseCtlArgs).Do
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	st := m.gitRebase
	if !st.InProgress {
		call.fail(webproto.CodeConflict, "No rebase is in progress.")
		return nil
	}
	where := "the rebase"
	if st.Hash != "" {
		where = "the rebase (stopped at " + st.Hash + " \"" + webName(st.Subject) + "\")"
	}
	var title, body, action string
	switch do {
	case "continue":
		title, action = "Continue the rebase?", "Continue"
		body = "Go on with " + where + "? Fixed files are staged first."
	case "skip":
		title, action = "Skip this commit?", "Skip"
		body = "Drop the commit " + where + " stopped at, and go on? Its changes are lost from the branch."
	default:
		title, action = "Abort the rebase?", "Abort"
		body = "Stop " + where + " and put the branch back as it was? Conflict fixes made so far are lost."
	}
	return &webAsk{summary: do, title: title, body: body, action: action,
		run: func(m *Model) tea.Cmd { return m.onRebaseControl("rebase-" + do) }}
}
