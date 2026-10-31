package explorer

import (
	"maps"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// GitStatus maps an absolute file path to a single-letter git status:
// "M" modified, "A" added, "D" deleted, "U" untracked, "C" conflict,
// "I" ignored.
type GitStatus = map[string]string

// ─── Layout constants ─────────────────────────────────────────────────────

const (
	// Outer padding inside the panel.
	leftPad  = 1
	rightPad = 1
	// Indent step per nesting level.
	indentStep = 2
	// Width of the marker column (chevron / ext-tag) in ASCII mode, and
	// with an icon set (chevron + space + icon; the trailing gap is
	// markerGap).
	markerW      = 2
	markerWIcons = 3
	// Gap between the marker column and the filename.
	markerGap = 1

	// Total chrome rows around the tree body when there's room:
	// header (1) + hairline (1) + footer divider (1) + footer (1) = 4.
	// Hairline is a 1/8-block underline (▁) under the EXPLORER label.
	chromeFull = 4
	// Compact chrome: header (1) + divider (1) + divider (1) + footer (1) = 4.
	chromeCompact = 4
)

// SetMeta wires runtime metadata into the explorer so its header and footer
// can render workspace name, branch, and accurate file counts. Call once per
// render frame from the host.
func (m *Model) SetMeta(workspace, branch string, isRepo bool, totalFiles int) {
	m.metaWorkspace = workspace
	m.metaBranch = branch
	m.metaIsRepo = isRepo
	m.metaTotalFiles = totalFiles
}

// View renders the entire explorer panel: header, body (tree), footer. The
// returned string is exactly m.w columns wide and m.h rows tall, with every
// row's background filled by the appropriate theme token.
//
// The last result is reused while nothing it reads changed (cache.go).
func (m Model) View(focused bool, activePath string, activeDirty bool, gitStatus GitStatus) string {
	if m.PreviewVisible() {
		return m.previewView(focused, activePath, activeDirty, gitStatus)
	}
	if m.previewOn && m.terminalWidth < 100 {
		out := strings.Split(m.draw(focused, activePath, activeDirty, gitStatus), "\n")
		if len(out) > 0 {
			hint := runewidth.Truncate("Preview needs 100 cols", m.w, "")
			out[len(out)-1] = hint + strings.Repeat(" ", max(m.w-runewidth.StringWidth(hint), 0))
		}
		return strings.Join(out, "\n")
	}
	k := m.viewKey(focused, activePath, activeDirty, gitStatus)
	viewCache.Lock()
	defer viewCache.Unlock()
	if viewCache.ok && viewCache.key.equal(&k) {
		return viewCache.out
	}
	out := m.draw(focused, activePath, activeDirty, gitStatus)
	k.extraRoots = append([]*Node(nil), m.extraRoots...)
	k.git = maps.Clone(gitStatus)
	viewCache.ok, viewCache.key, viewCache.out = true, k, out
	return out
}

// draw renders the panel (View without the cache).
func (m Model) draw(focused bool, activePath string, activeDirty bool, gitStatus GitStatus) string {
	if m.err != "" {
		return "explorer error: " + m.err
	}
	if m.h <= 0 || m.w <= 0 {
		return ""
	}

	// Decide chrome budget based on available height.
	var chrome int
	switch {
	case m.h >= 12:
		chrome = chromeFull
	case m.h >= 6:
		chrome = chromeCompact
	default:
		chrome = 0 // tiny panels: just the tree, no header/footer
	}
	bodyH := m.h - chrome
	if bodyH < 1 {
		bodyH = 1
	}

	rows := make([]string, 0, m.h)

	// ── Header chrome ────────────────────────────────────────────────────
	// Header label + a hairline (1/8-block ▁ underline) for visual
	// separation from the tree body without the heaviness of a full
	// divider row. Compact mode swaps the hairline for a regular divider
	// when the panel is tall enough but not full.
	if chrome > 0 {
		rows = append(rows, m.renderHeader(focused))
		if chrome == chromeFull {
			rows = append(rows, m.renderHeaderHairline(focused))
		} else if chrome == chromeCompact {
			rows = append(rows, sidebarDividerRow(m.w))
		}
	}

	// ── Tree body ────────────────────────────────────────────────────────
	bodyRows := m.renderTree(focused, activePath, activeDirty, gitStatus, bodyH)
	rows = append(rows, bodyRows...)

	// ── Footer chrome ────────────────────────────────────────────────────
	if chrome > 0 {
		rows = append(rows, sidebarDividerRow(m.w))
		rows = append(rows, m.renderFooter(gitStatus))
	}

	// Pad to exactly m.h rows.
	for len(rows) < m.h {
		rows = append(rows, blankSidebarRow(m.w))
	}
	if len(rows) > m.h {
		rows = rows[:m.h]
	}
	return strings.Join(rows, "\n")
}

// ─── Header ───────────────────────────────────────────────────────────────

// renderHeader produces:
//
//	"  E X P L O R E R · {workspace}                {branch} ⋯  "
//
// — caps + letter-spaced label, dim separator, secondary workspace name,
// muted branch (right-aligned, ≤12 chars), muted ⋯ kebab.
func (m Model) renderHeader(focused bool) string {
	bg := theme.Bg(theme.BgSidebar)
	primary := lipgloss.NewStyle().Background(theme.LG(theme.BgSidebar)).
		Foreground(theme.PaneTitle(focused)).Bold(true)
	secondary := theme.FgBg(theme.TextSecondary, theme.BgSidebar)
	muted := theme.FgBg(theme.TextMuted, theme.BgSidebar)

	leftPadStr := bg.Render(strings.Repeat(" ", leftPad+1))
	rightPadStr := bg.Render(strings.Repeat(" ", rightPad+1))

	innerW := m.w - leftPad - rightPad - 2
	if innerW <= 0 {
		return blankSidebarRow(m.w)
	}

	// Plain caps (letter spacing crowded narrow sidebars).
	spacedLabel := "EXPLORER"
	left := primary.Render(spacedLabel)
	if ws := m.metaWorkspace; ws != "" {
		room := innerW - lipgloss.Width(left) - 3 - 4 // keep space for the right side
		if room >= 4 {
			if runewidth.StringWidth(ws) > room {
				ws = runewidth.Truncate(ws, room, "…")
			}
			left += bg.Render(" ") + muted.Render("·") + bg.Render(" ") + secondary.Render(ws)
		}
	}
	leftW := lipgloss.Width(left)

	// Right side: branch (truncated) + ⋯ kebab.
	var right string
	rightW := 0
	br := m.metaBranch
	if !m.metaIsRepo {
		br = ""
	}
	if br != "" {
		if runewidth.StringWidth(br) > 12 {
			br = runewidth.Truncate(br, 12, "…")
		}
		right = muted.Render(br) + bg.Render(" ") + muted.Render(theme.GlyphMore.String())
	} else {
		right = muted.Render(theme.GlyphMore.String())
	}
	rightW = lipgloss.Width(right)

	gap := innerW - leftW - rightW
	if gap < 1 {
		// Not enough room — drop the right side, then drop the workspace.
		right = ""
		rightW = 0
		gap = innerW - leftW
		if gap < 0 {
			// Truncate the left.
			left = primary.Render(spacedLabel)
			leftW = lipgloss.Width(left)
			gap = innerW - leftW
			if gap < 0 {
				gap = 0
			}
		}
	}
	gapStr := bg.Render(strings.Repeat(" ", gap))
	row := leftPadStr + left + gapStr + right + rightPadStr
	return padOrTruncRow(row, m.w)
}

// ─── Footer ───────────────────────────────────────────────────────────────

// renderFooter produces a 1-row mini status bar:
//
//	"  ● 3   ● 1                     87 files  "
//
// Modified count (amber dot) and untracked count (green dot) on the left,
// total file count "87 files" muted on the right. If the project isn't a
// git repo, only the right side renders.
func (m Model) renderFooter(gitStatus GitStatus) string {
	bg := theme.Bg(theme.BgSidebar)
	muted := theme.FgBg(theme.TextMuted, theme.BgSidebar)
	amberDot := theme.FgBg(theme.AccentAmber, theme.BgSidebar).Bold(true)
	greenDot := theme.FgBg(theme.AccentGreen, theme.BgSidebar).Bold(true)
	countStyle := theme.FgBg(theme.TextSecondary, theme.BgSidebar)

	leftPadStr := bg.Render(strings.Repeat(" ", leftPad))
	rightPadStr := bg.Render(strings.Repeat(" ", rightPad))

	innerW := m.w - leftPad - rightPad
	if innerW <= 0 {
		return blankSidebarRow(m.w)
	}

	mod, untracked := 0, 0
	if m.metaIsRepo && gitStatus != nil {
		for _, code := range gitStatus {
			switch code {
			case "M", "A", "D", "C":
				mod++
			case "U":
				untracked++
			}
		}
	}

	var leftSegs []string
	if m.metaIsRepo {
		if mod > 0 {
			leftSegs = append(leftSegs,
				amberDot.Render("●")+bg.Render(" ")+countStyle.Render(itoa(mod)))
		}
		if untracked > 0 {
			leftSegs = append(leftSegs,
				greenDot.Render("●")+bg.Render(" ")+countStyle.Render(itoa(untracked)))
		}
	}
	left := strings.Join(leftSegs, bg.Render("   "))
	leftW := lipgloss.Width(left)

	rightText := itoa(m.metaTotalFiles) + " files"
	right := muted.Render(rightText)
	rightW := lipgloss.Width(right)

	gap := innerW - leftW - rightW
	if gap < 1 {
		// Not enough room — drop the right side.
		right = ""
		rightW = 0
		gap = innerW - leftW
		if gap < 0 {
			gap = 0
		}
	}
	gapStr := bg.Render(strings.Repeat(" ", gap))
	row := leftPadStr + left + gapStr + right + rightPadStr
	return padOrTruncRow(row, m.w)
}

// ─── Tree body ────────────────────────────────────────────────────────────

func (m Model) renderTree(focused bool, activePath string, activeDirty bool, gitStatus GitStatus, h int) []string {
	vis := m.visible()
	// Adjust top so the cursor stays in view if the body height changed
	// since the last scrollIntoView.
	top := m.top
	if top > len(vis) {
		top = len(vis)
	}
	if top < 0 {
		top = 0
	}
	if m.cursor >= top+h {
		top = m.cursor - h + 1
		if top < 0 {
			top = 0
		}
	}
	if m.cursor < top {
		top = m.cursor
	}
	top = m.drawTop(top, len(vis))
	end := top + h
	if end > len(vis) {
		end = len(vis)
	}

	// Pre-compute per-folder change counts when in a git repo.
	folderCounts := map[string]int{}
	if m.metaIsRepo && gitStatus != nil {
		folderCounts = computeFolderCounts(m.allRoots(), gitStatus)
	}

	out := make([]string, 0, h)
	for i := top; i < end; i++ {
		out = append(out, m.renderTreeRow(vis[i], i == m.cursor, focused, activePath, activeDirty, gitStatus, folderCounts))
	}
	for len(out) < h {
		out = append(out, blankSidebarRow(m.w))
	}
	return out
}

func (m Model) renderTreeRow(
	e VisibleEntry,
	isCursor, focused bool,
	activePath string,
	activeDirty bool,
	gitStatus GitStatus,
	folderCounts map[string]int,
) string {
	// Background for the row. Cursor and multi-selected rows share the
	// same "glass-black faint" highlight so a multi-select reads as a
	// uniform group; the cursor row only stands apart by being bold (when
	// the panel is focused).
	highlighted := isCursor || m.IsSelected(e.Node.Path)
	rowBgC := theme.LG(theme.BgSidebar)
	if highlighted {
		// Soft accent wash for the cursor / selection; stronger while the
		// panel has focus.
		lvl := theme.WashSelected
		if focused {
			lvl = theme.WashSelectedFocused
		}
		rowBgC = theme.Wash(theme.BgSidebar, lvl)
	}
	// Every piece is drawn with paint (paint.go), a cheap Render.
	bgPaint := func(t string) string { return paint("", rowBgC, false, t) }

	// ── 1. Left pad / accent bar ────────────────────────────────────────
	var prefix strings.Builder
	if highlighted {
		// `▌` (left-half block) accent on the leftmost cell. Hard-coded
		// 256-color 98 (#875fd7 — a darker purple) instead of the global
		// AccentMagenta (#d75fd7 — too light against the BgPanel
		// highlight); this keeps the welcome-screen icon's bright magenta
		// untouched while the explorer's selection bar reads as a deeper,
		// more grounded purple.
		prefix.WriteString(paint(theme.LG(theme.BorderFocus), rowBgC, focused && isCursor, "▌"))
		// remaining left pad cells (leftPad - 1)
		if leftPad > 1 {
			prefix.WriteString(bgPaint(strings.Repeat(" ", leftPad-1)))
		}
	} else {
		prefix.WriteString(bgPaint(strings.Repeat(" ", leftPad)))
	}

	// ── 2. Indent guides (one per nesting level beyond root) ────────────
	if e.Depth > 0 {
		guideFg := theme.Mix(theme.BgSidebar, theme.TextDimmer, 0.6)
		for d := 0; d < e.Depth; d++ {
			// `│` U+2502 + space — indentStep cols.
			prefix.WriteString(paint(guideFg, rowBgC, false, "│"))
			if indentStep > 1 {
				prefix.WriteString(bgPaint(strings.Repeat(" ", indentStep-1)))
			}
		}
	}
	prefixW := leftPad + e.Depth*indentStep

	// ── 3. Determine flags for styling ──────────────────────────────────
	isActive := !e.Node.IsDir && e.Node.Path == activePath
	_ = isActive
	_ = activeDirty
	isIgnored := nodeIsIgnored(e.Node, gitStatus)

	// ── 4. Marker column (chevron for dirs, ext-tag for files) ──────────
	// With an icon set the column is "chevron, space, icon, space" for
	// folders and "two spaces, icon, space" for files, so file icons line
	// up under their folder's icon (VSCode's twistie column).
	mW := markerColW()
	var marker string
	var markerFg lipgloss.Color // the non-icon marker's colour
	iconMode := mW == markerWIcons
	if iconMode {
		g, c := theme.FileIconFor(e.Node.Name, e.Node.IsDir, e.Node.Expanded)
		if isIgnored {
			c = theme.TextDim
		}
		chev := "  "
		chevFg := theme.TextDim
		if e.Node.IsDir && theme.CurrentIconMode == theme.IconModeNerdFont {
			chev = " " // nf-fa-angle_right
			if e.Node.Expanded {
				chev = " " // nf-fa-angle_down
			}
		}
		iconFg := theme.LG(c)
		if !e.Node.IsDir {
			if code, ok := gitStatus[e.Node.Path]; ok && code == "U" && !isIgnored {
				iconFg = theme.LG(theme.AccentGreen)
			}
		}
		marker = paint(theme.LG(chevFg), rowBgC, false, chev) +
			paint(iconFg, rowBgC, false, g) + bgPaint(" ")
	} else if e.Node.IsDir {
		var ch string
		if e.Node.Expanded {
			ch = "▾"
			// Open: chevron amber, name primary bold (handled later).
			markerFg = theme.LG(theme.AccentAmber)
		} else {
			ch = "▸"
			markerFg = theme.LG(theme.TextMuted)
		}
		// marker column is markerW=2 wide; chevron + 1 space.
		marker = ch + " "
	} else {
		tag := extTag(e.Node.Name)
		tagFG := extTagColor(e.Node.Name)
		if isIgnored {
			tagFG = theme.TextMuted
		}
		markerFg = theme.LG(tagFG)
		// Pad/truncate tag to exactly 2 cells.
		if runewidth.StringWidth(tag) > 2 {
			tag = runewidth.Truncate(tag, 2, "")
		}
		for runewidth.StringWidth(tag) < 2 {
			tag += " "
		}
		marker = tag
	}

	// ── 5. Filename styling ─────────────────────────────────────────────
	nameFG := theme.TextPrimary
	bold := false
	switch {
	case isCursor:
		nameFG = theme.TextWhite
		bold = true
	case e.Node.IsDir && e.Node.Expanded:
		nameFG = theme.TextPrimary
		bold = true
	case isIgnored:
		nameFG = theme.TextDim // one shade darker than secondary
	default:
		nameFG = theme.TextPrimary
	}
	// Untracked file → tint name + ext tag green.
	if !e.Node.IsDir && !isIgnored && !isCursor {
		if code, ok := gitStatus[e.Node.Path]; ok && code == "U" {
			nameFG = theme.AccentGreen
			markerFg = theme.LG(theme.AccentGreen)
		}
	}

	// ── 6. Right-side git status badge ──────────────────────────────────
	rightBadge := ""
	rightBadgeW := 0
	if m.metaIsRepo {
		if e.Node.IsDir {
			// Folder: count of changed descendants in muted amber.
			if c := folderCounts[e.Node.Path]; c > 0 {
				rightBadge = paint(theme.LG(theme.AccentAmber), rowBgC, false, itoa(c))
				rightBadgeW = lipgloss.Width(rightBadge)
			}
		} else if !isIgnored && gitStatus != nil {
			if code, ok := gitStatus[e.Node.Path]; ok && code != "" && code != "I" {
				badgeFG, badgeBold := gitBadgeStyle(code)
				rightBadge = paint(theme.LG(badgeFG), rowBgC, badgeBold, code)
				rightBadgeW = lipgloss.Width(rightBadge)
			}
		}
	}

	// ── 7. Compose the row ──────────────────────────────────────────────
	// Width budget:
	//   leftPad (already in prefix)
	//   indent guides (already in prefix)
	//   marker column (2 cells)
	//   markerGap (1 cell)
	//   name (variable, truncated)
	//   spacer + rightBadge (variable, only if badge present)
	//   rightPad (1 cell)
	usedFixed := prefixW + mW + markerGap + rightPad
	availForName := m.w - usedFixed - rightBadgeW
	if rightBadgeW > 0 {
		availForName -= 1 // 1-col gap before the badge
	}
	if availForName < 4 {
		availForName = 4
	}
	name := e.Node.Name
	if e.Node.loading {
		name += " …" // a container folder being read (fs_load.go)
	}
	if runewidth.StringWidth(name) > availForName {
		name = runewidth.Truncate(name, availForName, "…")
	}

	var rendered string
	if iconMode {
		rendered = prefix.String() + marker + paint(theme.LG(nameFG), rowBgC, bold, name)
	} else {
		rendered = prefix.String() +
			paint(markerFg, rowBgC, false, marker) +
			bgPaint(strings.Repeat(" ", markerGap)) +
			paint(theme.LG(nameFG), rowBgC, bold, name)
	}

	// Compute the actual visual width so far so we know how much spacer to add.
	usedSoFar := prefixW + mW + markerGap + runewidth.StringWidth(name)
	tail := m.w - usedSoFar - rightBadgeW - rightPad
	if rightBadgeW > 0 {
		// 1-cell gap before the badge.
		if tail < 1 {
			tail = 1
		}
	}
	if tail < 0 {
		tail = 0
	}
	rendered += bgPaint(strings.Repeat(" ", tail))
	if rightBadgeW > 0 {
		rendered += rightBadge
	}
	rendered += bgPaint(strings.Repeat(" ", rightPad))

	return padOrTruncRow(rendered, m.w)
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// markerColW is the marker column width for the active icon mode.
func markerColW() int {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return markerW
	}
	return markerWIcons
}

// blankSidebarRow returns a styled spacer row of width w in the sidebar bg.
func blankSidebarRow(w int) string {
	if w <= 0 {
		return ""
	}
	return theme.Bg(theme.BgSidebar).Render(strings.Repeat(" ", w))
}

// sidebarDividerRow renders a 1-row divider in BgSidebar with a single thin
// rule. We keep it as a flat tinted row (no glyphs) so the eye reads it as
// scaffolding rather than a horizontal bar.
func sidebarDividerRow(w int) string {
	if w <= 0 {
		return ""
	}
	// A row of `─` (U+2500) in dim text on the sidebar bg.
	style := lipgloss.NewStyle().
		Foreground(theme.LG(theme.TextDimmer)).
		Background(theme.LG(theme.BgSidebar))
	return style.Render(strings.Repeat("─", w))
}

// renderHeaderHairline draws a 1/8-block (▔) underline beneath the EXPLORER
// label. Uses U+2594 UPPER ONE EIGHTH BLOCK so the line sits at the TOP of
// the row (right under the label text on the row above) rather than the
// bottom — pulls the divider visually closer to the header. It takes the
// focus accent while the explorer has focus.
func (m Model) renderHeaderHairline(focused bool) string {
	if m.w <= 0 {
		return ""
	}
	return theme.PaneHairline(m.w, theme.BgSidebar, focused)
}

// padOrTruncRow guarantees the row is exactly w cells wide. If it's already
// styled and the visual width matches, we return as-is; otherwise we pad in
// the sidebar background.
func padOrTruncRow(row string, w int) string {
	rw := lipgloss.Width(row)
	if rw == w {
		return row
	}
	if rw < w {
		return row + theme.Bg(theme.BgSidebar).Render(strings.Repeat(" ", w-rw))
	}
	// Too wide — fall back to a runewidth-based truncate. This is rare since
	// each segment is already budgeted.
	return runewidth.Truncate(row, w, "")
}

// nodeIsIgnored decides whether a node should be rendered with the de-
// emphasized style. We combine an explicit gitStatus["I"] entry with a
// curated list of common build artefacts and noisy filenames.
func nodeIsIgnored(n *Node, gs GitStatus) bool {
	if n == nil {
		return false
	}
	if gs != nil {
		if code, ok := gs[n.Path]; ok && code == "I" {
			return true
		}
	}
	return IgnoredName(n.Name, n.IsDir)
}

// IgnoredName is the explorer's list of build artefacts and noisy files
// (node_modules, dist, lock files, minified bundles) drawn dimmed. The
// web UI's tree marks the same entries.
func IgnoredName(name string, isDir bool) bool {
	if isDir {
		switch name {
		case "node_modules", ".git", "dist", "build", "target", "out", "vendor", ".next", ".cache":
			return true
		}
		return false
	}
	// Files: extension/name patterns.
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".lock"):
		return true
	case strings.HasSuffix(lower, ".min.js"):
		return true
	case strings.HasSuffix(lower, ".min.css"):
		return true
	case lower == "go.sum":
		return true
	case lower == "package-lock.json":
		return true
	case lower == "yarn.lock":
		return true
	case lower == "pnpm-lock.yaml":
		return true
	case lower == "composer.lock":
		return true
	}
	return false
}

