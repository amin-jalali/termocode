package app

import (
	"errors"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The git lists of the desktop's quick input (WEBP-9, Git): Git: Show
// Tags… and Git: Show Stash List…. The lists are read off the UI thread
// and kept; checking out a tag and applying a stash entry take only an
// entry of the list the page was given, wait for a laptop card, and run
// as the laptop's own git command (one remote op at a time, its toasts).

// webGitLists is what the git lists gave last (Update only).
type webGitLists struct {
	tags   []string
	stash  []git.StashEntry
	reflog []git.ReflogEntry
	undo   *webUndoPlan // the plan git.undo_plan gave last (git.undo runs it once)
}

func init() {
	webOps[webproto.OpGitTags] = (*Model).webGitTags
	webOps[webproto.OpGitStashList] = (*Model).webGitStashList
	webAskOps[webproto.OpGitCheckoutTag] = (*Model).webGitCheckoutTag
	webAskOps[webproto.OpGitStashApply] = (*Model).webGitStashApply
}

func (m *Model) webGitTags(call *webCall) tea.Cmd {
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		tags, err := git.Tags(root)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		return applyMsg(func(m *Model) tea.Cmd {
			out := webproto.GitTags{Tags: []string{}}
			for i, n := range tags {
				if i == webproto.MaxGitTags {
					out.More = len(tags) - i
					break
				}
				out.Tags = append(out.Tags, webName(n))
			}
			if m.web != nil {
				m.web.more.gitLists.tags = out.Tags
			}
			call.reply(out)
			return nil
		})
	}
}

func (m *Model) webGitStashList(call *webCall) tea.Cmd {
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		entries, err := git.StashList(root)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if m.web != nil {
				m.web.more.gitLists.stash = entries
			}
			out := webproto.GitStashList{Entries: []webproto.GitStashEntry{}}
			for _, e := range entries {
				out.Entries = append(out.Entries, webproto.GitStashEntry{Ref: e.Ref, Message: webName(e.Message)})
			}
			call.reply(out)
			return nil
		})
	}
}

func (m *Model) webGitCheckoutTag(call *webCall) *webAsk {
	tag := call.args.(*webproto.GitTagArgs).Tag
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	if !slices.Contains(m.web.more.gitLists.tags, tag) {
		call.fail(webproto.CodeNotFound, "That tag is not in the list. Open the list again.")
		return nil
	}
	body := "Check out the tag " + tag + "? HEAD ends up detached: new commits there belong to no branch."
	if len(m.gitFiles) > 0 {
		body += " " + m.webGitChanged() + " go with you; git stops if they would be lost."
	}
	return &webAsk{summary: tag, title: "Check out tag?", body: body, action: "Check Out",
		run: webGitLaptopRun(func(m *Model) tea.Cmd {
			return m.gitRemoteOp("Checking out "+tag+"…", func(cwd string) gitRemoteDoneMsg {
				return gitRemoteDoneMsg{failTitle: "Checkout failed", okText: "Checked out tag (detached HEAD) " + tag, err: git.Checkout(cwd, tag), reload: true}
			})
		})}
}

func (m *Model) webGitStashApply(call *webCall) *webAsk {
	ref := call.args.(*webproto.GitStashRefArgs).Ref
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	i := slices.IndexFunc(m.web.more.gitLists.stash, func(e git.StashEntry) bool { return e.Ref == ref })
	if i < 0 {
		call.fail(webproto.CodeNotFound, "That stash entry is not in the list. Open the list again.")
		return nil
	}
	want := m.web.more.gitLists.stash[i].Message
	msg := webName(want)
	return &webAsk{summary: ref, title: "Apply the stash?", action: "Apply",
		body: "Apply " + ref + " (" + msg + ") to the working files? The entry stays in the stash.",
		run: webGitLaptopRun(func(m *Model) tea.Cmd {
			return m.gitRemoteOp("Applying the stash…", func(cwd string) gitRemoteDoneMsg {
				// The entry may have moved since the list (a new stash
				// shifts every ref): apply only the entry the card named.
				now, err := git.StashList(cwd)
				if err == nil && !slices.ContainsFunc(now, func(e git.StashEntry) bool { return e.Ref == ref && e.Message == want }) {
					err = errors.New(ref + " is another entry now. Open the list again.")
				}
				if err == nil {
					err = git.StashApply(cwd, ref)
				}
				return gitRemoteDoneMsg{failTitle: "Stash apply failed", okText: "Applied " + ref, err: err, reload: true}
			})
		})}
}
