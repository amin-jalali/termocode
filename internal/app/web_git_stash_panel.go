package app

import (
	"errors"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The STASHES section of the desktop's Source Control view (GITX-10):
// the git topic carries the laptop's stash list (m.gitStashes, read on
// every git poll); git.stash_show reads one entry's changes and
// git.stash_act applies, pops, drops or branches from one entry after a
// laptop card. Both take only an entry on show; the action checks again,
// right before it runs, that the ref still names the same entry (a new
// stash shifts every ref).

func init() {
	webOps[webproto.OpGitStashShow] = (*Model).webGitStashShow
	webAskOps[webproto.OpGitStashAct] = (*Model).webGitStashAct
}

// webGitStashes is the stash list for the git topic.
func (m *Model) webGitStashes() []webproto.GitStashEntry {
	var out []webproto.GitStashEntry
	for i, e := range m.gitStashes {
		if i == webproto.MaxGitStashes {
			break
		}
		out = append(out, webproto.GitStashEntry{Ref: e.Ref, Message: webName(e.Message)})
	}
	return out
}

// webStashOnShow is the entry ref names in the laptop's stash list.
func (m *Model) webStashOnShow(call *webCall, ref string) (git.StashEntry, bool) {
	i := slices.IndexFunc(m.gitStashes, func(e git.StashEntry) bool { return e.Ref == ref })
	if i < 0 {
		call.fail(webproto.CodeNotFound, "That stash entry is gone. Look at the list again.")
		return git.StashEntry{}, false
	}
	return m.gitStashes[i], true
}

// webStashRepo is the stash commands of the open workspace: git in the
// container or on the remote host through its Host Exec, or on this
// machine (as the laptop's STASHES section runs them).
func (m *Model) webStashRepo(call *webCall) (git.StashRepo, bool) {
	if _, ok := m.webGitRoot(call); !ok {
		return git.StashRepo{}, false
	}
	repo, err := m.gitStashRepo()
	if err != nil {
		call.fail(webproto.CodeConflict, "This workspace cannot run git commands here.")
		return git.StashRepo{}, false
	}
	return repo, true
}

func (m *Model) webGitStashShow(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitStashShowArgs)
	repo, ok := m.webStashRepo(call)
	if !ok {
		return nil
	}
	if _, ok := m.webStashOnShow(call, a.Ref); !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		out, err := repo.Diff(a.Ref)
		if err != nil {
			call.fail(webproto.CodeNotFound, "That stash entry is not here.")
			return nil
		}
		cut := len(out) > webproto.MaxGitDiff
		if cut {
			out = out[:webproto.MaxGitDiff]
		}
		d := webGitPage(webGitRows(out, true), a.From)
		d.Truncated = cut
		call.reply(d)
		return nil
	}
}

func (m *Model) webGitStashAct(call *webCall) *webAsk {
	a := call.args.(*webproto.GitStashActArgs)
	repo, ok := m.webStashRepo(call)
	if !ok {
		return nil
	}
	e, ok := m.webStashOnShow(call, a.Ref)
	if !ok {
		return nil
	}
	ref, want, branch := e.Ref, e.Message, a.Branch
	name := ref + " (" + webName(want) + ")"
	ask := &webAsk{summary: ref}
	var run func() error
	var running, fail, done string
	switch a.Do {
	case webproto.StashApply:
		ask.title, ask.action = "Apply the stash?", "Apply"
		ask.body = "Apply " + name + " to the working files? The entry stays in the stash."
		run, running, fail, done = func() error { return repo.Apply(ref) }, "Applying the stash…", "Stash apply failed", "Applied "+ref
	case webproto.StashPop:
		ask.title, ask.action = "Pop the stash?", "Pop"
		ask.body = "Apply " + name + " to the working files, then drop it from the stash?"
		run, running, fail, done = func() error { return repo.Pop(ref) }, "Popping the stash…", "Pop failed", "Popped "+ref
	case webproto.StashDrop:
		ask.title, ask.action = "Drop the stash?", "Drop"
		ask.body = "Delete " + name + " from the stash? Its commit stays in git for a while, so it can still be brought back."
		run, running, fail, done = func() error { return repo.Drop(ref) }, "Dropping the stash…", "Drop failed", "Dropped "+ref
	case webproto.StashBranch:
		ask.title, ask.action = "Branch from the stash?", "Create Branch"
		ask.summary = ref + " " + branch
		ask.body = "Create the branch " + branch + " from the commit " + name + " was made on, switch to it, apply the entry there and drop it?"
		run, running, fail, done = func() error { return repo.Branch(branch, ref) }, "Creating the branch…", "Branch from stash failed", "Created "+branch+" from "+ref
	default:
		call.fail(webproto.CodeBadRequest, "Unknown stash action.")
		return nil
	}
	ask.run = webGitLaptopRun(func(m *Model) tea.Cmd {
		return m.gitRemoteOp(running, func(string) gitRemoteDoneMsg {
			now, err := repo.List()
			if err == nil && !slices.ContainsFunc(now, func(e git.StashEntry) bool { return e.Ref == ref && e.Message == want }) {
				err = errors.New(ref + " is another entry now. Look at the list again.")
			}
			if err == nil {
				err = run()
			}
			return gitRemoteDoneMsg{failTitle: fail, okText: done, err: err, reload: true}
		})
	})
	return ask
}
