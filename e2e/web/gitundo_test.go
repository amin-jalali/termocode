//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Git: Undo Last Git Action… shows the laptop's plan in words, asks here
// (Cancel first), then the laptop.
func TestGitUndoAsks(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	runCmd(t, p, c, "Git: Undo Last Git Action…", true)
	reply(t, c, expectOp(t, c, "git.undo_plan", `{}`), map[string]any{"title": "Undo the last git action?", "text": "Last action: commit 'fix x'.\n\nHEAD (main) goes back to a1b2c3d 'first'.", "action": "Undo"})
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("HEAD (main) goes back to a1b2c3d") && document.activeElement === document.querySelector(".ask .choices button")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, "git.undo", `{}`), webproto.AskRef{Card: "card-8"})
	p.WaitJS(`document.querySelector(".waiting")`)
}

// Git: Redo Undone Git Action asks here, then the laptop.
func TestGitRedoAsks(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	runCmd(t, p, c, "Git: Redo Undone Git Action", true)
	p.WaitJS(`document.querySelector(".ask") && document.activeElement === document.querySelector(".ask .choices button")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, "git.redo", `{}`), webproto.AskRef{Card: "card-9"})
	p.WaitJS(`document.querySelector(".waiting")`)
}

// Git: Reflog… lists HEAD's moves; picking one shows the plan to move
// HEAD back there, then asks.
func TestGitReflogPick(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	runCmd(t, p, c, "Git: Reflog…", true)
	reply(t, c, waitReq(t, c, "git.reflog"), map[string]any{"entries": []map[string]any{
		{"hash": "c3c3c3c", "summary": "commit 'add c'", "when": 1700000300000},
		{"hash": "b2b2b2b", "summary": "commit 'add b'", "when": 1700000200000},
		{"hash": "a1a1a1a", "summary": "commit 'first'", "when": 1700000100000}}})
	p.WaitJS(`document.querySelectorAll(".quick-row").length === 3`)
	p.Key("first")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "commit 'first'"`)
	p.Key("\r")
	reply(t, c, expectOp(t, c, "git.undo_plan", `{"reflog":true,"index":2,"hash":"a1a1a1a"}`), map[string]any{"title": "Move HEAD back to here?", "text": "Reflog line: commit 'first'.", "action": "Move HEAD", "move": true})
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("Reflog line: commit 'first'.")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, "git.undo", `{"move":true}`), webproto.AskRef{Card: "card-10"})
	p.WaitJS(`document.querySelector(".waiting")`)
}
