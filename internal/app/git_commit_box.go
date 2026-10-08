package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	tea "github.com/charmbracelet/bubbletea"
)

// The Source Control panel carries an always-visible commit area beneath the
// branch sub-header, mirroring VSCode: a single-line editable message box and
// an action bar (Commit · Sync · ⋯). These rows are FIXED chrome — they live
// above the scrollable accordion, so their sidebar-relative Y positions are
// constants the renderer and the mouse hit-test both read. Keep them in sync
// with gitPanelTopOffset().
//
//	row 0  title
//	row 1  hairline
//	row 2  branch sub-header           (gitSubheaderRow)
//	row 3  spacer
//	row 4  commit message box          (gitCommitInputRow)
//	row 5  action bar                  (gitActionBarRow)
//	row 6  spacer
//	row 7… scrollable accordion        (gitPanelTopOffset)
const (
	gitCommitInputRow = 4
	gitActionBarRow   = 5
)

// Commit-area palette. The input field reads as a sunken control via a bg one
// step lighter than the panel; the primary Commit chip uses the same green as
// staged/added signs.
var (
	gitInputBg      = lipgloss.Color("#1b1b1b") // sunken trough, clearly darker than the #303030 panel
	gitInputFg      = lipgloss.Color("#d6d6d6")
	gitInputFocusFg = lipgloss.Color("#ffffff")
	gitInputPlace   = lipgloss.Color("#7a7a7a")
	gitInputAccent  = lipgloss.Color("#36a3d9") // focus bar, matches the cursor-row accent
	gitInputRail    = lipgloss.Color("#45454d") // quiet rail when the field is blurred

	// Primary Commit button: confident VSCode-blue when actionable, muted slate
	// when there's nothing to commit yet.
	gitCommitReadyBg = lipgloss.Color("#0e639c")
	gitCommitReadyFg = lipgloss.Color("#ffffff")
	gitCommitIdleBg  = lipgloss.Color("#37373d")
	gitCommitIdleFg  = lipgloss.Color("#9a9a9a")

	// Secondary square buttons (Sync, overflow).
	gitChipBg = lipgloss.Color("#3a3a40") // raised, lighter than the panel
	gitChipFg = lipgloss.Color("#cfcfcf")
)

// gitCommitPlaceholder is shown dim in the empty message box.
const gitCommitPlaceholder = "Message…"

// ── editing ───────────────────────────────────────────────────────────────

// gitFocusCommitBox gives the inline message box keyboard focus and parks the
// caret at the end of the current text.
func (m *Model) gitFocusCommitBox() {
	m.gitCommitFocused = true
	m.gitCommitCaret = len([]rune(m.gitCommitMsg))
}

// gitBlurCommitBox returns keystrokes to the accordion navigation.
func (m *Model) gitBlurCommitBox() { m.gitCommitFocused = false }

func (m *Model) gitCommitInsert(s string) {
	r := []rune(m.gitCommitMsg)
	if m.gitCommitCaret < 0 || m.gitCommitCaret > len(r) {
		m.gitCommitCaret = len(r)
	}
	ins := []rune(s)
	out := make([]rune, 0, len(r)+len(ins))
	out = append(out, r[:m.gitCommitCaret]...)
	out = append(out, ins...)
	out = append(out, r[m.gitCommitCaret:]...)
	m.gitCommitMsg = string(out)
	m.gitCommitCaret += len(ins)
}

func (m *Model) gitCommitBackspace() {
	r := []rune(m.gitCommitMsg)
	if m.gitCommitCaret <= 0 || m.gitCommitCaret > len(r) {
		return
	}
	out := append(r[:m.gitCommitCaret-1], r[m.gitCommitCaret:]...)
	m.gitCommitMsg = string(out)
	m.gitCommitCaret--
}

func (m *Model) gitCommitDeleteFwd() {
	r := []rune(m.gitCommitMsg)
	if m.gitCommitCaret < 0 || m.gitCommitCaret >= len(r) {
		return
	}
	out := append(r[:m.gitCommitCaret], r[m.gitCommitCaret+1:]...)
	m.gitCommitMsg = string(out)
}

func (m *Model) gitCommitMoveCaret(d int) {
	n := len([]rune(m.gitCommitMsg))
	m.gitCommitCaret += d
	if m.gitCommitCaret < 0 {
		m.gitCommitCaret = 0
	}
	if m.gitCommitCaret > n {
		m.gitCommitCaret = n
	}
}

