//go:build browser

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop editor's language features: the laptop's language servers
// through the hub (diagnostics topic, lsp.* reads), never one in the
// browser. Problems show as squiggles, in the PROBLEMS panel and as
// counts in the status bar; hover, completion, go to definition and
// find references ask the hub at the tick the page's text is known at.

func diagFixture() webproto.DiagSnap {
	return webproto.DiagSnap{Errors: 1, Warnings: 1, Files: []webproto.DiagFile{{Path: "a.go", Items: []webproto.Diag{
		{Sev: 1, Line: 1, Col: 2, EndLine: 1, EndCol: 5, Message: "undefined: foo", Source: "compiler", Code: "UndeclaredName"},
		{Sev: 2, Line: 0, Col: 0, EndLine: 0, EndCol: 7, Message: "missing package doc"},
	}}}}
}

// lspArgs reads the args of an lsp.* request.
func lspArgs(t *testing.T, r *webproto.Req) webproto.LspPosArgs {
	t.Helper()
	var a webproto.LspPosArgs
	if err := json.Unmarshal(r.Args, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

// answerOpen answers the buf.open of a file the page went to by itself.
func answerOpen(t *testing.T, p *Page, c *HubConn, buf int, path string, lines ...string) {
	t.Helper()
	r := nextOpen(t, c)
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"buf": buf, "path": path, "tick": 3, "lines": len(lines), "dirty": false, "ft": "go", "readonly": false})})
	topic := fmt.Sprint("buf:", buf)
	waitSubSkipping(t, c, topic)
	c.Send(t, Snap(topic, 1, bufSnap(buf, path, lines...)))
	p.WaitJS(filesView + `.querySelector(".desk-editor")`)
}

// enterKey presses Enter as a key only: no text comes with it (a real
// Enter that takes a completion item types nothing more).
func enterKey(p *Page) {
	p.T.Helper()
	ev := func(typ input.DispatchKeyEventType) chromedp.Action[chromedp.Void] {
		return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, input.DispatchKeyEvent, input.DispatchKeyEventParams{Type: typ, Key: "Enter", Code: "Enter", WindowsVirtualKeyCode: 13, NativeVirtualKeyCode: 13})
			return err
		})
	}
	p.Do(ev(input.DispatchKeyEventTypeRawKeyDown), ev(input.DispatchKeyEventTypeKeyUp))
}

// deskCaret puts the caret on row (0-based) after n characters.
func deskCaret(p *Page, row, n int) {
	p.T.Helper()
	deskFocus(p)
	p.Do(deskTextEnd(false))
	for i := 0; i < row; i++ {
		p.Do(chromedp.KeyEvent(kb.ArrowDown))
	}
	p.Do(chromedp.KeyEvent(kb.Home))
	for i := 0; i < n; i++ {
		p.Do(chromedp.KeyEvent(kb.ArrowRight))
	}
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

// Hovering a word asks lsp.hover; the answer (a markdown AST) shows in
// the editor's hover with its code block, and markup in the text stays
// text.
func TestDeskHover(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a", "x foo()")
	at := Eval[struct{ X, Y float64 }](p, `(() => {
		const f = `+deskFrame+`, d = f.contentDocument, fr = f.getBoundingClientRect();
		const line = [...d.querySelectorAll(".view-lines .view-line")].sort((a, b) => a.offsetTop - b.offsetTop)[1];
		const w = d.createTreeWalker(line, NodeFilter.SHOW_TEXT);
		for (let n = w.nextNode(); n; n = w.nextNode()) {
			const i = n.textContent.indexOf("oo");
			if (i < 0) continue;
			const r = d.createRange(); r.setStart(n, i); r.setEnd(n, i + 1);
			const b = r.getBoundingClientRect();
			return { X: fr.left + b.left + b.width / 2, Y: fr.top + b.top + b.height / 2 };
		}
		return { X: 0, Y: 0 };
	})()`)
	p.Do(chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, at.X-2, at.Y), chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, at.X, at.Y))
	r := expectReq(t, c, "lsp.hover")
	if a := lspArgs(t, r); a.Buf != 2 || a.Tick != 3 || a.Line != 1 || a.Col < 2 || a.Col > 5 {
		t.Fatalf("hover args %+v", a)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(webproto.LspHover{MD: &webproto.MdNode{K: "doc", C: []webproto.MdNode{
		{K: "codeblock", Lang: "go", Text: "func foo() int"},
		{K: "p", C: []webproto.MdNode{{K: "text", Text: "Does <b>things</b> *twice*"}}},
	}}})})
	p.WaitJS(deskDoc + `.querySelector(".monaco-hover")?.textContent.includes("func foo() int")`)
	hover := deskDoc + `.querySelector(".monaco-hover")`
	if !Eval[bool](p, hover+`.textContent.includes("Does <b>things</b> *twice*") && !`+hover+`.querySelector("b, em, script")`) {
		t.Errorf("the hover made markup: %s", Eval[string](p, hover+`.innerHTML`))
	}
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// Typing "." sends the text first, then asks lsp.complete at the new
// tick; taking an item inserts it (a snippet) and that goes out as
// typing.
func TestDeskCompletion(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a", "fmt")
	deskType(p, true, ".")
	r, a := expectSetText(t, c)
	if a.Tick != 3 || strings.Join(a.Lines, "|") != "." {
		t.Fatalf("set_text %+v", a)
	}
	okEdit(t, c, r, 4)
	r = expectReq(t, c, "lsp.complete")
	if a := lspArgs(t, r); a.Buf != 2 || a.Tick != 4 || a.Line != 1 || a.Col != 4 {
		t.Fatalf("complete args %+v", a)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(webproto.LspCompletion{Items: []webproto.LspItem{
		{Label: "Println", Kind: 3, Detail: "func(a ...any)", Insert: "Println(${1:a})", Snippet: true, From: -1, To: -1},
		{Label: "Sprint", Kind: 3, Insert: "Sprint", From: -1, To: -1},
	}})})
	p.WaitJS(deskDoc + `.querySelector(".suggest-widget .monaco-list-row")?.textContent.includes("Println")`)
	enterKey(p)
	r, a = expectSetText(t, c)
	if a.Tick != 4 || strings.Join(a.Lines, "|") != "Println(a)" || a.Start != (webproto.Pos{Row: 1, Col: 4}) {
		t.Fatalf("the item went out as %+v", a)
	}
	okEdit(t, c, r, 5)
	if got := deskText(p); got != "package a\nfmt.Println(a)" {
		t.Errorf("the editor has %q", got)
	}
}

