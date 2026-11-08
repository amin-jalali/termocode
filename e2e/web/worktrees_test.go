//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// WORKTREES (WTX-16) on the desktop workbench against the fake hub.

func worktreesSnap() webproto.WorktreesSnap {
	return webproto.WorktreesSnap{State: "ready", Worktrees: []webproto.WorktreeItem{
		{ID: "a1b2c3d4e5f6", Title: "feat/wt-sidebar", Branch: "feat/wt-sidebar", Where: "~/src/app.worktrees/wt-sidebar", Kind: "user", Group: "pinned",
			State: "working", Pinned: true, Agent: "Claude Code", Agents: 1, Status: true, Ahead: 3, Dirty: 4, Note: "adding the pin row"},
		{ID: "0f0e0d0c0b0a", Title: "main", Branch: "main", Where: "~/src/app", Kind: "main", Group: "active", State: "idle", Here: true, Status: true},
		{ID: "112233445566", Title: "fix/toast-race", Branch: "fix/toast-race", Where: "~/src/app.worktrees/toast", Kind: "user", Group: "active", State: "idle", Status: true, Dirty: 2},
		{ID: "665544332211", Title: "feat/old-ui", Branch: "feat/old-ui", Where: "~/src/app.worktrees/old-ui", Kind: "user", Group: "archived", State: "archived", Gone: true},
	}}
}

func wtRow(id string) string { return `document.querySelector('.tr[data-id="` + id + `"]')` }

// wtAct presses a row's hover button.
func wtAct(p *Page, id, act string) {
	p.T.Helper()
	p.WaitJS(wtRow(id) + `?.querySelector('[data-act="` + act + `"]')`)
	Eval[bool](p, wtRow(id)+`.querySelector('[data-act="`+act+`"]').click() ?? true`)
}

// The view groups the worktrees like the laptop's list (each row with its
// branch, ahead and changed files); the filter narrows it here; Pin sends
// the id (never a path); Sleep asks here first (Cancel focused) and then
// asks the laptop; Note sends one line; Archive and Delete go through a
// laptop card; the main checkout offers none of them.
func TestDeskWorktrees(t *testing.T) {
	p, c := openFiles(t, Desktop)
	p.TapJS(`document.querySelector('nav.act-bar .act-item[data-act="worktrees"]')`)
	c.WaitSub(t, "worktrees")
	c.Send(t, Snap("worktrees", 1, worktreesSnap()))
	p.WaitJS(`document.querySelectorAll('.wb-side [data-view="worktrees"] .tr').length === 4`)
	if got := Eval[int](p, `document.querySelectorAll('.wb-side [data-view="worktrees"] .ps').length`); got != 3 {
		t.Errorf("%d groups, want 3 (pinned, active, archived)", got)
	}
	if got := Eval[string](p, wtRow("a1b2c3d4e5f6")+`.querySelector(".tr-desc").textContent`); got != "feat/wt-sidebar · 3↑ · 4 dirty" {
		t.Errorf("desc %q", got)
	}
	if Eval[bool](p, `!!`+wtRow("0f0e0d0c0b0a")+`.querySelector('[data-act="archive"], [data-act="delete"], [data-act="sleep"]')`) {
		t.Error("the main checkout offers archive, delete or sleep")
	}

	// The filter is the page's.
	Eval[bool](p, `(() => { const i = document.querySelector(".wt-filter input"); i.value = "toast"; i.dispatchEvent(new Event("input", {bubbles: true})); return true })()`)
	p.WaitJS(`document.querySelectorAll('.wb-side [data-view="worktrees"] .tr').length === 1`)
	noReq(t, c, 200*time.Millisecond)
	Eval[bool](p, `(() => { const i = document.querySelector(".wt-filter input"); i.value = ""; i.dispatchEvent(new Event("input", {bubbles: true})); return true })()`)
	p.WaitJS(`document.querySelectorAll('.wb-side [data-view="worktrees"] .tr').length === 4`)

	wtAct(p, "112233445566", "pin")
	reply(t, c, expectOp(t, c, webproto.OpWorktreesPin, `{"id":"112233445566","on":true}`), webproto.WorktreeDone{})

	wtAct(p, "112233445566", "sleep")
	p.WaitJS(`document.activeElement?.textContent === "Cancel"`)
	noReq(t, c, 200*time.Millisecond)
	p.TapJS(`document.querySelector('.wb-side .pv-ask .pv-btn:not(.is-2)')`)
	reply(t, c, expectOp(t, c, webproto.OpWorktreesSleep, `{"id":"112233445566","on":true}`), webproto.WorktreeDone{Asked: true})
	p.WaitJS(`document.body.textContent.includes("A card asks first")`)

	wtAct(p, "112233445566", "note")
	p.WaitJS(`document.activeElement?.closest(".wt-note")`)
	Eval[bool](p, `(() => { const i = document.activeElement; i.value = "tests pass"; i.dispatchEvent(new Event("input", {bubbles: true})); return true })()`)
	p.TapJS(`document.querySelector('.wt-note .pv-btn:not(.is-2)')`)
	reply(t, c, expectOp(t, c, webproto.OpWorktreesNote, `{"id":"112233445566","text":"tests pass"}`), webproto.WorktreeDone{})

	wtAct(p, "112233445566", "delete")
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("fix/toast-race")`)
	noReq(t, c, 300*time.Millisecond)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, webproto.OpWorktreesDelete, `{"id":"112233445566"}`), webproto.AskRef{Card: "a-1"})

	// The archived row restores through the laptop too.
	wtAct(p, "665544332211", "archive")
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("from its branch")`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}
