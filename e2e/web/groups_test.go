//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Editor groups: the editor splits right or down (Ctrl/Cmd+\, or a tab
// dragged to the editor's side), Ctrl/Cmd+1 and +2 move between the two
// groups, a tab drags to another place, a single click in the tree opens
// a preview tab (in italics) that the next one replaces until it is kept.

// groupTabs is group g's tab paths, in order.
func groupTabs(p *Page, g int) string {
	return Eval[string](p, `[...document.querySelectorAll('.ed-group[data-group="`+itoa(g)+`"] .ed-tab')].map((e) => e.dataset.path).join(",")`)
}

func itoa(n int) string { return string(rune('0' + n)) }

// dragJS drags the tab of path (as the mouse would, with one data
// transfer) and drops it where drop (a JS expression for the element and
// the point) says, after the drag shows.
func dragTabTo(p *Page, path, target string, fx, fy float64) {
	p.T.Helper()
	Eval[bool](p, `(() => {
		window.__dt = new DataTransfer();
		const tab = document.querySelector('.ed-tab[data-path=`+jsq(path)+`]');
		tab.dispatchEvent(new DragEvent("dragstart", { bubbles: true, cancelable: true, dataTransfer: window.__dt }));
		return true;
	})()`)
	p.WaitJS(target)
	Eval[bool](p, `(() => {
		const el = `+target+`;
		const r = el.getBoundingClientRect();
		const x = r.left + r.width * `+ftoa(fx)+`, y = r.top + r.height * `+ftoa(fy)+`;
		const o = { bubbles: true, cancelable: true, dataTransfer: window.__dt, clientX: x, clientY: y };
		el.dispatchEvent(new DragEvent("dragenter", o));
		el.dispatchEvent(new DragEvent("dragover", o));
		return true;
	})()`)
	Eval[bool](p, `(() => {
		const el = `+target+`;
		const r = el.getBoundingClientRect();
		const x = r.left + r.width * `+ftoa(fx)+`, y = r.top + r.height * `+ftoa(fy)+`;
		const o = { bubbles: true, cancelable: true, dataTransfer: window.__dt, clientX: x, clientY: y };
		el.dispatchEvent(new DragEvent("drop", o));
		document.querySelector('.ed-tab[data-path=`+jsq(path)+`]')?.dispatchEvent(new DragEvent("dragend", { bubbles: true, dataTransfer: window.__dt }));
		return true;
	})()`)
}

func ftoa(f float64) string {
	switch f {
	case 0.1:
		return "0.1"
	case 0.5:
		return "0.5"
	case 0.9:
		return "0.9"
	}
	return "0.5"
}

