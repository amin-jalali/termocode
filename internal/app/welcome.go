package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"termocode/internal/keymap"
	"termocode/internal/theme"
)

// ─── Welcome screen — modern start page ─────────────────────────────────
//
// Sections (top → bottom, centered horizontally inside the editor pane):
//
//   1. Logo block        – ASCII "Termocode" + letter-spaced tagline
//   2. START             – 2×2 grid of action cards w/ shortcut keycaps
//   3. RECENT FILES + WORKSPACES (two-column row)
//   4. Footer            – horizontal rule + version line + F1 keycap
//
// Render-width contract: every emitted line fills its container width
// exactly; padding everywhere goes through theme.Bg(...) styled spaces,
// never bare whitespace, so the terminal default bg can never leak
// through overlay splices.
//
// Color contract: this file MUST NOT contain raw hex literals
// (lipgloss.Color("#…")). Every color is a theme token — accents,
// background tints, and the four-tier text emphasis ladder all come
// from internal/theme.

// ─── Sizing constants ──────────────────────────────────────────────────

// welcomeContentWidth caps the inner content width on wide terminals so
// the start page reads as a focused page, not a sprawling form.
const welcomeContentWidth = 88

// welcomeWideMin is the threshold below which the action grid collapses
// to a single column and the recent-files / workspaces row stacks
// vertically. Mirrors the spec's "narrower than ~80 cols" rule.
const welcomeWideMin = 80

// welcomeNarrowMin is the floor below which subtitles drop from the
// action cards (icon + title + shortcut only).
const welcomeNarrowMin = 50

// welcomeBannerWidth is preserved as the visual width of the optional
// faint backdrop banner. Kept exported via welcomeBanner for legacy
// callers / tests.
const welcomeBannerWidth = 43

// welcomeColumnGap is the cell gap between recents and workspaces in the
// wide split layout.
const welcomeColumnGap = 6

// welcomeRulerWidth is preserved for the optional banner fallback math
// (kept const so existing tests that reference it don't drift).
const welcomeRulerWidth = 42

// welcomeOuterPadCols is the column padding inside the editor pane
// before the centered content column starts (3 cols left, 3 cols right).
const welcomeOuterPadCols = 3

// welcomeOuterPadTop / welcomeOuterPadBottom are the row paddings
// reserved at the top/bottom of the editor pane around the centered
// content. Exact when the pane is tall; auto-shrunk when it isn't.
const welcomeOuterPadTop = 3
const welcomeOuterPadBottom = 2

// welcomeSentinelViewAll / welcomeSentinelBrowseAll are magic Path values
// used by the recent / workspace hit lists for the "view all → " and
// "browse all → " link rows. The mouse handler intercepts these and
// opens the recents / workspaces pickers respectively.
const welcomeSentinelViewAll = "\x00view-all"
const welcomeSentinelBrowseAll = "\x00browse-all"

// ─── ASCII logo ────────────────────────────────────────────────────────

// welcomeBanner returns the 4-row "Termocode" banner when there's room
// for it, falling back to plain text on narrower terminals. Kept on the
// API surface (callers / older tests reference it).
func welcomeBanner(style lipgloss.Style, w int) []string {
	if w >= welcomeBannerWidth {
		plain := style.Bold(false)
		return []string{
			plain.Render(` _____                   ___         _     `),
			plain.Render(`|_   _|__ _ _ _ __  ___ / __|___  __| |___ `),
			plain.Render(`  | |/ -_) '_| '  \/ _ \ (__/ _ \/ _` + "`" + ` / -_)`),
			plain.Render(`  |_|\___|_| |_|_|_\___/\___\___/\__,_\___|`),
		}
	}
	return []string{style.Bold(true).Render("Termocode")}
}

// ─── Hit-zone types ────────────────────────────────────────────────────

// welcomeRecentHit binds an editor-pane row + col span to a clickable
// recents target. ColEnd == 0 means full-row hit (legacy fallback).
//
// `Path` carries the absolute file path for normal entries, and the
// sentinel value welcomeSentinelViewAll for the "view all → " row.
type welcomeRecentHit struct {
	Row      int
	Path     string
	ColStart int
	ColEnd   int
}

// welcomeWorkspaceHit is the workspaces-column counterpart of
// welcomeRecentHit.
type welcomeWorkspaceHit struct {
	Row      int
	Path     string
	ColStart int
	ColEnd   int
}

// welcomeQuickActionHit binds a row+col rectangle to a keymap action so
// clicks on the action cards dispatch the same handler as the keyboard
// binding.
type welcomeQuickActionHit struct {
	Row      int
	ColStart int
	ColEnd   int
	Action   keymap.Action
}

// ─── Public API (kept stable) ──────────────────────────────────────────

// renderWelcome returns the centered welcome screen for the editor pane.
// `focusIdx` selects which action card is highlighted (0..3).
func renderWelcome(w, h, focusIdx int) string {
	rendered, _, _, _ := renderWelcomeFull(w, h, focusIdx)
	return rendered
}

// renderWelcomeLayout returns the recent + workspace hits expected by
// mouse.go. Quick-action hits are surfaced via welcomeQuickActionHits.
func renderWelcomeLayout(w, h, focusIdx int) (string, []welcomeRecentHit, []welcomeWorkspaceHit) {
	rendered, rh, wh, _ := renderWelcomeFull(w, h, focusIdx)
	return rendered, rh, wh
}

