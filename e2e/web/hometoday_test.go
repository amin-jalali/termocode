//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The phone's Home before its first message, as the design sheet 32
// draws it: one summary line that names what matters today, "My work"
// as one grouped card (a row per non-empty state: its colour tile, the
// newest title under it, its count), "Shortcuts" with live counts and
// the chip strip moved into it (the box's foot keeps only the box); a
// long press on a row offers it to the AI. A quiet day is one line and
// a few chips that only fill the box.

// openToday logs in on the phone with an empty chat, the inbox and
// dirty files given, opens the Home and answers its saved views.
func openToday(t *testing.T, inbox []map[string]any, dirty int) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	c.Send(t, Snap("chat", 1, chatWith(false)))
	c.Send(t, Snap("status", 2, statusData("main", dirty)))
	items := make([]any, len(inbox))
	for i, x := range inbox {
		items[i] = x
	}
	Eval[bool](p, `(location.hash = "#/home", true)`)
	c.WaitSub(t, "inbox")
	c.Send(t, Snap("inbox", 1, map[string]any{"items": items}))
	if r := nextReq(t, c); r.Op != "view.list" {
		t.Fatalf("page sent %s; want view.list", show(r))
	} else {
		c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
	}
	return p, c
}

func inboxItem(id, state, title string, since int64) map[string]any {
	return map[string]any{"id": id, "kind": "run", "ref": id, "title": title, "state": state, "since": since}
}

func TestHomeTodayMyWorkAndShortcuts(t *testing.T) {
	now := time.Now().UnixMilli()
	p, _ := openToday(t, []map[string]any{
		inboxItem("r1", "needs_you", "Add checkout endpoint asks to run go mod tidy", now),
		inboxItem("r2", "working", "Fix TestDiscount", now),
		inboxItem("r3", "done", "Checkout endpoint", now),
		inboxItem("r4", "done", "Cart totals", now),
	}, 3)
	p.WaitJS(homeView + `?.querySelector(".home-today .home-sec")`)
	hello := Eval[string](p, homeView+`.querySelector(".home-today .home-hello").textContent`)
	if !strings.Contains(hello, "Here is your laptop today.") || !strings.Contains(hello, "1 waits for you · 1 running · 3 changed files") {
		t.Errorf("summary line %q", hello)
	}
	type row struct{ Title, Meta, End string }
	rows := Eval[[]row](p, `[...`+homeView+`.querySelectorAll('.home-sec[aria-label="My work"] .home-row')].map((r) => ({Title: r.querySelector(".ph-row-title").textContent, Meta: r.querySelector(".ph-row-meta")?.textContent ?? "", End: r.querySelector(".ph-row-end").textContent}))`)
	want := []row{{"Needs you", "Add checkout endpoint asks to run go mod tidy", "1"}, {"Working", "Fix TestDiscount", "1"}, {"Done today", "Checkout endpoint", "2"}}
	if len(rows) != len(want) {
		t.Fatalf("My work rows %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("My work row %d is %+v, want %+v", i, rows[i], want[i])
		}
	}
	if bare := Eval[int](p, `[...`+homeView+`.querySelectorAll(".home-today .home-row")].filter((r) => !r.querySelector(".home-tile svg")).length`); bare > 0 {
		t.Errorf("%d rows without a colour tile", bare)
	}
	// Shortcuts: Git changes with its count, then the chip strip.
	git := Eval[string](p, `[...`+homeView+`.querySelectorAll('.home-sec[aria-label="Shortcuts"] .home-row')].find((r) => r.textContent.includes("Git changes"))?.querySelector(".ph-row-end").textContent ?? ""`)
	if git != "3" {
		t.Errorf("Git changes shows %q, want 3", git)
	}
	if !Eval[bool](p, `!!`+homeView+`.querySelector('.home-sec[aria-label="Shortcuts"] .home-chips .home-chip')`) {
		t.Error("the chip strip is not under Shortcuts")
	}
	if Eval[bool](p, `!!`+homeView+`.querySelector(".home-foot .home-chips")`) {
		t.Error("the box's foot still carries the chip strip on an empty Home")
	}
	// A long press on a row offers it to the AI (the action sheet).
	Eval[bool](p, homeView+`.querySelectorAll('.home-sec[aria-label="My work"] .home-row')[1].dispatchEvent(new MouseEvent("contextmenu", {bubbles: true, cancelable: true}))`)
	p.WaitJS(`[...document.querySelectorAll(".sheet button")].some((b) => b.textContent.includes("Add to message"))`)
	p.noOverflow("home")
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("page problems: %q", probs)
	}
}

func TestHomeTodayQuietDay(t *testing.T) {
	p, _ := openToday(t, nil, 3)
	p.WaitJS(homeView + `?.querySelector(".home-quiet")`)
	if got := Eval[string](p, homeView+`.querySelector(".home-quiet").textContent`); !strings.Contains(got, "All quiet.") || !strings.Contains(got, "Nothing waits, nothing runs.") {
		t.Errorf("quiet day %q", got)
	}
	if Eval[bool](p, `!!`+homeView+`.querySelector(".home-today .home-sec")`) {
		t.Error("a quiet day still shows its empty sections")
	}
	chips := Eval[[]string](p, `[...`+homeView+`.querySelectorAll(".home-quiet .chip")].map((c) => c.textContent.trim())`)
	if strings.Join(chips, "|") != "Review my 3 changes|Start a task" {
		t.Errorf("quiet chips %q", chips)
	}
	// A chip fills the box; nothing is sent.
	p.TapJS(`[...` + homeView + `.querySelectorAll(".home-quiet .chip")].find((c) => c.textContent.includes("Review"))`)
	p.WaitJS(homeView + `.querySelector(".home-foot textarea")?.value.includes("Review")`)
	p.noOverflow("home quiet")
}
