package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"termocode/internal/activity"
	"termocode/internal/statusbar"
	"termocode/internal/tests"
	"termocode/internal/theme"
)

// ── Testing sidebar view (Group E) ───────────────────────────────────────
//
// Layout (sidebar-local rows):
//
//	0  " T E S T I N G"
//	1  hairline
//	2  action bar: ▶ Run All  ↻ Failed  ⟳ Refresh  ⊟ Collapse   (■ Stop while running)
//	3  summary: ✓ n  ✗ n  ⊘ n  ○ n · 1.2s
//	4  spacer
//	5… tree: package → file → test, failed tests followed by their
//	   message lines; a footer hint row when the view has focus.
//
// Keys (sidebar focused): j/k ↑/↓ move · h/l ←/→ fold · Space toggle ·
// Enter open (failure line for failed tests) · r run item · o output ·
// R run all · f rerun failed · u refresh · c collapse all · x stop.

const (
	testsHeaderRows = 5
	testsActionRow  = 2
	testMsgMaxLines = 3
)

type testRowKind int

const (
	testRowPackage testRowKind = iota
	testRowFile
	testRowCase
	testRowMessage  // failure-message line under a failed test
	testRowPkgError // package-level error line (build failed, suite error)
)

type testRow struct {
	kind  testRowKind
	depth int
	pkg   *tests.Package
	file  *tests.File
	c     *tests.Case
	text  string
}

func testPkgKey(p *tests.Package) string { return "p:" + p.Dir }
func testFileKey(f *tests.File) string   { return "f:" + f.Path }

// firstLines returns up to n non-empty lines of s.
func firstLines(s string, n int) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			out = append(out, ln)
			if len(out) == n {
				break
			}
		}
	}
	return out
}

// rows flattens the tree into visible rows.
func (st *testsState) rows() []testRow {
	if st == nil || st.tree == nil {
		return nil
	}
	var out []testRow
	for _, p := range st.tree.Packages {
		out = append(out, testRow{kind: testRowPackage, pkg: p})
		if st.collapsed[testPkgKey(p)] {
			continue
		}
		for _, ln := range firstLines(p.Error, testMsgMaxLines) {
			out = append(out, testRow{kind: testRowPkgError, depth: 1, pkg: p, text: ln})
		}
		for _, f := range p.Files {
			out = append(out, testRow{kind: testRowFile, depth: 1, pkg: p, file: f})
			if st.collapsed[testFileKey(f)] {
				continue
			}
			for _, c := range f.Cases {
				out = append(out, testRow{kind: testRowCase, depth: 2, pkg: p, file: f, c: c})
				if c.Status == tests.StatusFailed {
					for _, ln := range firstLines(c.Message, testMsgMaxLines) {
						out = append(out, testRow{kind: testRowMessage, depth: 3, pkg: p, file: f, c: c, text: ln})
					}
				}
			}
		}
	}
	return out
}

// ── Entry points ─────────────────────────────────────────────────────────

// revealTestsView switches the sidebar to Testing (optionally focusing
// it). Callers that do not start a run follow up with
// ensureTestsDiscovered; runTestsRequest discovers on its own.
func (m *Model) revealTestsView(focus bool) {
	m.activity.SetActive(activity.ViewTests)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	if focus {
		m.focus = FocusExplorer
	}
}

// ensureTestsDiscovered runs discovery once.
func (m *Model) ensureTestsDiscovered() tea.Cmd {
	if st := m.testView; st != nil && !st.discovered {
		return m.refreshTests()
	}
	return nil
}

// testsCollapseAll folds every package.
func (m *Model) testsCollapseAll() {
	st := m.testView
	if st == nil || st.tree == nil {
		return
	}
	for _, p := range st.tree.Packages {
		st.collapsed[testPkgKey(p)] = true
	}
	st.cursor = 0
}

// testsClampCursor keeps the cursor on an existing row.
func (m *Model) testsClampCursor() {
	st := m.testView
	n := len(st.rows())
	if st.cursor >= n {
		st.cursor = n - 1
	}
	if st.cursor < 0 {
		st.cursor = 0
	}
}

// runTestRow runs the item a row stands for.
func (m *Model) runTestRow(r testRow) tea.Cmd {
	switch r.kind {
	case testRowPackage, testRowPkgError:
		return m.runTestsRequest(testsRunRequest{pkgDir: r.pkg.Dir})
	case testRowFile:
		return m.runTestsRequest(testsRunRequest{file: r.file.Path})
	case testRowCase, testRowMessage:
		return m.runTestsRequest(testsRunRequest{caseKey: []string{r.c.Key()}})
	}
	return nil
}

