//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Line staging in the working tree's diff editor: Stage Selected Lines
// stages the caret's row at once; Discard Selected Lines and Git:
// Discard Hunk (at cursor) ask here (Cancel first), then the laptop.
func TestGitLinesInDiffEditor(t *testing.T) {
	p, c := openDeskSCMAt(t, Wide, nil)
	p.TapJS(scmRow("changes", "src/a.go"))
	old := []string{"package a", "", "func A() int {", "\treturn 1", "}", "", "// one", "// two", "// three", "// four", "// five", "// six", "// seven", "// eight", "// end"}
	cur := append([]string(nil), old...)
	cur[3] = "\treturn 2"
	answerGitText(t, c, "src/a.go", map[string]string{"index": strings.Join(old, "\n") + "\n"}, 2, cur)
	p.WaitJS(diffDoc + `?.querySelector(".monaco-diff-editor.side-by-side .editor.modified .view-line")`)
	Eval[bool](p, `(`+diffView+`.querySelector(".desk-editor").focus(), `+diffDoc+`.querySelector(".editor.modified .native-edit-context, .editor.modified textarea").focus(), true)`)
	p.Do(chromedp.KeyEvent(kb.F7))
	waitLine(p, "4")

	p.TapJS(`document.querySelector('.ed-actions [data-act="stage-lines"]')`)
	r := waitReq(t, c, "git.stage_lines")
	if got := canonJSON(t, r.Args); got != `{"from":4,"path":"src/a.go","to":4}` {
		t.Errorf("stage_lines args %s", got)
	}
	reply(t, c, r, nil)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Staged line 4")`)

	// Discard asks here first: Cancel first and focused.
	p.TapJS(`document.querySelector('.ed-actions [data-act="discard-lines"]')`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("line 4 of src/a.go") && document.activeElement === document.querySelector(".ask .choices button")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	r = waitReq(t, c, "git.discard_lines")
	if got := canonJSON(t, r.Args); got != `{"from":4,"path":"src/a.go","to":4}` {
		t.Errorf("discard_lines args %s", got)
	}
	reply(t, c, r, webproto.AskRef{Card: "card-4"})
	p.WaitJS(`document.querySelector(".waiting")`)
}

// Git: Discard Hunk (at cursor) names the change by its diff header.
func TestGitDiscardHunkCommand(t *testing.T) {
	p, c := openDeskSCMAt(t, Wide, nil)
	p.TapJS(scmRow("changes", "src/a.go"))
	old := []string{"package a", "", "func A() int {", "\treturn 1", "}"}
	cur := append([]string(nil), old...)
	cur[3] = "\treturn 2"
	answerGitText(t, c, "src/a.go", map[string]string{"index": strings.Join(old, "\n") + "\n"}, 2, cur)
	p.WaitJS(diffDoc + `?.querySelector(".monaco-diff-editor.side-by-side .editor.modified .view-line")`)
	Eval[bool](p, `(`+diffView+`.querySelector(".desk-editor").focus(), `+diffDoc+`.querySelector(".editor.modified .native-edit-context, .editor.modified textarea").focus(), true)`)
	p.Do(chromedp.KeyEvent(kb.F7))
	waitLine(p, "4")
	runGitCmd(t, p, c, "Git: Discard Hunk (at cursor)", true)
	r := waitReq(t, c, "git.diff")
	reply(t, c, r, webproto.GitDiff{Path: "src/a.go", Rows: []webproto.DiffRow{
		{K: "hunk", Text: "@@ -1,5 +1,5 @@", Old: 1, New: 1},
		{K: "ctx", Text: "package a", Old: 1, New: 1},
		{K: "ctx", Text: "", Old: 2, New: 2},
		{K: "ctx", Text: "func A() int {", Old: 3, New: 3},
		{K: "del", Text: "\treturn 1", Old: 4},
		{K: "add", Text: "\treturn 2", New: 4},
		{K: "ctx", Text: "}", Old: 5, New: 5},
	}})
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("src/a.go") && document.activeElement === document.querySelector(".ask .choices button")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	r = waitReq(t, c, "git.discard_hunk")
	if got := canonJSON(t, r.Args); got != `{"hunk":"@@ -1,5 +1,5 @@","path":"src/a.go"}` {
		t.Errorf("discard_hunk args %s", got)
	}
	reply(t, c, r, webproto.AskRef{Card: "card-5"})
	p.WaitJS(`document.querySelector(".waiting")`)
}
