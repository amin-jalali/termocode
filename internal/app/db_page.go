package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The SQL Result page (plan F5): an nvim scratch buffer like the HTTP
// Response page. Row 1 the SQL and the connection, row 2 the summary and
// the view chips, row 3 the key hints, row 4 a rule, then the view (Grid,
// JSON or Messages). Only one page of rows (db_page_rows) is written to
// the buffer; the loaded rows stay in Go, capped at db_max_rows.

const (
	dbViewGrid = iota
	dbViewJSON
	dbViewMessages
	dbViewCount
)

var dbViewNames = [dbViewCount]string{"Grid", "JSON", "Messages"}

// dbPageHeadRows is the number of chrome rows above a view.
const dbPageHeadRows = 4

// dbMaxCellWidth cuts long cells in the grid (v shows the whole value).
const dbMaxCellWidth = 40

var (
	dbGlyphVBar  = theme.Icon{NerdFont: "│", Unicode: "│", ASCII: "|"}
	dbGlyphHBar  = theme.Icon{NerdFont: "─", Unicode: "─", ASCII: "-"}
	dbGlyphCross = theme.Icon{NerdFont: "┼", Unicode: "┼", ASCII: "+"}
	dbGlyphMore  = theme.Icon{NerdFont: "…", Unicode: "…", ASCII: "..."}
)

func dbGlyphRun() string { return theme.GlyphSend.String() }

func dbRedact(s string) string { return ai.Redact(dbclient.Redact(s)) }

// dbSafeMultiline redacts s and makes every line safe for the page.
func dbSafeMultiline(s string) string {
	lines := strings.Split(strings.ReplaceAll(dbRedact(s), "\r\n", "\n"), "\n")
	for i := range lines {
		lines[i] = sanitizeOutputLine(lines[i])
	}
	return strings.Join(lines, "\n")
}

// dbStripComments drops the comment lines before the SQL ("-- @conn …").
func dbStripComments(sql string) string {
	lines := strings.Split(sql, "\n")
	for len(lines) > 1 {
		if t := strings.TrimSpace(lines[0]); t == "" || strings.HasPrefix(t, "--") {
			lines = lines[1:]
			continue
		}
		break
	}
	return strings.Join(lines, "\n")
}

// dbOneLine is s on one line (runs of white space become one space).
func dbOneLine(s string) string {
	return strings.Join(strings.Fields(sanitizeOutputLine(strings.ReplaceAll(dbRedact(s), "\n", " "))), " ")
}

func init() {
	registerNotifyHandler("termocode_db_key", func(m *Model, args []any) tea.Cmd {
		return m.databasePageKey(notifyArgString(args, 0), notifyArgInt(args, 1), notifyArgInt(args, 2))
	})
}

// displayedResult is the loaded result with the page's filter and sort
// applied (cached until one of them changes).
func (s *dbConsoleState) displayedResult() dbclient.Result {
	if s.shown == nil {
		return dbclient.Result{}
	}
	key := dbViewKey{run: s.shown, filter: s.filter, sortCol: s.sortCol, desc: s.desc}
	if s.cacheKey == key && s.cache != nil {
		return *s.cache
	}
	r := dbclient.Filter(s.shown.result, s.filter)
	if s.sortCol >= 0 {
		r.Rows = append([][]dbclient.Cell(nil), r.Rows...)
		dbclient.Sort(&r, s.sortCol, s.desc)
	}
	s.cacheKey, s.cache = key, &r
	return r
}

type dbViewKey struct {
	run     *dbRun
	filter  string
	sortCol int
	desc    bool
}

func (s *dbConsoleState) pageCount() int {
	n := len(s.displayedResult().Rows)
	size := loadDBConsoleSettings().pageRows()
	return max((n+size-1)/size, 1)
}

func (s *dbConsoleState) pageResult() dbclient.Result {
	r := s.displayedResult()
	n := loadDBConsoleSettings().pageRows()
	start := min(s.page*n, len(r.Rows))
	end := min(start+n, len(r.Rows))
	r.Rows = r.Rows[start:end]
	return r
}

// dbPage is the page text plus what Lua needs: the highlight spans, the
// first grid data row and the column start cells.
type dbPage struct {
	lines  []string
	hls    []map[string]any
	first  int   // 1-based line of the first data row (0: no grid)
	bounds []int // display cell where each grid column starts
}

func (p *dbPage) add(line string) int {
	p.lines = append(p.lines, line)
	return len(p.lines)
}

func (p *dbPage) hl(line, start, end int, group string) {
	if end > start {
		p.hls = append(p.hls, map[string]any{"l": line, "s": start, "e": end, "g": group})
	}
}

