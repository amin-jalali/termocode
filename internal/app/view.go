package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"termocode/internal/activity"
	"termocode/internal/explorer"
	"termocode/internal/git"
	"termocode/internal/keymap"
	"termocode/internal/statusbar"
	"termocode/internal/theme"
)

func (m Model) View() string {
	if m.w == 0 || m.h == 0 {
		return "termocode loading..."
	}

	// All pickers and prompts share the same modalOverlay recipe so
	// pseudo-transparency reads consistently across the UI. Other
	// overlays (preview / search / confirm) are full-screen and keep
	// their own rendering.
	if m.settingsModalOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		return modalOverlay(base, m.settingsModal.Box(), m.w, m.h, nil)
	}
	if m.pickerOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		return modalOverlay(base, m.picker.Box(), m.w, m.h, nil)
	}
	if m.recentsOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		return modalOverlay(base, m.recents.Box(), m.w, m.h, nil)
	}
	if m.previewOpen {
		return m.preview.View()
	}
	if m.searchOpen {
		return m.search.View()
	}
	if m.promptOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		return modalOverlay(base, m.prompt.Box(), m.w, m.h, nil)
	}
	if m.confirmOpen {
		base := m.renderBase()
		base = m.overlayToastsInEditor(base)
		return modalOverlay(base, m.confirm.Box(), m.w, m.h, nil)
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
	return base
}

// glassyMenuOverlay places the right-click menu over base and runs a
// per-cell glass blend over the menu's panel cells (the body/selection
// areas) so the menu feels like a translucent floating layer rather than
// a solid drop. Border + accent cells stay crisp.
func (m Model) glassyMenuOverlay(base string) string {
	const alpha = 1.0 // fully opaque body — no editor bg bleeds through the menu
	tint := rgbColor{0x00, 0x01, 0x03} // matches modalTint
	panelBg := rgbColor{0x26, 0x26, 0x26}

	mx, my, mw, mh := m.menu.Bounds()
	if mw <= 0 || mh <= 0 {
		recordError(fmt.Sprintf("[glass.menu] FALLBACK to plain Overlay — bounds=%d,%d,%d,%d", mx, my, mw, mh))
		return m.menu.Overlay(base)
	}

	boxStr := m.menu.View()
	boxLines := strings.Split(strings.TrimRight(boxStr, "\n"), "\n")
	baseLines := strings.Split(base, "\n")

	totalCells, blendedCells, skipNoBg, skipWrongBg, skipOutOfRange := 0, 0, 0, 0, 0
	var sampleSGR, sampleBaseLine string
	var minBaseCells, maxBaseCells int = 1 << 30, 0
	for i, line := range boxLines {
		row := my + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseCells := parseANSIRow(baseLines[row])
		boxCells := parseANSIRow(line)
		bcLen := len(baseCells)
		if bcLen < minBaseCells {
			minBaseCells = bcLen
			if bcLen < mx+len(boxCells) && sampleBaseLine == "" && len(baseLines[row]) > 0 {
				snip := baseLines[row]
				if len(snip) > 200 {
					snip = snip[:200]
				}
				sampleBaseLine = snip
			}
		}
		if bcLen > maxBaseCells {
			maxBaseCells = bcLen
		}
		for j := range boxCells {
			totalCells++
			ci := mx + j
			if ci < 0 || ci >= len(baseCells) {
				skipOutOfRange++
				continue
			}
			cellBg, ok := extractBgRGB(boxCells[j].sgr)
			if !ok {
				skipNoBg++
				if sampleSGR == "" {
					sampleSGR = boxCells[j].sgr
				}
				continue
			}
			if cellBg != panelBg {
				skipWrongBg++
				continue
			}
			_ = cellBg
			baseBg, baseOK := extractBgRGB(baseCells[ci].sgr)
			if !baseOK {
				continue
			}
			blendedBg := blendRGB(baseBg, tint, alpha)
			if boxCells[j].glyph != " " && boxCells[j].glyph != "" {
				boxCells[j].sgr = replaceBgRGB(boxCells[j].sgr, blendedBg)
				continue
			}
			// Empty body cell: show the underlying ASCII glyph dimmed so
			// editor text bleeds through. Non-ASCII glyphs masked with a
			// space (they don't dim reliably).
			if isPrintableASCII(baseCells[ci].glyph) {
				baseFg, fgOK := extractFgRGB(baseCells[ci].sgr)
				if !fgOK {
					baseFg = textPrimaryFallback
				}
				boxCells[j].glyph = baseCells[ci].glyph
				boxCells[j].sgr = makeFgBgSGR(dimRGB(baseFg, 0.20), blendedBg)
			} else {
				boxCells[j].glyph = " "
				boxCells[j].sgr = makeFgBgSGR(rgbColor{}, blendedBg)
			}
			blendedCells++
		}
		baseLines[row] = spliceAt(baseLines[row], renderCells(boxCells), mx)
	}
	logMenuGlass(mx, my, mw, mh, totalCells, blendedCells, skipNoBg, skipWrongBg, sampleSGR)
	return strings.Join(baseLines, "\n")
}

var lastMenuGlassKey string

func logMenuGlass(mx, my, mw, mh, total, blended, noBg, wrongBg int, sample string) {
	key := fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d:%d", mx, my, mw, mh, total, blended, noBg, wrongBg)
	if key == lastMenuGlassKey {
		return
	}
	lastMenuGlassKey = key
	recordError(fmt.Sprintf("[glass.menu] mx=%d my=%d mw=%d mh=%d total=%d blended=%d skipNoBg=%d skipWrongBg=%d sampleSGR=%q",
		mx, my, mw, mh, total, blended, noBg, wrongBg, sample))
}

// overlayToastsInEditor positions toasts inside the code-text area:
//   - left edge:  cs + 1   (cs = code-start = editorPaneStart + actualGutter)
//   - right edge: cs + 0.8*(ce - cs)   (cs..ce = code-text width)
//
// Gutter width is queried from nvim (getwininfo().textoff) — the real
// total of numberwidth + signcolumn + foldcolumn — instead of being
// hardcoded; that way the toast alignment tracks whatever nvim is
// actually painting (signs come and go with diagnostics).
func (m Model) overlayToastsInEditor(base string) string {
	// Hot-path early exit: when no toasts are queued, there's nothing to
	// overlay — and we'd otherwise pay a synchronous Lua RPC for
	// queryGutterWidth + a strings.Split of the whole frame on every
	// View() call (including every mouse drag event). Skipping when empty
	// is what makes drag-select feel snappy.
	if m.toast.Empty() {
		return base
	}
	const codePadding = 1
	edPaneW := m.w - activity.Width - editorScrollbarWidth - m.actionsColumnWidth()
	if m.showExp {
		edPaneW -= m.explorerWidth
	}
	editorStart := m.w - edPaneW - m.actionsColumnWidth()
	gutter := m.queryGutterWidth() // real value from nvim, falls back to 7
	cs := editorStart + gutter
	ce := m.w - editorScrollbarWidth
	codeWidth := ce - cs
	toastLeft := cs + codePadding
	toastRight := cs + (codeWidth*80)/100
	toastW := toastRight - toastLeft
	if toastW < 16 {
		return base
	}
	rows := m.toast.View(toastW, m.h)
	if len(rows) == 0 {
		return base
	}
	logToastGeometry(m.w, m.h, edPaneW, m.showExp, m.explorerWidth, editorStart, gutter, cs, ce, toastLeft, toastRight, toastW)
	// Glassify each toast row using the underlying base bg per cell so
	// the toast reads as a translucent strip (same recipe as the modal
	// body). Compute the row indices the way overlayToastsAt does.
	baseLines := strings.Split(base, "\n")
	const bottomMargin = 4
	startRow := m.h - 1 - bottomMargin - len(rows)
	if startRow < 0 {
		startRow = 0
	}
	for i := range rows {
		bi := startRow + i
		if bi < 0 || bi >= len(baseLines) {
			continue
		}
		rows[i] = glassifyToastRow(rows[i], baseLines[bi], toastLeft)
	}
	return overlayToastsAt(base, rows, toastLeft, m.h)
}

