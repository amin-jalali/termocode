// Package toast renders transient corner notifications over the editor.
//
// Toasts surface short status messages (saved, error, etc.) that appear in
// the bottom-right of the chrome and fade after a few seconds. The Model is
// owned by the main app, which calls Push to enqueue and View to overlay.
package toast

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/anim"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Severity distinguishes the colour of the bar accent.
type Severity int

const (
	Info Severity = iota
	Warn
	Errr
)

// Toast is one queued message.
type Toast struct {
	ID        int
	Severity  Severity
	Title     string
	Detail    string
	ExpiresAt time.Time
	// Key, when set, identifies an updatable toast (PushKeyed): a later push
	// with the same key replaces it in place instead of stacking.
	Key string
	// Click, when set, is the message a click on the toast sends (the app
	// hit-tests the cards; see PushClick).
	Click tea.Msg
	// Private marks a toast that carries a secret (a one-time link): it
	// stays on this screen only (no OnPush, no mirror to other screens).
	Private bool

	// slide is the horizontal slide position: 0 = in place, 1 = fully
	// off to the right. New toasts slide in, expired ones slide out.
	slide   anim.Value
	leaving bool
}

// TickMsg fires when the next-expiring toast should be expired and re-rendered.
type TickMsg time.Time

const (
	defaultLifetime = 3 * time.Second
	maxStacked      = 3
	// A toast is a rounded card: border + title + detail + border. Toasts
	// without a detail line drop the detail row.
	rowsPerToast        = 4
	rowsPerToastCompact = 3
	// One row separates stacked toasts (room for the drop shadow).
	gapBetweenToasts = 1
)

type Model struct {
	toasts []Toast
	nextID int
}

func New() Model { return Model{} }

// Empty reports whether there are no queued toasts. Hot-path callers (the
// per-frame editor overlay) check this BEFORE querying nvim for gutter
// width, so a no-toast View() doesn't pay an RPC round-trip.
func (m Model) Empty() bool { return len(m.toasts) == 0 }

// Live returns the toasts on show, oldest first (copies; a toast already
// sliding out is left out). The web side mirrors them (WEB-12).
func (m Model) Live() []Toast {
	out := make([]Toast, 0, len(m.toasts))
	for _, t := range m.toasts {
		if !t.leaving {
			out = append(out, t)
		}
	}
	return out
}

// OnPush is an optional package-level hook fired every time a toast is
// enqueued. The app uses it to mirror error / warning toasts into the
// global error log, so the full text survives even after the toast
// fades or gets clipped on render.
//
// nil-by-default; nil-safe at call sites — Push won't panic if unset.
var OnPush func(severity Severity, msg string)

// Push enqueues a single-line toast (title only, no detail).
func (m Model) Push(severity Severity, msg string) (Model, tea.Cmd) {
	return m.PushDetail(severity, msg, "")
}

// PushDetail enqueues a two-line toast: title row + detail row.
// Pass detail="" to leave the second line blank.
func (m Model) PushDetail(severity Severity, title, detail string) (Model, tea.Cmd) {
	return m.push(severity, title, detail, true)
}

// push adds a toast; hook: tell OnPush.
func (m Model) push(severity Severity, title, detail string, hook bool) (Model, tea.Cmd) {
	m.nextID++
	t := Toast{
		ID:        m.nextID,
		Severity:  severity,
		Title:     title,
		Detail:    detail,
		ExpiresAt: time.Now().Add(defaultLifetime),
	}
	t.slide.Jump(1)
	t.slide.Set(0)
	m.toasts = append(m.toasts, t)
	if hook && OnPush != nil {
		combined := title
		if detail != "" {
			combined += " — " + detail
		}
		OnPush(severity, combined)
	}
	return m, m.scheduleTick()
}

