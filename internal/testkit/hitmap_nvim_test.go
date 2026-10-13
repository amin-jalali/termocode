package testkit

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The hit-map check (hitmap_test.go) on layouts that need the embedded
// nvim: editor tabs, the bottom panel, a side-by-side diff.

// nvimHitStep is one step of the nvim layout walk: it changes the layout
// left by the step before.
type nvimHitStep struct {
	name string
	do   func(d *Driver)
}

// dragPanelBar drags the bottom panel's tab bar by dy rows.
func dragPanelBar(d *Driver, dy int) {
	for _, t := range d.Model().HitTargets() {
		if strings.HasPrefix(t.ID, "panel:") {
			// A non-button cell of the bar row is the drag handle: the
			// cell right after the first tab.
			d.Drag(t.X+t.W+1, t.Y, t.X+t.W+1, t.Y+dy)
			return
		}
	}
}

var nvimHitSteps = []nvimHitStep{
	{"three tabs", func(d *Driver) {
		for _, f := range []string{"README.md", "docs/guide.md", "main.go"} {
			open(d, f)
		}
	}},
	{"terminal panel", func(d *Driver) { d.Action("panel:terminal") }},
	{"terminal panel taller", func(d *Driver) { dragPanelBar(d, -4) }},
	{"problems tab", func(d *Driver) { d.Action("panel:problems") }},
	{"terminal panel shorter", func(d *Driver) { dragPanelBar(d, 6) }},
	{"AI panel", func(d *Driver) { tryAIPanel(d) }},
	{"AI panel dragged", func(d *Driver) { dragAIPanel(d, -6) }},
	{"sidebar hidden", func(d *Driver) { d.Palette("toggle-explorer") }},
	{"sidebar back, wider", func(d *Driver) { d.Palette("toggle-explorer"); dragSidebar(d, 8) }},
	{"zen", func(d *Driver) { d.Palette("zen-mode") }},
	{"zen off", func(d *Driver) { d.Palette("zen-mode") }},
	{"diff view", func(d *Driver) {
		d.Action("view:git")
		d.WaitForText("CHANGES", 3*time.Second)
		scmClickText(d, "main.go")
		d.WaitForText("working", 3*time.Second)
	}},
	{"many tabs", func(d *Driver) {
		for _, f := range []string{"pkg/util.go", "docs/remove.txt", "docs/guide.md", "README.md"} {
			open(d, f)
		}
	}},
}

// Regression: in a narrow editor nvim cut the diff winbar from the left,
// so the ‹ › change buttons were gone while columns 0-5 still jumped
// between changes on click. In a tiny one the left pane is narrower than
// the buttons, and clicks on the right pane's first columns jumped too.
// Spec: Given a side-by-side diff in a narrow editor, then the ‹ › buttons
// are drawn where a click on them is taken.
func TestDiffNavButtonsInNarrowEditor(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 80, H: 24, Nvim: true, Files: scmFiles, Setup: func(tb testing.TB, dir string) {
		r := newRepo(tb, dir)
		r.commit("init")
		r.write("main.go", "package main\n\nfunc main() { println(\"new\") }\n")
	}})
	d.Action("view:git")
	d.WaitForText("CHANGES", 3*time.Second)
	dragSidebar(d, 8)
	scmClickText(d, "main.go")
	if !d.WaitForText("working", 3*time.Second) {
		t.Fatalf("no diff view:\n%s", screen(d))
	}
	var found bool
	for _, h := range d.Model().HitTargets() {
		found = found || h.ID == "diff:prev"
	}
	if !found {
		t.Fatal("no diff:prev target")
	}
	checkHits(t, d, "narrow diff")
	// Narrower still (the AI panel takes most of the room): the left pane
	// has no room for the buttons, so its first columns are not buttons.
	dragAIPanel(d, -6)
	d.Action("toasts:clear")
	d.Settle(100 * time.Millisecond)
	checkHits(t, d, "tiny diff")
}

// Spec: with a real nvim (editor tabs, the bottom panel at several
// heights, the Problems tab, a side-by-side diff), each click target's
// label is drawn in its rect and a click on it reaches it.
func TestHitMapLayoutsNvim(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	for _, size := range [][2]int{{80, 24}, {120, 40}, {200, 50}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			d := New(t, Options{W: size[0], H: size[1], Nvim: true, Files: scmFiles, Setup: func(tb testing.TB, dir string) {
				r := newRepo(tb, dir)
				r.commit("init")
				r.write("main.go", "package main\n\nfunc main() { println(\"new\") }\n")
			}})
			for _, s := range nvimHitSteps {
				s.do(d)
				d.Action("toasts:clear")
				d.Settle(100 * time.Millisecond)
				checkHits(t, d, s.name)
			}
		})
	}
}
