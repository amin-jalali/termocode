//go:build browser

package web

import (
	"testing"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Context menus on the desktop (right click): editor tabs (close, close
// others, close to the right, copy paths, split) and the tree (new file,
// rename, delete through the laptop, copy paths, reveal on the laptop,
// open to the side). The clipboard is a fake in these tests.

// fakeClipboard replaces the page's clipboard; what is copied lands in
// window.__clip and a paste reads paste.
func fakeClipboard(p *Page, paste string) {
	p.T.Helper()
	Eval[bool](p, `(Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async (t) => { window.__clip = t; }, readText: async () => `+jsq(paste)+` } }), true)`)
}

// rightClick opens the context menu of the element el (a JS expression).
func rightClick(p *Page, el string) {
	p.T.Helper()
	Eval[bool](p, `(() => { const el = `+el+`; const r = el.getBoundingClientRect(); el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, button: 2, clientX: r.left + 8, clientY: r.top + 8 })); return true; })()`)
	p.WaitJS(`document.querySelector(".ctx-menu .ctx-item") && document.activeElement?.classList.contains("ctx-item")`)
}

// menuPick clicks the menu item labelled label.
func menuPick(p *Page, label string) {
	p.T.Helper()
	p.WaitJS(`[...document.querySelectorAll(".ctx-menu .ctx-item")].some((b) => b.textContent.startsWith(` + jsq(label) + `) && !b.disabled)`)
	Eval[bool](p, `([...document.querySelectorAll(".ctx-menu .ctx-item")].find((b) => b.textContent.startsWith(`+jsq(label)+`)).click(), true)`)
	p.WaitJS(`!document.querySelector(".ctx-menu")`)
}

func menuLabels(p *Page) string {
	return Eval[string](p, `[...document.querySelectorAll(".ctx-menu .ctx-item .ctx-label")].map((e) => e.textContent).join("|")`)
}

func tabEl(path string) string {
	return `document.querySelector('.ed-tab[data-path=` + jsq(path) + `]')`
}

func TestTabContextMenu(t *testing.T) {
	p, c := openFiles(t, Desktop)
	fakeClipboard(p, "")
	openBufAs(t, p, c, 2, "a.txt", nil, "a")
	openBufAs(t, p, c, 4, "src/b.txt", nil, "b")
	openDesk(t, p, c, 6, "c.txt", "c")
	rightClick(p, tabEl("src/b.txt"))
	if got := menuLabels(p); got != "Close|Close Others|Close to the Right|Close Saved|Copy Path|Copy Relative Path|Split Right|Split Down" {
		t.Errorf("tab menu %q", got)
	}
	// Esc closes it and nothing happens.
	p.Key(kb.Escape)
	p.WaitJS(`!document.querySelector(".ctx-menu")`)
	if got := groupTabs(p, 0); got != "a.txt,src/b.txt,c.txt" {
		t.Fatalf("tabs %q", got)
	}
	rightClick(p, tabEl("src/b.txt"))
	menuPick(p, "Close to the Right")
	p.WaitJS(`[...document.querySelectorAll(".ed-tab")].map((e) => e.dataset.path).join(",") === "a.txt,src/b.txt"`)
	rightClick(p, tabEl("src/b.txt"))
	menuPick(p, "Copy Path")
	p.WaitJS(`window.__clip === "/w/src/b.txt"`)
	rightClick(p, tabEl("src/b.txt"))
	menuPick(p, "Copy Relative Path")
	p.WaitJS(`window.__clip === "src/b.txt"`)
	rightClick(p, tabEl("src/b.txt"))
	menuPick(p, "Split Right")
	p.WaitJS(`document.querySelectorAll(".ed-groups.split-right > .ed-group").length === 2`)
	rightClick(p, `document.querySelector('.ed-group[data-group="0"] .ed-tab[data-path="a.txt"]')`)
	menuPick(p, "Close Others")
	p.WaitJS(`[...document.querySelectorAll('.ed-group[data-group="0"] .ed-tab')].map((e) => e.dataset.path).join(",") === "a.txt"`)
}

func TestTreeContextMenu(t *testing.T) {
	p, c := openTree(t, Desktop)
	fakeClipboard(p, "")
	row := func(path string) string {
		return `document.querySelector('.wb-side .tree-row[data-path=` + jsq(path) + `]')`
	}
	rightClick(p, row("a.go"))
	if got := menuLabels(p); got != "Open to the Side|New File…|New Folder…|Copy Path|Copy Relative Path|Reveal on the Laptop|Rename…|Delete…" {
		t.Errorf("file menu %q", got)
	}
	menuPick(p, "Copy Relative Path")
	p.WaitJS(`window.__clip === "a.go"`)

	rightClick(p, row("a.go"))
	menuPick(p, "Reveal on the Laptop")
	reply(t, c, expectOp(t, c, webproto.OpTreeRevealLaptop, `{"path":"a.go"}`), nil)

	// New File on a folder: the name form, for that folder.
	rightClick(p, row("src"))
	if got := menuLabels(p); got != "New File…|New Folder…|Copy Path|Copy Relative Path|Reveal on the Laptop|Rename…|Delete…" {
		t.Errorf("folder menu %q", got)
	}
	menuPick(p, "New File…")
	p.WaitJS(`document.activeElement === document.querySelector(".name-input")`)
	setInput(p, `document.querySelector(".name-input")`, "new.go")
	p.TapJS(ButtonByText("Create"))
	reply(t, c, expectOp(t, c, webproto.OpTreeNewFile, `{"path":"src/new.go"}`), webproto.TreeChanged{Path: "src/new.go"})
	p.WaitJS(`!document.querySelector(".file-actions")`)

	// Delete asks here (Cancel first), then the laptop.
	rightClick(p, row("a.go"))
	menuPick(p, "Delete…")
	p.WaitJS(`document.querySelector(".ask") && document.activeElement === document.querySelector(".ask .choices button")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, webproto.OpTreeDelete, `{"path":"a.go"}`), webproto.AskRef{Card: "card-4"})
	p.WaitJS(`document.querySelector(".waiting")`)
}

// A viewer's tree menu only opens and copies.
func TestTreeContextMenuViewer(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	m := Meta("c-v")
	m.Role = webproto.RoleViewer
	c := p.LoginMeta(m)
	c.Send(t, Snap("tree", 1, treeSnap(false)))
	expectList(t, c, "tree.list", "")
	c.Send(t, Delta("tree", 2, dirDelta("", false, entry("a.go", false))))
	rightClick(p, `document.querySelector('.wb-side .tree-row[data-path="a.go"]')`)
	if got := menuLabels(p); got != "Open to the Side|Copy Path|Copy Relative Path" {
		t.Errorf("viewer menu %q", got)
	}
}
