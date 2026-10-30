//go:build browser

package web

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// The workbench's grid as the laptop's editor has it: a double click on a
// sash puts its default size back; the panel maximizes (the editor hides)
// and restores; it moves right of the editor and back; where it is and
// whether it is maximized stay for the next visit; the editor's actions
// sit at the end of the tab bar.

func TestPanelMaximizePositionAndSashReset(t *testing.T) {
	p, c := openFiles(t, Wide)
	openDesk(t, p, c, 2, "a.go", "package a")
	// The editor's actions are in the tab bar, on the tabs' row.
	if !Eval[bool](p, `(() => { const a = document.querySelector(".ed-tabbar .ed-actions .view-actions button"); const tab = document.querySelector(".ed-tab"); return !!a && !!tab && Math.abs(a.getBoundingClientRect().top + a.offsetHeight / 2 - (tab.getBoundingClientRect().top + 17.5)) < 3; })()`) {
		t.Error("the editor's actions are not on the tab bar")
	}
	// Double click on the side bar's edge: back to 264 px.
	Eval[bool](p, `(document.querySelector(".sash-x").focus(), true)`)
	p.Key(kb.ArrowRight + kb.ArrowRight)
	p.WaitJS(`document.querySelector(".wb-side").getBoundingClientRect().width === 296`)
	r := Eval[[]float64](p, `(() => { const r = document.querySelector(".sash-x").getBoundingClientRect(); return [r.left + r.width / 2, r.top + 200]; })()`)
	p.Do(chromedp.MouseClickXY(r[0], r[1], chromedp.ClickCount(2)))
	p.WaitJS(`document.querySelector(".wb-side").getBoundingClientRect().width === 264`)

	// The panel: shown, maximized (no editor), restored.
	layoutKey(p, "j")
	p.WaitJS(`document.querySelector(".wb-panel")`)
	p.TapJS(`document.querySelector('.wb-panel [data-act="panel-max"]')`)
	p.WaitJS(`document.querySelector(".wb.is-panel-max") && document.querySelector(".wb-editor").offsetHeight === 0 && document.querySelector(".wb-panel").getBoundingClientRect().top < 60`)
	p.TapJS(`document.querySelector('.wb-panel [data-act="panel-max"]')`)
	p.WaitJS(`!document.querySelector(".wb.is-panel-max") && document.querySelector(".wb-editor").offsetHeight > 200`)
	// Moved right of the editor, from the panel's … menu.
	p.TapJS(`document.querySelector('.wb-panel [data-act="panel-more"]')`)
	p.TapJS(`[...document.querySelectorAll('[role="menuitem"]')].find((b) => b.textContent.includes("Move Panel Right"))`)
	p.WaitJS(`(() => { const e = document.querySelector(".wb-editor").getBoundingClientRect(), q = document.querySelector(".wb-panel").getBoundingClientRect(), ch = document.querySelector(".chatcol").getBoundingClientRect(); return q.left >= e.right - 1 && q.right <= ch.left + 1 && q.top <= e.top + 1; })()`)
	// It stays there after a reload.
	Eval[bool](p, `(location.reload(), true)`)
	p.WaitJS(`document.querySelector(".wb.is-panel-right .wb-panel")`)
	if w := Eval[float64](p, `document.querySelector(".wb-panel").getBoundingClientRect().width`); w < 400 {
		t.Errorf("the right panel is %v px wide", w)
	}
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}
