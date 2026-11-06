//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Quick fixes and inlay hints in the desktop editor (WEBP-9, Editor and
// files): Quick Fix… lists the laptop's code actions of the caret's place
// and applies the one picked there; Apply Preferred Quick Fix applies
// the preferred one at once; Toggle Inlay Hints shows the servers' hints
// in the rows and hides them again.

func TestDeskQuickFix(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a", "var x = f(1)")
	deskRow(p, 2)
	runCmd(t, p, c, true, "quick fix", "Edit: Quick Fix…")
	r := waitArgs(t, c, webproto.OpLspCodeActions, `{"buf":2,"tick":3,"line":1,"col":0,"end_line":1,"end_col":0}`)
	reply(t, c, r, webproto.LspActions{Items: []webproto.LspAction{{Title: "Add import", Kind: "quickfix"}, {Title: "Rename x", Kind: "refactor", Preferred: true}}})
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "Edit: Quick Fix…"`)
	p.Key("add")
	pickRow(p, "Add import")
	r = waitArgs(t, c, webproto.OpLspCodeActionApply, `{"buf":2,"tick":3,"index":0}`)
	reply(t, c, r, webproto.EditRes{Tick: 4, Dirty: true})

	runCmd(t, p, c, false, "preferred quick fix", "Edit: Apply Preferred Quick Fix")
	r = waitReq(t, c, webproto.OpLspCodeActions)
	reply(t, c, r, webproto.LspActions{Items: []webproto.LspAction{{Title: "Add import", Kind: "quickfix"}, {Title: "Rename x", Kind: "refactor", Preferred: true}}})
	r = waitReq(t, c, webproto.OpLspCodeActionApply)
	if canonJSON(t, r.Args) != canonJSON(t, []byte(`{"buf":2,"tick":3,"index":1}`)) {
		t.Errorf("preferred args %s", r.Args)
	}
	reply(t, c, r, webproto.EditRes{Tick: 4, Dirty: true})

	runCmd(t, p, c, false, "preferred quick fix", "Edit: Apply Preferred Quick Fix")
	r = waitReq(t, c, webproto.OpLspCodeActions)
	reply(t, c, r, webproto.LspActions{Items: []webproto.LspAction{{Title: "Rename x", Kind: "refactor"}}})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("No preferred quick fix here.")`)
}

func TestDeskInlayHints(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a", "x := f(1)")
	runCmd(t, p, c, true, "inlay hints", "LSP: Toggle Inlay Hints")
	// Each ask (the view may settle once more) gets the same hint.
	hints := webproto.LspHints{Items: []webproto.LspHint{{Line: 1, Col: 1, Label: ": int", PadLeft: true}}}
	shown := deskDoc + `.querySelector(".tc-inlay")?.textContent.replace(/\u00a0/g, " ").includes(": int")`
	for end := time.Now().Add(wait); !Eval[bool](p, `!!(`+shown+`)`); {
		if time.Now().After(end) {
			t.Fatal("the hint does not show")
		}
		select {
		case m := <-c.in:
			if r, ok := m.(*webproto.Req); ok && r.Op == webproto.OpLspInlayHints {
				reply(t, c, r, hints)
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	runCmd(t, p, c, false, "inlay hints", "LSP: Toggle Inlay Hints")
	p.WaitJS(`!` + deskDoc + `.querySelector(".tc-inlay")`)
}
