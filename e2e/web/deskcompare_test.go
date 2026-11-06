//go:build browser

package web

import (
	"encoding/json"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// File: Compare Two Files… (WEBP-9, Editor and files): the open file
// against one picked from the workspace, both as they are on disk, side
// by side in a read-only diff tab.
func TestDeskCompareTwoFiles(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a")
	runCmd(t, p, c, true, "compare two files", "File: Compare Two Files…")
	r := waitReq(t, c, webproto.OpFilesList)
	reply(t, c, r, webproto.FilesListRes{Paths: []string{"a.go", "b.go", "pkg/c.go"}})
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "Compare a.go with…"`)
	p.Key("b.go")
	pickRow(p, "b.go")
	got := map[string]bool{}
	for len(got) < 2 {
		r := waitReq(t, c, webproto.OpFileText)
		var a struct{ Path string }
		if err := json.Unmarshal(r.Args, &a); err != nil {
			t.Fatal(err)
		}
		got[a.Path] = true
		text := map[string]string{"a.go": "package a\n\nvar x = 1\n", "b.go": "package a\n\nvar x = 2\n"}[a.Path]
		reply(t, c, r, webproto.FileText{Path: a.Path, Text: text})
	}
	if !got["a.go"] || !got["b.go"] {
		t.Fatalf("read %v", got)
	}
	p.WaitJS(`document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent === "a.go ↔ b.go"`)
	p.WaitJS(deskDoc + `?.querySelector(".editor.original .view-lines")?.textContent.replace(/\u00a0/g, " ").includes("var x = 1")`)
	p.WaitJS(deskDoc + `?.querySelector(".editor.modified .view-lines")?.textContent.replace(/\u00a0/g, " ").includes("var x = 2")`)
}