// F12 asks lsp.definition at the caret; a definition in another file
// opens there with the caret on it.
func TestDeskGoToDefinition(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a", "x foo()")
	deskCaret(p, 1, 3)
	p.Do(chromedp.KeyEvent(kb.F12))
	r := expectReq(t, c, "lsp.definition")
	if a := lspArgs(t, r); a.Buf != 2 || a.Tick != 3 || a.Line != 1 || a.Col != 3 {
		t.Fatalf("definition args %+v", a)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(webproto.LspLocations{Locs: []webproto.LspLoc{
		{Path: "b.go", Line: 2, Col: 5, EndLine: 2, EndCol: 8, Text: "func foo() int { return 1 }"},
	}})})
	p.WaitJS(`location.hash === "#/files/b.go"`)
	answerOpen(t, p, c, 3, "b.go", "package a", "", "func foo() int { return 1 }")
	p.WaitJS(`document.querySelector(".statusbar .sb-pos")?.textContent === "Ln 3, Col 6"`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// Shift+F12 asks lsp.references and lists the places by file, the word
// marked; a place outside the workspace is listed but cannot open; a
// row opens its file; Esc closes the list.
func TestDeskFindReferences(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a", "x foo()")
	deskCaret(p, 1, 3)
	p.Do(chromedp.KeyEvent(kb.F12, chromedp.KeyModifiers(chromedp.ModifierShift)))
	r := expectReq(t, c, "lsp.references")
	if a := lspArgs(t, r); a.Line != 1 || a.Col != 3 || a.Tick != 3 {
		t.Fatalf("references args %+v", a)
	}
	p.WaitJS(`document.querySelector(".refs-peek .refs-title")?.textContent === "References to foo"`)
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(webproto.LspLocations{Locs: []webproto.LspLoc{
		{Name: "lib.go", Outside: true, Line: 9, EndLine: 9},
		{Path: "a.go", Line: 1, Col: 2, EndLine: 1, EndCol: 5, Text: "x foo()"},
		{Path: "b.go", Line: 2, Col: 5, EndLine: 2, EndCol: 8, Text: "func foo() int { return 1 }"},
	}})})
	p.WaitJS(`document.querySelectorAll(".refs-peek .refs-row").length === 3`)
	if got := Eval[string](p, `[...document.querySelectorAll(".refs-peek .refs-row mark")].map((m) => m.textContent).join(",")`); got != "foo,foo" {
		t.Errorf("marks %q", got)
	}
	if !Eval[bool](p, `document.querySelector(".refs-peek .refs-row").disabled && document.querySelector(".refs-peek .refs-path").textContent === "Outside the workspace"`) {
		t.Error("the place outside the workspace is not listed as such")
	}
	p.TapJS(`[...document.querySelectorAll(".refs-peek .refs-row")][2]`)
	p.WaitJS(`location.hash === "#/files/b.go"`)
	answerOpen(t, p, c, 3, "b.go", "package a", "", "func foo() int { return 1 }")
	p.WaitJS(`document.querySelector(".statusbar .sb-pos")?.textContent === "Ln 3, Col 6"`)
	Eval[bool](p, `(document.querySelector(".refs-peek .refs-row:not([disabled])").focus(), true)`)
	p.Key(kb.Escape)
	p.WaitJS(`!document.querySelector(".refs-peek")`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}
