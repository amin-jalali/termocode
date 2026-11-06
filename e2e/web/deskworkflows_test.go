//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Workflows on the desktop (WFX, plan §5.7): an activity bar item opens
// the side bar's list of workflows and their runs; a run opens as an
// editor tab with its counts, the last lines and the write card that
// waits for it, inline, Deny first; Allow once arms first. Stop asks
// inside the tab, Cancel first and focused. Run, on/off, edit and "Allow
// for this workflow" stay on the laptop.
func wfSnap() webproto.WorkflowsSnap {
	return webproto.WorkflowsSnap{
		Workflows: []webproto.WorkflowItem{
			{Name: "weekday-bugs", Title: "Weekday bug fixer", State: "running", When: "weekdays 09:00 Europe/Berlin", Last: "running"},
			{Name: "pr-review", State: "off"},
			{Name: "bad", State: "broken"},
		},
		Runs: []webproto.WorkflowRun{
			{ID: "20261106-090000", Workflow: "weekday-bugs", State: "running", Started: 1_762_419_600_000, Items: 3, Failed: 1, Wrote: 1,
				Waiting: "state", Tail: []string{"fix LIN-1: tests ok", "fix LIN-2: agent works"}},
			{ID: "20261105-090000", Workflow: "weekday-bugs", State: "done", Started: 1_762_333_200_000, Ended: 1_762_333_800_000, Items: 2, Wrote: 2},
		},
	}
}

func wfCard(id string) map[string]any {
	return map[string]any{"id": id, "kind": "workflow", "title": "weekday-bugs wants to write",
		"body": "Step state · item LIN-1\nlinear · update_issue\n\n\"Allow for this workflow\" is on the laptop only.", "since": 1700000000000,
		"choices": []any{
			map[string]any{"id": "deny", "label": "Deny", "safe": true},
			map[string]any{"id": "allow", "label": "Allow once"},
		}}
}

func TestDeskWorkflows(t *testing.T) {
	p, c := deskLogin(t, nil)
	p.TapJS(`document.querySelector('nav.act-bar .act-item[data-act="workflows"]')`)
	c.WaitSub(t, "workflows")
	c.Send(t, Snap("workflows", 1, wfSnap()))
	const side = `document.querySelector('.wb-side [data-view="workflows"]')`
	p.WaitJS(side + `?.querySelectorAll('[data-section="list"] .tr[data-workflow]').length === 3`)
	if got := Eval[string](p, side+`.querySelector('.tr[data-workflow="weekday-bugs"]').textContent`); !strings.Contains(got, "weekday-bugs") || !strings.Contains(got, "weekdays 09:00") {
		t.Errorf("workflow row %q", got)
	}
	if !Eval[bool](p, side+`.querySelector('.tr[data-workflow="bad"] .cod-error') && !!`+side+`.querySelector('.tr[data-workflow="pr-review"] .cod-circle-outline')`) {
		t.Error("the state icons are missing (✗ broken, ○ off)")
	}
	if got := Eval[string](p, `[...`+side+`.querySelectorAll('[data-section="runs"] .tr[data-run]')].map((r) => r.dataset.run).join(",")`); got != "20261106-090000,20261105-090000" {
		t.Errorf("runs %q", got)
	}
	if !Eval[bool](p, side+`.querySelector('.tr[data-run="20261106-090000"] .cod-warning')`) {
		t.Error("the run that waits for a write is not marked")
	}
	if got := Eval[string](p, `document.querySelector('nav.act-bar .act-item[data-act="workflows"] .act-badge')?.textContent ?? ""`); got != "1" {
		t.Errorf("activity badge %q, want 1 (a run waits for you)", got)
	}
	if n := Eval[int](p, side+`.querySelectorAll('[data-act="run"], [data-act="enable"], [data-act="edit"]').length`); n != 0 {
		t.Errorf("%d laptop-only controls in the side bar", n)
	}

	p.TapJS(side + `.querySelector('.tr[data-run="20261106-090000"]')`)
	const tab = `document.querySelector('[data-view="workflow-run"]')`
	p.WaitJS(tab + `?.querySelector("h1")?.textContent === "weekday-bugs"`)
	if got := Eval[string](p, tab+`.textContent`); !strings.Contains(got, "3 items") || !strings.Contains(got, "fix LIN-2: agent works") || !strings.Contains(got, "state") {
		t.Errorf("run tab %q", got)
	}
	if got := Eval[string](p, `document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent`); !strings.Contains(got, "weekday-bugs") {
		t.Errorf("tab name %q", got)
	}

	// The write card, inline: Deny first; Allow once arms before it acts.
	c.Send(t, Snap("cards", 1, cardsData(wfCard("card-7"))))
	card := tab + `.querySelector(".card-workflow")`
	p.WaitJS(card)
	if got := Eval[string](p, `[...`+card+`.querySelectorAll(".choices .btn")].map((b) => b.textContent.trim()).join("|")`); !strings.HasPrefix(got, "Deny|Allow once") {
		t.Errorf("card buttons %q, want Deny first", got)
	}
	if Eval[bool](p, `!!`+card+`.textContent.match(/always allow/i)`) {
		t.Error("the web offers an always allow")
	}
	p.TapJS(card + `.querySelector(".choices .btn")`)
	r := expectReq(t, c, "cards.answer")
	if string(r.Args) != `{"id":"card-7","choice":"deny"}` {
		t.Errorf("answer %s", r.Args)
	}
	reply(t, c, r, nil)
	c.Send(t, Delta("cards", 2, map[string]any{"op": "remove", "id": "card-7", "by": ""}))

	// Stop asks first: Cancel is first and focused and sends nothing.
	p.TapJS(tab + `.querySelector('[data-act="stop"]')`)
	p.WaitJS(tab + `.querySelector(".pv-ask") && document.activeElement?.textContent === "Cancel"`)
	p.TapJS(tab + `.querySelector(".pv-ask .pv-btn.is-2")`)
	p.WaitJS(`!` + tab + `.querySelector(".pv-ask")`)
	noReq(t, c, 300*time.Millisecond)
	p.TapJS(tab + `.querySelector('[data-act="stop"]')`)
	p.WaitJS(tab + `.querySelector(".pv-ask .pv-btn.is-danger")`)
	p.TapJS(tab + `.querySelector(".pv-ask .pv-btn.is-danger")`)
	r = expectReq(t, c, "workflows.stop")
	if string(r.Args) != `{"run":"20261106-090000"}` {
		t.Errorf("stop %s", r.Args)
	}
	reply(t, c, r, nil)

	// A finished run has no Stop; the file's verbs are on the laptop.
	p.TapJS(side + `.querySelector('.tr[data-run="20261105-090000"]')`)
	p.WaitJS(tab + `?.dataset.run === "20261105-090000"`)
	if Eval[bool](p, `!!`+tab+`.querySelector('[data-act="stop"]')`) {
		t.Error("a finished run offers Stop")
	}
}
