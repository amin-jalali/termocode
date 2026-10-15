package app

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
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
	Index    int // card index into welcomeQuickActions()
}

// ─── Public API (kept stable) ──────────────────────────────────────────

// renderWelcome returns the centered welcome screen for the editor pane.
// `focusIdx` selects which action card is highlighted (0..3).
func renderWelcome(w, h, focusIdx int) string {
	key := newWelcomeKey(w, h, focusIdx)
	if c := welcomeCache; c != nil && c.key == key && time.Since(c.at) < welcomeCacheTTL {
		welcomeLogoAt = c.logo
		return c.out
	}
	rendered, hits := renderWelcomeHits(w, h, focusIdx)
	welcomeCache = &welcomeCached{key: key, out: rendered, logo: welcomeLogoAt, at: time.Now(), hits: hits}
	return rendered
}

// The welcome page reads the recents files from disk, so View reuses the
// last render for a short while (frames come at up to 60/s, e.g. while a
// splitter is dragged). A new size, focus, theme or icon set re-renders.
const welcomeCacheTTL = time.Second

type welcomeKey struct {
	w, h, focus int
	theme       string
	cfg         string
	icons       theme.IconMode
	profile     int
	logo        string
	setup       string // the setup card's signature ("" = not shown)
}

func newWelcomeKey(w, h, focus int) welcomeKey {
	return welcomeKey{w: w, h: h, focus: focus, theme: theme.Hex(theme.BgEditor) + theme.Hex(theme.BorderFocus),
		cfg: os.Getenv("XDG_CONFIG_HOME"), icons: theme.CurrentIconMode, profile: int(theme.Profile()), logo: dashboardLogo,
		setup: welcomeSetup.sig}
}

// welcomeHits are the click areas of one welcome render. They are
// recorded by the same render that made the cached page, so the mouse
// code hit-tests exactly what is on screen.
type welcomeHits struct {
	recents    []welcomeRecentHit
	workspaces []welcomeWorkspaceHit
	actions    []welcomeQuickActionHit
	setup      []welcomeSetupHit
}

type welcomeCached struct {
	key  welcomeKey
	out  string
	logo welcomeLogoRect
	at   time.Time
	hits welcomeHits
}

var welcomeCache *welcomeCached

// invalidateWelcome drops the cached dashboard render. Called when the
// recent files or workspaces change: the cache key does not cover them, so
// the dashboard kept showing the old lists ("No files yet" right after
// files were opened and closed).
func invalidateWelcome() { welcomeCache = nil }

// welcomeLayoutHits returns the click areas of the welcome page at w×h.
// The card focus only changes colors, not the layout, so the hits of the
// last render are reused whatever card it highlighted. Without a fresh
// render (new size, theme, lists, or the cache is too old) it renders the
// page once — the hover path used to re-render on every mouse move.
func welcomeLayoutHits(w, h int) welcomeHits {
	if c := welcomeCache; c != nil && time.Since(c.at) < welcomeCacheTTL {
		key := newWelcomeKey(w, h, c.key.focus)
		if c.key == key {
			return c.hits
		}
	}
	renderWelcome(w, h, 0)
	if c := welcomeCache; c != nil {
		return c.hits
	}
	return welcomeHits{} // w or h <= 0: nothing drawn, nothing to hit
}

// renderWelcomeLayout returns the recent + workspace hits expected by
// mouse.go. Quick-action hits are surfaced via welcomeQuickActionHits.
func renderWelcomeLayout(w, h, focusIdx int) (string, []welcomeRecentHit, []welcomeWorkspaceHit) {
	rendered, hits := renderWelcomeHits(w, h, focusIdx)
	return rendered, hits.recents, hits.workspaces
}

// welcomeRecentHits returns the recents hit map of the welcome page.
func welcomeRecentHits(w, h int) []welcomeRecentHit {
	return welcomeLayoutHits(w, h).recents
}

// welcomeWorkspaceHits returns clickable workspace rectangles on the
// welcome screen with their absolute editor-pane coordinates.
func welcomeWorkspaceHits(w, h int) []welcomeWorkspaceHit {
	return welcomeLayoutHits(w, h).workspaces
}

