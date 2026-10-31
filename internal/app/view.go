package app

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/statusbar"
	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// View renders the frame, then lays the terminal-graphics layer (image
// preview, dashboard logo) on top of it — see image_view.go.
func (m Model) View() string {
	return m.gatedView(m.drawView)
}

// drawView draws the whole screen (View goes through the frame gate).
func (m Model) drawView() string {
	frame := m.viewFrame()
	if m.w == 0 || m.h == 0 {
		return frame
	}
	defer firstFrameDrawn()
	frame = fitTinyScreen(frame, m.w)
	previewShown := m.imageOpen && m.imagePrev != nil && m.imagePrev.img != nil
	logoShown := false
	if !previewShown && m.overlayKind() == "" && !m.previewOpen && !m.searchOpen && !m.commitViewShown() && m.welcomeVisible() {
		frame, logoShown = m.placeLogo(frame)
	}
	return m.markAttach(m.cleanupGraphics(frame, previewShown, logoShown))
}

// tinyScreenWidth is the narrowest screen the columns lay out on: the
// activity bar, the sidebar at its minimum width and the smallest editor.
const tinyScreenWidth = activity.Width + explorerMinWidth + minEditorPaneWidth

// fitTinyScreen cuts every row to w on a screen narrower than the
// columns need. Wider screens lay out to fit and skip the work.
func fitTinyScreen(frame string, w int) string {
	if w >= tinyScreenWidth {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > w {
			lines[i] = ansi.Truncate(l, w, "")
		}
	}
	return strings.Join(lines, "\n")
}

// welcomeVisible mirrors renderBase's welcome-screen condition.
func (m Model) welcomeVisible() bool {
	return m.editorEmpty() && !m.zenMode
}

// placeLogo draws the image wordmark over the half-block one. Only with
// dashboard_logo=image: images are the fragile part (sixel placement
// differs between terminals), so the text logo is the default.
func (m Model) placeLogo(frame string) (string, bool) {
	r := welcomeLogoAt
	if dashboardLogo != "image" || !r.ok || m.toast.Animating() || !m.toast.Empty() {
		return frame, false
	}
	cw, ch := termCaps.CellSize()
	key := termimg.CacheKey{Path: "\x00logo:" + m.themeID, Cols: r.cols, Rows: r.rows}
	seq := graphicsSeq(key, func() image.Image { return welcomeLogoImage(r.cols, r.rows, cw, ch) }, r.cols, r.rows, kittyIDLogo)
	if seq == "" {
		return frame, false
	}
	x0 := activity.Width
	if m.showExp {
		x0 += m.explorerWidth
	}
	y0 := m.tabs.Height()
	if m.replaceOpen {
		y0 += 2
	}
	if len(m.breadcrumbs) > 0 || m.stickyContext != "" {
		y0++
	}
	return placeGraphics(frame, seq, y0+r.y, x0+r.x, r.rows), true
}

func (m Model) viewFrame() string {
	if m.w == 0 || m.h == 0 {
		return "termocode loading..."
	}
	if m.imageOpen && m.imagePrev != nil {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		return m.imageOverlay(base)
	}

	// All pickers and prompts share the same modalOverlay recipe so
	// pseudo-transparency reads consistently across the UI. Other
	// overlays (preview / search / confirm) are full-screen and keep
	// their own rendering. The preview comes first: it takes the keys
	// first too (the guide opens over Settings or the palette, Shift+F1).
	if m.previewOpen {
		return m.preview.View()
	}
	if m.settingsModalOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		return modalOverlay(base, m.settingsModal.Box(), m.w, m.h, m.modalOpts())
	}
	if m.pickerOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		o := m.overlayOpts()
		bw, bh := m.picker.BoxSize()
		o.At = true
		o.X, o.Y = m.picker.Origin(bw, bh)
		o.Slide = 2
		return modalOverlay(base, m.picker.Box(), m.w, m.h, &o)
	}
	if m.recentsOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		return modalOverlay(base, m.recents.Box(), m.w, m.h, m.modalOpts())
	}
	if m.searchOpen {
		return m.search.View()
	}
	if m.promptOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		return modalOverlay(base, m.prompt.Box(), m.w, m.h, m.modalOpts())
	}
	if m.confirmOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		base = m.reviewLayer(base)      // a confirm can sit on the review card
		base = m.importLayer(base)      // or on the import review panel
		base = m.profileFormLayer(base) // or on the profile form
		return modalOverlay(base, m.confirm.Box(), m.w, m.h, m.modalOpts())
	}
	if m.connectOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		o := m.overlayOpts()
		o.At = true
		o.X, o.Y = m.connect.Origin()
		return modalOverlay(base, m.connect.Box(), m.w, m.h, &o)
	}
	if m.addAIOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		base = m.profileFormLayer(base) // the card can sit on the profile form
		o := m.overlayOpts()
		o.At = true
		o.X, o.Y = m.addAI.Origin()
		return modalOverlay(base, m.addAI.Box(), m.w, m.h, &o)
	}
	if m.profileFormOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		o := m.overlayOpts()
		o.At = true
		o.X, o.Y = m.profileForm.Origin()
		return modalOverlay(base, m.profileForm.Box(), m.w, m.h, &o)
	}
	if m.importOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		// Toasts ("Nothing selected") show on top of the panel.
		return m.overlayToastsInEditor(m.importLayer(base))
	}
	if m.reviewOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		if m.ai != nil && m.ai.review != nil && m.ai.review.onAction != nil {
			// A card with its own buttons (schema compare): their toasts
			// ("Migration script copied") show on top of it.
			return m.overlayToastsInEditor(m.reviewLayer(base))
		}
		return m.reviewLayer(base)
	}

	base := m.renderBase()
	base = m.overlayToastsInEditor(base)
	if m.menuOpen {
		return m.glassyMenuOverlay(base)
	}
	// Overflow (⋮) menu: dropdown overlays the editor body when open.
	// The glyph itself is painted inside renderBase so it stays present
	// even when the dropdown is closed.
	if m.overflowMenuOpen {
		base = m.overlayOverflowMenuDropdown(base)
	}
	base = m.overlayCommitHoverCard(base)
	base = m.overlayTooltip(base)
	return m.overlayKeyHints(base)
}

// glassyMenuOverlay floats the right-click menu over base with a drop
// shadow (no backdrop dim: a menu is a light-weight popup).
func (m Model) glassyMenuOverlay(base string) string {
	mx, my, mw, mh := m.menu.Bounds()
	if mw <= 0 || mh <= 0 {
		return m.menu.Overlay(base)
	}
	o := m.overlayOpts()
	o.At, o.X, o.Y, o.NoDim, o.Slide = true, mx, my, true, 1
	return modalOverlay(base, m.menu.View(), m.w, m.h, &o)
}

// overlayToastsInEditor stacks the toast cards in the bottom-right corner
// of the editor pane, newest at the bottom. Each card slides in from the
// right (and out again when it expires) while animations are on.
func (m Model) overlayToastsInEditor(base string) string {
	// Hot path: nothing to do (and no frame split) without toasts.
	if m.toast.Empty() {
		return base
	}
	for _, p := range m.toastPlaces() {
		o := ModalOpts{At: true, X: p.x, Y: p.y, NoDim: true, SlideX: p.w + 4,
			Animating: p.card.Slide != 0, Progress: 1 - p.card.Slide}
		if p.compact {
			o.NoShadow, o.SlideX = true, p.w+2
		}
		base = modalOverlay(base, strings.Join(p.card.Lines, "\n"), m.w, m.h, &o)
	}
	return base
}

// toastPlaces lays out the toast cards (see overlayToastsInEditor); the
// mouse hit-test (toastClickAt) uses the same places.
func (m Model) toastPlaces() []toastPlace {
	if m.toast.Empty() {
		return nil
	}
	pane := m.editorPaneLayout()
	edPaneW, editorStart := pane.W, pane.X
	// Errors need a separate reason row, even beside a narrow chat pane.
	errorDetail := false
	for _, t := range m.toast.Live() {
		if t.Severity == toast.Errr && t.Detail != "" {
			errorDetail = true
			break
		}
	}
	if errorDetail {
		editorStart, edPaneW = 0, m.w
	}
	if !errorDetail && (m.h <= toastCompactMaxH || edPaneW < toastCompactMinW) {
		return m.compactToastPlaces(editorStart, edPaneW)
	}
	if edPaneW-6 < 24 {
		return nil
	}
	// As wide as the text needs (28..52 cells), not always 52.
	limit := 52
	if errorDetail {
		limit = 80
	}
	w := m.toast.FitWidth(min(limit, edPaneW-6))
	cards := m.toast.Cards(w)
	x := editorStart + edPaneW - w - 2
	y := m.h - 3 // the shadow row of the newest card sits just above the status bar
	var out []toastPlace
	for i := len(cards) - 1; i >= 0; i-- {
		c := cards[i]
		y -= len(c.Lines)
		if y < 1 {
			break
		}
		out = append(out, toastPlace{card: c, x: x, y: y, w: w})
		y-- // gap row (holds the shadow of the card above)
	}
	return out
}

// A window this short, or an editor pane this narrow, gets the compact
// toast (see compactToastPlaces).
const (
	toastCompactMaxH = 30
	toastCompactMinW = 72
)

// compactToastPlaces lays out the small-window toast, like VSCode's
// collapsed notifications: 3-row cards (title · detail on one row) about
// half the editor wide, with no drop shadow, stacked with no gap — only
// the newest in a window of 30 rows or less, the newest two otherwise.
// The stack sits in the bottom-right corner just above the status bar;
// when that would cover the cursor line it moves to the top-right of the
// editor.
func (m Model) compactToastPlaces(editorStart, edPaneW int) []toastPlace {
	w := edPaneW / 2
	if w > 40 {
		w = 40
	}
	if w < 24 {
		w = 24
	}
	// Never so narrow that the title is cut ("Tests: 1 failed, 1 passed").
	if need := m.toast.CompactTitleWidth(); need > w {
		w = min(need, edPaneW-2)
	}
	if w > edPaneW-2 {
		return nil
	}
	n := 2
	if m.h <= toastCompactMaxH {
		n = 1
	}
	cards := m.toast.CompactCards(w, n)
	if len(cards) == 0 {
		return nil
	}
	h := 0
	for _, c := range cards {
		h += len(c.Lines)
	}
	top := m.tabs.Height()
	if m.replaceOpen {
		top += 2
	}
	if len(m.breadcrumbs) > 0 || m.stickyContext != "" {
		top++
	}
	y := m.h - 1 - h // the row above the status bar is the stack's last
	if cur := top + m.editor.Cursor().Line; cur >= y && cur < y+h && top+h < y {
		y = top
	}
	x := editorStart + edPaneW - w - 1
	var out []toastPlace
	for _, c := range cards {
		if y >= 1 {
			out = append(out, toastPlace{card: c, x: x, y: y, w: w, compact: true})
		}
		y += len(c.Lines)
	}
	return out
}

