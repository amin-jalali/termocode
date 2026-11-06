//go:build browser

package web

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Git: Peek Change Under Cursor (GITX-21 on the web): a click on a git
// mark in the gutter (or the command, on the caret's line) opens a card
// with that change in colour. Close is first and has the focus; Stage
// (Unstage for a staged change) and Next Change act at once; Revert…
// asks here first, Cancel first, then the laptop.

const peekCard = `document.querySelector(".peek-card")`

// gitMarkAt is the page point of the git mark on a 1-based row.
func gitMarkAt(p *Page, line int) (float64, float64) {
	pt := Eval[[]float64](p, fmt.Sprintf(`(() => {
		const f = %s, d = f.contentDocument, fr = f.getBoundingClientRect();
		const n = [...d.querySelectorAll(".margin .line-numbers")].find((e) => e.textContent.trim() === "%d");
		const g = d.querySelector(".tc-gm").getBoundingClientRect(), r = n.getBoundingClientRect();
		return [fr.left + g.left + g.width / 2, fr.top + r.top + r.height / 2];
	})()`, deskFrame, line))
	return pt[0], pt[1]
}

// peekButtons are the card's buttons in order, and the focused one's text.
func peekButtons(p *Page) string {
	return Eval[string](p, `[...`+peekCard+`.querySelectorAll(".choices button")].map((b) => b.textContent).join() + "|" + (document.activeElement?.textContent ?? "")`)
}

func TestDeskGitPeek(t *testing.T) {
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("git", 1, webproto.GitSnap{Repo: true, Branch: webproto.GitBranch{Name: "main"},
		Files: []webproto.GitFile{{Path: "a.txt", X: " ", Y: "M", Unstaged: true}}}))
	openDesk(t, p, c, 2, "a.txt", "one", "TWO", "three", "new", "five")
	r := expectReq(t, c, "git.diff")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(webproto.GitDiff{Path: "a.txt", Rows: []webproto.DiffRow{
		{K: "hunk", Text: "@@ -1,5 +1,5 @@"},
		{K: "ctx", Text: "one", Old: 1, New: 1},
		{K: "del", Text: "two", Old: 2},
		{K: "add", Text: "TWO", New: 2},
		{K: "ctx", Text: "three", Old: 3, New: 3},
		{K: "add", Text: "new", New: 4},
		{K: "ctx", Text: "five", Old: 4, New: 5},
	}})})
	p.WaitJS(deskDoc + `.querySelector(".tc-gm-mod") && ` + deskDoc + `.querySelector(".tc-gm-add")`)
	first := webproto.GitPeek{Path: "a.txt", Line: 2, Header: "@@ -1,2 +1,2 @@", Lines: []string{" one", "-two", "+TWO"}, Index: 1, Count: 2, Starts: []int{2, 4}}
	second := webproto.GitPeek{Path: "a.txt", Line: 4, Header: "@@ -3,1 +3,2 @@", Lines: []string{" three", "+new"}, Index: 2, Count: 2, Starts: []int{2, 4}}

	// A click on the mark of row 2 peeks there.
	x, y := gitMarkAt(p, 2)
	p.Do(chromedp.MouseClickXY(x, y))
	reply(t, c, reqFor(t, c, webproto.OpGitPeek, `{"path":"a.txt","line":2}`), first)
	p.WaitJS(peekCard + `?.querySelector(".peek-title")?.textContent === "Change 1 of 2 · a.txt"`)
	p.WaitJS(peekCard + `.querySelector(".drow-del") && ` + peekCard + `.querySelector(".drow-add")`)
	if got := peekButtons(p); got != "Close,Stage,Revert…,Next Change|Close" {
		t.Errorf("buttons %q", got)
	}

	// Next Change goes to the next mark and peeks there.
	p.TapJS(ButtonByText("Next Change"))
	reply(t, c, reqFor(t, c, webproto.OpGitPeek, `{"path":"a.txt","line":4}`), second)
	p.WaitJS(peekCard + `?.querySelector(".peek-title")?.textContent === "Change 2 of 2 · a.txt"`)
	waitLine(p, "4")

	// Stage stages that hunk by its header and closes the card.
	p.TapJS(ButtonByText("Stage"))
	reply(t, c, reqFor(t, c, webproto.OpGitStageHunk, `{"path":"a.txt","hunk":"@@ -3,1 +3,2 @@"}`), nil)
	p.WaitJS(`!` + peekCard)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Staged the change in a.txt.")`)

	// The command peeks on the caret's line; a staged change offers
	// Unstage and no Revert.
	runGitCmd(t, p, c, "Git: Peek Change Under Cursor", true)
	staged := second
	staged.Staged, staged.Count, staged.Index = true, 1, 1
	reply(t, c, reqFor(t, c, webproto.OpGitPeek, `{"path":"a.txt","line":4}`), staged)
	p.WaitJS(peekCard + `?.textContent.includes("This change is staged.")`)
	if got := peekButtons(p); got != "Close,Unstage|Close" {
		t.Errorf("staged buttons %q", got)
	}
	p.Key("\x1b")
	p.WaitJS(`!` + peekCard)

	// Revert… asks here first, Cancel first and focused; nothing is sent.
	runGitCmd(t, p, c, "Git: Peek Change Under Cursor", false)
	reply(t, c, reqFor(t, c, webproto.OpGitPeek, `{"path":"a.txt","line":4}`), second)
	p.WaitJS(peekCard)
	p.TapJS(ButtonByText("Revert…"))
	p.WaitJS(`!` + peekCard + ` && document.querySelector(".ask")?.textContent.includes("Your edits in this change are lost.")`)
	p.WaitJS(`document.activeElement?.textContent === "Cancel"`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}