// split puts left and right on one line of width w (right-aligned when it
// fits). It returns the line and the byte where right starts.
func dbSplitLine(left, right string, w int) (string, int) {
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	if right == "" {
		return ansi.Truncate(left, w, dbGlyphMore.String()), -1
	}
	if lw+2+rw > w {
		left = ansi.Truncate(left, max(w-rw-2, 8), dbGlyphMore.String())
		lw = ansi.StringWidth(left)
	}
	line := left + strings.Repeat(" ", max(2, w-lw-rw))
	return line + right, len(line)
}

func (m *Model) databasePageWidth() int {
	w := m.editorPaneWidth()
	if w >= 120 {
		w /= 2
	}
	return max(w-2, 20)
}

func (m *Model) databasePage() dbPage {
	var p dbPage
	s := m.dbConsole()
	r := s.shown
	if r == nil {
		return p
	}
	width := m.databasePageWidth()

	// Row 1: the SQL and the connection.
	who := sanitizeDBText(r.conn)
	if r.by == "ai" {
		who += " · by AI"
	}
	line, at := dbSplitLine(" "+dbOneLine(dbStripComments(r.sql)), who, width)
	n := p.add(line)
	p.hl(n, 0, max(at, 0), "TermocodeHttpReq")
	if at >= 0 {
		p.hl(n, at, len(line), "TermocodeHttpName")
	}

	// Row 2: the summary and the view chips.
	res := r.result
	rows := s.displayedResult()
	s.page = min(s.page, s.pageCount()-1)
	summary := ""
	group := "TermocodeHttpSummary"
	switch {
	case s.running != nil:
		summary = "Running… · Esc cancel"
		group = "TermocodeHttpLensBusy"
	case res.Err != nil:
		summary = dbGlyphFail() + " " + dbErrorText(res.Err)
		group = "TermocodeHttpStatusErr"
	case len(res.Columns) == 0:
		summary = fmt.Sprintf("%s · %d ms", dbChangeSummary(r), res.Elapsed.Milliseconds())
	default:
		summary = dbPlural(len(rows.Rows), "row")
		if s.filter != "" {
			summary += fmt.Sprintf(" of %d (filter %q)", len(res.Rows), sanitizeDBText(s.filter))
		}
		if pages := s.pageCount(); pages > 1 {
			summary += fmt.Sprintf(" · page %d of %d", s.page+1, pages)
		}
		if res.Truncated {
			summary += " · row limit reached"
		}
		summary += fmt.Sprintf(" · %d ms", res.Elapsed.Milliseconds())
	}
	chips := make([]string, dbViewCount)
	copy(chips, dbViewNames[:])
	sep := "  "
	if ansi.StringWidth(summary)+3+ansi.StringWidth(strings.Join(chips, sep)) > width {
		chips, sep = []string{"Grid", "JSON", "Msgs"}, " "
	}
	line, at = dbSplitLine(" "+summary, strings.Join(chips, sep), width)
	n = p.add(line)
	p.hl(n, 0, max(at, 0), group)
	if at >= 0 {
		pos := at
		for i, c := range chips {
			g := "TermocodeHttpChipOff"
			if i == s.view {
				g = "TermocodeHttpChipOn"
			}
			p.hl(n, pos, pos+len(c), g)
			pos += len(c) + len(sep)
		}
	}

	// Row 3: the keys that matter now; row 4: the rule.
	hints := " Esc cancel"
	if s.running == nil {
		hints = " Tab view  r rerun  gd sql  h history  q close"
		if len(res.Columns) > 0 && res.Err == nil {
			hints = " ] next  o sort  f filter  v value  c csv  Tab view  r rerun  q close"
		}
	}
	n = p.add(ansi.Truncate(hints, width, dbGlyphMore.String()))
	p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpHint")
	n = p.add(" " + strings.Repeat(dbGlyphHBar.String(), width-1))
	p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpSep")
	if s.running != nil {
		return p
	}

	switch s.view {
	case dbViewGrid:
		m.databaseGrid(&p, s)
	case dbViewJSON:
		var b bytes.Buffer
		page := s.pageResult()
		if json.Indent(&b, []byte(dbclient.ToJSON(page)), " ", "  ") == nil {
			for _, l := range strings.Split(dbSafeMultiline(" "+b.String()), "\n") {
				p.add(l)
			}
		}
	case dbViewMessages:
		databaseMessages(&p, r)
	}
	return p
}

func dbPlural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func dbGlyphFail() string { return theme.GlyphFail.String() }

