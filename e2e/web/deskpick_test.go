//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop's pickers (WEBP-9, Editor and files): the laptop's
// bookmarks (toggle on the caret's row, the list, clear all after a
// question here), its recent files, the workspace symbols of a query and
// the snippets of the file's type, each in the quick input.

// pickRow waits for the quick input's selected row named label and runs it.
func pickRow(p *Page, label string) {
	p.T.Helper()
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === ` + jsq(label))
	p.Key("\r")
	p.WaitJS(`!document.querySelector(".quick")`)
}

func TestDeskBookmarks(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a", "var x = 1")
	deskRow(p, 2)
	runCmd(t, p, c, true, "bookmarks toggle", "Bookmarks: Toggle on Current Line")
	r := waitArgs(t, c, webproto.OpBookmarksToggle, `{"buf":2,"line":1}`)
	reply(t, c, r, webproto.BookmarkToggle{On: true, Mark: "A"})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Bookmark A set.")`)

	runCmd(t, p, c, false, "bookmarks list", "Bookmarks: Show List")
	r = waitReq(t, c, webproto.OpBookmarksList)
	reply(t, c, r, webproto.BookmarkList{Items: []webproto.Bookmark{{Mark: "A", Path: "a.go", Line: 1, Text: "var x = 1"}, {Mark: "B", Path: "pkg/b.go", Line: 4}}})
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join() === "A  a.go:2,B  pkg/b.go:5"`)
	p.Key("pkg")
	pickRow(p, "B  pkg/b.go:5")
	r = nextOpen(t, c)
	if canonJSON(t, r.Args) != `{"path":"pkg/b.go"}` {
		t.Errorf("opened %s", r.Args)
	}
	reply(t, c, r, map[string]any{"buf": 3, "path": "pkg/b.go", "tick": 3, "lines": 1, "dirty": false, "ft": "go", "readonly": false})

	// Clear all asks here first; Cancel is first and focused.
	runCmd(t, p, c, false, "bookmarks clear", "Bookmarks: Clear All")
	p.WaitJS(`document.activeElement === document.querySelector(".ask-here .choices button")`)
	p.WaitJS(`document.querySelector(".ask-here .btn-danger.is-armed")`)
	p.TapJS(`document.querySelector(".ask-here .btn-danger")`)
	r = waitReq(t, c, webproto.OpBookmarksClear)
	reply(t, c, r, nil)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("All bookmarks cleared.")`)
}

func TestDeskOpenRecent(t *testing.T) {
	p, c := openFiles(t, Desktop)
	runCmd(t, p, c, true, "open recent", "File: Open Recent…")
	r := waitReq(t, c, webproto.OpFilesRecent)
	reply(t, c, r, webproto.FilesRecent{Paths: []string{"pkg/b.go", "a.go"}})
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "b.go"`)
	p.Key("a.go")
	pickRow(p, "a.go")
	r = nextOpen(t, c)
	if canonJSON(t, r.Args) != `{"path":"a.go"}` {
		t.Errorf("opened %s", r.Args)
	}
}

func TestDeskWorkspaceSymbols(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a")
	runCmd(t, p, c, true, "symbol in workspace", "Go to Symbol in Workspace…")
	p.Key("Hand")
	r := waitArgs(t, c, webproto.OpLspWorkspaceSymbols, `{"buf":2,"query":"Hand"}`)
	reply(t, c, r, webproto.LspSymbols{Items: []webproto.LspSymbol{{Name: "Handler", Kind: 23, Path: "pkg/h.go", Line: 2, Col: 5, Container: "pkg"}}})
	if got := Eval[string](p, `(async () => { for (let i = 0; i < 100; i++) { const d = document.querySelector(".quick-row.is-on .quick-detail"); if (d) return d.textContent; await new Promise((r) => setTimeout(r, 30)); } return ""; })()`); got != "pkg · pkg/h.go" {
		t.Errorf("symbol detail %q", got)
	}
	pickRow(p, "Handler")
	r = nextOpen(t, c)
	if canonJSON(t, r.Args) != `{"path":"pkg/h.go"}` {
		t.Errorf("opened %s", r.Args)
	}
}

func TestDeskSnippetPicker(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a", "")
	deskRow(p, 2)
	runCmd(t, p, c, true, "snippets browse", "Snippets: Browse…")
	r := waitArgs(t, c, webproto.OpSnippetsList, `{"buf":2}`)
	reply(t, c, r, webproto.SnippetList{Items: []webproto.Snippet{{Trigger: "fn", Body: "func ${1:name}() {\n\t$0\n}"}, {Trigger: "iferr", Body: "if err != nil {\n\treturn ${1:err}\n}"}}})
	p.Key("iferr")
	pickRow(p, "iferr")
	waitDeskText(t, p, "package a", "if err != nil {", "    return err", "}")
}
