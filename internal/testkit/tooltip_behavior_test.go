package testkit

import (
	"fmt"
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
	if !d.WaitFor(func() bool { return tipShown(d) }, 3*time.Second) {
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

	// Move to another target: gone at once.
	hoverTarget(t, d, "ai:new")
	o := targetByID(t, d, "overflow:glyph")
	d.Move(o.X, o.Y)
	if tipShown(d) || strings.Contains(screen(d), "New chat") {
		t.Errorf("label stays after moving to another target:\n%s", screen(d))
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

// Spec: no label over the editor text or on empty chrome.
func TestTooltipNotOnText(t *testing.T) {
	d := New(t, Options{W: 120, H: 40, Git: true})
	d.Action("toasts:clear")
	_, y, _ := find(d, "main.go")
	d.Move(60, 20) // welcome page body, between the cards
	d.WaitFor(func() bool { return tipShown(d) }, 800*time.Millisecond)
	if tipShown(d) {
		t.Errorf("label shows over the editor:\n%s", screen(d))
	}
	d.Move(10, y) // an explorer row: text, not an icon
	d.WaitFor(func() bool { return tipShown(d) }, 800*time.Millisecond)
	if tipShown(d) {
		t.Errorf("label shows over an explorer row:\n%s", screen(d))
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
		for _, h := range d.Model().HitTargets() {
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
				if gx, gy, ok := tipRow(d, strings.TrimSpace(text)); !ok || gy != y || gx < x {
					t.Errorf("label %q not drawn at (%d,%d) (found %v at %d,%d):\n%s", text, x, y, ok, gx, gy, screen(d))
				}
				d.Move(0, d.H/2) // off the target: the next one starts clean
			})
		}
	}
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
