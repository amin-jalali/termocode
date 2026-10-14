package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Hover labels (tooltips): when the pointer rests on an icon button for
// tooltipDelay, a one-row label with the button's name and key shows
// near it ("New chat (Ctrl+N)"). The button under the pointer comes from
// the hit map (hit_targets.go), the same one the click tests check, so
// the label always names the button a click would reach.
//
// Cost: a mouse motion only stores the time (and, while a label shows,
// checks the pointer is still in its target's rect). One tea.Tick is in
// flight at most; when it fires before the pointer has rested long
// enough it waits for the rest. Clicks, keys, scrolls and resizes bump
// the generation, so a tick from before them does nothing.

// tooltipDelay is how long the pointer must rest before the label shows.
const tooltipDelay = 500 * time.Millisecond

// tooltipState is the hover-label part of the Model.
type tooltipState struct {
	gen     int       // bumped to cancel the tick in flight
	pending bool      // a tick of gen is in flight
	moved   time.Time // last pointer motion
	shown   bool
	text    string    // the label row, padding included (plain)
	key     string    // the key part of text ("" = none), drawn dimmer
	x, y    int       // screen cell of the label's first column
	target  hitTarget // the button it names
}

// tooltipTickMsg fires tooltipDelay after a motion; gen ties it to the
// state that scheduled it.
type tooltipTickMsg struct{ gen int }

func tooltipTick(gen int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tooltipTickMsg{gen} })
}

// hide drops the label and cancels the tick in flight.
func (t *tooltipState) hide() {
	t.shown = false
	t.pending = false
	t.gen++
}

// tooltipOnInput updates the hover label for a user event: a plain
// motion (re)starts the rest timer; anything else hides it.
func (m *Model) tooltipOnInput(msg tea.Msg) tea.Cmd {
	t := &m.tip
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Action != tea.MouseActionMotion || msg.Button != tea.MouseButtonNone {
			if t.shown || t.pending {
				t.hide()
			}
			return nil
		}
		if t.shown {
			r := t.target
			if msg.X >= r.X && msg.X < r.X+r.W && msg.Y >= r.Y && msg.Y < r.Y+r.H {
				return nil // still on the same button
			}
			t.shown = false
		}
		t.moved = time.Now()
		if t.pending {
			return nil // the tick in flight waits for the rest
		}
		t.pending = true
		return tooltipTick(t.gen, tooltipDelay)
	case tea.KeyMsg, tea.WindowSizeMsg:
		if t.shown || t.pending {
			t.hide()
		}
	}
	return nil
}

// onTooltipTick shows the label once the pointer has rested long enough.
func (m *Model) onTooltipTick(msg tooltipTickMsg) tea.Cmd {
	t := &m.tip
	if msg.gen != t.gen || !t.pending {
		return nil // stale: a click, key or scroll came after it
	}
	if left := tooltipDelay - time.Since(t.moved); left > 0 {
		return tooltipTick(t.gen, left)
	}
	t.pending = false
	m.showTooltipAt(m.hoverX, m.hoverY)
	return nil
}

// showTooltipAt shows the label of the button at (x, y), if it has one.
func (m *Model) showTooltipAt(x, y int) {
	target, ok := m.tooltipTargetAt(x, y)
	if !ok {
		return
	}
	name, key := m.tooltipLabel(target.ID)
	if name == "" {
		return
	}
	text := " " + name + " "
	if key != "" {
		text = " " + name + " (" + key + ") "
	}
	if w := ansi.StringWidth(text); w > m.w {
		text, key = ansi.Truncate(text, m.w, "…"), ""
	}
	lx, ly, ok := placeTooltip(ansi.StringWidth(text), x, y, target, m.w, m.h)
	if !ok {
		return
	}
	m.tip.shown, m.tip.text, m.tip.key = true, text, key
	m.tip.x, m.tip.y, m.tip.target = lx, ly, target
}

// tooltipTargetAt is the hit target under (x, y): the smallest one when
// they nest (a tab's close button sits inside the tab).
func (m Model) tooltipTargetAt(x, y int) (hitTarget, bool) {
	var best hitTarget
	found := false
	for _, t := range m.hitTargets() {
		if x < t.X || x >= t.X+t.W || y < t.Y || y >= t.Y+t.H {
			continue
		}
		if !found || t.W*t.H < best.W*best.H {
			best, found = t, true
		}
	}
	return best, found
}

// placeTooltip puts a w-wide label for target near the pointer (px, py):
// on the row below the target, or above it at the bottom edge; starting
// at the pointer, or ending there at the right edge. A target as tall as
// the screen gets the label beside it. ok is false when it does not fit.
func placeTooltip(w, px, py int, target hitTarget, sw, sh int) (x, y int, ok bool) {
	if w <= 0 || w > sw || sh <= 0 {
		return 0, 0, false
	}
	switch {
	case target.Y+target.H < sh:
		y = target.Y + target.H
	case target.Y-1 >= 0:
		y = target.Y - 1
	default: // beside it, on the pointer's row
		switch {
		case target.X+target.W+w <= sw:
			return target.X + target.W, py, true
		case target.X-w >= 0:
			return target.X - w, py, true
		}
		return 0, 0, false
	}
	x = px
	if x+w > sw {
		x = px - w + 1 // flip: end at the pointer
	}
	x = max(0, min(x, sw-w))
	return x, y, true
}

// overlayTooltip draws the label over the frame.
func (m Model) overlayTooltip(base string) string {
	t := m.tip
	if !t.shown || t.y < 0 || t.y >= m.h {
		return base
	}
	lines := strings.Split(base, "\n")
	if t.y >= len(lines) {
		return base
	}
	c := theme.Float()
	body := lipgloss.NewStyle().Background(c.Bg).Foreground(c.Text)
	row := body.Render(t.text)
	if t.key != "" {
		if name, rest, ok := strings.Cut(t.text, "("+t.key+")"); ok {
			row = body.Render(name) + lipgloss.NewStyle().Background(c.Bg).Foreground(c.Hint).Render("("+t.key+")") + body.Render(rest)
		}
	}
	lines[t.y] = spliceAt(lines[t.y], row, t.x)
	return strings.Join(lines, "\n")
}
