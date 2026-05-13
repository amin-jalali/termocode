package editor

import (
	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/grid"
	"termocode/internal/nvim"
)

// Cursor mirrors a (line, col) pair. Both fields are 0-based.
type Cursor struct {
	Line int
	Col  int
}

// Model is a thin nvim-backed editor pane. It owns the grid that mirrors
// nvim's UI cells; nvim itself owns the buffer, syntax, LSP, and undo state.
type Model struct {
	client *nvim.Client
	g      *grid.Grid

	w, h     int
	path     string
	dirty    bool
	lang     string
	errors   int
	warnings int

	// mouseDown tracks whether the left button is currently pressed,
	// so motion-without-press events (Bubble Tea sends them when
	// WithMouseAllMotion is on, regardless of button state) don't get
	// forwarded to nvim as bogus "left drag" — which made nvim think
	// the user was always selecting text and broke real drag-select.
	mouseDown bool

	// Last drag cell forwarded to nvim. Coalesces redundant drag events:
	// xterm 1002 already only emits on cell change, but Bubble Tea can
	// emit a duplicate MouseLeft + MouseMotion for the same cell, and
	// nvim_input_mouse with the same coords doesn't extend the visual
	// selection — it just costs a round-trip. -1 means "no drag yet".
	lastDragX, lastDragY int
}

func New(client *nvim.Client) Model {
	return Model{
		client:    client,
		g:         grid.New(),
		lastDragX: -1,
		lastDragY: -1,
	}
}

func (m Model) Init() tea.Cmd { return nil }

// SetSize updates the pane size. If nvim is attached, the grid is resized too.
func (m *Model) SetSize(w, h int) {
	if w == m.w && h == m.h {
		return
	}
	m.w, m.h = w, h
	if m.client != nil && w > 0 && h > 0 {
		_ = m.client.Resize(w, h)
	}
}

// ApplyRedraw consumes a batch of nvim redraw events.
func (m *Model) ApplyRedraw(events [][]any) {
	m.g.Apply(events)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		if m.client == nil {
			return m, nil
		}
		// Drop SGR mouse-tracking fragments (e.g. "[<35", ";70", "19M")
		// that Bubble Tea occasionally emits as KeyRunes when a wheel /
		// motion event isn't recognised cleanly. Without this guard
		// they get forwarded to nvim's input stream and typed verbatim
		// into the active buffer — visible to the user as random
		// "[<35;70;19M…" text appearing while wheel-scrolling near a
		// buffer edge.
		if k.Type == tea.KeyRunes {
			if isMouseFragmentEvent(k.Runes) {
				return m, nil
			}
			if k.Alt && isMouseCodeRunes(k.Runes) {
				return m, nil
			}
		}
		keys := translateKey(k)
		if keys != "" {
			_ = m.client.Input(keys)
		}
	}
	return m, nil
}

// isMouseCodeRunes reports whether the runes consist entirely of
// characters that appear in an SGR mouse-tracking sequence
// (digits, `;`, `<`, `[`, `M`, `m`). Used together with `Alt` to drop
// the lone-bracket case where Bubble Tea consumes ESC as Alt and emits
// the rest as a single rune.
func isMouseCodeRunes(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	for _, r := range runes {
		switch {
		case r >= '0' && r <= '9':
		case r == ';' || r == '<' || r == '[' || r == 'M' || r == 'm':
		default:
			return false
		}
	}
	return true
}

// isMouseFragmentEvent reports whether a KeyRunes payload looks like
// the fragment of an SGR mouse code that escaped the parser.
func isMouseFragmentEvent(runes []rune) bool {
	if len(runes) < 2 {
		return false
	}
	first := runes[0]
	if first != '[' && first != '<' && first != ';' {
		return false
	}
	for _, r := range runes {
		switch {
		case r >= '0' && r <= '9':
		case r == ';' || r == '<' || r == '[' || r == 'M' || r == 'm':
		default:
			return false
		}
	}
	return true
}

func (m Model) View(focused bool) string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	return m.g.Render()
}

