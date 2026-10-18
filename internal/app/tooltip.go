package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Hover labels (tooltips): when the pointer rests on an icon button for
// tooltipDelay, a one-row label with the button's name and key shows
// near it ("New chat (Ctrl+N)"). The button under the pointer comes from
// the hit map (hit_targets.go), the same one the click tests check, so
// the label always names the button a click would reach.
//
// A button with a guide section (tooltipSpec.help) gets a second row,
// "ⓘ More info": a click on it opens the guide there (help.go). So the
// pointer can reach that row, the label rect counts as "still on" the
// button, and leaving both keeps the label until the tick in flight
// fires and finds the pointer still outside (the grace).
//
// Cost: a mouse motion only stores the time (and, while a label shows,
// checks the pointer is still in its target's or label's rect). One
// tea.Tick is in flight at most; when it fires before the pointer has
// rested long enough it waits for the rest. Clicks, keys, scrolls and
// resizes bump the generation, so a tick from before them does nothing.

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
	// help is the guide topic of the button ("" = no help line); when
	// set, row y+1 shows helpText, as wide as text.
	help     string
	helpText string
}

// rows is how many screen rows the label takes.
func (t *tooltipState) rows() int {
	if t.help != "" {
		return 2
	}
	return 1
}

// onLabel reports whether (x, y) is on the shown label.
func (t *tooltipState) onLabel(x, y int) bool {
	return t.shown && y >= t.y && y < t.y+t.rows() && x >= t.x && x < t.x+ansi.StringWidth(t.text)
}

// onHelpLine reports whether (x, y) is on the shown help line.
func (t *tooltipState) onHelpLine(x, y int) bool {
	return t.shown && t.help != "" && y == t.y+1 && x >= t.x && x < t.x+ansi.StringWidth(t.helpText)
}

// onTarget reports whether (x, y) is on the button the label names.
func (t *tooltipState) onTarget(x, y int) bool {
	r := t.target
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// tooltipHelpLine is the text of the help line, without padding.
func tooltipHelpLine() string { return helpIcon() + " More info" }

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
			if t.onTarget(msg.X, msg.Y) {
				return nil // still on the same button
			}
			if t.help == "" {
				t.shown = false
			} else if t.onLabel(msg.X, msg.Y) {
				return nil // on the label, on its way to the help line
			}
			// else the grace: the tick below hides it if still outside.
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
// It also ends the grace: a label the pointer left goes.
func (m *Model) onTooltipTick(msg tooltipTickMsg) tea.Cmd {
	t := &m.tip
	if msg.gen != t.gen || !t.pending {
		return nil // stale: a click, key or scroll came after it
	}
	if t.shown && !t.onTarget(m.hoverX, m.hoverY) && !t.onLabel(m.hoverX, m.hoverY) {
		t.shown = false
	}
	if left := tooltipDelay - time.Since(t.moved); left > 0 {
		return tooltipTick(t.gen, left)
	}
	t.pending = false
	if !t.shown {
		m.showTooltipAt(m.hoverX, m.hoverY)
	}
	return nil
}

// tooltipHelpPress opens the guide when msg is a left press on the shown
// help line; ok reports that the press was taken (nothing under the
// label gets it).
func (m *Model) tooltipHelpPress(msg tea.Msg) (cmd tea.Cmd, ok bool) {
	mm, isMouse := msg.(tea.MouseMsg)
	if !isMouse || mm.Action != tea.MouseActionPress || mm.Button != tea.MouseButtonLeft ||
		!m.tip.onHelpLine(mm.X, mm.Y) {
		return nil, false
	}
	l, err := help.Parse(m.tip.help)
	m.tip.hide()
	if err != nil {
		return nil, true
	}
	return m.openHelp(l), true
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
	topic, helpText := tooltipHelp(target.ID), ""
	if topic != "" {
		helpText = " " + tooltipHelpLine() + " "
		if hw := ansi.StringWidth(helpText); hw > m.w {
			topic, helpText = "", "" // no room: the label alone
		} else if w := ansi.StringWidth(text); w < hw {
			text += strings.Repeat(" ", hw-w)
		}
	}
	if w := ansi.StringWidth(text); w > m.w {
		text, key = ansi.Truncate(text, m.w, "…"), ""
	}
	w := ansi.StringWidth(text)
	rows := 1
	if topic != "" {
		helpText += strings.Repeat(" ", w-ansi.StringWidth(helpText))
		rows = 2
	}
	lx, ly, ok := placeTooltip(w, rows, x, y, target, m.w, m.h)
	if !ok {
		return
	}
	m.tip.shown, m.tip.text, m.tip.key = true, text, key
	m.tip.x, m.tip.y, m.tip.target = lx, ly, target
	m.tip.help, m.tip.helpText = topic, helpText
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

// placeTooltip puts a w-wide, h-tall label for target near the pointer
// (px, py): on the rows below the target, or above it at the bottom
// edge; starting at the pointer, or ending there at the right edge. A
// target as tall as the screen gets the label beside it. ok is false
// when it does not fit.
func placeTooltip(w, h, px, py int, target hitTarget, sw, sh int) (x, y int, ok bool) {
	if w <= 0 || w > sw || h <= 0 || h > sh {
		return 0, 0, false
	}
	switch {
	case target.Y+target.H+h <= sh:
		y = target.Y + target.H
	case target.Y-h >= 0:
		y = target.Y - h
	default: // beside it, from the pointer's row
		y = min(py, sh-h)
		switch {
		case target.X+target.W+w <= sw:
			return target.X + target.W, y, true
		case target.X-w >= 0:
			return target.X - w, y, true
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

// overlayTooltip draws the label over the frame. The help line takes
// the primary colours while the pointer is on it.
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
	if t.help != "" && t.y+1 < len(lines) {
		hs := lipgloss.NewStyle().Background(c.Bg).Foreground(c.Hint)
		if t.onHelpLine(m.hoverX, m.hoverY) {
			hs = lipgloss.NewStyle().Background(c.Primary).Foreground(c.PrimaryFg)
		}
		lines[t.y+1] = spliceAt(lines[t.y+1], hs.Render(t.helpText), t.x)
	}
	return strings.Join(lines, "\n")
}
