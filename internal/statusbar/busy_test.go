package statusbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Spec (UI review signature #4): while work streams in (the AI answering)
// a small comet next to the project pill moves one step per event. It is
// drawn from the event count, never from a timer, and is gone when idle.
func TestBusyComet(t *testing.T) {
	m := New()
	base := State{Project: "p", Path: "/p/a.go", Line: 1, Col: 1}
	idle := ansi.Strip(m.renderLeft(base))
	s := base
	s.Busy, s.BusyStep = true, 0
	a := ansi.Strip(m.renderLeft(s))
	s.BusyStep = 1
	b := ansi.Strip(m.renderLeft(s))
	if a == idle || a == b {
		t.Errorf("comet missing or not moving:\nidle %q\nstep0 %q\nstep1 %q", idle, a, b)
	}
	if ansi.StringWidth(a) != ansi.StringWidth(b) {
		t.Errorf("comet changes the width: %d vs %d", ansi.StringWidth(a), ansi.StringWidth(b))
	}
	if !strings.HasPrefix(a, strings.TrimRight(idle, " ")) {
		t.Errorf("comet should follow the pill: %q", a)
	}
}
