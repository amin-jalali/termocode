package app

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ─── Welcome screen — the start page ───────────────────────────────────
//
// One column of one-line rows under the wordmark (welcome_row.go):
//
//	wordmark       pixel (gradient) or text "T E R M O C O D E"
//	START          6 actions with their keycaps
//	SET UP         the first-run checklist (setup_card*.go), when shown
//	RECENT FILES   up to 5 files + "All N recent files…"   (when any)
//	WORKSPACES     up to 3 folders + "All workspaces…"     (when any)
//	hint bar       ↑↓ move · Enter open · F1 commands      version
//
// The page is anchored to the top of the editor pane (never vertically
// centred) and a fixed shrink ladder (welcomeLadder) makes it fit any
// height by dropping whole rows. The content column is W = min(68,
// paneW-4) cells, centred horizontally.
//
// Render-width contract: every emitted line fills the pane width exactly;
// padding is theme-styled spaces, never bare whitespace.
//
// Color contract: no raw hex literals here; every color is a theme token.

// welcomeMaxW is the content column on wide panes.
const welcomeMaxW = 68

// welcomeNarrowMin: below this content width, rows drop their directory
// column and setup statuses go on their own line.
const welcomeNarrowMin = 50

// welcomeMinHitW: a narrower content column records no click areas.
const welcomeMinHitW = 12

// welcomePixelMinH: the pixel wordmark needs a pane at least this tall.
const welcomePixelMinH = 30

// welcomeSentinelViewAll / welcomeSentinelBrowseAll are the Path values of
// the "All N recent files…" and "All workspaces…" rows; the mouse and key
// handlers open the recents / workspaces pickers for them.
const welcomeSentinelViewAll = "\x00view-all"
const welcomeSentinelBrowseAll = "\x00browse-all"

// ─── Hit-zone types ────────────────────────────────────────────────────

// welcomeRecentHit binds an editor-pane row + col span to a recent file
// (or the All… row, Path == welcomeSentinelViewAll).
type welcomeRecentHit struct {
	Row      int
	Path     string
	ColStart int
	ColEnd   int
}

// welcomeWorkspaceHit is the workspaces counterpart of welcomeRecentHit.
type welcomeWorkspaceHit struct {
	Row      int
	Path     string
	ColStart int
	ColEnd   int
}

// welcomeRowHit is one interactive row: its place and id ("card:N",
// "setup:<button>", "recent:<path>", "workspace:<path>").
type welcomeRowHit struct {
	Row      int
	ColStart int
	ColEnd   int
	ID       string
	H        int // lines tall; 0 = one line (a tile is 3 or 4)
}

// height is the number of editor rows the hit covers.
func (h welcomeRowHit) height() int { return max(h.H, 1) }

// welcomeQuickActionHit binds a row to a START action; clicks dispatch
// the same handler as the keyboard binding.
type welcomeQuickActionHit struct {
	Row      int
	ColStart int
	ColEnd   int
	Action   keymap.Action
	Index    int // index into welcomeQuickActions()
}

// ─── Public API ────────────────────────────────────────────────────────

// renderWelcome returns the start page for the editor pane. focusIdx is
// the keyboard cursor (an index into welcomeHits.rows); focused says the
// editor pane has the keys (the cursor is lit) or not (quieter).
func renderWelcome(w, h, focusIdx int, focused bool) string {
	key := newWelcomeKey(w, h, focusIdx, focused)
	if c := welcomeCache; c != nil && c.key == key && time.Since(c.at) < welcomeCacheTTL {
		welcomeLogoAt = c.logo
		return c.out
	}
	rendered, hits := renderWelcomeHits(w, h, focusIdx, focused)
	welcomeCache = &welcomeCached{key: key, out: rendered, logo: welcomeLogoAt, at: time.Now(), hits: hits}
	return rendered
}

// The welcome page reads the recents files from disk, so View reuses the
// last render for a short while (frames come at up to 60/s, e.g. while a
// splitter is dragged). A new size, focus, theme or icon set re-renders.
const welcomeCacheTTL = time.Second

