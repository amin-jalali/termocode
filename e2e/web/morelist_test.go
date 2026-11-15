//go:build browser

package web

import (
	"strings"
	"testing"
)

// The More list (RM-3) on a phone and a tablet: a search box on top,
// grouped rows with a small header and an icon each, the recent views
// first, no lone "Home" row; every row opens its view.
func TestMoreListGroupsSearchRecent(t *testing.T) {
	for _, vp := range []Viewport{Phone, Tablet} {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
			p.Login("c-morelist")
			p.GoTab("more")
			p.WaitJS(`document.querySelector(".more-find") && document.querySelectorAll(".more-group").length >= 4`)
			if n := Eval[int](p, `document.querySelectorAll('.more-link[data-sub="home"]').length`); n != 0 {
				t.Error("the lone Home row is still there")
			}
			heads := Eval[[]string](p, `[...document.querySelectorAll(".more-group .group-title")].map((h) => h.textContent)`)
			if len(heads) < 4 {
				t.Errorf("group headers %q", heads)
			}
			bare := Eval[[]string](p, `[...document.querySelectorAll(".more-group .more-link")].filter((b) => !b.querySelector("svg.more-ic")).map((b) => b.dataset.sub)`)
			if len(bare) > 0 {
				t.Errorf("rows without an icon: %q", bare)
			}
			// the search box filters the rows
			p.Tap(".more-find")
			p.Key("kube")
			p.WaitJS(`[...document.querySelectorAll(".more-list .more-link")].map((b) => b.dataset.sub).join() === "kube"`)
			Eval[int](p, `(() => { const i = document.querySelector(".more-find"); i.value = ""; i.dispatchEvent(new Event("input", {bubbles: true})); return 1; })()`)
			p.WaitJS(`document.querySelectorAll(".more-group").length >= 4`)
			// a visited view comes first, under Pinned (until the user pins his own)
			p.Tap(`.more-link[data-sub="containers"]`)
			p.WaitJS(`location.hash === "#/more/containers"`)
			Eval[bool](p, `(history.back(), true)`) // back to the list
			p.WaitJS(`document.querySelector(".more-group.is-pinned .more-link")?.dataset.sub === "containers"`)
			if got := Eval[string](p, `document.querySelector(".more-group.is-pinned .group-title").textContent`); !strings.Contains(got, "Pinned") {
				t.Errorf("pinned header %q", got)
			}
			p.noOverflow("more")
		})
	}
}

// The More page as the design sheet 09 draws it on a touch phone: the
// search field with its glass, grouped cards of rows with a colour icon
// tile each, Pinned first with a ⋯ that opens Edit Pinned; a long press
// on a row pins it or asks the AI about it (the chat gets the view's name
// as a draft, nothing is sent); a pinned row subscribes its topic and
// shows its live count (1 failed in red); Edit Pinned removes and keeps
// the order on this device.
func TestMorePinnedCardsCountsEdit(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-morepins")
	c.Send(t, Snap("chat", 1, chatWith(false)))
	p.GoTab("more")
	p.WaitJS(`document.querySelectorAll(".more-group").length >= 4`)
	if !Eval[bool](p, `!!document.querySelector(".more-findbox svg") && !!document.querySelector(".more-findbox .more-find")`) {
		t.Error("the search field has no glass icon")
	}
	if bare := Eval[[]string](p, `[...document.querySelectorAll(".more-group .more-link")].filter((b) => !b.querySelector(".more-tile svg")).map((b) => b.dataset.sub)`); len(bare) > 0 {
		t.Errorf("rows without a colour tile: %q", bare)
	}
	if n := Eval[int](p, `document.querySelectorAll(".more-group .more-links.more-card").length`); n < 4 {
		t.Errorf("%d grouped cards, want one per group", n)
	}
	// no pins and nothing visited: no Pinned group
	if Eval[bool](p, `!!document.querySelector(".more-group.is-pinned")`) {
		t.Error("an empty Pinned group shows")
	}
	// a long press on a row opens its menu
	hold := func(sub string) {
		Eval[bool](p, `document.querySelector('.more-group:not(.is-pinned) .more-link[data-sub="`+sub+`"]').dispatchEvent(new MouseEvent("contextmenu", {bubbles: true, cancelable: true}))`)
		p.WaitJS(`document.querySelector(".more-menu")`)
	}
	pin := `[...document.querySelectorAll(".more-menu button")].find((b) => b.textContent === "Pin")`
	hold("testing")
	if got := Eval[[]string](p, `[...document.querySelectorAll(".more-menu button")].map((b) => b.textContent.trim())`); strings.Join(got, "|") != "✦ Ask AI about this|Pin" {
		t.Errorf("row menu %q", got)
	}
	p.TapJS(pin)
	p.WaitJS(`document.querySelector(".more-group.is-pinned .more-link")?.dataset.sub === "testing"`)
	hold("db")
	p.TapJS(pin)
	p.WaitJS(`document.querySelectorAll(".more-group.is-pinned .more-link").length === 2`)
	if first := Eval[string](p, `document.querySelector(".more-group")?.className`); !strings.Contains(first, "is-pinned") {
		t.Errorf("Pinned is not the first group: %q", first)
	}
	// the pinned Testing row asks for its topic and shows the failures
	c.WaitSub(t, "tests")
	c.Send(t, Snap("tests", 1, testsTree("idle", 1)))
	p.WaitJS(`document.querySelector('.is-pinned .more-link[data-sub="testing"] .more-n.is-bad')?.textContent === "1 failed"`)
	// ⋯ on Pinned: Edit Pinned, ✕ removes Databases, Done keeps it
	p.Tap(".more-group.is-pinned .more-gmenu")
	p.WaitJS(`document.querySelector(".more-sheet")?.textContent.includes("Edit Pinned")`)
	if n := Eval[int](p, `document.querySelectorAll(".more-sheet .more-edit-row .more-grip").length`); n != 2 {
		t.Errorf("%d rows with a drag grip, want 2", n)
	}
	p.TapJS(`document.querySelector('.more-sheet .more-edit-row[data-sub="db"] .more-x')`)
	p.WaitJS(`document.querySelectorAll(".more-sheet .more-edit-row").length === 1`)
	p.TapJS(`[...document.querySelectorAll(".more-sheet button")].find((b) => b.textContent === "Done")`)
	p.WaitJS(`!document.querySelector(".more-sheet") && document.querySelectorAll(".more-group.is-pinned .more-link").length === 1`)
	if got := Eval[string](p, `localStorage.getItem("termocode.more.pins")`); got != `["testing"]` {
		t.Errorf("pins kept as %q", got)
	}
	// Ask AI about this: the chat gets a draft that names the view
	hold("races")
	p.TapJS(`[...document.querySelectorAll(".more-menu button")].find((b) => b.textContent.includes("Ask AI"))`)
	p.WaitJS(`location.hash.startsWith("#/chat")`)
	p.WaitJS(`document.querySelector(".composer-box")?.value.includes("Races")`)
	p.noOverflow("more")
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("page problems: %q", probs)
	}
}