// welcomeRecentHits computes the recents hit map without surfacing the
// rendered string.
func welcomeRecentHits(w, h int) []welcomeRecentHit {
	_, hits, _, _ := renderWelcomeFull(w, h, 0)
	return hits
}

// welcomeWorkspaceHits returns clickable workspace rectangles on the
// welcome screen with their absolute editor-pane coordinates.
func welcomeWorkspaceHits(w, h int) []welcomeWorkspaceHit {
	_, _, hits, _ := renderWelcomeFull(w, h, 0)
	return hits
}

// welcomeQuickActionHits returns clickable rectangles for the action
// cards. Each entry carries a keymap.Action that the mouse handler
// dispatches through the same path the keyboard uses.
func welcomeQuickActionHits(w, h int) []welcomeQuickActionHit {
	_, _, _, hits := renderWelcomeFull(w, h, 0)
	return hits
}

// ─── Action card data ──────────────────────────────────────────────────

// quickAction is one card in the START grid. `iconColor` is a theme
// color slot (resolved at render time so theme switches re-tint the
// icon without rebuilding the slice).
type quickAction struct {
	icon     string
	title    string
	subtitle string
	shortcut string
	iconTok  theme.Color256
	action   keymap.Action
}

func welcomeQuickActions() []quickAction {
	return []quickAction{
		{
			icon:     "+",
			title:    "Open file",
			subtitle: "browse your filesystem",
			shortcut: "^P",
			iconTok:  theme.AccentBlue,
			action:   keymap.ActionQuickOpen,
		},
		{
			icon:     ">",
			title:    "Command palette",
			subtitle: "run any action",
			shortcut: "F1",
			iconTok:  theme.AccentGreen,
			action:   keymap.ActionCommandPalette,
		},
		{
			icon:     "?",
			title:    "Find in files",
			subtitle: "project-wide search",
			shortcut: "F8",
			iconTok:  theme.AccentAmber,
			action:   keymap.ActionWorkspaceSearch,
		},
		{
			icon:     "$",
			title:    "Open shell",
			subtitle: "integrated terminal",
			shortcut: "^T",
			iconTok:  theme.AccentMagenta,
			action:   keymap.ActionToggleTerminal,
		},
	}
}

// ─── Render entry point ────────────────────────────────────────────────

