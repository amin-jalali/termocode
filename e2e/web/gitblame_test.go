//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Line blame on the desktop editor: in a git repository the saved file's
// blame is asked once; the cursor's line shows its author, age and
// subject at its end; Git: Toggle Line Blame turns it off.
func TestDeskEditorLineBlame(t *testing.T) {
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("git", 1, gitSnap()))
	openBufAs(t, p, c, 2, "b.go", nil, "package b", "var x = 2")
	r := waitReq(t, c, "git.blame")
	if canonJSON(t, r.Args) != `{"path":"b.go"}` {
		t.Errorf("blame args %s", r.Args)
	}
	reply(t, c, r, webproto.GitBlame{Path: "b.go", Commits: []webproto.GitBlameCommit{
		{Hash: "1a2b3c4", Author: "Ann", Subject: "first", When: time.Now().Add(-21 * 24 * time.Hour).UnixMilli()}, {Uncommitted: true}}, Lines: []int{0, 1}})
	p.WaitJS(deskDoc + `?.querySelector(".tc-blame")`)
	if got := Eval[string](p, deskDoc+`.querySelector(".tc-blame").textContent.replace(/\u00a0/g, " ")`); !strings.Contains(got, "Ann, 3 weeks ago • first") {
		t.Errorf("blame of line 1: %q", got)
	}
	// Hovering the note shows the commit.
	at := Eval[struct{ X, Y float64 }](p, `(() => {
		const f = `+deskFrame+`, fr = f.getBoundingClientRect(), b = f.contentDocument.querySelector(".tc-blame").getBoundingClientRect();
		return { X: fr.left + b.left + b.width / 2, Y: fr.top + b.top + b.height / 2 };
	})()`)
	p.Do(chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, at.X-2, at.Y), chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, at.X, at.Y))
	answerReqs(t, c, map[string]any{"lsp.hover": nil})
	p.WaitJS(deskDoc + `.querySelector(".monaco-hover")?.textContent.includes("1a2b3c4")`)
	if !Eval[bool](p, deskDoc+`.querySelector(".monaco-hover").textContent.includes("Ann")`) {
		t.Error("the hover does not name the author")
	}
	// The palette's Git: Toggle Line Blame turns it off.
	wbKey(p, "P", true, true)
	answerReqs(t, c, map[string]any{"palette.list": webproto.PaletteList{Items: []webproto.PaletteItem{}}})
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("toggle line blame")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Git: Toggle Line Blame"`)
	p.Key("\r")
	p.WaitJS(`!` + deskDoc + `.querySelector(".tc-blame")`)
}