// HandleMouse forwards a mouse event to nvim, translating Bubble Tea event
// types into nvim's button/action pair.
//
// Subtle bit: Bubble Tea's MouseLeft event fires for BOTH the initial
// button-down AND every cell crossed while the button is held. We only
// want the very first one to be reported as "press"; subsequent
// MouseLeft events (and MouseMotion events) within the same down-up
// cycle should be reported as "drag" so nvim extends a visual
// selection instead of treating each cell as a fresh click.
//
// Performance: drag bursts during selection are the hot path. Two
// optimisations keep them snappy:
//
//  1. Coalesce duplicate-cell drags (lastDragX/Y) — xterm 1002 normally
//     emits one event per cell crossed, but Bubble Tea can fire a
//     redundant MouseLeft + MouseMotion for the same cell, and
//     nvim_input_mouse with identical coords just costs a round-trip.
//
//  2. Fire-and-forget via MouseAsync so Update() doesn't block on the
//     msgpack round-trip. Ordering is preserved by the dispatcher
//     goroutine inside the nvim client (single-consumer queue), so
//     press → drag → drag → release still arrive at nvim in order.
func (m Model) HandleMouse(x, y int, t tea.MouseEventType) (Model, tea.Cmd) {
	if m.client == nil {
		return m, nil
	}
	var button, action string
	switch t {
	case tea.MouseLeft:
		if m.mouseDown {
			button, action = "left", "drag"
		} else {
			m.mouseDown = true
			m.lastDragX, m.lastDragY = -1, -1
			button, action = "left", "press"
		}
	case tea.MouseRight:
		button, action = "right", "press"
	case tea.MouseMiddle:
		button, action = "middle", "press"
	case tea.MouseRelease:
		m.mouseDown = false
		m.lastDragX, m.lastDragY = -1, -1
		button, action = "left", "release"
	case tea.MouseWheelUp:
		button, action = "wheel", "up"
	case tea.MouseWheelDown:
		button, action = "wheel", "down"
	case tea.MouseMotion:
		if m.mouseDown {
			button, action = "left", "drag"
		}
	}
	if button == "" {
		return m, nil
	}
	if action == "drag" {
		if x == m.lastDragX && y == m.lastDragY {
			return m, nil
		}
		m.lastDragX, m.lastDragY = x, y
	}
	m.client.MouseAsync(button, action, "", y, x)
	return m, nil
}

// Open issues `:edit <path>` in nvim. The redraw stream will reflect the new buffer.
func (m *Model) Open(path string) error {
	m.path = path
	if m.client == nil {
		return nil
	}
	return m.client.Command("edit " + path)
}

// Save issues `:silent w` in nvim. The `silent` keeps nvim's cmdline
// message (`"path" 50L, 1234B written`) from bleeding through into the
// rendered grid as a stray status line; we surface our own toast on
// save instead.
func (m *Model) Save() error {
	if m.client == nil {
		return nil
	}
	return m.client.Command("silent w")
}

// MarkSaved is kept for compatibility; nvim is authoritative for dirty state.
func (m *Model) MarkSaved() { m.dirty = false }

// SetMeta updates locally-cached file metadata that nvim reports separately
// (filename, dirty flag, filetype). Phase 1.6 wires this from nvim events.
func (m *Model) SetMeta(path string, dirty bool, lang string) {
	m.path = path
	m.dirty = dirty
	m.lang = lang
}

// SetDiagnostics records the current LSP diagnostic counts.
func (m *Model) SetDiagnostics(errors, warnings int) {
	m.errors = errors
	m.warnings = warnings
}

func (m Model) Path() string  { return m.path }
func (m Model) IsDirty() bool { return m.dirty }
func (m Model) Lang() string  { return m.lang }
func (m Model) Errors() int   { return m.errors }
func (m Model) Warnings() int { return m.warnings }

func (m Model) Cursor() Cursor {
	return Cursor{Line: m.g.CursorRow, Col: m.g.CursorCol}
}

func (m Model) LineCount() int { return m.g.Height }
