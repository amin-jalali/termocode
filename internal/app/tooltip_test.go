package app

import "testing"

// Spec: a hover label stays fully on screen and off its target: at the
// right edge it ends at the pointer, at the bottom it goes above, and a
// full-height target gets it beside.
func TestPlaceTooltip(t *testing.T) {
	const sw, sh = 80, 24
	cases := []struct {
		name   string
		w, h   int
		px, py int
		target hitTarget
		x, y   int
		ok     bool
	}{
		{"below, from the pointer", 10, 1, 5, 0, hitTarget{X: 5, Y: 0, W: 1, H: 1}, 5, 1, true},
		{"right edge flips left", 20, 1, 79, 0, hitTarget{X: 79, Y: 0, W: 1, H: 1}, 60, 1, true},
		{"bottom edge goes above", 10, 1, 40, 23, hitTarget{X: 38, Y: 23, W: 5, H: 1}, 40, 22, true},
		{"bottom-right corner", 30, 1, 79, 23, hitTarget{X: 70, Y: 23, W: 10, H: 1}, 50, 22, true},
		{"two-row icon at the bottom", 10, 1, 1, 23, hitTarget{X: 0, Y: 22, W: 4, H: 2}, 1, 21, true},
		{"full height: beside", 10, 1, 3, 12, hitTarget{X: 3, Y: 0, W: 1, H: sh}, 4, 12, true},
		{"full height at the right: beside, left", 10, 1, 79, 12, hitTarget{X: 79, Y: 0, W: 1, H: sh}, 69, 12, true},
		{"wider than the screen", 81, 1, 0, 0, hitTarget{W: 1, H: 1}, 0, 0, false},
		// Two rows (a help line): below while both fit, else above.
		{"two rows below", 13, 2, 5, 0, hitTarget{X: 5, Y: 0, W: 1, H: 1}, 5, 1, true},
		{"two rows above at the bottom", 13, 2, 40, 23, hitTarget{X: 38, Y: 23, W: 5, H: 1}, 40, 21, true},
		{"two rows, one row left below", 13, 2, 40, 22, hitTarget{X: 38, Y: 22, W: 5, H: 1}, 40, 20, true},
		{"two rows beside, kept on screen", 13, 2, 3, 23, hitTarget{X: 3, Y: 0, W: 1, H: sh}, 4, 22, true},
	}
	for _, c := range cases {
		x, y, ok := placeTooltip(c.w, c.h, c.px, c.py, c.target, sw, sh)
		if x != c.x || y != c.y || ok != c.ok {
			t.Errorf("%s: got (%d,%d,%v), want (%d,%d,%v)", c.name, x, y, ok, c.x, c.y, c.ok)
		}
		if ok && (x < 0 || x+c.w > sw || y < 0 || y+c.h > sh) {
			t.Errorf("%s: off screen", c.name)
		}
	}
}

// Spec: every label that names a palette command gets its key from the
// keymap, so the palette id must run a keymap action.
func TestTooltipPaletteIDs(t *testing.T) {
	for id, s := range tooltipLabels {
		if s.palette == "" {
			continue
		}
		if _, ok := paletteActions[s.palette]; !ok {
			t.Errorf("%s: palette id %q runs no keymap action", id, s.palette)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestModel(t)
	if got := m.tooltipText("actions:close"); got != "Close panel (Alt+A)" {
		t.Errorf("actions:close label = %q", got)
	}
	if got := m.tooltipText("close:7"); got != "Close (Ctrl+W)" {
		t.Errorf("close:7 label = %q", got)
	}
}

// Spec: a stale delay tick (from before a click, key or scroll) does
// nothing.
func TestTooltipStaleTick(t *testing.T) {
	var m Model
	m.tip.pending = true
	gen := m.tip.gen
	m.tip.hide()
	if cmd := m.onTooltipTick(tooltipTickMsg{gen}); cmd != nil || m.tip.shown {
		t.Errorf("stale tick acted: cmd=%v shown=%v", cmd != nil, m.tip.shown)
	}
}
