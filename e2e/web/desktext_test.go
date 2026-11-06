//go:build browser

package web

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop editor's line and text commands, as the laptop's editor
// has them (WEBP-9, Editor and files): each runs from the command list
// on the editor's own text, as one step of the user's own undo, and the
// change goes to the laptop like typing.

// runCmd opens the command list, types q and runs the row named label.
// first: the page asks the laptop for its commands (the first open).
func runCmd(t *testing.T, p *Page, c *HubConn, first bool, q, label string) {
	t.Helper()
	wbKey(p, "P", true, true)
	if first {
		answerReqs(t, c, map[string]any{"palette.list": webproto.PaletteList{Items: []webproto.PaletteItem{}}})
	}
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key(q)
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === ` + jsq(label))
	p.Key("\r")
	p.WaitJS(`!document.querySelector(".quick")`)
}

// waitDeskText waits until the desktop editor shows lines.
func waitDeskText(t *testing.T, p *Page, lines ...string) {
	t.Helper()
	want := strings.Join(lines, "\n")
	end := time.Now().Add(wait)
	for {
		got := deskText(p)
		if got == want {
			return
		}
		if time.Now().After(end) {
			t.Fatalf("the editor shows %q, want %q", got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// openDeskTabs opens path on the desktop and waits for its rows (a tab
// shows as spaces, so the text is not compared).
func openDeskTabs(t *testing.T, p *Page, c *HubConn, buf int, path string, lines ...string) {
	t.Helper()
	openBufAs(t, p, c, buf, path, nil, lines...)
	p.WaitJS(fmt.Sprintf(`%s?.querySelectorAll(".view-lines .view-line").length === %d`, deskDoc, len(lines)))
}

// deskRow puts the caret at the start of 1-based row n.
func deskRow(p *Page, n int) {
	p.T.Helper()
	deskFocus(p)
	p.Do(deskTextEnd(false))
	for i := 1; i < n; i++ {
		p.Do(chromedp.KeyEvent(kb.ArrowDown))
	}
	p.Do(chromedp.KeyEvent(kb.Home))
}

func TestDeskEditorLineCommands(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "c  ", "", "", "a", "b")
	runCmd(t, p, c, true, "trim trailing", "Edit: Trim Trailing Whitespace")
	waitDeskText(t, p, "c", "", "", "a", "b")
	runCmd(t, p, c, false, "collapse blank", "Edit: Collapse Blank Lines")
	waitDeskText(t, p, "c", "", "a", "b")
	openDesk(t, p, c, 3, "s.go", "b", "c", "a")
	runCmd(t, p, c, false, "sort lines asc", "Edit: Sort Lines Ascending")
	waitDeskText(t, p, "a", "b", "c")
	runCmd(t, p, c, false, "sort lines desc", "Edit: Sort Lines Descending")
	waitDeskText(t, p, "c", "b", "a")
	runCmd(t, p, c, false, "reverse lines", "Edit: Reverse Lines")
	waitDeskText(t, p, "a", "b", "c")

	deskRow(p, 1)
	runCmd(t, p, c, false, "duplicate line", "Edit: Duplicate Line")
	waitDeskText(t, p, "a", "a", "b", "c")
	runCmd(t, p, c, false, "uppercase", "Edit: Convert to UPPERCASE")
	waitDeskText(t, p, "a", "A", "b", "c")
	runCmd(t, p, c, false, "lowercase", "Edit: Convert to lowercase")
	waitDeskText(t, p, "a", "a", "b", "c")
	// A Go file comments with //; again takes it back.
	runCmd(t, p, c, false, "toggle line comment", "Edit: Toggle Line Comment")
	waitDeskText(t, p, "a", "// a", "b", "c")
	runCmd(t, p, c, false, "toggle line comment", "Edit: Toggle Line Comment")
	waitDeskText(t, p, "a", "a", "b", "c")
	runCmd(t, p, c, false, "join lines", "Edit: Join Lines")
	waitDeskText(t, p, "a", "a b", "c")

	// Each command is one step of the user's own undo.
	deskFocus(p)
	p.Do(chromedp.KeyEvent("z", modKey()))
	waitDeskText(t, p, "a", "a", "b", "c")

	// Replace in File opens the editor's find with its replace field.
	runCmd(t, p, c, false, "replace in file", "Edit: Replace in File")
	p.WaitJS(deskDoc + `.querySelector(".find-widget.visible.replaceToggled")`)
}

func TestDeskEditorFoldCommands(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDeskTabs(t, p, c, 2, "f.go", "func a() {", "\tx()", "}", "func b() {", "\ty()", "}")
	rows := func() int {
		return Eval[int](p, deskDoc+`.querySelectorAll(".view-lines .view-line").length`)
	}
	runCmd(t, p, c, true, "fold all", "View: Fold All")
	p.WaitJS(deskDoc + `.querySelectorAll(".view-lines .view-line").length === 4`)
	runCmd(t, p, c, false, "unfold all", "View: Unfold All")
	p.WaitJS(deskDoc + `.querySelectorAll(".view-lines .view-line").length === 6`)
	deskRow(p, 1)
	runCmd(t, p, c, false, "toggle fold", "View: Toggle Fold")
	p.WaitJS(deskDoc + `.querySelectorAll(".view-lines .view-line").length === 5`)
	runCmd(t, p, c, false, "toggle fold", "View: Toggle Fold")
	p.WaitJS(deskDoc + `.querySelectorAll(".view-lines .view-line").length === 6`)
	if n := rows(); n != 6 {
		t.Errorf("%d rows after the fold opened again", n)
	}
}

// Convert Tabs to Spaces and back change the rows' indentation at the
// file's tab width; the laptop gets the new rows.
func TestDeskEditorIndentCommands(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDeskTabs(t, p, c, 2, "t.go", "\tx", "\t\ty")
	runCmd(t, p, c, true, "tabs to spaces", "Edit: Convert Tabs to Spaces")
	r, a := expectSetText(t, c)
	if got := strings.Join(a.Lines, "|"); !strings.Contains(got, "    x") || strings.Contains(got, "\t") {
		t.Fatalf("tabs to spaces sent %q", got)
	}
	reply(t, c, r, map[string]any{"tick": 4, "dirty": true})
	runCmd(t, p, c, false, "spaces to tabs", "Edit: Convert Spaces to Tabs")
	_, a = expectSetText(t, c)
	if got := strings.Join(a.Lines, "|"); !strings.Contains(got, "\tx") || strings.Contains(got, "    ") {
		t.Fatalf("spaces to tabs sent %q", got)
	}
}
