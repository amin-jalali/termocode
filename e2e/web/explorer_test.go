//go:build browser

package web

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The explorer's OUTLINE and TIMELINE sections on the desktop.

// reqSkipping reads the page's messages until a request for op; others
// (subs, unsubs, a buf.close of a file the editor leaves) go by, a
// buf.close answered.
func reqSkipping(t *testing.T, c *HubConn, op string) *webproto.Req {
	t.Helper()
	for {
		if r, ok := c.Next(t).(*webproto.Req); ok {
			if r.Op == op {
				return r
			}
			if r.Op == "buf.close" {
				c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
			}
		}
	}
}

const outline = `document.querySelector('.ex-section[data-section="outline"]')`
const timeline = `document.querySelector('.ex-section[data-section="timeline"]')`

// OUTLINE starts closed; opened, it lists the open file's symbols
// (nested by depth, a guess says so) and a click puts the editor's
// cursor on the symbol's line.
func TestExplorerOutlineJumps(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openBufAs(t, p, c, 2, "src/a.go", nil, "package a", "", "type Box struct{}", "", "func (b Box) Size() int { return 0 }", "", "func main() {}")
	p.WaitJS(deskDoc + `?.querySelector(".view-lines .view-line")`)
	if !Eval[bool](p, outline+` && !`+outline+`.querySelector(".ex-body")`) {
		t.Fatal("the outline is not there, closed")
	}
	p.TapJS(outline + `.querySelector(".ex-toggle")`)
	r := reqSkipping(t, c, "buf.symbols")
	var a webproto.BufArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.Buf != 2 {
		t.Fatalf("buf.symbols args %s", r.Args)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"buf": 2, "source": "syntax", "symbols": []map[string]any{
		{"name": "Box", "kind": 5, "line": 3, "col": 6, "depth": 0},
		{"name": "Size", "kind": 6, "line": 5, "col": 14, "depth": 1},
		{"name": "main", "kind": 12, "line": 7, "col": 6, "depth": 0},
	}})})
	p.WaitJS(`[...` + outline + `.querySelectorAll(".ex-row .ex-name")].map((e) => e.textContent).join() === "Box,Size,main"`)
	if !Eval[bool](p, outline+`.querySelector('.ex-row[data-line="5"]').classList.contains("depth-1") && `+outline+`.querySelector('.ex-row[data-line="5"] .cod-symbol-method') !== null`) {
		t.Error("Size is not a nested method")
	}
	if !Eval[bool](p, outline+`.innerText.includes("Guessed from the code")`) {
		t.Error("a guessed outline does not say so")
	}
	// Open under the tree, with room for its rows, above TIMELINE.
	p.TapJS(timeline + `.querySelector(".ex-toggle")`)
	geo := Eval[[]float64](p, `[`+outline+`, `+timeline+`, document.querySelector(".ex-stack > .view")].flatMap((e) => { const r = e.getBoundingClientRect(); return [r.top, r.bottom]; })`)
	if geo[1]-geo[0] < 3*24 || geo[1] > geo[2] || geo[5] > geo[0] {
		t.Errorf("tree %v, outline %v, timeline %v: the sections overlap or have no room", geo[4:6], geo[0:2], geo[2:4])
	}
	p.TapJS(outline + `.querySelector('.ex-row[data-line="7"]')`)
	p.WaitJS(`document.querySelector(".statusbar .sb-pos")?.textContent === "Ln 7, Col 1"`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

// TIMELINE lists the commits that changed the open file; a click opens
// that commit's change of the file in a read-only editor tab.
func TestExplorerTimelineOpensDiffTab(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openBufAs(t, p, c, 2, "src/a.go", nil, "package a")
	p.TapJS(timeline + `.querySelector(".ex-toggle")`)
	r := reqSkipping(t, c, "git.file_log")
	var a webproto.GitFileLogArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.Path != "src/a.go" {
		t.Fatalf("git.file_log args %s", r.Args)
	}
	when := time.Now().Add(-3 * time.Hour).UnixMilli()
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"path": "src/a.go", "log": []map[string]any{
		{"hash": "abc1234", "subject": "fix the box", "author": "Ann", "when": when},
		{"hash": "0ff1ce0", "subject": "first", "author": "Bo", "when": when - 86400000},
	}})})
	p.WaitJS(`[...` + timeline + `.querySelectorAll(".ex-commit")].map((e) => e.innerText.replace(/\s+/g, " ").trim()).join("|") === "fix the box Ann · 3h|first Bo · 1d"`)

	p.TapJS(timeline + `.querySelector('.ex-commit[data-hash="abc1234"]')`)
	s := reqSkipping(t, c, "git.show")
	var g webproto.GitShowArgs
	if err := json.Unmarshal(s.Args, &g); err != nil || g.Hash != "abc1234" || g.Path != "src/a.go" {
		t.Fatalf("git.show args %s", s.Args)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: s.ID, Data: raw(map[string]any{
		"commit": map[string]any{"hash": "abc1234", "subject": "fix the box", "author": "Ann", "when": when},
		"rows": []map[string]any{
			{"k": "file", "text": "src/a.go"}, {"k": "hunk", "text": "@@ -1 +1,2 @@"},
			{"k": "ctx", "text": "package a", "old": 1, "new": 1}, {"k": "add", "text": "type Box struct{}", "new": 2},
		},
		"from": 0, "added": 1, "removed": 0,
	})})
	const tl = `document.querySelector('[data-view="timeline"]')`
	p.WaitJS(tl + `?.querySelector(".drow-add")?.textContent.includes("type Box struct{}")`)
	if got := Eval[string](p, `document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent ?? ""`); got != "a.go (abc1234)" {
		t.Errorf("the tab is %q, want a.go (abc1234)", got)
	}
	if !Eval[bool](p, tl+`.innerText.includes("fix the box") && `+tl+`.innerText.includes("View only")`) {
		t.Error("the diff tab does not show its commit or that it is read-only")
	}
	// The file's own tab is still there; back to it, the editor shows the file.
	p.TapJS(`document.querySelector('.ed-tab-open[data-path="src/a.go"]')`)
	reqSkipping(t, c, "buf.open")
}
