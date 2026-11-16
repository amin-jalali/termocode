//go:build browser

package web

import (
	"strings"
	"testing"
	"time"
)

// Now fixes (owner's phone, 390x844): a row with no time shows no age
// (never "20773d"); a finished item that waits on nobody is not on Now,
// not even under All; with things on the user's turn a tap on ✦ (its
// count badge) opens Now and, on Now, the radial menu as before; Now
// adds nothing to a Home whose chat has messages, and the badge stays.

func nowFixInbox(now int64) map[string]any {
	return map[string]any{
		"items": []any{
			nowItem("card:c-1", "card", "needs_you", "checkout-fix asks to run tests", "me", "Waits for your OK", "agents", 1, now-12*60_000, "answer"),
			nowItem("ci:main", "ci", "failed", "CI · main", "me", "Your branch is red", "redci", 1, 0, "fix"),
			nowItem("run:c1/r5", "run", "done", "سلتم چطوری؟", "none", "Done", "", 4, now-60_000),
			nowItem("task:build", "task", "done", "build", "none", "Done", "", 4, now-2*60_000, "run"),
			nowItem("run:c2/r1", "run", "working", "docs-sync agent", "none", "Running", "", 4, now-4*60_000, "stop"),
		},
		"counts": map[string]any{"needs_you": 1, "working": 1, "failed": 1, "done": 2},
		"now":    nowInfo(true, 5),
	}
}

func TestNowPhoneFixes(t *testing.T) {
	now := time.Now().UnixMilli()
	p, c := openNow(t, nowFixInbox(now))
	p.WaitJS(nowHome + `?.querySelector('.now-row[data-id="ci:main"]')`)
	if a := Eval[string](p, nowHome+`.querySelector('.now-row[data-id="ci:main"] .now-age').textContent`); a != "" {
		t.Errorf("CI row without a time shows age %q", a)
	}
	if all := Eval[string](p, nowHome+`.querySelector(".now-all").textContent`); !strings.Contains(all, "See all 3") {
		t.Errorf("see all %q, want 3 (the 2 finished items are not on Now)", all)
	}
	shotTo(t, p, "nowfix-home")
	if b := Eval[string](p, `document.documentElement.style.getPropertyValue("--now-badge")`); b != `"2"` {
		t.Errorf("✦ badge %q, want \"2\"", b)
	}

	// A tap on ✦ with a count opens Now (no menu).
	p.WaitJS(`!!document.querySelector(".rm-fab")`)
	p.Tap(".rm-fab")
	p.WaitJS(`location.hash.startsWith("#/home/now") && !!document.querySelector(".now-page")`)
	if Eval[bool](p, `!!document.querySelector(".rm-overlay")`) {
		t.Error("the tap opened the menu too")
	}
	p.TapJS(`[...document.querySelectorAll(".now-page .now-tabs button")].find((b) => b.textContent.trim().startsWith("All"))`)
	p.WaitJS(`!!document.querySelector('.now-page .now-sec[data-sec="other"]')`)
	if tabs := Eval[[]string](p, `[...document.querySelectorAll(".now-page .now-tabs button")].map((b) => b.textContent.trim())`); strings.Join(tabs, "|") != "Your turn 2|Waiting 0|All 3" {
		t.Errorf("tabs %q", tabs)
	}
	for _, id := range []string{"run:c1/r5", "task:build"} {
		if Eval[bool](p, `!!document.querySelector('.now-page .now-row[data-id="`+id+`"]')`) {
			t.Errorf("finished %s is on Now", id)
		}
	}
	shotTo(t, p, "nowfix-all")

	// On Now, ✦ opens the radial menu as before.
	p.Tap(".rm-fab")
	p.WaitJS(`!!document.querySelector(".rm-overlay")`)
	shotTo(t, p, "nowfix-radial")
	Eval[bool](p, `(document.querySelector(".rm-overlay").dispatchEvent(new KeyboardEvent("keydown", {key: "Escape", bubbles: true})), true)`)
	p.WaitJS(`!document.querySelector(".rm-overlay")`)

	// A Home whose chat has messages: Now adds nothing; the badge stays.
	Eval[bool](p, `(location.hash = "#/home", true)`)
	c.Send(t, Snap("chat", 2, chatWith(false, userMsg(0, "سلتم چطوری؟"))))
	p.WaitJS(homeView + `?.querySelector(".msg")`)
	if Eval[bool](p, `!!`+homeView+`.querySelector(".now-home")`) {
		t.Error("Now shows on a Home with messages")
	}
	if b := Eval[string](p, `document.documentElement.style.getPropertyValue("--now-badge")`); b != `"2"` {
		t.Errorf("✦ badge %q on a Home with messages, want \"2\"", b)
	}
	shotTo(t, p, "nowfix-chat")
	p.noOverflow("now fixes")
}

// With nothing on the user's turn ✦ has no badge and opens the menu.
func TestNowFabNoBadge(t *testing.T) {
	now := time.Now().UnixMilli()
	p, _ := openNow(t, map[string]any{
		"items":  []any{nowItem("run:c2/r1", "run", "working", "docs-sync agent", "none", "Running", "", 4, now-4*60_000, "stop")},
		"counts": map[string]any{"working": 1},
		"now":    nowInfo(true, 5),
	})
	p.WaitJS(nowHome + `?.querySelector(".now-clear")`)
	if b := Eval[string](p, `document.documentElement.style.getPropertyValue("--now-badge")`); b != "" {
		t.Errorf("✦ badge %q with nothing on your turn", b)
	}
	p.WaitJS(`!!document.querySelector(".rm-fab")`)
	p.Tap(".rm-fab")
	p.WaitJS(`!!document.querySelector(".rm-overlay")`)
	if strings.HasPrefix(Eval[string](p, `location.hash`), "#/home/now") {
		t.Error("✦ without a count opened Now")
	}
}