// queryGutterWidth asks nvim for the actual gutter width via
// getwininfo().textoff — the authoritative number of cells reserved on
// the LEFT of the buffer for line-number + sign + fold columns.
// Returns 7 (= our `numberwidth`) when nvim can't be reached so the
// toast doesn't blow up before nvim has attached.
//
// Result is cached for `gutterCacheTTL` because View() can fire dozens
// of times per second during e.g. a mouse drag, and a synchronous Lua
// round-trip per render is the dominant cost. The actual gutter width
// changes only when signs come/go or numberwidth flips — refreshing at
// 4 Hz tracks that finely enough for visual alignment.
func (m Model) queryGutterWidth() int {
	if m.nvim == nil {
		return 7
	}
	gutterCacheMu.Lock()
	if !gutterCacheAt.IsZero() && time.Since(gutterCacheAt) < gutterCacheTTL && gutterCacheVal > 0 {
		v := gutterCacheVal
		gutterCacheMu.Unlock()
		return v
	}
	gutterCacheMu.Unlock()

	out, err := m.nvim.EvalLuaString(`
		local info = vim.fn.getwininfo(vim.api.nvim_get_current_win())
		if #info == 0 then return '7' end
		return tostring(info[1].textoff or 7)
	`)
	if err != nil {
		return 7
	}
	n := 0
	for _, c := range out {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 || n > 30 {
		return 7
	}
	gutterCacheMu.Lock()
	gutterCacheVal = n
	gutterCacheAt = time.Now()
	gutterCacheMu.Unlock()
	return n
}

const gutterCacheTTL = 250 * time.Millisecond

var (
	gutterCacheMu  sync.Mutex
	gutterCacheVal int
	gutterCacheAt  time.Time
)

// spliceAt overlays `mid` onto `base` starting at the leftCol-th visible
// cell, preserving base on both sides. textWidth(mid) cells of base
// are dropped under the overlay; SGR state for the right-side base is
// rebuilt from the most recent SGR seen before the cut so colour bleed
// doesn't carry over.
func spliceAt(base, mid string, leftCol int) string {
	rw := textWidth(mid)
	left := truncRightVisual(base, leftCol)
	right := dropLeftVisual(base, leftCol+rw)
	return left + mid + right
}

// spliceMemo is spliceAt with the last call remembered (spliceAt is a
// pure function of its arguments).
type spliceMemo struct {
	sync.Mutex
	ok        bool
	base, mid string
	col       int
	out       string
}

func (c *spliceMemo) splice(base, mid string, col int) string {
	c.Lock()
	defer c.Unlock()
	if c.ok && c.col == col && c.mid == mid && c.base == base {
		return c.out
	}
	c.ok, c.base, c.mid, c.col, c.out = true, base, mid, col, spliceAt(base, mid, col)
	return c.out
}

// dropLeftVisual returns the suffix of s that remains after dropping the
// leftmost `cells` visible cells. SGR escapes inside the dropped region
// are not emitted, but the most recent SGR seen before the cut point is
// re-emitted at the start of the suffix so the right-side cells render
// with the correct active style.
func dropLeftVisual(s string, cells int) string {
	if cells <= 0 {
		return s
	}
	want := ansi.StringWidth(s) - cells
	out := ansi.TruncateLeft(s, cells, "")
	if ansi.StringWidth(out) > want {
		// The cut lands inside a wide glyph, which TruncateLeft keeps
		// whole; drop it and keep its right half as a blank cell.
		out = " " + ansi.TruncateLeft(s, cells+1, "")
	}
	if gap := want - ansi.StringWidth(out); gap > 0 {
		out = strings.Repeat(" ", gap) + out
	}
	return out
}

// truncRightVisual keeps only the leftmost `cells` visible cells from s,
// preserving ANSI escapes that apply to the kept range. It works on whole
// grapheme clusters, so a multi-byte glyph is never split (a split rune is
// invalid UTF-8; the terminal then draws extra cells, the line wraps and
// the whole screen scrolls).
func truncRightVisual(s string, cells int) string {
	if cells <= 0 {
		return ""
	}
	out := ansi.Truncate(s, cells, "")
	if w := ansi.StringWidth(out); w < cells && w < ansi.StringWidth(s) {
		out += strings.Repeat(" ", cells-w)
	}
	return out
}

// renderBase composes the main layout as a horizontal stack of three
// full-height columns. The status bar lives ONLY under the editor column
// — the activity bar and sidebar paint their own backgrounds all the way
// to the bottom edge:
//
//	┌──────────┬──────────────┬──────────────────────────┐
//	│ activity │ explorer     │ editor                   │
//	│ (4 cols) │ (30 cols)    │ (remaining cols)         │
//	│          │              │                          │
//	│ icons    │ ┌──────────┐ │ ┌──────────────────────┐ │
//	│ +accent  │ │ HEADER   │ │ │ tab bar              │ │
//	│          │ ├──────────┤ │ ├──────────────────────┤ │
//	│          │ │ tree …   │ │ │ breadcrumbs          │ │
//	│          │ │          │ │ ├──────────────────────┤ │
//	│          │ │          │ │ │ editor content       │ │
//	│          │ │          │ │ ├──────────────────────┤ │
//	│          │ │          │ │ │ status bar           │ │
//	│          │ └──────────┘ │ └──────────────────────┘ │
//	└──────────┴──────────────┴──────────────────────────┘
//
// The activity bar's right edge meets the sidebar via a BgActivityBar→
// BgSidebar contrast (no border glyph drawn). The sidebar's right edge
// has a single ▕ in BorderDefault. Each component renders strictly
// inside its column width so there are no overlaps.
func (m Model) renderBase() string {
	return m.rebaseLayer(m.commitViewLayer(strings.Join(m.renderBaseLines(), "\n")))
}

// renderBaseLines composes the frame as rows. The pieces are joined and
// overlaid row by row and turned into one string only at the end: the
// frame is large and drawn often, and every whole-frame join / split
// was a full copy.
func (m Model) renderBaseLines() []string {
	refreshSidebarColorsIfStale()
	nextFrameWidths()
	if m.ai != nil && !m.actionsPanelVisible() {
		m.ai.dropRenderCaches() // a hidden chat panel keeps no drawn rows
	}
	bodyH := m.h - 1
	if bodyH < 1 {
		bodyH = 1
	}

	var editorRegion []string
	// Editor pane geometry (width between sidebar and AI panel; content
	// height below replace bar, tabs and breadcrumbs). The mouse hit tests
	// use the same editorPaneLayout, so clicks match what is drawn.
	// The find bar is a floating panel spliced onto the top-right of the
	// content area (see overlayFindBarInEditorLines), not a chrome row.
	pane := m.editorPaneLayout()
	edPaneW, editorH := pane.W, pane.H
	hasBreadcrumbs := len(m.breadcrumbs) > 0 || m.stickyContext != ""
	actionsColW := m.actionsColumnWidth()

	var editorContent string
	var editorLines []string // the editor's rows when not joined (no Split)
	// The welcome screen shows only on a truly empty editor. During a Source
	// Control diff the focused pane may be the nameless, unlisted HEAD scratch
	// buffer (Path()=="" and not counted in m.bufs) — without the gitDiffActive
	// guard, moving the mouse onto the HEAD pane would flip the whole editor to
	// the welcome screen and the diff would appear to vanish.
	if m.editorEmpty() {
		m.publishSetupCard()
		editorContent = renderWelcome(edPaneW, editorH, m.welcomeFocus, m.editorHasKeys())
	} else if m.runsPageShown() {
		// The Runs page: drawn here, not by nvim (runs_page.go).
		editorContent = m.renderRunsPage(edPaneW, editorH)
	} else if m.itermocPageShown() {
		// The Itermoc detail page: drawn here, not by nvim (itermoc_page.go).
		editorContent = m.renderItermocPage(edPaneW, editorH)
	} else {
		// When the integrated terminal panel is open, applyLayout shrinks
		// nvim's render area by 1 row so we can splice our own tab-bar
		// row between nvim's editor split and nvim's terminal split.
		// Splice at the boundary so the output is exactly editorH rows
		// tall.
		if m.termOpen {
			editorContent = m.spliceTerminalTabBar(m.editor.View(m.focus == FocusEditor), edPaneW, editorH)
		} else {
			editorLines = m.editor.ViewLines(m.focus == FocusEditor)
		}
	}
	if editorLines == nil {
		editorLines = strings.Split(editorContent, "\n")
	}
	// Belt-and-braces: clip editorContent to exactly editorH rows so the
	// editor column stack always settles at chromeRows + editorH = bodyH,
	// and editorCol (= editorRegion + status) lands at exactly m.h rows.
	// Welcome's renderer can return more rows than its `h` argument when
	// the page's content (banner + sections) is naturally taller than h —
	// without this clip those extra rows push the column past m.h, which
	// in turn makes lipgloss.JoinHorizontal pad the shorter activity /
	// sidebar columns with unstyled spaces (visible as a default-bg
	// stripe along the LEFT edge).
	editorRows := clipRowsLines(editorLines, editorH, edPaneW, theme.Bg(theme.BgEditor))

	// Build the editor stack, then horizontally append the scrollbar so the
	// scrollbar spans the full chrome height (tabs/breadcrumbs/sticky/editor).
	var blocks [][]string
	if m.replaceOpen {
		blocks = append(blocks, strings.Split(m.replace.View(), "\n"))
	}
	// In Zen mode the user gets the editor and nothing else — no tabs,
	// breadcrumbs, status bar, sidebar, or activity bar.
	if !m.zenMode {
		// applyLayout sized the tab bar to `edPaneW - overflowMenuReservedCells`
		// so the rightmost cells are free for the ⋮ glyph. Pad the strip back
		// out to edPaneW with title-bar bg here so the row's overall width
		// matches the editor pane and overlayOverflowMenuGlyph has clean cells
		// to overpaint. Zero-pad path stays a no-op when actions-panel /
		// narrow-pane math collapses the reservation.
		tabs := m.tabs
		tabs.SetFocused(m.editorHasKeys())
		tabsRow := tabs.View()
		if pad := edPaneW - textWidth(tabsRow); pad > 0 {
			tabsRow += bgSpaces(theme.BgTitleBar, pad)
		}
		blocks = append(blocks, strings.Split(tabsRow, "\n"))
		if hasBreadcrumbs {
			blocks = append(blocks, strings.Split(m.renderBreadcrumbs(edPaneW), "\n"))
		}
		if pane.Merge > 0 { // git_merge_view.go
			blocks = append(blocks, m.renderMergeRows(edPaneW, pane.Merge))
		}
	}
	blocks = append(blocks, editorRows)
	stack := joinRowsLines(blocks...)

	// Scrollbar is disabled (editorScrollbarWidth = 0) for scroll perf.
	// JoinHorizontal across the editor stack is the dominant cost during
	// fast scroll bursts — re-enable when we have a cached/incremental
	// scrollbar implementation.
	editorRegion = stack
	if editorScrollbarWidth > 0 {
		scrollbar := m.renderEditorScrollbar(bodyH)
		editorRegion = joinColumnsLines(stack, strings.Split(scrollbar, "\n"))
	}

	// Markdown live preview now lives as a regular nvim split window with
	// its own tab — no extra rendering needed here.

	if m.zenMode {
		// Zen: nothing but the editor stack stretched to full width. We
		// already excluded tabs/breadcrumbs from `stack` above, so this
		// is just the editor content with no chrome at all.
		return m.normalizeFrameLines(editorRegion)
	}

	// The status bar now lives ONLY under the editor column — the
	// activity bar + sidebar keep their own backgrounds running all the
	// way to the bottom edge. To make that work the status bar is
	// rendered at edPaneW (not m.w), and the activity / sidebar columns
	// are now full-screen tall (m.h) instead of bodyH.
	m.status.SetWidth(edPaneW)
	editorCol := joinRowsLines(editorRegion, strings.Split(m.status.View(m.statusState()), "\n"))

	activityCol := strings.Split(m.activity.View(), "\n")

	// Right-side Actions panel column. Only rendered when pinned & open;
	// height matches the editor column (editor body + status bar = m.h)
	// so the JoinHorizontal aligns cleanly with the rest of the chrome.
	var actionsCol []string
	if m.actionsPanelVisible() {
		actionsCol = m.renderActionsPanelLines(actionsColW, m.h)
	}

	cols := [][]string{activityCol}
	if m.showExp {
		cols = append(cols, strings.Split(m.renderSidebar(m.h), "\n"))
	}
	cols = append(cols, editorCol)
	if len(actionsCol) > 0 {
		cols = append(cols, actionsCol)
	}
	// Force every column to exactly m.h rows BEFORE the join. Without this,
	// lipgloss.JoinHorizontal pads shorter columns with unstyled ASCII
	// spaces — which leak the terminal's default bg as a strip on the side
	// of whichever column was shortest. Common trigger: a welcome/editor
	// pane whose source content exceeds the available height before the
	// columns settle, e.g. the start page on a small terminal. Padding
	// here uses the editor-bg filler so the safety net is invisible.
	for i, c := range cols {
		cols[i] = padColumnLines(c, m.h, theme.Bg(theme.BgEditor))
	}
	middle := mainJoin.join(cols...)

	if m.findOpen {
		m.overlayFindBarInEditorLines(middle)
	}

	if m.actionsLauncherVisible() {
		m.overlayActionsLauncherLines(middle)
	}

	// Overflow (⋮) glyph at the right edge of the tab bar's body row.
	// Always rendered (even when the dropdown is closed) so the chevron
	// is discoverable. Skipped in zen mode (no chrome at all).
	if !m.zenMode {
		m.overlayOverflowMenuGlyphLines(middle)
	}

	// Inline 💡 preferred-action hint, anchored to the editor cursor.
	// Skipped while modal overlays / pickers are open (View() takes a
	// different branch above) and in zen mode (no chrome / hint clutter).
	if !m.zenMode && m.preferredCodeAction != nil {
		m.overlayPreferredActionHintLines(middle)
	}

	return m.normalizeFrameLines(middle)
}

// overlayActionsLauncherLines splices the bottom-right launcher chip onto
// the frame rows (in place), one row above the status bar. The row
// underneath is kept everywhere except the chip's own cells.
func (m Model) overlayActionsLauncherLines(lines []string) {
	x1, _, y, ok := m.actionsLauncherRect()
	if !ok {
		return
	}
	chip := renderActionsLauncher()
	if chip == "" {
		return
	}
	if y < 0 || y >= len(lines) {
		return
	}
	lines[y] = spliceAt(lines[y], chip, x1)
}

// overlayFindBarInEditorLines splices the compact floating find panel onto
// the top-right of the editor pane (frame rows, in place), 1 row below the
// editor content's top and 2 cells from its right edge. The editor content
// under it stays; the chrome above (tabs, breadcrumbs) is not covered.
func (m Model) overlayFindBarInEditorLines(baseLines []string) {
	pw := m.find.PanelWidth()
	ph := m.find.PanelHeight()
	if pw <= 0 || ph <= 0 {
		return
	}

	// Same editor pane geometry renderBase composed, so the splice
	// column is exactly aligned. The find panel overlays the editor
	// content (NOT the chrome above it).
	pane := m.editorPaneLayout()
	edPaneW, editorStart, topRow := pane.W, pane.X, pane.Top
	// Hover 1 row down from the top of the editor body so the panel feels
	// like it floats inside the editor (à la VS Code) rather than sitting
	// flush against the breadcrumbs row.
	topRow++

	// Right-align with a small inset from the editor pane's right edge.
	const rightInset = 2
	leftCol := editorStart + edPaneW - pw - rightInset
	if leftCol < editorStart+1 {
		leftCol = editorStart + 1
	}

	// Splice each line of the panel into the corresponding row of base.
	panel := m.find.View()
	if panel == "" {
		return
	}
	panelLines := strings.Split(panel, "\n")
	// The panel has a minimum width; in a narrow editor pane it is
	// clipped at the pane's right edge instead of sticking out past the
	// window (an over-wide row wraps in the terminal).
	room := editorStart + edPaneW - leftCol
	if room <= 0 {
		return
	}
	for i, pline := range panelLines {
		row := topRow + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		glassed := pline
		if textWidth(glassed) > room {
			glassed = ansi.Truncate(glassed, room, "")
		}
		baseLines[row] = spliceAt(baseLines[row], glassed, leftCol)
	}
}

// normalizeFrame guarantees the rendered frame is exactly m.h rows tall and
// every row is exactly m.w visible cells wide. Short rows are padded with
// the editor's bg color so the terminal can never expose its default bg
// (the source of "black gaps" you see when an underlying renderer — e.g.
// the editor view on a short file, or JoinHorizontal aligning unequal-
// height columns — emits a row that doesn't reach m.w cells).
//
// Padding uses BgEditor specifically because most under-filled rows are
// the editor's own (the right-of-content / below-EOF area), so that bg
// makes the gap visually invisible. spliceRight callers (e.g. toast
// overlay) then sit on top of a guaranteed-uniform base.
//
// As a second safety net, every row is also scanned for any UNSTYLED
// trailing whitespace (no SGR active) — lipgloss.JoinHorizontal pads
// shorter columns with naked ASCII spaces, which leak the terminal's
// default bg. We rewrite those cells with styled BgEditor so the right
// edge of every row has a guaranteed background.
func (m Model) normalizeFrame(s string) string {
	if m.w <= 0 || m.h <= 0 {
		return s
	}
	return strings.Join(m.normalizeFrameLines(strings.Split(s, "\n")), "\n")
}

// normalizeFrameLines is normalizeFrame on the frame rows (may reuse
// lines' backing array).
func (m Model) normalizeFrameLines(lines []string) []string {
	if m.w <= 0 || m.h <= 0 {
		return lines
	}
	filler := theme.Bg(theme.BgEditor)
	nm := &normMemo
	nm.Lock()
	if rk := theme.CurrentRenderKey(); nm.w != m.w || nm.rk != rk {
		nm.w, nm.rk, nm.in, nm.out = m.w, rk, nil, nil
	}
	for len(nm.in) < len(lines) {
		nm.in = append(nm.in, "")
		nm.out = append(nm.out, "")
	}
	for i, line := range lines {
		if line == nm.in[i] && line != "" {
			lines[i] = nm.out[i]
			continue
		}
		nm.in[i] = line
		w := frameWidth(line)
		if w < m.w {
			line += bgSpaces(theme.BgEditor, m.w-w)
		}
		lines[i] = restyleTrailingUnstyled(line, filler)
		nm.out[i] = lines[i]
	}
	nm.Unlock()
	for len(lines) < m.h {
		lines = append(lines, bgSpaces(theme.BgEditor, m.w))
	}
	if len(lines) > m.h {
		// Trim from the TOP, not bottom — the status bar lives on the
		// last row and is the most important UI element to preserve.
		lines = lines[len(lines)-m.h:]
	}
	return lines
}

// normMemo keeps normalizeFrameLines' rows of the last frame by
// position: a row equal to last frame's comes out as last frame's
// result. Valid for one window width and theme render key.
var normMemo struct {
	sync.Mutex
	w       int
	rk      theme.RenderKey
	in, out []string
}

// padColumnLines forces a column's rows (all the same width) to exactly h
// rows by appending styled filler rows; taller columns stay as they are.
// It runs before columns are joined, so the join never pads a short seam
// with bare spaces that show the terminal's own background.
func padColumnLines(rows []string, h int, filler lipgloss.Style) []string {
	if h <= 0 || len(rows) >= h {
		return rows
	}
	// Width inferred from row 0; fallback to 1-cell pad if empty.
	w := 0
	if len(rows) > 0 {
		w = frameWidth(rows[0])
	}
	if w <= 0 {
		w = 1
	}
	pad := filler.Render(strings.Repeat(" ", w))
	for len(rows) < h {
		rows = append(rows, pad)
	}
	return rows
}

// clipRowsLines keeps the first h rows (the bottom ones go) and pads
// short output to exactly h rows, each right-padded to w cells with styled
// filler, so a briefly narrower nvim grid never leaves a bare strip. It
// may reuse rows' backing array.
func clipRowsLines(rows []string, h, w int, filler lipgloss.Style) []string {
	if h <= 0 {
		return nil
	}
	if len(rows) > h {
		rows = rows[:h]
	}
	if w > 0 {
		// Right-pad each existing row to `w` cells with styled filler.
		for i, row := range rows {
			rw := frameWidth(row)
			switch {
			case rw < w:
				rows[i] = row + filler.Render(strings.Repeat(" ", w-rw))
			case rw > w:
				// A grid wider than its rect (nvim not resized yet during
				// a splitter drag) is clipped, never painted over the
				// neighbouring panel.
				rows[i] = truncRightVisual(row, w)
			}
		}
		if len(rows) < h {
			pad := filler.Render(strings.Repeat(" ", w))
			for len(rows) < h {
				rows = append(rows, pad)
			}
		}
	}
	return rows
}

// restyleTrailingUnstyled rewrites trailing UNSTYLED SPACE cells so they
// pick up `filler`'s background. Catches the unstyled padding that
// lipgloss.JoinHorizontal emits when columns have unequal row counts —
// without this rewrite those naked spaces leak the terminal's default
// bg as a stripe along the right edge.
//
// Only trailing SPACE glyphs with empty SGR are touched. Non-space
// glyphs (real text) and already-styled cells are preserved verbatim,
// so this is safe to run even when the entire row happens to be
// unstyled (e.g. a test environment with the lipgloss profile set to
// "no color"): non-space content keeps its glyph.
func restyleTrailingUnstyled(line string, filler lipgloss.Style) string {
	if !mayEndWithUnstyledSpace(line) {
		return line // the common case: nothing to rewrite, no parse
	}
	cells := parseANSIRow(line)
	if len(cells) == 0 {
		return line
	}
	// Walk from the right, count cells whose active SGR is empty AND
	// whose glyph is a single space. Stop at the first cell that's
	// either styled or contains a non-space glyph.
	trailing := 0
	for i := len(cells) - 1; i >= 0; i-- {
		if cells[i].sgr != "" {
			break
		}
		if cells[i].glyph != " " {
			break
		}
		trailing++
	}
	if trailing == 0 {
		return line
	}
	keep := cells[:len(cells)-trailing]
	return renderCells(keep) + filler.Render(strings.Repeat(" ", trailing))
}

// mayEndWithUnstyledSpace is a quick, allocation-free scan that tells
// restyleTrailingUnstyled when it can skip the full cell parse. It walks
// the row the way parseANSIRow does, but only tracks whether a style is
// active. It returns false only when parseANSIRow would surely end the
// row with a cell that is not an unstyled space; when two styles stack
// without a reset (their merge may cancel out) it stays on the safe side
// and returns true.
//
// Only the text after the last reset matters (a reset clears the style
// state), so the scan starts there and walks back one reset at a time
// only while that tail holds no glyph. Rows end soon after their last
// reset, so this reads a few cells instead of the whole row.
func mayEndWithUnstyledSpace(s string) bool {
	end := len(s)
	for {
		r, rlen := lastSGRReset(s[:end])
		if r < 0 {
			last, _ := scanUnstyledSpace(s[:end])
			return last
		}
		// s[r:end] starts with the reset, so the scan starts unstyled.
		if last, glyph := scanUnstyledSpace(s[r+rlen : end]); glyph {
			return last
		}
		end = r
	}
}

// lastSGRReset returns where the last "\x1b[0m" or "\x1b[m" in s starts
// and its length, or -1.
func lastSGRReset(s string) (int, int) {
	for i := strings.LastIndexByte(s, 0x1b); i >= 0; i = strings.LastIndexByte(s[:i], 0x1b) {
		switch {
		case strings.HasPrefix(s[i:], "\x1b[0m"):
			return i, 4
		case strings.HasPrefix(s[i:], "\x1b[m"):
			return i, 3
		}
	}
	return -1, 0
}

// scanUnstyledSpace is the forward scan behind mayEndWithUnstyledSpace,
// starting with no style. glyph reports whether s holds any glyph.
// lastMaybe: the last glyph is a lone ' ' with no style surely on it.
// It jumps from escape to escape: between them only the last byte
// matters (a ' ' byte is always a whole one-byte glyph).
func scanUnstyledSpace(s string) (lastMaybe, glyph bool) {
	styled, unsure := false, false
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			sgr := s[i:j]
			switch {
			case sgr == "\x1b[0m" || sgr == "\x1b[m":
				styled, unsure = false, false
			case sgr[len(sgr)-1] == 'm':
				if styled {
					unsure = true // mergeSGR may cancel the style out
				}
				styled = true
			}
			i = j
			continue
		}
		// Text up to the next escape (a lone ESC is a glyph of its own).
		end := len(s)
		if k := strings.IndexByte(s[i+1:], 0x1b); k >= 0 {
			end = i + 1 + k
		}
		lastMaybe = s[end-1] == ' ' && (!styled || unsure)
		glyph = true
		i = end
	}
	return lastMaybe, glyph
}