// welcomeSetupHits returns the setup card's button rectangles.
func welcomeSetupHits(w, h int) []welcomeSetupHit {
	return welcomeLayoutHits(w, h).setup
}

// welcomeQuickActionHits returns clickable rectangles for the action
// cards. Each entry carries a keymap.Action that the mouse handler
// dispatches through the same path the keyboard uses.
func welcomeQuickActionHits(w, h int) []welcomeQuickActionHit {
	return welcomeLayoutHits(w, h).actions
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
	// id names a card that has no keymap action (action == ActionNone);
	// runWelcomeCard dispatches it directly. "clone" = Clone repository.
	id string
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
		{
			icon:     "~",
			title:    "Open workspace",
			subtitle: "recent folders",
			iconTok:  theme.AccentLavender,
			id:       welcomeCardWorkspace,
		},
		{
			icon:     "↓",
			title:    "Clone repository",
			subtitle: "git clone, then open it",
			iconTok:  theme.AccentRedCoral,
			id:       welcomeCardClone,
		},
	}
}

// ─── Render entry point ────────────────────────────────────────────────

// renderWelcomeFull produces the rendered string and all three hit-zone
// slices. Splitting the work this way keeps the public layout helpers
// thin without recomputing layout for each accessor.
func renderWelcomeFull(w, h, focusIdx int) (string, []welcomeRecentHit, []welcomeWorkspaceHit, []welcomeQuickActionHit) {
	out, hits := renderWelcomeHits(w, h, focusIdx)
	return out, hits.recents, hits.workspaces, hits.actions
}

// renderWelcomeHits renders the page (with the setup card when
// welcomeSetup shows one) and returns every click area.
func renderWelcomeHits(w, h, focusIdx int) (string, welcomeHits) {
	hero := dashboardLogo != "off"
	out, hits := renderWelcomePage(w, h, focusIdx, hero, false)
	// A short pane drops the logo first, then the extra blank rows, so
	// the cards, the recent files and the footer still fit.
	tall := func() bool { return strings.Count(out, "\n")+1 > h }
	if hero && tall() {
		out, hits = renderWelcomePage(w, h, focusIdx, false, false)
	}
	if tall() {
		out, hits = renderWelcomePage(w, h, focusIdx, false, true)
	}
	return out, hits
}

