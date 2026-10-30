package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop page's merge editor (WEBP-5): a conflicted file's incoming
// and current sides and the merged result, as the laptop's merge view
// has them. The page only splices the texts the laptop gives it; saving
// the result goes through the laptop's own checks (git.MarkResolved).

func init() {
	webAskOps[webproto.OpGitResolveSide] = (*Model).webGitResolveSide
}

// webGitConflicted is the laptop's entry for a conflicted path inside
// the workspace (not one that may hold secrets).
func (m *Model) webGitConflicted(call *webCall, path string) (string, bool) {
	root, ok := m.webGitRoot(call)
	if !ok || !webGitInWorkspace(call, root, path) {
		return "", false
	}
	if ai.IsSecretFileName(path) {
		webFail(call, webErrSecret())
		return "", false
	}
	if f, ok := m.webGitFind(root, path); !ok || !webGitConflict(f.Code) {
		call.fail(webproto.CodeNotFound, "That file has no merge conflict now.")
		return "", false
	}
	return root, true
}

// webGitResolveSide keeps one side of the whole file after a laptop
// card; the laptop's merge code runs it (a side that deleted the file
// deletes it).
func (m *Model) webGitResolveSide(call *webCall) *webAsk {
	a := call.args.(*webproto.GitResolveSideArgs)
	if _, ok := m.webGitConflicted(call, a.Path); !ok {
		return nil
	}
	side := map[string]string{"ours": "current (ours)", "theirs": "incoming (theirs)"}[a.Side]
	return &webAsk{summary: a.Path + " " + a.Side, title: "Keep one side?",
		body:   "Settle the conflict in " + a.Path + " with the " + side + " file? Your other changes to it are lost. If that side deleted the file, it is deleted.",
		action: "Keep " + a.Side,
		run: func(m *Model) tea.Cmd {
			cwd, err := m.rootDir()
			if err != nil {
				return nil
			}
			path, side := a.Path, a.Side
			return func() tea.Msg {
				return mergeWholeDoneMsg{rel: path, verb: "keep-" + side, err: git.ResolveWithSide(cwd, path, side)}
			}
		}}
}