// openTestRow jumps to the row's location: the failure line for failed
// tests and their message rows, the declaration otherwise.
func (m *Model) openTestRow(r testRow) {
	switch r.kind {
	case testRowCase, testRowMessage:
		f, l := r.c.Location()
		m.openTestLocation(f, l)
	case testRowPkgError:
		if r.pkg.ErrFile != "" {
			m.openTestLocation(r.pkg.ErrFile, r.pkg.ErrLine)
		}
	case testRowFile:
		m.openTestLocation(r.file.Path, 1)
	}
}

// openTestLocation opens path at line in the editor and focuses it.
func (m *Model) openTestLocation(path string, line int) {
	if m.nvim == nil || path == "" {
		return
	}
	if line < 1 {
		line = 1
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`
		local p, l = ...
		vim.cmd('edit ' .. vim.fn.fnameescape(p))
		local last = vim.api.nvim_buf_line_count(0)
		pcall(vim.api.nvim_win_set_cursor, 0, { math.min(l, last), 0 })
		vim.cmd('normal! zz')
	`, path, line)
	m.focus = FocusEditor
}

// showTestOutput opens the TEST RESULTS tab, narrowed to one test when the
// row is a test (or its message), else showing the whole run log.
func (m *Model) showTestOutput(r *testRow) {
	st := m.testView
	st.resultsFor = ""
	if r != nil && r.c != nil {
		st.resultsFor = r.c.Key()
	}
	st.resultsScroll = 0
	m.showPanelTab(panelKindTests)
}

// ── Keyboard ─────────────────────────────────────────────────────────────

func (m Model) handleTestsSidebarKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	st := m.testView
	rows := st.rows()
	var cur *testRow
	if st.cursor >= 0 && st.cursor < len(rows) {
		cur = &rows[st.cursor]
	}
	move := func(d int) {
		st.cursor += d
		m.testsClampCursor()
	}
	setFold := func(collapse bool) bool {
		if cur == nil {
			return false
		}
		var key string
		switch cur.kind {
		case testRowPackage:
			key = testPkgKey(cur.pkg)
		case testRowFile:
			key = testFileKey(cur.file)
		default:
			return false
		}
		if st.collapsed[key] == collapse {
			return false
		}
		st.collapsed[key] = collapse
		return true
	}
	switch k.String() {
	case "up", "k":
		move(-1)
	case "down", "j":
		move(1)
	case "pgup":
		move(-10)
	case "pgdown":
		move(10)
	case "home", "g":
		st.cursor = 0
	case "end", "G":
		st.cursor = len(rows) - 1
		m.testsClampCursor()
	case "left", "h":
		if !setFold(true) && cur != nil {
			// Already folded / a leaf: go to the parent row.
			for i := st.cursor - 1; i >= 0; i-- {
				if rows[i].depth < cur.depth && rows[i].kind != testRowPkgError {
					st.cursor = i
					break
				}
			}
		}
	case "right", "l":
		setFold(false)
	case " ":
		if !setFold(true) {
			setFold(false)
		}
	case "enter":
		if cur == nil {
			return m, nil
		}
		if cur.kind == testRowPackage || (cur.kind == testRowFile && st.collapsed[testFileKey(cur.file)]) {
			if !setFold(true) {
				setFold(false)
			}
			return m, nil
		}
		m.openTestRow(*cur)
	case "r":
		if cur != nil {
			return m, m.runTestRow(*cur)
		}
	case "o":
		m.showTestOutput(cur)
	case "R":
		return m, m.runTestsRequest(testsRunRequest{all: true})
	case "f":
		return m, m.runTestsRequest(testsRunRequest{failed: true})
	case "u":
		return m, m.refreshTests()
	case "c":
		m.testsCollapseAll()
	case "x":
		m.stopTestRun()
	case "esc":
		m.focus = FocusEditor
	}
	return m, nil
}

// ── Mouse ────────────────────────────────────────────────────────────────

// testsAction is one clickable label on the action bar.
type testsAction struct {
	id, label  string
	start, end int // half-open columns
}