func TestSplitEditorAndFocusGroups(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "alpha")
	wbKey(p, `\`, true, false)
	// The same file shows in both groups, opened once on the laptop.
	p.WaitJS(`document.querySelectorAll(".ed-groups.is-split.split-right > .ed-group").length === 2 && document.querySelectorAll(".desk-editor").length === 2`)
	if got := groupTabs(p, 1); got != "a.txt" {
		t.Errorf("the second group's tabs %q", got)
	}
	p.WaitJS(`document.querySelector('.ed-group.is-focus')?.dataset.group === "1"`)
	// Ctrl+1 and Ctrl+2 move between the groups.
	wbKey(p, "1", true, false)
	p.WaitJS(`document.querySelector('.ed-group.is-focus')?.dataset.group === "0"`)
	wbKey(p, "2", true, false)
	p.WaitJS(`document.querySelector('.ed-group.is-focus')?.dataset.group === "1"`)
	// Another file in the focused group; the first keeps its own.
	openBufAs(t, p, c, 4, "b.txt", nil, "beta")
	p.WaitJS(`document.querySelector('.ed-group[data-group="1"] .ed-tab.is-on')?.dataset.path === "b.txt"`)
	if got := groupTabs(p, 1); got != "a.txt,b.txt" {
		t.Errorf("the second group's tabs %q", got)
	}
	if got := Eval[string](p, `document.querySelector('.ed-group[data-group="0"] .ed-tab.is-on')?.dataset.path`); got != "a.txt" {
		t.Errorf("the first group shows %q", got)
	}
	// Closing the second group's tabs closes the group.
	wbKey(p, "w", true, false)
	wbKey(p, "w", true, false)
	p.WaitJS(`document.querySelectorAll(".ed-group").length === 1 && !document.querySelector(".ed-groups.is-split")`)
}

func TestTabsDragToReorderAndToTheSide(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openBufAs(t, p, c, 2, "a.txt", nil, "a")
	openBufAs(t, p, c, 4, "b.txt", nil, "b")
	openBufAs(t, p, c, 6, "c.txt", nil, "c")
	if got := groupTabs(p, 0); got != "a.txt,b.txt,c.txt" {
		t.Fatalf("tabs %q", got)
	}
	// c onto a's left half: c goes first.
	dragTabTo(p, "c.txt", `document.querySelector('.ed-tab[data-path="a.txt"]')`, 0.1, 0.5)
	p.WaitJS(`[...document.querySelectorAll('.ed-group[data-group="0"] .ed-tab')].map((e) => e.dataset.path).join(",") === "c.txt,a.txt,b.txt"`)
	// b to the editor's right edge: a new group to the right holds it.
	dragTabTo(p, "b.txt", `document.querySelector('.ed-drop[data-group="0"]')`, 0.9, 0.5)
	p.WaitJS(`document.querySelectorAll(".ed-groups.split-right > .ed-group").length === 2`)
	if got := groupTabs(p, 1); got != "b.txt" {
		t.Errorf("the new group's tabs %q", got)
	}
	if got := groupTabs(p, 0); got != "c.txt,a.txt" {
		t.Errorf("the first group's tabs %q", got)
	}
	p.WaitJS(`!document.querySelector(".ed-drop")`)
}

func TestTreeClickOpensAPreviewTab(t *testing.T) {
	p, c := openTree(t, Desktop)
	c.Send(t, Delta("tree", 3, dirDelta("", false, entry("a.go", false), entry("b.go", false), entry("c.go", false))))
	side := func(path string) string { return `document.querySelector('.wb-side .tree-row[data-path=` + jsq(path) + `]')` }
	p.WaitJS(side("c.go"))
	open := func(path string, buf int) {
		t.Helper()
		r := nextOpen(t, c)
		c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"buf": buf, "path": path, "tick": 1, "lines": 1, "dirty": false, "ft": "go", "readonly": false})})
	}
	p.TapJS(side("a.go"))
	open("a.go", 2)
	p.WaitJS(`document.querySelector('.ed-tab.is-preview[data-path="a.go"] .ed-tab-name') && getComputedStyle(document.querySelector('.ed-tab.is-preview .ed-tab-name')).fontStyle === "italic"`)
	// The next single click takes the preview's place.
	p.TapJS(side("b.go"))
	open("b.go", 4)
	p.WaitJS(`[...document.querySelectorAll(".ed-tab")].map((e) => e.dataset.path).join(",") === "b.go" && document.querySelector('.ed-tab.is-preview[data-path="b.go"]')`)
	// A double click on the tab keeps it; the next file gets its own tab.
	Eval[bool](p, `(document.querySelector('.ed-tab[data-path="b.go"]').dispatchEvent(new MouseEvent("dblclick", { bubbles: true })), true)`)
	p.WaitJS(`!document.querySelector(".ed-tab.is-preview")`)
	p.TapJS(side("c.go"))
	open("c.go", 6)
	p.WaitJS(`[...document.querySelectorAll(".ed-tab")].map((e) => e.dataset.path).join(",") === "b.go,c.go" && document.querySelector('.ed-tab.is-preview[data-path="c.go"]')`)
}