// renderBreadcrumbs renders one row of "seg › seg › seg", clipped from the
// LEFT (i.e. drop earlier segments first) when too wide so the closest
// symbol stays visible. Background uses BgHover (slightly lighter than
// BgEditor) so the breadcrumb strip reads as separate from the editor body
// without needing a divider line.
func (m Model) renderBreadcrumbs(width int) string {
	if width <= 0 {
		return ""
	}
	rk := theme.CurrentRenderKey()
	c := &breadcrumbsMemo
	c.Lock()
	defer c.Unlock()
	if c.ok && c.width == width && c.rk == rk && c.sticky == m.stickyContext && slices.Equal(c.segs, m.breadcrumbs) {
		return c.out
	}
	out := m.drawBreadcrumbs(width)
	c.ok, c.width, c.rk, c.sticky, c.segs, c.out = true, width, rk, m.stickyContext, slices.Clone(m.breadcrumbs), out
	return out
}

// breadcrumbsMemo keeps the last breadcrumbs row: it is drawn on every
// frame and changes only with the path, the sticky context, the width
// and the theme.
var breadcrumbsMemo struct {
	sync.Mutex
	ok     bool
	width  int
	rk     theme.RenderKey
	sticky string
	segs   []string
	out    string
}

func (m Model) drawBreadcrumbs(width int) string {
	// Quiet path trail on the editor surface: folders muted, the file name
	// (with its type icon) one step brighter.
	bg := theme.Bg(theme.BgEditor)
	seg := theme.FgBg(theme.TextMuted, theme.BgEditor)
	last := theme.FgBg(theme.TextSecondary, theme.BgEditor)
	sep := theme.FgBg(theme.TextDimmer, theme.BgEditor)

	bcArea := width

	segs := append([]string(nil), m.breadcrumbs...)
	signature := strings.TrimSpace(m.stickyContext)
	for {
		var b strings.Builder
		b.WriteString(bg.Render(" "))
		for i, s := range segs {
			if i > 0 {
				b.WriteString(sep.Render(" › "))
			}
			if i == len(segs)-1 {
				if theme.CurrentIconMode != theme.IconModeASCII {
					g, c := theme.FileIconFor(s, false, false)
					b.WriteString(theme.FgBg(c, theme.BgEditor).Render(g) + bg.Render(" "))
				}
				b.WriteString(last.Render(s))
				continue
			}
			b.WriteString(seg.Render(s))
		}
		if signature != "" {
			if len(segs) > 0 {
				b.WriteString(sep.Render(" › "))
			}
			b.WriteString(renderSignature(signature))
		}
		out := b.String()
		used := textWidth(out)
		if used <= bcArea {
			pad := bcArea - used
			if pad > 0 {
				out += bg.Render(strings.Repeat(" ", pad))
			}
			return out
		}
		if len(segs) > 0 {
			segs = segs[1:]
			continue
		}
		if signature != "" {
			signature = ""
			continue
		}
		// Nothing fits — emit a bg-filled placeholder.
		return bg.Render(strings.Repeat(" ", bcArea))
	}
}