// renderWelcomeFull produces the rendered string and all three hit-zone
// slices. Splitting the work this way keeps the public layout helpers
// thin without recomputing layout for each accessor.
func renderWelcomeFull(w, h, focusIdx int) (string, []welcomeRecentHit, []welcomeWorkspaceHit, []welcomeQuickActionHit) {
	if w <= 0 || h <= 0 {
		return "", nil, nil, nil
	}

	actions := welcomeQuickActions()
	if focusIdx < 0 {
		focusIdx = 0
	}
	if len(actions) > 0 && focusIdx >= len(actions) {
		focusIdx = len(actions) - 1
	}

	bgFill := theme.Bg(theme.BgEditor)

	// Inner content width — capped so the start page reads as a focused
	// page on ultra-wide terminals.
	innerW := w - welcomeOuterPadCols*2
	if innerW > welcomeContentWidth {
		innerW = welcomeContentWidth
	}
	if innerW < 24 {
		innerW = w
		if innerW < 24 {
			innerW = 24
		}
	}
	wide := innerW >= welcomeWideMin
	showSubtitle := innerW >= welcomeNarrowMin

	var contentLines []string

	// ── 1. Logo + tagline ─────────────────────────────────────────────
	logoStyle := theme.FgBg(theme.AccentBlue, theme.BgEditor).Bold(true)
	tagStyle := theme.FgBg(theme.TextMuted, theme.BgEditor)
	for _, b := range welcomeBanner(logoStyle, innerW) {
		contentLines = append(contentLines, centerInWidth(b, innerW))
	}
	contentLines = append(contentLines, blank(innerW))
	tagline := "A TERMINAL-NATIVE EDITOR"
	if wide {
		tagline = spaceLetters(tagline)
	}
	contentLines = append(contentLines, centerInWidth(tagStyle.Render(tagline), innerW))
	contentLines = append(contentLines, blank(innerW), blank(innerW), blank(innerW))

	// ── 2. START — 2×2 (or 1×4 on narrow) grid of action cards ────────
	contentLines = append(contentLines, padRowToWidth(sectionHeader("START"), innerW, bgFill))
	contentLines = append(contentLines, blank(innerW))
	qaLines, qaHits := buildActionGrid(actions, innerW, len(contentLines), focusIdx, wide, showSubtitle)
	contentLines = append(contentLines, qaLines...)

	contentLines = append(contentLines, blank(innerW), blank(innerW), blank(innerW))

	// ── 3. RECENT FILES + WORKSPACES ──────────────────────────────────
	recents := loadRecents()
	workspaces := loadWorkspaces()

	var recentLineIdx []int
	var recentPaths []string
	var wsLineIdx []int
	var wsPaths []string
	var recentColStart, recentColEnd int
	var wsColStart, wsColEnd int

	if wide {
		colW := (innerW - welcomeColumnGap) / 2
		recentBlock, rIdx, rPaths := buildRecentsColumn(recents, colW)
		wsBlock, wIdx, wPaths := buildWorkspacesColumn(workspaces, colW)
		nMax := max(len(recentBlock), len(wsBlock))
		emptyCell := bgFill.Render(strings.Repeat(" ", colW))
		startRow := len(contentLines)
		gap := bgFill.Render(strings.Repeat(" ", welcomeColumnGap))
		for i := 0; i < nMax; i++ {
			var left, right string
			if i < len(recentBlock) {
				left = recentBlock[i]
			} else {
				left = emptyCell
			}
			if i < len(wsBlock) {
				right = wsBlock[i]
			} else {
				right = emptyCell
			}
			contentLines = append(contentLines, left+gap+right)
		}
		for k, ridx := range rIdx {
			recentLineIdx = append(recentLineIdx, startRow+ridx)
			recentPaths = append(recentPaths, rPaths[k])
		}
		for k, widx := range wIdx {
			wsLineIdx = append(wsLineIdx, startRow+widx)
			wsPaths = append(wsPaths, wPaths[k])
		}
		recentColStart = 0
		recentColEnd = colW
		wsColStart = colW + welcomeColumnGap
		wsColEnd = colW + welcomeColumnGap + colW
	} else {
		// Stacked: recents first, then workspaces.
		recentBlock, rIdx, rPaths := buildRecentsColumn(recents, innerW)
		startRow := len(contentLines)
		contentLines = append(contentLines, recentBlock...)
		for k, ridx := range rIdx {
			recentLineIdx = append(recentLineIdx, startRow+ridx)
			recentPaths = append(recentPaths, rPaths[k])
		}
		contentLines = append(contentLines, blank(innerW), blank(innerW))
		wsBlock, wIdx, wPaths := buildWorkspacesColumn(workspaces, innerW)
		wsStart := len(contentLines)
		contentLines = append(contentLines, wsBlock...)
		for k, widx := range wIdx {
			wsLineIdx = append(wsLineIdx, wsStart+widx)
			wsPaths = append(wsPaths, wPaths[k])
		}
	}

	// ── 4. Footer ─────────────────────────────────────────────────────
	contentLines = append(contentLines, blank(innerW), blank(innerW), blank(innerW))
	rule := footerRule(innerW)
	contentLines = append(contentLines, rule)
	contentLines = append(contentLines, blank(innerW))
	contentLines = append(contentLines, footerRow(innerW))

	// ── Pad each row to exactly innerW ────────────────────────────────
	for i, line := range contentLines {
		cur := lipgloss.Width(line)
		if cur < innerW {
			contentLines[i] = line + bgFill.Render(strings.Repeat(" ", innerW-cur))
		}
	}

	// ── Center horizontally inside w; vertically inside h ─────────────
	leftPad := (w - innerW) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	rightPad := w - innerW - leftPad
	if rightPad < 0 {
		rightPad = 0
	}
	rowsTotal := len(contentLines)
	topPad := (h - rowsTotal) / 2
	if topPad < 0 {
		topPad = 0
	}
	bottomPad := h - rowsTotal - topPad
	if bottomPad < 0 {
		bottomPad = 0
	}

	var sb strings.Builder
	emptyRow := bgFill.Render(strings.Repeat(" ", w))
	for i := 0; i < topPad; i++ {
		sb.WriteString(emptyRow)
		sb.WriteString("\n")
	}
	for i, line := range contentLines {
		sb.WriteString(bgFill.Render(strings.Repeat(" ", leftPad)))
		sb.WriteString(line)
		sb.WriteString(bgFill.Render(strings.Repeat(" ", rightPad)))
		if i < len(contentLines)-1 {
			sb.WriteString("\n")
		}
	}
	for i := 0; i < bottomPad; i++ {
		sb.WriteString("\n")
		sb.WriteString(emptyRow)
	}

	// Translate hit coordinates to the absolute editor-pane frame.
	rHits := make([]welcomeRecentHit, len(recentLineIdx))
	for i, idx := range recentLineIdx {
		hit := welcomeRecentHit{Row: topPad + idx, Path: recentPaths[i]}
		if recentColEnd > 0 {
			hit.ColStart = leftPad + recentColStart
			hit.ColEnd = leftPad + recentColEnd
		} else {
			hit.ColStart = leftPad
			hit.ColEnd = leftPad + innerW
		}
		rHits[i] = hit
	}
	wHits := make([]welcomeWorkspaceHit, len(wsLineIdx))
	for i, idx := range wsLineIdx {
		hit := welcomeWorkspaceHit{Row: topPad + idx, Path: wsPaths[i]}
		if wsColEnd > 0 {
			hit.ColStart = leftPad + wsColStart
			hit.ColEnd = leftPad + wsColEnd
		} else {
			hit.ColStart = leftPad
			hit.ColEnd = leftPad + innerW
		}
		wHits[i] = hit
	}
	qHits := make([]welcomeQuickActionHit, 0, len(qaHits))
	for _, h := range qaHits {
		h.Row += topPad
		h.ColStart += leftPad
		h.ColEnd += leftPad
		qHits = append(qHits, h)
	}

	return sb.String(), rHits, wHits, qHits
}

// ─── Section header / footer pieces ────────────────────────────────────

// sectionHeader renders a small caps + letter-spaced section header
// in the muted (TextDim) color. Used for "START", "RECENT FILES",
// "WORKSPACES".
func sectionHeader(s string) string {
	style := theme.FgBg(theme.TextDim, theme.BgEditor)
	return style.Render(spaceLetters(s))
}

