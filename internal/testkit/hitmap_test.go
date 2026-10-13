package testkit

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/app"
)

// The hit-map invariant: for every click target the mouse code knows
// about (app.Model.HitTargets),
//
//	(a) its label is drawn inside its rect in the frame, and
//	(b) a click at the rect's centre reaches it (app.Model.ProbeClick).
//
// The view and the mouse code compute positions in different places; when
// a panel moves or resizes they drift apart and clicks land on the wrong
// thing. This catches that for every target at once.

// rectText is the plain text of the frame cells in t's rect, one line per
// row.
func rectText(rows []string, t app.HitTarget) string {
	var lines []string
	for y := t.Y; y < t.Y+t.H && y < len(rows); y++ {
		lines = append(lines, ansi.Strip(ansi.Cut(rows[y], t.X, t.X+t.W)))
	}
	return strings.Join(lines, "\n")
}

// hitProblems lists the targets of the current frame that break the
// invariant.
func hitProblems(d *Driver) []string {
	m := d.Model()
	rows := strings.Split(d.Frame(), "\n")
	var out []string
	for _, t := range m.HitTargets() {
		where := fmt.Sprintf("%s at (%d,%d) %dx%d", t.ID, t.X, t.Y, t.W, t.H)
		if t.X < 0 || t.Y < 0 || t.X+t.W > d.W || t.Y+t.H > d.H {
			out = append(out, where+": rect is off screen")
			continue
		}
		if t.Label != "" {
			if got := rectText(rows, t); !strings.Contains(got, t.Label) {
				out = append(out, fmt.Sprintf("%s: label %q not drawn there, the frame shows %q", where, t.Label, got))
			}
		}
		cx, cy := t.X+t.W/2, t.Y+t.H/2
		if got := m.ProbeClick(cx, cy); got != t.ID {
			out = append(out, fmt.Sprintf("%s: a click at (%d,%d) reaches %q", where, cx, cy, got))
		}
	}
	return out
}

// checkHits fails t for every broken target; where names the layout.
func checkHits(t testing.TB, d *Driver, where string) bool {
	t.Helper()
	if testing.Verbose() {
		kinds := map[string]int{}
		for _, h := range d.Model().HitTargets() {
			kind, _, _ := strings.Cut(h.ID, ":")
			kinds[kind]++
		}
		t.Logf("%s (%dx%d): targets %v", where, d.W, d.H, kinds)
	}
	probs := hitProblems(d)
	if len(probs) > 0 {
		t.Errorf("%s (%dx%d): %d hit-map problems:\n  %s\nframe:\n%s",
			where, d.W, d.H, len(probs), strings.Join(probs, "\n  "), screen(d))
	}
	return len(probs) == 0
}

// hitLayout is one layout of the matrix: set changes the app from its
// start state.
type hitLayout struct {
	name string
	set  func(d *Driver)
}

func dragSidebar(d *Driver, dx int) {
	x := activity.Width + d.Model().ExplorerWidth() - 1
	d.Drag(x, 10, x+dx, 10)
}

// tryAIPanel opens the AI chat panel (Alt+A) and gives the focus back to
// the editor. In a narrow window there is no room and it stays closed.
func tryAIPanel(d *Driver) bool {
	shown := func() bool {
		_, y, ok := find(d, "AI CHAT")
		return ok && y == 0
	}
	if !shown() {
		d.Key("alt+a", "esc")
	}
	return shown()
}

func dragAIPanel(d *Driver, dx int) {
	if !tryAIPanel(d) {
		return
	}
	x := aiPanelEdge(d)
	d.Drag(x, 10, x+dx, 10)
}

// baseHitLayouts are the layouts without nvim (the editor shows the
// Welcome page).
var baseHitLayouts = []hitLayout{
	{"default", func(d *Driver) {}},
	{"sidebar hidden", func(d *Driver) { d.Palette("toggle-explorer") }},
	{"sidebar wider", func(d *Driver) { dragSidebar(d, 12) }},
	{"source control", func(d *Driver) {
		d.Action("view:git")
		d.WaitForText("Commit", 2*time.Second)
	}},
	{"AI panel", func(d *Driver) { tryAIPanel(d) }},
	{"AI panel dragged", func(d *Driver) { dragAIPanel(d, -8) }},
	{"AI panel, no sidebar", func(d *Driver) { tryAIPanel(d); d.Palette("toggle-explorer") }},
	{"zen", func(d *Driver) { d.Palette("zen-mode") }},
	{"palette", func(d *Driver) { d.Key("f1") }},
	{"confirm", func(d *Driver) {
		clickExplorer(d, "main.go")
		d.Key("delete")
	}},
}

var hitSizes = [][2]int{{60, 20}, {80, 24}, {120, 40}, {200, 50}}

// Spec: in every layout of the matrix, each click target's label is drawn
// in its rect and a click on it reaches it.
func TestHitMapLayouts(t *testing.T) {
	for _, size := range hitSizes {
		for _, l := range baseHitLayouts {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], l.name), func(t *testing.T) {
				d := New(t, Options{W: size[0], H: size[1], Git: true})
				l.set(d)
				// Toasts float over everything for a few seconds and take
				// no clicks; they would hide labels.
				d.Action("toasts:clear")
				d.Settle(50 * time.Millisecond)
				if len(d.Model().HitTargets()) == 0 {
					t.Fatalf("no hit targets:\n%s", screen(d))
				}
				checkHits(t, d, l.name)
			})
		}
	}
}