// renderSignature paints `sig` with three colour roles: leading language
// keyword (func/class/def/…) in SyntaxKeyword, function name in
// SyntaxFunction, everything else in TextSecondary. It's not a real parser
// — just enough tokenisation to make the breadcrumb feel like code. The
// background is BgEditor, like the breadcrumb strip.
func renderSignature(sig string) string {
	// Trim trailing language brackets / colons that aren't part of the
	// useful signature: `func foo() {` → `func foo()`.
	sig = strings.TrimRight(strings.TrimSpace(sig), "{:")
	sig = strings.TrimSpace(sig)
	// No Faint (SGR 2): many terminals ignore it or make it unreadable on
	// light themes. The rest of the signature is one tier quieter instead.
	kwStyle := theme.FgBg(theme.SyntaxKeyword, theme.BgEditor)
	nameStyle := theme.FgBg(theme.SyntaxFunction, theme.BgEditor)
	restStyle := theme.FgBg(theme.TextSecondary, theme.BgEditor)
	keywords := []string{"func ", "class ", "def ", "interface ", "type ", "impl ", "struct ", "fn "}
	for _, kw := range keywords {
		if strings.HasPrefix(sig, kw) {
			head := strings.TrimSpace(kw)
			tail := sig[len(kw):]
			// Function name: identifier sitting right before the LAST '(' in
			// the line (handles Go-style receiver `(m Model) name(args)`).
			lp := strings.LastIndexByte(tail, '(')
			if lp <= 0 {
				return kwStyle.Render(head) + restStyle.Render(" "+tail)
			}
			before := tail[:lp]
			rest := tail[lp:]
			// Walk back from `lp` over identifier chars to find the name.
			i := len(before)
			for i > 0 && isIdentChar(before[i-1]) {
				i--
			}
			name := before[i:]
			pre := before[:i]
			return kwStyle.Render(head) + restStyle.Render(" "+pre) + nameStyle.Render(name) + restStyle.Render(rest)
		}
	}
	return restStyle.Render(sig)
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// renderEditorScrollbar returns a height-tall, 1-column overview ruler. It
// renders in three layers (lowest priority first):
//
//  1. Track: a dim │ on BgEditor for every row.
//  2. Git markers (TODO v1: stubbed — git change line numbers aren't yet
//     threaded through to the editor model).
//  3. Diagnostic markers (TODO v1: stubbed — only counts are exposed today;
//     per-line locations would require a separate Lua fetch).
//  4. Thumb: a bright █ block sized proportionally to viewport / total
//     lines, positioned by m.editor.Cursor().Line / m.editor.LineCount().
//
// When the editor has no buffer (LineCount == 0) we just render the track —
// no thumb, no markers.
func (m Model) renderEditorScrollbar(height int) string {
	if height <= 0 {
		return ""
	}
	trackStyle := theme.FgBg(theme.TextDimmer, theme.BgEditor)
	thumbStyle := theme.FgBg(theme.BorderFocus, theme.BgEditor)

	// Default every row to the track glyph.
	rows := make([]string, height)
	track := trackStyle.Render("│")
	for i := range rows {
		rows[i] = track
	}

	// Compute thumb extent. The "total line count" approximation we have
	// today is the editor grid Height (one screen). Without a true total-
	// line count from nvim, we just render the full column as the thumb
	// when the cursor is on screen — which collapses to "no thumb visible"
	// most of the time. v1 acceptable; a follow-up will fetch line('$').
	totalLines := m.editor.LineCount()
	if totalLines > 0 {
		cur := m.editor.Cursor()
		// Thumb size: at minimum 1 row, otherwise (height/total)*height
		// rounded up. Since we don't have a true total here we approximate
		// the thumb at 1 row at the cursor's grid row, which still gives a
		// useful "where am I" cue inside the visible viewport.
		row := cur.Line
		if row < 0 {
			row = 0
		}
		if row >= height {
			row = height - 1
		}
		rows[row] = thumbStyle.Render("█")
	}

	// Diagnostic markers from vim.diagnostic.get(0). Each LSP severity is
	// projected to a scrollbar row via line/totalLines * height. Errors win
	// over warnings on the same row; cursor thumb wins over both for the
	// "you are here" cue.
	if m.scrollbarTotalLines > 0 && len(m.scrollbarMarks) > 0 {
		errStyle := theme.FgBg(theme.DiagError, theme.BgEditor)
		warnStyle := theme.FgBg(theme.DiagWarning, theme.BgEditor)
		// Track "what's on each row" so errors override warnings.
		hasErr := make([]bool, height)
		for _, mark := range m.scrollbarMarks {
			y := mark.Line * height / m.scrollbarTotalLines
			if y < 0 {
				y = 0
			}
			if y >= height {
				y = height - 1
			}
			switch mark.Severity {
			case 1: // error
				rows[y] = errStyle.Render("█")
				hasErr[y] = true
			case 2: // warning
				if !hasErr[y] {
					rows[y] = warnStyle.Render("█")
				}
			}
		}
	}

	return strings.Join(rows, "\n")
}

// sidebarFrameCache keeps the last sidebar column: the panel content,
// the hovered row and the height in, the hover tint and the divider
// joined on, out. The explorer's View is itself cached, so on a frame
// where the sidebar did not change this is a pointer compare.
var sidebarFrameCache struct {
	sync.Mutex
	content string
	hy, h   int
	rk      theme.RenderKey
	out     string
}

func (m Model) renderSidebar(h int) string {
	contentW := m.explorerWidth - 1 // 1 col reserved for the right-edge divider
	var content string
	hy := -1
	switch m.activity.Active() {
	case activity.ViewFiles:
		// Explorer renders its own header + footer, so we use its output
		// directly (no sidebarHeader join). Size is already set to (contentW,
		// m.h) in update.go.
		ex := m.explorer
		ex.SetMeta(m.projectLabel(), m.gitBranch.Name, m.gitIsRepo, totalVisibleFiles(&ex))
		content = ex.View(
			m.focus == FocusExplorer,
			m.editor.Path(),
			m.editor.IsDirty(),
			m.gitStatusMap(),
		)
		hy = m.explorerHoverLine()
	case activity.ViewSearch:
		header := sidebarHeader(contentW, "SEARCH", "")
		body := strings.Join(renderEmptyState(contentW, h-1, theme.BgSidebar, emptyState{Glyph: theme.IconSearch,
			Title: "Find in files", Hint: "Search the whole workspace. Ctrl+F finds in the current file.", Key: "f8"}), "\n")
		content = joinRows(header, body)
	case activity.ViewGit:
		content = m.renderGitSidebar(contentW, h)
	case activity.ViewRun:
		content = m.renderRunSidebar(contentW, h) // Group D (debug_view.go)
	case activity.ViewTests: // Group E (test_explorer.go)
		content = m.renderTestsSidebar(contentW, h)
	case activity.ViewServices: // services_view.go
		content = m.renderServicesSidebar(contentW, h)
	case activity.ViewKubernetes: // kube_view.go
		content = m.renderKubeSidebar(contentW, h)
	case activity.ViewDatabases: // db_view.go
		content = m.renderDatabasesSidebar(contentW, h)
	case activity.ViewItermoc: // itermoc_view.go
		content = m.renderItermocSidebar(contentW, h)
	case activity.ViewRuns: // runs_view.go
		content = m.renderRunsSidebar(contentW, h)
	default:
		if !m.isExtSidebar() { // Group I: extension panels (ext.go)
			return ""
		}
		content = m.renderExtSidebar(contentW, h)
	}
	rk := theme.CurrentRenderKey()
	c := &sidebarFrameCache
	c.Lock()
	defer c.Unlock()
	if c.out != "" && c.content == content && c.hy == hy && c.h == h && c.rk == rk {
		return c.out
	}
	raw := content
	// VSCode-style hover: tint the body row under the pointer. The re-tint
	// only touches panel-bg cells, so the cursor/selection rows (which use a
	// different bg) and blank rows are skipped automatically.
	if hy >= 0 {
		lines := strings.Split(content, "\n")
		if hy < len(lines) {
			lines[hy] = sidebarHoverLineBg(lines[hy])
			content = strings.Join(lines, "\n")
		}
	}
	out := joinColumns(content, sidebarDivider(h))
	c.content, c.hy, c.h, c.rk, c.out = raw, hy, h, rk, out
	return out
}

func sidebarDivider(h int) string {
	style := lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.BorderSubtle))
	cell := style.Render("▕")
	var lines []string
	for i := 0; i < h; i++ {
		lines = append(lines, cell)
	}
	return strings.Join(lines, "\n")
}