// glassifyToastRow gives the toast a real glass look by blending each
// body cell's bg with the underlying base bg AND showing the underlying
// ASCII glyph dimmed where the toast cell is empty. Without the glyph
// passthrough the toast over a uniform editor surface produces uniform
// blended cells — which reads as a solid card, not glass.
//
// The severity accent strip (bg != toast panel bg) and any non-space
// content in the toast (label, title, detail text) pass through with
// just the bg swapped, so the toast's own text stays crisp.
func glassifyToastRow(toastRow, baseRow string, leftCol int) string {
	const alpha = 0.99
	tint := rgbColor{0x00, 0x01, 0x03} // matches modalTint
	toastPanelBg := rgbColor{0x26, 0x26, 0x26}

	tcells := parseANSIRow(toastRow)
	bcells := parseANSIRow(baseRow)
	for i := range tcells {
		bi := leftCol + i
		if bi < 0 || bi >= len(bcells) {
			continue
		}
		toastBg, ok := extractBgRGB(tcells[i].sgr)
		if !ok || toastBg != toastPanelBg {
			continue
		}
		baseBg, baseOK := extractBgRGB(bcells[bi].sgr)
		if !baseOK {
			continue
		}
		blendedBg := blendRGB(baseBg, tint, alpha)
		// Non-space toast cells (text glyphs) keep their glyph + fg,
		// only their bg swaps to the blended one.
		if tcells[i].glyph != " " && tcells[i].glyph != "" {
			tcells[i].sgr = replaceBgRGB(tcells[i].sgr, blendedBg)
			continue
		}
		// Empty body cell: show the underlying ASCII glyph dimmed so
		// the editor's text bleeds through. Non-ASCII / wide / emoji
		// underlying chars get masked with a space (they don't dim
		// reliably and would pop bright).
		if isPrintableASCII(bcells[bi].glyph) {
			baseFg, fgOK := extractFgRGB(bcells[bi].sgr)
			if !fgOK {
				baseFg = textPrimaryFallback
			}
			tcells[i].glyph = bcells[bi].glyph
			tcells[i].sgr = makeFgBgSGR(dimRGB(baseFg, 0.45), blendedBg)
		} else {
			tcells[i].glyph = " "
			tcells[i].sgr = makeFgBgSGR(rgbColor{}, blendedBg)
		}
	}
	return renderCells(tcells)
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

var lastToastGeomKey string

func logToastGeometry(mw, mh, edPaneW int, showExp bool, explorerW, editorStart, gutter, cs, ce, tl, tr, tw int) {
	key := fmt.Sprintf("%d:%d:%d:%v:%d:%d:%d:%d:%d:%d:%d:%d",
		mw, mh, edPaneW, showExp, explorerW, editorStart, gutter, cs, ce, tl, tr, tw)
	if key == lastToastGeomKey {
		return
	}
	lastToastGeomKey = key
	recordError(fmt.Sprintf(
		"[toast.geom] m.w=%d m.h=%d edPaneW=%d showExp=%v explorerW=%d editorStart=%d gutter=%d cs=%d ce=%d toastLeft=%d toastRight=%d toastW=%d",
		mw, mh, edPaneW, showExp, explorerW, editorStart, gutter, cs, ce, tl, tr, tw))
}

// overlayToastsAt splices toast rows into the base above the status bar,
// inserting each toast row at column `leftCol` (cell-aligned). The base
// content to the LEFT of leftCol is preserved; the base content from
// leftCol+toastW onward is preserved too — the toast lives in the middle.
func overlayToastsAt(base string, toasts []string, leftCol, height int) string {
	if len(toasts) == 0 || height <= 0 {
		return base
	}
	lines := strings.Split(base, "\n")
	const bottomMargin = 4
	startRow := height - 1 - bottomMargin - len(toasts)
	if startRow < 0 {
		startRow = 0
	}
	for i, t := range toasts {
		row := startRow + i
		if row >= len(lines) {
			break
		}
		baseW := lipgloss.Width(lines[row])
		toastW := lipgloss.Width(t)
		spliced := spliceAt(lines[row], t, leftCol)
		splicedW := lipgloss.Width(spliced)
		logSpliceRow(i, row, leftCol, baseW, toastW, splicedW, len(lines[row]), len(t), len(spliced))
		lines[row] = spliced
	}
	return strings.Join(lines, "\n")
}

var lastSpliceLogKey string

func logSpliceRow(toastIdx, row, leftCol, baseW, toastW, splicedW, baseBytes, toastBytes, splicedBytes int) {
	key := fmt.Sprintf("%d:%d:%d:%d:%d:%d", toastIdx, leftCol, baseW, toastW, splicedW, baseBytes)
	if key == lastSpliceLogKey {
		return
	}
	lastSpliceLogKey = key
	recordError(fmt.Sprintf(
		"[toast.splice] toastIdx=%d row=%d leftCol=%d baseW=%d toastW=%d splicedW=%d baseBytes=%d toastBytes=%d splicedBytes=%d",
		toastIdx, row, leftCol, baseW, toastW, splicedW, baseBytes, toastBytes, splicedBytes))
}

// spliceAt overlays `mid` onto `base` starting at the leftCol-th visible
// cell, preserving base on both sides. lipgloss.Width(mid) cells of base
// are dropped under the overlay; SGR state for the right-side base is
// rebuilt from the most recent SGR seen before the cut so colour bleed
// doesn't carry over.
func spliceAt(base, mid string, leftCol int) string {
	rw := lipgloss.Width(mid)
	left := truncRightVisual(base, leftCol)
	right := dropLeftVisual(base, leftCol+rw)
	return left + mid + right
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
	var out []byte
	var lastSGR []byte
	visible := 0
	emitted := false
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// Properly skip ESC + '[', then params, then a single final
			// byte in the CSI 0x40..0x7E range. '[' is NOT a terminator.
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			sgr := s[i:j]
			i = j
			if visible >= cells {
				if !emitted && len(lastSGR) > 0 {
					out = append(out, lastSGR...)
					emitted = true
				}
				out = append(out, sgr...)
			} else {
				lastSGR = []byte(sgr)
			}
			continue
		}
		if visible >= cells {
			if !emitted && len(lastSGR) > 0 {
				out = append(out, lastSGR...)
				emitted = true
			}
			out = append(out, c)
			i++
			continue
		}
		if c < 0x80 || (c&0xC0) != 0x80 {
			visible++
		}
		i++
	}
	return string(out)
}

