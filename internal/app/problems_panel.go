package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"termocode/internal/nvim"
	"termocode/internal/theme"
)

// ── Problems panel (Group C) ─────────────────────────────────────────────
//
// A bottom-panel tab listing every diagnostic nvim knows about — LSP and
// task problem matchers (tasks_run.go) alike — grouped by file. Ported from
// mobocode's problems_panel.dart + diagnostics_helpers.dart.
//
// Live: lspSetupLua fires termocode_notify('termocode_diagnostics_changed')
// on DiagnosticChanged; the handler below debounces it and refetches.
//
// Three ways in: palette "View: Problems", Ctrl+Shift+M / Alt+M, and a
// click on the status-bar error / warning counters.

// applyMsg runs a function against the Model inside Update. Background
// work (tea.Cmd goroutines) returns one to hand results back. Group C.
type applyMsg func(m *Model) tea.Cmd

const problemsDebounce = 200 * time.Millisecond

// problemsPanel is the Problems tab state. Shared by pointer.
type problemsPanel struct {
	items []nvim.Diagnostic // sorted by problemFileOrder
	// hidden severities: index 1 = errors, 2 = warnings, 3 = info + hints.
	hide      [4]bool
	collapsed map[string]bool
	sel       int // index into rows()
	scroll    int // first visible row
	viewH     int // last body height, for scroll math
}

func newProblemsPanel() *problemsPanel {
	return &problemsPanel{collapsed: map[string]bool{}}
}

// problemRow is one visible line: a file header or a diagnostic.
type problemRow struct {
	Header bool
	Path   string
	Count  int // header only: visible diagnostics in the file
	Diag   nvim.Diagnostic
}

// sevBucket maps a severity to the filter slot (hints share info's).
func sevBucket(sev int) int {
	switch sev {
	case 1:
		return 1
	case 2:
		return 2
	}
	return 3
}

