//go:build browser

package web

import (
	"testing"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// runCmd runs a workbench command by its name from the command list.
func runCmd(t *testing.T, p *Page, c *HubConn, first bool, search, label string) {
	t.Helper()
	wbKey(p, "P", true, true)
	if first {
		reply(t, c, expectOp(t, c, "palette.list", `{}`), webproto.PaletteList{})
	}
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key(search)
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === ` + jsq(label))
	realKeys(p, "\r") // one keydown, as a keyboard sends it
	p.WaitJS(`!document.querySelector(".quick")`)
}

// deskCursors counts the editor's carets.
const deskCursors = deskDoc + `.querySelectorAll(".cursors-layer .cursor").length`

// Spec (WEBP-1): multi-cursor editing has its commands in the list, with
// the editor's own keys: Selection: Add Cursor Below / Above, Add
// Selection to Next Find Match and Select All Occurrences. Typing then
// edits at every caret, through the laptop's buffer like one edit.
func TestDeskMultiCursorCommands(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "foo", "foo", "bar")
	deskFocus(p)
	p.Do(deskTextEnd(false))
	runCmd(t, p, c, true, "add cursor below", "Selection: Add Cursor Below")
	p.WaitJS(deskCursors + ` === 2`)
	v := &vimRig{t: t, p: p, c: c, tick: 3}
	v.keys("X", "Xfoo", "Xfoo", "bar")

	// One caret again, then the same word twice by Next Find Match.
	deskFocus(p)
	realKeys(p, string(kb.Escape))
	p.Do(deskTextEnd(false))
	p.WaitJS(deskCursors + ` === 1`)
	runCmd(t, p, c, false, "next find match", "Selection: Add Selection to Next Find Match")
	runCmd(t, p, c, false, "next find match", "Selection: Add Selection to Next Find Match")
	p.WaitJS(deskCursors + ` === 2`)
	v.keys("Y", "Y", "Y", "bar")

	// All occurrences of the word under the caret at once.
	deskFocus(p)
	p.Do(deskTextEnd(false))
	runCmd(t, p, c, false, "select all occurrences", "Selection: Select All Occurrences")
	p.WaitJS(deskCursors + ` === 2`)
	v.keys("Z", "Z", "Z", "bar")

	// Add Cursor Above, from the last row.
	deskFocus(p)
	p.Do(deskTextEnd(true))
	runCmd(t, p, c, false, "add cursor above", "Selection: Add Cursor Above")
	p.WaitJS(deskCursors + ` === 2`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}