// truncRightVisual keeps only the leftmost `cells` visible cells from s,
// preserving ANSI escapes that apply to the kept range.
func truncRightVisual(s string, cells int) string {
	if cells <= 0 {
		return ""
	}
	var out []byte
	visible := 0
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// CSI escape: skip ESC + '[' first, then read params
			// (0x30..0x3F + 0x20..0x2F) until a final byte (0x40..0x7E).
			// Treating '[' as a possible terminator is wrong — it's the
			// CSI introducer, not the end byte.
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++ // consume final byte
			}
			out = append(out, s[i:j]...)
			i = j
			continue
		}
		out = append(out, c)
		if c < 0x80 || (c&0xC0) != 0x80 {
			visible++
			if visible >= cells {
				return string(out)
			}
		}
		i++
	}
	return string(out)
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
	bodyH := m.h - 1
	if bodyH < 1 {
		bodyH = 1
	}

	var editorRegion string
	// chromeRows accounts for everything that lives ABOVE the editor content
	// inside the editor pane. Stack order, top-to-bottom:
	//   replacebar (optional, 2 rows)
	//   tabs (always, +1)
	//   breadcrumbs (optional, +1)
	//   sticky-scroll (optional, +1)
	//   editor content (consumes the remainder)
	//
	// The find bar no longer reserves a chrome row — it's now a compact
	// floating panel that gets spliced onto the top-right of the editor
	// content area (see overlayFindBarInEditor below).
	chromeRows := m.tabs.Height() // tabs (2 rows: accent strip + body)
	if m.replaceOpen {
		chromeRows += 2 // replacebar renders two rows
	}
	// Breadcrumbs row now carries the function signature inline (with syntax
	// colours) instead of using a separate sticky-scroll row — saves one
	// chrome row.
	hasBreadcrumbs := len(m.breadcrumbs) > 0 || m.stickyContext != ""
	if hasBreadcrumbs {
		chromeRows++
	}
	editorH := bodyH - chromeRows
	if editorH < 1 {
		editorH = 1
	}
	// Editor pane width = total - activity bar - sidebar (if shown) -
	// scrollbar - right-side actions column (if pinned & open).
	actionsColW := m.actionsColumnWidth()
	edPaneW := m.w - activity.Width - editorScrollbarWidth - actionsColW
	if m.showExp {
		edPaneW -= m.explorerWidth
	}
	if edPaneW < 1 {
		edPaneW = 1
	}

	var editorContent string
	if m.editor.Path() == "" && len(m.bufs) <= 1 && !m.termOpen {
		editorContent = renderWelcome(edPaneW, editorH, m.welcomeFocus)
	} else {
		editorContent = m.editor.View(m.focus == FocusEditor)
		// When the integrated terminal panel is open, applyLayout shrinks
		// nvim's render area by 1 row so we can splice our own tab-bar
		// row between nvim's editor split and nvim's terminal split.
		// Splice at the boundary so the output is exactly editorH rows
		// tall.
		if m.termOpen {
			editorContent = m.spliceTerminalTabBar(editorContent, edPaneW, editorH)
		}
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
	editorContent = clipRowsToHeight(editorContent, editorH, edPaneW, theme.Bg(theme.BgEditor))

	// Build the editor stack, then horizontally append the scrollbar so the
	// scrollbar spans the full chrome height (tabs/breadcrumbs/sticky/editor).
	rows := []string{}
	if m.replaceOpen {
		rows = append(rows, m.replace.View())
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
		tabsRow := m.tabs.View()
		if pad := edPaneW - lipgloss.Width(tabsRow); pad > 0 {
			tabsRow += theme.Bg(theme.BgTitleBar).Render(strings.Repeat(" ", pad))
		}
		rows = append(rows, tabsRow)
		if hasBreadcrumbs {
			rows = append(rows, m.renderBreadcrumbs(edPaneW))
		}
	}
	rows = append(rows, editorContent)
	stack := lipgloss.JoinVertical(lipgloss.Left, rows...)

	// Scrollbar is disabled (editorScrollbarWidth = 0) for scroll perf.
	// JoinHorizontal across the editor stack is the dominant cost during
	// fast scroll bursts — re-enable when we have a cached/incremental
	// scrollbar implementation.
	editorRegion = stack
	if editorScrollbarWidth > 0 {
		scrollbar := m.renderEditorScrollbar(bodyH)
		editorRegion = lipgloss.JoinHorizontal(lipgloss.Top, stack, scrollbar)
	}

	// Markdown live preview now lives as a regular nvim split window with
	// its own tab — no extra rendering needed here.

	if m.zenMode {
		// Zen: nothing but the editor stack stretched to full width. We
		// already excluded tabs/breadcrumbs from `stack` above, so this
		// is just the editor content with no chrome at all.
		return m.normalizeFrame(editorRegion)
	}

	// The status bar now lives ONLY under the editor column — the
	// activity bar + sidebar keep their own backgrounds running all the
	// way to the bottom edge. To make that work the status bar is
	// rendered at edPaneW (not m.w), and the activity / sidebar columns
	// are now full-screen tall (m.h) instead of bodyH.
	m.status.SetWidth(edPaneW)
	editorCol := lipgloss.JoinVertical(lipgloss.Left, editorRegion, m.status.View(m.statusState()))

	activityCol := m.activity.View()

	// Right-side Actions panel column. Only rendered when pinned & open;
	// height matches the editor column (editor body + status bar = m.h)
	// so the JoinHorizontal aligns cleanly with the rest of the chrome.
	var actionsCol string
	if m.actionsPanelVisible() {
		actionsCol = m.renderActionsPanel(actionsColW, m.h)
	}

	var middle string
	cols := []string{activityCol}
	if m.showExp {
		cols = append(cols, m.renderSidebar(m.h))
	}
	cols = append(cols, editorCol)
	if actionsCol != "" {
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
		cols[i] = padColumnRowsToHeight(c, m.h, theme.Bg(theme.BgEditor))
	}
	middle = lipgloss.JoinHorizontal(lipgloss.Top, cols...)

	if m.findOpen {
		middle = m.overlayFindBarInEditor(middle)
	}

	if m.actionsLauncherVisible() {
		middle = m.overlayActionsLauncher(middle)
	}

	// Overflow (⋮) glyph at the right edge of the tab bar's body row.
	// Always rendered (even when the dropdown is closed) so the chevron
	// is discoverable. Skipped in zen mode (no chrome at all).
	if !m.zenMode {
		middle = m.overlayOverflowMenuGlyph(middle)
	}

	// Inline 💡 preferred-action hint, anchored to the editor cursor.
	// Skipped while modal overlays / pickers are open (View() takes a
	// different branch above) and in zen mode (no chrome / hint clutter).
	if !m.zenMode && m.preferredCodeAction != nil {
		middle = m.overlayPreferredActionHint(middle)
	}

	return m.normalizeFrame(middle)
}

// overlayActionsLauncher splices the bottom-right launcher chip onto the
// rendered base, one row above the status bar. The base row underneath is
// preserved everywhere except the chip's exact column range.
func (m Model) overlayActionsLauncher(base string) string {
	x1, _, y, ok := m.actionsLauncherRect()
	if !ok {
		return base
	}
	chip := renderActionsLauncher()
	if chip == "" {
		return base
	}
	lines := strings.Split(base, "\n")
	if y < 0 || y >= len(lines) {
		return base
	}
	lines[y] = spliceAt(lines[y], chip, x1)
	return strings.Join(lines, "\n")
}

