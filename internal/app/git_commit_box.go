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
	gitInputBg      = lipgloss.Color("#252526")
	gitInputFg      = lipgloss.Color("#d6d6d6")
	gitInputPlace   = lipgloss.Color("#6a6a6a")
	gitChipBg       = lipgloss.Color("#2d2d30")
	gitChipFg       = lipgloss.Color("#cccccc")
	gitCommitChipBg = lipgloss.Color("#2ea043")
	gitCommitChipFg = lipgloss.Color("#ffffff")
	gitChipDisabled = lipgloss.Color("#5a5a5a")
)

// gitCommitPlaceholder is shown dim in the empty message box.
const gitCommitPlaceholder = "Message (c to edit)"

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
	inner := w - 2 // one column of padding on each side

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
		caret := m.gitCommitCaret
		if caret > len(runes) {
			caret = len(runes)
		}
		start := 0
		if caret >= inner {
			start = caret - inner + 1
		}
		end := start + inner
		if end > len(runes) {
			end = len(runes)
		}
		vis := runes[start:end]

		var sb strings.Builder
		txt := lipgloss.NewStyle().Background(gitInputBg).Foreground(gitInputFg)
		caretStyle := lipgloss.NewStyle().Background(gitInputFg).Foreground(gitInputBg)
		cells := 0
		for i, r := range vis {
			if m.gitCommitFocused && start+i == caret {
				sb.WriteString(caretStyle.Render(string(r)))
			} else {
				sb.WriteString(txt.Render(string(r)))
			}
			cells += runewidth.RuneWidth(r)
		}
		// Caret sitting past the last visible rune → a reverse-video space.
		if m.gitCommitFocused && caret >= start+len(vis) && cells < inner {
			sb.WriteString(lipgloss.NewStyle().Background(gitInputFg).Render(" "))
			cells++
		}
		if cells < inner {
			sb.WriteString(lipgloss.NewStyle().Background(gitInputBg).Render(strings.Repeat(" ", inner-cells)))
		}
		body = sb.String()
	}

	pad := field.Render(" ")
	return pad + body + pad
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

// gitActionButtons lays the chips out left-to-right and records their column
// spans. The Commit chip turns green only when something is staged; otherwise
// it stays muted to signal "nothing to commit yet" without disabling the click
// (clicking offers to stage-all).
func (m Model) gitActionButtons(w int) []gitActionBtn {
	staged, _ := m.gitFileCounts()

	commitBg, commitFg := gitChipBg, lipgloss.Color(gitChipDisabled)
	if staged > 0 || strings.TrimSpace(m.gitCommitMsg) != "" {
		commitBg, commitFg = gitCommitChipBg, gitCommitChipFg
	}

	syncLabel := "⟳ Sync"
	if git := m.gitBranch; git.Ahead > 0 || git.Behind > 0 {
		var b strings.Builder
		b.WriteString("⟳")
		if git.Behind > 0 {
			b.WriteString(" ↓")
			b.WriteString(itoa(git.Behind))
		}
		if git.Ahead > 0 {
			b.WriteString(" ↑")
			b.WriteString(itoa(git.Ahead))
		}
		syncLabel = b.String()
	}

	specs := []gitActionBtn{
		{id: gitBtnCommit, label: " ✓ Commit ", bg: commitBg, fg: commitFg},
		{id: gitBtnSync, label: " " + syncLabel + " ", bg: gitChipBg, fg: gitChipFg},
		{id: gitBtnMore, label: " ⋯ ", bg: gitChipBg, fg: gitChipFg},
	}
	// Place chips with a single space gap, starting one column in.
	x := 1
	for i := range specs {
		lw := runewidth.StringWidth(specs[i].label)
		specs[i].x0 = x
		specs[i].x1 = x + lw - 1
		x += lw + 1
	}
	return specs
}

// renderGitActionBar draws the chips returned by gitActionButtons, padded to w.
func (m Model) renderGitActionBar(w int) string {
	rowBg := lipgloss.NewStyle().Background(sidebarBg)
	var sb strings.Builder
	sb.WriteString(rowBg.Render(" "))
	used := 1
	for i, b := range m.gitActionButtons(w) {
		if i > 0 {
			sb.WriteString(rowBg.Render(" "))
			used++
		}
		lw := runewidth.StringWidth(b.label)
		if used+lw > w {
			break
		}
		sb.WriteString(lipgloss.NewStyle().Background(b.bg).Foreground(b.fg).Bold(b.id == gitBtnCommit).Render(b.label))
		used += lw
	}
	if used < w {
		sb.WriteString(rowBg.Render(strings.Repeat(" ", w-used)))
	}
	return sb.String()
}

// ── mouse ───────────────────────────────────────────────────────────────────

// gitCommitAreaClick handles a left click on the commit box or action bar rows.
// Returns (model, cmd, true) when the click landed in the commit area.
func (m Model) gitCommitAreaClick(x, y int, w int) (tea.Model, tea.Cmd, bool) {
	switch y {
	case gitCommitInputRow:
		m.gitFocusCommitBox()
		// Drop the caret near the clicked column (best-effort, 1 col padding).
		col := x - 1
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
