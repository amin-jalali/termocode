//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// zenChrome is true while any part of the workbench around the editor shows.
const zenChrome = `!!(document.querySelector(".act-bar")?.offsetParent || document.querySelector(".wb-side") || document.querySelector(".statusbar")?.offsetParent || document.querySelector(".chatcol") || document.querySelector(".ed-tabs")?.offsetParent)`

// Spec (WEBP-1): Zen mode: Ctrl/Cmd+K then Z (pressed in the editor
// too, where the chord types nothing) hides everything but the editor:
// the activity bar, the side bar, the panel, the chat, the tabs and the
// status bar. Escape twice, or Ctrl/Cmd+K Z again, brings them back.
func TestDeskZenMode(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "one")
	if !Eval[bool](p, zenChrome) {
		t.Fatal("no workbench chrome before Zen")
	}
	deskFocus(p)
	p.Do(chromedp.KeyEvent("k", modKey()))
	p.Do(chromedp.KeyEvent("z"))
	p.WaitJS(`document.documentElement.dataset.zen === "on"`)
	p.WaitJS(`!(` + zenChrome + `)`)
	if !Eval[bool](p, deskDoc+`?.querySelector(".view-lines") !== null`) {
		t.Fatal("the editor went away in Zen")
	}
	noReq(t, c, 300*time.Millisecond) // the chord typed nothing
	if got := deskText(p); got != "one" {
		t.Fatalf("the chord typed into the file: %q", got)
	}
	// Escape twice leaves Zen, from the editor too.
	deskFocus(p)
	p.Do(chromedp.KeyEvent(kb.Escape))
	p.Do(chromedp.KeyEvent(kb.Escape))
	p.WaitJS(`document.documentElement.dataset.zen !== "on"`)
	p.WaitJS(zenChrome)
	if !Eval[bool](p, `!!document.querySelector(".wb-side")`) {
		t.Error("the side bar did not come back")
	}
	// The chord again from the page toggles it on and off.
	wbKey(p, "k", true, false)
	wbKey(p, "z", false, false)
	p.WaitJS(`document.documentElement.dataset.zen === "on"`)
	wbKey(p, "k", true, false)
	wbKey(p, "z", false, false)
	p.WaitJS(`document.documentElement.dataset.zen !== "on"`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}