type welcomeKey struct {
	w, h, focus int
	focused     bool
	theme       string
	cfg         string
	icons       theme.IconMode
	profile     int
	logo        string
	setup       string // the setup card's signature ("" = not shown)
	secretsNote bool   // the move-your-secrets line (secrets_migrate.go)
}

func newWelcomeKey(w, h, focus int, focused bool) welcomeKey {
	return welcomeKey{w: w, h: h, focus: focus, focused: focused, theme: theme.Hex(theme.BgEditor) + theme.Hex(theme.BorderFocus),
		cfg: os.Getenv("XDG_CONFIG_HOME"), icons: theme.CurrentIconMode, profile: int(theme.Profile()), logo: dashboardLogo,
		setup: welcomeSetup.sig, secretsNote: welcomeSecretsNote}
}

// welcomeHits are the click areas of one welcome render. They are
// recorded by the same render that made the cached page, so the mouse
// code hit-tests exactly what is on screen.
type welcomeHits struct {
	rows       []welcomeRowHit // every interactive row, in cursor order
	tileCols   int             // START tile columns (0 = one-line rows)
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
// recent files or workspaces change: the cache key does not cover them.
func invalidateWelcome() { welcomeCache = nil }

// welcomeLayoutHits returns the click areas of the welcome page at w×h.
// The cursor only changes colors, not the layout, so the hits of the
// last render are reused whatever row it highlighted. Without a fresh
// render (new size, theme, lists, or the cache is too old) it renders the
// page once — the hover path used to re-render on every mouse move.
func welcomeLayoutHits(w, h int) welcomeHits {
	if c := welcomeCache; c != nil && time.Since(c.at) < welcomeCacheTTL {
		key := newWelcomeKey(w, h, c.key.focus, c.key.focused)
		if c.key == key {
			return c.hits
		}
	}
	renderWelcome(w, h, 0, true)
	if c := welcomeCache; c != nil {
		return c.hits
	}
	return welcomeHits{} // w or h <= 0: nothing drawn, nothing to hit
}

// welcomeRecentHits returns the recents hit map of the welcome page.
func welcomeRecentHits(w, h int) []welcomeRecentHit {
	return welcomeLayoutHits(w, h).recents
}

// welcomeQuickActionHits returns the START rows.
func welcomeQuickActionHits(w, h int) []welcomeQuickActionHit {
	return welcomeLayoutHits(w, h).actions
}

// welcomeRows returns the interactive rows of the welcome page at w×h in
// cursor order (START, setup, recent files, workspaces).
func welcomeRows(w, h int) []welcomeRowHit {
	return welcomeLayoutHits(w, h).rows
}

// ─── START actions ─────────────────────────────────────────────────────

// quickAction is one START row.
type quickAction struct {
	icon     string // tile glyph
	iconTok  theme.Color256
	title    string
	subtitle string // tile second line
	shortcut string
	action   keymap.Action
	// id names a row that has no keymap action (action == ActionNone);
	// runWelcomeCard dispatches it directly. "clone" = Clone repository.
	id string
}

func welcomeQuickActions() []quickAction {
	return []quickAction{
		{icon: "+", iconTok: theme.AccentBlue, title: "Open file", subtitle: "browse your filesystem", shortcut: "Ctrl+P", action: keymap.ActionQuickOpen},
		{icon: ">", iconTok: theme.AccentGreen, title: "Command palette", subtitle: "run any action", shortcut: "F1", action: keymap.ActionCommandPalette},
		{icon: "?", iconTok: theme.AccentAmber, title: "Find in files", subtitle: "project-wide search", shortcut: "F8", action: keymap.ActionWorkspaceSearch},
		{icon: "$", iconTok: theme.AccentMagenta, title: "New terminal", subtitle: "integrated terminal", shortcut: "Ctrl+T", action: keymap.ActionToggleTerminal},
		{icon: "~", iconTok: theme.AccentLavender, title: "Open workspace", subtitle: "recent folders", id: welcomeCardWorkspace},
		{icon: "↓", iconTok: theme.AccentRedCoral, title: "Clone repository", subtitle: "git clone, then open it", id: welcomeCardClone},
	}
}

// ─── Render entry point ────────────────────────────────────────────────

// renderWelcomeFull returns the page and the recent / workspace / START
// hits (tests).
func renderWelcomeFull(w, h, focusIdx int) (string, []welcomeRecentHit, []welcomeWorkspaceHit, []welcomeQuickActionHit) {
	out, hits := renderWelcomeHits(w, h, focusIdx, true)
	return out, hits.recents, hits.workspaces, hits.actions
}

// welcomeLayout is one rung of the shrink ladder: what the page draws.
type welcomeLayout struct {
	recentMax, wsMax int
	pixel            bool // pixel wordmark (else the text one)
	allWs            bool // "All workspaces…" row
	hintBlank        bool // blank row above the hint bar
	hint             bool
	showRecent       bool
	showWs           bool
	tiles            int // START as tiles in this many columns (0 = one-line rows)
}

// welcomeLadder is the shrink order (§5.4 of the start page design):
// each rung drops a little more, the first that fits wins.
func welcomeLadder(pixelOK bool) []welcomeLayout {
	l := welcomeLayout{recentMax: 5, wsMax: 3, pixel: pixelOK, allWs: true, hintBlank: true, hint: true, showRecent: true, showWs: true}
	out := []welcomeLayout{l}
	step := func(f func(*welcomeLayout)) { f(&l); out = append(out, l) }
	step(func(l *welcomeLayout) { l.recentMax, l.wsMax = 3, 2 })
	step(func(l *welcomeLayout) { l.pixel = false })
	step(func(l *welcomeLayout) { l.allWs = false })
	step(func(l *welcomeLayout) { l.recentMax = 2 })
	step(func(l *welcomeLayout) { l.hintBlank = false })
	step(func(l *welcomeLayout) { l.hint = false })
	step(func(l *welcomeLayout) { l.showWs = false })
	step(func(l *welcomeLayout) { l.showRecent = false })
	return out
}

// welcomeData is what one render shows (read once per render).
type welcomeData struct {
	actions    []quickAction
	card       setupCardView
	recents    []RecentEntry
	workspaces []WorkspaceEntry
	// secretsNote: the secrets are still in the plain file and the card
	// was put off three times (secrets_migrate.go).
	secretsNote bool
}

// welcomeColumn is the content column: width W and left pad in a pane
// w cells wide.
func welcomeColumn(w int) (W, leftPad int) {
	W = min(welcomeMaxW, w-4)
	if W < 1 {
		W = w
	}
	return W, (w - W) / 2
}

// renderWelcomeHits renders the page (with the setup card when
// welcomeSetup shows one) and returns every click area. It walks the
// shrink ladder until the page fits h.
func renderWelcomeHits(w, h, focusIdx int, focused bool) (string, welcomeHits) {
	if w <= 0 || h <= 0 {
		welcomeLogoAt = welcomeLogoRect{}
		return "", welcomeHits{}
	}
	d := welcomeData{actions: welcomeQuickActions(), card: welcomeSetup, recents: loadRecents(), workspaces: loadWorkspaces(),
		secretsNote: welcomeSecretsNote}
	W, _ := welcomeColumn(w)
	pixelOK := dashboardLogo != "off" && h >= welcomePixelMinH && welcomeLogo(W) != nil
	var lines []string
	var hits welcomeHits
	var logo welcomeLogoRect
	fit := false
	// START as tiles first: two columns, then one; a rung is accepted
	// only while the recent files and workspaces stay on the page.
	for _, cols := range welcomeTileModes(W) {
		for _, lay := range welcomeLadder(pixelOK) {
			if !lay.showRecent || !lay.showWs {
				continue
			}
			lay.tiles = cols
			lines, hits, logo = renderWelcomePage(w, h, focusIdx, focused, lay, d)
			if fit = len(lines) <= h; fit {
				break
			}
		}
		if fit {
			break
		}
	}
	if !fit { // the calm one-line rows
		for _, lay := range welcomeLadder(pixelOK) {
			lines, hits, logo = renderWelcomePage(w, h, focusIdx, focused, lay, d)
			if len(lines) <= h {
				break
			}
		}
	}
	welcomeLogoAt = logo
	if W < welcomeMinHitW {
		// A sliver of a pane (the AI panel dragged wide): the rows are
		// cut to a few letters, so they are not offered as buttons.
		hits = welcomeHits{}
	}
	if len(lines) > h {
		lines = lines[:h] // below the ladder: clipped (hits past h are ignored)
	}
	bg := theme.Bg(theme.BgEditor)
	for len(lines) < h {
		lines = append(lines, bg.Render(strings.Repeat(" ", w)))
	}
	return strings.Join(lines, "\n"), hits
}

// renderWelcomePage lays the page out with lay: every line is w cells
// wide; the result may be taller than h (the ladder then tries the next
// rung).
func renderWelcomePage(w, h, focusIdx int, focused bool, lay welcomeLayout, d welcomeData) ([]string, welcomeHits, welcomeLogoRect) {
	W, leftPad := welcomeColumn(w)
	bg := theme.Bg(theme.BgEditor)
	left := bg.Render(strings.Repeat(" ", leftPad))
	right := bg.Render(strings.Repeat(" ", max(w-W-leftPad, 0)))
	var lines []string
	var hits welcomeHits
	var logo welcomeLogoRect
	add := func(s string) { lines = append(lines, left+padRowToWidth(s, W, bg)+right) }
	gap := func() { add(blank(W)) }

	// The keyboard cursor runs over every interactive row in reading
	// order; row adds one and says how to draw it.
	cursorSt := rowCursorFocused
	if !focused {
		cursorSt = rowCursorBlurred
	}
	row := func(id string) welcomeRowState {
		hits.rows = append(hits.rows, welcomeRowHit{Row: len(lines), ColStart: leftPad, ColEnd: leftPad + W, ID: id})
		if len(hits.rows)-1 == focusIdx {
			return cursorSt
		}
		return rowRest
	}

	top := 1
	if h >= welcomePixelMinH {
		top = 2
	}
	for i := 0; i < top; i++ {
		gap()
	}

	// ── Wordmark ──────────────────────────────────────────────────────
	if lay.pixel {
		mark := welcomeLogo(W)
		lw := textWidth(mark[0])
		logo = welcomeLogoRect{ok: true, x: leftPad + (W-lw)/2, y: len(lines), cols: lw, rows: len(mark)}
		for _, m := range mark {
			add(centerInWidth(m, W))
		}
		gap()
		add(centerInWidth(theme.FgBg(theme.TextMuted, theme.BgEditor).Render(spaceLetters("A TERMINAL-NATIVE EDITOR")), W))
		add(centerInWidth(heroRule(lw/2), W))
		gap()
	} else if dashboardLogo != "off" {
		add(centerInWidth(theme.FgBg(theme.TextSecondary, theme.BgEditor).Render(spaceLetters("TERMOCODE")), W))
		gap()
	}

	// ── START ─────────────────────────────────────────────────────────
	add(sectionHeader("START"))
	if lay.tiles > 0 {
		hits.tileCols = lay.tiles
		cols := lay.tiles
		cardW, tileH := welcomeTileSize(W, cols)
		for r0 := 0; r0 < len(d.actions); r0 += cols {
			var bodies [][]string
			for i := r0; i < min(r0+cols, len(d.actions)); i++ {
				a := d.actions[i]
				c0 := (i - r0) * (cardW + welcomeTileGap)
				hits.rows = append(hits.rows, welcomeRowHit{Row: len(lines), ColStart: leftPad + c0, ColEnd: leftPad + c0 + cardW,
					ID: "card:" + itoa(i), H: tileH})
				st := rowRest
				if len(hits.rows)-1 == focusIdx {
					st = cursorSt
				}
				hits.actions = append(hits.actions, welcomeQuickActionHit{Row: len(lines), ColStart: leftPad + c0, ColEnd: leftPad + c0 + cardW, Action: a.action, Index: i})
				bodies = append(bodies, renderWelcomeTile(a, cardW, tileH, st))
			}
			for k := 0; k < tileH; k++ {
				var sb strings.Builder
				for ci, body := range bodies {
					if ci > 0 {
						sb.WriteString(bg.Render(strings.Repeat(" ", welcomeTileGap)))
					}
					sb.WriteString(body[k])
				}
				add(sb.String())
			}
		}
	} else {
		for i, a := range d.actions {
			st := row("card:" + itoa(i))
			r := welcomeRow{id: "card:" + itoa(i), title: a.title, right: a.shortcut, rightIsKeycap: true, interactive: true}
			hits.actions = append(hits.actions, welcomeQuickActionHit{Row: len(lines), ColStart: leftPad, ColEnd: leftPad + W, Action: a.action, Index: i})
			add(renderWelcomeRow(r, W, st))
		}
	}

	// ── SET UP (setup_card_render.go) ─────────────────────────────────
	if d.card.show {
		gap()
		for _, l := range setupCardLines(d.card, W) {
			if l.row == nil {
				add(l.text(W))
				continue
			}
			st := rowRest
			if l.row.interactive {
				hits.setup = append(hits.setup, welcomeSetupHit{Row: len(lines), ColStart: leftPad, ColEnd: leftPad + W,
					ID: strings.TrimPrefix(l.row.id, "setup:")})
				st = row(l.row.id)
			}
			add(renderWelcomeRow(*l.row, W, st))
		}
	}

	// ── RECENT FILES ──────────────────────────────────────────────────
	if lay.showRecent && len(d.recents) > 0 {
		gap()
		add(sectionHeader("RECENT FILES"))
		for _, r := range welcomeRecentRows(d.recents, lay.recentMax, W) {
			path := strings.TrimPrefix(r.id, "recent:")
			hits.recents = append(hits.recents, welcomeRecentHit{Row: len(lines), Path: path, ColStart: leftPad, ColEnd: leftPad + W})
			add(renderWelcomeRow(r, W, row(r.id)))
		}
	}

	// ── WORKSPACES ────────────────────────────────────────────────────
	if lay.showWs && len(d.workspaces) > 0 {
		gap()
		add(sectionHeader("WORKSPACES"))
		for _, r := range welcomeWorkspaceRows(d.workspaces, lay.wsMax, lay.allWs, W) {
			path := strings.TrimPrefix(r.id, "workspace:")
			hits.workspaces = append(hits.workspaces, welcomeWorkspaceHit{Row: len(lines), Path: path, ColStart: leftPad, ColEnd: leftPad + W})
			add(renderWelcomeRow(r, W, row(r.id)))
		}
	}

	// ── The secrets note (one line, only with the hint bar) ───────────
	if lay.hint && d.secretsNote {
		gap()
		add(theme.FgBg(theme.TextMuted, theme.BgEditor).Render(ansi.Truncate(welcomeSecretsNoteText, W, "…")))
	}

	// ── Hint bar ──────────────────────────────────────────────────────
	if lay.hint {
		if lay.hintBlank {
			gap()
		}
		add(welcomeHintBar(W))
	}
	return lines, hits, logo
}

// welcomeRecentRows are the RECENT FILES rows: up to n files, then the
// "All N recent files…" row when there are more.
func welcomeRecentRows(entries []RecentEntry, n, W int) []welcomeRow {
	now := clock.Now()
	home, _ := os.UserHomeDir()
	narrow := W < welcomeNarrowMin
	var out []welcomeRow
	for i := 0; i < len(entries) && i < n; i++ {
		e := entries[i]
		g, c := theme.FileIconFor(e.Path, false, false)
		r := welcomeRow{id: "recent:" + e.Path, glyph: g, glyphTok: c, title: filepath.Base(e.Path),
			right: formatTimeShort(e.OpenedAt, now), rightDim: true, interactive: true}
		if !narrow {
			r.titleW = 20
			r.status, r.statusLeft = homeShort(filepath.Dir(e.Path), home), true
		}
		out = append(out, r)
	}
	if len(entries) > n {
		title := fmt.Sprintf("All %d recent files…", len(entries))
		if narrow {
			title = "All recent…"
		}
		out = append(out, welcomeRow{id: "recent:" + welcomeSentinelViewAll, title: title, titleMuted: true,
			right: "Ctrl+R", rightIsKeycap: true, interactive: true})
	}
	return out
}

// welcomeWorkspaceRows are the WORKSPACES rows: up to n folders, then
// "All workspaces…" when all is set.
func welcomeWorkspaceRows(entries []WorkspaceEntry, n int, all bool, W int) []welcomeRow {
	now := clock.Now()
	home, _ := os.UserHomeDir()
	narrow := W < welcomeNarrowMin
	var out []welcomeRow
	for i := 0; i < len(entries) && i < n; i++ {
		e := entries[i]
		name := filepath.Base(e.Path)
		if name == "" || name == "/" || name == "." {
			name = e.Path
		}
		g, c := theme.FileIconFor(e.Path, true, false)
		r := welcomeRow{id: "workspace:" + e.Path, glyph: g, glyphTok: c, title: name,
			right: formatTimeShort(e.OpenedAt, now), rightDim: true, interactive: true}
		if !narrow {
			r.titleW = 20
			r.status, r.statusLeft = homeShort(e.Path, home), true
		}
		out = append(out, r)
	}
	if all {
		out = append(out, welcomeRow{id: "workspace:" + welcomeSentinelBrowseAll, title: "All workspaces…", titleMuted: true, interactive: true})
	}
	return out
}

// homeShort writes dir with "~" for the home directory.
func homeShort(dir, home string) string {
	if home != "" && (dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

// ─── Section header / hint bar ─────────────────────────────────────────

// sectionHeader renders a section header ("START", "RECENT FILES") in
// plain bold caps and secondary text: the one bold text of its section.
func sectionHeader(s string) string {
	return theme.FgBg(theme.TextSecondary, theme.BgEditor).Bold(true).Render(s)
}

// welcomeHintBar is the last row: key hints on the left, the version on
// the right (dropped first when the row is short).
func welcomeHintBar(W int) string {
	bgC := theme.LG(theme.BgEditor)
	bg := theme.Bg(theme.BgEditor)
	pairs := []string{"↑↓", "move", "Enter", "open", "F1", "commands"}
	label := lipgloss.NewStyle().Background(bgC).Foreground(theme.Float().Dim)
	bar := func(pairs []string, gap int) string {
		var b strings.Builder
		for i := 0; i+1 < len(pairs); i += 2 {
			if i > 0 {
				b.WriteString(bg.Render(strings.Repeat(" ", gap)))
			}
			b.WriteString(theme.Keycap(pairs[i], bgC))
			b.WriteString(label.Render(" " + pairs[i+1]))
		}
		return b.String()
	}
	hint := ""
	for n := len(pairs); n >= 2 && hint == ""; n -= 2 {
		for _, gap := range []int{3, 2} {
			if s := bar(pairs[:n], gap); textWidth(s) <= W {
				hint = s
				break
			}
		}
	}
	ver := theme.Readable(theme.TextDim, theme.BgEditor, 3.0)
	v := lipgloss.NewStyle().Background(bgC).Foreground(ver).Render(welcomeVersion())
	if space := W - 1 - textWidth(hint) - textWidth(v); space >= 2 {
		return hint + bg.Render(strings.Repeat(" ", space)) + v + bg.Render(" ")
	}
	return padRowToWidth(hint, W, bg)
}

// welcomeVersion is "v<version>" from the build info ("v0.0.0" for a
// development build).
func welcomeVersion() string {
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
	return ver
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

// ─── Helpers ───────────────────────────────────────────────────────────

// formatTimeShort renders a compact relative time:
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

// spaceLetters puts a space between the letters ("T E R M"): the wordmark
// and its tagline are the only letter-spaced text.
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

// centerInWidth pads s so its visible width equals w, centered.
func centerInWidth(s string, w int) string {
	cur := textWidth(s)
	if cur >= w {
		return s
	}
	pad := w - cur
	left := pad / 2
	bg := theme.Bg(theme.BgEditor)
	return bg.Render(strings.Repeat(" ", left)) + s + bg.Render(strings.Repeat(" ", pad-left))
}

// padRowToWidth right-pads a styled row to width w with bg-styled cells.
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
