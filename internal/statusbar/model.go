package statusbar

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

type State struct {
	Path     string // active buffer path; only used as a "has-a-buffer" probe now
	Project  string // workspace / project basename rendered in the left zone
	Lang     string
	Line     int // 1-based
	Col      int // 1-based
	Dirty    bool
	Err      string
	Errors   int // LSP error count
	Warnings int // LSP warning count
	Branch   string
	Ahead    int
	Behind   int
	Indent   string // e.g. "Spaces: 4" or "Tab Size: 4"
	Encoding string // e.g. "UTF-8"
	Term     bool   // integrated terminal panel is open
	Debug    bool   // a Debug Adapter Protocol session is active
	// TermFocused: the integrated terminal has the keyboard. The right
	// side then shows TermLabel ("zsh · ~/proj") instead of editor facts.
	TermFocused bool
	TermLabel   string

	// LSP chip: ShowLSP turns it on (a file buffer is open); LSP lists the
	// attached client names ("{} gopls"), empty → dim "{} none".
	ShowLSP bool
	LSP     []string

	// Group E: test chip "✓ n ✗ n" in the center (after a run; "◐" while
	// running). Clicking it opens the Testing view (TestsChipSpan).
	ShowTests    bool
	TestsRunning bool
	TestsPassed  int
	TestsFailed  int

	// Ext holds extension status items (Group I), as {{token}} markup.
	// They render at the end of the center cluster; see ext_items.go.
	Ext []string

	// Group A — AI badge "✦ <model>". AI is the label (model name, or
	// "AI" when unconfigured); AIState picks the look. AIOff hides it.
	AI      string
	AIState AIState
}

// AIState is the status-bar AI badge look (Group A).
type AIState int

const (
	AIOff          AIState = iota // no badge
	AIUnconfigured                // dim "✦ AI off" — click to configure
	AIIdle                        // "✦ <model>" in AIAccent
	AIStreaming                   // bold "✦ <model> …"
)

type Model struct {
	w int
}

// New keeps the legacy constructor signature so existing callers compile;
// the bar styles itself directly from the palette and ignores the argument.
func New(_ theme.Theme) Model { return Model{} }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) { return m, nil }

func (m *Model) SetWidth(w int) { m.w = w }

// barBgColor is the dark neutral the entire status bar paints. We use
// BgPanel rather than BgStatusBar so the bar tucks into the dark IDE
// chrome instead of glowing VSCode-blue. Every other style on the bar
// layers a foreground color on top of this background.
func barBgColor() theme.Color256 { return theme.BgPanel }

func barBg() lipgloss.Style { return theme.Bg(barBgColor()) }
func barFg(c theme.Color256) lipgloss.Style {
	return lipgloss.NewStyle().Background(theme.LG(barBgColor())).Foreground(theme.LG(c))
}

// sep is the gap between right-hand items: plain breathing room (the
// pills and colours do the grouping, not pipes).
func sep() string {
	return barBg().Render("  ")
}

// pill draws a status-bar pill on the bar background (rounded Powerline
// caps with a Nerd Font, a padded block otherwise).
func pill(text string, fg theme.Color256, bg lipgloss.Color, bold bool) string {
	return theme.Pill(text, theme.LG(fg), bg, theme.LG(barBgColor()), bold)
}

// glyph picks the icon variant for the active icon mode ("" in ASCII).
func glyph(nerd, uni string) string {
	switch theme.CurrentIconMode {
	case theme.IconModeNerdFont:
		return nerd
	case theme.IconModeUnicode:
		return uni
	}
	return ""
}

func withGlyph(g, text string) string {
	if g == "" {
		return text
	}
	return g + " " + text
}

// barLayout is the fitted content of one status-bar row.
type barLayout struct {
	left, center, right    string
	leftW, centerW, rightW int
	gapL, gapR             int
	chipStart, chipW       int                  // LSP chip offset inside right (chipW 0 = none)
	aiStart, aiW           int                  // Group A: AI badge offset inside right (aiW 0 = none)
	spans                  map[rightItem][2]int // offset, width of each right item shown
	debug                  bool                 // the DEBUG badge is shown (last in right)
}

// Drop order when the bar is too narrow (VSCode-like status-item
// priority): low-value right items go first, then the center cluster,
// then the language and TERM. Items are dropped whole, never cut, so the
// "Ln, Col" pill (and the DEBUG badge) always show complete.
var dropOrder = []rightItem{itemAI, itemLSP, itemEncoding, itemIndent, itemLang, itemCenter, itemTermLabel}

