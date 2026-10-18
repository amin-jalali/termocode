package testkit

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
)

// Hover labels: the pointer rests on a button for about half a second
// and a one-row label with the button's name (and key) shows near it.

// targetByID is the hit target with id in the current state.
func targetByID(t *testing.T, d *Driver, id string) app.HitTarget {
	t.Helper()
	for _, h := range d.Model().HitTargets() {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("no hit target %q:\n%s", id, screen(d))
	return app.HitTarget{}
}

// tipShown reports whether the hover label is on screen.
func tipShown(d *Driver) bool {
	_, _, _, ok := d.Model().Tooltip()
	return ok
}

// hoverTarget rests the pointer on the centre of target id until its
// label shows, and returns the target.
func hoverTarget(t *testing.T, d *Driver, id string) app.HitTarget {
	t.Helper()
	h := targetByID(t, d, id)
	d.Move(h.X+h.W/2, h.Y+h.H/2)
	// The label of this target: the one of the last target may stay a
	// moment (the grace of a label with a help line).
	want := d.Model().TooltipFor(id)
	if !d.WaitFor(func() bool {
		text, _, _, ok := d.Model().Tooltip()
		return ok && strings.Contains(text, want)
	}, 3*time.Second) {
		t.Fatalf("no hover label on %s after 3 s:\n%s", id, screen(d))
	}
	return h
}

// tipRow finds text in the frame and reports where it is drawn.
func tipRow(d *Driver, text string) (x, y int, ok bool) { return find(d, text) }

// Spec: Given the AI panel, when the pointer rests on its + button, then
// after a short delay a label "New chat (Ctrl+N)" shows near it, on
// another row (the button stays visible). Moving away, a click, a key
// or a scroll hides it.
func TestTooltipShowsAndHides(t *testing.T) {
	d := New(t, Options{W: 120, H: 40, Git: true})
	if !tryAIPanel(d) {
		t.Fatal("no AI panel")
	}
	d.Action("toasts:clear")
	h := targetByID(t, d, "ai:new")
	d.Move(h.X, h.Y)
	if tipShown(d) || strings.Contains(screen(d), "New chat") {
		t.Fatalf("the label shows at once, want it after a delay:\n%s", screen(d))
	}
	hoverTarget(t, d, "ai:new")
	x, y, ok := tipRow(d, "New chat (Ctrl+N)")
	if !ok {
		t.Fatalf("label text not drawn:\n%s", screen(d))
	}
	if y >= h.Y && y < h.Y+h.H {
		t.Errorf("label on row %d covers the button's row:\n%s", y, screen(d))
	}
	if x < 0 || x+len("New chat (Ctrl+N)") > d.W {
		t.Errorf("label at x=%d is off screen", x)
	}
	if got := cellAt(d, h.X, h.Y); got != "+" {
		t.Errorf("the button shows %q under the label, want +", got)
	}

	// The keymap gives the key: Alt+A toggles the panel.
	hoverTarget(t, d, "actions:close")
	if _, _, ok := tipRow(d, "(Alt+A)"); !ok {
		t.Errorf("close button label has no Alt+A:\n%s", screen(d))
	}

	// Move to another target: a label without a help line goes at once.
	hoverTarget(t, d, "status:pos")
	o := targetByID(t, d, "overflow:glyph")
	d.Move(o.X, o.Y)
	if tipShown(d) || strings.Contains(screen(d), "Line and column") {
		t.Errorf("label stays after moving to another target:\n%s", screen(d))
	}
	// A label with a help line stays for the grace, then the tick swaps
	// it for the new button's label: never two labels.
	hoverTarget(t, d, "ai:new")
	d.Move(o.X, o.Y)
	if !d.WaitFor(func() bool { return strings.Contains(screen(d), "More actions") }, 3*time.Second) {
		t.Fatalf("no label on the next button:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "New chat") {
		t.Errorf("two labels on screen:\n%s", screen(d))
	}

	// Small moves inside the same target keep it.
	a := hoverTarget(t, d, "activity:Explorer")
	d.Move(a.X+a.W-1, a.Y+a.H-1)
	if !tipShown(d) {
		t.Errorf("label hides on a move inside the same target")
	}

	for _, c := range []struct {
		name string
		do   func(h app.HitTarget)
	}{
		{"click", func(h app.HitTarget) { d.Click(h.X, h.Y) }},
		{"key", func(app.HitTarget) { d.Key("right") }},
		{"scroll", func(h app.HitTarget) { d.Wheel(h.X, h.Y, false) }},
	} {
		h := hoverTarget(t, d, "overflow:glyph")
		c.do(h)
		if tipShown(d) || strings.Contains(screen(d), "More actions") {
			t.Errorf("%s: label stays:\n%s", c.name, screen(d))
		}
		// A stale delay tick must not bring it back.
		d.WaitFor(func() bool { return tipShown(d) }, 700*time.Millisecond)
		if tipShown(d) {
			t.Errorf("%s: label came back without a move", c.name)
		}
		if c.name == "click" {
			d.Key("esc") // close the menu the click opened
		}
	}
}

// Spec: no label over the welcome page body or editor text; Explorer rows
// show their kind label and More info line.
func TestTooltipNotOnText(t *testing.T) {
	d := New(t, Options{W: 120, H: 40, Git: true})
	d.Action("toasts:clear")
	_, y, _ := find(d, "main.go")
	d.Move(60, 20) // welcome page body, between the cards
	d.WaitFor(func() bool { return tipShown(d) }, 800*time.Millisecond)
	if tipShown(d) {
		t.Errorf("label shows over the editor:\n%s", screen(d))
	}
	var row app.HitTarget
	for _, h := range d.Model().HitTargets() {
		if strings.HasPrefix(h.ID, "explorer:") && h.Y == y && h.X <= 10 && 10 < h.X+h.W {
			row = h
			break
		}
	}
	if row.ID == "" {
		t.Fatalf("no Explorer row at main.go row %d:\n%s", y, screen(d))
	}
	want := d.Model().TooltipFor(row.ID)
	d.Move(10, y)
	if !d.WaitFor(func() bool {
		text, _, _, ok := d.Model().Tooltip()
		_, _, _, _, help := d.Model().TooltipHelp()
		return ok && strings.Contains(text, want) && help
	}, 3*time.Second) {
		t.Errorf("Explorer row %s did not show its kind label and More info:\n%s", row.ID, screen(d))
	}
}

// cellAt is the plain text of screen cell (x, y).
func cellAt(d *Driver, x, y int) string {
	rows := strings.Split(d.Frame(), "\n")
	if y < 0 || y >= len(rows) {
		return ""
	}
	return ansi.Strip(ansi.Cut(rows[y], x, x+1))
}

// Spec: a label for a target at the right or bottom edge flips left / up
// and stays fully on screen, never over the target.
func TestTooltipStaysOnScreen(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {120, 40}} {
		d := New(t, Options{W: size[0], H: size[1], Git: true})
		tryAIPanel(d)
		d.Action("toasts:clear")
		// Git status loads in the background and reshapes the status bar
		// (slow runners: after the targets were read). Read them once the
		// layout stopped changing.
		for _, h := range stableTargets(t, d) {
			if d.Model().TooltipFor(h.ID) == "" {
				continue
			}
			edge := h.X+h.W >= d.W-2 || h.Y+h.H >= d.H || h.Y == 0 || h.X == 0
			if !edge {
				continue
			}
			t.Run(fmt.Sprintf("%dx%d/%s", d.W, d.H, h.ID), func(t *testing.T) {
				hoverTarget(t, d, h.ID)
				text, x, y, _ := d.Model().Tooltip()
				w := ansi.StringWidth(text)
				if x < 0 || y < 0 || x+w > d.W || y >= d.H {
					t.Errorf("label %q at (%d,%d) w=%d is off a %dx%d screen", text, x, y, w, d.W, d.H)
				}
				if y >= h.Y && y < h.Y+h.H && x < h.X+h.W && x+w > h.X {
					t.Errorf("label %q at (%d,%d) covers its target %+v", text, x, y, h)
				}
				frameRows := strings.Split(d.Frame(), "\n")
				if y >= len(frameRows) || strings.TrimSpace(ansi.Strip(ansi.Cut(frameRows[y], x, x+w))) != strings.TrimSpace(text) {
					t.Errorf("label %q not drawn at (%d,%d):\n%s", text, x, y, screen(d))
				}
				if ht, hx, hy, _, ok := d.Model().TooltipHelp(); ok {
					if hy >= d.H || hx+ansi.StringWidth(ht) > d.W {
						t.Errorf("help line %q at (%d,%d) is off screen", ht, hx, hy)
					}
					if hy >= h.Y && hy < h.Y+h.H && hx < h.X+h.W && hx+ansi.StringWidth(ht) > h.X {
						t.Errorf("help line at (%d,%d) covers its target %+v", hx, hy, h)
					}
					if got := strings.TrimSpace(ansi.Strip(ansi.Cut(strings.Split(d.Frame(), "\n")[hy], hx, hx+ansi.StringWidth(ht)))); got != strings.TrimSpace(ht) {
						t.Errorf("help line drawn as %q, want %q", got, strings.TrimSpace(ht))
					}
				}
				d.Move(0, d.H/2) // off the target: the next one starts clean
			})
		}
	}
}

// stableTargets returns the hit targets once two reads 300 ms apart are
// the same (background loads such as git status have landed).
func stableTargets(t *testing.T, d *Driver) []app.HitTarget {
	t.Helper()
	prev := d.Model().HitTargets()
	var cur []app.HitTarget
	stable := d.WaitFor(func() bool {
		d.WaitFor(func() bool { return false }, 300*time.Millisecond) // pump a full 300 ms
		cur = d.Model().HitTargets()
		same := slices.Equal(prev, cur)
		prev = cur
		return same
	}, 10*time.Second)
	if !stable {
		t.Fatalf("hit targets kept changing for 10 s")
	}
	return cur
}

// iconOnly reports whether a hit target is a button without a word on
// it, which needs a hover label to say what it does.
func iconOnly(h app.HitTarget) bool {
	for _, p := range []string{
		"activity:", "overflow:glyph", "actions:launcher", "actions:pin", "actions:close",
		"ai:new", "ai:history", "panel:new", "panel:maximize", "panel:minimize", "panel:close",
		"panel:term-close:", "panel:tab-close:", "close:", "tabs:", "scm:toggle-view",
		"scm:btn:sync", "scm:btn:more", "find:", "diff:",
	} {
		if strings.HasPrefix(h.ID, p) {
			return true
		}
	}
	if strings.HasPrefix(h.ID, "status:") {
		return !strings.HasPrefix(h.ID, "status:ext:") // extension text
	}
	// Any other one- or two-cell glyph is an icon too.
	w := ansi.StringWidth(h.Label)
	return h.Label != "" && w <= 2 && !strings.Contains(h.ID, "splitter")
}

// checkLabels fails t for every icon-only target without a hover label.
func checkLabels(t *testing.T, d *Driver, where string, seen map[string]bool) {
	t.Helper()
	m := d.Model()
	for _, h := range m.HitTargets() {
		if !iconOnly(h) {
			continue
		}
		kind := h.ID
		if i := strings.LastIndexByte(kind, ':'); i > 0 && strings.Trim(kind[i+1:], "0123456789") == "" {
			kind = kind[:i]
		}
		seen[kind] = true
		if m.TooltipFor(h.ID) == "" {
			t.Errorf("%s: icon button %s (%q) has no hover label", where, h.ID, h.Label)
		}
	}
}

// Spec: every icon-only button the hit map knows has a hover label.
func TestTooltipCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, size := range hitSizes {
		for _, l := range baseHitLayouts {
			d := New(t, Options{W: size[0], H: size[1], Git: true})
			l.set(d)
			checkLabels(t, d, fmt.Sprintf("%dx%d/%s", size[0], size[1], l.name), seen)
		}
	}
	if !testing.Short() {
		d := New(t, Options{W: 200, H: 50, Nvim: true, Git: true})
		for _, s := range nvimHitSteps {
			s.do(d)
			d.Settle(100 * time.Millisecond)
			checkLabels(t, d, "nvim/"+s.name, seen)
		}
	}
	// The walk must reach the buttons named in the design.
	want := []string{"activity:Explorer", "activity:Settings", "overflow:glyph", "ai:new", "ai:history",
		"actions:pin", "actions:close", "status:branch", "status:pos", "status:encoding", "status:indent"}
	if !testing.Short() {
		want = append(want, "panel:new", "panel:close", "panel:maximize", "panel:minimize",
			"panel:term-close", "close", "status:term", "status:lsp", "scm:toggle-view", "scm:btn:sync")
	}
	for _, id := range want {
		if !seen[id] {
			t.Errorf("the walk never met %s", id)
		}
	}
}
