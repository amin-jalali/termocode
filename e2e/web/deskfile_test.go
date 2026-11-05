//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop editor's language and file commands (WEBP-9, Editor and
// files): go to type definition and to implementation from the laptop's
// language server, Format Document at the page's tick, Revert File
// (asked here first, Cancel first: unsaved changes go) and the Markdown
// preview in its own tab.

func TestDeskTypeDefinitionAndImplementation(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a", "var x T")
	deskRow(p, 2)
	runCmd(t, p, c, true, "type definition", "Go to Type Definition")
	r := waitReq(t, c, webproto.OpLspTypeDefinition)
	if canonJSON(t, r.Args) != `{"buf":2,"col":0,"line":1,"tick":3}` {
		t.Errorf("type definition args %s", r.Args)
	}
	reply(t, c, r, webproto.LspLocations{Locs: []webproto.LspLoc{{Path: "t.go", Line: 2, Col: 5, EndLine: 2, EndCol: 6}}})
	r = nextOpen(t, c)
	if canonJSON(t, r.Args) != `{"path":"t.go"}` {
		t.Errorf("opened %s", r.Args)
	}
	reply(t, c, r, map[string]any{"buf": 3, "path": "t.go", "tick": 3, "lines": 3, "dirty": false, "ft": "go", "readonly": false})
	waitSubSkipping(t, c, "buf:3")
	c.Send(t, Snap("buf:3", 1, bufSnap(3, "t.go", "package a", "", "type T int")))
	p.WaitJS(activeTab + ` === "t.go"`)

	runCmd(t, p, c, false, "implementation", "Go to Implementation")
	r = waitReq(t, c, webproto.OpLspImplementation)
	reply(t, c, r, webproto.LspLocations{Locs: []webproto.LspLoc{}})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("No implementation found.")`)
}

func TestDeskFormatDocument(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package  a")
	runCmd(t, p, c, true, "format document", "Format Document")
	r := waitArgs(t, c, webproto.OpBufFormat, `{"buf":2,"tick":3}`)
	reply(t, c, r, webproto.EditRes{Tick: 4, Dirty: true})
	c.Send(t, Delta("buf:2", 2, map[string]any{"kind": "lines", "first": 0, "last": 1, "lines": []string{"package a"}, "tick": 4}))
	waitDeskText(t, p, "package a")
	runCmd(t, p, c, false, "format document", "Format Document")
	r = waitArgs(t, c, webproto.OpBufFormat, `{"buf":2,"tick":4}`)
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeNotFound, Msg: "No formatter is available for this file."})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("No formatter is available for this file.")`)
}

func TestDeskRevertFileAsksFirst(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a")
	runCmd(t, p, c, true, "revert file", "File: Revert File")
	p.WaitJS(`document.querySelector(".ask-here") && document.activeElement === document.querySelector(".ask-here .choices button")`)
	if got := p.Text(".ask-here .ask-text"); got != "Read a.go from disk again? Its unsaved changes are lost." {
		t.Errorf("question %q", got)
	}
	// Enter takes the safe choice.
	p.Key(kb.Enter)
	p.WaitJS(`!document.querySelector(".ask-here")`)
	c.Quiet(t, 300*time.Millisecond)
	runCmd(t, p, c, false, "revert file", "File: Revert File")
	p.WaitJS(`document.querySelector(".ask-here .btn-danger.is-armed")`)
	p.TapJS(`document.querySelector(".ask-here .btn-danger")`)
	r := waitArgs(t, c, webproto.OpBufReload, `{"buf":2}`)
	reply(t, c, r, webproto.EditRes{Tick: 5})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Read again from disk.")`)
}

func TestDeskMarkdownPreview(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "README.md", "# Hi", "", "Some text")
	runCmd(t, p, c, true, "markdown preview", "Markdown: Open Preview")
	r := waitArgs(t, c, webproto.OpBufMarkdown, `{"buf":2}`)
	reply(t, c, r, webproto.BufMarkdown{MD: &webproto.MdNode{K: webproto.MdDoc, C: []webproto.MdNode{
		{K: webproto.MdHeading, Level: 1, C: []webproto.MdNode{{K: webproto.MdText, Text: "Hi"}}},
		{K: webproto.MdParagraph, C: []webproto.MdNode{{K: webproto.MdText, Text: "Some text"}}},
	}}})
	p.WaitJS(`document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent === "Preview README.md"`)
	p.WaitJS(`document.querySelector(".md-preview h1")?.textContent === "Hi"`)
}