// sortProblems orders files by their worst severity, then path; inside a
// file by severity, then line, then column. Pure for tests.
func sortProblems(items []nvim.Diagnostic) {
	worst := map[string]int{}
	for _, d := range items {
		if w, ok := worst[d.Path]; !ok || d.Severity < w {
			worst[d.Path] = d.Severity
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Path != b.Path {
			if worst[a.Path] != worst[b.Path] {
				return worst[a.Path] < worst[b.Path]
			}
			return a.Path < b.Path
		}
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
}

// counts returns (errors, warnings, info+hints) over all items.
func (p *problemsPanel) counts() (e, w, i int) {
	if p == nil {
		return
	}
	for _, d := range p.items {
		switch sevBucket(d.Severity) {
		case 1:
			e++
		case 2:
			w++
		default:
			i++
		}
	}
	return
}

// rows flattens the visible items into header + diagnostic rows.
func (p *problemsPanel) rows() []problemRow {
	if p == nil {
		return nil
	}
	var out []problemRow
	header := -1
	for _, d := range p.items {
		if p.hide[sevBucket(d.Severity)] {
			continue
		}
		if header < 0 || out[header].Path != d.Path {
			out = append(out, problemRow{Header: true, Path: d.Path})
			header = len(out) - 1
		}
		out[header].Count++
		if !p.collapsed[d.Path] {
			out = append(out, problemRow{Path: d.Path, Diag: d})
		}
	}
	return out
}

// setItems installs a fresh diagnostic list, keeping the selection on the
// same file + line when it still exists.
func (p *problemsPanel) setItems(items []nvim.Diagnostic) {
	if p == nil {
		return
	}
	var keep problemRow
	if rows := p.rows(); p.sel >= 0 && p.sel < len(rows) {
		keep = rows[p.sel]
	}
	sortProblems(items)
	p.items = items
	rows := p.rows()
	p.sel = 0
	for i, r := range rows {
		if r.Path == keep.Path && r.Header == keep.Header && (r.Header || r.Diag.Line == keep.Diag.Line) {
			p.sel = i
			break
		}
	}
	p.clamp(len(rows))
}

// clamp keeps sel / scroll inside the row list and sel inside the view.
func (p *problemsPanel) clamp(n int) {
	if p.sel >= n {
		p.sel = n - 1
	}
	if p.sel < 0 {
		p.sel = 0
	}
	h := p.viewH
	if h < 1 {
		h = 1
	}
	if p.sel < p.scroll {
		p.scroll = p.sel
	}
	if p.sel >= p.scroll+h {
		p.scroll = p.sel - h + 1
	}
	if max := n - h; p.scroll > max {
		p.scroll = max
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
}

func (p *problemsPanel) move(delta int) {
	p.sel += delta
	p.clamp(len(p.rows()))
}

// toggleCollapse folds / unfolds the file of the selected row.
func (p *problemsPanel) toggleCollapse(want *bool) {
	rows := p.rows()
	if p.sel < 0 || p.sel >= len(rows) {
		return
	}
	path := rows[p.sel].Path
	next := !p.collapsed[path]
	if want != nil {
		next = *want
	}
	p.collapsed[path] = next
	// Keep the selection on the file header.
	for i, r := range p.rows() {
		if r.Header && r.Path == path {
			p.sel = i
			break
		}
	}
	p.clamp(len(p.rows()))
}

// toggleCollapseAll folds every file, or unfolds all when all are folded.
func (p *problemsPanel) toggleCollapseAll() {
	all := true
	for _, d := range p.items {
		if !p.collapsed[d.Path] {
			all = false
			break
		}
	}
	for _, d := range p.items {
		p.collapsed[d.Path] = !all
	}
	p.sel = 0
	p.clamp(len(p.rows()))
}

// ── Fetching ─────────────────────────────────────────────────────────────

// problemsLua returns every diagnostic of every file buffer (loaded or not
// — task matchers attach diagnostics to buffers created with bufadd) as a
// JSON list of {path, line, col, severity, source, message}.
const problemsLua = `
	if not (vim.diagnostic and vim.diagnostic.get) then return '' end
	local out, names = {}, {}
	for _, d in ipairs(vim.diagnostic.get(nil)) do
		local b = d.bufnr
		local name = b and names[b]
		if b and name == nil then
			name = ''
			if vim.api.nvim_buf_is_valid(b) and vim.bo[b].buftype == '' then
				name = vim.api.nvim_buf_get_name(b)
			end
			names[b] = name
		end
		if name and name ~= '' then
			out[#out + 1] = { name, d.lnum + 1, (d.col or 0) + 1, d.severity or 1,
				d.source or '', d.message or '' }
		end
	end
	if #out == 0 then return '' end
	return vim.json.encode(out)
`

// parseProblemsJSON decodes problemsLua's output. Pure for tests.
func parseProblemsJSON(s string) []nvim.Diagnostic {
	if s == "" {
		return nil
	}
	var raw [][]any
	if json.Unmarshal([]byte(s), &raw) != nil {
		return nil
	}
	out := make([]nvim.Diagnostic, 0, len(raw))
	for _, r := range raw {
		if len(r) < 6 {
			continue
		}
		path, _ := r[0].(string)
		line, _ := r[1].(float64)
		col, _ := r[2].(float64)
		sev, _ := r[3].(float64)
		src, _ := r[4].(string)
		msg, _ := r[5].(string)
		out = append(out, nvim.Diagnostic{
			Path: path, Line: int(line), Col: int(col), Severity: int(sev),
			Source: src, Message: msg,
		})
	}
	return out
}

// refreshProblemsCmd fetches diagnostics in the background and installs
// them in the panel.
func (m Model) refreshProblemsCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		out, err := c.EvalLuaString(problemsLua)
		if err != nil {
			return nil
		}
		items := parseProblemsJSON(out)
		return applyMsg(func(m *Model) tea.Cmd {
			m.problems.setItems(items)
			return nil
		})
	}
}

// showProblemsPanel opens (or focuses) the Problems tab and refreshes it.
func (m *Model) showProblemsPanel() tea.Cmd {
	if m.problems == nil {
		m.problems = newProblemsPanel()
	}
	m.showPanelTab(panelKindProblems)
	return m.refreshProblemsCmd()
}

// jumpToDiagnostic opens path at line / col in the editor window.
func (m *Model) jumpToDiagnostic(path string, line, col int) {
	if m.nvim == nil || path == "" {
		return
	}
	m.ensureEditorWindowCurrent()
	if col < 1 {
		col = 1
	}
	if line < 1 {
		line = 1
	}
	_ = m.nvim.ExecLuaArgs(`
		local p, l, c = ...
		if vim.fn.fnamemodify(vim.api.nvim_buf_get_name(0), ':p') ~= vim.fn.fnamemodify(p, ':p') then
			vim.cmd('edit ' .. vim.fn.fnameescape(p))
			-- Task problems may sit on a buffer that was not loaded yet;
			-- older nvim only draws them on the next show.
			pcall(vim.diagnostic.show, nil, 0)
		end
		local last = vim.api.nvim_buf_line_count(0)
		if l > last then l = last end
		pcall(vim.api.nvim_win_set_cursor, 0, { l, c - 1 })
		pcall(vim.cmd, 'normal! zz')
	`, path, line, col)
	m.panelFocused = false
	m.focus = FocusEditor
}

// openSelectedProblem jumps to the selected diagnostic (or toggles a file
// header).
func (m *Model) openSelectedProblem() {
	p := m.problems
	rows := p.rows()
	if p.sel < 0 || p.sel >= len(rows) {
		return
	}
	r := rows[p.sel]
	if r.Header {
		p.toggleCollapse(nil)
		return
	}
	m.jumpToDiagnostic(r.Diag.Path, r.Diag.Line, r.Diag.Col)
}

// ── Rendering ────────────────────────────────────────────────────────────

// problemsChip is one clickable filter in the header row.
type problemsChip struct {
	Bucket     int // 1 errors, 2 warnings, 3 info; 0 = collapse-all
	Start, End int
}

// problemsHeaderLayout lays out " ✘ 3   ⚠ 5   ℹ 2  …  ⊟ ". Pure for tests
// and hit-testing.
func problemsHeaderLayout(e, w, i, width int) []problemsChip {
	labels := []string{
		fmt.Sprintf(" %s %d ", severityIcon(1), e),
		fmt.Sprintf(" %s %d ", severityIcon(2), w),
		fmt.Sprintf(" %s %d ", severityIcon(3), i),
	}
	var out []problemsChip
	col := 1
	for b, l := range labels {
		lw := runewidth.StringWidth(l)
		if col+lw > width-4 {
			break
		}
		out = append(out, problemsChip{Bucket: b + 1, Start: col, End: col + lw})
		col += lw + 1
	}
	if width >= col+4 {
		out = append(out, problemsChip{Bucket: 0, Start: width - 4, End: width - 1})
	}
	return out
}

func renderProblemsHeader(p *problemsPanel, width int) string {
	e, w, i := p.counts()
	nums := [4]int{0, e, w, i}
	sevColor := [4]theme.Color256{theme.TextMuted, theme.DiagError, theme.DiagWarning, theme.DiagInfo}
	bg := theme.Bg(theme.BgEditor)
	var b strings.Builder
	col := 0
	for _, c := range problemsHeaderLayout(e, w, i, width) {
		if c.Start > col {
			b.WriteString(bg.Render(strings.Repeat(" ", c.Start-col)))
		}
		if c.Bucket == 0 {
			b.WriteString(theme.FgBg(theme.TextMuted, theme.BgEditor).Render(" ⊟ "))
		} else {
			label := fmt.Sprintf(" %s %d ", severityIcon(c.Bucket), nums[c.Bucket])
			if p.hide[c.Bucket] {
				b.WriteString(theme.FgBg(theme.TextDim, theme.BgEditor).Strikethrough(true).Render(label))
			} else {
				b.WriteString(theme.FgBg(sevColor[c.Bucket], theme.BgHover).Render(label))
			}
		}
		col = c.End
	}
	return b.String()
}

// problemRelPath shows path relative to the cwd when it is inside it.
func problemRelPath(path string) string {
	if cwd, err := os.Getwd(); err == nil {
		if r, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return path
}

func renderProblemsTab(m *Model, w, h int) []string {
	p := m.problems
	if p == nil {
		p = newProblemsPanel()
		m.problems = p
	}
	rows := []string{renderProblemsHeader(p, w)}
	bodyH := h - 1
	if bodyH <= 0 {
		return rows
	}
	p.viewH = bodyH
	list := p.rows()
	if len(list) == 0 {
		msg := " No problems have been detected in the workspace."
		if len(p.items) > 0 {
			msg = " All problems are hidden by the filters."
		}
		return append(rows, theme.FgBg(theme.TextMuted, theme.BgEditor).Render(msg))
	}
	p.clamp(len(list))
	sevColor := map[int]theme.Color256{1: theme.DiagError, 2: theme.DiagWarning, 3: theme.DiagInfo, 4: theme.DiagHint}
	for idx := p.scroll; idx < len(list) && len(rows) < h; idx++ {
		r := list[idx]
		bgc := theme.BgEditor
		if idx == p.sel {
			bgc = theme.BgHover
			if m.panelFocused {
				bgc = theme.BgSelection
			}
		}
		bg := theme.Bg(bgc)
		var b strings.Builder
		if r.Header {
			arrow := "▾"
			if p.collapsed[r.Path] {
				arrow = "▸"
			}
			rel := problemRelPath(r.Path)
			dir := filepath.Dir(rel)
			b.WriteString(theme.FgBg(theme.TextMuted, bgc).Render(" " + arrow + " "))
			b.WriteString(theme.FgBg(theme.TextPrimary, bgc).Bold(true).Render(filepath.Base(rel)))
			if dir != "." && dir != "" {
				b.WriteString(theme.FgBg(theme.TextDim, bgc).Render("  " + dir))
			}
			b.WriteString(theme.FgBg(theme.TextMuted, bgc).Render(fmt.Sprintf("  %d", r.Count)))
		} else {
			d := r.Diag
			loc := fmt.Sprintf("[Ln %d, Col %d]", d.Line, d.Col)
			msg := strings.SplitN(d.Message, "\n", 2)[0]
			src := ""
			if d.Source != "" {
				src = "  " + d.Source
			}
			// "    ✘ msg  src  [Ln, Col]" — clip the message to fit.
			fixed := 4 + runewidth.StringWidth(severityIcon(d.Severity)) + 1 + runewidth.StringWidth(src) + 2 + runewidth.StringWidth(loc) + 1
			if room := w - fixed; room < runewidth.StringWidth(msg) {
				if room < 1 {
					room = 1
				}
				msg = runewidth.Truncate(msg, room, "…")
			}
			b.WriteString(bg.Render("    "))
			b.WriteString(theme.FgBg(sevColor[d.Severity], bgc).Render(severityIcon(d.Severity)))
			b.WriteString(theme.FgBg(theme.TextPrimary, bgc).Render(" " + msg))
			b.WriteString(theme.FgBg(theme.TextDim, bgc).Render(src + "  " + loc))
		}
		line := b.String()
		if idx == p.sel {
			line = padTabBarToWidth(line, w, bg)
		}
		rows = append(rows, line)
	}
	return rows
}

// ── Input ────────────────────────────────────────────────────────────────

func problemsClick(m *Model, row, col int) tea.Cmd {
	p := m.problems
	if p == nil {
		return nil
	}
	if row == 0 {
		e, w, i := p.counts()
		for _, c := range problemsHeaderLayout(e, w, i, m.editorPaneWidth()) {
			if col >= c.Start && col < c.End {
				if c.Bucket == 0 {
					p.toggleCollapseAll()
				} else {
					p.hide[c.Bucket] = !p.hide[c.Bucket]
					p.clamp(len(p.rows()))
				}
			}
		}
		return nil
	}
	idx := p.scroll + row - 1
	if idx < 0 || idx >= len(p.rows()) {
		return nil
	}
	p.sel = idx
	m.openSelectedProblem()
	return nil
}

func problemsKey(m *Model, msg tea.KeyMsg) (tea.Cmd, bool) {
	p := m.problems
	if p == nil {
		return nil, false
	}
	yes, no := true, false
	switch msg.String() {
	case "up", "k":
		p.move(-1)
	case "down", "j":
		p.move(1)
	case "pgup":
		p.move(-10)
	case "pgdown":
		p.move(10)
	case "home", "g":
		p.move(-len(p.rows()))
	case "end", "G":
		p.move(len(p.rows()))
	case "enter", "o":
		m.openSelectedProblem()
	case " ":
		p.toggleCollapse(nil)
	case "left", "h":
		p.toggleCollapse(&yes)
	case "right", "l":
		p.toggleCollapse(&no)
	case "e", "w", "i":
		b := map[string]int{"e": 1, "w": 2, "i": 3}[msg.String()]
		p.hide[b] = !p.hide[b]
		p.clamp(len(p.rows()))
	case "c":
		p.toggleCollapseAll()
	case "r":
		return m.refreshProblemsCmd(), true
	default:
		return nil, false
	}
	return nil, true
}

// problemsTitle is the live tab title: "PROBLEMS (n)".
func problemsTitle(m *Model) string {
	if m.problems == nil || len(m.problems.items) == 0 {
		return "PROBLEMS"
	}
	return fmt.Sprintf("PROBLEMS (%d)", len(m.problems.items))
}

// hitStatusDiagCounts reports whether a click at (x, y) lands on the
// status-bar error / warning counters.
func (m Model) hitStatusDiagCounts(x, y int) bool {
	if m.zenMode || y != m.h-1 {
		return false
	}
	bx, bw := m.statusBarRect()
	sb := m.status
	sb.SetWidth(bw)
	x0, x1, ok := sb.DiagSpan(m.statusState())
	return ok && x >= bx+x0 && x < bx+x1
}

func init() {
	registerPanelKind(panelKindProblems, panelKindSpec{
		Title:   "PROBLEMS",
		Icon:    "⚠",
		TitleFn: problemsTitle,
		Render:  renderProblemsTab,
		Scroll: func(m *Model, delta int) {
			if p := m.problems; p != nil {
				n := len(p.rows())
				p.scroll += delta
				if max := n - p.viewH; p.scroll > max {
					p.scroll = max
				}
				if p.scroll < 0 {
					p.scroll = 0
				}
				// Drag the selection along so clamp keeps the new scroll.
				if p.sel < p.scroll {
					p.sel = p.scroll
				}
				if p.viewH > 0 && p.sel >= p.scroll+p.viewH {
					p.sel = p.scroll + p.viewH - 1
				}
			}
		},
		Click: problemsClick,
		Key:   problemsKey,
	})
	// lsp_lua.go fires this on every DiagnosticChanged (LSP and task
	// matchers alike). Debounced: one refetch per quiet period.
	registerNotifyHandler("termocode_diagnostics_changed", func(m *Model, _ []any) tea.Cmd {
		if m.nvim == nil {
			return nil
		}
		c := m.nvim
		return m.debounce.Do("problems", problemsDebounce, func() tea.Msg {
			out, err := c.EvalLuaString(problemsLua)
			if err != nil {
				return nil
			}
			items := parseProblemsJSON(out)
			return applyMsg(func(m *Model) tea.Cmd {
				m.problems.setItems(items)
				return nil
			})
		})
	})
}