// PushKeyed shows a toast that later pushes with the same key update in
// place (and re-arm its lifetime) — used for progress such as a background
// clone. OnPush fires only when the toast is first created.
func (m Model) PushKeyed(key string, severity Severity, title, detail string) (Model, tea.Cmd) {
	for i := range m.toasts {
		if m.toasts[i].Key == key && key != "" {
			// Copy-on-write: the slice may be shared with an older Model.
			ts := append([]Toast(nil), m.toasts...)
			ts[i].Severity = severity
			ts[i].Title = title
			ts[i].Detail = detail
			ts[i].ExpiresAt = time.Now().Add(defaultLifetime)
			m.toasts = ts
			return m, m.scheduleTick()
		}
	}
	m, cmd := m.PushDetail(severity, title, detail)
	m.toasts[len(m.toasts)-1].Key = key
	return m, cmd
}

// clickLifetime is how long a clickable toast stays: long enough to
// reach it with the mouse.
const clickLifetime = 8 * time.Second

// PushClick shows a toast that does something when clicked: the app
// sends click (detail should say what, e.g. "Show in folder").
func (m Model) PushClick(severity Severity, title, detail string, click tea.Msg) (Model, tea.Cmd) {
	m, cmd := m.PushDetail(severity, title, detail)
	t := &m.toasts[len(m.toasts)-1]
	t.Click = click
	t.ExpiresAt = time.Now().Add(clickLifetime)
	return m, cmd
}

// PushPrivate shows a Private toast for life: clickable like PushClick,
// but OnPush (the error log) never sees it.
func (m Model) PushPrivate(severity Severity, title, detail string, click tea.Msg, life time.Duration) (Model, tea.Cmd) {
	m, cmd := m.push(severity, title, detail, false)
	t := &m.toasts[len(m.toasts)-1]
	t.Click, t.Private = click, true
	t.ExpiresAt = time.Now().Add(life)
	return m, cmd
}

// DismissID removes the toast with the given ID, if any.
func (m Model) DismissID(id int) Model {
	out := make([]Toast, 0, len(m.toasts))
	for _, t := range m.toasts {
		if t.ID != id {
			out = append(out, t)
		}
	}
	m.toasts = out
	return m
}

// Dismiss removes the toast with the given key, if any.
func (m Model) Dismiss(key string) Model {
	out := make([]Toast, 0, len(m.toasts))
	for _, t := range m.toasts {
		if key == "" || t.Key != key {
			out = append(out, t)
		}
	}
	m.toasts = out
	return m
}

// Tick removes any toasts whose ExpiresAt is in the past and returns the
// model + a Cmd waking us at the next expiry.
func (m Model) Tick(now time.Time) (Model, tea.Cmd) {
	out := make([]Toast, 0, len(m.toasts))
	for _, t := range m.toasts {
		if !t.ExpiresAt.After(now) {
			if !anim.Enabled {
				continue
			}
			// Slide out first; Step drops it once it is off screen.
			if !t.leaving {
				t.leaving = true
				t.slide.Set(1)
			}
		}
		out = append(out, t)
	}
	m.toasts = out
	return m, m.scheduleTick()
}

// Animating reports whether any toast is sliding.
func (m Model) Animating() bool {
	for _, t := range m.toasts {
		if t.slide.Moving() {
			return true
		}
	}
	return false
}

// Step advances the slide animations one frame and drops toasts that
// finished sliding out. It reports whether anything is still moving.
func (m Model) Step() (Model, bool) {
	if len(m.toasts) == 0 {
		return m, false
	}
	moving := false
	out := make([]Toast, 0, len(m.toasts))
	for _, t := range m.toasts {
		if t.slide.Step() {
			moving = true
		}
		if t.leaving && !t.slide.Moving() {
			continue
		}
		out = append(out, t)
	}
	m.toasts = out
	return m, moving
}

func (m Model) scheduleTick() tea.Cmd {
	if len(m.toasts) == 0 {
		return nil
	}
	var soonest time.Time
	for _, t := range m.toasts {
		if t.leaving {
			continue
		}
		if soonest.IsZero() || t.ExpiresAt.Before(soonest) {
			soonest = t.ExpiresAt
		}
	}
	if soonest.IsZero() {
		return nil
	}
	d := time.Until(soonest)
	if d < 50*time.Millisecond {
		d = 50 * time.Millisecond
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return TickMsg(t) })
}

