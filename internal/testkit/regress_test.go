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
