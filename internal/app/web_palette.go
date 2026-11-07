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
// the stash: the same commands the git view asks the laptop for). The
// user's own AI commands are the one exception to the AI policy
// (webPaletteUserOnly).
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
	// The user's own AI commands (webPaletteUserOnly says why the web may).
	"ai-until-done":        true,
	"ai-plan-approval":     true,
	"ai-autopilot":         true,
	"ai-reasoning-next":    true,
	"ai-keep-going":        true,
	"ai-edit-review":       true,
	"ai-review-order":      true,
	"ai-worktrees-toggle":  true,
	"ai-critic-toggle":     true,
	"ai-memory-toggle":     true,
	"ai-undo-turn":         true,
	"ai-apply-code":        true,
	"ai-handoff":           true,
	"ai-ship":              true,
	"ai-plan-worktree":     true,
	"ai-mcp-reconnect-all": false,
	"ai-mcp-background":    false,
	"ai-mcp-reset-cache":   false,
	"ai-mcp-updates":       false,
}

// webPaletteUserOnly are the commands the AI itself may never run (its
// policy is never: they change the user's AI setup, spend the user's
// tokens, undo or apply the AI's work, or start work that pushes) but
// the user may run from the web: the web user is the user. Each says why
// that is safe; the ones marked true above wait for a laptop card, so a
// setting, a paid turn or a push changes only after the laptop's yes.
var webPaletteUserOnly = map[string]string{
	"ai-plan-approval":    "the user's own chat setting, after a laptop card",
	"ai-autopilot":        "the user's own approval setting, raised only after a laptop card",
	"ai-reasoning-next":   "the user's own chat setting (more thinking costs more), after a laptop card",
	"ai-keep-going":       "the user's own chat setting (extra paid turns), after a laptop card",
	"ai-edit-review":      "the user's own review setting, after a laptop card",
	"ai-review-order":     "the user's own review setting (the order of the files), after a laptop card",
	"ai-worktrees-toggle": "the user's own agent setting, after a laptop card",
	"ai-critic-toggle":    "the user's own agent setting (a critic review costs tokens), after a laptop card",
	"ai-memory-toggle":    "the user's own workspace setting, after a laptop card",
	"ai-undo-turn":        "the user undoes the AI's changes, after a laptop card (and the laptop's restore card)",
	"ai-apply-code":       "the user proposes the last code block as an edit to review, after a laptop card",
	"ai-handoff":          "the user's own handoff (one paid request), after a laptop card",
	"ai-ship":             "the user starts or stops shipping (push, pull request), after a laptop card",
	"ai-plan-worktree":    "the user arms or stops a plan run in a worktree, after a laptop card",
	"ai-mcp-background":   "moves a running MCP call to the background; nothing is lost",
	"ai-mcp-reset-cache":  "lists the MCP servers' tools again; changes no setting",
	"ai-mcp-updates":      "only checks the MCP catalog for newer versions; installs nothing",
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
		if _, form := webPaletteLaptopForms[it.ID]; form { // its form opens on the laptop (web_palette_forms.go)
			out = append(out, webproto.PaletteItem{ID: it.ID, Title: it.Title, Keys: it.Hint, Ask: true})
			continue
		}
		card, ok := webPaletteAllow[it.ID]
		if !ok {
			continue
		}
		p, known := aiActionPolicyFor(it.ID)
		if _, user := webPaletteUserOnly[it.ID]; !known || p.Gate == aiGateNever && !user {
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
	body := "Run “" + it.Title + "” on the laptop?"
	if what, form := webPaletteLaptopForms[it.ID]; form {
		body += "\n\nIt will " + what + ". " + webPaletteFormNote
	}
	return &webAsk{
		summary: it.ID,
		title:   "Run " + it.Title + "?",
		body:    body,
		action:  "Run",
		run:     func(m *Model) tea.Cmd { return webPaletteDispatch(m, it.ID) },
	}
}