// footerRule renders a horizontal divider made of a single-row run of
// the panel border-grey character. Same color as the action grid
// dividers — communicates "the page ends here".
func footerRule(w int) string {
	style := theme.FgBg(theme.BorderSubtle, theme.BgEditor)
	return style.Render(strings.Repeat("─", w))
}

// footerRow lays out the version line on the left and the "F1 for help"
// keycap row on the right. Both halves are themed; padding between is
// styled BgEditor so no terminal default bg leaks.
func footerRow(w int) string {
	bg := theme.Bg(theme.BgEditor)
	versionStyle := theme.FgBg(theme.TextQuaternary, theme.BgEditor)
	helpStyle := theme.FgBg(theme.TextMuted, theme.BgEditor)

	left := versionStyle.Render(welcomeVersionLine())
	right := keycap("F1") + bg.Render(" ") + helpStyle.Render("for help")

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := w - leftW - rightW
	if gap < 1 {
		gap = 1
		// If we'd overflow, drop the version line entirely.
		if leftW+rightW+1 > w {
			left = ""
			leftW = 0
			gap = w - rightW
			if gap < 0 {
				gap = 0
			}
		}
	}
	return left + bg.Render(strings.Repeat(" ", gap)) + right
}

// welcomeVersionLine returns "v<ver> · go <maj.min>". The version comes
// from build info when available; falls back to a placeholder so the
// footer never crashes the build.
func welcomeVersionLine() string {
	ver := "v0.0.0"
	if info, ok := debug.ReadBuildInfo(); ok {
		v := strings.TrimSpace(info.Main.Version)
		if v != "" && v != "(devel)" {
			if !strings.HasPrefix(v, "v") {
				v = "v" + v
			}
			ver = v
		}
	}
	return fmt.Sprintf("%s  %s  go %s", ver, "·", shortGoVersion())
}

