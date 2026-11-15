//go:build browser

package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Home = Now (phone, 390x844): when the laptop's inbox carries the Now
// info, the Home shows one summary line ("3 need you · 1 running"), the
// first-run rules card once, "Your turn" (the user's 3/5/10 items, each
// with ▶, its age, a why chip, a dashed rule tag and ✦ Fix on a red CI),
// "See all N ›", and what runs. A tap opens "Why this?" with the item's
// one fix, Done, Later and Not important; each step sends now.act and
// shows a toast with Undo. Nothing acts by itself. With nothing on the
// user's turn the Home says "All clear." with the other two lists one
// tap away.

func nowRules() []map[string]any {
	return []map[string]any{
		{"id": "agents", "name": "Agents", "sentence": "An agent waits for my OK", "signal": "agent", "level": 1, "on": true, "count": 1},
		{"id": "redci", "name": "Red CI", "sentence": "CI fails on my branch", "signal": "ci_red", "level": 1, "on": true, "count": 1},
		{"id": "reviews", "name": "Reviews", "sentence": "Someone asks me to review", "signal": "review", "level": 2, "on": true, "count": 1},
		{"id": "due", "name": "Due today", "sentence": "Tracker task due today or late", "signal": "due", "level": 2, "on": true, "count": 0},
		{"id": "nobots", "name": "No bots", "sentence": "Bots never take my turn", "level": 0, "on": true, "count": 0},
		{"id": "mentions", "name": "Mentions", "sentence": "Someone @-mentions me", "signal": "mention", "level": 3, "on": false, "count": 0},
	}
}

func nowInfo(setup bool, show int) map[string]any {
	return map[string]any{"setup": setup, "show": show, "rules": nowRules()}
}

// nowItem is an inbox item with its Now fields.
func nowItem(id, kind, state, title, turn, why, rule string, level int, since int64, can ...string) map[string]any {
	it := map[string]any{"id": id, "kind": kind, "ref": strings.SplitN(id, ":", 2)[1], "title": title, "state": state, "since": since,
		"turn": turn, "why": why, "why_at": since, "level": level}
	if rule != "" {
		it["rule"] = rule
	}
	if len(can) > 0 {
		it["can"] = can
	}
	return it
}

func nowInbox(now int64, setup bool) map[string]any {
	return map[string]any{
		"items": []any{
			nowItem("card:c-1", "card", "needs_you", "checkout-fix asks to run tests", "me", "Waits for your OK", "agents", 1, now-12*60_000, "answer"),
			nowItem("ci:feat/retry-webhook", "ci", "failed", "CI · feat/retry-webhook", "me", "Your branch is red", "redci", 1, now-40*60_000, "fix"),
			nowItem("lane:0123456789ab", "lane", "done", "#412 Add retry to webhook", "me", "Done · review it", "reviews", 2, now-24*3600_000),
			nowItem("task:lint", "task", "failed", "lint", "me", "Task failed", "", 4, now-5*60_000, "run"),
			nowItem("run:r-2", "run", "working", "docs-sync agent", "none", "Running", "", 4, now-4*60_000, "stop"),
			nowItem("lane:aaaaaaaaaaaa", "lane", "waiting", "migrate-db", "others", "Waits on Sara", "", 4, now-3*3600_000),
		},
		"counts": map[string]any{"needs_you": 1, "working": 1, "failed": 2, "done": 1},
		"now":    nowInfo(setup, 3),
	}
}

// openNow logs in on the phone, opens the Home and sends the inbox.
func openNow(t *testing.T, inbox map[string]any) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	c.Send(t, Snap("chat", 1, chatWith(false)))
	c.Send(t, Snap("status", 2, statusData("main", 0)))
	Eval[bool](p, `(location.hash = "#/home", true)`)
	c.WaitSub(t, "inbox")
	c.Send(t, Snap("inbox", 1, inbox))
	if r := nextReq(t, c); r.Op != "view.list" {
		t.Fatalf("page sent %s; want view.list", show(r))
	} else {
		c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
	}
	return p, c
}

