package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── RUN AND DEBUG sidebar (Group D) ──────────────────────────────────────
//
// Same accordion pattern as Source Control (git_panel.go): one flattened
// row list shared by the renderer, keyboard navigation and mouse
// hit-testing. Fixed chrome on top:
//
//	row 0  R U N   A N D   D E B U G
//	row 1  ▔▔▔▔ hairline
//	row 2  toolbar — idle: [▶ Start] [config ▾] … [⚙]
//	                 live: ▶/‖  ↷  ↓  ↑  ↻  ■   status
//	row 3  spacer
//
// Body sections: CONFIGURATIONS, VARIABLES, WATCH, CALL STACK, BREAKPOINTS.
// Information architecture follows mobocode's debug_panel.dart.

type runSection int

const (
	runSecConfigs runSection = iota
	runSecVariables
	runSecWatch
	runSecStack
	runSecBreakpoints
)

var runSectionOrder = []runSection{runSecConfigs, runSecVariables, runSecWatch, runSecStack, runSecBreakpoints}

func (s runSection) label() string {
	switch s {
	case runSecConfigs:
		return "CONFIGURATIONS"
	case runSecVariables:
		return "VARIABLES"
	case runSecWatch:
		return "WATCH"
	case runSecStack:
		return "CALL STACK"
	}
	return "BREAKPOINTS"
}

type runRowKind int

const (
	runRowSection runRowKind = iota
	runRowConfig
	runRowScope
	runRowVar
	runRowWatch
	runRowWatchAdd
	runRowThread
	runRowFrame
	runRowBreakpoint
	runRowNote   // dim text (not selectable)
	runRowAction // dim text with an action (selectable): hint / create launch.json
	runRowSpacer
)

// runRow is one rendered line of the body.
type runRow struct {
	kind    runRowKind
	section runSection
	depth   int

	label, detail string
	glyph         string
	glyphFG       int
	labelFG       int
	detailFG      int
	bold          bool

	key        string // tree key (scope / var) or config name
	ref        int    // variablesReference
	expandable bool
	expanded   bool
	index      int    // config / watch index
	path       string // breakpoint / frame file
	line       int
	frameID    int
	action     string // runRowAction: "install" | "launch"
	count      int    // section header count
	hasCount   bool
}

func (r runRow) selectable() bool {
	return r.kind != runRowNote && r.kind != runRowSpacer
}

// runHeaderRows is the fixed chrome height above the accordion.
const runHeaderRows = 4

// runToolbarRow is the screen row of the toolbar.
const runToolbarRow = 2

// workspaceRoot is the cwd (termocode runs from the workspace root).
func workspaceRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// runConfigs returns the merged config list (cached, stat-throttled).
func (d *debugState) runConfigs() []launchConfig {
	if d == nil {
		return nil
	}
	return d.configs.get(workspaceRoot(), time.Now())
}

// selectedConfig resolves the selected config: by name, else the first
// debuggable one, else the first. ok=false when the list is empty.
func (d *debugState) selectedConfig() (launchConfig, int, bool) {
	list := d.runConfigs()
	if len(list) == 0 {
		return launchConfig{}, -1, false
	}
	for i, c := range list {
		if c.Name == d.selected {
			return c, i, true
		}
	}
	for i, c := range list {
		if c.Debuggable() {
			return c, i, true
		}
	}
	return list[0], 0, true
}