func (m *Model) databaseGrid(p *dbPage, s *dbConsoleState) {
	r := s.shown.result
	if r.Err != nil || len(r.Columns) == 0 {
		databaseMessages(p, s.shown)
		return
	}
	page := s.pageResult()
	more := dbGlyphMore.String()
	cell := func(c dbclient.Cell) string {
		return ansi.Truncate(dbOneLine(c.String()), dbMaxCellWidth, more)
	}
	widths := make([]int, len(page.Columns))
	for i, c := range page.Columns {
		widths[i] = ansi.StringWidth(ansi.Truncate(dbOneLine(c.Name), dbMaxCellWidth, more))
		for _, row := range page.Rows {
			if i < len(row) {
				widths[i] = max(widths[i], ansi.StringWidth(cell(row[i])))
			}
		}
		widths[i] = max(widths[i], 1)
	}
	bar := " " + dbGlyphVBar.String() + " "
	pos := 1
	for _, w := range widths {
		p.bounds = append(p.bounds, pos)
		pos += w + ansi.StringWidth(bar)
	}
	pad := func(text string, w int, right bool) string {
		gap := strings.Repeat(" ", max(0, w-ansi.StringWidth(text)))
		if right {
			return gap + text
		}
		return text + gap
	}
	var b strings.Builder
	b.WriteString(" ")
	for i, c := range page.Columns {
		if i > 0 {
			b.WriteString(bar)
		}
		b.WriteString(pad(ansi.Truncate(dbOneLine(c.Name), dbMaxCellWidth, more), widths[i], false))
	}
	n := p.add(strings.TrimRight(b.String(), " "))
	p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpSection")
	b.Reset()
	b.WriteString(dbGlyphHBar.String())
	for i, w := range widths {
		if i > 0 {
			b.WriteString(dbGlyphHBar.String() + dbGlyphCross.String() + dbGlyphHBar.String())
		}
		b.WriteString(strings.Repeat(dbGlyphHBar.String(), w))
	}
	n = p.add(b.String())
	p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpSep")
	if len(page.Rows) == 0 {
		n = p.add(" (no rows)")
		p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpSummary")
		return
	}
	p.first = len(p.lines) + 1
	for _, row := range page.Rows {
		b.Reset()
		b.WriteString(" ")
		var spans [][2]int
		for i := range page.Columns {
			if i > 0 {
				b.WriteString(bar)
			}
			c := dbclient.Cell{Null: true}
			if i < len(row) {
				c = row[i]
			}
			start := b.Len()
			b.WriteString(pad(cell(c), widths[i], c.Kind == "number"))
			if c.Null {
				spans = append(spans, [2]int{start, b.Len()})
			}
		}
		n = p.add(strings.TrimRight(b.String(), " "))
		for _, sp := range spans {
			p.hl(n, sp[0], min(sp[1], len(p.lines[n-1])), "TermocodeHttpSep")
		}
	}
	if s.page+1 < s.pageCount() {
		n = p.add(" " + more + " ] next page")
		p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpHint")
	}
}

// databaseMessages is the Messages view: the error in full, the counts, the
// server's notices and the timing.
func databaseMessages(p *dbPage, r *dbRun) {
	res := r.result
	if res.Err != nil {
		reason := dbErrorText(res.Err)
		n := p.add(" " + dbGlyphFail() + " " + reason)
		p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpStatusErr")
		for _, l := range strings.Split(dbSafeMultiline(res.Err.Error()), "\n") {
			if t := strings.TrimSpace(l); t != "" && t != reason {
				p.add(" " + strings.TrimRight(l, " "))
			}
		}
		p.add("")
	}
	switch {
	case res.Err != nil:
	case len(res.Columns) == 0:
		p.add(" " + dbChangeSummary(r))
	default:
		p.add(" " + dbPlural(len(res.Rows), "row") + " loaded")
	}
	if res.Truncated {
		p.add(" Row limit reached (db_max_rows). Add a LIMIT or a WHERE to see other rows.")
	}
	for _, notice := range res.Notices {
		for _, l := range strings.Split(dbSafeMultiline(notice), "\n") {
			if strings.TrimSpace(l) != "" {
				p.add(" " + l)
			}
		}
	}
	n := p.add(fmt.Sprintf(" Time: %d ms", res.Elapsed.Milliseconds()))
	p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpSummary")
}

type dbAppendMsg struct {
	gen   uint64
	lines []string
}

// dbPageBatch is the number of lines sent to nvim in one call.
const dbPageBatch = 200

func (m *Model) databaseAppendPage(msg dbAppendMsg) tea.Cmd {
	s := m.dbConsole()
	if msg.gen != s.pageGen || m.nvim == nil {
		return nil
	}
	end := min(dbPageBatch, len(msg.lines))
	var ok bool
	_ = m.nvim.ExecLuaResult(`return _G._termocode_db_append(...)`, &ok, msg.lines[:end])
	if !ok {
		return nil // the page was closed or replaced
	}
	if end < len(msg.lines) {
		rest := msg.lines[end:]
		return func() tea.Msg { return dbAppendMsg{gen: msg.gen, lines: rest} }
	}
	return nil
}

