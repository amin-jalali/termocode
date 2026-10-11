package testkit

import (
	"testing"
)

// Regressions found by the walk and monkey tests. Each one replays the
// short step list that broke a frame invariant.

// The floating find / replace panel has a minimum width; in a narrow
// editor pane it stuck out past the window edge (monkey seed 3).
func TestFindPanelInNarrowEditor(t *testing.T) {
	for _, w := range []int{62, 70, 80} {
		d := New(t, Options{W: 150, H: 30, Git: true})
		d.Key("ctrl+h")
		d.Resize(w, 30)
		d.Key("ctrl+f")
		d.Resize(w-1, 29)
	}
}

// The sidebar dragged wide in a big window, then the window shrank
// (monkey seed 8).
func TestWideSidebarInNarrowWindow(t *testing.T) {
	d := New(t, Options{W: 160, H: 40, Git: true})
	d.Action("view:git")
	for x := 30; x < 45; x++ { // the splitter is somewhere here
		d.Drag(x, 20, 100, 20)
	}
	for _, w := range []int{120, 80, 61} {
		d.Resize(w, 30)
	}
	d.Resize(160, 40)
}

// The AI chat panel opened in a wide window, then the window shrank:
// sidebar + editor + chat no longer fit (monkey seeds 2 and 4).
func TestAIPanelInNarrowWindow(t *testing.T) {
	d := New(t, Options{W: 160, H: 50, Git: true})
	d.Key("alt+a")
	// Drag the panel's splitter far left (try the columns where it may be).
	for x := d.W - 50; x < d.W-20; x += 2 {
		d.Drag(x, 20, 40, 20)
	}
	for _, w := range []int{120, 80, 66, 60, 50, 45, 40} {
		d.Resize(w, 30)
	}
}