// runRows flattens the accordion.
func (m Model) runRows() []runRow {
	d := m.debug
	if d == nil {
		d = newDebugState()
	}
	snap := d.snap
	var rows []runRow
	section := func(s runSection, count int, hasCount bool) bool {
		if len(rows) > 0 {
			rows = append(rows, runRow{kind: runRowSpacer})
		}
		open := !d.collapsed[s]
		rows = append(rows, runRow{kind: runRowSection, section: s, expanded: open, count: count, hasCount: hasCount})
		return open
	}
	note := func(s runSection, text string) {
		rows = append(rows, runRow{kind: runRowNote, section: s, label: text, labelFG: theme.TextMuted})
	}

	// CONFIGURATIONS
	cfgs := d.runConfigs()
	if section(runSecConfigs, len(cfgs), true) {
		if d.hint != "" {
			rows = append(rows, runRow{kind: runRowAction, section: runSecConfigs, action: "install",
				glyph: "⚠", glyphFG: theme.DiagWarning, label: d.hint, labelFG: theme.DiagWarning})
		}
		if d.configs.err != "" {
			note(runSecConfigs, d.configs.err)
		}
		_, sel, _ := d.selectedConfig()
		for i, c := range cfgs {
			r := runRow{kind: runRowConfig, section: runSecConfigs, index: i, key: c.Name,
				label: c.Name, labelFG: theme.TextPrimary, detailFG: theme.TextDim}
			if c.Debuggable() {
				r.glyph, r.glyphFG = "▶", theme.AccentGreen
			} else {
				r.glyph, r.glyphFG = "›", theme.TextMuted
			}
			if c.Source == "launch.json" {
				r.detail = "launch.json"
			} else {
				r.detail = "auto"
			}
			if i == sel {
				r.bold = true
				r.label = c.Name
				r.labelFG = theme.TextWhite
			}
			rows = append(rows, r)
		}
		if len(cfgs) == 0 {
			rows = append(rows, runRow{kind: runRowAction, section: runSecConfigs, action: "launch",
				glyph: "+", glyphFG: theme.AccentBlue, label: "Create launch.json", labelFG: theme.TextSecondary})
		}
	}

	// VARIABLES
	if section(runSecVariables, 0, false) {
		switch {
		case !snap.Active:
			note(runSecVariables, "Not debugging")
		case !snap.Stopped:
			note(runSecVariables, "Running…")
		case len(snap.Scopes) == 0:
			note(runSecVariables, "Loading…")
		default:
			for _, sc := range snap.Scopes {
				key := scopeKey(sc.Name)
				open := d.scopeOpen(sc)
				rows = append(rows, runRow{kind: runRowScope, section: runSecVariables, key: key, ref: sc.Ref,
					label: sc.Name, labelFG: theme.TextSecondary, bold: true, expandable: true, expanded: open})
				if !open {
					continue
				}
				vars, loaded := sc.Variables, sc.Loaded
				if !loaded {
					vars, loaded = d.children[key]
				}
				rows = m.appendVarRows(rows, d, key, vars, loaded, 1)
			}
		}
	}

	// WATCH
	if section(runSecWatch, len(d.watches), len(d.watches) > 0) {
		for i, w := range d.watches {
			r := runRow{kind: runRowWatch, section: runSecWatch, index: i, label: w.Expr,
				labelFG: theme.SyntaxVariable, detailFG: theme.TextSecondary}
			switch {
			case !snap.Stopped:
				r.detail, r.detailFG = "not available", theme.TextDim
			case w.Err:
				r.detail, r.detailFG = w.Value, theme.DiagError
			case w.Value == "":
				r.detail, r.detailFG = "…", theme.TextDim
			default:
				r.detail = w.Value
			}
			rows = append(rows, r)
		}
		rows = append(rows, runRow{kind: runRowWatchAdd, section: runSecWatch, glyph: "+", glyphFG: theme.AccentBlue,
			label: "Add Expression", labelFG: theme.TextMuted})
	}

	// CALL STACK
	if section(runSecStack, len(snap.Frames), snap.Stopped && len(snap.Frames) > 0) {
		switch {
		case !snap.Active:
			note(runSecStack, "Not debugging")
		default:
			multi := len(snap.Threads) > 1
			for _, t := range snap.Threads {
				if multi {
					st := "running"
					if t.Stopped {
						st = "paused"
					}
					rows = append(rows, runRow{kind: runRowThread, section: runSecStack, label: t.Name,
						labelFG: theme.TextSecondary, detail: st, detailFG: theme.TextDim, glyph: "≡", glyphFG: theme.TextMuted})
				}
				if t.ID != snap.Thread {
					continue
				}
				rows = appendFrameRows(rows, snap, boolToInt(multi))
			}
			if len(snap.Threads) == 0 || (!multi && snap.Thread == 0) {
				if !snap.Stopped {
					note(runSecStack, "Running…")
				} else if len(snap.Threads) == 0 {
					rows = appendFrameRows(rows, snap, 0)
				}
			}
		}
	}

	// BREAKPOINTS
	bps := sortedBreakpointRows(d.bps, d.liveBps)
	if section(runSecBreakpoints, len(bps), true) {
		if len(bps) == 0 {
			note(runSecBreakpoints, "None — F9 toggles one at the cursor")
		}
		for _, b := range bps {
			glyph, fg := "●", theme.DiagError
			switch {
			case b.Verified != nil && !*b.Verified:
				glyph, fg = "○", theme.TextMuted
			case b.BP.LogMessage != "":
				glyph = "◇"
			case b.BP.Condition != "" || b.BP.HitCondition != "":
				glyph = "◆"
			}
			detail := relPathForDisplay(filepath.Dir(b.Path))
			switch {
			case b.BP.Condition != "":
				detail = "if " + b.BP.Condition
			case b.BP.LogMessage != "":
				detail = "log " + b.BP.LogMessage
			}
			rows = append(rows, runRow{kind: runRowBreakpoint, section: runSecBreakpoints,
				glyph: glyph, glyphFG: fg, label: fmt.Sprintf("%s:%d", filepath.Base(b.Path), b.BP.Line),
				labelFG: theme.TextPrimary, detail: detail, detailFG: theme.TextDim, path: b.Path, line: b.BP.Line})
		}
	}
	return rows
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func appendFrameRows(rows []runRow, snap dbgSnapshot, depth int) []runRow {
	if !snap.Stopped {
		return append(rows, runRow{kind: runRowNote, section: runSecStack, depth: depth, label: "Running…", labelFG: theme.TextMuted})
	}
	for _, f := range snap.Frames {
		r := runRow{kind: runRowFrame, section: runSecStack, depth: depth, frameID: f.ID,
			label: f.Name, labelFG: theme.TextPrimary, detailFG: theme.TextDim, path: f.Path, line: f.Line}
		src := f.Source
		if f.Path != "" {
			src = filepath.Base(f.Path)
		}
		if src != "" {
			r.detail = fmt.Sprintf("%s:%d", src, f.Line)
		}
		if f.ID == snap.Frame {
			r.glyph, r.glyphFG = "▶", theme.AccentAmber
			r.labelFG = theme.TextWhite
		} else {
			r.glyph = " "
		}
		if f.Path == "" {
			r.labelFG = theme.TextMuted
		}
		rows = append(rows, r)
	}
	return rows
}

// appendVarRows adds the rows of a variable list (recursively for open
// expandable variables).
func (m Model) appendVarRows(rows []runRow, d *debugState, parent string, vars []dbgVar, loaded bool, depth int) []runRow {
	if !loaded {
		if e, ok := d.childErr[parent]; ok {
			return append(rows, runRow{kind: runRowNote, section: runSecVariables, depth: depth, label: e, labelFG: theme.DiagError})
		}
		return append(rows, runRow{kind: runRowNote, section: runSecVariables, depth: depth, label: "Loading…", labelFG: theme.TextMuted})
	}
	if len(vars) == 0 {
		return append(rows, runRow{kind: runRowNote, section: runSecVariables, depth: depth, label: "(empty)", labelFG: theme.TextMuted})
	}
	for _, v := range vars {
		key := parent + "/" + v.Name
		open := v.Ref > 0 && d.open[key]
		rows = append(rows, runRow{kind: runRowVar, section: runSecVariables, depth: depth, key: key, ref: v.Ref,
			label: v.Name, labelFG: theme.SyntaxVariable, detail: v.Value, detailFG: theme.TextSecondary,
			expandable: v.Ref > 0, expanded: open})
		if open && depth < 12 {
			kids, ok := d.children[key]
			rows = m.appendVarRows(rows, d, key, kids, ok, depth+1)
		}
	}
	return rows
}

// relPathForDisplay shortens an absolute path relative to the workspace.
func relPathForDisplay(p string) string {
	if rel, err := filepath.Rel(workspaceRoot(), p); err == nil && !strings.HasPrefix(rel, "..") {
		if rel == "." {
			return ""
		}
		return rel
	}
	return p
}

// ── Rendering ────────────────────────────────────────────────────────────

func (m Model) renderRunSidebar(w, h int) string {
	focused := m.focus == FocusExplorer
	fill := theme.Bg(theme.BgSidebar)
	blank := fill.Render(strings.Repeat(" ", w))

	var lines []string
	title := " " + letterSpace("RUN AND DEBUG")
	if runewidth.StringWidth(title) > w {
		title = " RUN AND DEBUG"
	}
	lines = append(lines, padRunRow(theme.FgBg(theme.TextSecondary, theme.BgSidebar).Bold(true).Render(title), w))
	lines = append(lines, theme.FgBg(theme.BorderSubtle, theme.BgSidebar).Render(strings.Repeat("▔", maxInt(w, 0))))
	lines = append(lines, m.renderRunToolbar(w))
	lines = append(lines, blank)

	rows := m.runRows()
	cursor := 0
	if m.debug != nil {
		cursor = m.debug.cursor
	}
	top, bodyH := runLayout(len(rows), cursor, h)
	for i := top; i < len(rows) && len(lines) < runHeaderRows+bodyH; i++ {
		lines = append(lines, renderRunRow(rows[i], i == cursor, focused, w))
	}
	for len(lines) < h {
		lines = append(lines, blank)
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	if hy := m.gitHoverLine(); hy >= runHeaderRows && hy < len(lines) && hy != runHeaderRows+cursor-top {
		lines[hy] = sidebarHoverLineBg(lines[hy])
	}
	return strings.Join(lines, "\n")
}

// runLayout returns the scroll offset keeping the cursor visible and the
// body height. Shared by the renderer and the mouse hit-test.
func runLayout(n, cursor, h int) (top, bodyH int) {
	bodyH = h - runHeaderRows
	if bodyH < 1 {
		bodyH = 1
	}
	if cursor >= bodyH {
		top = cursor - bodyH + 1
	}
	if maxTop := n - bodyH; top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	return top, bodyH
}

// padRunRow pads / clips a styled row to w cells on the sidebar bg.
func padRunRow(s string, w int) string {
	return padTabBarToWidth(s, w, theme.Bg(theme.BgSidebar))
}

func renderRunRow(r runRow, active, focused bool, w int) string {
	bg := theme.BgSidebar
	if active && r.selectable() {
		bg = theme.BgInactiveSel
		if focused {
			bg = theme.BgSelection
		}
	}
	rowSt := theme.Bg(bg)
	var b strings.Builder
	used := 0
	write := func(s string, st func(...string) string) {
		b.WriteString(st(s))
		used += runewidth.StringWidth(s)
	}
	plain := func(s ...string) string { return rowSt.Render(s...) }

	if r.kind == runRowSpacer {
		return rowSt.Render(strings.Repeat(" ", w))
	}
	// Lead: accent bar on the focused cursor row.
	if active && focused && r.selectable() {
		write("▌", theme.FgBg(theme.AccentBlue, bg).Render)
	} else {
		write(" ", plain)
	}

	if r.kind == runRowSection {
		chev := "▾"
		if !r.expanded {
			chev = "▸"
		}
		write(chev+" ", theme.FgBg(theme.TextMuted, bg).Render)
		write(r.section.label(), theme.FgBg(theme.TextSecondary, bg).Bold(true).Render)
		chip := ""
		if r.hasCount {
			chip = " " + strconv.Itoa(r.count) + " "
		}
		ruleW := w - used - 1 - runewidth.StringWidth(chip) - 1
		if ruleW >= 1 {
			write(" ", plain)
			write(strings.Repeat("─", ruleW), theme.FgBg(theme.BorderSubtle, bg).Render)
		}
		if chip != "" && used+runewidth.StringWidth(chip)+1 <= w {
			write(" ", plain)
			write(chip, theme.FgBg(theme.TextSecondary, theme.BgHover).Bold(true).Render)
		}
		return clampRunRow(b.String(), used, w, rowSt)
	}

	write(strings.Repeat("  ", r.depth), plain)
	switch {
	case r.expandable:
		chev := "▸"
		if r.expanded {
			chev = "▾"
		}
		write(chev+" ", theme.FgBg(theme.TextMuted, bg).Render)
	case r.glyph != "":
		write(r.glyph+" ", theme.FgBg(r.glyphFG, bg).Render)
	case r.kind == runRowVar:
		write("  ", plain)
	}

	labelSt := theme.FgBg(r.labelFG, bg)
	if r.bold {
		labelSt = labelSt.Bold(true)
	}
	label := r.label
	detail := r.detail
	sep := "  "
	if r.kind == runRowVar || r.kind == runRowWatch {
		sep = ": "
	}
	avail := w - used
	if detail == "" {
		write(runewidth.Truncate(label, maxInt(avail, 0), "…"), labelSt.Render)
	} else {
		// Label keeps at least half the room; the detail gets the rest.
		lw := runewidth.StringWidth(label)
		maxLabel := avail - runewidth.StringWidth(sep) - 4
		if maxLabel < avail/2 {
			maxLabel = avail / 2
		}
		if lw > maxLabel {
			label = runewidth.Truncate(label, maxInt(maxLabel, 1), "…")
		}
		write(label, labelSt.Render)
		rest := w - used - runewidth.StringWidth(sep)
		if rest >= 2 {
			write(sep, theme.FgBg(theme.TextDim, bg).Render)
			detail = strings.ReplaceAll(detail, "\n", "↵")
			write(runewidth.Truncate(detail, rest, "…"), theme.FgBg(r.detailFG, bg).Render)
		}
	}
	return clampRunRow(b.String(), used, w, rowSt)
}

func clampRunRow(s string, used, w int, st interface{ Render(...string) string }) string {
	if used < w {
		return s + st.Render(strings.Repeat(" ", w-used))
	}
	if used > w {
		return truncRightVisual(s, w)
	}
	return s
}

// ── Toolbar ──────────────────────────────────────────────────────────────

// runButton is one clickable toolbar segment ([start, end) columns).
type runButton struct {
	id         string
	label      string
	start, end int
}

// runToolbarLayout lays the toolbar out for width w. Pure for tests and
// hit-testing.
func runToolbarLayout(active, stopped bool, configName string, w int) []runButton {
	var out []runButton
	col := 1
	add := func(id, label string) bool {
		lw := runewidth.StringWidth(label)
		if col+lw > w {
			return false
		}
		out = append(out, runButton{id: id, label: label, start: col, end: col + lw})
		col += lw + 1
		return true
	}
	if !active {
		const gear = " ⚙ "
		limit := w - runewidth.StringWidth(gear) - 1
		add("start", " ▶ Start ")
		name := configName
		if name == "" {
			name = "No configuration"
		}
		room := limit - col - 3
		if room >= 4 {
			name = runewidth.Truncate(name, room, "…")
			lbl := " " + name + " ▾"
			lw := runewidth.StringWidth(lbl) + 1
			out = append(out, runButton{id: "config", label: lbl + " ", start: col, end: col + lw})
			col += lw + 1
		}
		if w-runewidth.StringWidth(gear) > col-1 {
			s := w - runewidth.StringWidth(gear)
			out = append(out, runButton{id: "launch", label: gear, start: s, end: w})
		}
		return out
	}
	if stopped {
		add("continue", "▶")
	} else {
		add("pause", "‖")
	}
	add("over", "↷")
	add("into", "↓")
	add("out", "↑")
	add("restart", "↻")
	add("stop", "■")
	add("console", "≡")
	return out
}

func (m Model) renderRunToolbar(w int) string {
	d := m.debug
	if d == nil {
		d = newDebugState()
	}
	cfg, _, _ := d.selectedConfig()
	active := m.dapSessionActive
	btns := runToolbarLayout(active, d.snap.Stopped, cfg.Name, w)
	bg := theme.BgSidebar
	var b strings.Builder
	col := 0
	for _, bt := range btns {
		if bt.start > col {
			b.WriteString(theme.Bg(bg).Render(strings.Repeat(" ", bt.start-col)))
		}
		var st = theme.FgBg(theme.AccentBlue, bg)
		switch bt.id {
		case "start":
			st = theme.FgBg(theme.TextWhite, theme.AccentGreen).Bold(true)
		case "config":
			st = theme.FgBg(theme.TextPrimary, theme.BgHover)
		case "launch":
			st = theme.FgBg(theme.TextMuted, bg)
		case "continue":
			st = theme.FgBg(theme.AccentGreen, bg).Bold(true)
		case "stop":
			st = theme.FgBg(theme.DiagError, bg).Bold(true)
		case "restart", "console":
			st = theme.FgBg(theme.TextSecondary, bg)
		}
		b.WriteString(st.Render(bt.label))
		col = bt.end
	}
	if active && d.status != "" && col+2 < w {
		status := runewidth.Truncate(d.status, w-col-2, "…")
		fg := theme.TextMuted
		if d.snap.Stopped {
			fg = theme.AccentAmber
		}
		b.WriteString(theme.Bg(bg).Render("  "))
		b.WriteString(theme.FgBg(fg, bg).Render(status))
		col += 2 + runewidth.StringWidth(status)
	}
	if col < w {
		b.WriteString(theme.Bg(bg).Render(strings.Repeat(" ", w-col)))
	}
	return padRunRow(b.String(), w)
}

// ── Navigation & actions ─────────────────────────────────────────────────

func (m *Model) debugClampCursor() {
	d := m.ensureDebug()
	rows := m.runRows()
	if d.cursor >= len(rows) {
		d.cursor = len(rows) - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
	if len(rows) > 0 && !rows[d.cursor].selectable() {
		d.cursor = runNearestSelectable(rows, d.cursor)
	}
}

func runNearestSelectable(rows []runRow, idx int) int {
	for off := 0; off < len(rows); off++ {
		if i := idx - off; i >= 0 && rows[i].selectable() {
			return i
		}
		if i := idx + off; i < len(rows) && rows[i].selectable() {
			return i
		}
	}
	return 0
}

func (m *Model) runMoveCursor(dir int) {
	d := m.ensureDebug()
	rows := m.runRows()
	for i := d.cursor + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].selectable() {
			d.cursor = i
			return
		}
	}
}

// runSetOpen opens / closes an expandable row or section.
func (m *Model) runSetOpen(r runRow, open bool) {
	d := m.ensureDebug()
	switch r.kind {
	case runRowSection:
		d.collapsed[r.section] = !open
	case runRowScope, runRowVar:
		d.open[r.key] = open
		if open {
			m.debugRequestOpenChildren()
		}
	}
	m.debugClampCursor()
}

// runActivate is Enter / click on a row.
func (m *Model) runActivate(r runRow, viaMouse bool) tea.Cmd {
	d := m.ensureDebug()
	switch r.kind {
	case runRowSection:
		m.runSetOpen(r, d.collapsed[r.section])
	case runRowScope, runRowVar:
		if r.expandable {
			m.runSetOpen(r, !r.expanded)
		}
	case runRowConfig:
		d.selected = r.key
		cfg, _, _ := d.selectedConfig()
		d.hint = adapterHintFor(cfg)
		if !viaMouse {
			return m.dapStart()
		}
	case runRowWatchAdd:
		m.openDebugWatchPrompt()
	case runRowFrame:
		m.debugSelectFrame(r.frameID)
		if r.path != "" {
			m.openFileAtLine(r.path, r.line)
		}
	case runRowBreakpoint:
		m.openFileAtLine(r.path, r.line)
	case runRowAction:
		switch r.action {
		case "install":
			return m.openToolManager(lspinstall.CategoryDAP)
		case "launch":
			return m.debugOpenLaunchJSON()
		}
	}
	return nil
}

// openFileAtLine opens path in the editor window at line (1-based).
func (m *Model) openFileAtLine(path string, line int) {
	if m.nvim == nil || path == "" {
		return
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`local p, l = ...
		vim.cmd('edit ' .. vim.fn.fnameescape(p))
		pcall(vim.api.nvim_win_set_cursor, 0, { math.max(l, 1), 0 })
		pcall(vim.cmd, 'normal! zz')`, path, line)
	m.focus = FocusEditor
}

// handleRunSidebarKey is the Run view's keymap (focus = sidebar).
func (m *Model) handleRunSidebarKey(msg tea.KeyMsg) tea.Cmd {
	d := m.ensureDebug()
	rows := m.runRows()
	m.debugClampCursor()
	var cur runRow
	if d.cursor < len(rows) {
		cur = rows[d.cursor]
	}
	switch msg.String() {
	case "down", "j":
		m.runMoveCursor(1)
	case "up", "k":
		m.runMoveCursor(-1)
	case "home", "g":
		d.cursor = 0
		m.debugClampCursor()
	case "end", "G":
		d.cursor = len(rows) - 1
		m.debugClampCursor()
	case "enter", " ":
		return m.runActivate(cur, false)
	case "right", "l":
		if cur.kind == runRowSection || cur.expandable {
			m.runSetOpen(cur, true)
		}
	case "left", "h":
		if cur.kind == runRowSection || cur.expanded {
			m.runSetOpen(cur, false)
		}
	case "x", "delete", "backspace":
		switch cur.kind {
		case runRowBreakpoint:
			cmd := m.debugRemoveBreakpoint(cur.path, cur.line)
			m.debugClampCursor()
			return cmd
		case runRowWatch:
			m.debugRemoveWatch(cur.index)
			m.debugClampCursor()
		}
	case "a":
		m.openDebugWatchPrompt()
	case "r":
		if cur.kind == runRowConfig {
			d.selected = cur.key
			return m.debugRunWithoutDebugging()
		}
	case "esc":
		m.focus = FocusEditor
	}
	return nil
}

// handleRunSidebarMouse handles clicks / wheel in the Run view. x is
// sidebar-local, y the screen row.
func (m *Model) handleRunSidebarMouse(x, y int, t tea.MouseEventType) tea.Cmd {
	d := m.ensureDebug()
	rows := m.runRows()
	switch t {
	case tea.MouseWheelUp:
		for i := 0; i < 3; i++ {
			m.runMoveCursor(-1)
		}
		return nil
	case tea.MouseWheelDown:
		for i := 0; i < 3; i++ {
			m.runMoveCursor(1)
		}
		return nil
	case tea.MouseLeft:
	default:
		return nil
	}
	m.focus = FocusExplorer
	contentW := m.explorerWidth - 1
	if y == runToolbarRow {
		cfg, _, _ := d.selectedConfig()
		for _, b := range runToolbarLayout(m.dapSessionActive, d.snap.Stopped, cfg.Name, contentW) {
			if x >= b.start && x < b.end {
				return m.runToolbarAction(b.id)
			}
		}
		return nil
	}
	if y < runHeaderRows {
		return nil
	}
	top, _ := runLayout(len(rows), d.cursor, m.h)
	idx := top + (y - runHeaderRows)
	if idx < 0 || idx >= len(rows) || !rows[idx].selectable() {
		return nil
	}
	d.cursor = idx
	return m.runActivate(rows[idx], true)
}

// runToolbarAction runs a toolbar button.
func (m *Model) runToolbarAction(id string) tea.Cmd {
	switch id {
	case "start", "continue":
		return m.dapStart()
	case "config":
		return m.openDebugConfigPicker()
	case "launch":
		return m.debugOpenLaunchJSON()
	case "pause":
		return m.dapPause()
	case "over":
		return m.dapStepOver()
	case "into":
		return m.dapStepInto()
	case "out":
		return m.dapStepOut()
	case "restart":
		return m.dapRestart()
	case "stop":
		return m.dapStop()
	case "console":
		m.showDebugConsole(true)
	}
	return nil
}

// openRunView shows the RUN AND DEBUG sidebar, optionally expanding and
// moving the cursor to a section.
func (m *Model) openRunView(sec runSection, focusSection bool) {
	d := m.ensureDebug()
	m.activity.SetActive(activity.ViewRun)
	m.showExp = true
	m.applyLayout()
	if focusSection {
		d.collapsed[sec] = false
		for i, r := range m.runRows() {
			if r.kind == runRowSection && r.section == sec {
				d.cursor = i
				break
			}
		}
		m.focus = FocusExplorer
	}
}

// hitStatusDebugBadge reports whether a click at (x, y) lands on the
// status-bar "● DEBUG" badge.
func (m Model) hitStatusDebugBadge(x, y int) bool {
	if m.zenMode || y != m.h-1 || !m.dapSessionActive {
		return false
	}
	bx, bw := m.statusBarRect()
	sb := m.status
	sb.SetWidth(bw)
	x0, x1, ok := sb.DebugBadgeSpan(m.statusState())
	return ok && x >= bx+x0 && x < bx+x1
}
