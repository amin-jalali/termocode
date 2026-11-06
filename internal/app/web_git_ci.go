package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Git: Fix CI with AI from the web (WEBP-9, Git): the laptop's ciFixWithAI
// without the laptop's chat. The first failed run's log is read through
// the laptop's gh off the UI thread; the answer is the laptop's own
// message (ghci.FixPrompt redacts the log), which the page puts in its
// chat input for the user to read and send.

func init() {
	webOps[webproto.OpCIFixPrompt] = (*Model).webCIFixPrompt
}

func (m *Model) webCIFixPrompt(call *webCall) tea.Cmd {
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	w := m.ciW()
	failed := w.snap.FailedRuns()
	if !w.loaded || w.snap.State() != ghci.StateFail || len(failed) == 0 {
		call.fail(webproto.CodeConflict, "CI is not red on "+webName(w.branch)+".")
		return nil
	}
	r, branch, run := failed[0], w.branch, w.runner()
	call.later()
	return func() tea.Msg {
		fl, err := ghci.FetchFailedLog(context.Background(), run, root, r.ID)
		if err != nil {
			call.fail(webproto.CodeInternal, "Could not read the CI log: "+webName(err.Error()))
			return nil
		}
		return applyMsg(func(m *Model) tea.Cmd {
			m.webAuditOp(call, branch+" run "+webName(r.Workflow), "ok")
			call.reply(webproto.CIFixPrompt{Prompt: ghci.FixPrompt(branch, r, fl)})
			return nil
		})
	}
}