// totalVisibleFiles returns the number of file (non-dir) entries currently
// visible in the explorer. Used by the explorer footer's "N files" total.
func totalVisibleFiles(ex *explorer.Model) int {
	if ex == nil {
		return 0
	}
	return ex.CountVisibleFiles()
}

// projectLabel is projectName for the Model's workspace (drawn on every
// frame: no getcwd).
func (m Model) projectLabel() string {
	root, err := m.rootDir()
	if err != nil {
		return ""
	}
	return strings.ToUpper(filepath.Base(root))
}

func projectName() string {
	cwd, err := procRootDir()
	if err != nil {
		return ""
	}
	return strings.ToUpper(filepath.Base(cwd))
}

// sidebarHeader renders one row: "  EXPLORER … <project> ⋯ ".
// Layout: 2 cols left pad, label (TextSecondary, bold, uppercase), then
// flexible filler, then the project name (TextMuted) and the more-options
// ellipsis (TextMuted), then 1 col right pad. ":" between label and project
// is intentionally dropped — the visual separation is the gap.
//
// The header bg matches BgSidebar (not BgSectionHdr) so the top row of the
// sidebar reads as part of the sidebar — distinct from the BgTitleBar tabs
// to its right and the BgActivityBar column to its left.
func sidebarHeader(w int, label, project string) string {
	fill := theme.Bg(theme.BgSidebar)
	labelStyle := theme.FgBg(theme.TextSecondary, theme.BgSidebar).Bold(true)
	mutedStyle := theme.FgBg(theme.TextMuted, theme.BgSidebar)

	left := fill.Render("  ") + labelStyle.Render(strings.ToUpper(label))
	leftW := textWidth(left)

	var right string
	if project != "" {
		right = mutedStyle.Render(strings.ToUpper(project)) + fill.Render(" ") + mutedStyle.Render(theme.GlyphMore.String()) + fill.Render(" ")
	} else {
		right = mutedStyle.Render(theme.GlyphMore.String()) + fill.Render(" ")
	}
	rightW := textWidth(right)

	gap := w - leftW - rightW
	if gap < 1 {
		// Drop the project name and keep only the ⋯ if it doesn't fit.
		right = mutedStyle.Render(theme.GlyphMore.String()) + fill.Render(" ")
		rightW = textWidth(right)
		gap = w - leftW - rightW
		if gap < 0 {
			gap = 0
		}
	}
	return left + fill.Render(strings.Repeat(" ", gap)) + right
}

// Sidebar colours: derived from theme tokens on every frame (refreshSidebarColors
// runs from renderBase), so a theme switch recolours Source Control at once.
var (
	sidebarBg      lipgloss.Color
	sidebarFill    lipgloss.Style
	gitTitleStyle  lipgloss.Style
	gitBranchStyle lipgloss.Style
	gitNameStyle   lipgloss.Style
	gitDirStyle    lipgloss.Style
	gitCursorBlur  lipgloss.Style
	gitCursorFocus lipgloss.Style
)

func init() { refreshSidebarColors() }

// sidebarColorsKey is the theme the sidebar styles were last built for.
var sidebarColorsKey struct {
	sync.Mutex
	ok bool
	rk theme.RenderKey
}

// refreshSidebarColorsIfStale rebuilds the sidebar styles when the theme
// changed since the last frame (they are built from the palette only).
func refreshSidebarColorsIfStale() {
	rk := theme.CurrentRenderKey()
	sidebarColorsKey.Lock()
	defer sidebarColorsKey.Unlock()
	if sidebarColorsKey.ok && sidebarColorsKey.rk == rk {
		return
	}
	refreshSidebarColors()
	sidebarColorsKey.ok, sidebarColorsKey.rk = true, rk
}

func refreshSidebarColors() {
	sidebarBg = theme.LG(theme.BgSidebar)
	sidebarFill = lipgloss.NewStyle().Background(sidebarBg)
	gitTitleStyle = lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.TextPrimary)).Bold(true)
	gitBranchStyle = lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.TextMuted))
	gitNameStyle = lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.TextPrimary))
	gitDirStyle = lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.TextMuted))
	gitCursorBlur = lipgloss.NewStyle().Background(theme.Wash(theme.BgSidebar, theme.WashSelected)).Foreground(theme.LG(theme.TextPrimary))
	gitCursorFocus = lipgloss.NewStyle().Background(theme.LG(theme.BorderFocus)).Foreground(theme.LG(theme.OnAccent))
	refreshGitGraphColors()
	refreshCommitBoxColors()
}

func (m Model) renderGitSidebar(w, h int) string {
	if !m.gitLoaded {
		return strings.Join(renderEmptyState(w, h, theme.BgSidebar, emptyState{Title: "Loading…"}), "\n")
	}
	if !m.gitIsRepo {
		return strings.Join(scmNotRepoEmpty(w, h), "\n")
	}

	focused := m.focus == FocusExplorer

	// ── Fixed header (mirrors the file-explorer header): letter-spaced
	// title, a hairline underline, then a branch + tree·flat sub-row, then a
	// blank spacer. gitPanelTopOffset() must stay in sync (4 rows).
	var header []string
	header = append(header, renderGitTitleRow(w, focused))
	header = append(header, renderGitHairline(w, focused))
	header = append(header, renderGitSubheader(m.gitBranch, m.gitViewTree, w))
	header = append(header, sidebarFill.Render(strings.Repeat(" ", w)))
	header = append(header, m.renderGitCommitBox(w))
	header = append(header, sidebarFill.Render(strings.Repeat(" ", w)))
	header = append(header, m.renderGitActionBar(w))
	header = append(header, sidebarFill.Render(strings.Repeat(" ", w)))

	// ── Scrollable body + optional footer hints (shared layout with the
	// mouse hit-test so clicks land on the row that was drawn) ────────────
	top, bodyH, footerRows := m.gitPanelLayout(h)
	rows := m.gitPanelRows()

	var body []string
	for i := top; i < len(rows) && len(body) < bodyH; i++ {
		body = append(body, m.renderGitPanelRow(rows[i], i == m.gitCursor, focused, w))
	}
	for len(body) < bodyH {
		body = append(body, sidebarFill.Render(strings.Repeat(" ", w)))
	}

	lines := append(header, body...)
	if footerRows == 2 {
		lines = append(lines,
			padBgToWidth(gitBranchStyle.Render(" s stage  d diff  c commit"), w),
			padBgToWidth(gitBranchStyle.Render(" x discard  r refresh  t tree"), w),
		)
	}
	for len(lines) < h {
		lines = append(lines, sidebarFill.Render(strings.Repeat(" ", w)))
	}
	if len(lines) > h {
		lines = lines[:h]
	}

	// VSCode-style hover: tint the row under the pointer (skipping the cursor
	// row, which keeps its stronger selection, and blank rows).
	if hy := m.gitHoverLine(); hy >= 0 && hy < len(lines) {
		cursorLine := len(header) + (m.gitCursor - top)
		if hy != cursorLine {
			lines[hy] = sidebarHoverLineBg(lines[hy])
		}
	}
	return strings.Join(lines, "\n")
}

// gitHoverLine returns the sidebar-row index under the mouse pointer, or -1 if
// the pointer isn't over the Source Control panel. The panel's first screen row
// is 0, so the row index equals the absolute hover Y.
func (m Model) gitHoverLine() int {
	if m.hoverX < activity.Width || m.hoverX >= activity.Width+m.explorerWidth {
		return -1
	}
	return m.hoverY
}

// explorerHoverLine returns the sidebar screen-row under the pointer when the
// Files pane is showing and the pointer is over an explorer BODY row (not the
// header chrome, footer, or the right-edge divider). -1 otherwise. Recomputed
// from the live pointer each render, so it never goes stale when the pointer
// leaves the panel.
func (m Model) explorerHoverLine() int {
	if m.activity.Active() != activity.ViewFiles {
		return -1
	}
	if m.hoverX < activity.Width || m.hoverX >= activity.Width+m.explorerWidth-1 {
		return -1
	}
	headerRows := m.explorer.HeaderChromeRows()
	footerRows := m.explorer.FooterChromeRows()
	if m.hoverY < headerRows || m.hoverY >= m.h-footerRows {
		return -1
	}
	return m.hoverY
}

// sidebarHoverLineBg re-tints the panel-background cells of a rendered row to
// the hover colour, leaving pills, chips and the cursor/selection rows
// untouched (their bg isn't the panel bg, so the re-tint skips them). Blank
// rows are left alone so empty space doesn't light up. Shared by the Source
// Control panel and the file explorer so hover reads identically in both.
func sidebarHoverLineBg(line string) string {
	if strings.TrimSpace(ansi.Strip(line)) == "" {
		return line
	}
	from := theme.ToRGB(theme.BgSidebar)
	to, _ := theme.ParseHex(string(theme.Wash(theme.BgSidebar, theme.WashHover)))
	cells := parseANSIRow(line)
	for i := range cells {
		if bg, ok := extractBgRGB(cells[i].sgr); ok && bg == from {
			cells[i].sgr = replaceBgRGB(cells[i].sgr, to)
		}
	}
	return renderCells(cells)
}

// renderGitPanelRow dispatches one accordion row to its renderer.
func (m Model) renderGitPanelRow(r gitPanelRow, active, focused bool, w int) string {
	switch r.kind {
	case gitRowSection:
		switch r.section {
		case gitSecStaged:
			staged, _ := m.gitFileCounts()
			return renderGitSectionHeader("STAGED", strconv.Itoa(staged), !m.gitStagedCollapsed, active, focused, w)
		case gitSecChanges:
			_, changed := m.gitFileCounts()
			return renderGitSectionHeader("CHANGES", strconv.Itoa(changed), !m.gitChangesCollapsed, active, focused, w)
		case gitSecConflicts:
			return renderGitSectionHeader("CONFLICTS", strconv.Itoa(m.gitConflictCount()), !m.gitConflictsCollapsed, active, focused, w)
		default:
			n := ""
			if len(m.gitCommits) > 0 {
				n = strconv.Itoa(len(m.gitCommits))
				if m.gitGraphMore {
					n += "+"
				}
			}
			return renderGitSectionHeader("GRAPH", n, !m.gitGraphCollapsed, active, focused, w)
		}
	case gitRowDir:
		name := r.dirPath
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		}
		return renderGitTreeDir(name, r.depth, !m.gitCollapsed[r.dirPath], active, focused, w)
	case gitRowFile:
		if m.gitViewTree {
			return renderGitTreeFile(m.gitFiles[r.fileIndex], r.depth, active, focused, w)
		}
		return renderGitFile(m.gitFiles[r.fileIndex], active, focused, w)
	case gitRowCommit:
		return m.gitRowMemo.commitRow(m.gitCommits, r.ci, m.gitCompareMarkOf(m.gitCommits[r.ci].FullHash), active, focused, w)
	case gitRowGroup:
		return renderGitGroupRow(r.group, r.groupN, m.gitGroupFolded[r.group], active, focused, w)
	case gitRowMore:
		return renderGitMoreRow(r.older, active, focused, w)
	case gitRowNote:
		return padBgToWidth(gitBranchStyle.Render("   "+r.note), w)
	case gitRowRebase:
		return m.renderGitRebaseRow(active, focused, w)
	case gitRowSpacer:
		return sidebarFill.Render(strings.Repeat(" ", w))
	}
	return sidebarFill.Render(strings.Repeat(" ", w))
}

