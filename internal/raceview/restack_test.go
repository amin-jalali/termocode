package raceview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Spec (CMP-56): b restacks the selected finished lane (puts its work on
// top of your current commit as clean commits); on a working lane it says
// so and does nothing; the wide hint line shows the key.
func TestCompareRestackKey(t *testing.T) {
	c := NewCompare(demoRace())
	c.SetSize(150, 42)
	if a, _ := run(c.Key(key("b"))).(ActionMsg); a != (ActionMsg{Action: ActRestack, Lane: "race-3-1-claude"}) {
		t.Errorf("b %+v", a)
	}
	if out := ansi.Strip(c.Box()); !strings.Contains(out, "restack") {
		t.Errorf("no restack hint:\n%s", out)
	}
	c.Key(key("down"))
	c.Key(key("down"))
	if a, _ := run(c.Key(key("b"))).(ActionMsg); a.Action != ActNone || !strings.Contains(ansi.Strip(c.Box()), "still working") {
		t.Errorf("b on a working lane %+v", a)
	}
}