// nowReq waits for the page's next request of op and answers it.
func nowReq(t *testing.T, c *HubConn, op string, data any) map[string]any {
	t.Helper()
	for {
		r := nextReq(t, c)
		if r.Op != op {
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
			continue
		}
		var args map[string]any
		if err := json.Unmarshal(r.Args, &args); err != nil {
			t.Fatal(err)
		}
		c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(data)})
		return args
	}
}

const nowHome = homeView + `?.querySelector(".now-home")`

func TestNowHomeYourTurn(t *testing.T) {
	now := time.Now().UnixMilli()
	p, c := openNow(t, nowInbox(now, true))
	p.WaitJS(nowHome + `?.querySelector(".now-row")`)
	if sum := Eval[string](p, nowHome+`.querySelector(".now-sum").textContent`); !strings.Contains(sum, "4 need you · 1 running") {
		t.Errorf("summary line %q", sum)
	}
	if Eval[bool](p, `!!`+homeView+`.querySelector(".home-today .home-sec")`) {
		t.Error("the old My work card is still on a Home that is Now")
	}
	type row struct{ ID, Why, Rule, Age string }
	rows := Eval[[]row](p, `[...`+nowHome+`.querySelectorAll('.now-sec[data-sec="turn"] .now-row')].map((r) => ({ID: r.dataset.id, Why: r.querySelector(".now-why")?.textContent ?? "", Rule: r.querySelector(".now-rule")?.textContent ?? "", Age: r.querySelector(".now-age")?.textContent ?? ""}))`)
	// Show 3: P1 oldest first, then P2.
	want := []row{
		{"ci:feat/retry-webhook", "Your branch is red", "rule: Red CI", "40m"},
		{"card:c-1", "Waits for your OK", "rule: Agents", "12m"},
		{"lane:0123456789ab", "Done · review it", "rule: Reviews", "1d"},
	}
	if len(rows) != len(want) {
		t.Fatalf("your turn rows %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d is %+v, want %+v", i, rows[i], want[i])
		}
	}
	if all := Eval[string](p, nowHome+`.querySelector(".now-all").textContent`); !strings.Contains(all, "See all 6") {
		t.Errorf("see all %q", all)
	}
	if fix := Eval[int](p, nowHome+`.querySelectorAll(".now-row .now-fix").length`); fix != 1 {
		t.Errorf("%d ✦ Fix buttons, want 1 (the red CI)", fix)
	}
	if run := Eval[string](p, nowHome+`.querySelector('.now-sec[data-sec="running"]')?.textContent ?? ""`); !strings.Contains(run, "docs-sync agent") {
		t.Errorf("running section %q", run)
	}
	shotTo(t, p, "now-home")

	// A tap opens Why this?, with Done; Done sends now.act and a toast
	// with Undo; Undo sends now.act undo.
	p.TapJS(nowHome + `.querySelector('.now-row[data-id="card:c-1"] .now-open')`)
	p.WaitJS(nowHome + `.querySelector(".now-whyp")?.textContent.includes("Why this?")`)
	shotTo(t, p, "now-why")
	p.TapJS(`[...` + nowHome + `.querySelectorAll(".now-whyp button")].find((b) => b.textContent.trim() === "Done")`)
	if a := nowReq(t, c, "now.act", map[string]any{"id": "card:c-1"}); a["do"] != "done" || a["id"] != "card:c-1" {
		t.Fatalf("done sent %v", a)
	}
	p.WaitJS(`[...document.querySelectorAll(".toast")].some((x) => x.textContent.includes("Done: checkout-fix"))`)
	p.TapJS(`[...document.querySelectorAll(".toast button")].find((b) => b.textContent.trim() === "Undo")`)
	if a := nowReq(t, c, "now.act", map[string]any{"id": "card:c-1"}); a["do"] != "undo" {
		t.Fatalf("undo sent %v", a)
	}
	p.noOverflow("now home")
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("page problems: %q", probs)
	}
}

