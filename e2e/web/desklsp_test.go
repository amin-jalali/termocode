//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop editor's language features: the laptop's language servers
// through the hub (the diagnostics topic), never one in the browser.
// Problems show as squiggles, in the PROBLEMS panel and as counts in the
// status bar.

func diagFixture() webproto.DiagSnap {
	return webproto.DiagSnap{Errors: 1, Warnings: 1, Files: []webproto.DiagFile{{Path: "a.go", Items: []webproto.Diag{
		{Sev: 1, Line: 1, Col: 2, EndLine: 1, EndCol: 5, Message: "undefined: foo", Source: "compiler", Code: "UndeclaredName"},
		{Sev: 2, Line: 0, Col: 0, EndLine: 0, EndCol: 7, Message: "missing package doc"},
	}}}}
}

// The file's diagnostics are squiggles in the editor; the status bar
// counts them, a click there opens the PROBLEMS panel grouped by file,
// and a row puts the caret on its place.
func TestDeskProblems(t *testing.T) {
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("diagnostics", 1, diagFixture()))
	p.WaitJS(`[...document.querySelectorAll(".statusbar .sb-problems .sb-n")].map((x) => x.textContent).join(",") === "1,1"`)
	openDesk(t, p, c, 2, "a.go", "package a", "x foo()")
	p.WaitJS(deskDoc + `.querySelector(".squiggly-error") && ` + deskDoc + `.querySelector(".squiggly-warning")`)

	p.TapJS(`document.querySelector(".statusbar .sb-problems")`)
	p.WaitJS(`document.querySelectorAll(".wb-panel .prob-row").length === 2`)
	if got := Eval[string](p, `document.querySelector(".wb-panel .wb-panel-title.is-on")?.textContent ?? ""`); got != "Problems2" {
		t.Errorf("the panel's tab says %q", got)
	}
	if got := Eval[string](p, `document.querySelector(".wb-panel .prob-row").textContent`); !strings.Contains(got, "undefined: foo") || !strings.Contains(got, "compiler(UndeclaredName)") || !strings.Contains(got, "[Ln 2, Col 3]") {
		t.Errorf("the first row says %q", got)
	}
	p.TapJS(`document.querySelector(".wb-panel .prob-row")`)
	p.WaitJS(`document.querySelector(".statusbar .sb-pos")?.textContent === "Ln 2, Col 3"`)

	// The problem is fixed on the laptop: the squiggle goes.
	c.Send(t, Delta("diagnostics", 2, webproto.DiagSnap{Warnings: 1, Files: []webproto.DiagFile{{Path: "a.go", Items: diagFixture().Files[0].Items[1:]}}}))
	p.WaitJS(`!` + deskDoc + `.querySelector(".squiggly-error") && document.querySelectorAll(".wb-panel .prob-row").length === 1`)
	// Ctrl/Cmd+Shift+M hides the panel again.
	Eval[bool](p, `(document.dispatchEvent(new KeyboardEvent("keydown", {key: "M", ctrlKey: true, shiftKey: true, bubbles: true})), true)`)
	p.WaitJS(`!document.querySelector(".wb-panel")`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}