// Card is one rendered toast: its rows plus the slide position (0 = in
// place, 1 = off to the right) the compositor applies.
type Card struct {
	Lines []string
	Slide float64
	ID    int     // the toast's ID (DismissID)
	Click tea.Msg // what a click sends; nil when the toast is not clickable
}

// FitWidth is the card width the visible toasts need: the longest title
// or detail plus the icon lead, padding and border (renderToast), at least
// 28 and at most max. 0 when there are none.
func (m Model) FitWidth(max int) int {
	if len(m.toasts) == 0 {
		return 0
	}
	count := min(len(m.toasts), maxStacked)
	need := 0
	for _, t := range m.toasts[len(m.toasts)-count:] {
		need = maxInt(need, runewidth.StringWidth(t.Title), runewidth.StringWidth(t.Detail))
	}
	w := need + 8 // "  ●  " lead, one pad cell, two border cells
	if w < 28 {
		w = 28
	}
	if w > max {
		w = max
	}
	return w
}

// CompactTitleWidth is the compact card width that shows the newest
// visible toast's title in full (renderToastCompact: border, " ● ", pad).
func (m Model) CompactTitleWidth() int {
	if len(m.toasts) == 0 {
		return 0
	}
	return runewidth.StringWidth(m.toasts[len(m.toasts)-1].Title) + 6
}

func maxInt(a int, bs ...int) int {
	for _, b := range bs {
		if b > a {
			a = b
		}
	}
	return a
}

// Cards returns the visible toasts (oldest first), each as a rounded card
// `width` cells wide.
func (m Model) Cards(width int) []Card {
	if len(m.toasts) == 0 || width <= 0 {
		return nil
	}
	count := len(m.toasts)
	if count > maxStacked {
		count = maxStacked
	}
	visible := m.toasts[len(m.toasts)-count:]
	out := make([]Card, 0, count)
	for _, t := range visible {
		out = append(out, Card{Lines: renderToast(t, width), Slide: t.slide.Pos, ID: t.ID, Click: t.Click})
	}
	return out
}

// CompactCards returns the newest n toasts (oldest first), each as a
// 3-row card `width` cells wide (title · detail on one row), for small
// windows where a stack of 4-row cards would cover too much text.
func (m Model) CompactCards(width, n int) []Card {
	if len(m.toasts) == 0 || width <= 0 || n <= 0 {
		return nil
	}
	if n > len(m.toasts) {
		n = len(m.toasts)
	}
	out := make([]Card, 0, n)
	for _, t := range m.toasts[len(m.toasts)-n:] {
		out = append(out, Card{Lines: renderToastCompact(t, width), Slide: t.slide.Pos, ID: t.ID, Click: t.Click})
	}
	return out
}

// View returns the toast block as rows (cards separated by blank gap
// rows). The app composites Cards directly; View is kept for callers that
// want a flat block.
func (m Model) View(width, height int) []string {
	if len(m.toasts) == 0 || width <= 0 || height <= 0 {
		return nil
	}
	var out []string
	for i, c := range m.Cards(width) {
		if i > 0 {
			for g := 0; g < gapBetweenToasts; g++ {
				out = append(out, strings.Repeat(" ", width))
			}
		}
		out = append(out, c.Lines...)
	}
	return out
}

// severityIcon is the glyph + colour token for a severity.
func severityIcon(s Severity) (string, theme.Color256) {
	type icons struct{ nerd, uni, ascii string }
	var ic icons
	var c theme.Color256
	switch s {
	case Warn:
		ic, c = icons{"\uf071", "▲", "!"}, theme.DiagWarning
	case Errr:
		ic, c = icons{"\uf057", "✕", "x"}, theme.DiagError
	default:
		ic, c = icons{"\uf05a", "●", "i"}, theme.DiagInfo
	}
	switch theme.CurrentIconMode {
	case theme.IconModeNerdFont:
		return ic.nerd, c
	case theme.IconModeUnicode:
		return ic.uni, c
	}
	return ic.ascii, c
}

