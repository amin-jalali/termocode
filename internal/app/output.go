package app

import (
	"regexp"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Output panel tab ─────────────────────────────────────────────────────
//
// A generic log view in the bottom panel ("View: Output"). Any feature can
// write lines to a NAMED channel; the tab shows one channel at a time with
// a chip row to switch between them.
//
// Writing from Update code:
//
//	m.output.Append("tasks", "go build ./...", "ok")
//
// Writing from a tea.Cmd (forces a repaint when it lands):
//
//	return appendOutputCmd("tasks", line)
//
// Writing from any goroutine: capture `out := m.output` first, then call
// out.Append — the store is mutex-guarded. The view refreshes on the next
// message.
//
// Writing from Lua:
//
//	_G.termocode_notify('termocode_output', 'lsp', 'server started')

const (
	// outputDefaultChannel is used when a writer passes an empty name.
	outputDefaultChannel = "termocode"
	// outputMaxLines caps each channel; the oldest lines are dropped.
	outputMaxLines = 5000
)

// OutputMsg appends Lines to Channel when it reaches Update.
type OutputMsg struct {
	Channel string
	Lines   []string
}

// appendOutputCmd returns a Cmd that appends lines to an Output channel.
func appendOutputCmd(channel string, lines ...string) tea.Cmd {
	return func() tea.Msg { return OutputMsg{Channel: channel, Lines: lines} }
}

type outputChannel struct {
	lines []string
	// scroll is how many rows the view sits above the tail. 0 follows new
	// output; >0 stays anchored on the same lines while more arrive.
	scroll int
}

// outputStore holds every Output channel. All methods are nil-safe and
// safe for concurrent use.
type outputStore struct {
	mu     sync.Mutex
	chans  map[string]*outputChannel
	order  []string // creation order (chip order)
	active string
	viewH  int // last rendered body height, for scroll clamping
}

func newOutputStore() *outputStore {
	return &outputStore{chans: map[string]*outputChannel{}}
}

// channelLocked returns (creating if needed) the named channel. The first
// channel ever created becomes the active one.
func (s *outputStore) channelLocked(name string) *outputChannel {
	if name == "" {
		name = outputDefaultChannel
	}
	if s.chans == nil {
		s.chans = map[string]*outputChannel{}
	}
	c, ok := s.chans[name]
	if !ok {
		c = &outputChannel{}
		s.chans[name] = c
		s.order = append(s.order, name)
		if s.active == "" {
			s.active = name
		}
	}
	return c
}

// Append adds text to a channel. Each argument may hold several lines
// ("\n"-separated); one trailing newline is ignored.
func (s *outputStore) Append(channel string, text ...string) {
	if s == nil {
		return
	}
	var add []string
	for _, t := range text {
		t = strings.TrimSuffix(t, "\n")
		for _, ln := range strings.Split(t, "\n") {
			add = append(add, sanitizeOutputLine(ln))
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channelLocked(channel)
	c.lines = append(c.lines, add...)
	if c.scroll > 0 {
		c.scroll += len(add)
	}
	if over := len(c.lines) - outputMaxLines; over > 0 {
		c.lines = append([]string(nil), c.lines[over:]...)
	}
	if c.scroll > len(c.lines) {
		c.scroll = len(c.lines)
	}
}

// Clear empties a channel (it stays in the chip row).
func (s *outputStore) Clear(channel string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.chans[channel]; ok {
		c.lines = nil
		c.scroll = 0
	}
}

// Channels returns the channel names in creation order.
func (s *outputStore) Channels() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.order...)
}

// Active returns the channel shown in the Output tab ("" when none).
func (s *outputStore) Active() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// SetActive switches the shown channel, creating it when missing.
func (s *outputStore) SetActive(channel string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channelLocked(channel)
	if channel == "" {
		channel = outputDefaultChannel
	}
	s.active = channel
}

// Lines returns a copy of a channel's lines.
func (s *outputStore) Lines(channel string) []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.chans[channel]; ok {
		return append([]string(nil), c.lines...)
	}
	return nil
}

// Scroll moves the active channel's view by delta rows (negative = up,
// toward older lines). Clamped so the top line never scrolls past row 0.
func (s *outputStore) Scroll(delta int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.chans[s.active]
	if !ok {
		return
	}
	c.scroll = clampOutputScroll(c.scroll-delta, len(c.lines), s.viewH)
}

// ScrollToEdge jumps to the oldest (top=true) or newest line.
func (s *outputStore) ScrollToEdge(top bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.chans[s.active]
	if !ok {
		return
	}
	if top {
		c.scroll = clampOutputScroll(len(c.lines), len(c.lines), s.viewH)
	} else {
		c.scroll = 0
	}
}

// window returns the visible slice of the active channel for a body of h
// rows and remembers h for later Scroll clamping.
func (s *outputStore) window(h int) []string {
	if s == nil || h <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewH = h
	c, ok := s.chans[s.active]
	if !ok {
		return nil
	}
	c.scroll = clampOutputScroll(c.scroll, len(c.lines), h)
	start, end := outputWindow(len(c.lines), h, c.scroll)
	return append([]string(nil), c.lines[start:end]...)
}