// testsActionLayout lays out the action bar for width w: text buttons on
// the left (Run All / Stop, Failed), icon buttons ⟳ refresh and ⊟ collapse
// pinned to the right edge (so they fit the default 30-col sidebar).
// Buttons that do not fit are dropped. Pure for hit-testing.
func testsActionLayout(running, hasFailed bool, w int) []testsAction {
	labels := [][2]string{{"run", "▶ Run All"}}
	if running {
		labels[0] = [2]string{"stop", "■ Stop"}
	}
	if hasFailed {
		labels = append(labels, [2]string{"failed", "↻ Failed"})
	}
	icons := [][2]string{{"refresh", "⟳"}, {"collapse", "⊟"}}
	iconStart := w - 2*len(icons)
	var out []testsAction
	col := 1
	for _, l := range labels {
		lw := runewidth.StringWidth(l[1])
		if col+lw > iconStart-1 {
			break
		}
		out = append(out, testsAction{id: l[0], label: l[1], start: col, end: col + lw})
		col += lw + 2
	}
	if iconStart >= col-1 && iconStart > 0 {
		for i, ic := range icons {
			s := iconStart + 2*i
			out = append(out, testsAction{id: ic[0], label: ic[1], start: s, end: s + 1})
		}
	}
	return out
}

func (m Model) handleTestsSidebarMouse(x, y int, t tea.MouseEventType) (tea.Model, tea.Cmd) {
	st := m.testView
	switch t {
	case tea.MouseWheelUp:
		st.cursor -= 3
		m.testsClampCursor()
		return m, nil
	case tea.MouseWheelDown:
		st.cursor += 3
		m.testsClampCursor()
		return m, nil
	case tea.MouseLeft:
	default:
		return m, nil
	}
	m.focus = FocusExplorer
	w := m.explorerWidth - 1
	if y == testsActionRow {
		c := st.tree.Counts()
		for _, a := range testsActionLayout(st.running, c.Failed > 0, w) {
			if x >= a.start && x < a.end {
				return m, m.testsAction(a.id)
			}
		}
		return m, nil
	}
	top, bodyH, _ := m.testsPanelLayout(m.h)
	bodyRow := y - testsHeaderRows
	if bodyRow < 0 || bodyRow >= bodyH {
		return m, nil
	}
	rows := st.rows()
	idx := top + bodyRow
	if idx < 0 || idx >= len(rows) {
		return m, nil
	}
	st.cursor = idx
	r := rows[idx]
	// The ▶ glyph at the right edge runs the row.
	if x >= w-3 && r.kind != testRowMessage {
		return m, m.runTestRow(r)
	}
	switch r.kind {
	case testRowPackage:
		st.collapsed[testPkgKey(r.pkg)] = !st.collapsed[testPkgKey(r.pkg)]
	case testRowFile:
		st.collapsed[testFileKey(r.file)] = !st.collapsed[testFileKey(r.file)]
	default:
		m.openTestRow(r)
	}
	return m, nil
}

// testsAction runs an action-bar / palette action by id.
func (m *Model) testsAction(id string) tea.Cmd {
	switch id {
	case "run":
		return m.runTestsRequest(testsRunRequest{all: true})
	case "stop":
		m.stopTestRun()
	case "failed":
		return m.runTestsRequest(testsRunRequest{failed: true})
	case "refresh":
		return m.refreshTests()
	case "collapse":
		m.testsCollapseAll()
	}
	return nil
}

// ── Rendering ────────────────────────────────────────────────────────────

// testsPanelLayout returns the scroll offset that keeps the cursor
// visible, the body height and the footer rows for sidebar height h.
func (m Model) testsPanelLayout(h int) (top, bodyH, footer int) {
	if m.focus == FocusExplorer && h >= testsHeaderRows+6 {
		footer = 1
	}
	bodyH = h - testsHeaderRows - footer
	if bodyH < 1 {
		bodyH = 1
	}
	st := m.testView
	n := len(st.rows())
	if st.cursor >= bodyH {
		top = st.cursor - bodyH + 1
	}
	if maxTop := n - bodyH; top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	return top, bodyH, footer
}

func testsBg() lipgloss.Style { return theme.Bg(theme.BgSidebar) }

func testsFill(w int) string {
	if w <= 0 {
		return ""
	}
	return testsBg().Render(strings.Repeat(" ", w))
}

// testsPad pads/clips a styled row to w with the sidebar background.
func testsPad(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw > w {
		return ansi.Truncate(s, w, "")
	}
	return s + testsFill(w-sw)
}

