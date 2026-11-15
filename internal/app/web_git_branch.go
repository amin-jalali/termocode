package app

import (
	"slices"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"

	tea "github.com/charmbracelet/bubbletea"
)

// A new branch from the phone's Branches sheet (git.branch_create): the
// laptop's Git: Create Branch From… at HEAD, after a laptop card. Git on
// the laptop makes it and switches to it (git checkout -b); the changes in
// the work tree go with it, as they do there.

func init() {
	webAskOps[webproto.OpGitBranchCreate] = (*Model).webGitBranchCreate
}

func (m *Model) webGitBranchCreate(call *webCall) *webAsk {
	name := call.args.(*webproto.GitBranchCreateArgs).Name
	if _, ok := m.webGitRoot(call); !ok {
		return nil
	}
	from, ok := m.webGitBranchName(call)
	if !ok {
		return nil
	}
	if name == from || slices.Contains(m.web.more.branches, name) {
		call.fail(webproto.CodeConflict, "There is a branch "+name+" already.")
		return nil
	}
	body := "Make the branch " + name + " from " + from + " and switch to it?"
	if len(m.gitFiles) > 0 {
		body += " " + m.webGitChanged() + " go with you."
	}
	return &webAsk{summary: name, title: "New branch?", body: body, action: "Create",
		run: webGitLaptopRun(func(m *Model) tea.Cmd {
			return m.gitRemoteOp("Making the branch…", func(cwd string) gitRemoteDoneMsg {
				return gitRemoteDoneMsg{failTitle: "New branch failed", okText: "Switched to the new branch " + name, err: git.CreateBranch(cwd, name), reload: true}
			})
		})}
}