// glassifyFindBarRow gives the find panel a real glass look by blending
// each panel-bg cell with the underlying base bg AND showing the underlying
// ASCII glyph dimmed where the panel cell is empty. Same recipe as
// glassifyToastRow / glassyMenuOverlay; only the panel bg differs.
//
// The drop-shadow row uses ShadowBg (not PanelBg) and is left untouched —
// stays solid so it still reads as a lifted-card shadow.
func glassifyFindBarRow(panelRow, baseRow string, leftCol int) string {
	const alpha = 0.95
	tint := rgbColor{0x00, 0x01, 0x03} // matches modalTint
	findPanelBg := rgbColor{0x20, 0x20, 0x22}

	pcells := parseANSIRow(panelRow)
	bcells := parseANSIRow(baseRow)
	for i := range pcells {
		bi := leftCol + i
		if bi < 0 || bi >= len(bcells) {
			continue
		}
		cellBg, ok := extractBgRGB(pcells[i].sgr)
		if !ok || cellBg != findPanelBg {
			continue
		}
		baseBg, baseOK := extractBgRGB(bcells[bi].sgr)
		if !baseOK {
			continue
		}
		blendedBg := blendRGB(baseBg, tint, alpha)
		if pcells[i].glyph != " " && pcells[i].glyph != "" {
			pcells[i].sgr = replaceBgRGB(pcells[i].sgr, blendedBg)
			continue
		}
		if isPrintableASCII(bcells[bi].glyph) {
			baseFg, fgOK := extractFgRGB(bcells[bi].sgr)
			if !fgOK {
				baseFg = textPrimaryFallback
			}
			pcells[i].glyph = bcells[bi].glyph
			pcells[i].sgr = makeFgBgSGR(dimRGB(baseFg, 0.45), blendedBg)
		} else {
			pcells[i].glyph = " "
			pcells[i].sgr = makeFgBgSGR(rgbColor{}, blendedBg)
		}
	}
	return renderCells(pcells)
}

// overlayFindBarInEditor splices the compact floating find panel onto the
// top-right of the editor pane, leaving the editor content under it intact
// (the chrome above — tabs and breadcrumbs — is NOT covered). Modeled on
// VS Code's find widget: a small dark panel that hovers over the code.
//
// The panel sits 1 cell down from the editor content's top row and is
// right-aligned with a 2-cell margin from the editor pane's right edge.
func (m Model) overlayFindBarInEditor(base string) string {
	pw := m.find.PanelWidth()
	ph := m.find.PanelHeight()
	if pw <= 0 || ph <= 0 {
		return base
	}

	// Recompute the editor pane's geometry — same recipe as renderBase so
	// the splice column is exactly aligned with what got composed.
	actionsColW := m.actionsColumnWidth()
	edPaneW := m.w - activity.Width - editorScrollbarWidth - actionsColW
	if m.showExp {
		edPaneW -= m.explorerWidth
	}
	if edPaneW < 1 {
		edPaneW = 1
	}
	editorStart := m.w - edPaneW - actionsColW

	// Top of the editor content area: replacebar (if open) + tabs +
	// breadcrumbs (if any). We want the find panel to overlay the editor
	// content (NOT the chrome above it).
	topRow := 0
	if m.replaceOpen {
		topRow += 2
	}
	topRow += m.tabs.Height()
	if len(m.breadcrumbs) > 0 || m.stickyContext != "" {
		topRow++
	}
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
		return base
	}
	panelLines := strings.Split(panel, "\n")
	baseLines := strings.Split(base, "\n")
	for i, pline := range panelLines {
		row := topRow + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		glassed := glassifyFindBarRow(pline, baseLines[row], leftCol)
		baseLines[row] = spliceAt(baseLines[row], glassed, leftCol)
	}
	return strings.Join(baseLines, "\n")
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
	filler := theme.Bg(theme.BgEditor)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w < m.w {
			line += filler.Render(strings.Repeat(" ", m.w-w))
		}
		lines[i] = restyleTrailingUnstyled(line, filler)
	}
	for len(lines) < m.h {
		lines = append(lines, filler.Render(strings.Repeat(" ", m.w)))
	}
	if len(lines) > m.h {
		// Trim from the TOP, not bottom — the status bar lives on the
		// last row and is the most important UI element to preserve.
		lines = lines[len(lines)-m.h:]
	}
	return strings.Join(lines, "\n")
}

// padColumnRowsToHeight forces a column (assumed all rows the same width)
// to exactly `h` rows by appending styled `filler` rows at the bottom.
// Tall columns are returned unchanged — clipping is done at the source
// (e.g. clipRowsToHeight on editorContent) where we know which rows are
// flexible body vs. fixed chrome (status bar, header, etc.).
//
// This is the LEFT-SIDE / BETWEEN-COLUMN counterpart to normalizeFrame:
// it runs BEFORE lipgloss.JoinHorizontal so the join never has to pad
// an unequal-height seam (lipgloss does that with naked ASCII spaces,
// which leak the terminal's default bg as a stripe).
func padColumnRowsToHeight(col string, h int, filler lipgloss.Style) string {
	if h <= 0 {
		return col
	}
	rows := strings.Split(col, "\n")
	if len(rows) >= h {
		// Don't trim from here — chrome/status-bar layout matters.
		return col
	}
	// Width inferred from row 0; fallback to 1-cell pad if empty.
	w := 0
	if len(rows) > 0 {
		w = lipgloss.Width(rows[0])
	}
	if w <= 0 {
		w = 1
	}
	pad := filler.Render(strings.Repeat(" ", w))
	for len(rows) < h {
		rows = append(rows, pad)
	}
	return strings.Join(rows, "\n")
}

