package app

import (
	"slices"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Git push, pull, checkout and stash from the page (WEB-42, web design
// 2.6). Each one waits for a laptop card; confirmed there, it runs as the
// laptop's own command (Git: Push, Pull, …): the same toasts, the same
// "one remote op at a time" rule and git's own credential helper on the
// laptop. No password, token or prompt ever goes through the web: git
// runs with no terminal and no stdin, and its error text passes the
// redaction like every string. There is no force push from the web.

func init() {
	webOps[webproto.OpGitBranches] = (*Model).webGitBranches
	webAskOps[webproto.OpGitPush] = (*Model).webGitPush
	webAskOps[webproto.OpGitPull] = (*Model).webGitPull
	webAskOps[webproto.OpGitCheckout] = (*Model).webGitCheckout
	webAskOps[webproto.OpGitStash] = (*Model).webGitStash
	webAskOps[webproto.OpGitStashPop] = (*Model).webGitStashPop
}

// webGitBranches lists the local branches (off the UI thread) and keeps
// them for git.checkout.
func (m *Model) webGitBranches(call *webCall) tea.Cmd {
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	current := m.gitBranch.Name
	call.later()
	return func() tea.Msg {
		names, err := git.Branches(root)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		stashes, _ := git.StashList(root)
		up := git.HasUpstream(root)
		return applyMsg(func(m *Model) tea.Cmd {
			if m.web != nil {
				m.web.more.branches = names
			}
			out := webproto.GitBranches{Current: webName(current), Branches: []string{}, Stashes: len(stashes), Upstream: up}
			for i, n := range names {
				if i == webproto.MaxGitBranches {
					out.More = len(names) - i
					break
				}
				out.Branches = append(out.Branches, n)
			}
			call.reply(out)
			return nil
		})
	}
}

// webGitLaptopRun is the run of a git card: the laptop's own command.
func webGitLaptopRun(cmd func(m *Model) tea.Cmd) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		if m.gitRemoteBusy != "" {
			return m.pushToastDetail(toast.Warn, "Git operation already running", m.gitRemoteBusy)
		}
		return cmd(m)
	}
}

// webGitChanged says how many files have changes, for the cards.
func (m *Model) webGitChanged() string {
	n := len(m.gitFiles)
	if n == 1 {
		return "1 changed file"
	}
	return strconv.Itoa(n) + " changed files"
}

// webGitBranchName is the branch the cards name ("" on a detached HEAD).
func (m *Model) webGitBranchName(call *webCall) (string, bool) {
	b := pushBranch(m.gitBranch)
	if b == "" {
		call.fail(webproto.CodeConflict, "HEAD is not on a branch. Do this on the laptop.")
		return "", false
	}
	return b, true
}

func (m *Model) webGitPush(call *webCall) *webAsk {
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	b, ok := m.webGitBranchName(call)
	if !ok {
		return nil
	}
	what := "the branch " + b
	if n := m.gitBranch.Ahead; n > 0 {
		what = strconv.Itoa(n) + " commits on " + b
		if n == 1 {
			what = "1 commit on " + b
		}
	}
	return &webAsk{summary: b, title: "Push?", body: "Push " + what + " to its remote? Git on the laptop signs in, never the web.",
		action: "Push", run: webGitLaptopRun((*Model).gitPush)}
}

func (m *Model) webGitPull(call *webCall) *webAsk {
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	b, ok := m.webGitBranchName(call)
	if !ok {
		return nil
	}
	body := "Pull into " + b + "? Files on the laptop may change."
	if len(m.gitFiles) > 0 {
		body += " There are " + m.webGitChanged() + "; git stops if they would be lost."
	}
	return &webAsk{summary: b, title: "Pull?", body: body, action: "Pull", run: webGitLaptopRun((*Model).gitPull)}
}

func (m *Model) webGitCheckout(call *webCall) *webAsk {
	to := call.args.(*webproto.GitCheckoutArgs).Branch
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	if !slices.Contains(m.web.more.branches, to) {
		call.fail(webproto.CodeNotFound, "That branch is not in the list. Open the list again.")
		return nil
	}
	if to == m.gitBranch.Name {
		call.fail(webproto.CodeConflict, "You are on that branch already.")
		return nil
	}
	body := "Switch to " + to + "?"
	if len(m.gitFiles) > 0 {
		body += " " + m.webGitChanged() + " go with you; git stops if they would be lost."
	}
	return &webAsk{summary: to, title: "Switch branch?", body: body, action: "Switch",
		run: webGitLaptopRun(func(m *Model) tea.Cmd {
			return m.gitRemoteOp("Switching branch…", func(cwd string) gitRemoteDoneMsg {
				return gitRemoteDoneMsg{failTitle: "Checkout failed", okText: "Switched to " + to, err: git.Checkout(cwd, to), reload: true}
			})
		})}
}

func (m *Model) webGitStash(call *webCall) *webAsk {
	msg := call.args.(*webproto.GitStashArgs).Message
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	if len(m.gitFiles) == 0 {
		call.fail(webproto.CodeConflict, "There is nothing to stash.")
		return nil
	}
	return &webAsk{summary: m.webGitChanged(), title: "Stash changes?",
		body:   "Stash the changes of " + m.webGitChanged() + "? The files go back to the last commit; Pop brings the changes back.",
		action: "Stash",
		run: webGitLaptopRun(func(m *Model) tea.Cmd {
			return m.gitRemoteOp("Stashing…", func(cwd string) gitRemoteDoneMsg {
				return gitRemoteDoneMsg{failTitle: "Stash failed", okText: "Stashed the changes", err: git.Stash(cwd, msg), reload: true}
			})
		})}
}

func (m *Model) webGitStashPop(call *webCall) *webAsk {
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	return &webAsk{summary: "stash@{0}", title: "Pop the stash?",
		body:   "Bring back the newest stashed changes and drop them from the stash?",
		action: "Pop",
		run: webGitLaptopRun(func(m *Model) tea.Cmd {
			return m.gitRemoteOp("Popping the stash…", func(cwd string) gitRemoteDoneMsg {
				return gitRemoteDoneMsg{failTitle: "Stash pop failed", okText: "Popped the stash", err: git.StashPop(cwd), reload: true}
			})
		})}
}