// shortGoVersion turns runtime.Version() ("go1.22.3") into "1.22".
func shortGoVersion() string {
	v := runtime.Version()
	v = strings.TrimPrefix(v, "go")
	// Take only the major.minor pair for a compact footer.
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

// ─── Action grid ───────────────────────────────────────────────────────

// buildActionGrid lays out the four action cards as a 2×2 panel (or a
// 1-column stack when innerW < welcomeWideMin). No outer border; the
// only chrome is a single-cell vertical divider between columns and a
// single-cell horizontal divider between rows, both in BorderSubtle.
//
// `lineOffset` is the running line count of the surrounding contentLines
// slice at the point where the grid is appended; hits returned have
// Row computed in that absolute frame.
func buildActionGrid(actions []quickAction, innerW, lineOffset, focusIdx int, wide, showSubtitle bool) ([]string, []welcomeQuickActionHit) {
	if len(actions) == 0 {
		return nil, nil
	}
	cols := 1
	if wide {
		cols = 2
	}
	rows := (len(actions) + cols - 1) / cols
	cardW := (innerW - (cols - 1)) / cols // cols-1 = number of vertical dividers

	// Card height: 2 content rows when subtitle visible, 1 when not.
	// Plus 1 row of internal vertical padding above and below the
	// content => 3 (or 4 with subtitle) rows per card.
	contentRows := 1
	if showSubtitle {
		contentRows = 2
	}
	cardH := contentRows + 2 // 1 pad row top + content + 1 pad row bottom

	bgFill := theme.Bg(theme.BgEditor)
	dividerStyle := theme.FgBg(theme.BorderSubtle, theme.BgEditor)
	vDivider := dividerStyle.Render("│")
	// The horizontal divider is one row of "─" characters spanning the
	// full innerW; where the column boundary falls, we use a "┼" so the
	// internal cross-divider reads as a continuous line.
	hDivider := buildHDivider(innerW, cols, cardW, dividerStyle)

	var allLines []string
	var allHits []welcomeQuickActionHit

	for r := 0; r < rows; r++ {
		startIdx := r * cols
		endIdx := startIdx + cols
		if endIdx > len(actions) {
			endIdx = len(actions)
		}
		rowActs := actions[startIdx:endIdx]
		// Build per-row card bodies (each body is `cardH` lines wide).
		bodies := make([][]string, len(rowActs))
		for i, a := range rowActs {
			focused := startIdx+i == focusIdx
			bodies[i] = renderActionCard(a, cardW, contentRows, focused, showSubtitle)
		}
		// Compose row by row.
		for ri := 0; ri < cardH; ri++ {
			var sb strings.Builder
			for ci, body := range bodies {
				if ci > 0 {
					sb.WriteString(vDivider)
				}
				if ri < len(body) {
					sb.WriteString(body[ri])
				} else {
					sb.WriteString(bgFill.Render(strings.Repeat(" ", cardW)))
				}
			}
			// If we have fewer cards than cols (last row, narrow odd
			// count), pad the trailing column with bg-fill so the row
			// width matches.
			cur := lipgloss.Width(sb.String())
			if cur < innerW {
				sb.WriteString(bgFill.Render(strings.Repeat(" ", innerW-cur)))
			}
			allLines = append(allLines, sb.String())
		}
		// Per-card hit zones: cover the whole card body height.
		rowStartLine := lineOffset + len(allLines) - cardH
		for i := range rowActs {
			colStart := i * (cardW + 1) // +1 = vertical divider
			colEnd := colStart + cardW
			for k := 0; k < cardH; k++ {
				allHits = append(allHits, welcomeQuickActionHit{
					Row:      rowStartLine + k,
					ColStart: colStart,
					ColEnd:   colEnd,
					Action:   rowActs[i].action,
				})
			}
		}
		// Horizontal divider was previously rendered between rows but
		// looked like a hazy underline beneath any focused card (the
		// focused card's BgHover bg made the BorderSubtle line visually
		// pop). The cards' own top/bottom padding rows already separate
		// rows visually; the vertical divider between columns is enough
		// chrome.
		_ = hDivider
		_ = r
	}
	return allLines, allHits
}

// buildHDivider builds a single-row horizontal divider whose column
// boundaries (where the vertical divider in the row above/below meets
// it) are rendered as "┼" so the cross is continuous.
func buildHDivider(innerW, cols, cardW int, style lipgloss.Style) string {
	if cols <= 1 {
		return style.Render(strings.Repeat("─", innerW))
	}
	var sb strings.Builder
	for c := 0; c < cols; c++ {
		sb.WriteString(strings.Repeat("─", cardW))
		if c < cols-1 {
			sb.WriteString("┼")
		}
	}
	out := sb.String()
	if w := runewidth.StringWidth(out); w < innerW {
		out += strings.Repeat("─", innerW-w)
	}
	return style.Render(out)
}

// renderActionCard renders one card's body lines (cardH = contentRows+2).
// Layout per card:
//
//	(blank pad row)
//	[icon]  [title]                   [shortcut keycap]
//	        [subtitle]                                (optional)
//	(blank pad row)
//
// Icon is colored per-action; title/subtitle/shortcut use the theme
// emphasis ladder; the keycap uses BgKeycap. When `focused` is true the
// card body is tinted with BgHover and the title gets the AccentLavender
// foreground so the active card visually pops.
func renderActionCard(a quickAction, cardW, contentRows int, focused, showSubtitle bool) []string {
	// Choose card background tier.
	cardBgTok := theme.BgEditor
	if focused {
		cardBgTok = theme.BgHover
	}
	cardBg := theme.Bg(cardBgTok)

	titleTok := theme.TextPrimary
	if focused {
		titleTok = theme.AccentLavender
	}
	titleStyle := theme.FgBg(titleTok, cardBgTok).Bold(true)
	subtitleStyle := theme.FgBg(theme.TextDim, cardBgTok)
	iconStyle := theme.FgBg(a.iconTok, cardBgTok).Bold(true)
	keycapStyle := theme.FgBg(theme.TextSecondary, theme.BgKeycap)
	keycapEdge := theme.Bg(theme.BgKeycap)

	innerPad := 2 // 2 cols of cardBg padding on each side
	avail := cardW - innerPad*2
	if avail < 8 {
		avail = cardW
		innerPad = 0
	}

	// Icon: 1 visual cell + 2 cells of gap before title (3 cells total).
	iconCells := 3
	keycapW := keycapWidth(a.shortcut)

	// title row: " [icon][gap][title]              [keycap] "
	titleAvail := avail - iconCells - keycapW - 1
	if titleAvail < 4 {
		// Pathological narrow card; collapse to title only.
		titleAvail = avail - iconCells
		if titleAvail < 1 {
			titleAvail = 0
		}
	}
	title := a.title
	if runewidth.StringWidth(title) > titleAvail && titleAvail > 1 {
		title = runewidth.Truncate(title, titleAvail, "…")
	}
	titleW := runewidth.StringWidth(title)

	// Build the title row. Focused cards reserve the leftmost padding
	// cell for a thin "▌" bar in AccentLavender — a quiet "you are here"
	// marker. UNfocused cards reserve the same cell as a card-bg-styled
	// space so the column aligns AND the bg doesn't leak through to the
	// editor surface behind it (the bug the user spotted: a bare `" "`
	// has no styled bg, so the terminal default bg shows through).
	focusBar := cardBg.Render(" ")
	if focused && innerPad >= 1 {
		focusBar = theme.FgBg(theme.AccentLavender, cardBgTok).Render("▌")
	}
	leftPadStr := cardBg.Render(strings.Repeat(" ", innerPad-1))
	if innerPad < 1 {
		focusBar = ""
		leftPadStr = ""
	}
	titleRowGap := avail - iconCells - titleW - keycapW
	if titleRowGap < 1 {
		titleRowGap = 1
	}
	titleRow := focusBar + leftPadStr +
		iconStyle.Render(a.icon) + cardBg.Render("  ") +
		titleStyle.Render(title) +
		cardBg.Render(strings.Repeat(" ", titleRowGap)) +
		renderKeycap(a.shortcut, keycapStyle, keycapEdge) +
		cardBg.Render(strings.Repeat(" ", innerPad))
	// Pad/trim to exact cardW.
	titleRow = padRowToWidth(titleRow, cardW, cardBg)

	// Subtitle row: indented under the title.
	var subRow string
	if showSubtitle && contentRows >= 2 {
		sub := a.subtitle
		subAvail := avail - iconCells
		if runewidth.StringWidth(sub) > subAvail && subAvail > 1 {
			sub = runewidth.Truncate(sub, subAvail, "…")
		}
		subW := runewidth.StringWidth(sub)
		fill := avail - iconCells - subW
		if fill < 0 {
			fill = 0
		}
		subRow = cardBg.Render(strings.Repeat(" ", innerPad+iconCells)) +
			subtitleStyle.Render(sub) +
			cardBg.Render(strings.Repeat(" ", fill)) +
			cardBg.Render(strings.Repeat(" ", innerPad))
		subRow = padRowToWidth(subRow, cardW, cardBg)
	}

	pad := cardBg.Render(strings.Repeat(" ", cardW))
	out := []string{pad, titleRow}
	if showSubtitle && contentRows >= 2 {
		out = append(out, subRow)
	}
	out = append(out, pad)
	return out
}

// keycapWidth is the rendered width of a shortcut chip including its
// 1-cell horizontal padding on each side. e.g. " ^P " == 4.
func keycapWidth(s string) int {
	if s == "" {
		return 0
	}
	return runewidth.StringWidth(s) + 2
}

// renderKeycap returns " <text> " styled with the keycap bg/fg. The
// outer styled spaces are the visual key-cap edges (one shade lighter
// than the surrounding bg).
func renderKeycap(s string, fg, edge lipgloss.Style) string {
	if s == "" {
		return ""
	}
	return edge.Render(" ") + fg.Render(s) + edge.Render(" ")
}

// keycap is the default keycap renderer (BgKeycap + TextSecondary).
// Used by the footer; action cards use renderKeycap to inherit the
// card background for the surrounding cells.
func keycap(s string) string {
	if s == "" {
		return ""
	}
	fg := theme.FgBg(theme.TextSecondary, theme.BgKeycap)
	edge := theme.Bg(theme.BgKeycap)
	return edge.Render(" ") + fg.Render(s) + edge.Render(" ")
}

// ─── Recent files column ───────────────────────────────────────────────

// buildRecentsColumn renders the RECENT FILES column. Header row carries
// the section title on the left and a "{shown} of {total}" counter on
// the right. Up to 3 entries follow, each a two-line block (Line A:
// EXT • filename • time; Line B: path). After the last entry, a
// "view all NN → " link line.
//
// Returns (lines, rowIdxsForHits, pathsForHits). The view-all sentinel
// uses welcomeSentinelViewAll as its path.
func buildRecentsColumn(entries []RecentEntry, w int) ([]string, []int, []string) {
	bgFill := theme.Bg(theme.BgEditor)

	var lines []string
	var idxs []int
	var paths []string

	// Header: "RECENT FILES" left, "X of Y" right.
	const maxShown = 3
	total := len(entries)
	shown := total
	if shown > maxShown {
		shown = maxShown
	}
	left := sectionHeader("RECENT FILES")
	right := ""
	if total > 0 {
		right = theme.FgBg(theme.TextQuaternary, theme.BgEditor).
			Render(fmt.Sprintf("%d of %d", shown, total))
	}
	lines = append(lines, padRowToWidth(headerRow(left, right, w), w, bgFill))
	lines = append(lines, blank(w))

	if total == 0 {
		// Empty state.
		emptyTitle := theme.FgBg(theme.TextSecondary, theme.BgEditor).Render("No files yet")
		emptyHint := theme.FgBg(theme.TextDim, theme.BgEditor).Italic(true).
			Render("Open a file or start a new project")
		lines = append(lines, padRowToWidth(emptyTitle, w, bgFill))
		lines = append(lines, padRowToWidth(emptyHint, w, bgFill))
		return lines, idxs, paths
	}

	now := time.Now()
	home, _ := os.UserHomeDir()

	for i := 0; i < shown; i++ {
		e := entries[i]
		name := filepath.Base(e.Path)
		dir := filepath.Dir(e.Path)
		if home != "" && strings.HasPrefix(dir, home) {
			dir = "~" + strings.TrimPrefix(dir, home)
		}
		when := formatTimeShort(e.OpenedAt, now)
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		extTag := extTag(ext)

		nameRow, pathRow := composeRecentEntryV2(extTag, name, dir, when, w)

		if i > 0 {
			lines = append(lines, blank(w))
		}
		// Both line A and line B carry the same hit so a click on the
		// path opens the file too.
		idxs = append(idxs, len(lines))
		paths = append(paths, e.Path)
		lines = append(lines, padRowToWidth(nameRow, w, bgFill))
		idxs = append(idxs, len(lines))
		paths = append(paths, e.Path)
		lines = append(lines, padRowToWidth(pathRow, w, bgFill))
	}

	if total > maxShown {
		lines = append(lines, blank(w))
		linkText := fmt.Sprintf("view all %d →", total)
		linkStyle := theme.FgBg(theme.AccentLavender, theme.BgEditor)
		// Indent under the filename column (past the EXT tag width = 4).
		const indent = 4
		row := bgFill.Render(strings.Repeat(" ", indent)) + linkStyle.Render(linkText)
		idxs = append(idxs, len(lines))
		paths = append(paths, welcomeSentinelViewAll)
		lines = append(lines, padRowToWidth(row, w, bgFill))
	}
	return lines, idxs, paths
}

// extTag is the rendered 2-character extension tag. Color is per-language;
// fall back to TextDim grey for unrecognised extensions. Width: padded to
// 2 cells when shorter so the filename column always lines up.
func extTag(ext string) string {
	tok := theme.TextDim
	switch ext {
	case "go":
		tok = theme.AccentLavender
	case "md", "markdown":
		tok = theme.AccentGreen
	case "py":
		tok = theme.AccentAmber
	case "rs":
		tok = theme.AccentRedCoral
	case "js", "ts", "jsx", "tsx":
		tok = theme.AccentAmber
	}
	display := ext
	if len(display) > 2 {
		display = display[:2]
	}
	if display == "" {
		display = "··"
	}
	for runewidth.StringWidth(display) < 2 {
		display = display + " "
	}
	return theme.FgBg(tok, theme.BgEditor).Render(display)
}

// composeRecentEntryV2 lays out the Recent Files entry as two rows:
//
//	[ext]  <name bold>                                        <when>
//	       <path muted>
//
// EXT tag + 2-cell gap = 4 cells of indent before name/path. when is
// right-aligned on Line A; path is left-truncated with "…" when needed.
func composeRecentEntryV2(extTag, name, dir, when string, w int) (string, string) {
	bgFill := theme.Bg(theme.BgEditor)
	nameStyle := theme.FgBg(theme.TextPrimary, theme.BgEditor).Bold(true)
	pathStyle := theme.FgBg(theme.TextDim, theme.BgEditor)
	timeStyle := theme.FgBg(theme.TextDim, theme.BgEditor)

	const extW = 2
	const extGap = 2
	indent := extW + extGap // 4

	// Line A: ext + gap + name + fill + when.
	whenW := runewidth.StringWidth(when)
	availA := w - indent - whenW - 1 // 1-cell minimum gap before when
	if availA < 4 {
		availA = w - indent - whenW
		if availA < 1 {
			availA = 1
		}
	}
	if runewidth.StringWidth(name) > availA && availA > 1 {
		name = runewidth.Truncate(name, availA, "…")
	}
	nameW := runewidth.StringWidth(name)
	gap := w - indent - nameW - whenW
	if gap < 1 {
		gap = 1
	}
	nameRow := extTag + bgFill.Render(strings.Repeat(" ", extGap)) +
		nameStyle.Render(name) +
		bgFill.Render(strings.Repeat(" ", gap)) +
		timeStyle.Render(when)

	// Line B: indent + dir (left-truncated when too long).
	pathAvail := w - indent
	curDir := dir
	if pathAvail <= 0 {
		curDir = ""
	} else if runewidth.StringWidth(curDir) > pathAvail {
		curDir = leftTruncate(curDir, pathAvail)
	}
	pathRow := bgFill.Render(strings.Repeat(" ", indent)) + pathStyle.Render(curDir)

	return nameRow, pathRow
}

// ─── Workspaces column ─────────────────────────────────────────────────

// buildWorkspacesColumn renders the WORKSPACES column. Header on top
// (no counter), then up to 3 entries, then a "browse all → " link.
func buildWorkspacesColumn(entries []WorkspaceEntry, w int) ([]string, []int, []string) {
	bgFill := theme.Bg(theme.BgEditor)

	var lines []string
	var idxs []int
	var paths []string

	left := sectionHeader("WORKSPACES")
	lines = append(lines, padRowToWidth(headerRow(left, "", w), w, bgFill))
	lines = append(lines, blank(w))

	const maxShown = 3
	total := len(entries)
	if total == 0 {
		emptyStyle := theme.FgBg(theme.TextDim, theme.BgEditor).Italic(true)
		lines = append(lines, padRowToWidth(emptyStyle.Render("No recent workspaces"), w, bgFill))
		return lines, idxs, paths
	}

	shown := total
	if shown > maxShown {
		shown = maxShown
	}
	now := time.Now()
	home, _ := os.UserHomeDir()
	wsCounts := workspaceFileCounts()

	for i := 0; i < shown; i++ {
		e := entries[i]
		name := filepath.Base(e.Path)
		if name == "" || name == "/" {
			name = e.Path
		}
		dir := e.Path
		if home != "" && strings.HasPrefix(dir, home) {
			dir = "~" + strings.TrimPrefix(dir, home)
		}
		when := formatTimeShort(e.OpenedAt, now)

		fileCount := wsCounts[e.Path]
		nameRow, pathRow := composeWorkspaceEntryV2(name, dir, when, fileCount, w, i == 0)

		if i > 0 {
			lines = append(lines, blank(w))
		}
		idxs = append(idxs, len(lines))
		paths = append(paths, e.Path)
		lines = append(lines, padRowToWidth(nameRow, w, bgFill))
		idxs = append(idxs, len(lines))
		paths = append(paths, e.Path)
		lines = append(lines, padRowToWidth(pathRow, w, bgFill))
	}

	if total > 0 {
		lines = append(lines, blank(w))
		linkStyle := theme.FgBg(theme.AccentLavender, theme.BgEditor)
		const indent = 4
		row := bgFill.Render(strings.Repeat(" ", indent)) + linkStyle.Render("browse all →")
		idxs = append(idxs, len(lines))
		paths = append(paths, welcomeSentinelBrowseAll)
		lines = append(lines, padRowToWidth(row, w, bgFill))
	}

	return lines, idxs, paths
}

// composeWorkspaceEntryV2 lays out a workspace entry as two rows. The
// PIN glyph on Line A is amber for the most-recent workspace, muted
// grey otherwise — keeps the column visually aligned but signals which
// one is "hot".
func composeWorkspaceEntryV2(name, dir, when string, fileCount int, w int, mostRecent bool) (string, string) {
	bgFill := theme.Bg(theme.BgEditor)

	pinTok := theme.TextDim
	if mostRecent {
		pinTok = theme.AccentAmber
	}
	pinStyle := theme.FgBg(pinTok, theme.BgEditor).Bold(true)
	nameStyle := theme.FgBg(theme.TextPrimary, theme.BgEditor)
	if mostRecent {
		nameStyle = nameStyle.Bold(true)
	}
	pathStyle := theme.FgBg(theme.TextDim, theme.BgEditor)
	timeStyle := theme.FgBg(theme.TextDim, theme.BgEditor)

	const pinW = 1
	const pinGap = 3 // visual indent matches recents (extW=2 + extGap=2 = 4)
	indent := pinW + pinGap

	whenW := runewidth.StringWidth(when)
	availA := w - indent - whenW - 1
	if availA < 4 {
		availA = w - indent - whenW
		if availA < 1 {
			availA = 1
		}
	}
	if runewidth.StringWidth(name) > availA && availA > 1 {
		name = runewidth.Truncate(name, availA, "…")
	}
	nameW := runewidth.StringWidth(name)
	gap := w - indent - nameW - whenW
	if gap < 1 {
		gap = 1
	}
	nameRow := pinStyle.Render(">") + bgFill.Render(strings.Repeat(" ", pinGap)) +
		nameStyle.Render(name) +
		bgFill.Render(strings.Repeat(" ", gap)) +
		timeStyle.Render(when)

	// Line B: dir + " · NN files" (or just dir when no count).
	pathAvail := w - indent
	tail := ""
	if fileCount > 0 {
		tail = " · " + fmt.Sprintf("%d files", fileCount)
	}
	tailW := runewidth.StringWidth(tail)
	curDir := dir
	if pathAvail <= 0 {
		curDir = ""
	} else if runewidth.StringWidth(curDir)+tailW > pathAvail {
		// Trim path from the left first, keep tail fully visible.
		dirW := pathAvail - tailW
		if dirW < 1 {
			dirW = pathAvail
			tail = ""
		}
		curDir = leftTruncate(curDir, dirW)
	}
	pathRow := bgFill.Render(strings.Repeat(" ", indent)) +
		pathStyle.Render(curDir+tail)

	return nameRow, pathRow
}

// workspaceFileCounts looks up cached file counts per workspace. We do
// NOT walk the filesystem at render time — that would block startup.
// Instead, we read an optional sidecar file written by a future indexer.
// For now this is a no-op stub: the function returns an empty map and
// the workspace entries render without a file count, which the layout
// gracefully omits.
func workspaceFileCounts() map[string]int {
	return nil
}

// ─── Helpers ───────────────────────────────────────────────────────────

// formatTimeShort renders a compact relative-time string per the spec:
//
//	< 1 minute  → "just now"
//	< 1 hour    → "5m"
//	< 24 hours  → "3h"
//	< 7 days    → "2d"
//	else        → "Mar 4"
func formatTimeShort(t time.Time, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		return "just now"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}

// headerRow builds a row with `left` justified to the column's left edge
// and `right` justified to the column's right edge. Padding between is
// styled BgEditor so the splice contract holds.
func headerRow(left, right string, w int) string {
	bg := theme.Bg(theme.BgEditor)
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := w - lw - rw
	if gap < 1 {
		gap = 1
	}
	return left + bg.Render(strings.Repeat(" ", gap)) + right
}

// leftTruncate keeps the right side of `s` and prefixes "…" so the last
// directory component stays readable, e.g. "…/internal/recents".
func leftTruncate(s string, w int) string {
	if w <= 1 {
		return "…"
	}
	cur := runewidth.StringWidth(s)
	if cur <= w {
		return s
	}
	target := w - 1
	for runewidth.StringWidth(s) > target && len(s) > 0 {
		_, sz := decodeFirstRune(s)
		s = s[sz:]
	}
	return "…" + s
}

// decodeFirstRune returns the first rune and its UTF-8 byte size. Cheap
// alternative to importing unicode/utf8 just for one call.
func decodeFirstRune(s string) (rune, int) {
	for i, r := range s {
		_ = i
		return r, len(string(r))
	}
	return 0, 0
}

// spaceLetters inserts a single space between consecutive ASCII letters
// to mimic CSS letter-spacing on the section title.
func spaceLetters(s string) string {
	s = strings.ToUpper(s)
	var sb strings.Builder
	for i, r := range s {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// centerInWidth pads `s` so its visible width equals `w`, centered.
func centerInWidth(s string, w int) string {
	cur := lipgloss.Width(s)
	if cur >= w {
		return s
	}
	pad := w - cur
	left := pad / 2
	right := pad - left
	bg := theme.Bg(theme.BgEditor)
	return bg.Render(strings.Repeat(" ", left)) + s + bg.Render(strings.Repeat(" ", right))
}

// padRowToWidth right-pads a styled row to width w using `bg`-styled
// cells. Caller passes the bg they want for the trailing fill (usually
// the editor bg, but the action card uses its own card bg for inner
// rows).
func padRowToWidth(s string, w int, bg lipgloss.Style) string {
	cur := lipgloss.Width(s)
	if cur >= w {
		return s
	}
	return s + bg.Render(strings.Repeat(" ", w-cur))
}

// blank returns a single editor-bg row of width w.
func blank(w int) string {
	return theme.Bg(theme.BgEditor).Render(strings.Repeat(" ", w))
}

// max is a tiny helper to avoid importing cmp.Ordered for a single call.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// padLeftS / padRightS are kept for legacy/external callers that
// reference them; pre-date the new layout.
func padLeftS(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}

func padRightS(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}