// renderGitFile renders a flat-list file row: the lead/accent cell, a coloured
// status dot, the basename, then a dim parent dir.
func renderGitFile(f git.FileStatus, active, focused bool, w int) string {
	statusChar, statusFG := gitStatusDot(f)
	name := filepath.Base(f.Path)
	dir := filepath.Dir(f.Path)
	if dir == "." {
		dir = ""
	}

	bgC := gitSelBg(active, focused)
	fgName := theme.LG(theme.TextPrimary)
	fgDir := theme.LG(theme.TextMuted)
	fgStatus := statusFG
	if active {
		fgName = theme.Mix(theme.TextPrimary, theme.TextWhite, 0.5)
		fgDir = theme.LG(theme.TextSecondary)
		if focused {
			fgName = theme.LG(theme.TextWhite)
			fgDir = theme.Mix(theme.TextSecondary, theme.BorderFocus, 0.3)
		}
	}

	rowStyle := lipgloss.NewStyle().Background(bgC)
	nameStyle := lipgloss.NewStyle().Background(bgC).Foreground(fgName)
	dirStyle := lipgloss.NewStyle().Background(bgC).Foreground(fgDir)
	statusStyle := lipgloss.NewStyle().Background(bgC).Foreground(fgStatus).Bold(true)

	// Layout: "▌●  name  dir            " — lead + status dot + name + dim dir.
	avail := w - 3
	if avail < 4 {
		avail = 4
	}
	if runewidth.StringWidth(name) > avail-1 {
		name = runewidth.Truncate(name, avail-1, "…")
		dir = ""
	}
	dirAvail := avail - runewidth.StringWidth(name) - 3
	if dirAvail < 3 {
		dir = ""
	} else if dir != "" {
		dir = leftEllipsize(dir, dirAvail)
	}

	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bgC))
	sb.WriteString(statusStyle.Render(statusChar))
	sb.WriteString(rowStyle.Render("  "))
	sb.WriteString(nameStyle.Render(name))
	used := 1 + 1 + 2 + runewidth.StringWidth(name)
	if dir != "" {
		sb.WriteString(rowStyle.Render("  "))
		sb.WriteString(dirStyle.Render(dir))
		used += 2 + runewidth.StringWidth(dir)
	}
	if pad := w - used; pad > 0 {
		sb.WriteString(rowStyle.Render(strings.Repeat(" ", pad)))
		used += pad
	}
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}

// gitViewIconTree / gitViewIconFlat are the 1-cell glyphs shown at the right
// edge of the SOURCE CONTROL title row. The glyph reflects the CURRENT mode;
// clicking it (or pressing `t`) toggles to the other.
// gitViewToggleWidth is the on-screen width of the view-mode toggle glyph plus
// its trailing breather; handleGitSidebarMouse hit-tests the right edge of the
// sub-header row against it. The width depends on the icon mode (a Nerd-Font
// glyph is 1 cell; the ASCII fallback is the 4-cell word).
func gitViewToggleWidth(viewTree bool) int {
	return textWidth(gitViewToggleGlyph(viewTree)) + 1
}

// gitViewToggleGlyph is the toggle's glyph for the current view mode.
func gitViewToggleGlyph(viewTree bool) string {
	if viewTree {
		return theme.IconViewTree.String()
	}
	return theme.IconViewFlat.String()
}

// renderGitTitleRow draws the letter-spaced " S O U R C E   C O N T R O L "
// title, mirroring the file-explorer's "E X P L O R E R" header. Falls back to
// the plain title when the panel is too narrow for the spaced form.
func renderGitTitleRow(w int, focused bool) string {
	return sidebarTitle("SOURCE CONTROL", w, focused, projectName())
}

// sidebarTitle is the title row of a side panel: plain caps (no letter
// spacing, so long titles fit), bold, in the accent while the panel has the
// keyboard (theme.PaneTitle), with a two-cell margin. Exactly w cells.
func sidebarTitle(label string, w int, focused bool, project ...string) string {
	if w <= 0 {
		return ""
	}
	if max := w - 3; runewidth.StringWidth(label) > max && max > 0 {
		label = runewidth.Truncate(label, max, "…")
	}
	st := lipgloss.NewStyle().Background(theme.LG(theme.BgSidebar)).
		Foreground(theme.PaneTitle(focused)).Bold(true)
	row := theme.Bg(theme.BgSidebar).Render("  ") + st.Render(label)
	// "· PROJECT" after the title, like the Explorer's, when it fits.
	if len(project) > 0 && project[0] != "" {
		if room := w - textWidth(row) - 4; room >= 4 {
			p := project[0]
			if runewidth.StringWidth(p) > room {
				p = runewidth.Truncate(p, room, "…")
			}
			row += theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(" · ") + theme.FgBg(theme.TextSecondary, theme.BgSidebar).Render(p)
		}
	}
	return padBgToWidth(row, w)
}

// renderGitHairline draws the line under the title — the same pane
// hairline the explorer uses (lit while the panel has the keyboard).
func renderGitHairline(w int, focused bool) string {
	return theme.PaneHairline(w, theme.BgSidebar, focused)
}

// gitViewToggleWidth (declared above) is hit-tested on the sub-header row.

// renderGitSubheader renders the branch (left) and the tree·flat toggle
// (right) under the hairline — the row the mouse hit-tests for the toggle.
func renderGitSubheader(b git.Branch, viewTree bool, w int) string {
	rowStyle := lipgloss.NewStyle().Background(sidebarBg)
	branchStyle := lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.AccentBlue)).Bold(true)
	aheadStyle := lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.GitAdded))
	behindStyle := lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.GitModified))
	on := lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.AccentBlue)).Bold(true)

	toggleGlyph := gitViewToggleGlyph(viewTree)
	toggle := on.Render(toggleGlyph)
	toggleW := textWidth(toggleGlyph)

	var sb strings.Builder
	sb.WriteString(rowStyle.Render(" "))
	used := 1
	// Branch glyph, then the branch name. In nerd-font / unicode icon modes the
	// glyph is a clean fork mark; the ASCII fallback ("git:") is dropped — it
	// reads as noise next to the "SOURCE CONTROL" title, and the teal branch
	// name already says what it is.
	if bi := theme.IconBranch.String(); bi != "" && bi != theme.IconBranch.ASCII {
		sb.WriteString(branchStyle.Render(bi))
		sb.WriteString(rowStyle.Render(" "))
		used += textWidth(bi) + 1
	}
	if b.Name != "" {
		sync := ""
		if b.Ahead > 0 {
			sync += fmt.Sprintf(" ↑%d", b.Ahead)
		}
		if b.Behind > 0 {
			sync += fmt.Sprintf(" ↓%d", b.Behind)
		}
		name := b.Name
		if maxName := w - used - toggleW - 2 - runewidth.StringWidth(sync); runewidth.StringWidth(name) > maxName && maxName >= 1 {
			name = runewidth.Truncate(name, maxName, "…")
		}
		sb.WriteString(branchStyle.Render(name))
		used += runewidth.StringWidth(name)
		if b.Ahead > 0 {
			seg := fmt.Sprintf(" ↑%d", b.Ahead)
			sb.WriteString(aheadStyle.Render(seg))
			used += runewidth.StringWidth(seg)
		}
		if b.Behind > 0 {
			seg := fmt.Sprintf(" ↓%d", b.Behind)
			sb.WriteString(behindStyle.Render(seg))
			used += runewidth.StringWidth(seg)
		}
	}

	// Right-align the toggle (drop it if there isn't room).
	if pad := w - used - toggleW - 1; pad >= 0 {
		sb.WriteString(rowStyle.Render(strings.Repeat(" ", pad)))
		sb.WriteString(toggle)
		sb.WriteString(rowStyle.Render(" "))
		used += pad + toggleW + 1
	}
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}

// gitRowColors centralises the cursor/blur background + foreground choices
// shared by the tree dir and file rows so they highlight identically.
func gitRowColors(active, focused bool, statusFG lipgloss.Color) (bg, name, status lipgloss.Color) {
	bg = gitSelBg(active, focused)
	name = theme.LG(theme.TextPrimary)
	status = statusFG
	if active {
		name = theme.Mix(theme.TextPrimary, theme.TextWhite, 0.5)
		if focused {
			name = theme.LG(theme.TextWhite)
		}
	}
	return bg, name, status
}

// gitSelBg returns a row's background: a subtle dark slate for the cursor row
// (so the bright left accent bar — not a loud full-width fill — carries the
// selection) and the panel bg otherwise.
func gitSelBg(active, focused bool) lipgloss.Color {
	switch {
	case active && focused:
		return theme.Wash(theme.BgSidebar, theme.WashSelectedFocused)
	case active:
		return theme.Wash(theme.BgSidebar, theme.WashSelected)
	default:
		return sidebarBg
	}
}

// gitLead renders the leftmost cell of a row: a bright cyan accent bar on the
// focused cursor row, otherwise a blank in the row background.
func gitLead(active, focused bool, bg lipgloss.Color) string {
	if active && focused {
		return lipgloss.NewStyle().Background(bg).Foreground(theme.LG(theme.BorderFocus)).Render("▌")
	}
	return lipgloss.NewStyle().Background(bg).Render(" ")
}

// gitTreeGuide renders the accent-bar/blank lead cell + one `│ ` indent guide
// per nesting level, returning the written width so callers can budget the name.
func gitTreeGuide(sb *strings.Builder, bg lipgloss.Color, depth int, active, focused bool) int {
	rowStyle := lipgloss.NewStyle().Background(bg)
	guideStyle := lipgloss.NewStyle().Background(bg).Foreground(theme.Mix(theme.BgSidebar, theme.TextDimmer, 0.6))
	sb.WriteString(gitLead(active, focused, bg))
	width := 1
	for d := 0; d < depth; d++ {
		sb.WriteString(guideStyle.Render("│"))
		sb.WriteString(rowStyle.Render(" "))
		width += 2
	}
	return width
}

// renderGitTreeDir renders a collapsible directory header row: indent guides,
// an amber-when-open chevron, then the folder name in bold.
func renderGitTreeDir(name string, depth int, expanded, active, focused bool, w int) string {
	bg, fgName, _ := gitRowColors(active, focused, theme.LG(theme.TextMuted))
	rowStyle := lipgloss.NewStyle().Background(bg)
	chev, chevFG := "▸", theme.LG(theme.TextMuted)
	if expanded {
		chev, chevFG = "▾", theme.LG(theme.GitModified)
	}
	chevStyle := lipgloss.NewStyle().Background(bg).Foreground(chevFG)
	nameStyle := lipgloss.NewStyle().Background(bg).Foreground(fgName).Bold(true)

	var sb strings.Builder
	used := gitTreeGuide(&sb, bg, depth, active, focused)
	sb.WriteString(chevStyle.Render(chev))
	sb.WriteString(rowStyle.Render(" "))
	used += 2

	avail := w - used - 1
	if avail < 1 {
		avail = 1
	}
	if runewidth.StringWidth(name) > avail {
		name = runewidth.Truncate(name, avail, "…")
	}
	sb.WriteString(nameStyle.Render(name))
	used += runewidth.StringWidth(name)
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}

// renderGitTreeFile renders a file row inside the tree: indent guides, a
// coloured status dot, then the basename. The full path lives in the dir
// headers above it, so only the basename shows here.
func renderGitTreeFile(f git.FileStatus, depth int, active, focused bool, w int) string {
	statusChar, statusFG := gitStatusDot(f)
	bg, fgName, fgStatus := gitRowColors(active, focused, statusFG)
	rowStyle := lipgloss.NewStyle().Background(bg)
	nameStyle := lipgloss.NewStyle().Background(bg).Foreground(fgName)
	statusStyle := lipgloss.NewStyle().Background(bg).Foreground(fgStatus).Bold(true)

	var sb strings.Builder
	used := gitTreeGuide(&sb, bg, depth, active, focused)
	sb.WriteString(statusStyle.Render(statusChar))
	sb.WriteString(rowStyle.Render("  "))
	used += 3

	name := filepath.Base(f.Path)
	avail := w - used - 1
	if avail < 1 {
		avail = 1
	}
	if runewidth.StringWidth(name) > avail {
		name = runewidth.Truncate(name, avail, "…")
	}
	sb.WriteString(nameStyle.Render(name))
	used += runewidth.StringWidth(name)
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}