// clampOutputScroll keeps scroll inside [0, total-h].
func clampOutputScroll(scroll, total, h int) int {
	max := total - h
	if max < 0 {
		max = 0
	}
	if scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}

// outputWindow returns the half-open line range shown for `total` lines in
// h rows, `scroll` rows above the tail.
func outputWindow(total, h, scroll int) (start, end int) {
	end = total - scroll
	if end < 0 {
		end = 0
	}
	start = end - h
	if start < 0 {
		start = 0
	}
	return start, end
}

var outputANSIRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

// sanitizeOutputLine strips ANSI escapes and control characters (they
// would break the cell layout) and expands tabs.
func sanitizeOutputLine(s string) string {
	s = outputANSIRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// ── Header (channel chips + Clear) ───────────────────────────────────────

// outputChip is one clickable channel name in the header row.
type outputChip struct {
	Name       string
	Start, End int // half-open column range
}

// outputHeaderLayout lays out the header: " name  name  …   Clear ". Chips
// that do not fit are dropped; ClearStart is -1 when even Clear does not
// fit. Pure for tests and hit-testing.
func outputHeaderLayout(channels []string, w int) (chips []outputChip, clearStart, clearEnd int) {
	const clearLabel = " Clear "
	clearW := len(clearLabel)
	clearStart, clearEnd = -1, -1
	if w >= clearW+1 {
		clearStart = w - clearW
		clearEnd = w
	}
	limit := w
	if clearStart >= 0 {
		limit = clearStart - 1
	}
	col := 1
	for _, name := range channels {
		cw := runewidth.StringWidth(name) + 2 // " name "
		if col+cw > limit {
			break
		}
		chips = append(chips, outputChip{Name: name, Start: col, End: col + cw})
		col += cw + 1
	}
	return chips, clearStart, clearEnd
}

func renderOutputHeader(channels []string, active string, w int) string {
	bg := theme.Bg(theme.BgEditor)
	chipActive := theme.FgBg(theme.TextPrimary, theme.BgHover).Bold(true)
	chipIdle := theme.FgBg(theme.TextSecondary, theme.BgEditor)
	muted := theme.FgBg(theme.TextMuted, theme.BgEditor)
	chips, clearStart, _ := outputHeaderLayout(channels, w)
	var b strings.Builder
	col := 0
	for _, c := range chips {
		if c.Start > col {
			b.WriteString(bg.Render(strings.Repeat(" ", c.Start-col)))
		}
		st := chipIdle
		if c.Name == active {
			st = chipActive
		}
		b.WriteString(st.Render(" " + c.Name + " "))
		col = c.End
	}
	if clearStart >= 0 {
		if clearStart > col {
			b.WriteString(bg.Render(strings.Repeat(" ", clearStart-col)))
		}
		b.WriteString(muted.Render(" Clear "))
	}
	return b.String()
}

// renderOutputTab is the Output kind's Render callback.
func renderOutputTab(m *Model, w, h int) []string {
	channels := m.output.Channels()
	active := m.output.Active()
	rows := []string{renderOutputHeader(channels, active, w)}
	bodyH := h - 1
	text := theme.FgBg(theme.TextPrimary, theme.BgEditor)
	if len(channels) == 0 {
		if bodyH > 0 {
			rows = append(rows, theme.FgBg(theme.TextMuted, theme.BgEditor).Render(" No output yet."))
		}
		return rows
	}
	for _, ln := range m.output.window(bodyH) {
		if runewidth.StringWidth(ln) > w-2 {
			ln = runewidth.Truncate(ln, w-2, "…")
		}
		rows = append(rows, text.Render(" "+ln))
	}
	return rows
}

func init() {
	registerPanelKind(panelKindOutput, panelKindSpec{
		Title:  "Output",
		Icon:   "≡",
		Render: renderOutputTab,
		Scroll: func(m *Model, delta int) { m.output.Scroll(delta) },
		Click: func(m *Model, row, col int) tea.Cmd {
			if row != 0 {
				return nil
			}
			chips, clearStart, clearEnd := outputHeaderLayout(m.output.Channels(), m.editorPaneWidth())
			if clearStart >= 0 && col >= clearStart && col < clearEnd {
				m.output.Clear(m.output.Active())
				return nil
			}
			for _, c := range chips {
				if col >= c.Start && col < c.End {
					m.output.SetActive(c.Name)
				}
			}
			return nil
		},
		Key: func(m *Model, msg tea.KeyMsg) (tea.Cmd, bool) {
			switch msg.String() {
			case "up", "k":
				m.output.Scroll(-1)
			case "down", "j":
				m.output.Scroll(1)
			case "pgup":
				m.output.Scroll(-10)
			case "pgdown":
				m.output.Scroll(10)
			case "home", "g":
				m.output.ScrollToEdge(true)
			case "end", "G":
				m.output.ScrollToEdge(false)
			default:
				return nil, false
			}
			return nil, true
		},
	})
}