// layout fits left / center / right into the bar width (see View).
func (m Model) layout(s State) barLayout {
	var l barLayout
	l.left = m.renderLeft(s)
	l.center = m.renderCenter(s)
	l.leftW = lipgloss.Width(l.left)
	l.centerW = lipgloss.Width(l.center)

	innerW := m.w - 2 // subtract left+right edge pad
	if innerW < 0 {
		innerW = 0
	}

	drop := map[rightItem]bool{}
	render := func() {
		l.right, l.spans = m.renderRightItems(s, drop)
		l.chipStart, l.chipW = l.spans[itemLSP][0], l.spans[itemLSP][1]
		l.aiStart, l.aiW = l.spans[itemAI][0], l.spans[itemAI][1]
		l.rightW = lipgloss.Width(l.right)
	}
	render()
	// Gap needed between clusters that are both present.
	need := func() int {
		n := l.leftW + l.centerW + l.rightW
		if l.centerW > 0 {
			n += 2 * minClusterGap
		} else if l.rightW > 0 {
			n += minClusterGap
		}
		return n
	}
	for _, it := range dropOrder {
		if need() <= innerW {
			break
		}
		if it == itemCenter {
			l.center, l.centerW = "", 0
			continue
		}
		drop[it] = true
		render()
	}
	// Still too wide: shorten the left (project) cluster.
	if l.leftW+l.rightW+minClusterGap > innerW {
		avail := innerW - l.rightW - minClusterGap
		if avail < 0 {
			avail = 0
		}
		l.left = truncateStyled(l.left, avail)
		l.leftW = lipgloss.Width(l.left)
	}
	// Tiny bar: drop the DEBUG badge, then the cursor pill; never cut one.
	for _, it := range []rightItem{itemDebug, itemPos} {
		if l.leftW+l.rightW <= innerW {
			break
		}
		drop[it] = true
		render()
	}

	l.debug = s.Debug && !drop[itemDebug]

	contentW := l.leftW + l.centerW + l.rightW
	gap := innerW - contentW
	if gap < 0 {
		gap = 0
	}
	l.gapL = gap / 2
	l.gapR = gap - l.gapL
	return l
}

func (m Model) View(s State) string {
	if m.w <= 0 {
		return ""
	}
	return cachedView(m.w, s, func() string { return m.draw(s) })
}

func (m Model) draw(s State) string {
	pad := barBg().Render(" ")
	l := m.layout(s)
	leftSpace := barBg().Render(strings.Repeat(" ", l.gapL))
	rightSpace := barBg().Render(strings.Repeat(" ", l.gapR))

	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		pad, l.left, leftSpace, l.center, rightSpace, l.right, pad,
	)
	// Force the row to exactly m.w cells from lipgloss's perspective so
	// JoinVertical above can't pad it with default-styled spaces.
	return lipgloss.NewStyle().
		Width(m.w).
		Background(theme.LG(barBgColor())).
		Render(body)
}

// LSPChipSpan returns the [x0, x1) cell range of the LSP chip relative to
// the bar's left edge; ok=false when the chip is hidden or truncated.
func (m Model) LSPChipSpan(s State) (x0, x1 int, ok bool) {
	if m.w <= 0 {
		return 0, 0, false
	}
	l := m.layout(s)
	if l.chipW == 0 {
		return 0, 0, false
	}
	rightX := 1 + l.leftW + l.gapL + l.centerW + l.gapR
	return rightX + l.chipStart, rightX + l.chipStart + l.chipW, true
}

// DiagSpan returns the [x0, x1) cell range of the error / warning
// counters relative to the bar's left edge; ok=false when they are hidden
// (no diagnostics, an error message on show, or no room). Group C.
func (m Model) DiagSpan(s State) (x0, x1 int, ok bool) {
	if m.w <= 0 {
		return 0, 0, false
	}
	l := m.layout(s)
	if l.centerW == 0 {
		return 0, 0, false
	}
	_, start, w := m.renderCenterDiag(s)
	if w == 0 {
		return 0, 0, false
	}
	cx := 1 + l.leftW + l.gapL
	return cx + start, cx + start + w, true
}