// testStatusGlyph returns the glyph and colour for a status.
func testStatusGlyph(s tests.Status) (string, theme.Color256) {
	switch s {
	case tests.StatusPassed:
		return "✓", theme.AccentGreen
	case tests.StatusFailed:
		return "✗", theme.DiagError
	case tests.StatusSkipped:
		return "⊘", theme.TextDim
	case tests.StatusRunning:
		return "◐", theme.AccentAmber
	}
	return "○", theme.TextMuted
}

// testsPkgLabel is the package row label: its directory relative to the
// workspace root ("." → the project folder name).
func testsPkgLabel(root string, p *tests.Package) string {
	rel, err := filepath.Rel(root, p.Dir)
	if err != nil || rel == "." {
		return filepath.Base(root)
	}
	return filepath.ToSlash(rel)
}

// renderTestsCounts renders compact "✓3 ✗1" counts (only non-zero parts;
// "○n" when nothing ran yet) in bg.
func renderTestsCounts(c tests.Counts, bg theme.Color256) (string, int) {
	type part struct {
		n     int
		glyph string
		col   theme.Color256
	}
	parts := []part{{c.Passed, "✓", theme.AccentGreen}, {c.Failed, "✗", theme.DiagError}, {c.Skipped, "⊘", theme.TextDim}, {c.Running, "◐", theme.AccentAmber}}
	var b strings.Builder
	w := 0
	for _, p := range parts {
		if p.n == 0 {
			continue
		}
		if w > 0 {
			b.WriteString(theme.Bg(bg).Render(" "))
			w++
		}
		s := fmt.Sprintf("%s%d", p.glyph, p.n)
		b.WriteString(theme.FgBg(p.col, bg).Render(s))
		w += runewidth.StringWidth(s)
	}
	if w == 0 {
		s := fmt.Sprintf("○%d", c.Total)
		b.WriteString(theme.FgBg(theme.TextMuted, bg).Render(s))
		w = runewidth.StringWidth(s)
	}
	return b.String(), w
}