// renderWelcomePage renders the page, with or without the hero (logo,
// tagline and rule); compact puts one blank row between sections, not
// three, and drops the card subtitles.
func renderWelcomePage(w, h, focusIdx int, hero, compact bool) (string, welcomeHits) {
	if w <= 0 || h <= 0 {
		return "", welcomeHits{}
	}

	actions := welcomeQuickActions()
	card := welcomeSetup
	// The focus runs over the action cards, then the setup card buttons.
	total := len(actions) + len(card.buttons())
	if focusIdx < 0 {
		focusIdx = 0
	}
	if total > 0 && focusIdx >= total {
		focusIdx = total - 1
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
	// Compact (a short pane): one-line cards.
	showSubtitle := innerW >= welcomeNarrowMin && !compact

	var contentLines []string
	gap := []string{blank(innerW), blank(innerW), blank(innerW)}
	if compact {
		gap = gap[:1]
	}

	// ── 1. Logo + tagline ─────────────────────────────────────────────
	var logo []string
	pixelLogo := false
	if hero {
		logoStyle := theme.FgBg(theme.AccentBlue, theme.BgEditor).Bold(true)
		tagStyle := theme.FgBg(theme.TextMuted, theme.BgEditor)
		logo = welcomeLogo(innerW)
		pixelLogo = logo != nil
		if logo == nil {
			logo = welcomeBanner(logoStyle, innerW)
		}
		for _, b := range logo {
			contentLines = append(contentLines, centerInWidth(b, innerW))
		}
		contentLines = append(contentLines, blank(innerW))
		tagline := "A TERMINAL-NATIVE EDITOR"
		if wide {
			tagline = spaceLetters(tagline)
		}
		contentLines = append(contentLines, centerInWidth(tagStyle.Render(tagline), innerW))
		// A short lit rule under the hero.
		if rw := textWidth(logo[0]); rw > 8 {
			contentLines = append(contentLines, centerInWidth(heroRule(rw/2), innerW))
		} else {
			contentLines = append(contentLines, blank(innerW))
		}
		contentLines = append(contentLines, blank(innerW), blank(innerW))
	}

	// ── 2. START — 2×2 (or 1×4 on narrow) grid of action cards ────────
	contentLines = append(contentLines, padRowToWidth(sectionHeader("START"), innerW, bgFill))
	contentLines = append(contentLines, blank(innerW))
	qaLines, qaHits := buildActionGrid(actions, innerW, len(contentLines), focusIdx, wide, showSubtitle)
	contentLines = append(contentLines, qaLines...)

	contentLines = append(contentLines, gap...)

	// ── 2b. First-run setup card (setup_card_render.go) ───────────────
	var setupHits []welcomeSetupHit
	if card.show {
		lines, hits := buildSetupCard(card, innerW, len(contentLines), focusIdx-len(actions), compact)
		contentLines = append(contentLines, lines...)
		contentLines = append(contentLines, gap...)
		setupHits = hits
	}

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
	contentLines = append(contentLines, gap...)
	rule := footerRule(innerW)
	contentLines = append(contentLines, rule)
	contentLines = append(contentLines, blank(innerW))
	contentLines = append(contentLines, footerRow(innerW))

	// ── Pad each row to exactly innerW ────────────────────────────────
	for i, line := range contentLines {
		cur := textWidth(line)
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
	// Remember where the pixel wordmark landed, so the frame can draw the
	// image version on top of it (image_view.go / welcomeLogoImage).
	welcomeLogoAt = welcomeLogoRect{}
	if pixelLogo && len(logo) > 0 {
		lw := textWidth(logo[0])
		welcomeLogoAt = welcomeLogoRect{ok: true, x: leftPad + (innerW-lw)/2, y: topPad, cols: lw, rows: len(logo)}
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
	sHits := make([]welcomeSetupHit, 0, len(setupHits))
	for _, h := range setupHits {
		h.Row += topPad
		h.ColStart += leftPad
		h.ColEnd += leftPad
		sHits = append(sHits, h)
	}

	return sb.String(), welcomeHits{recents: rHits, workspaces: wHits, actions: qHits, setup: sHits}
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

	leftW := textWidth(left)
	rightW := textWidth(right)
	// Docs: append the docs hint to the version line when it fits.
	if docs := versionStyle.Render("  ·  Docs: " + docsHost); leftW+textWidth(docs)+rightW+1 <= w {
		left += docs
		leftW = textWidth(left)
	}
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
	// Cards are separate rounded tiles now; a blank column parts them.
	vDivider := bgFill.Render(" ")
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
			cur := textWidth(sb.String())
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
					Index:    startIdx + i,
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
	// A rounded tile: dim border at rest; on focus / hover the border
	// takes the accent and the inside gets a soft accent wash.
	cardBgC := theme.LG(theme.BgEditor)
	borderC := theme.Mix(theme.BgEditor, theme.BorderDefault, 0.85)
	if focused {
		cardBgC = theme.Wash(theme.BgEditor, theme.WashSelected)
		borderC = theme.LG(theme.BorderFocus)
	}
	cardBg := lipgloss.NewStyle().Background(cardBgC)
	edge := lipgloss.NewStyle().Background(theme.LG(theme.BgEditor)).Foreground(borderC)
	side := lipgloss.NewStyle().Background(cardBgC).Foreground(borderC)
	// The focused tile's left edge becomes a solid accent strip — the
	// focus stays visible even without colour.
	leftEdge := side.Render("│")
	if focused {
		leftEdge = side.Render("▌")
	}

	titleFg := theme.LG(theme.TextPrimary)
	if focused {
		titleFg = theme.LG(theme.TextWhite)
	}
	titleStyle := lipgloss.NewStyle().Background(cardBgC).Foreground(titleFg).Bold(true)
	subtitleStyle := lipgloss.NewStyle().Background(cardBgC).Foreground(theme.LG(theme.TextDim))
	iconStyle := lipgloss.NewStyle().Background(cardBgC).Foreground(theme.LG(a.iconTok)).Bold(true)
	keycapStyle := lipgloss.NewStyle().Background(theme.Mix(theme.BgEditor, theme.TextPrimary, 0.10)).
		Foreground(theme.LG(theme.TextSecondary))
	keycapEdge := lipgloss.NewStyle().Background(theme.Mix(theme.BgEditor, theme.TextPrimary, 0.10))

	if cardW < 6 {
		pad := cardBg.Render(strings.Repeat(" ", cardW))
		out := []string{pad, pad}
		if showSubtitle && contentRows >= 2 {
			out = append(out, pad)
		}
		return append(out, pad)
	}
	inner := cardW - 2
	innerPad := 2
	avail := inner - innerPad*2
	if avail < 8 {
		avail, innerPad = inner, 0
	}
	icon := welcomeCardIcon(a)
	iconCells := runewidth.StringWidth(icon) + 2
	keycapW := keycapWidth(a.shortcut)

	titleAvail := avail - iconCells - keycapW - 1
	if titleAvail < 4 {
		titleAvail = avail - iconCells
		if titleAvail < 1 {
			titleAvail = 0
		}
	}
	title := a.title
	if runewidth.StringWidth(title) > titleAvail && titleAvail > 1 {
		title = runewidth.Truncate(title, titleAvail, "…")
	}
	gap := avail - iconCells - runewidth.StringWidth(title) - keycapW
	if gap < 1 {
		gap = 1
	}
	titleRow := cardBg.Render(strings.Repeat(" ", innerPad)) +
		iconStyle.Render(icon) + cardBg.Render("  ") +
		titleStyle.Render(title) +
		cardBg.Render(strings.Repeat(" ", gap)) +
		renderKeycap(a.shortcut, keycapStyle, keycapEdge)
	titleRow = padRowToWidth(titleRow, inner, cardBg)

	out := []string{edge.Render("╭" + strings.Repeat("─", inner) + "╮")}
	out = append(out, leftEdge+titleRow+side.Render("│"))
	if showSubtitle && contentRows >= 2 {
		sub := a.subtitle
		subAvail := avail - iconCells
		if runewidth.StringWidth(sub) > subAvail && subAvail > 1 {
			sub = runewidth.Truncate(sub, subAvail, "…")
		}
		subRow := cardBg.Render(strings.Repeat(" ", innerPad+iconCells)) + subtitleStyle.Render(sub)
		out = append(out, leftEdge+padRowToWidth(subRow, inner, cardBg)+side.Render("│"))
	}
	out = append(out, edge.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return out
}

// welcomeCardIcon picks the card glyph for the icon mode.
func welcomeCardIcon(a quickAction) string {
	if theme.CurrentIconMode != theme.IconModeNerdFont {
		return a.icon
	}
	switch {
	case a.id == welcomeCardClone:
		return "\uf09b" // nf-fa-github
	case a.id == welcomeCardWorkspace:
		return "\uf07c" // nf-fa-folder_open
	case a.action == keymap.ActionQuickOpen:
		return "\uf15b" // nf-fa-file
	case a.action == keymap.ActionCommandPalette:
		return "\uf120" // nf-fa-terminal
	case a.action == keymap.ActionWorkspaceSearch:
		return "\uea6d" // nf-cod-search
	case a.action == keymap.ActionToggleTerminal:
		return "\uf489" // nf-oct-terminal
	}
	return a.icon
}

// ─── Hero logo ─────────────────────────────────────────────────────────

// logoGlyphs is a bold 5-pixel-high pixel font (2-pixel vertical stems)
// for the letters of "TERMOCODE". Drawn with half blocks, one pixel is
// half a cell tall and one cell wide, i.e. square.
var logoGlyphs = map[rune][5]string{
	'T': {"######", "  ##  ", "  ##  ", "  ##  ", "  ##  "},
	'E': {"######", "##    ", "##### ", "##    ", "######"},
	'R': {"##### ", "##  ##", "##### ", "## ## ", "##  ##"},
	'M': {"##   ##", "### ###", "## # ##", "##   ##", "##   ##"},
	'O': {" #### ", "##  ##", "##  ##", "##  ##", " #### "},
	'C': {" #####", "##    ", "##    ", "##    ", " #####"},
	'D': {"##### ", "##  ##", "##  ##", "##  ##", "##### "},
}

// logoPixels lays out text in the pixel font: 6 pixel rows (the last is
// blank so the rows pair up into 3 cell rows).
func logoPixels(text string, gap int) []string {
	rows := make([]string, 6)
	for i, r := range text {
		g, ok := logoGlyphs[r]
		if !ok {
			continue
		}
		for y := 0; y < 5; y++ {
			if i > 0 {
				rows[y] += strings.Repeat(" ", gap)
			}
			rows[y] += g[y]
		}
	}
	rows[5] = strings.Repeat(" ", len(rows[0]))
	return rows
}

// welcomeLogo renders the hero wordmark with a horizontal accent
// gradient (flat accent on terminals without truecolor). Returns nil when
// it does not fit or the terminal is in ASCII icon mode (the plain ASCII
// banner is used instead).
func welcomeLogo(innerW int) []string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return nil
	}
	var px []string
	for _, gap := range []int{2, 1} {
		px = logoPixels("TERMOCODE", gap)
		if len(px[0]) <= innerW {
			break
		}
	}
	if len(px[0]) > innerW {
		return nil
	}
	w := len(px[0])
	stops := []theme.RGB{theme.ToRGB(theme.BorderFocus), theme.ToRGB(theme.AccentLavender), theme.ToRGB(theme.AccentMagenta)}
	cols := theme.Gradient(w, stops...)
	flat := !theme.TrueColor()
	bg := theme.LG(theme.BgEditor)
	out := make([]string, 0, 3)
	for r := 0; r < 3; r++ {
		var b strings.Builder
		top, bot := px[2*r], px[2*r+1]
		for x := 0; x < w; x++ {
			t, u := top[x] == '#', bot[x] == '#'
			g := " "
			switch {
			case t && u:
				g = "█"
			case t:
				g = "▀"
			case u:
				g = "▄"
			}
			c := cols[x]
			if flat {
				c = stops[1]
			}
			b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(c.LG()).Render(g))
		}
		out = append(out, b.String())
	}
	return out
}

// heroRule is a short gradient hairline that fades out at both ends.
func heroRule(w int) string {
	bg := theme.ToRGB(theme.BgEditor)
	mid := theme.ToRGB(theme.AccentLavender)
	return theme.GradientBar(w, "─", theme.BgEditor, bg, theme.ToRGB(theme.BorderFocus), mid, bg)
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

	now := clock.Now()
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
	now := clock.Now()
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
	lw := textWidth(left)
	rw := textWidth(right)
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
	cur := textWidth(s)
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
	cur := textWidth(s)
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

// welcomeLogoRect is where the pixel wordmark was drawn in the last
// welcome render (editor-pane relative). View runs on one goroutine.
type welcomeLogoRect struct {
	ok               bool
	x, y, cols, rows int
}

var welcomeLogoAt welcomeLogoRect

// welcomeLogoImage paints the wordmark as an image: one logo pixel is a
// cellW × cellH/2 block, coloured along the same gradient as the text
// version but per image column (smoother), on the editor background.
func welcomeLogoImage(cols, rows, cellW, cellH int) image.Image {
	px := logoPixels("TERMOCODE", 2)
	if len(px[0]) != cols {
		px = logoPixels("TERMOCODE", 1)
	}
	w, h := cols*cellW, rows*cellH
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := theme.ToRGB(theme.BgEditor)
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{bg.R, bg.G, bg.B, 255}}, image.Point{}, draw.Src)
	stops := []theme.RGB{theme.ToRGB(theme.BorderFocus), theme.ToRGB(theme.AccentLavender), theme.ToRGB(theme.AccentMagenta)}
	grad := theme.Gradient(w, stops...)
	ph := cellH / 2
	inset := cellW / 8
	for y, row := range px {
		for x := 0; x < len(row) && x < cols; x++ {
			if row[x] != '#' {
				continue
			}
			x0, y0 := x*cellW, y*ph
			for yy := y0; yy < y0+ph && yy < h; yy++ {
				for xx := x0; xx < x0+cellW && xx < w; xx++ {
					c := grad[xx]
					// A hairline gap between pixels gives the mark a crisp,
					// tiled look; neighbours in the same stroke stay joined.
					if inset > 0 && (xx-x0 < inset && (x == 0 || row[x-1] != '#')) {
						continue
					}
					img.Set(xx, yy, color.RGBA{c.R, c.G, c.B, 255})
				}
			}
		}
	}
	return img
}