// clipRowsToHeight returns the first `h` rows of `s` — the rest are
// dropped — and pads short outputs up to `h` rows of `w`-wide styled
// spaces so callers can rely on the result being EXACTLY `h` rows tall
// and `w` cells wide. Trim is from the BOTTOM (last rows dropped) so
// content rendered top-down (banners, headers, action grid…) keeps its
// most-prominent rows.
//
// Each individual row is also right-padded with styled `filler` spaces
// when it falls short of `w` cells. This catches the transitional case
// where the editor pane's grid (driven asynchronously by nvim) is briefly
// narrower than the allocated edPaneW: without this width-pad, the
// downstream lipgloss.JoinVertical pads the short rows with naked ASCII
// spaces, which then reach the right edge of the screen and leak the
// terminal's default bg as a vertical strip on the editor's right side.
func clipRowsToHeight(s string, h, w int, filler lipgloss.Style) string {
	if h <= 0 {
		return ""
	}
	rows := strings.Split(s, "\n")
	if len(rows) > h {
		rows = rows[:h]
	}
	if w > 0 {
		// Right-pad each existing row to `w` cells with styled filler.
		for i, row := range rows {
			rw := lipgloss.Width(row)
			if rw < w {
				rows[i] = row + filler.Render(strings.Repeat(" ", w-rw))
			}
		}
		pad := filler.Render(strings.Repeat(" ", w))
		for len(rows) < h {
			rows = append(rows, pad)
		}
	}
	return strings.Join(rows, "\n")
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

// renderBreadcrumbs renders one row of "seg › seg › seg", clipped from the
// LEFT (i.e. drop earlier segments first) when too wide so the closest
// symbol stays visible. Background uses BgHover (slightly lighter than
// BgEditor) so the breadcrumb strip reads as separate from the editor body
// without needing a divider line.
func (m Model) renderBreadcrumbs(width int) string {
	if width <= 0 {
		return ""
	}
	bg := theme.Bg(theme.BgHover)
	seg := theme.FgBg(theme.BorderDefault, theme.BgHover).Italic(true).Faint(true)
	sep := theme.FgBg(theme.BorderDefault, theme.BgHover).Faint(true)

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
			b.WriteString(seg.Render(s))
		}
		if signature != "" {
			if len(segs) > 0 {
				b.WriteString(sep.Render(" › "))
			}
			b.WriteString(renderSignature(signature))
		}
		out := b.String()
		used := lipgloss.Width(out)
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
// keyword (func/class/def/…) in SyntaxKeyword bold, function name in
// SyntaxFunction, everything else in TextPrimary. It's not a real parser
// — just enough tokenisation to make the breadcrumb feel like code. The
// background is BgHover to match the breadcrumb strip.
func renderSignature(sig string) string {
	// Trim trailing language brackets / colons that aren't part of the
	// useful signature: `func foo() {` → `func foo()`.
	sig = strings.TrimRight(strings.TrimSpace(sig), "{:")
	sig = strings.TrimSpace(sig)
	// Faint(true) emits SGR 2 — most terminals render that with a thinner
	// weight, approximating a smaller font without actually changing the
	// per-cell font size (which terminals don't support).
	kwStyle := theme.FgBg(theme.SyntaxKeyword, theme.BgHover).Faint(true)
	nameStyle := theme.FgBg(theme.SyntaxFunction, theme.BgHover).Faint(true)
	restStyle := theme.FgBg(theme.TextPrimary, theme.BgHover).Faint(true)
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

// renderStickyScroll renders the 1-row "currently inside <signature>" strip.
// Uses BgHover so it visually sits ABOVE the editor body rather than blending
// into it. Italic + TextSecondary signals "this is metadata, not source".
func (m Model) renderStickyScroll(width int) string {
	if width <= 0 {
		return ""
	}
	bg := theme.Bg(theme.BgHover)
	style := theme.FgBg(theme.TextSecondary, theme.BgHover).Italic(true)
	text := strings.TrimRight(m.stickyContext, " \t")
	// Reserve 1 col left pad for visual breathing room.
	avail := width - 1
	if avail < 1 {
		return bg.Render(strings.Repeat(" ", width))
	}
	if runewidth.StringWidth(text) > avail {
		text = runewidth.Truncate(text, avail, "…")
	}
	out := bg.Render(" ") + style.Render(text)
	pad := width - lipgloss.Width(out)
	if pad > 0 {
		out += bg.Render(strings.Repeat(" ", pad))
	}
	return out
}

// renderEditorScrollbar returns a height-tall, 1-column overview ruler. It
// renders in three layers (lowest priority first):
//
//   1. Track: a dim │ on BgEditor for every row.
//   2. Git markers (TODO v1: stubbed — git change line numbers aren't yet
//      threaded through to the editor model).
//   3. Diagnostic markers (TODO v1: stubbed — only counts are exposed today;
//      per-line locations would require a separate Lua fetch).
//   4. Thumb: a bright █ block sized proportionally to viewport / total
//      lines, positioned by m.editor.Cursor().Line / m.editor.LineCount().
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

func (m Model) renderSidebar(h int) string {
	contentW := m.explorerWidth - 1 // 1 col reserved for the right-edge divider
	var content string
	switch m.activity.Active() {
	case activity.ViewFiles:
		// Explorer renders its own header + footer, so we use its output
		// directly (no sidebarHeader join). Size is already set to (contentW,
		// m.h) in update.go.
		ex := m.explorer
		ex.SetMeta(projectName(), m.gitBranch.Name, m.gitIsRepo, totalVisibleFiles(&ex))
		content = ex.View(
			m.focus == FocusExplorer,
			m.editor.Path(),
			m.editor.IsDirty(),
			m.gitStatusMap(),
		)
	case activity.ViewSearch:
		header := sidebarHeader(contentW, "SEARCH", "")
		body := placeholderSidebar(contentW, h-1, "", "Press F8 for\nFind in Files.\n\nUse Ctrl+F for\nin-file find.")
		content = lipgloss.JoinVertical(lipgloss.Left, header, body)
	case activity.ViewGit:
		content = m.renderGitSidebar(contentW, h)
	case activity.ViewRun:
		header := sidebarHeader(contentW, "RUN", "")
		body := placeholderSidebar(contentW, h-1, "", "Run / Debug — coming soon.\n\nPress F5 to run a build.")
		content = lipgloss.JoinVertical(lipgloss.Left, header, body)
	default:
		return ""
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, content, sidebarDivider(h))
}

func sidebarDivider(h int) string {
	style := lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#444444"))
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

func projectName() string {
	cwd, err := os.Getwd()
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
	leftW := lipgloss.Width(left)

	var right string
	if project != "" {
		right = mutedStyle.Render(strings.ToUpper(project)) + fill.Render(" ") + mutedStyle.Render("⋯") + fill.Render(" ")
	} else {
		right = mutedStyle.Render("⋯") + fill.Render(" ")
	}
	rightW := lipgloss.Width(right)

	gap := w - leftW - rightW
	if gap < 1 {
		// Drop the project name and keep only the ⋯ if it doesn't fit.
		right = mutedStyle.Render("⋯") + fill.Render(" ")
		rightW = lipgloss.Width(right)
		gap = w - leftW - rightW
		if gap < 0 {
			gap = 0
		}
	}
	return left + fill.Render(strings.Repeat(" ", gap)) + right
}

var (
	sidebarBg      = lipgloss.Color("#303030")
	sidebarFill    = lipgloss.NewStyle().Background(sidebarBg)
	gitTitleStyle  = lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#cccccc")).Bold(true)
	gitBranchStyle = lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#858585"))
	gitNameStyle   = lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#cccccc"))
	gitDirStyle    = lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#858585"))
	gitCursorBlur  = lipgloss.NewStyle().Background(lipgloss.Color("#2a2d2e")).Foreground(lipgloss.Color("#d0d0d0"))
	gitCursorFocus = lipgloss.NewStyle().Background(lipgloss.Color("#0087d7")).Foreground(lipgloss.Color("#ffffff"))
)

func (m Model) renderGitSidebar(w, h int) string {
	if !m.gitIsRepo {
		return placeholderSidebar(w, h, "Source Control", "Not a git repository.\n\nRun `git init` in a\nterminal to enable.")
	}

	focused := m.focus == FocusExplorer

	// ── Fixed header (mirrors the file-explorer header): letter-spaced
	// title, a hairline underline, then a branch + tree·flat sub-row, then a
	// blank spacer. gitPanelTopOffset() must stay in sync (4 rows).
	var header []string
	header = append(header, renderGitTitleRow(w))
	header = append(header, renderGitHairline(w))
	header = append(header, renderGitSubheader(m.gitBranch, m.gitViewTree, w))
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
			lines[hy] = gitHoverLineBg(lines[hy])
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

// gitHoverLineBg re-tints the panel-background cells of a rendered row to the
// hover colour, leaving pills, chips and the cursor accent untouched. Blank
// rows are left alone so empty space doesn't light up.
func gitHoverLineBg(line string) string {
	if strings.TrimSpace(ansi.Strip(line)) == "" {
		return line
	}
	from := rgbColor{0x30, 0x30, 0x30} // sidebarBg #303030
	to := rgbColor{0x2c, 0x31, 0x36}   // VSCode list.hoverBackground-ish
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
			return renderGitSectionHeader("STAGED", staged, true, !m.gitStagedCollapsed, active, focused, w)
		case gitSecChanges:
			_, changed := m.gitFileCounts()
			return renderGitSectionHeader("CHANGES", changed, true, !m.gitChangesCollapsed, active, focused, w)
		default:
			return renderGitSectionHeader("GRAPH", 0, false, !m.gitGraphCollapsed, active, focused, w)
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
		return renderGitGraphCommit(r, active, focused, w)
	case gitRowConnector:
		return renderGitGraphConnector(r, w)
	case gitRowNote:
		return padBgToWidth(gitBranchStyle.Render("   "+r.note), w)
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
	fgName := lipgloss.Color("#cccccc")
	fgDir := lipgloss.Color("#858585")
	fgStatus := statusFG
	if active {
		fgName = lipgloss.Color("#d6d6d6")
		fgDir = lipgloss.Color("#9aa6ad")
		if focused {
			fgName = lipgloss.Color("#ffffff")
			fgDir = lipgloss.Color("#a9c7e0")
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
// gitViewToggleWidth is the on-screen width of the "tree · flat" toggle and
// its trailing breather; handleGitSidebarMouse hit-tests the right edge of the
// title row against it.
const gitViewToggleWidth = 12 // "tree · flat" (11) + 1 trailing pad

// renderGitTitleRow draws the letter-spaced " S O U R C E   C O N T R O L "
// title, mirroring the file-explorer's "E X P L O R E R" header. Falls back to
// the plain title when the panel is too narrow for the spaced form.
func renderGitTitleRow(w int) string {
	spaced := " " + letterSpace("SOURCE CONTROL")
	if runewidth.StringWidth(spaced) > w {
		spaced = " SOURCE CONTROL"
	}
	return padBgToWidth(gitTitleStyle.Render(spaced), w)
}

// renderGitHairline draws the 1/8-block underline beneath the title — the same
// hairline the explorer header uses to separate itself from the body.
func renderGitHairline(w int) string {
	if w <= 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Background(sidebarBg).
		Foreground(lipgloss.Color("#454545")).
		Render(strings.Repeat("▔", w))
}

// gitViewToggleWidth (declared above) is hit-tested on the sub-header row.

// renderGitSubheader renders the branch (left) and the tree·flat toggle
// (right) under the hairline — the row the mouse hit-tests for the toggle.
func renderGitSubheader(b git.Branch, viewTree bool, w int) string {
	rowStyle := lipgloss.NewStyle().Background(sidebarBg)
	branchStyle := lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#6fd0bd")).Bold(true)
	aheadStyle := lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#73c991"))
	behindStyle := lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#e2c08d"))
	dim := lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#5f5f5f"))
	on := lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#4ec9e0")).Bold(true)

	treeStyle, flatStyle := dim, on
	if viewTree {
		treeStyle, flatStyle = on, dim
	}
	toggle := treeStyle.Render("tree") + dim.Render(" · ") + flatStyle.Render("flat")
	const toggleW = 11

	var sb strings.Builder
	sb.WriteString(rowStyle.Render(" "))
	used := 1
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
	name = lipgloss.Color("#cccccc")
	status = statusFG
	if active {
		name = lipgloss.Color("#d6d6d6")
		if focused {
			name = lipgloss.Color("#ffffff")
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
		return lipgloss.Color("#1f2d36")
	case active:
		return lipgloss.Color("#2a2a2e")
	default:
		return sidebarBg
	}
}

// gitLead renders the leftmost cell of a row: a bright cyan accent bar on the
// focused cursor row, otherwise a blank in the row background.
func gitLead(active, focused bool, bg lipgloss.Color) string {
	if active && focused {
		return lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("#36a3d9")).Render("▌")
	}
	return lipgloss.NewStyle().Background(bg).Render(" ")
}

// gitTreeGuide renders the accent-bar/blank lead cell + one `│ ` indent guide
// per nesting level, returning the written width so callers can budget the name.
func gitTreeGuide(sb *strings.Builder, bg lipgloss.Color, depth int, active, focused bool) int {
	rowStyle := lipgloss.NewStyle().Background(bg)
	guideStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("#46505a"))
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
	bg, fgName, _ := gitRowColors(active, focused, lipgloss.Color("#858585"))
	rowStyle := lipgloss.NewStyle().Background(bg)
	chev, chevFG := "▸", lipgloss.Color("#858585")
	if expanded {
		chev, chevFG = "▾", lipgloss.Color("#e2c08d")
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

var (
	gitLaneColor    = lipgloss.Color("#566270") // one clean dim slate for every lane/spine line
	gitNodeColor    = lipgloss.Color("#7fb3d5") // a regular commit node ●
	gitMergeColor   = lipgloss.Color("#c586c0") // a merge node (magenta ●)
	gitHeadColor    = lipgloss.Color("#4ec9b0") // the HEAD node ◉
	gitAgeColor     = lipgloss.Color("#6e7681") // dim relative timestamp
	gitSubjectColor = lipgloss.Color("#c5c5c5")
)

// renderGitSectionHeader renders a collapsible accordion divider as a quiet
// letter-spaced caption with a trailing count:
//
//	 C H A N G E S   4
//
// Expanded sections show just the spaced label (clean); a collapsed one gets a
// leading "▸" so the folded state is visible. The cursor row gets the accent
// bar + subtle bg like every other row.
func renderGitSectionHeader(label string, count int, hasCount, expanded, active, focused bool, w int) string {
	bg := gitSelBg(active, focused)
	chevFG := lipgloss.Color("#666c72")
	labelFG := lipgloss.Color("#9aa3ac")
	ruleFG := lipgloss.Color("#3a3d40")
	chipBg, chipFG := lipgloss.Color("#2b3138"), lipgloss.Color("#8b97a1")
	if active {
		chevFG = lipgloss.Color("#9aa3ac")
		labelFG = lipgloss.Color("#d6dee5")
		ruleFG = lipgloss.Color("#4a4f55")
		chipBg, chipFG = lipgloss.Color("#33424e"), lipgloss.Color("#bcd2e2")
		if focused {
			labelFG = lipgloss.Color("#e8f0f6")
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
	if hasCount {
		chip = chipStyle.Render(" " + fmt.Sprintf("%d", count) + " ")
		chipTotal = 1 + lipgloss.Width(chip) + 1
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

// letterSpace inserts a space between each rune of s ("GRAPH" → "G R A P H")
// for the section-divider caption look.
func letterSpace(s string) string {
	rs := []rune(s)
	if len(rs) <= 1 {
		return s
	}
	out := make([]rune, 0, len(rs)*2-1)
	for i, r := range rs {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, r)
	}
	return string(out)
}

// gitSubjectTypeColor returns the conventional-commit type colour for a subject
// ("feat: …" → green), or (_, false) when there's no recognised type prefix.
func gitSubjectTypeColor(subject string) (lipgloss.Color, bool) {
	colon := strings.IndexByte(subject, ':')
	if colon <= 0 || colon > 20 {
		return "", false
	}
	head := subject[:colon]
	typ := head
	if p := strings.IndexByte(head, '('); p >= 0 {
		typ = head[:p]
	}
	return gitCommitTypeColor(strings.ToLower(strings.TrimSpace(typ)))
}

// renderGitArt renders the graph topology prefix: lane glyphs (│ ╲ ╱) in the
// uniform lane colour, and the commit node "*" replaced by the given glyph and
// colour. Returns the rendered string and its width.
func renderGitArt(art, node string, nodeColor, bg lipgloss.Color) (string, int) {
	laneStyle := lipgloss.NewStyle().Background(bg).Foreground(gitLaneColor)
	nodeStyle := lipgloss.NewStyle().Background(bg).Foreground(nodeColor).Bold(true)
	rowStyle := lipgloss.NewStyle().Background(bg)

	var sb strings.Builder
	width := 0
	for _, r := range art {
		switch r {
		case '*':
			sb.WriteString(nodeStyle.Render(node))
		case '|', '│':
			sb.WriteString(laneStyle.Render("│"))
		case '/':
			sb.WriteString(laneStyle.Render("╱"))
		case '\\':
			sb.WriteString(laneStyle.Render("╲"))
		case '_', '-':
			sb.WriteString(laneStyle.Render("─"))
		default:
			sb.WriteString(rowStyle.Render(string(r)))
		}
		width += runewidth.RuneWidth(r)
	}
	return sb.String(), width
}

// renderGitGraphCommit renders one commit as a "ribbon" timeline row: a solid
// vertical bar (▌) at the left whose colour encodes the conventional-commit
// type (feat=green, fix=amber, …) — consecutive rows form one continuous
// colour-segmented rail — followed by an optional branch/tag pill, the clean
// commit message (type prefix stripped), and a right-aligned relative age.
func renderGitGraphCommit(r gitPanelRow, active, focused bool, w int) string {
	bg, _, _ := gitRowColors(active, focused, lipgloss.Color("#858585"))
	rowStyle := lipgloss.NewStyle().Background(bg)

	// Ribbon colour: commit type, with HEAD (teal) and merge (magenta) overrides.
	ribbonColor := gitNodeColor
	if tc, ok := gitSubjectTypeColor(r.subject); ok {
		ribbonColor = tc
	}
	switch {
	case r.isHead:
		ribbonColor = gitHeadColor
	case r.isMerge:
		ribbonColor = gitMergeColor
	}
	ribbonStyle := lipgloss.NewStyle().Background(bg).Foreground(ribbonColor)

	subjFG := gitSubjectColor
	if r.isHead {
		subjFG = lipgloss.Color("#eef4f8") // the current commit reads brightest
	}
	subjStyle := lipgloss.NewStyle().Background(bg).Foreground(subjFG).Bold(r.isHead)
	if active && focused {
		subjStyle = subjStyle.Foreground(lipgloss.Color("#ffffff"))
	}
	ageStyle := lipgloss.NewStyle().Background(bg).Foreground(gitAgeColor)

	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg)) // selection accent column
	sb.WriteString(ribbonStyle.Render("▌"))      // type-coloured ribbon rail
	sb.WriteString(rowStyle.Render(" "))
	used := 3

	ageW := 0
	if r.age != "" {
		ageW = 1 + runewidth.StringWidth(r.age)
	}

	// Branch/tag pill, if there's comfortable room.
	if pill := gitRefPill(r.refs); pill != "" {
		pillW := lipgloss.Width(pill)
		if used+pillW+1+ageW+4 <= w {
			sb.WriteString(pill)
			sb.WriteString(rowStyle.Render(" "))
			used += pillW + 1
		}
	}

	// Clean message (type prefix stripped — the ribbon colour carries the type).
	subject := gitStripCommitType(r.subject)
	if avail := w - used - ageW; avail >= 1 && subject != "" {
		if runewidth.StringWidth(subject) > avail {
			subject = runewidth.Truncate(subject, avail, "…")
		}
		sb.WriteString(subjStyle.Render(subject))
		used += runewidth.StringWidth(subject)
	}

	// Age, right-aligned.
	if r.age != "" {
		if pad := w - used - ageW; pad > 0 {
			sb.WriteString(rowStyle.Render(strings.Repeat(" ", pad)))
			used += pad
		}
		sb.WriteString(rowStyle.Render(" "))
		sb.WriteString(ageStyle.Render(r.age))
		used += ageW
	}
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}

// gitStripCommitType removes a recognised conventional-commit prefix
// ("feat(git): ", "fix: ") so the ribbon colour carries the type and the row
// shows just the message. Unrecognised subjects are returned unchanged.
func gitStripCommitType(subject string) string {
	if _, ok := gitSubjectTypeColor(subject); ok {
		if colon := strings.IndexByte(subject, ':'); colon >= 0 {
			return strings.TrimSpace(subject[colon+1:])
		}
	}
	return subject
}

// gitCommitTypeColor maps a conventional-commit type to its accent colour.
func gitCommitTypeColor(typ string) (lipgloss.Color, bool) {
	switch typ {
	case "feat":
		return lipgloss.Color("#73c991"), true // green
	case "fix":
		return lipgloss.Color("#e2c08d"), true // amber
	case "docs":
		return lipgloss.Color("#569cd6"), true // blue
	case "refactor":
		return lipgloss.Color("#c586c0"), true // magenta
	case "perf":
		return lipgloss.Color("#4ec9b0"), true // teal
	case "test":
		return lipgloss.Color("#56b6c2"), true // cyan
	case "style":
		return lipgloss.Color("#b5bd68"), true // olive
	case "revert":
		return lipgloss.Color("#f48771"), true // red-orange
	case "chore", "ci", "build", "deps":
		return lipgloss.Color("#8a949d"), true // muted
	}
	return "", false
}

// renderGitGraphConnector renders a topology-only line ("|/", "| |") in one
// clean, uniform lane colour so the rail reads as a single tidy thread.
func renderGitGraphConnector(r gitPanelRow, w int) string {
	rowStyle := lipgloss.NewStyle().Background(sidebarBg)
	artStr, artW := renderGitArt(r.art, "●", gitNodeColor, sidebarBg)
	return clampSidebarRow(rowStyle.Render(" ")+artStr, 1+artW, w, rowStyle)
}

// gitRefPill renders the first branch/tag decoration as a small coloured pill
// (e.g. a green " main "), or "" when there's no ref. Tags get an amber pill.
func gitRefPill(refs string) string {
	r := strings.TrimSpace(refs)
	r = strings.TrimPrefix(r, "(")
	r = strings.TrimSuffix(r, ")")
	if r == "" {
		return ""
	}
	r = strings.ReplaceAll(r, "HEAD -> ", "")
	if i := strings.IndexByte(r, ','); i >= 0 {
		r = r[:i]
	}
	r = strings.TrimSpace(r)
	if r == "" {
		return ""
	}
	pillBg, pillFG := lipgloss.Color("#234d36"), lipgloss.Color("#89d99b") // branch: green
	if strings.HasPrefix(r, "tag:") {
		r = strings.TrimSpace(strings.TrimPrefix(r, "tag:"))
		pillBg, pillFG = lipgloss.Color("#4a3a1f"), lipgloss.Color("#e2c08d") // tag: amber
	}
	if runewidth.StringWidth(r) > 12 {
		r = runewidth.Truncate(r, 12, "…")
	}
	return lipgloss.NewStyle().Background(pillBg).Foreground(pillFG).Render(" " + r + " ")
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
		return "○", lipgloss.Color("#73c991")
	}
	primary := byte(' ')
	if len(f.Code) >= 1 && f.Code[0] != ' ' {
		primary = f.Code[0]
	} else if len(f.Code) >= 2 {
		primary = f.Code[1]
	}
	switch primary {
	case 'M':
		return "●", lipgloss.Color("#e2c08d")
	case 'A':
		return "●", lipgloss.Color("#73c991")
	case 'D':
		return "●", lipgloss.Color("#f48771")
	case 'R':
		return "●", lipgloss.Color("#569cd6")
	case 'C', 'U':
		return "●", lipgloss.Color("#f44747")
	}
	return "·", lipgloss.Color("#8c8c8c")
}

func padToWidth(s string, w int) string {
	pad := w - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// padBgToWidth pads with the sidebar bg so empty space stays the panel color.
func padBgToWidth(s string, w int) string {
	width := lipgloss.Width(s)
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

func placeholderSidebar(w, h int, title, body string) string {
	titleStyle := lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#cccccc")).Bold(true)
	bodyStyle := lipgloss.NewStyle().Background(sidebarBg).Foreground(lipgloss.Color("#858585"))
	content := titleStyle.Render(title) + "\n\n" + bodyStyle.Render(body)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceBackground(sidebarBg),
	)
}

// buildSeparator is no longer used — boundaries are now expressed via
// background-color transitions. Kept here so callers from older revisions
// still compile if they reference it.
func buildSeparator(h int, t theme.Theme) string {
	_ = t
	var lines []string
	for i := 0; i < h; i++ {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) statusState() statusbar.State {
	cur := m.editor.Cursor()
	lang := m.editor.Lang()
	if lang != "" {
		lang = strings.ToUpper(lang[:1]) + lang[1:]
	}
	// Project name is the cwd basename — the breadcrumbs row above the
	// editor already carries the active filename + directory, so the
	// status bar only surfaces project-level context here.
	proj := projectName()
	if proj != "" {
		// projectName() upper-cases for the sidebar header; in the
		// status bar's left zone we want a softer, mixed-case look so
		// it reads as ambient context rather than another label.
		proj = strings.ToLower(proj)
	}
	return statusbar.State{
		Path:     m.editor.Path(),
		Project:  proj,
		Lang:     lang,
		Line:     cur.Line + 1,
		Col:      cur.Col + 1,
		Dirty:    m.editor.IsDirty(),
		Err:      m.err,
		Errors:   m.editor.Errors(),
		Warnings: m.editor.Warnings(),
		Branch:   m.gitBranch.Name,
		Ahead:    m.gitBranch.Ahead,
		Behind:   m.gitBranch.Behind,
		Indent:   "Spaces: 4",
		Encoding: "UTF-8",
		Term:     m.termOpen,
		Debug:    m.dapSessionActive,
	}
}

// overlayPreferredActionHint splices a 1-row "💡 <Title>  Ctrl+. ▸ Apply
// Ctrl+Shift+." chip onto the row directly below the editor cursor (or
// directly above when the cursor sits near the bottom of the editor body).
// The chip carries the LSP-flagged preferred action's title and the live
// keymap shortcuts so the user knows the chord for picker / direct-apply.
//
// The hint is purely advisory — pressing the advertised chords is what
// runs the action; this function only paints. It's cleared at the model
// layer by cursor moves (StateMsg) and by any subsequent keystroke that
// isn't the apply chord (handleGlobalKey).
func (m Model) overlayPreferredActionHint(base string) string {
	if m.preferredCodeAction == nil {
		return base
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
	chipW := lipgloss.Width(body)

	// Editor pane geometry — same recipe as renderBase / overlayToastsInEditor
	// so the splice column lands inside the editor.
	actionsColW := m.actionsColumnWidth()
	edPaneW := m.w - activity.Width - editorScrollbarWidth - actionsColW
	if m.showExp {
		edPaneW -= m.explorerWidth
	}
	if edPaneW < 1 {
		return base
	}
	editorStart := m.w - edPaneW - actionsColW

	// Compute chrome rows above the editor body.
	chrome := 0
	if m.replaceOpen {
		chrome += 2
	}
	chrome += m.tabs.Height()
	if len(m.breadcrumbs) > 0 || m.stickyContext != "" {
		chrome++
	}
	editorTop := chrome
	editorBottom := m.h - 2 // status bar lives on the last row; bottom-1 is our floor
	if editorBottom <= editorTop {
		return base
	}

	cur := m.editor.Cursor()
	cursorRow := editorTop + cur.Line
	if cursorRow < editorTop || cursorRow > editorBottom {
		// Cursor outside visible viewport — nothing sensible to anchor to.
		return base
	}

	// Prefer the row below the cursor; fall back to the row above when
	// there's no room below.
	row := cursorRow + 1
	if row > editorBottom {
		row = cursorRow - 1
	}
	if row < editorTop || row > editorBottom {
		return base
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
			return base
		}
		body = truncRightVisual(body, chipW)
	}

	lines := strings.Split(base, "\n")
	if row < 0 || row >= len(lines) {
		return base
	}
	// Glassify so the chip blends into the underlying editor cells (same
	// bg-blend recipe as toasts / find bar).
	body = glassifyHintRow(body, lines[row], leftCol)
	lines[row] = spliceAt(lines[row], body, leftCol)
	return strings.Join(lines, "\n")
}

// glassifyHintRow blends each panel-bg cell of the hint row with the
// underlying base row so the chip reads as a translucent strip rather than
// a solid block, matching the find bar / toast recipe.
func glassifyHintRow(panelRow, baseRow string, leftCol int) string {
	const alpha = 0.92
	tint := rgbColor{0x00, 0x01, 0x03}
	// theme.BgPanel resolves to a 256-color index; we accept whatever the
	// active theme produced and just blend the cells whose bg is the
	// dominant chip bg. extractBgRGB returns false for cells whose bg
	// can't be parsed as truecolor — we leave those alone.
	pcells := parseANSIRow(panelRow)
	bcells := parseANSIRow(baseRow)
	for i := range pcells {
		bi := leftCol + i
		if bi < 0 || bi >= len(bcells) {
			continue
		}
		_, ok := extractBgRGB(pcells[i].sgr)
		if !ok {
			continue
		}
		baseBg, baseOK := extractBgRGB(bcells[bi].sgr)
		if !baseOK {
			continue
		}
		blendedBg := blendRGB(baseBg, tint, alpha)
		if pcells[i].glyph != " " && pcells[i].glyph != "" {
			pcells[i].sgr = replaceBgRGB(pcells[i].sgr, blendedBg)
			continue
		}
		if isPrintableASCII(bcells[bi].glyph) {
			baseFg, fgOK := extractFgRGB(bcells[bi].sgr)
			if !fgOK {
				baseFg = textPrimaryFallback
			}
			pcells[i].glyph = bcells[bi].glyph
			pcells[i].sgr = makeFgBgSGR(dimRGB(baseFg, 0.45), blendedBg)
		} else {
			pcells[i].glyph = " "
			pcells[i].sgr = makeFgBgSGR(rgbColor{}, blendedBg)
		}
	}
	return renderCells(pcells)
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
