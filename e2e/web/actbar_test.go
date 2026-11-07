//go:build browser

package web

import (
	"strings"
	"testing"
)

// TapAct picks an activity bar item on the desktop: its icon, or, when
// the bar is too short and the item waits in the "…" menu, that menu's
// row (as on the laptop's editor).
func (p *Page) TapAct(id string) {
	p.T.Helper()
	sel := `nav.act-bar .act-item[data-act="` + id + `"]`
	p.WaitJS(`document.querySelector('` + sel + `')`)
	if !Eval[bool](p, `document.querySelector('`+sel+`').hidden`) {
		p.Tap(sel)
		return
	}
	p.Tap(`nav.act-bar .act-more`)
	p.Tap(`.ctx-menu .ctx-item[data-id="` + id + `"]`)
}

// A short screen: the activity bar never scrolls. The items that do not
// fit wait in a "…" menu at the end of the top group (as on the laptop's
// editor); a pick there opens the view and its item comes into the bar.
// A tall screen shows every item and no "…".
func TestWorkbenchActivityBarOverflow(t *testing.T) {
	p, _ := openFiles(t, Desktop)
	const bar = `document.querySelector("nav.act-bar")`
	p.WaitJS(`!!document.querySelector("nav.act-bar .act-more")`)
	if Eval[bool](p, bar+`.scrollHeight > `+bar+`.clientHeight`) {
		t.Error("the activity bar scrolls")
	}
	hiddenIDs := func() string {
		return Eval[string](p, `[...document.querySelectorAll("nav.act-bar .act-item[hidden]")].map((e) => e.dataset.act).join()`)
	}
	hidden := hiddenIDs()
	if hidden == "" || !strings.HasSuffix(hidden, "chat") || strings.Contains(hidden, "files") || strings.Contains(hidden, "device") || strings.Contains(hidden, "settings") {
		t.Errorf("items in the … menu %q", hidden)
	}
	// Every shown item is inside the bar.
	if !Eval[bool](p, `[...document.querySelectorAll("nav.act-bar .nav-item:not([hidden])")].every((e) => e.getBoundingClientRect().bottom <= `+bar+`.getBoundingClientRect().bottom + 0.5)`) {
		t.Error("an activity bar item is cut off")
	}
	if got := Eval[string](p, `document.querySelector("nav.act-bar .act-more").getAttribute("aria-label")`); got != "Additional Views" {
		t.Errorf("… label %q", got)
	}
	p.Tap(`nav.act-bar .act-more`)
	p.WaitJS(`!!document.querySelector(".ctx-menu .ctx-item")`)
	if got := Eval[string](p, `[...document.querySelectorAll(".ctx-menu .ctx-item")].map((e) => e.dataset.id).join()`); got != hidden {
		t.Errorf("… menu rows %q, want %q", got, hidden)
	}
	if got := Eval[string](p, `document.activeElement?.dataset.id ?? ""`); got != strings.Split(hidden, ",")[0] {
		t.Errorf("focus in the … menu on %q", got)
	}
	// A pick opens the view; the open view's item shows in the bar.
	p.Tap(`.ctx-menu .ctx-item[data-id="chat"]`)
	p.WaitJS(`location.hash === "#/chat" && !document.querySelector(".ctx-menu")`)
	p.WaitJS(`!document.querySelector('nav.act-bar .act-item[data-act="chat"]').hidden && !!document.querySelector('nav.act-bar .act-item[data-act="chat"][aria-current="page"]')`)
	if got := len(strings.Split(hiddenIDs(), ",")); got != len(strings.Split(hidden, ",")) {
		t.Errorf("%d items in the … menu after the pick, want %d", got, len(strings.Split(hidden, ",")))
	}
	if Eval[bool](p, bar+`.scrollHeight > `+bar+`.clientHeight`) {
		t.Error("the activity bar scrolls after the pick")
	}

	tall, _ := openFiles(t, Viewport{Name: "tall", W: 1280, H: 1100, Scale: 1})
	tall.WaitJS(`!!document.querySelector('nav.act-bar .act-item[data-act="chat"]')`)
	if Eval[bool](tall, `!!document.querySelector("nav.act-bar .act-more") || !!document.querySelector("nav.act-bar .act-item[hidden]")`) {
		t.Error("a tall screen has a … menu")
	}
}