func (m *Model) databaseShowPage() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.ensureDBLua()
	p := m.databasePage()
	if len(p.lines) == 0 {
		return nil
	}
	if p.bounds == nil {
		p.bounds = []int{}
	}
	if p.hls == nil {
		p.hls = []map[string]any{}
	}
	s := m.dbConsole()
	s.pageGen++
	gen := s.pageGen
	end := min(dbPageBatch, len(p.lines))
	cursor := min(dbPageHeadRows+1, len(p.lines))
	if p.first > 0 {
		cursor = p.first
	}
	_ = m.nvim.ExecLuaArgs(`_G._termocode_db_open(...)`, m.editorPaneWidth() >= 120, p.lines[:end], p.hls, p.first, p.bounds, cursor, p.first == 0 && s.view != dbViewJSON)
	m.focus = FocusEditor
	if end < len(p.lines) {
		rest := p.lines[end:]
		return tea.Batch(m.fetchStateCmd(), func() tea.Msg { return dbAppendMsg{gen: gen, lines: rest} })
	}
	return m.fetchStateCmd()
}

func (m *Model) databaseRerun() tea.Cmd {
	s := m.dbConsole()
	if s.running != nil || s.shown == nil {
		return nil
	}
	old := s.shown
	r := &dbRun{conn: old.conn, file: old.file, line: old.line, sql: old.sql, by: "user", statements: []string{old.sql}}
	return m.databaseReviewRun(r)
}

func (m *Model) databasePageKey(key string, row, col int) tea.Cmd {
	s := m.dbConsole()
	if s.running != nil {
		if key == "esc" {
			return m.databaseCancelRun()
		}
		return nil
	}
	if s.shown == nil {
		return nil
	}
	switch key {
	case "]":
		if s.page+1 >= s.pageCount() {
			return nil
		}
		s.page++
		return m.databaseShowPage()
	case "[":
		if s.page == 0 {
			return nil
		}
		s.page--
		return m.databaseShowPage()
	case "tab":
		s.view = (s.view + 1) % dbViewCount
		return m.databaseShowPage()
	case "r":
		return m.databaseRerun()
	case "h":
		return m.databaseHistory()
	case "f":
		cmd := m.openHTTPPrompt(promptKindDBResultFilter, "Filter rows", "Rows that contain (empty: all rows):", s.filter)
		m.prompt.SetAllowEmpty(true)
		return cmd
	case "o":
		var items []picker.Item
		for i, c := range s.shown.result.Columns {
			hint := ""
			if i == s.sortCol {
				hint = "ascending"
				if s.desc {
					hint = "descending"
				}
			}
			items = append(items, picker.Item{ID: fmt.Sprint(i), Title: dbOneLine(c.Name), Hint: hint})
		}
		if len(items) == 0 {
			return nil
		}
		m.picker = picker.NewItems("Sort by column (again: reverse)", items)
		m.picker.SetSize(m.w, m.h)
		m.pickerKind = pickerKindDBSort
		m.pickerOpen = true
		return nil
	case "c":
		return m.copyWithToast(dbRedact(dbclient.ToCSV(s.displayedResult())), "Copied rows as CSV")
	case "C":
		return m.copyWithToast(dbRedact(dbclient.ToJSON(s.displayedResult())), "Copied rows as JSON")
	case "v", "y":
		page := s.pageResult()
		if s.view != dbViewGrid || row < 0 || row >= len(page.Rows) || col < 0 || col >= len(page.Rows[row]) {
			return nil
		}
		text := dbSafeMultiline(page.Rows[row][col].String())
		if key == "y" {
			return m.copyWithToast(text, "Copied cell")
		}
		var pretty bytes.Buffer
		if json.Indent(&pretty, []byte(text), "", "  ") == nil {
			text = pretty.String()
		}
		_ = m.nvim.ExecLuaArgs(`_G._termocode_db_value(...)`, strings.Split(text, "\n"))
		return m.fetchStateCmd()
	case "gd":
		r := s.shown
		if r.file == "" {
			return nil
		}
		_ = m.nvim.ExecLuaArgs(`local f,l=...;local w=vim.fn.bufwinid(vim.fn.bufnr(f));if w>0 then vim.api.nvim_set_current_win(w) else vim.cmd('edit '..vim.fn.fnameescape(f)) end;pcall(vim.api.nvim_win_set_cursor,0,{l,0});vim.cmd('stopinsert')`, r.file, max(r.line, 1))
		return m.fetchStateCmd()
	case "q":
		s.pageGen++
		_ = m.nvim.ExecLua(`_G._termocode_db_close()`)
		return m.fetchStateCmd()
	}
	return nil
}