// handleGitCommitKey routes a key press while the message box is focused.
// Returns (model, cmd, true) when it consumes the key. Enter commits; Esc/Tab
// blur; the usual editing keys mutate the buffer.
func (m Model) handleGitCommitKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.Type {
	case tea.KeyEnter:
		cmd := m.gitInlineCommit(false, false)
		return m, cmd, true
	case tea.KeyEsc, tea.KeyTab:
		m.gitBlurCommitBox()
		return m, nil, true
	case tea.KeyBackspace:
		m.gitCommitBackspace()
		return m, nil, true
	case tea.KeyDelete:
		m.gitCommitDeleteFwd()
		return m, nil, true
	case tea.KeyLeft:
		m.gitCommitMoveCaret(-1)
		return m, nil, true
	case tea.KeyRight:
		m.gitCommitMoveCaret(1)
		return m, nil, true
	case tea.KeyHome:
		m.gitCommitCaret = 0
		return m, nil, true
	case tea.KeyEnd:
		m.gitCommitCaret = len([]rune(m.gitCommitMsg))
		return m, nil, true
	case tea.KeySpace:
		m.gitCommitInsert(" ")
		return m, nil, true
	case tea.KeyRunes:
		m.gitCommitInsert(string(msg.Runes))
		return m, nil, true
	}
	return m, nil, true // swallow everything else while focused
}

// ── rendering ───────────────────────────────────────────────────────────────

// renderGitCommitBox draws the single-line message field across width w. When
// focused it shows a caret and horizontally scrolls to keep it visible; when
// empty it shows a dim placeholder.
func (m Model) renderGitCommitBox(w int) string {
	field := lipgloss.NewStyle().Background(gitInputBg)
	if w < 4 {
		return field.Render(strings.Repeat(" ", max(w, 0)))
	}
	inner := w - 3 // rail column + one space + trailing pad column

	// Leftmost column: a rail that frames the field. Bright cyan when active
	// (matching the cursor-row accent), quiet grey when blurred — always present
	// so the row always reads as an editable input, not stray dim text.
	railColor := gitInputRail
	if m.gitCommitFocused {
		railColor = gitInputAccent
	}
	lead := lipgloss.NewStyle().Background(gitInputBg).Foreground(railColor).Render("▌")

	fg := gitInputFg
	if m.gitCommitFocused {
		fg = gitInputFocusFg
	}

	runes := []rune(m.gitCommitMsg)
	var body string
	if len(runes) == 0 && !m.gitCommitFocused {
		place := gitCommitPlaceholder
		if runewidth.StringWidth(place) > inner {
			place = runewidth.Truncate(place, inner, "…")
		}
		body = lipgloss.NewStyle().Background(gitInputBg).Foreground(gitInputPlace).Italic(true).Render(place)
		body = padBgRight(body, inner, gitInputBg)
	} else {
		// Horizontal scroll window around the caret.
		caret := min(m.gitCommitCaret, len(runes))
		start := 0
		if caret >= inner {
			start = caret - inner + 1
		}
		end := min(start+inner, len(runes))
		vis := runes[start:end]

		var sb strings.Builder
		txt := lipgloss.NewStyle().Background(gitInputBg).Foreground(fg)
		caretStyle := lipgloss.NewStyle().Background(gitInputAccent).Foreground(gitInputBg)
		cells := 0
		for i, r := range vis {
			if m.gitCommitFocused && start+i == caret {
				sb.WriteString(caretStyle.Render(string(r)))
			} else {
				sb.WriteString(txt.Render(string(r)))
			}
			cells += runewidth.RuneWidth(r)
		}
		// Caret sitting past the last visible rune → a solid accent block.
		if m.gitCommitFocused && caret >= start+len(vis) && cells < inner {
			sb.WriteString(lipgloss.NewStyle().Background(gitInputAccent).Render(" "))
			cells++
		}
		if cells < inner {
			sb.WriteString(field.Render(strings.Repeat(" ", inner-cells)))
		}
		body = sb.String()
	}

	return lead + field.Render(" ") + body + field.Render(" ")
}

// gitActionBtn is one clickable chip on the action bar. x0..x1 are inclusive
// sidebar-relative columns so the mouse hit-test maps a click back to the id.
type gitActionBtn struct {
	id    string
	label string
	x0    int
	x1    int
	bg    lipgloss.Color
	fg    lipgloss.Color
}

// Action-bar chip IDs.
const (
	gitBtnCommit = "commit"
	gitBtnSync   = "sync"
	gitBtnMore   = "more"
)

