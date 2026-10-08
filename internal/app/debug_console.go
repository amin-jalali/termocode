package app

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Debug Console bottom-panel tab (Group D) ─────────────────────────────
//
// Program output (DAP "output" events), nvim-dap messages and a REPL. The
// last row is the input line ("› expr"); Enter evaluates the expression in
// the current frame (DAP evaluate, context "repl"). Click the tab content
// to give it keyboard focus; Esc drops focus. Up / Down walk the input
// history, PgUp / PgDn / wheel scroll the output.

const debugConsoleMaxLines = 5000

type debugConsoleLine struct {
	Category string // stdout | stderr | console | important | input | result
	Text     string
}

// debugConsole is mutex-guarded so View can read it while a Cmd appends.
type debugConsole struct {
	mu      sync.Mutex
	lines   []debugConsoleLine
	partial map[string]string // category → unterminated tail
	scroll  int               // rows above the tail
	viewH   int

	input   []rune
	cursor  int // rune index into input
	history []string
	histIdx int // len(history) = editing a fresh line
}

func newDebugConsole() *debugConsole {
	return &debugConsole{partial: map[string]string{}}
}

// AppendOutput adds DAP output text. Output arrives in arbitrary chunks;
// text after the last newline is held until the next chunk of the same
// category completes it.
func (c *debugConsole) AppendOutput(category, text string) {
	if c == nil || text == "" {
		return
	}
	if category == "" {
		category = "console"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.partial == nil {
		c.partial = map[string]string{}
	}
	text = c.partial[category] + strings.ReplaceAll(text, "\r\n", "\n")
	parts := strings.Split(text, "\n")
	c.partial[category] = parts[len(parts)-1]
	added := 0
	for _, p := range parts[:len(parts)-1] {
		c.lines = append(c.lines, debugConsoleLine{Category: category, Text: sanitizeOutputLine(p)})
		added++
	}
	if c.scroll > 0 {
		c.scroll += added
	}
	if over := len(c.lines) - debugConsoleMaxLines; over > 0 {
		c.lines = append([]debugConsoleLine(nil), c.lines[over:]...)
	}
}

// Lines returns a copy of the complete lines plus pending partials.
func (c *debugConsole) Lines() []debugConsoleLine {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]debugConsoleLine(nil), c.lines...)
	for cat, p := range c.partial {
		if p != "" {
			out = append(out, debugConsoleLine{Category: cat, Text: sanitizeOutputLine(p)})
		}
	}
	return out
}

func (c *debugConsole) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = nil
	c.partial = map[string]string{}
	c.scroll = 0
}

func (c *debugConsole) Scroll(delta int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scroll = clampOutputScroll(c.scroll-delta, len(c.lines), c.viewH)
}

// window returns the visible lines for h rows (remembering h for Scroll).
func (c *debugConsole) window(h int) []debugConsoleLine {
	all := c.Lines()
	if c == nil || h <= 0 {
		return nil
	}
	c.mu.Lock()
	c.viewH = h
	c.scroll = clampOutputScroll(c.scroll, len(all), h)
	start, end := outputWindow(len(all), h, c.scroll)
	c.mu.Unlock()
	return all[start:end]
}

// ── Input line editing (pure on the console; called from Update) ───────

func (c *debugConsole) insert(r []rune) {
	c.input = append(c.input[:c.cursor], append(append([]rune(nil), r...), c.input[c.cursor:]...)...)
	c.cursor += len(r)
}

func (c *debugConsole) backspace() {
	if c.cursor == 0 {
		return
	}
	c.input = append(c.input[:c.cursor-1], c.input[c.cursor:]...)
	c.cursor--
}

func (c *debugConsole) deleteForward() {
	if c.cursor >= len(c.input) {
		return
	}
	c.input = append(c.input[:c.cursor], c.input[c.cursor+1:]...)
}

// submit returns the input line, records it in history and clears it.
func (c *debugConsole) submit() string {
	line := string(c.input)
	if strings.TrimSpace(line) != "" {
		if n := len(c.history); n == 0 || c.history[n-1] != line {
			c.history = append(c.history, line)
		}
	}
	c.histIdx = len(c.history)
	c.input, c.cursor = nil, 0
	c.mu.Lock()
	c.scroll = 0
	c.mu.Unlock()
	return line
}

// historyMove walks the input history (dir -1 = older).
func (c *debugConsole) historyMove(dir int) {
	if len(c.history) == 0 {
		return
	}
	i := c.histIdx + dir
	if i < 0 {
		i = 0
	}
	if i >= len(c.history) {
		c.histIdx = len(c.history)
		c.input, c.cursor = nil, 0
		return
	}
	c.histIdx = i
	c.input = []rune(c.history[i])
	c.cursor = len(c.input)
}