// AIBadgeSpan returns the [x0, x1) cell range of the AI badge relative to
// the bar's left edge; ok=false when hidden or truncated (Group A).
func (m Model) AIBadgeSpan(s State) (x0, x1 int, ok bool) {
	if m.w <= 0 {
		return 0, 0, false
	}
	l := m.layout(s)
	if l.aiW == 0 {
		return 0, 0, false
	}
	rightX := 1 + l.leftW + l.gapL + l.centerW + l.gapR
	return rightX + l.aiStart, rightX + l.aiStart + l.aiW, true
}

// truncateStyled clips a possibly-styled string to width w cells by
// stripping ANSI escapes, truncating with an ellipsis, and re-styling
// the result with the status-bar palette. Good enough for the trailing
// fields where we just need to fit.
// minClusterGap is the blank space kept between the left and right
// clusters when the right one has to be cut.
const minClusterGap = 2

func truncateStyled(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	plain := stripCSI(s)
	plain = truncate(plain, w)
	return barFg(theme.TextSecondary).Render(plain)
}

func stripCSI(s string) string {
	var out []byte
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			in = true
			i++
			continue
		}
		if in {
			if c >= 0x40 && c <= 0x7e {
				in = false
			}
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

// renderLeft shows the dirty marker (when a buffer is open + modified) and
// a muted workspace/project name. The filename + directory used to live
// here, but that duplicated the breadcrumb row above the editor; the
// breadcrumbs are now the canonical place for path display, so the status
// bar carries only project-level context.
//
// When Err is set it takes over the whole left segment (errors are loud
// on purpose).
func (m Model) renderLeft(s State) string {
	if s.Err != "" {
		return barFg(theme.DiagError).Bold(true).Render("⚠ " + truncate(s.Err, 60))
	}
	// No buffer open: show the muted "[no file]" placeholder where the
	// project name would normally sit.
	if s.Path == "" && s.Project == "" {
		return barFg(theme.TextDim).Render("[no file]")
	}

	if s.Project == "" {
		// Edge case: no project name resolved but a buffer is open —
		// fall back to the placeholder so the left zone is never empty.
		return barFg(theme.TextDim).Render("[no file]")
	}
	// The project pill is the bar's anchor: a soft tint whose colour
	// says what is going on (projectPillTint). A modified buffer shows a
	// dot inside it.
	text := withGlyph(glyph("\uf07c", "◆"), truncate(s.Project, 40))
	if s.Dirty && s.Path != "" {
		text += " " + theme.IconDirty.String()
	}
	return pill(text, theme.TextWhite, projectPillBg(s), true)
}

// projectPillTint is the state colour of the project pill: red in a debug
// session, the AI colour while the AI answers, amber with unsaved changes,
// the accent otherwise.
func projectPillTint(s State) theme.Color256 {
	switch {
	case s.Debug:
		return theme.DiagError
	case s.AIState == AIStreaming:
		return theme.AIAccent
	case s.Dirty && s.Path != "":
		return theme.GitModified
	}
	return theme.BorderFocus
}

// projectPillBg is the pill fill: the bar tinted toward projectPillTint.
func projectPillBg(s State) lipgloss.Color {
	return theme.Mix(barBgColor(), projectPillTint(s), 0.38)
}

// renderCenter shows git branch + LSP diagnostic counters. These are
// optional accents — when none of them apply (or when Err is set and
// the whole bar is given over to the error message) the center is
// empty.
func (m Model) renderCenter(s State) string {
	out, _, _ := m.renderCenterDiag(s)
	return out
}

// renderCenterDiag is renderCenter plus the cell offset / width of the
// diagnostic counters inside the returned string (width 0 when hidden).
// Group C: the counters are clickable (they open the Problems panel).
func (m Model) renderCenterDiag(s State) (string, int, int) {
	if s.Err != "" {
		return "", 0, 0
	}
	var parts []string
	if s.Branch != "" {
		parts = append(parts, barFg(theme.TextSecondary).Render(branchText(s)))
	}
	diagIdx := len(parts)
	if s.Errors > 0 {
		parts = append(parts, barFg(theme.DiagError).Bold(true).Render(
			fmt.Sprintf("%s %d", theme.IconError.String(), s.Errors)))
	}
	if s.Warnings > 0 {
		parts = append(parts, barFg(theme.DiagWarning).Bold(true).Render(
			fmt.Sprintf("%s %d", theme.IconWarning.String(), s.Warnings)))
	}
	diagEnd := len(parts)
	parts = append(parts, renderExtParts(s.Ext)...) // Group I
	if chip := renderTestsChip(s); chip != "" {     // Group E (always last)
		parts = append(parts, chip)
	}
	if len(parts) == 0 {
		return "", 0, 0
	}
	gap := barBg().Render("  ")
	out := strings.Join(parts, gap)
	if diagIdx == diagEnd {
		return out, 0, 0 // no counters
	}
	start := 0
	if diagIdx > 0 {
		start = lipgloss.Width(strings.Join(parts[:diagIdx], gap) + gap)
	}
	// Only the counters: the extension items and the test chip after them
	// are click targets of their own.
	return out, start, lipgloss.Width(strings.Join(parts[diagIdx:diagEnd], gap))
}

// branchText is the center cluster's branch item ("⌥ main ↑1").
func branchText(s State) string {
	t := withGlyph(glyph("\ue725", "⌥"), s.Branch)
	if theme.CurrentIconMode == theme.IconModeASCII {
		t = theme.IconBranch.String() + " " + s.Branch
	}
	if s.Ahead > 0 {
		t += fmt.Sprintf(" ↑%d", s.Ahead)
	}
	if s.Behind > 0 {
		t += fmt.Sprintf(" ↓%d", s.Behind)
	}
	return t
}

// rightItem names one part of the right cluster (see dropOrder).
type rightItem int

const (
	itemIndent rightItem = iota
	itemEncoding
	itemLang
	itemLSP
	itemAI
	itemTermLabel
	itemPos
	itemDebug
	itemCenter // the whole center cluster (only used in dropOrder)
)

// renderRight shows cursor position, indent style, encoding, language,
// the LSP chip, and (if active) the DEBUG pill; with the terminal focused,
// the shell and its folder instead.
func (m Model) renderRight(s State) string {
	out, _ := m.renderRightItems(s, nil)
	return out
}

// renderRightItems is renderRight without the items in drop, plus the cell
// offset and width of each item shown inside the returned string.
func (m Model) renderRightItems(s State, drop map[rightItem]bool) (string, map[rightItem][2]int) {
	var parts []string
	var kinds []rightItem
	add := func(k rightItem, p string) {
		if drop[k] {
			return
		}
		parts = append(parts, p)
		kinds = append(kinds, k)
	}
	if s.TermFocused {
		// The terminal has the keyboard: editor facts do not apply.
		if s.TermLabel != "" {
			add(itemTermLabel, barFg(theme.TextSecondary).Render(s.TermLabel))
		}
		if s.AIState != AIOff {
			add(itemAI, renderAIBadge(s.AI, s.AIState))
		}
		if s.Debug {
			add(itemDebug, pill(debugBadgeText, theme.OnAccent, theme.LG(theme.DiagError), true))
		}
		return m.joinRight(parts, kinds)
	}
	if s.Indent != "" {
		add(itemIndent, barFg(theme.TextSecondary).Render(s.Indent))
	}
	enc := s.Encoding
	if enc == "" {
		enc = "UTF-8"
	}
	add(itemEncoding, barFg(theme.TextSecondary).Render(enc))
	if s.Lang != "" {
		var lang string
		if g, c := theme.FileIconFor(s.Path, false, false); s.Path != "" && theme.CurrentIconMode != theme.IconModeASCII {
			lang = barFg(c).Render(g) + barBg().Render(" ") + barFg(theme.TextSecondary).Render(s.Lang)
		} else {
			lang = barFg(theme.TextSecondary).Render(s.Lang)
		}
		add(itemLang, lang)
	}
	if s.ShowLSP {
		add(itemLSP, renderLSPChip(s.LSP))
	}
	if s.AIState != AIOff {
		add(itemAI, renderAIBadge(s.AI, s.AIState))
	}
	// Cursor position sits in a soft accent pill near the right edge.
	add(itemPos, pill(fmt.Sprintf("Ln %d, Col %d", s.Line, s.Col), theme.TextWhite,
		theme.Wash(barBgColor(), theme.WashPressed), false))
	if s.Debug {
		// An active debug session is high-attention: a red pill, always
		// last (DebugBadgeSpan relies on that).
		add(itemDebug, pill(debugBadgeText, theme.OnAccent, theme.LG(theme.DiagError), true))
	}
	return m.joinRight(parts, kinds)
}

// joinRight joins the right-side items and records each one's span.
func (m Model) joinRight(parts []string, kinds []rightItem) (string, map[rightItem][2]int) {
	spans := make(map[rightItem][2]int, len(parts))
	start, sepW := 0, lipgloss.Width(sep())
	for i, p := range parts {
		w := lipgloss.Width(p)
		spans[kinds[i]] = [2]int{start, w}
		start += w + sepW
	}
	return strings.Join(parts, sep()), spans
}

// ItemSpan is a plain status-bar item's cell range [X0, X1) relative to
// the bar's left edge (hover labels name them).
type ItemSpan struct {
	ID     string // "branch", "indent", "encoding", "lang", "term", "pos"
	X0, X1 int
}

// ItemSpans lists the plain items on show: the branch and the right
// cluster's indent, encoding, language, TERM and cursor position. The
// clickable chips have spans of their own (LSPChipSpan, …).
func (m Model) ItemSpans(s State) []ItemSpan {
	if m.w <= 0 {
		return nil
	}
	l := m.layout(s)
	var out []ItemSpan
	if l.centerW > 0 && s.Err == "" && s.Branch != "" {
		// The branch is always the first part of the center cluster.
		cx := 1 + l.leftW + l.gapL
		w := lipgloss.Width(branchText(s))
		out = append(out, ItemSpan{"branch", cx, cx + w})
	}
	rightX := 1 + l.leftW + l.gapL + l.centerW + l.gapR
	for _, it := range []struct {
		k  rightItem
		id string
	}{{itemIndent, "indent"}, {itemEncoding, "encoding"}, {itemLang, "lang"}, {itemTermLabel, "term"}, {itemPos, "pos"}} {
		if sp, ok := l.spans[it.k]; ok && sp[1] > 0 {
			out = append(out, ItemSpan{it.id, rightX + sp[0], rightX + sp[0] + sp[1]})
		}
	}
	return out
}

// renderAIBadge draws the Group A "✦ <model>" badge.
func renderAIBadge(label string, st AIState) string {
	switch st {
	case AIUnconfigured:
		return barFg(theme.TextDim).Render("✦ AI off")
	case AIStreaming:
		return barFg(theme.AIAccent).Bold(true).Render("✦ " + truncate(label, 24) + " …")
	}
	return barFg(theme.AIAccent).Render("✦ " + truncate(label, 24))
}

// renderTestsChip is the Group E test chip: "✓ 12 ✗ 1" (◐ prefix while a
// run is in progress), or "" when hidden.
func renderTestsChip(s State) string {
	if !s.ShowTests {
		return ""
	}
	var b strings.Builder
	if s.TestsRunning {
		b.WriteString(barFg(theme.AccentAmber).Render("◐ "))
	}
	b.WriteString(barFg(theme.AccentGreen).Render(fmt.Sprintf("✓ %d", s.TestsPassed)))
	b.WriteString(barBg().Render(" "))
	failFg := theme.TextSecondary
	if s.TestsFailed > 0 {
		failFg = theme.DiagError
	}
	b.WriteString(barFg(failFg).Bold(s.TestsFailed > 0).Render(fmt.Sprintf("✗ %d", s.TestsFailed)))
	return b.String()
}

// TestsChipSpan returns the [x0, x1) cell range of the test chip relative
// to the bar's left edge; ok=false when hidden or dropped for width. The
// chip is always the last part of the center cluster.
func (m Model) TestsChipSpan(s State) (x0, x1 int, ok bool) {
	chip := renderTestsChip(s)
	if m.w <= 0 || chip == "" || s.Err != "" {
		return 0, 0, false
	}
	l := m.layout(s)
	if l.centerW == 0 {
		return 0, 0, false
	}
	cw := lipgloss.Width(chip)
	end := 1 + l.leftW + l.gapL + l.centerW
	return end - cw, end, true
}

// renderLSPChip is "{} gopls" (attached clients) or a dim "{} none".
func renderLSPChip(clients []string) string {
	if len(clients) == 0 {
		return barFg(theme.TextDim).Render("{} none")
	}
	return barFg(theme.TextSecondary).Render("{} " + strings.Join(clients, ", "))
}

// truncate clips s to n cells with a trailing "…". It counts cells, not
// bytes, and never cuts a multi-byte glyph (a cut glyph is invalid UTF-8:
// the terminal draws extra cells and the row wraps).
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}
