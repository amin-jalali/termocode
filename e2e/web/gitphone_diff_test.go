//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// A file's diff on the phone is a full page (page design 08): back, the
// file name with +N −N and ⋯; the file's header folds and has a round
// "viewed" tick; each part starts with its @@ line and its rows carry both
// line numbers; "Show next lines" reads on; a floating pill moves between
// the parts (n / N) and stages the file; a long press on a part gives
// Ask AI about this, Add to chat, Stage this part, Discard this part.
func TestGitPhoneDiffPage(t *testing.T) {
	p, c := openGit(t, Phone, gitSnap())
	p.TapJS(`document.querySelector('.gp-row[data-path="src/a.go"] .gp-open')`)
	h1, h2 := "@@ -1,2 +1,2 @@ func main() {", "@@ -10,1 +10,2 @@"
	reply(t, c, expectOp(t, c, webproto.OpGitDiff, `{"path":"src/a.go"}`), webproto.GitDiff{Path: "src/a.go", Added: 2, Removed: 1, Next: 7, Rows: []webproto.DiffRow{
		{K: "hunk", Text: h1, Old: 1, New: 1}, {K: "del", Text: "old <b>line</b>", Old: 1}, {K: "add", Text: "new line", New: 1}, {K: "ctx", Text: "same", Old: 2, New: 2},
		{K: "hunk", Text: h2, Old: 10, New: 10}, {K: "ctx", Text: "ten", Old: 10, New: 10}, {K: "add", Text: "eleven", New: 11}}})
	const page = `document.querySelector('[data-view="git-diff"]')`
	p.WaitJS(page + `?.querySelectorAll(".gp-hunk").length === 2`)
	if loc := Eval[string](p, "location.hash"); loc != "#/git/src/a.go" {
		t.Errorf("route %q", loc)
	}
	if n := Eval[int](p, `document.querySelectorAll(".sheet-layer").length`); n != 0 {
		t.Error("the diff is a sheet; it is a page")
	}
	if got := p.Text(`[data-view="git-diff"] .view-title`); got != "a.go" {
		t.Errorf("title %q", got)
	}
	if got := p.Text(".gp-counts"); got != "+2 −1" {
		t.Errorf("counts %q", got)
	}
	if got := p.Text(".gp-file-p"); got != "src/a.go" {
		t.Errorf("file header %q", got)
	}
	if got := texts(p, ".gp-hunk:first-child .gp-dl"); got != h1+"|1−old <b>line</b>|1+new line|22same" {
		t.Errorf("first part %q", got)
	}
	if n := Eval[int](p, `document.querySelectorAll(".gp-diff b").length`); n != 0 {
		t.Fatal("diff text became HTML")
	}
	if got := p.Text(".gp-fp-n"); got != "1 / 2" {
		t.Errorf("pill %q", got)
	}
	if got := p.Text(`[data-act="stage-file"]`); got != "Stage file" {
		t.Errorf("stage button %q", got)
	}
	p.TapJS(`document.querySelector('.gp-fpill [data-act="next"]')`)
	p.WaitJS(`document.querySelector(".gp-fp-n").textContent === "2 / 2"`)
	// Show next lines reads the next page.
	p.TapJS(`document.querySelector('[data-act="more-rows"]')`)
	reply(t, c, expectOp(t, c, webproto.OpGitDiff, `{"path":"src/a.go","from":7}`), webproto.GitDiff{Path: "src/a.go", From: 7, Rows: []webproto.DiffRow{{K: "ctx", Text: "tail", Old: 11, New: 12}}})
	p.WaitJS(`document.querySelectorAll(".gp-hunk:last-of-type .gp-dl").length === 4 && !document.querySelector('[data-act="more-rows"]')`)
	// A long press on a part.
	const menu = `document.querySelector(".gp-menu")`
	p.press(`document.querySelector(".gp-hunk")`, menu)
	if got := p.Text(".gp-menu-t"); got != "lines 1 −1 +1" {
		t.Errorf("part menu title %q", got)
	}
	if got := texts(p, ".gp-menu .gp-mi"); got != "Ask AI about this|Add to chat ↩|Stage this part|Discard this part" {
		t.Errorf("part menu %q", got)
	}
	p.TapJS(ButtonByText("Stage this part"))
	reply(t, c, expectOp(t, c, webproto.OpGitStageHunk, `{"path":"src/a.go","hunk":"`+h1+`"}`), nil)
	reply(t, c, expectOp(t, c, webproto.OpGitDiff, `{"path":"src/a.go"}`), webproto.GitDiff{Path: "src/a.go", Rows: []webproto.DiffRow{{K: "hunk", Text: h2, Old: 10, New: 10}, {K: "add", Text: "eleven", New: 11}}})
	p.WaitJS(`document.querySelectorAll(".gp-hunk").length === 1`)
	// The pill stages the file; ⋯ has the rest.
	p.TapJS(`document.querySelector('[data-act="stage-file"]')`)
	reply(t, c, expectOp(t, c, webproto.OpGitStage, `{"paths":["src/a.go"]}`), nil)
	p.TapJS(`document.querySelector('[data-act="file-more"]')`)
	p.WaitJS(menu)
	if got := texts(p, ".gp-menu .gp-mi"); got != "Ask AI about this|Add to chat ↩|Open file|Discard changes…" {
		t.Errorf("page menu %q", got)
	}
	p.TapJS(ButtonByText("Discard changes…"))
	p.WaitJS(`document.querySelector(".discard-text")?.textContent === "Discard changes in src/a.go? This cannot be undone."`)
	p.TapJS(`document.querySelector(".discard .choices button")`)
	// The viewed tick folds the file; back goes to the changes.
	Eval[bool](p, `(document.querySelector('[data-act="viewed"]').click(), true)`) // the Staged toast may lie over it
	p.WaitJS(`document.querySelector('[data-act="viewed"]').getAttribute("aria-checked") === "true" && !document.querySelector(".gp-diff")`)
	p.noOverflow("diff page")
	p.TapJS(`document.querySelector('[data-view="git-diff"] .head-back')`)
	p.WaitJS(`location.hash === "#/git" && document.querySelector('.gp-row[data-path="src/a.go"]')`)
}