// debugConsoleKey is the tab's Key callback: line editing + REPL.
func debugConsoleKey(m *Model, msg tea.KeyMsg) (tea.Cmd, bool) {
	c := m.ensureDebug().console
	switch msg.String() {
	case "enter":
		return m.debugEvalRepl(c.submit()), true
	case "backspace", "ctrl+h":
		c.backspace()
	case "delete", "ctrl+d":
		c.deleteForward()
	case "left", "ctrl+b":
		if c.cursor > 0 {
			c.cursor--
		}
	case "right", "ctrl+f":
		if c.cursor < len(c.input) {
			c.cursor++
		}
	case "home", "ctrl+a":
		c.cursor = 0
	case "end", "ctrl+e":
		c.cursor = len(c.input)
	case "ctrl+u":
		c.input, c.cursor = nil, 0
	case "up":
		c.historyMove(-1)
	case "down":
		c.historyMove(1)
	case "pgup":
		c.Scroll(-10)
	case "pgdown":
		c.Scroll(10)
	case "ctrl+l":
		c.Clear()
	case " ":
		c.insert([]rune{' '})
	default:
		if msg.Type == tea.KeyRunes && !msg.Alt {
			c.insert(msg.Runes)
			return nil, true
		}
		return nil, false
	}
	return nil, true
}

// debugConsoleStyle colours a line by category.
func debugConsoleStyle(cat string) (fg int) {
	switch cat {
	case "stderr":
		return theme.DiagError
	case "console", "telemetry":
		return theme.TextMuted
	case "important":
		return theme.DiagWarning
	case "input":
		return theme.AccentBlue
	case "result":
		return theme.SyntaxString
	}
	return theme.TextPrimary
}

// renderDebugConsole is the Debug Console kind's Render callback.
func renderDebugConsole(m *Model, w, h int) []string {
	bg := theme.Bg(theme.BgEditor)
	var c *debugConsole
	if m.debug != nil {
		c = m.debug.console
	}
	bodyH := h - 1
	var rows []string
	lines := c.window(bodyH)
	if len(lines) == 0 && bodyH > 0 {
		hint := " No output yet. Start a session with Alt+F5; type an expression below and press Enter."
		if !m.dapSessionActive {
			hint = " No active debug session — Alt+F5 or the Run view ▶ starts one."
		}
		rows = append(rows, theme.FgBg(theme.TextMuted, theme.BgEditor).Render(runewidth.Truncate(hint, w, "…")))
	}
	for _, ln := range lines {
		text := ln.Text
		if runewidth.StringWidth(text) > w-2 {
			text = runewidth.Truncate(text, w-2, "…")
		}
		rows = append(rows, theme.FgBg(debugConsoleStyle(ln.Category), theme.BgEditor).Render(" "+text))
	}
	for len(rows) < bodyH {
		rows = append(rows, bg.Render(""))
	}
	if len(rows) > bodyH && bodyH >= 0 {
		rows = rows[:bodyH]
	}
	return append(rows, renderDebugConsoleInput(m, c, w))
}

// renderDebugConsoleInput draws "› input" with a block cursor when focused.
func renderDebugConsoleInput(m *Model, c *debugConsole, w int) string {
	prompt := theme.FgBg(theme.AccentBlue, theme.BgHover).Bold(true)
	text := theme.FgBg(theme.TextPrimary, theme.BgHover)
	fill := theme.Bg(theme.BgHover)
	focused := m.panelFocused && m.panelActive == panelKindDebugConsole
	var input []rune
	cur := 0
	if c != nil {
		input, cur = c.input, c.cursor
	}
	var b strings.Builder
	b.WriteString(prompt.Render(" › "))
	used := 3
	if len(input) == 0 && !focused {
		hint := "evaluate expression (click to type)"
		hint = runewidth.Truncate(hint, maxInt(w-used, 0), "…")
		b.WriteString(theme.FgBg(theme.TextDim, theme.BgHover).Render(hint))
		used += runewidth.StringWidth(hint)
	} else {
		// Keep the cursor visible: scroll the input left when it overflows.
		avail := w - used - 1
		start := 0
		if avail > 0 && cur > avail {
			start = cur - avail
		}
		for i := start; i <= len(input); i++ {
			if used >= w {
				break
			}
			ch := " "
			if i < len(input) {
				ch = string(input[i])
			}
			if focused && i == cur {
				b.WriteString(theme.FgBg(theme.BgEditor, theme.Cursor).Render(ch))
			} else if i < len(input) {
				b.WriteString(text.Render(ch))
			} else {
				break
			}
			used += runewidth.StringWidth(ch)
		}
	}
	if used < w {
		b.WriteString(fill.Render(strings.Repeat(" ", w-used)))
	}
	return b.String()
}

// showDebugConsole opens the tab and (optionally) focuses its input.
func (m *Model) showDebugConsole(focus bool) {
	m.ensureDebug()
	m.showPanelTab(panelKindDebugConsole)
	if focus && m.termOpen && m.panelActive == panelKindDebugConsole {
		m.panelFocused = true
		m.focus = FocusEditor
	}
}

func init() {
	registerPanelKind(panelKindDebugConsole, panelKindSpec{
		Title:  "Debug Console",
		Icon:   "▷",
		Render: renderDebugConsole,
		Scroll: func(m *Model, delta int) { m.ensureDebug().console.Scroll(delta) },
		Key:    debugConsoleKey,
	})
}
