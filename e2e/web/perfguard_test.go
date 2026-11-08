//go:build browser

package web

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Perf guards (web perf): budgets the page must hold in a real Chrome.
// TestPerfReport (perfwalk_test.go) logs the numbers behind them.

// What goes over the wire: the page on a desktop and on a phone, and the
// editor the desktop loads at its first file. The page is sent anew on
// every load (no-store), so it must be packed: unpacked it was 0.84 MB
// (desktop) and 0.57 MB (phone), and the editor 4.6 MB, 1.2 s on a
// 50 Mbit/s Wi-Fi.
const (
	wireDesktopPage = 300 << 10
	wirePhonePage   = 220 << 10
	wireEditor      = 1400 << 10
)

func TestPerfGuardWireBytes(t *testing.T) {
	for _, vp := range []Viewport{Desktop, Phone} {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			r := StartPerf(p)
			p.Open()
			n, urls := r.Fetched(time.Time{})
			budget := wirePhonePage
			if vp.W >= 1200 {
				budget = wireDesktopPage
			}
			t.Logf("page: %d B over the wire", n)
			if n > budget {
				t.Errorf("the page took %d B over the wire, budget %d B: %v", n, budget, urls)
			}
			if vp.W < 1200 {
				return
			}
			c := p.LoginHere("c-wire")
			since := time.Now()
			openBufAs(t, p, c, 2, "a.go", nil, "package a")
			p.WaitJS(deskDoc + `?.querySelector(".view-lines .view-line")`)
			n, urls = r.Fetched(since)
			t.Logf("first file open: %d B over the wire", n)
			if n > wireEditor {
				t.Errorf("the first file open took %d B over the wire, budget %d B: %v", n, wireEditor, urls)
			}
		})
	}
}

// treeRowFn is a piece of the tree row's own source (ui/rows.tsx TreeRow).
const treeRowFn = "tree-row depth-"

// The explorer with a big folder open (1,900 rows): a change draws only
// the rows it changes, and nothing else the workbench does draws the
// tree again. Before, every tree delta, file open or tab change drew all
// 1,900 rows (18 ms at 1x, 65 ms at 4x CPU, and 1,700 DOM writes each:
// the file rows' empty chevron text was set again every time).
func TestPerfGuardTreeDrawsOnlyWhatChanged(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	calls := StartCalls(p)
	r := StartPerf(p)
	c := p.Login("c-tree")
	c.Send(t, Snap("tree", 1, bigTree(200)))
	p.WaitJS(treeRow("dir000"))
	p.TapJS(treeRow("dir000"))
	p.WaitJS(treeRow("dir000/inner1499.ts"))
	drain(c, 100*time.Millisecond)
	settle := func() { r.Measure("settle", func() {}) }
	seq := uint64(1)
	delta := func(path string, entries ...map[string]any) {
		seq++
		c.Send(t, Delta("tree", seq, dirDelta(path, false, entries...)))
	}
	rootWith := func(extra ...map[string]any) []map[string]any {
		var es []map[string]any
		for i := range 200 {
			es = append(es, entry(fmt.Sprintf("dir%03d", i), true))
		}
		for i := range 200 {
			es = append(es, entry(fmt.Sprintf("file%03d.go", i), false))
		}
		return append(es, extra...)
	}
	mut := func() int { return Eval[int](p, `window.__tcMut`) }

	// A folder that is not open changes: no row is drawn.
	m0 := mut()
	if n := calls.Count(treeRowFn, func() {
		delta("dir001", entry("new.go", false))
		settle()
	}); n != 0 {
		t.Errorf("a change in a closed folder drew %d tree rows, want 0", n)
	}
	if d := mut() - m0; d > 20 {
		t.Errorf("a change in a closed folder made %d DOM changes, want at most 20", d)
	}
	// A file gets a git letter: that row alone.
	if n := calls.Count(treeRowFn, func() {
		delta("", append(rootWith()[:399:399], entry("file199.go", false, "git=M"))...)
		p.WaitJS(treeRow("file199.go") + `?.querySelector(".git-badge")`)
		settle()
	}); n != 1 {
		t.Errorf("one file's git letter drew %d tree rows, want 1", n)
	}
	// No long task while a change of 1,500 names comes in (one new).
	sp := r.Measure("tree delta", func() {
		var es []map[string]any
		for i := range 1500 {
			es = append(es, entry(fmt.Sprintf("inner%03d.ts", i), false))
		}
		delta("dir000", append(es, entry("zz.go", false))...)
		p.WaitJS(`document.querySelector('.wb-side .tree-row[data-path="dir000/zz.go"]')`)
	})
	if sp.MaxLong() > 50 {
		t.Errorf("a tree change took a %d ms task (budget 50 ms)", sp.MaxLong())
	}
	// The workbench draws again (a file opens in the editor): the tree stays.
	if n := calls.Count(treeRowFn, func() {
		openBufAs(t, p, c, 2, "file000.go", nil, "package a")
		settle()
	}); n != 0 {
		t.Errorf("opening a file drew %d tree rows, want 0", n)
	}
}

// The explorer lays out only the rows in view: with 1,900 names the rest
// wait until they scroll in (content-visibility), so showing the
// explorer again or opening the big folder is not one long task (113 ms
// and 102 ms at 1x before, 430 ms at 4x CPU).
func TestPerfGuardTreeLaysOutOnlyWhatIsInView(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	r := StartPerf(p)
	c := p.Login("c-tree")
	c.Send(t, Snap("tree", 1, bigTree(200)))
	p.WaitJS(treeRow("dir000"))
	sp := r.Measure("open a folder of 1500", func() {
		p.TapJS(treeRow("dir000"))
		p.WaitJS(treeRow("dir000/inner1499.ts"))
	})
	t.Log(sp)
	const far = `document.querySelector('.wb-side .tree-row[data-path="dir000/inner1400.ts"]')`
	const near = `document.querySelector('.wb-side .tree-row[data-path="dir000"]')`
	if !Eval[bool](p, near+`.checkVisibility({contentVisibilityAuto: true})`) {
		t.Fatal("a row in view is not drawn")
	}
	if Eval[bool](p, far+`.checkVisibility({contentVisibilityAuto: true})`) {
		t.Error("a row 1,400 rows down is laid out while out of view")
	}
	// It is there for a reader, and drawn once it scrolls in.
	if !strings.Contains(Eval[string](p, far+`.textContent`), "inner1400.ts") {
		t.Errorf("the far row reads %q", Eval[string](p, far+`.textContent`))
	}
	Eval[bool](p, `(`+far+`.scrollIntoView({block: "center"}), true)`)
	p.WaitJS(far + `.checkVisibility({contentVisibilityAuto: true})`)
}
