package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The explorer's TIMELINE on the desktop page: the commits that changed
// the open file (git.file_log), each one shown as that file's part of
// the commit (git.show with a path). Reads only; git runs off the UI
// thread. The path must be inside the workspace, links followed.

func init() {
	webOps[webproto.OpGitFileLog] = (*Model).webGitFileLog
}

// webGitInWorkspace answers bad_request when path leads out of root.
func webGitInWorkspace(call *webCall, root, path string) bool {
	if _, err := (ai.Workspace{Root: root}).Resolve(path); err != nil {
		call.fail(webproto.CodeBadRequest, "That path is outside the workspace.")
		return false
	}
	return true
}

func (m *Model) webGitFileLog(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitFileLogArgs)
	root, ok := m.webGitRoot(call)
	if !ok || !webGitInWorkspace(call, root, a.Path) {
		return nil
	}
	call.later()
	return func() tea.Msg {
		cs, err := git.FileCommits(root, a.Path, webproto.MaxFileLog)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		out := webproto.GitFileLog{Path: a.Path, Log: []webproto.GitCommit{}}
		for _, c := range cs {
			out.Log = append(out.Log, webproto.GitCommit{Hash: c.Hash, Subject: webName(c.Subject), Author: webName(c.Author), When: c.When * 1000})
		}
		call.reply(out)
		return nil
	}
}
