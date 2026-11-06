package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// search.replace (WEBP-9): the laptop's Replace in Workspace from the web.
// The matches are counted for the laptop card; nothing is written before
// the laptop's yes.

func init() {
	webAskOps[webproto.OpSearchReplace] = (*Model).webSearchReplace
}

func (m *Model) webSearchReplace(call *webCall) *webAsk {
	a := call.args.(*webproto.SearchReplaceArgs)
	counts, err := search.CountLiteral(workspaceRoots(), a.Find)
	if err != nil {
		call.fail(webproto.CodeInternal, "The workspace could not be searched.")
		return nil
	}
	if len(counts) == 0 {
		call.fail(webproto.CodeNotFound, "No matches in the workspace.")
		return nil
	}
	find, repl := a.Find, a.Replace
	return &webAsk{
		summary: "replace in workspace",
		title:   "Replace in Workspace?",
		body:    replaceConfirmMessage(find, repl, counts, m.dirtyBufferCount(counts)),
		action:  "Replace",
		run: func(m *Model) tea.Cmd {
			m.replaceFind, m.replaceRepl, m.replaceTargets = find, repl, counts
			return m.applyReplaceInWorkspace()
		},
	}
}