// clampSidebarRow forces a styled sidebar row to exactly w cells: pad with
// the row's own background when short (so a cursor/selected row keeps its
// highlight to the edge), or ANSI-truncate when an extreme indent depth
// pushes it past w (so it can't widen the sidebar and shift the editor).
func clampSidebarRow(row string, width, w int, rowStyle lipgloss.Style) string {
	if pad := w - width; pad > 0 {
		return row + rowStyle.Render(strings.Repeat(" ", pad))
	} else if width > w {
		return ansi.Truncate(row, w, "")
	}
	return row
}

// ── Accordion section + graph rows ──────────────────────────────────────────

// refreshGitGraphColors is kept for the theme hook; the GRAPH rows read
// their colours from the theme on every render.
func refreshGitGraphColors() {}

// renderGitSectionHeader renders a collapsible accordion divider as a quiet
// letter-spaced caption with a trailing count:
//
//	C H A N G E S   4
//
// Expanded sections show just the spaced label (clean); a collapsed one gets a
// leading "▸" so the folded state is visible. The cursor row gets the accent
// bar + subtle bg like every other row.
func renderGitSectionHeader(label, count string, expanded, active, focused bool, w int) string {
	bg := gitSelBg(active, focused)
	chevFG := theme.LG(theme.TextDim)
	labelFG := theme.LG(theme.TextSecondary)
	ruleFG := theme.LG(theme.BorderSubtle)
	chipBg, chipFG := theme.Mix(theme.BgSidebar, theme.TextPrimary, 0.08), theme.LG(theme.TextMuted)
	if active {
		chevFG = theme.LG(theme.TextSecondary)
		labelFG = theme.LG(theme.TextPrimary)
		ruleFG = theme.LG(theme.BorderDefault)
		chipBg, chipFG = theme.Wash(theme.BgSidebar, theme.WashPressed), theme.LG(theme.TextWhite)
		if focused {
			labelFG = theme.LG(theme.TextWhite)
		}
	}
	rowStyle := lipgloss.NewStyle().Background(bg)
	chevStyle := lipgloss.NewStyle().Background(bg).Foreground(chevFG)
	labelStyle := lipgloss.NewStyle().Background(bg).Foreground(labelFG).Bold(true)
	ruleStyle := lipgloss.NewStyle().Background(bg).Foreground(ruleFG)
	chipStyle := lipgloss.NewStyle().Background(chipBg).Foreground(chipFG).Bold(true)

	chev := "▾"
	if !expanded {
		chev = "▸"
	}

	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg))
	sb.WriteString(chevStyle.Render(chev))
	sb.WriteString(rowStyle.Render(" "))
	sb.WriteString(labelStyle.Render(label))
	sb.WriteString(rowStyle.Render(" "))
	used := 1 + 1 + 1 + runewidth.StringWidth(label) + 1

	// Count as a small chip on the right (" 3 "); the rule fills the gap.
	// chipTotal = leading space + chip + trailing breather.
	chip := ""
	chipTotal := 0
	if count != "" {
		chip = chipStyle.Render(" " + count + " ")
		chipTotal = 1 + textWidth(chip) + 1
	}
	if rule := w - used - chipTotal; rule >= 1 {
		sb.WriteString(ruleStyle.Render(strings.Repeat("─", rule)))
		used += rule
	}
	if chip != "" && used+chipTotal <= w {
		sb.WriteString(rowStyle.Render(" "))
		sb.WriteString(chip)
		sb.WriteString(rowStyle.Render(" "))
		used += chipTotal
	}
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}

// gitTypePrefix is the length of a conventional-commit prefix ("fix: ",
// "feat(git)!: "), 0 when the subject has none. The row shows it muted.
func gitTypePrefix(subject string) int {
	colon := strings.Index(subject, ": ")
	if colon <= 0 || colon > 24 {
		return 0
	}
	head := strings.TrimSuffix(subject[:colon], "!")
	if p := strings.IndexByte(head, '('); p >= 0 {
		if !strings.HasSuffix(head, ")") {
			return 0
		}
		head = head[:p]
	}
	for _, r := range head {
		if r < 'a' || r > 'z' {
			return 0
		}
	}
	if head == "" {
		return 0
	}
	return colon + 2
}

// gitRefPills renders a commit's refs as quiet pills (local blue, tag
// amber, remote grey) while they fit in room cells; the rest fold into a
// dim "+N". It returns the text and its width.
func gitRefPills(refs []git.Ref, room int, bg lipgloss.Color) (string, int) {
	var sb strings.Builder
	used := 0
	for i, r := range refs {
		name := r.Name
		if runewidth.StringWidth(name) > 12 {
			name = runewidth.Truncate(name, 12, "…")
		}
		var fg, pbg lipgloss.Color
		switch r.Kind {
		case git.RefTag:
			fg, pbg = theme.TintPill(theme.AccentAmber, theme.BgSidebar)
		case git.RefRemote:
			fg, pbg = theme.LG(theme.TextMuted), theme.Wash(theme.BgSidebar, theme.WashHover)
		default:
			fg, pbg = theme.TintPill(theme.AccentBlue, theme.BgSidebar)
		}
		pw := theme.PillWidth(name) + 1
		rest := len(refs) - i - 1
		fold := 0
		if rest > 0 {
			fold = 2 + len(strconv.Itoa(rest)) // "+N "
		}
		if used+pw+fold > room {
			n := len(refs) - i
			plus := "+" + strconv.Itoa(n) + " "
			if used+len(plus) <= room {
				sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(theme.LG(theme.TextDim)).Render(plus))
				used += len(plus)
			}
			break
		}
		sb.WriteString(theme.Pill(name, fg, pbg, bg, false))
		sb.WriteString(lipgloss.NewStyle().Background(bg).Render(" "))
		used += pw
	}
	return sb.String(), used
}

// renderGitGraphCommit renders one GRAPH commit on the quiet grid:
//
//	lead · node · pills · subject …  initials  ⇡  age
//
// The node is the only colour (accent on HEAD); a conventional-commit
// prefix is muted (hidden below 40 cells, where the message needs the
// room); ⇡ marks commits not pushed yet; initials show from 40 cells.
func renderGitGraphCommit(c git.GraphCommit, active, focused bool, w int) string {
	return renderGitGraphCommitMark(c, "", active, focused, w)
}

// renderGitGraphCommitMark is renderGitGraphCommit with a compare mark
// ("1" / "2", BorderFocus) in the mark column in place of ⇡.
func renderGitGraphCommitMark(c git.GraphCommit, mark string, active, focused bool, w int) string {
	bg, _, _ := gitRowColors(active, focused, theme.LG(theme.TextMuted))
	rowStyle := lipgloss.NewStyle().Background(bg)
	fg := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Background(bg).Foreground(c) }

	node, nodeFG, subjFG := theme.GlyphCommit.String(), theme.LG(theme.TextDimmer), theme.LG(theme.TextPrimary)
	switch {
	case c.IsHead:
		node, nodeFG, subjFG = theme.GlyphHead.String(), theme.LG(theme.BorderFocus), theme.LG(theme.TextWhite)
	case c.IsMerge:
		node, nodeFG, subjFG = theme.GlyphMerge.String(), theme.LG(theme.TextMuted), theme.LG(theme.TextSecondary)
	}
	if active && focused {
		subjFG = theme.LG(theme.TextWhite)
	}

	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg))
	sb.WriteString(fg(nodeFG).Render(node))
	sb.WriteString(rowStyle.Render(" "))
	used := 3

	// The right edge: [ AJ] ⇡ age — age right-aligned in 3 cells.
	age := git.CompactAge(c.Committed)
	initials := ""
	if w >= 40 {
		initials = git.Initials(c.Author)
	}
	tail := 1 + 3 // " " + age
	marked := c.Unpushed || mark != ""
	if marked {
		tail += 2 // "⇡ "
	}
	if initials != "" {
		tail += 1 + runewidth.StringWidth(initials)
	}

	const minSubject = 10
	if len(c.Refs) > 0 {
		pills, pw := gitRefPills(c.Refs, w-used-tail-minSubject, bg)
		sb.WriteString(pills)
		used += pw
	}

	subject := c.Subject
	pre := gitTypePrefix(subject)
	if w < 40 && pre > 0 {
		// Narrow: the prefix would leave too little of the message.
		subject, pre = subject[pre:], 0
	}
	if avail := w - used - tail; avail >= 1 {
		if runewidth.StringWidth(subject) > avail {
			subject = runewidth.Truncate(subject, avail, "…")
		}
		if pre > 0 && pre <= len(subject) {
			sb.WriteString(fg(theme.LG(theme.TextMuted)).Render(subject[:pre]))
			sb.WriteString(fg(subjFG).Render(subject[pre:]))
		} else {
			sb.WriteString(fg(subjFG).Render(subject))
		}
		used += runewidth.StringWidth(subject)
	}
	if pad := w - used - tail; pad > 0 {
		sb.WriteString(rowStyle.Render(strings.Repeat(" ", pad)))
		used += pad
	}
	if initials != "" {
		sb.WriteString(rowStyle.Render(" "))
		sb.WriteString(fg(theme.LG(theme.TextDim)).Render(initials))
		used += 1 + runewidth.StringWidth(initials)
	}
	switch {
	case mark != "":
		sb.WriteString(rowStyle.Render(" "))
		sb.WriteString(fg(theme.LG(theme.BorderFocus)).Render(mark))
	case c.Unpushed:
		sb.WriteString(rowStyle.Render(" "))
		sb.WriteString(fg(theme.LG(theme.GitAdded)).Render(theme.GlyphUnpushed.String()))
	}
	sb.WriteString(rowStyle.Render(" "))
	sb.WriteString(fg(theme.LG(theme.TextDim)).Render(fmt.Sprintf("%3s", age)))
	used += 4
	if marked {
		used += 2
	}
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}

// renderGitGroupRow renders a GRAPH date group header: a dim label and
// its commit count on the right; a folded group shows ▸.
func renderGitGroupRow(label string, n int, folded, active, focused bool, w int) string {
	bg := gitSelBg(active, focused)
	rowStyle := lipgloss.NewStyle().Background(bg)
	dim := lipgloss.NewStyle().Background(bg).Foreground(theme.LG(theme.TextDim))
	if active {
		dim = dim.Foreground(theme.LG(theme.TextSecondary))
	}
	mark := "  "
	if folded {
		mark = theme.IconFolderClosed.Unicode + " "
		if theme.CurrentIconMode == theme.IconModeASCII {
			mark = "> "
		}
	}
	count := strconv.Itoa(n)
	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg))
	sb.WriteString(dim.Render(" " + mark + label))
	used := 1 + 1 + 2 + runewidth.StringWidth(label)
	if pad := w - used - len(count) - 1; pad > 0 {
		sb.WriteString(rowStyle.Render(strings.Repeat(" ", pad)))
		sb.WriteString(dim.Render(count))
		sb.WriteString(rowStyle.Render(" "))
		used += pad + len(count) + 1
	}
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}

// renderGitMoreRow renders "Show 15 more ▾" or "Load older commits ▾".
func renderGitMoreRow(older, active, focused bool, w int) string {
	bg := gitSelBg(active, focused)
	rowStyle := lipgloss.NewStyle().Background(bg)
	fgc := theme.LG(theme.TextSecondary)
	if active && focused {
		fgc = theme.LG(theme.TextWhite)
	}
	label := fmt.Sprintf("Show %d more", gitGraphStep)
	if older {
		label = "Load older commits"
	}
	label += " " + theme.GlyphExpand.String()
	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg))
	sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(fgc).Render("   " + label))
	return clampSidebarRow(sb.String(), 1+3+runewidth.StringWidth(label), w, rowStyle)
}

// leftEllipsize keeps the tail of s and prepends "…" when the string is wider
// than max. Useful for paths so the closest directory stays visible.
func leftEllipsize(s string, max int) string {
	if max <= 0 {
		return ""
	}
	w := runewidth.StringWidth(s)
	if w <= max {
		return s
	}
	r := []rune(s)
	target := max - 1
	keep := r
	for runewidth.StringWidth(string(keep)) > target && len(keep) > 0 {
		keep = keep[1:]
	}
	return "…" + string(keep)
}

