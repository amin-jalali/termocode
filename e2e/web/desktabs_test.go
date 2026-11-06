//go:build browser

package web

import (
	"encoding/json"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop's tab and explorer commands, as the laptop's editor has
// them (WEBP-9, Editor and files): last file, pinned tabs, reopen a
// closed tab, close the split, open the URL on the caret's row (asked
// first), reveal the file in the tree, hidden files, preview tabs,
// rename the open file and undo that rename.

// waitArgs waits for a req for op (others go by) and checks its args.
func waitArgs(t *testing.T, c *HubConn, op, args string) *webproto.Req {
	t.Helper()
	r := waitReq(t, c, op)
	if canonJSON(t, r.Args) != canonJSON(t, json.RawMessage(args)) {
		t.Fatalf("%s args %s; want %s", op, r.Args, args)
	}
	return r
}

const activeTab = `document.querySelector(".ed-tab.is-on")?.dataset.path`

func TestDeskTabCommands(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "package a")
	openDesk(t, p, c, 3, "b.go", "package b")
	runCmd(t, p, c, true, "switch to last", "View: Switch to Last File")
	p.WaitJS(activeTab + ` === "a.go"`)
	runCmd(t, p, c, false, "switch to last", "View: Switch to Last File")
	p.WaitJS(activeTab + ` === "b.go"`)

	// A pinned tab moves first and shows a pin; Close Others keeps it.
	runCmd(t, p, c, false, "toggle pinned", "Tab: Toggle Pinned")
	p.WaitJS(`document.querySelector(".ed-tab")?.dataset.path === "b.go" && document.querySelector('.ed-tab[data-path="b.go"]').classList.contains("is-pinned")`)
	rightClick(p, `document.querySelector('.ed-tab[data-path="a.go"]')`)
	p.TapJS(`[...document.querySelectorAll(".ctx-menu .ctx-item")].find((b) => b.querySelector(".ctx-label")?.textContent === "Close Others")`)
	p.WaitJS(`[...document.querySelectorAll(".ed-tab")].map((t) => t.dataset.path).join() === "a.go,b.go" || [...document.querySelectorAll(".ed-tab")].map((t) => t.dataset.path).join() === "b.go,a.go"`)

	// A closed tab comes back.
	p.TapJS(`document.querySelector('.ed-tab[data-path="a.go"] .ed-tab-open')`)
	p.WaitJS(activeTab + ` === "a.go"`)
	runCmd(t, p, c, false, "close editor", "Close Editor")
	p.WaitJS(`!document.querySelector('.ed-tab[data-path="a.go"]')`)
	runCmd(t, p, c, false, "reopen closed", "View: Reopen Closed Editor")
	p.WaitJS(activeTab + ` === "a.go"`)

	// Close Split closes the focused group; its files stay open on the laptop.
	runCmd(t, p, c, false, "split editor right", "View: Split Editor Right")
	p.WaitJS(`document.querySelectorAll(".ed-group").length === 2`)
	runCmd(t, p, c, false, "close split", "Editor: Close Split")
	p.WaitJS(`document.querySelectorAll(".ed-group").length === 1`)
}

func TestDeskOpenURLAsksFirst(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.go", "// see https://example.com/doc?x=1 for more", "package a")
	deskRow(p, 1)
	runCmd(t, p, c, true, "open url", "View: Open URL on Current Line")
	p.WaitJS(`document.querySelector(".link-ask .card-subject")?.textContent === "https://example.com/doc?x=1"`)
	if first := p.Text(".link-ask .choices button"); first != "Cancel" {
		t.Errorf("the first choice is %q", first)
	}
	// A row with no URL says so. The card closes first (while it is
	// open it takes the keys), and the command waits until the caret is
	// on row 2: it reads the caret's row, which comes a moment after the
	// key, and a quick runner asked about row 1's URL again.
	p.TapJS(`document.querySelector(".link-ask .choices button")`)
	p.WaitJS(`!document.querySelector(".link-ask")`)
	deskRow(p, 2)
	p.WaitJS(`document.querySelector(".statusbar .sb-pos")?.textContent.startsWith("Ln 2,")`)
	runCmd(t, p, c, false, "open url", "View: Open URL on Current Line")
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("No URL on this line.")`)
}

func TestDeskExplorerCommands(t *testing.T) {
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("tree", 1, treeSnap(true)))
	expectList(t, c, "tree.list", "")
	c.Send(t, Delta("tree", 2, dirDelta("", false, entry("pkg", true), entry(".env.example", false, "hidden"), entry("a.go", false))))
	p.WaitJS(treeRow(".env.example"))

	// Hidden files: hidden here, shown again.
	runCmd(t, p, c, true, "toggle hidden", "Files: Toggle Hidden Files")
	p.WaitJS(`!` + treeRow(".env.example"))
	runCmd(t, p, c, false, "toggle hidden", "Files: Toggle Hidden Files")
	p.WaitJS(treeRow(".env.example"))

	// Preview tabs off: a single click keeps the tab.
	runCmd(t, p, c, false, "toggle preview", "Explorer: Toggle Preview")
	p.TapJS(treeRow("a.go"))
	r := nextOpen(t, c)
	reply(t, c, r, map[string]any{"buf": 2, "path": "a.go", "tick": 3, "lines": 1, "dirty": false, "ft": "go", "readonly": false})
	waitSubSkipping(t, c, "buf:2")
	c.Send(t, Snap("buf:2", 1, bufSnap(2, "a.go", "package a")))
	p.WaitJS(`document.querySelector('.ed-tab[data-path="a.go"]') && !document.querySelector('.ed-tab[data-path="a.go"]').classList.contains("is-preview")`)

	// Reveal: the file's folders open in the tree.
	openDesk(t, p, c, 3, "pkg/c.go", "package pkg")
	runCmd(t, p, c, false, "reveal file", "File: Reveal in Files")
	waitArgs(t, c, webproto.OpTreeList, `{"path":"pkg"}`)

	// Rename the open file, then undo it.
	runCmd(t, p, c, false, "rename selected", "Explorer: Rename Selected…")
	p.WaitJS(`document.querySelector(".name-input")?.value === "c.go"`)
	setInput(p, `document.querySelector(".name-input")`, "d.go")
	p.TapJS(`[...document.querySelectorAll(".name-form .btn")].find((b) => b.textContent === "Rename")`)
	r = waitArgs(t, c, webproto.OpTreeRename, `{"path":"pkg/c.go","name":"d.go"}`)
	reply(t, c, r, webproto.TreeChanged{Path: "pkg/d.go"})
	p.WaitJS(`!document.querySelector(".name-form") && document.querySelector(".toasts")?.textContent.includes("Renamed to d.go.")`)
	runCmd(t, p, c, false, "undo rename", "Explorer: Undo Rename")
	r = waitArgs(t, c, webproto.OpTreeRename, `{"path":"pkg/d.go","name":"c.go"}`)
	reply(t, c, r, webproto.TreeChanged{Path: "pkg/c.go"})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Renamed back to c.go.")`)
}