func (m Model) renderTestsSidebar(w, h int) string {
	st := m.testView
	focused := m.focus == FocusExplorer
	var lines []string

	title := " " + letterSpace("TESTING")
	lines = append(lines, testsPad(theme.FgBg(theme.TextPrimary, theme.BgSidebar).Bold(true).Render(title), w))
	lines = append(lines, theme.FgBg(theme.BorderDefault, theme.BgSidebar).Render(strings.Repeat("▔", w)))

	c := st.tree.Counts()
	// Action bar.
	{
		var b strings.Builder
		col := 0
		for _, a := range testsActionLayout(st.running, c.Failed > 0, w) {
			b.WriteString(testsFill(a.start - col))
			fg := theme.TextSecondary
			if a.id == "run" {
				fg = theme.AccentGreen
			} else if a.id == "stop" || a.id == "failed" {
				fg = theme.DiagError
			}
			b.WriteString(theme.FgBg(fg, theme.BgSidebar).Render(a.label))
			col = a.end
		}
		lines = append(lines, testsPad(b.String(), w))
	}
	// Summary.
	lines = append(lines, testsPad(m.renderTestsSummary(c, w), w))
	lines = append(lines, testsFill(w))

	top, bodyH, footer := m.testsPanelLayout(h)
	rows := st.rows()
	var body []string
	switch {
	case st.tree == nil || (len(rows) == 0 && st.discovering):
		body = append(body, testsPad(theme.FgBg(theme.TextMuted, theme.BgSidebar).Render("  Discovering tests…"), w))
	case st.tree.FW == tests.FrameworkNone:
		for _, ln := range []string{"  No test runner detected.", "", "  Looks for go.mod, Cargo.toml,", "  pyproject.toml / setup.py /", "  pytest.ini, package.json."} {
			body = append(body, testsPad(theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(ln), w))
		}
	case len(rows) == 0:
		body = append(body, testsPad(theme.FgBg(theme.TextMuted, theme.BgSidebar).Render("  No tests found."), w))
	default:
		for i := top; i < len(rows) && len(body) < bodyH; i++ {
			body = append(body, m.renderTestRow(rows[i], i == st.cursor, focused, w))
		}
	}
	for len(body) < bodyH {
		body = append(body, testsFill(w))
	}
	lines = append(lines, body...)
	if footer > 0 {
		lines = append(lines, testsPad(theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(" ⏎ open  r run  o output  f failed"), w))
	}
	for len(lines) < h {
		lines = append(lines, testsFill(w))
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// renderTestsSummary is the counts / state row under the action bar.
func (m Model) renderTestsSummary(c tests.Counts, w int) string {
	st := m.testView
	muted := theme.FgBg(theme.TextMuted, theme.BgSidebar)
	switch {
	case st.err != "" && (st.tree == nil || st.tree.FW == tests.FrameworkNone || len(st.tree.Packages) == 0):
		return theme.FgBg(theme.DiagWarning, theme.BgSidebar).Render(" " + st.err)
	case st.runError != "" && !st.running:
		return theme.FgBg(theme.DiagError, theme.BgSidebar).Render(" ✗ " + firstLines(st.runError, 1)[0])
	}
	counts, _ := renderTestsCounts(c, theme.BgSidebar)
	s := testsBg().Render(" ") + counts
	tail := ""
	switch {
	case st.running:
		tail = fmt.Sprintf(" · running %s", time.Since(st.started).Round(time.Second))
	case st.hasRun:
		tail = " · " + st.elapsed.String()
	case st.tree != nil && st.tree.FW != tests.FrameworkNone:
		tail = " · " + st.tree.FW.String()
	}
	return s + muted.Render(tail)
}

// testsGuide writes the lead cell (accent bar on the focused cursor row)
// plus one indent guide per depth level, returning the width written.
func testsGuide(sb *strings.Builder, bg theme.Color256, depth int, active, focused bool) int {
	if active && focused {
		sb.WriteString(theme.FgBg(theme.BorderFocus, bg).Render("▌"))
	} else {
		sb.WriteString(theme.Bg(bg).Render(" "))
	}
	for d := 0; d < depth; d++ {
		sb.WriteString(theme.FgBg(theme.BorderSubtle, bg).Render("│"))
		sb.WriteString(theme.Bg(bg).Render(" "))
	}
	return 1 + depth*2
}

// renderTestRow renders one tree row at width w.
func (m Model) renderTestRow(r testRow, active, focused bool, w int) string {
	st := m.testView
	bg := theme.BgSidebar
	if active {
		bg = theme.BgInactiveSel
		if focused {
			bg = theme.BgSelection
		}
	}
	rowStyle := theme.Bg(bg)
	var sb strings.Builder
	used := testsGuide(&sb, bg, r.depth, active, focused)

	var right string
	rightW := 0
	label, labelFG, bold := "", theme.TextPrimary, false
	switch r.kind {
	case testRowPackage, testRowFile:
		key, name, counts := "", "", tests.Counts{}
		if r.kind == testRowPackage {
			key, name, counts, bold = testPkgKey(r.pkg), testsPkgLabel(st.tree.Root, r.pkg), tests.CountPackage(r.pkg), true
			if r.pkg.Error != "" {
				labelFG = theme.DiagError
			}
		} else {
			key, name, counts = testFileKey(r.file), filepath.Base(r.file.Path), tests.CountFile(r.file)
		}
		chev, chevFG := "▾", theme.AccentAmber
		if st.collapsed[key] {
			chev, chevFG = "▸", theme.TextMuted
		}
		sb.WriteString(theme.FgBg(chevFG, bg).Render(chev))
		sb.WriteString(rowStyle.Render(" "))
		used += 2
		label = name
		right, rightW = renderTestsCounts(counts, bg)
	case testRowCase:
		glyph, col := testStatusGlyph(r.c.Status)
		sb.WriteString(theme.FgBg(col, bg).Bold(true).Render(glyph))
		sb.WriteString(rowStyle.Render(" "))
		used += 2
		label = r.c.Name
		if r.c.Duration > 0 {
			d := formatTestDuration(r.c.Duration)
			right, rightW = theme.FgBg(theme.TextDim, bg).Render(d), runewidth.StringWidth(d)
		}
	case testRowMessage:
		sb.WriteString(theme.FgBg(theme.DiagError, bg).Render("┃"))
		sb.WriteString(rowStyle.Render(" "))
		used += 2
		label, labelFG = r.text, theme.TextSecondary
	case testRowPkgError:
		sb.WriteString(theme.FgBg(theme.DiagError, bg).Render("⚠"))
		sb.WriteString(rowStyle.Render(" "))
		used += 2
		label, labelFG = r.text, theme.DiagError
	}
	// Right side: counts / duration, then the ▶ run glyph on the cursor row.
	runGlyph := active && r.kind != testRowMessage
	reserve := 1
	if runGlyph {
		reserve = 3
	}
	avail := w - used - reserve
	if rightW > 0 {
		avail -= rightW + 1
	}
	if avail < 1 {
		right, rightW = "", 0
		avail = w - used - reserve
	}
	if avail < 1 {
		avail = 1
	}
	if runewidth.StringWidth(label) > avail {
		label = runewidth.Truncate(label, avail, "…")
	}
	st2 := theme.FgBg(labelFG, bg)
	if bold {
		st2 = st2.Bold(true)
	}
	sb.WriteString(st2.Render(label))
	used += runewidth.StringWidth(label)
	tailW := reserve
	if rightW > 0 {
		tailW += rightW + 1
	}
	if pad := w - used - tailW; pad > 0 {
		sb.WriteString(rowStyle.Render(strings.Repeat(" ", pad)))
		used += pad
	}
	if rightW > 0 {
		sb.WriteString(rowStyle.Render(" "))
		sb.WriteString(right)
		used += rightW + 1
	}
	if runGlyph {
		sb.WriteString(rowStyle.Render(" "))
		sb.WriteString(theme.FgBg(theme.AccentGreen, bg).Render("▶"))
		sb.WriteString(rowStyle.Render(" "))
	} else {
		sb.WriteString(rowStyle.Render(" "))
	}
	used += reserve
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}

// formatTestDuration renders 0.4s / 12ms style durations.
func formatTestDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// ── TEST RESULTS bottom-panel tab ────────────────────────────────────────

// testResultsLines returns the lines the tab shows: one test's output
// (when narrowed) or the whole run log.
func (st *testsState) testResultsLines() (lines []string, label string) {
	if st.resultsFor != "" && st.tree != nil {
		var found *tests.Case
		st.tree.Each(func(_ *tests.Package, _ *tests.File, c *tests.Case) {
			if c.Key() == st.resultsFor {
				found = c
			}
		})
		if found != nil {
			for _, ln := range found.Output {
				lines = append(lines, sanitizeOutputLine(ln))
			}
			if len(lines) == 0 && found.Message != "" {
				lines = strings.Split(found.Message, "\n")
			}
			if len(lines) == 0 {
				lines = []string{"(no output recorded for this test)"}
			}
			return lines, found.Name
		}
	}
	return st.log, ""
}

// testResultsHeaderLayout returns the [start,end) columns of the "✕ all
// output" chip on the header row (-1 when absent).
func testResultsHeaderLayout(label string, w int) (start, end int) {
	if label == "" {
		return -1, -1
	}
	chip := " ✕ " + label + " "
	cw := runewidth.StringWidth(chip)
	if cw > w/2 {
		cw = w / 2
	}
	return w - cw, w
}

func renderTestResultsTab(m *Model, w, h int) []string {
	st := m.testView
	bg := theme.BgEditor
	lines, label := st.testResultsLines()
	c := st.tree.Counts()

	// Header: summary · command, plus the "✕ <test>" chip when narrowed.
	counts := fmt.Sprintf(" ✓ %d passed  ✗ %d failed  ⊘ %d skipped", c.Passed, c.Failed, c.Skipped)
	var head strings.Builder
	head.WriteString(theme.FgBg(theme.TextPrimary, bg).Bold(true).Render(counts))
	meta := ""
	switch {
	case st.running:
		meta = " · running…"
	case st.hasRun:
		meta = " · " + st.elapsed.String()
	}
	if len(st.cmd.Args) > 0 {
		meta += " · " + st.cmd.Label()
	}
	cs, _ := testResultsHeaderLayout(label, w)
	headW := runewidth.StringWidth(counts)
	limit := w
	if cs >= 0 {
		limit = cs - 1
	}
	if room := limit - headW; room > 1 {
		head.WriteString(theme.FgBg(theme.TextMuted, bg).Render(runewidth.Truncate(meta, room, "…")))
	}
	row0 := head.String()
	if cs >= 0 {
		row0 = padTabBarToWidth(row0, cs, theme.Bg(bg))
		row0 += theme.FgBg(theme.TextPrimary, theme.BgHover).Render(runewidth.Truncate(" ✕ "+label+" ", w-cs, "…"))
	}
	out := []string{row0}

	bodyH := h - 1
	if bodyH <= 0 {
		return out
	}
	st.resultsScroll = clampOutputScroll(st.resultsScroll, len(lines), bodyH)
	start, end := outputWindow(len(lines), bodyH, st.resultsScroll)
	for _, ln := range lines[start:end] {
		if runewidth.StringWidth(ln) > w-2 {
			ln = runewidth.Truncate(ln, w-2, "…")
		}
		out = append(out, theme.FgBg(testResultLineColor(ln), bg).Render(" "+ln))
	}
	if len(lines) == 0 {
		out = append(out, theme.FgBg(theme.TextMuted, bg).Render(" No test run yet — Alt+T runs all tests."))
	}
	return out
}

// testResultLineColor colours PASS / FAIL style lines.
func testResultLineColor(ln string) theme.Color256 {
	t := strings.TrimSpace(ln)
	switch {
	case strings.HasPrefix(t, "$ "), strings.HasPrefix(t, "[done"), strings.HasPrefix(t, "==="):
		return theme.TextMuted
	case strings.HasPrefix(t, "--- FAIL"), strings.HasPrefix(t, "FAIL"), strings.Contains(t, " FAILED"),
		strings.HasPrefix(t, "panic:"), strings.HasPrefix(t, "E "), strings.Contains(t, "✕"):
		return theme.DiagError
	case strings.HasPrefix(t, "--- PASS"), strings.HasPrefix(t, "ok "), strings.HasPrefix(t, "PASS"),
		strings.Contains(t, " PASSED"), strings.HasSuffix(t, "... ok"), strings.Contains(t, "✓"):
		return theme.AccentGreen
	case strings.HasPrefix(t, "--- SKIP"), strings.Contains(t, " SKIPPED"), strings.HasSuffix(t, "... ignored"):
		return theme.TextDim
	}
	return theme.TextPrimary
}

// testLocRe finds "path/file.ext:LINE" in an output line.
var testLocRe = regexp.MustCompile(`((?:[A-Za-z]:)?[\w./\\\-]*[\w\-]+\.(?:go|py|rs|js|jsx|ts|tsx|mjs|cjs|mts|cts)):(\d+)`)

// resolveTestLocation maps a location printed by a runner to an existing
// absolute path: absolute as-is, else relative to the workspace root, the
// narrowed test's directory, or any package directory.
func (st *testsState) resolveTestLocation(p string) string {
	exists := func(f string) bool {
		info, err := os.Stat(f)
		return err == nil && !info.IsDir()
	}
	if filepath.IsAbs(p) {
		if exists(p) {
			return p
		}
		return ""
	}
	if st.tree == nil {
		return ""
	}
	cands := []string{filepath.Join(st.tree.Root, p)}
	st.tree.Each(func(_ *tests.Package, _ *tests.File, c *tests.Case) {
		if c.Key() == st.resultsFor {
			cands = append(cands, filepath.Join(filepath.Dir(c.File), p))
		}
	})
	for _, pk := range st.tree.Packages {
		cands = append(cands, filepath.Join(pk.Dir, p))
	}
	for _, c := range cands {
		if exists(c) {
			return c
		}
	}
	return ""
}

func init() {
	registerPanelKind(panelKindTests, panelKindSpec{
		Title:  "Test Results",
		Icon:   "✓",
		Render: renderTestResultsTab,
		Scroll: func(m *Model, delta int) {
			st := m.testView
			lines, _ := st.testResultsLines()
			_, _, _, h, _ := m.panelContentRect()
			st.resultsScroll = clampOutputScroll(st.resultsScroll-delta, len(lines), h-1)
		},
		Click: func(m *Model, row, col int) tea.Cmd {
			st := m.testView
			lines, label := st.testResultsLines()
			_, _, w, h, _ := m.panelContentRect()
			if row == 0 {
				if cs, ce := testResultsHeaderLayout(label, w); cs >= 0 && col >= cs && col < ce {
					st.resultsFor, st.resultsScroll = "", 0
				}
				return nil
			}
			start, end := outputWindow(len(lines), h-1, clampOutputScroll(st.resultsScroll, len(lines), h-1))
			idx := start + row - 1
			if idx < start || idx >= end {
				return nil
			}
			if mm := testLocRe.FindStringSubmatch(lines[idx]); mm != nil {
				if p := st.resolveTestLocation(mm[1]); p != "" {
					n, _ := strconv.Atoi(mm[2])
					m.panelFocused = false
					m.openTestLocation(p, n)
				}
			}
			return nil
		},
		Key: func(m *Model, msg tea.KeyMsg) (tea.Cmd, bool) {
			st := m.testView
			lines, _ := st.testResultsLines()
			_, _, _, h, _ := m.panelContentRect()
			scroll := func(d int) {
				st.resultsScroll = clampOutputScroll(st.resultsScroll-d, len(lines), h-1)
			}
			switch msg.String() {
			case "up", "k":
				scroll(-1)
			case "down", "j":
				scroll(1)
			case "pgup":
				scroll(-10)
			case "pgdown":
				scroll(10)
			case "home", "g":
				scroll(-len(lines))
			case "end", "G":
				st.resultsScroll = 0
			case "a":
				st.resultsFor, st.resultsScroll = "", 0
			default:
				return nil, false
			}
			return nil, true
		},
	})
}

// ── Status bar chip ──────────────────────────────────────────────────────

// fillTestsStatus sets the status-bar test chip: shown while a run is in
// progress and after one finished.
func (m Model) fillTestsStatus(s *statusbar.State) {
	st := m.testView
	if st == nil || st.tree == nil || (!st.running && !st.hasRun) {
		return
	}
	c := st.tree.Counts()
	s.ShowTests, s.TestsRunning = true, st.running
	s.TestsPassed, s.TestsFailed = c.Passed, c.Failed
}

// hitStatusTestsChip reports whether a click at (x, y) lands on the
// status-bar test chip.
func (m Model) hitStatusTestsChip(x, y int) bool {
	if m.zenMode || y != m.h-1 {
		return false
	}
	bx, bw := m.statusBarRect()
	sb := m.status
	sb.SetWidth(bw)
	x0, x1, ok := sb.TestsChipSpan(m.statusState())
	return ok && x >= bx+x0 && x < bx+x1
}

// ── Palette + code actions ───────────────────────────────────────────────

// dispatchTestingPalette runs the "Testing: …" palette commands.
func (m *Model) dispatchTestingPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "testing-focus":
		m.revealTestsView(true)
		return m.ensureTestsDiscovered(), true
	case "testing-run-all":
		return m.runTestsCmd(), true
	case "testing-rerun-failed":
		m.revealTestsView(false)
		return m.runTestsRequest(testsRunRequest{failed: true}), true
	case "testing-run-file", "testing-run-cursor":
		path := m.editor.Path()
		if path == "" {
			return nil, true
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		req := testsRunRequest{file: path}
		if id == "testing-run-cursor" {
			req.atLine = m.cursorLine
		}
		m.revealTestsView(false)
		return m.runTestsRequest(req), true
	case "testing-stop":
		m.stopTestRun()
		return nil, true
	case "testing-refresh":
		m.revealTestsView(false)
		return m.refreshTests(), true
	case "testing-collapse":
		m.testsCollapseAll()
		return nil, true
	case "testing-results":
		m.showTestOutput(nil)
		return nil, true
	}
	return nil, false
}

// testCaseAtLine returns the test declared nearest above line in f.
func testCaseAtLine(f *tests.File, line int) *tests.Case {
	var best *tests.Case
	for _, c := range f.Cases {
		if c.Line > 0 && c.Line <= line && (best == nil || c.Line > best.Line) {
			best = c
		}
	}
	return best
}

// testCodeActions adds Ctrl+. "Run Test" / "Run Tests in File" items for a
// discovered (non-Go) test file. Go files get theirs from the buffer scan
// in extraCodeActionsForContext, which works before discovery too.
func (m *Model) testCodeActions(path string, line int, add func(title, kind string, payload synthCodeAction)) {
	st := m.testView
	if st == nil || st.tree == nil || path == "" || strings.HasSuffix(path, ".go") {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	_, f := st.tree.FindFile(abs)
	if f == nil {
		return
	}
	if c := testCaseAtLine(f, line); c != nil {
		add("Run Test: "+c.Name, synthKindTestRun, synthCodeAction{FuncName: c.Name, Path: abs})
	}
	add("Run Tests in File", synthKindTestRun, synthCodeAction{Path: abs, Command: "file"})
}

// runSynthTest runs a Ctrl+. "Run Test" action on the streaming runner.
// FuncName == "" means the whole package (Go) or file (Command "file").
func (m *Model) runSynthTest(a synthCodeAction) tea.Cmd {
	path := a.Path
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	m.revealTestsView(false)
	switch {
	case a.Command == "file":
		return m.runTestsRequest(testsRunRequest{file: path})
	case a.FuncName == "":
		return m.runTestsRequest(testsRunRequest{pkgDir: filepath.Dir(path)})
	}
	req := testsRunRequest{caseKey: []string{path + "::" + a.FuncName}, file: path}
	if strings.HasSuffix(path, ".go") {
		req.goFunc = a.FuncName
	}
	return m.runTestsRequest(req)
}
