package app

import (
	"bytes"
	"unicode/utf8"

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
	webOps[webproto.OpGitText] = (*Model).webGitText
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

// webGitText answers one file as git keeps it (the diff editor's older
// side). A file that may hold secrets stays on the laptop, as in buf.open.
func (m *Model) webGitText(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitTextArgs)
	root, ok := m.webGitRoot(call)
	if !ok || !webGitInWorkspace(call, root, a.Path) {
		return nil
	}
	if ai.IsSecretFileName(a.Path) {
		webFail(call, webErrSecret())
		return nil
	}
	call.later()
	return func() tea.Msg {
		text, found, cut, err := git.FileAt(root, a.Rev, a.Path, webproto.MaxGitText)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		out := webproto.GitText{Path: a.Path, Rev: a.Rev, Missing: !found, Truncated: cut}
		head := text[:min(len(text), 8000)]
		if bytes.IndexByte([]byte(head), 0) >= 0 || !utf8.ValidString(text) {
			out.Binary = true
		} else {
			out.Text = text
		}
		call.reply(out)
		return nil
	}
}