func TestNowHomeFirstRunCard(t *testing.T) {
	now := time.Now().UnixMilli()
	p, c := openNow(t, nowInbox(now, false))
	p.WaitJS(nowHome + `?.querySelector(".now-setup")`)
	txt := Eval[string](p, nowHome+`.querySelector(".now-setup").textContent`)
	for _, w := range []string{"Set up Now", "On top: agents waiting, red CI, reviews", "Use these", "Change"} {
		if !strings.Contains(txt, w) {
			t.Errorf("setup card lacks %q: %q", w, txt)
		}
	}
	shotTo(t, p, "now-setup")
	p.TapJS(`[...` + nowHome + `.querySelectorAll(".now-setup button")].find((b) => b.textContent.trim() === "Use these")`)
	a := nowReq(t, c, "now.rules", nowInfo(true, 5))
	setup, _ := a["setup"].(map[string]any)
	if setup == nil || setup["show"] != float64(5) || setup["hide_bots"] != true {
		t.Fatalf("use these sent %v", a)
	}
}

func TestNowHomeAllClear(t *testing.T) {
	now := time.Now().UnixMilli()
	p, _ := openNow(t, map[string]any{
		"items": []any{
			nowItem("run:r-2", "run", "working", "docs-sync agent", "none", "Running", "", 4, now-4*60_000, "stop"),
			nowItem("lane:aaaaaaaaaaaa", "lane", "waiting", "migrate-db", "others", "Waits on Sara", "", 4, now-3*3600_000),
		},
		"counts": map[string]any{"working": 1},
		"now":    nowInfo(true, 5),
	})
	p.WaitJS(nowHome + `?.querySelector(".now-clear")`)
	txt := Eval[string](p, nowHome+`.querySelector(".now-clear").textContent`)
	for _, w := range []string{"All clear.", "Nothing waits on you.", "Waiting on others 1", "Everything else 1"} {
		if !strings.Contains(txt, w) {
			t.Errorf("all clear lacks %q: %q", w, txt)
		}
	}
	shotTo(t, p, "now-all-clear")
}

func TestNowSeeAllAndLater(t *testing.T) {
	now := time.Now().UnixMilli()
	p, c := openNow(t, nowInbox(now, true))
	p.WaitJS(nowHome + `?.querySelector(".now-all")`)
	p.TapJS(nowHome + `.querySelector(".now-all")`)
	p.WaitJS(`document.querySelector(".now-page .now-tabs")`)
	tabs := Eval[[]string](p, `[...document.querySelectorAll(".now-page .now-tabs button")].map((b) => b.textContent.trim())`)
	if strings.Join(tabs, "|") != "Your turn 4|Waiting 1|All 6" {
		t.Errorf("tabs %q", tabs)
	}
	if n := Eval[int](p, `document.querySelectorAll('.now-page .now-sec[data-sec="turn"] .now-row').length`); n != 4 {
		t.Errorf("See all shows %d of your 4 items", n)
	}
	shotTo(t, p, "now-see-all")
	p.TapJS(`document.querySelector('.now-page .now-row[data-id="task:lint"] .now-open')`)
	p.TapJS(`[...document.querySelectorAll(".now-page .now-whyp button")].find((b) => b.textContent.trim() === "Later")`)
	p.WaitJS(`document.querySelector(".now-later input")`)
	shotTo(t, p, "now-later")
	Eval[bool](p, `(() => { const i = document.querySelector(".now-later input"); i.value = "in 2 hours"; i.dispatchEvent(new Event("input", {bubbles: true})); return true })()`)
	p.TapJS(`[...document.querySelectorAll(".now-later button")].find((b) => b.textContent.trim() === "Later")`)
	if a := nowReq(t, c, "now.act", map[string]any{"id": "task:lint", "until": now + 7_200_000}); a["do"] != "later" || a["when"] != "in 2 hours" || a["id"] != "task:lint" {
		t.Fatalf("later sent %v", a)
	}
	p.WaitJS(`[...document.querySelectorAll(".toast")].some((x) => x.textContent.includes("Later: lint"))`)
	// Back goes to the Home.
	p.TapJS(`document.querySelector(".now-page .now-back")`)
	p.WaitJS(`!document.querySelector(".now-page")`)
}