// extTag returns the 2-char extension tag for a filename. For files with a
// recognised extension we use the extension itself (truncated/padded by the
// caller); for files without we return "  ".
func extTag(name string) string {
	// Special-case go.sum / *.lock so they get a "lk" / "sm" tag.
	lower := strings.ToLower(name)
	switch {
	case lower == "go.sum":
		return "sm"
	case lower == "go.mod":
		return "go"
	case strings.HasSuffix(lower, ".lock"):
		return "lk"
	case lower == "makefile":
		return "mk"
	case lower == "dockerfile":
		return "dk"
	case lower == "readme.md":
		return "md"
	}
	ext := strings.TrimPrefix(filepath.Ext(name), ".")
	if ext == "" {
		return "  "
	}
	if len(ext) == 1 {
		return ext + " "
	}
	return ext[:2]
}

// extTagColor maps an extension to its theme color slot.
func extTagColor(name string) theme.Color256 {
	lower := strings.ToLower(name)
	if lower == "go.sum" || strings.HasSuffix(lower, ".lock") || lower == "package-lock.json" {
		return theme.TextMuted
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	switch ext {
	case "go":
		return theme.AccentLavender
	case "md":
		return theme.AccentGreen
	case "py":
		return theme.AccentAmber
	case "rs":
		return theme.AccentRedCoral
	case "js", "ts", "tsx", "jsx", "mjs", "cjs":
		return theme.AccentAmber
	case "json", "yml", "yaml", "toml":
		return theme.AccentMagenta
	case "sh", "bash", "zsh", "fish":
		return theme.AccentGreen
	case "sum", "lock":
		return theme.TextMuted
	}
	return theme.TextMuted
}

// gitBadgeStyle returns the foreground color and bold flag for a single-
// letter git status badge displayed at the right edge of a file row.
func gitBadgeStyle(code string) (theme.Color256, bool) {
	switch code {
	case "M":
		return theme.AccentAmber, false
	case "U":
		return theme.AccentGreen, false
	case "A":
		return theme.AccentGreen, false
	case "D":
		return theme.AccentRedCoral, false
	case "C", "!":
		return theme.AccentRedCoral, true
	}
	return theme.TextMuted, false
}

// computeFolderCounts tallies the changed entries (M/A/D/C/U; an untracked
// folder counts once, as in Source Control) under each folder of every
// root. It works from the status paths, not the loaded tree, so a
// collapsed folder whose children were never read still shows its count
// (before, the badge depended on whether something had happened to load
// the folder). Linear in len(gs) × path depth.
func computeFolderCounts(roots []*Node, gs GitStatus) map[string]int {
	out := map[string]int{}
	if gs == nil {
		return out
	}
	sep := string(filepath.Separator)
	for p, code := range gs {
		if code == "" || code == "I" {
			continue
		}
		var root string
		for _, r := range roots {
			if r != nil && strings.HasPrefix(p, r.Path+sep) {
				root = r.Path
				break
			}
		}
		if root == "" {
			continue
		}
		for dir := filepath.Dir(p); ; dir = filepath.Dir(dir) {
			out[dir]++
			if dir == root || len(dir) <= len(root) {
				break
			}
		}
	}
	return out
}

// itoa is a tiny dependency-free integer-to-string helper for hot paths.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
