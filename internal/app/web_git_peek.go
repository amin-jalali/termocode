package app

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The gutter peek from the desktop editor (GITX-21 on the web): git.peek
// answers the change under a line of the saved file, as the laptop's
// peek card (gitx_peek.go) reads it. A file with unsaved edits on the
// laptop is refused, as the laptop's card refuses it: the change is read
// from the saved file.

func init() {
	webOps[webproto.OpGitPeek] = (*Model).webGitPeek
}

func (m *Model) webGitPeek(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitPeekArgs)
	root, ok := m.webGitRoot(call)
	if !ok || !webGitInWorkspace(call, root, a.Path) {
		return nil
	}
	if ai.IsSecretFileName(a.Path) {
		webFail(call, webErrSecret())
		return nil
	}
	if m.webGitUnsaved(a.Path) {
		call.fail(webproto.CodeConflict, "Save the file first: the change is read from the saved file.")
		return nil
	}
	path, line := a.Path, a.Line
	call.later()
	return func() tea.Msg {
		p, err := git.PeekHunk(root, path, line)
		switch {
		case errors.Is(err, git.ErrNoHunk):
			call.fail(webproto.CodeNotFound, "No change on this line.")
			return nil
		case err != nil:
			webGitFail(call, err)
			return nil
		}
		call.reply(webproto.GitPeek{Path: path, Line: line, Header: p.Header, Lines: p.Lines, Index: p.Index, Count: p.Count, Staged: p.Staged, Starts: p.Starts})
		return nil
	}
}