// gitStatusDot maps a file status to a small colour-coded marker for the
// change rows: a filled ● for tracked changes (amber=modified, green=added,
// red=deleted, blue=renamed, bright-red=conflict) and a hollow ○ for brand-new
// untracked files.
func gitStatusDot(f git.FileStatus) (string, lipgloss.Color) {
	if f.Untracked() {
		return "○", theme.LG(theme.GitAdded)
	}
	primary := byte(' ')
	if len(f.Code) >= 1 && f.Code[0] != ' ' {
		primary = f.Code[0]
	} else if len(f.Code) >= 2 {
		primary = f.Code[1]
	}
	switch primary {
	case 'M':
		return "●", theme.LG(theme.GitModified)
	case 'A':
		return "●", theme.LG(theme.GitAdded)
	case 'D':
		return "●", theme.LG(theme.AccentRedCoral)
	case 'R':
		return "●", theme.LG(theme.AccentBlue)
	case 'C', 'U':
		return "●", theme.LG(theme.DiagError)
	}
	return "·", theme.LG(theme.TextMuted)
}

// padBgToWidth pads with the sidebar bg so empty space stays the panel color.
func padBgToWidth(s string, w int) string {
	width := textWidth(s)
	if width > w {
		// Truncate over-wide rows. A row wider than the sidebar would widen
		// the whole sidebar column on JoinHorizontal and shove the editor
		// right (e.g. a focus-only footer hint or a long branch name).
		// ANSI-aware so we never slice through an SGR escape.
		return ansi.Truncate(s, w, "")
	}
	if pad := w - width; pad > 0 {
		return s + sidebarFill.Render(strings.Repeat(" ", pad))
	}
	return s
}

func (m Model) statusState() statusbar.State {
	lang := m.editor.Lang()
	if lang == "findresults" {
		lang = "Find Results"
	} else if lang == "httpresponse" || lang == "http" {
		lang = map[string]string{"httpresponse": "HTTP Response", "http": "HTTP"}[lang]
	} else if lang != "" {
		lang = strings.ToUpper(lang[:1]) + lang[1:]
	}
	// Project name is the cwd basename — the breadcrumbs row above the
	// editor already carries the active filename + directory, so the
	// status bar only surfaces project-level context here.
	proj := m.projectLabel()
	if proj != "" {
		// projectName() upper-cases for the sidebar header; in the
		// status bar's left zone we want a softer, mixed-case look so
		// it reads as ambient context rather than another label.
		proj = strings.ToLower(proj)
	}
	aiLabel, aiState := m.aiStatus() // Group A
	st := statusbar.State{
		AI:      aiLabel,
		AIState: aiState,
		Path:    m.editor.Path(),
		Project: proj,
		Lang:    lang,
		// nvim's cursor in the file (StateMsg), not the grid cursor: that
		// is the screen row / column (scrolled view, gutter, wrapped lines).
		Line:     max(m.cursorLine, 1),
		Col:      max(m.cursorCol, 1),
		Dirty:    m.editor.IsDirty(),
		Err:      m.err,
		Errors:   m.editor.Errors(),
		Warnings: m.editor.Warnings(),
		Branch:   m.gitBranch.Name,
		Ahead:    m.gitBranch.Ahead,
		Behind:   m.gitBranch.Behind,
		Indent:   m.statusIndent(),
		Encoding: "UTF-8",
		Term:     m.termOpen && len(m.terminalTabs) > 0,
		// The terminal has the keyboard: show its shell and folder.
		TermFocused: m.inTerminal && m.termOpen && len(m.terminalTabs) > 0,
		TermLabel:   m.terminalStatusLabel(),
		Busy:        aiState == statusbar.AIStreaming,
		BusyStep:    m.aiStreamSteps(),
		Debug:       m.dapSessionActive,
		ShowLSP:     m.editor.Path() != "",
		LSP:         m.lspClients(),
		Ext:         append(append(m.extStatusTexts(), m.httpStatusTexts()...), m.aiNESStatusTexts()...), // Group I, HTTP client, AI next edit
	}
	if m.layoutHint != "" { // G-LAY: the layout just opened
		st.Ext = append(st.Ext, "Layout: "+m.layoutHint)
	}
	m.fillTestsStatus(&st) // Group E: ✓ n ✗ n chip (test_explorer.go)
	m.fillServicesStatus(&st)
	m.fillCIStatus(&st)
	m.fillUpdateStatus(&st)            // G-UPD: ⬆ v0.2.0
	st.Agents = m.mcpServeAgentCount() // ⇄ n agents: outside agents on the MCP server
	st.AgentsReady = m.mcpServe != nil // ⇄ ready: it listens, nobody is connected
	// ◉ web on / web connected (web_tui.go)
	if m.webPanel != nil && m.webPanel.up {
		st.WebOn = true
		if m.web != nil {
			st.WebBrowsers = len(m.web.live)
		}
	}
	if m.databases != nil && m.databases.activeID != "" {
		if c, ok := m.databases.connection(m.databases.activeID); ok {
			st.ShowDatabase = true
			st.DatabaseName = dbConnName(c)
			if mk := m.databases.txMarker(c.ID); mk != "" {
				st.DatabaseName += " · " + mk // the transaction state (db_tx.go)
			}
			st.DatabaseProd = c.Environment == "production"
		}
	}
	return st
}

// aiStreamSteps is the AI chat's event count (the busy comet's step).
func (m Model) aiStreamSteps() int {
	if m.ai == nil {
		return 0
	}
	return m.ai.streamSteps
}

// terminalStatusLabel is "zsh · ~/proj" for the active terminal tab.
func (m Model) terminalStatusLabel() string {
	if len(m.terminalTabs) == 0 {
		return ""
	}
	i := min(max(m.terminalActiveTab, 0), len(m.terminalTabs)-1)
	tab := m.terminalTabs[i]
	dir := tab.Cwd
	// Before the shell reports its folder this is nvim's terminal name,
	// term://DIR//PID:CMD; keep DIR.
	if rest, ok := strings.CutPrefix(dir, "term://"); ok {
		if j := strings.Index(rest, "//"); j >= 0 {
			rest = rest[:j]
		}
		dir = rest
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if dir == home {
			dir = "~"
		} else if strings.HasPrefix(dir, home+string(os.PathSeparator)) {
			dir = "~" + dir[len(home):]
		}
	}
	switch {
	case tab.Shell != "" && dir != "":
		return tab.Shell + " · " + dir
	case tab.Shell != "":
		return tab.Shell
	}
	return dir
}

// overlayPreferredActionHintLines paints a 1-row "💡 <Title>  Ctrl+. ▸
// Apply Ctrl+Shift+." chip (frame rows, in place) below the editor cursor,
// or above it near the bottom. It only paints: the keys run the action,
// and a cursor move or another key clears the hint (handleGlobalKey).
func (m Model) overlayPreferredActionHintLines(lines []string) {
	if m.preferredCodeAction == nil {
		return
	}
	// Compose the chip text with live keymap shortcuts so user
	// rebindings stay in sync with what the hint advertises.
	pickerKey := keyForAction(m.keys, keymap.ActionCodeActions, "Ctrl+.")
	applyKey := keyForAction(m.keys, keymap.ActionApplyPreferredCodeAction, "Ctrl+Shift+.")
	titleStyle := theme.FgBg(theme.TextPrimary, theme.BgPanel).Bold(true)
	chipBg := theme.Bg(theme.BgPanel)
	hintFg := theme.FgBg(theme.TextMuted, theme.BgPanel)
	bulb := theme.FgBg(theme.AccentAmber, theme.BgPanel).Bold(true).Render("💡")

	rawTitle := strings.SplitN(strings.TrimSpace(m.preferredCodeAction.Title), "\n", 2)[0]
	if rawTitle == "" {
		rawTitle = "Quick Fix"
	}
	// Truncate the title so the chip stays compact (≤ 60 visible cells).
	if runewidth.StringWidth(rawTitle) > 36 {
		rawTitle = runewidth.Truncate(rawTitle, 36, "…")
	}

	// Build the chip body. Layout: "💡 <Title>  <Ctrl+.>  Apply: <Ctrl+Shift+.>"
	// Picker key is rendered as plain hint text; the apply key is the
	// emphasis the user is being told about, but for visual quietness we
	// keep both in the muted hint colour (same convention as the overflow
	// menu's right-side shortcut column).
	body := chipBg.Render(" ") + bulb + chipBg.Render(" ") +
		titleStyle.Render(rawTitle) + chipBg.Render("  ") +
		hintFg.Render(pickerKey) + chipBg.Render("  ") +
		hintFg.Render("Apply: "+applyKey) + chipBg.Render(" ")
	chipW := textWidth(body)

	// Same editor pane geometry renderBase composed, so the splice
	// column lands inside the editor.
	pane := m.editorPaneLayout()
	edPaneW, editorStart, editorTop := pane.W, pane.X, pane.Top
	editorBottom := m.h - 2 // status bar lives on the last row; bottom-1 is our floor
	if editorBottom <= editorTop {
		return
	}

	cur := m.editor.Cursor()
	cursorRow := editorTop + cur.Line
	if cursorRow < editorTop || cursorRow > editorBottom {
		// Cursor outside visible viewport — nothing sensible to anchor to.
		return
	}

	// Prefer the row below the cursor; fall back to the row above when
	// there's no room below.
	row := cursorRow + 1
	if row > editorBottom {
		row = cursorRow - 1
	}
	if row < editorTop || row > editorBottom {
		return
	}

	// Anchor the chip's left edge a few cells right of the gutter so it
	// reads as "attached to the cursor line" rather than floating in the
	// margin. Right-clip when the chip would overflow the editor pane.
	gutter := m.queryGutterWidth()
	leftCol := editorStart + gutter + 1
	maxLeft := editorStart + edPaneW - chipW - 1
	if leftCol > maxLeft && maxLeft >= editorStart+gutter {
		leftCol = maxLeft
	}
	if leftCol < editorStart+gutter {
		leftCol = editorStart + gutter
	}
	if leftCol+chipW > m.w {
		// Last-ditch: chip wider than viewport — clip on the right.
		chipW = m.w - leftCol
		if chipW <= 0 {
			return
		}
		body = truncRightVisual(body, chipW)
	}

	if row < 0 || row >= len(lines) {
		return
	}
	lines[row] = spliceAt(lines[row], body, leftCol)
}

// keyForAction walks the live keymap and returns the friendly-formatted
// first key bound to `act`. When the action isn't bound (e.g. user removed
// the override) the supplied `fallback` is returned verbatim so the hint
// still reads as a real shortcut.
func keyForAction(km keymap.KeyMap, act keymap.Action, fallback string) string {
	best := ""
	for raw, a := range km.Bindings() {
		if a != act {
			continue
		}
		pretty := prettyHintKey(raw)
		if best == "" || len(pretty) < len(best) {
			best = pretty
		}
	}
	if best == "" {
		return fallback
	}
	return best
}

// prettyHintKey converts a Bubble Tea key string ("ctrl+shift+.") into a
// presentation-friendly form ("Ctrl+Shift+.") matching the convention
// used elsewhere in the chrome (overflow menu, palette hints).
func prettyHintKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "+")
	for i, p := range parts {
		switch p {
		case "ctrl":
			parts[i] = "Ctrl"
		case "shift":
			parts[i] = "Shift"
		case "alt":
			parts[i] = "Alt"
		case "cmd", "meta", "super":
			parts[i] = "Cmd"
		case "enter":
			parts[i] = "Enter"
		}
	}
	return strings.Join(parts, "+")
}

// reviewLayer floats the AI review card (ai_review.go) over base.
func (m Model) reviewLayer(base string) string {
	if !m.reviewOpen || m.ai == nil || m.ai.review == nil {
		return base
	}
	c := m.ai.review.card
	o := m.overlayOpts()
	o.At = true
	o.X, o.Y = c.Origin()
	return modalOverlay(base, c.Box(), m.w, m.h, &o)
}