// gitActionButtons lays the action row out as one dominant primary plus two
// small secondary squares: a WIDE Commit button fills the left, while a Sync
// (⟳) and an overflow (▾) square sit at the right edge. The single bold primary
// + quiet secondaries is what gives the toolbar a clear hierarchy. Each button
// records its column span so the mouse hit-test maps a click back to the id.
func (m Model) gitActionButtons(w int) []gitActionBtn {
	staged, _ := m.gitFileCounts()
	ready := staged > 0 || strings.TrimSpace(m.gitCommitMsg) != ""

	commitBg, commitFg := gitCommitIdleBg, gitCommitIdleFg
	if ready {
		commitBg, commitFg = gitCommitReadyBg, gitCommitReadyFg
	}

	// Primary label: "✓ Commit" plus the staged count when there is one.
	label := "✓ Commit"
	if staged > 0 {
		label = "✓ Commit (" + itoa(staged) + ")"
	}

	commit := gitActionBtn{id: gitBtnCommit, label: label, bg: commitBg, fg: commitFg}
	sync := gitActionBtn{id: gitBtnSync, label: "⟳", bg: gitChipBg, fg: gitChipFg}
	more := gitActionBtn{id: gitBtnMore, label: "▾", bg: gitChipBg, fg: gitChipFg}

	// Right edge: two 3-wide squares (glyph + 1 pad each side), 1-col gap, 1-col
	// right margin. Commit fills everything to their left from a 1-col margin.
	const sq = 3
	more.x1 = w - 2
	more.x0 = more.x1 - sq + 1
	sync.x1 = more.x0 - 2
	sync.x0 = sync.x1 - sq + 1
	commit.x0 = 1
	commit.x1 = sync.x0 - 2
	return []gitActionBtn{commit, sync, more}
}

// renderGitActionBar draws the primary Commit button (label centered) and the
// two secondary squares at the spans gitActionButtons assigns, filling gaps
// with the panel background so the row is exactly w wide and lines up with the
// hit-test zones.
func (m Model) renderGitActionBar(w int) string {
	rowBg := lipgloss.NewStyle().Background(sidebarBg)
	var sb strings.Builder
	col := 0
	for _, b := range m.gitActionButtons(w) {
		bw := b.x1 - b.x0 + 1
		if b.x0 < col || b.x1 >= w || bw <= 0 {
			continue // doesn't fit — drop it rather than corrupt the row
		}
		if b.x0 > col {
			sb.WriteString(rowBg.Render(strings.Repeat(" ", b.x0-col)))
		}
		style := lipgloss.NewStyle().Background(b.bg).Foreground(b.fg)
		var content string
		if b.id == gitBtnCommit {
			content = centerLabel(b.label, bw)
			style = style.Bold(true)
		} else {
			content = centerLabel(b.label, bw)
		}
		sb.WriteString(style.Render(content))
		col = b.x1 + 1
	}
	if col < w {
		sb.WriteString(rowBg.Render(strings.Repeat(" ", w-col)))
	}
	return sb.String()
}

// centerLabel centers s within width cells (truncating with … if it overflows),
// padding with plain spaces so the caller's bg style fills the button.
func centerLabel(s string, width int) string {
	if width <= 0 {
		return ""
	}
	lw := runewidth.StringWidth(s)
	if lw > width {
		return runewidth.Truncate(s, width, "…")
	}
	left := (width - lw) / 2
	right := width - lw - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// ── mouse ───────────────────────────────────────────────────────────────────

// gitCommitAreaClick handles a left click on the commit box or action bar rows.
// Returns (model, cmd, true) when the click landed in the commit area.
func (m Model) gitCommitAreaClick(x, y int, w int) (tea.Model, tea.Cmd, bool) {
	switch y {
	case gitCommitInputRow:
		m.gitFocusCommitBox()
		// Drop the caret near the clicked column (text starts after the rail +
		// one space, i.e. column 2).
		col := x - 2
		n := len([]rune(m.gitCommitMsg))
		if col < 0 {
			col = 0
		}
		if col > n {
			col = n
		}
		m.gitCommitCaret = col
		return m, nil, true
	case gitActionBarRow:
		for _, b := range m.gitActionButtons(w) {
			if x >= b.x0 && x <= b.x1 {
				return m.gitActionBarDispatch(b.id)
			}
		}
		return m, nil, true
	}
	return m, nil, false
}

// gitActionBarDispatch runs the chip action. Commit and Sync act immediately;
// ⋯ opens the overflow menu anchored under the chip.
func (m Model) gitActionBarDispatch(id string) (tea.Model, tea.Cmd, bool) {
	switch id {
	case gitBtnCommit:
		cmd := m.gitInlineCommit(false, false)
		return m, cmd, true
	case gitBtnSync:
		cmd := m.gitSync()
		return m, cmd, true
	case gitBtnMore:
		m.openGitActionsMenu()
		return m, nil, true
	}
	return m, nil, true
}

// itoa is a tiny strconv.Itoa to keep the import set lean in this file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// padBgRight pads s on the right with spaces in bg until it reaches width w.
func padBgRight(s string, w int, bg lipgloss.Color) string {
	cur := lipgloss.Width(s)
	if cur >= w {
		return s
	}
	return s + lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", w-cur))
}
