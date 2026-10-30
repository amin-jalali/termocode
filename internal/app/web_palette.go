package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The laptop palette from the web (the desktop workbench's command list).
// Only the commands on webPaletteAllow are offered: ones that act on the
// workspace as a whole (save all, git refresh/fetch/stage all, tests,
// auto save), not the laptop's own screen (focus, panels, zen), not its
// active buffer (the page edits its own file), never a picker or prompt
// only the laptop can answer, never one that quits, restarts or leaves
// the IDE. The AI policy is checked on top: a command the AI may never
// run is never offered, and one it calls destructive always waits for a
// laptop card, like the ones marked true here (they push, pull or touch
// the stash: the same commands the git view asks the laptop for).
var webPaletteAllow = map[string]bool{
	"save-all":             false,
	"toggle-auto-save":     false,
	"git-refresh":          false,
	"git-fetch":            false,
	"git-stage-all":        false,
	"git-pull":             true,
	"git-push":             true,
	"git-sync":             true,
	"git-stash":            true,
	"git-stash-pop":        true,
	"run-tests":            false,
	"testing-run-all":      false,
	"testing-rerun-failed": false,
	"testing-stop":         false,
	"testing-refresh":      false,
	"run-doctor":           false,
}

// webPaletteDispatch runs a laptop palette command (a seam for tests).
var webPaletteDispatch = (*Model).dispatchPaletteAction

func init() {
	webOps[webproto.OpPaletteList] = (*Model).webPaletteList
	webOps[webproto.OpPaletteRun] = (*Model).webPaletteRun
	webAskOps[webproto.OpPaletteAsk] = (*Model).webPaletteAsk
}

// webPaletteEntries are the commands the web may offer, in the laptop
// palette's order, with the live keymap's hints.
func (m *Model) webPaletteEntries() []webproto.PaletteItem {
	var out []webproto.PaletteItem
	for _, it := range withKeyHints(paletteItems(), m.keys) {
		card, ok := webPaletteAllow[it.ID]
		if !ok {
			continue
		}
		p, known := aiActionPolicyFor(it.ID)
		if !known || p.Gate == aiGateNever {
			continue
		}
		if p.Gate == aiGateConfirm && p.Class == "" {
			card = true
		}
		out = append(out, webproto.PaletteItem{ID: it.ID, Title: it.Title, Keys: it.Hint, Ask: card})
	}
	return out
}

// webPaletteFind returns the entry of id, if the web may offer it.
func (m *Model) webPaletteFind(id string) (webproto.PaletteItem, bool) {
	for _, it := range m.webPaletteEntries() {
		if it.ID == id {
			return it, true
		}
	}
	return webproto.PaletteItem{}, false
}

func (m *Model) webPaletteList(call *webCall) tea.Cmd {
	call.reply(webproto.PaletteList{Items: m.webPaletteEntries()})
	return nil
}

// webPaletteRun runs an offered command that needs no laptop card.
func (m *Model) webPaletteRun(call *webCall) tea.Cmd {
	it, ok := m.webPaletteFind(call.args.(*webproto.PaletteArgs).ID)
	if !ok || it.Ask {
		call.fail(webproto.CodeForbidden, "This command cannot run from the web.")
		return nil
	}
	call.reply(nil)
	return webPaletteDispatch(m, it.ID)
}

// webPaletteAsk asks the laptop to run an offered risky command.
func (m *Model) webPaletteAsk(call *webCall) *webAsk {
	it, ok := m.webPaletteFind(call.args.(*webproto.PaletteArgs).ID)
	if !ok || !it.Ask {
		call.fail(webproto.CodeForbidden, "This command does not need the laptop.")
		return nil
	}
	return &webAsk{
		summary: it.ID,
		title:   "Run " + it.Title + "?",
		body:    "Run “" + it.Title + "” on the laptop?",
		action:  "Run",
		run:     func(m *Model) tea.Cmd { return webPaletteDispatch(m, it.ID) },
	}
}
