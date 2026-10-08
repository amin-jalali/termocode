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
	"github.com/mattn/go-runewidth"

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
}

// TickMsg fires when the next-expiring toast should be expired and re-rendered.
type TickMsg time.Time

const (
	defaultLifetime = 3 * time.Second
	maxStacked      = 3
	// Each toast occupies these rows: title + detail. Top + bottom edge
	// "borders" are drawn as ANSI overline / underline on those rows
	// (1/8-cell thick), not as separate rows.
	rowsPerToast = 2
	// One blank row separates stacked toasts.
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
	m.nextID++
	t := Toast{
		ID:        m.nextID,
		Severity:  severity,
		Title:     title,
		Detail:    detail,
		ExpiresAt: time.Now().Add(defaultLifetime),
	}
	m.toasts = append(m.toasts, t)
	if OnPush != nil {
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
	out := m.toasts[:0]
	for _, t := range m.toasts {
		if !t.ExpiresAt.After(now) {
			continue
		}
		out = append(out, t)
	}
	m.toasts = out
	return m, m.scheduleTick()
}

func (m Model) scheduleTick() tea.Cmd {
	if len(m.toasts) == 0 {
		return nil
	}
	soonest := m.toasts[0].ExpiresAt
	for _, t := range m.toasts[1:] {
		if t.ExpiresAt.Before(soonest) {
			soonest = t.ExpiresAt
		}
	}
	d := time.Until(soonest)
	if d < 50*time.Millisecond {
		d = 50 * time.Millisecond
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return TickMsg(t) })
}

// View returns the rendered toast block as a list of full-width rows. Each
// toast contributes `rowsPerToast` rows: topBorder + title + detail + botBorder.
// Stacked toasts are separated by `gapBetweenToasts` blank rows (rendered
// transparent — caller's base bg shows through).
func (m Model) View(width, height int) []string {
	if len(m.toasts) == 0 || width <= 0 || height <= 0 {
		return nil
	}
	count := len(m.toasts)
	if count > maxStacked {
		count = maxStacked
	}
	visible := m.toasts[len(m.toasts)-count:]
	out := make([]string, 0, count*rowsPerToast+(count-1)*gapBetweenToasts)
	for i, t := range visible {
		if i > 0 {
			// Gap row: empty string of `width` cells. Returning a string of
			// pure spaces (no bg style) lets dropLeftVisual / spliceAt
			// preserve the underlying editor row through the gap.
			for g := 0; g < gapBetweenToasts; g++ {
				out = append(out, transparentRow(width))
			}
		}
		out = append(out, renderToast(t, width)...)
	}
	return out
}

// transparentRow returns a row that splices through the toast region as
// "no overlay" — same width but rendered as the underlying base row's
// content. We achieve that by emitting a sentinel: ANY string of width
// cells will overwrite the base in spliceAt, so we emit an empty string
// with a marker. Caller must skip these rows. Simpler: just emit a
// width-long string of spaces with no SGR — terminal will use whatever
// SGR was last open from the left base portion, which (after normalize)
// is the editor bg. That gives a clean editor-bg gap.
func transparentRow(width int) string {
	return strings.Repeat(" ", width)
}

func renderToast(t Toast, paneW int) []string {
	// Severity → accent colour + level label. Width-1 ASCII labels so the
	// reported visual width always matches the rendered cell count.
	accentColor := theme.DiagInfo
	label := "INFO"
	switch t.Severity {
	case Warn:
		accentColor = theme.DiagWarning
		label = "WARN"
	case Errr:
		accentColor = theme.DiagError
		label = "ERR "
	}

	bg := theme.Bg(theme.BgHover)
	accentStrip := theme.Bg(accentColor)
	labelStyle := theme.FgBg(accentColor, theme.BgHover).Bold(true)
	titleStyle := theme.FgBg(theme.TextPrimary, theme.BgHover).Bold(true)
	detailStyle := theme.FgBg(theme.TextMuted, theme.BgHover)
	// Pale (Faint) accent line that traces the top + bottom of the toast
	// box — same colour as the severity accent, dimmed so it reads as a
	// subtle outline rather than a second strong block.
	borderStyle := theme.FgBg(accentColor, theme.BgHover).Faint(true)

	w := paneW
	if w < 16 {
		w = 16
	}

	const stripW = 1
	const labelW = 4
	const labelGapL = 1
	const labelGapR = 2
	leadingW := stripW + labelGapL + labelW + labelGapR

	titleAvail := w - leadingW - 1 // 1-cell trailing pad for symmetry
	if titleAvail < 4 {
		titleAvail = 4
	}
	title := t.Title
	if runewidth.StringWidth(title) > titleAvail {
		title = runewidth.Truncate(title, titleAvail, "…")
	}
	titleBody := accentStrip.Render(strings.Repeat(" ", stripW)) +
		bg.Render(strings.Repeat(" ", labelGapL)) +
		labelStyle.Render(label) +
		bg.Render(strings.Repeat(" ", labelGapR)) +
		titleStyle.Render(title)
	if used := lipgloss.Width(titleBody); used < w {
		titleBody += bg.Render(strings.Repeat(" ", w-used))
	}

	// Detail row mirrors the title-row indent EXACTLY (strip + gapL +
	// label-shaped gap + gapR), so the SGR boundary positions line up
	// between the two rows.
	detailAvail := w - leadingW - 1
	if detailAvail < 4 {
		detailAvail = 4
	}
	// Use labelStyle (bold) for the row-2 label-shaped gap too — even
	// though we're rendering spaces, matching the row-1 SGR sequence
	// keeps glyph metrics identical between the two rows. Without this,
	// row-1's bold "WARN" can render ~1px wider than row-2's plain bg
	// in some terminal fonts and the rows drift apart by a cell.
	detailBody := accentStrip.Render(strings.Repeat(" ", stripW)) +
		bg.Render(strings.Repeat(" ", labelGapL)) +
		labelStyle.Render(strings.Repeat(" ", labelW)) +
		bg.Render(strings.Repeat(" ", labelGapR))
	if t.Detail != "" {
		detail := t.Detail
		if runewidth.StringWidth(detail) > detailAvail {
			detail = runewidth.Truncate(detail, detailAvail, "…")
		}
		detailBody += detailStyle.Render(detail)
	}
	if used := lipgloss.Width(detailBody); used < w {
		detailBody += bg.Render(strings.Repeat(" ", w-used))
	}

	_ = borderStyle
	return []string{titleBody, detailBody}
}