// renderToastCompact draws one toast as a 3-row card exactly w cells wide:
//
//	╭──────────────────────────────╮
//	│ ● Saved · internal/foo.go …  │
//	╰──────────────────────────────╯
func renderToastCompact(t Toast, w int) []string {
	if w < 16 {
		w = 16
	}
	f := theme.Float()
	icon, sev := severityIcon(t.Severity)
	bg := lipgloss.NewStyle().Background(f.Bg)
	iconStyle := lipgloss.NewStyle().Background(f.Bg).Foreground(theme.LG(sev)).Bold(true)
	titleStyle := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Title).Bold(true)
	detailStyle := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Hint)

	inner := w - 2
	const lead = 3 // " ● "
	avail := inner - lead - 1
	if avail < 4 {
		avail = 4
	}
	title := runewidth.Truncate(t.Title, avail, "…")
	if t.Severity == Errr {
		title = errorDetailText(t.Title, avail)
	}
	row := bg.Render(" ") + iconStyle.Render(icon) + bg.Render(" ") + titleStyle.Render(title)
	if rest := avail - runewidth.StringWidth(title) - 3; t.Detail != "" && rest >= 4 {
		detail := runewidth.Truncate(t.Detail, rest, "…")
		if t.Severity == Errr {
			detail = errorDetailText(t.Detail, rest)
		}
		row += detailStyle.Render(" · " + detail)
	}
	if used := lipgloss.Width(row); used < inner {
		row += bg.Render(strings.Repeat(" ", inner-used))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(f.Border).
		BorderLeftForeground(theme.LG(sev)).
		BorderBackground(f.Bg).
		Background(f.Bg).
		Width(inner).
		Render(row)
	return strings.Split(box, "\n")
}

// renderToast draws one toast as a rounded card exactly w cells wide:
//
//	╭──────────────────────────────╮
//	│  ●  Saved                    │
//	│     internal/foo.go · 480 L  │
//	╰──────────────────────────────╯
func renderToast(t Toast, w int) []string {
	if w < 16 {
		w = 16
	}
	f := theme.Float()
	icon, sev := severityIcon(t.Severity)
	bg := lipgloss.NewStyle().Background(f.Bg)
	iconStyle := lipgloss.NewStyle().Background(f.Bg).Foreground(theme.LG(sev)).Bold(true)
	titleStyle := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Title).Bold(true)
	detailStyle := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Hint)

	inner := w - 2
	const lead = 5 // "  ●  "
	avail := inner - lead - 1
	if avail < 4 {
		avail = 4
	}
	fit := func(s string) string {
		if runewidth.StringWidth(s) > avail {
			return runewidth.Truncate(s, avail, "…")
		}
		return s
	}
	pad := func(s string) string {
		if used := lipgloss.Width(s); used < inner {
			s += bg.Render(strings.Repeat(" ", inner-used))
		}
		return s
	}
	title := fit(t.Title)
	if t.Severity == Errr {
		title = errorDetailText(t.Title, avail)
	}
	rows := []string{pad(bg.Render("  ") + iconStyle.Render(icon) + bg.Render("  ") + titleStyle.Render(title))}
	if t.Detail != "" {
		detail := fit(t.Detail)
		if t.Severity == Errr {
			detail = errorDetailText(t.Detail, avail)
		}
		rows = append(rows, pad(bg.Render(strings.Repeat(" ", lead))+detailStyle.Render(detail)))
	}
	// The normal card frame; the severity shows as the left edge only.
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(f.Border).
		BorderLeftForeground(theme.LG(sev)).
		BorderBackground(f.Bg).
		Background(f.Bg).
		Width(inner).
		Render(strings.Join(rows, "\n"))
	return strings.Split(box, "\n")
}
