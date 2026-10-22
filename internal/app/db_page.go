package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
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
	// Pending-change marks in the grid's left margin (plain ASCII in every tier).
	dbGlyphChanged = theme.Icon{NerdFont: "*", Unicode: "*", ASCII: "*"}
	dbGlyphAdded   = theme.Icon{NerdFont: "+", Unicode: "+", ASCII: "+"}
	dbGlyphDeleted = theme.Icon{NerdFont: "-", Unicode: "-", ASCII: "-"}
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
	key := dbViewKey{run: s.shown, filter: s.filter, sortCol: s.sortCol, desc: s.desc, rows: len(s.shown.result.Rows)}
	if s.cacheKey == key && s.cache != nil {
		return *s.cache
	}
	// Each row carries its index in the loaded result (in a cell's Kind,
	// which neither the filter nor the sort reads) so edits find it.
	tagged := s.shown.result
	tagged.Rows = make([][]dbclient.Cell, len(s.shown.result.Rows))
	for i, row := range s.shown.result.Rows {
		tagged.Rows[i] = append(row[:len(row):len(row)], dbclient.Cell{Kind: dbRowTag + strconv.Itoa(i)})
	}
	r := dbclient.Filter(tagged, s.filter)
	if s.sortCol >= 0 {
		r.Rows = append([][]dbclient.Cell(nil), r.Rows...)
		dbclient.Sort(&r, s.sortCol, s.desc)
	}
	s.cacheIdx = make([]int, len(r.Rows))
	for i, row := range r.Rows {
		s.cacheIdx[i], _ = strconv.Atoi(strings.TrimPrefix(row[len(row)-1].Kind, dbRowTag))
		r.Rows[i] = row[:len(row)-1]
	}
	s.cacheKey, s.cache = key, &r
	return r
}

// dbRowTag marks the hidden cell that holds a row's index.
const dbRowTag = "\x00row:"

// pageIdx is the index in the loaded result of each row on the page.
func (s *dbConsoleState) pageIdx() []int {
	s.displayedResult()
	n := loadDBConsoleSettings().pageRows()
	start := min(s.page*n, len(s.cacheIdx))
	end := min(start+n, len(s.cacheIdx))
	return s.cacheIdx[start:end]
}

type dbViewKey struct {
	run     *dbRun
	filter  string
	sortCol int
	desc    bool
	rows    int
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
	lines   []string
	hls     []map[string]any
	first   int   // 1-based line of the first data row (0: no grid)
	bounds  []int // display cell where each grid column starts
	lastNew int   // 1-based line of the last new row (0: none)
	fold    bool  // fold by indent (the Plan view)
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
	left := " " + dbOneLine(dbStripComments(r.sql))
	if r.redis != nil {
		left = " Redis: " + dbOneLine(dbclient.RedisDisplay(r.redis.key))
	}
	if strip := s.tabStrip(); strip != "" {
		left = " " + strip + " " + dbGlyphVBar.String() + left
	}
	line, at := dbSplitLine(left, who, width)
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
	var short []string // shorter summaries for a narrow page (data view, Transposed)
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
	case r.plan != nil:
		short = databasePlanSummary(r)
		summary, short = short[0], short[1:]
	case r.redis != nil:
		c, _ := m.databases.connection(r.conn)
		short = dbRedisSummary(c, r.redis)
		summary, short = short[0], short[1:]
		if c.Environment == "production" {
			group = "TermocodeHttpStatusErr"
		}
	case s.trans:
		page := s.pageResult()
		s.trow = max(min(s.trow, len(page.Rows)-1), 0)
		summary = "no rows"
		if len(page.Rows) > 0 {
			summary = fmt.Sprintf("row %d of %d", s.trow+1, len(page.Rows))
		}
		short = []string{summary + fmt.Sprintf(" · %d ms", res.Elapsed.Milliseconds()), summary}
		if tv := r.tv; tv != nil && tv.offset > 0 && len(page.Rows) > 0 {
			summary += fmt.Sprintf(" · rows %d-%d", tv.offset+1, tv.offset+len(page.Rows))
		}
		summary += fmt.Sprintf(" · %d ms", res.Elapsed.Milliseconds())
	case r.tv != nil:
		summary = dbTableSummary(r.tv, len(res.Rows), res.Elapsed.Milliseconds())
		short = []string{dbTableSummary(r.tv, len(res.Rows), -1)}
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
	if s.running == nil {
		if extra, brief := m.databaseEditSummary(r); extra != "" {
			// Narrow pages keep the change count (the chip shows the
			// transaction) before they drop the rest.
			base := append([]string{summary}, short...)
			summary += extra
			short = short[:0]
			for _, v := range base[1:] {
				short = append(short, v+extra)
			}
			for _, v := range base {
				short = append(short, v+brief)
			}
			last := base[len(base)-1]
			if cut := strings.Index(last, " · "); cut > 0 {
				short = append(short, last[:cut]+brief) // "rows 1-100 · 2 changes"
			}
			short = append(short, strings.TrimPrefix(brief, " · "))
		}
	}
	chips := make([]string, dbViewCount)
	copy(chips, dbViewNames[:])
	plan := r.plan != nil && res.Err == nil && len(res.Columns) > 0
	if plan {
		chips[dbViewGrid], chips[dbViewJSON] = "Plan", "Raw"
	}
	if s.trans && !plan {
		chips[dbViewGrid] = "Transposed"
	}
	sep := "  "
	if ansi.StringWidth(summary)+3+ansi.StringWidth(strings.Join(chips, sep)) > width {
		chips, sep = []string{"Grid", "JSON", "Msgs"}, " "
		if plan {
			chips[dbViewGrid], chips[dbViewJSON] = "Plan", "Raw"
		}
		if s.trans && !plan {
			chips[dbViewGrid] = "Trans"
		}
		for _, v := range short {
			summary = v
			if ansi.StringWidth(v)+3+ansi.StringWidth(strings.Join(chips, sep)) <= width {
				break
			}
		}
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
		hints = dbFitHints(width, "Tab view", "r rerun", "gd show SQL", "h history", "q close")
		if len(res.Columns) > 0 && res.Err == nil {
			hints = dbFitHints(width, "] next page", "o sort", "f filter", "v value", "c csv", "Tab view", "r rerun", "q close")
			switch {
			case r.redis != nil:
				hints = dbRedisHints(width, r.redis)
			case plan:
				hints = dbFitHints(width, "za fold", "x plan", "X analyze", "y copy", "Tab raw", "r rerun", "q close")
			case s.curTab().dirty():
				hints = dbFitHints(width, append(m.dbTxHints(r.conn), "w submit", "u revert", "e edit", "+ row", "- delete", "]/[ page", "Tab view", "q close")...)
			case len(m.dbTxHints(r.conn)) > 0:
				hints = dbFitHints(width, append(m.dbTxHints(r.conn), "e edit", "+ row", "- delete", "]/[ page", "o sort", "f filter", "r rerun", "q close")...)
			case s.trans:
				hints = dbFitHints(width, "]/[ row", "t grid", "v value", "Tab view", "r rerun", "q close")
			case r.tv != nil && m.databaseEditable(s.curTab()):
				hints = dbFitHints(width, "]/[ page", "e edit", "f where", "o sort", "t switch view", "# count", "v value", "r rerun", "q close")
			case r.tv != nil:
				hints = dbFitHints(width, "]/[ page", "f where", "o sort", "t switch view", "# count", "v value", "r rerun", "q close")
			}
		} else if len(m.dbTxHints(r.conn)) > 0 {
			hints = dbFitHints(width, append(m.dbTxHints(r.conn), "Tab view", "r rerun", "h history", "q close")...)
		} else if r.tv != nil && res.Err != nil {
			hints = dbFitHints(width, "f where", "o sort", "r rerun", "Tab view", "q close")
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
		if plan {
			m.databasePlanView(&p, r.plan, width)
			break
		}
		if s.trans && r.result.Err == nil && len(r.result.Columns) > 0 {
			m.databaseTransposed(&p, s, width)
			break
		}
		if r.redis != nil && r.redis.info.Type == "string" && r.result.Err == nil && len(r.result.Columns) > 0 {
			databaseRedisStringView(&p, r.redis)
			break
		}
		m.databaseGrid(&p, s)
	case dbViewJSON:
		if plan {
			databasePlanRaw(&p, r.plan)
			break
		}
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
	idx := s.pageIdx()
	var edits *dbEdits
	if t := s.curTab(); t != nil && t.run == s.shown {
		edits = t.edits
	}
	more := dbGlyphMore.String()
	cell := func(c dbclient.Cell) string {
		return ansi.Truncate(dbOneLine(c.String()), dbMaxCellWidth, more)
	}
	// rows to draw: the page (pending values in place), then new rows.
	type gridRow struct {
		cells   []dbclient.Cell
		mark    string
		changed map[int]bool
		unset   map[int]bool // new row: the default
		deleted bool
	}
	grid := make([]gridRow, 0, len(page.Rows))
	for i, row := range page.Rows {
		g := gridRow{cells: row, mark: " "}
		if edits != nil && i < len(idx) {
			orig := idx[i]
			if edits.deleted[orig] {
				g.mark, g.deleted = dbGlyphDeleted.String(), true
			}
			for ref, c := range edits.cells {
				if ref.row != orig || ref.col >= len(row) {
					continue
				}
				if g.changed == nil {
					g.cells = append([]dbclient.Cell(nil), row...)
					g.changed = map[int]bool{}
				}
				g.cells[ref.col] = c
				g.changed[ref.col] = true
			}
			if g.changed != nil && !g.deleted {
				g.mark = dbGlyphChanged.String()
			}
		}
		grid = append(grid, g)
	}
	if edits != nil {
		for _, nr := range edits.added {
			g := gridRow{cells: make([]dbclient.Cell, len(page.Columns)), mark: dbGlyphAdded.String(), changed: map[int]bool{}, unset: map[int]bool{}}
			for i := range g.cells {
				if c, ok := nr.set[i]; ok {
					g.cells[i] = c
					g.changed[i] = true
				} else {
					g.cells[i] = dbclient.Cell{Text: "<default>"}
					g.unset[i] = true
				}
			}
			grid = append(grid, g)
		}
	}
	widths := make([]int, len(page.Columns))
	for i, c := range page.Columns {
		widths[i] = ansi.StringWidth(ansi.Truncate(dbOneLine(c.Name), dbMaxCellWidth, more))
		for _, row := range grid {
			if i < len(row.cells) {
				widths[i] = max(widths[i], ansi.StringWidth(cell(row.cells[i])))
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
	if len(grid) == 0 {
		n = p.add(" (no rows)")
		p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpSummary")
		return
	}
	p.first = len(p.lines) + 1
	for _, g := range grid {
		b.Reset()
		b.WriteString(g.mark)
		markEnd := b.Len()
		var dim, changed [][2]int
		for i := range page.Columns {
			if i > 0 {
				b.WriteString(bar)
			}
			c := dbclient.Cell{Null: true}
			if i < len(g.cells) {
				c = g.cells[i]
			}
			start := b.Len()
			b.WriteString(pad(cell(c), widths[i], c.Kind == "number"))
			switch {
			case g.unset[i] || c.Null && !g.changed[i]:
				dim = append(dim, [2]int{start, b.Len()})
			case g.changed[i]:
				changed = append(changed, [2]int{start, b.Len()})
			}
		}
		n = p.add(strings.TrimRight(b.String(), " "))
		end := len(p.lines[n-1])
		switch {
		case g.deleted:
			p.hl(n, 0, end, "TermocodeDbDeleted")
			continue
		case g.mark != " ":
			group := "TermocodeDbChanged"
			if g.unset != nil {
				group = "TermocodeDbAdded"
			}
			p.hl(n, 0, markEnd, group)
		}
		for _, sp := range dim {
			p.hl(n, sp[0], min(sp[1], end), "TermocodeHttpSep")
		}
		for _, sp := range changed {
			group := "TermocodeDbChanged"
			if g.unset != nil {
				group = "TermocodeDbAdded"
			}
			p.hl(n, sp[0], min(sp[1], end), group)
		}
		if g.unset != nil {
			p.lastNew = n
		}
	}
	if s.page+1 < s.pageCount() || (s.shown.tv != nil && s.shown.tv.more) || (s.shown.redis != nil && s.shown.redis.next != "") {
		n = p.add(" " + more + " ] next page")
		p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpHint")
	}
}

// dbFitHints joins as many key hints as fit the page, most useful first;
// the rest are dropped whole (no half hint). The row always ends with
// "? keys", so the full list is one key away at any width.
func dbFitHints(width int, parts ...string) string {
	tail := "  ? keys"
	out := ""
	for _, p := range parts {
		next := out + "  " + p
		if out == "" {
			next = " " + p
		}
		if ansi.StringWidth(next)+len(tail) > width {
			break
		}
		out = next
	}
	if out == "" {
		return " ? keys"
	}
	return out + tail
}

// dbTableSummary is row 2 of a data view: the rows on the page, the total
// once # counted it, and whether the server has a next page.
func dbTableSummary(tv *dbTableView, n int, ms int64) string {
	var b strings.Builder
	if n == 0 {
		b.WriteString("no rows")
	} else {
		fmt.Fprintf(&b, "rows %d-%d", tv.offset+1, tv.offset+n)
	}
	if tv.total >= 0 {
		fmt.Fprintf(&b, " of %d", tv.total)
	}
	if tv.more {
		b.WriteString(" · more")
	}
	if ms >= 0 {
		fmt.Fprintf(&b, " · %d ms", ms)
	}
	return b.String()
}

// databaseTransposed is the Transposed view: one row of the page as
// "column | value" lines. ] and [ step through the rows.
func (m *Model) databaseTransposed(p *dbPage, s *dbConsoleState, width int) {
	page := s.pageResult()
	if len(page.Rows) == 0 {
		n := p.add(" (no rows)")
		p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpSummary")
		return
	}
	more := dbGlyphMore.String()
	at := min(s.trow, len(page.Rows)-1)
	row := page.Rows[at]
	var changed map[int]bool
	deleted := false
	if t := s.curTab(); t != nil && t.run == s.shown && t.edits != nil {
		if idx := s.pageIdx(); at < len(idx) {
			orig := idx[at]
			deleted = t.edits.deleted[orig]
			for ref, c := range t.edits.cells {
				if ref.row == orig && ref.col < len(row) {
					if changed == nil {
						row = append([]dbclient.Cell(nil), row...)
						changed = map[int]bool{}
					}
					row[ref.col], changed[ref.col] = c, true
				}
			}
		}
	}
	nameW := 1
	names := make([]string, len(page.Columns))
	for i, c := range page.Columns {
		names[i] = ansi.Truncate(dbOneLine(c.Name), 24, more)
		nameW = max(nameW, ansi.StringWidth(names[i]))
	}
	bar := " " + dbGlyphVBar.String() + " "
	p.first = len(p.lines) + 1
	p.bounds = []int{0}
	for i := range page.Columns {
		c := dbclient.Cell{Null: true}
		if i < len(row) {
			c = row[i]
		}
		left := " " + names[i] + strings.Repeat(" ", nameW-ansi.StringWidth(names[i]))
		mark := ""
		switch {
		case deleted:
			mark = "  " + dbGlyphDeleted.String()
		case changed[i]:
			mark = "  " + dbGlyphChanged.String()
		}
		value := ansi.Truncate(dbOneLine(c.String()), max(width-nameW-ansi.StringWidth(bar)-2-len(mark), 8), more)
		n := p.add(strings.TrimRight(left+bar+value+mark, " "))
		p.hl(n, 0, len(left), "TermocodeHttpSection")
		switch {
		case deleted:
			p.hl(n, len(left)+len(bar), len(p.lines[n-1]), "TermocodeDbDeleted")
		case changed[i]:
			p.hl(n, len(left)+len(bar), len(p.lines[n-1]), "TermocodeDbChanged")
		case c.Null:
			p.hl(n, len(left)+len(bar), len(p.lines[n-1]), "TermocodeHttpSep")
		}
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
	switch {
	case s.cursorNewRow && p.lastNew > 0:
		cursor = p.lastNew
	case s.keepCursor:
		cursor = -1 // stay on the row being edited
	}
	s.cursorNewRow, s.keepCursor = false, false
	_ = m.nvim.ExecLuaArgs(`_G._termocode_db_open(...)`, m.editorPaneWidth() >= 120, p.lines[:end], p.hls, p.first, p.bounds, cursor, p.first == 0 && s.view != dbViewJSON && !p.fold, p.fold)
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
	if old.redis != nil {
		return m.databaseRedisOpen(old.conn, old.redis.db, old.redis.key, old.redis.from, old.redis.prev, s.curTab())
	}
	if old.tv != nil {
		return m.databaseRunTable(old.tv)
	}
	if old.describe != nil {
		return m.databaseLoadMeta(*old.describe, false)
	}
	if old.plan != nil { // plan again: an analyze asks its card again
		r := &dbRun{conn: old.conn, file: old.file, line: old.line, by: "user", sessKey: old.sessKey, tab: s.curTab(),
			plan: &dbPlanRun{analyze: old.plan.analyze}, statements: []string{old.plan.target}}
		return m.databaseExplainStart(r)
	}
	r := &dbRun{conn: old.conn, file: old.file, line: old.line, sql: old.sql, by: "user", statements: []string{old.sql}, sessKey: old.sessKey, tab: s.curTab()}
	return m.databaseReviewRun(r)
}

func (m *Model) databasePageKey(key string, row, col int) tea.Cmd {
	s := m.dbConsole()
	if s.job != nil && key == "esc" {
		m.databaseJobCancel()
		return nil
	}
	if s.running != nil {
		if key == "esc" {
			return m.databaseCancelRun()
		}
		return nil
	}
	if s.shown == nil {
		return nil
	}
	if s.submitting != nil {
		return m.pushToastDetail(toast.Info, "Saving the changes…", "One moment.")
	}
	if s.shown.redis != nil {
		if cmd, ok := m.databaseRedisPageKey(key, row, col); ok {
			return cmd
		}
	}
	tv := s.shown.tv
	canGrid := s.shown.result.Err == nil && len(s.shown.result.Columns) > 0 && s.shown.plan == nil
	switch key {
	case "x", "X":
		return m.databasePageExplain(key == "X")
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return m.databaseSwitchTab(int(key[0] - '0'))
	case "p":
		return m.databasePinTab()
	case "e", "E", "+", "-", "u", "w":
		if s.shown.plan != nil {
			return m.pushToastDetail(toast.Info, "A plan cannot be edited", "x explains again, r runs the plan again.")
		}
		return m.databaseEditKey(key, row, col)
	case "?":
		return m.databaseKeysHelp()
	case "K":
		return m.databaseEndTx(s.shown.conn, true, nil)
	case "Z":
		return m.databaseEndTx(s.shown.conn, false, nil)
	case "]":
		if s.trans && canGrid {
			if s.trow+1 < len(s.pageResult().Rows) {
				s.trow++
				return m.databaseShowPage()
			}
			if tv != nil && tv.more {
				return m.databaseUnlessDirty(func() tea.Cmd {
					if cmd, ok := m.databaseTablePage(tv, 1); ok {
						s.trow = 0
						return cmd
					}
					return nil
				})
			}
			return nil
		}
		if tv != nil {
			if !tv.more {
				return nil
			}
			return m.databaseUnlessDirty(func() tea.Cmd { cmd, _ := m.databaseTablePage(tv, 1); return cmd })
		}
		if s.page+1 >= s.pageCount() {
			return nil
		}
		s.page++
		return m.databaseShowPage()
	case "[":
		if s.trans && canGrid {
			if s.trow > 0 {
				s.trow--
				return m.databaseShowPage()
			}
			if tv != nil && tv.offset > 0 {
				return m.databaseUnlessDirty(func() tea.Cmd {
					if cmd, ok := m.databaseTablePage(tv, -1); ok {
						s.trow = max(s.size-1, 0)
						return cmd
					}
					return nil
				})
			}
			return nil
		}
		if tv != nil {
			if tv.offset <= 0 {
				return nil
			}
			return m.databaseUnlessDirty(func() tea.Cmd { cmd, _ := m.databaseTablePage(tv, -1); return cmd })
		}
		if s.page == 0 {
			return nil
		}
		s.page--
		return m.databaseShowPage()
	case "t":
		if !canGrid {
			return nil
		}
		s.trans = !s.trans
		if s.trans {
			s.view = dbViewGrid
			s.trow = 0
			if row >= 0 && row < len(s.pageResult().Rows) {
				s.trow = row
			}
		}
		return m.databaseShowPage()
	case "#":
		if tv == nil {
			return nil
		}
		return m.databaseCount(tv)
	case "tab":
		s.view = (s.view + 1) % dbViewCount
		return m.databaseShowPage()
	case "r":
		return m.databaseUnlessDirty(m.databaseRerun)
	case "h":
		return m.databaseHistory()
	case "f":
		if tv != nil {
			cmd := m.openHTTPPrompt(promptKindDBResultFilter, "Filter rows", "WHERE condition (empty: all rows):", tv.where)
			m.prompt.SetAllowEmpty(true)
			return cmd
		}
		cmd := m.openHTTPPrompt(promptKindDBResultFilter, "Filter rows", "Rows that contain (empty: all rows):", s.filter)
		m.prompt.SetAllowEmpty(true)
		return cmd
	case "o":
		var items []picker.Item
		for i, c := range s.shown.result.Columns {
			hint := ""
			sorted, desc := i == s.sortCol, s.desc
			if tv != nil {
				sorted, desc = c.Name == tv.orderCol, tv.desc
			}
			if sorted {
				hint = "ascending"
				if desc {
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
	case "S":
		return m.databaseExportStart()
	case "c":
		return m.copyWithToast(dbRedact(dbclient.ToCSV(s.displayedResult())), "Copied rows as CSV")
	case "C":
		return m.copyWithToast(dbRedact(dbclient.ToJSON(s.displayedResult())), "Copied rows as JSON")
	case "v", "y":
		if s.shown.plan != nil {
			if key == "y" && canPlan(s.shown) {
				return m.databasePlanCopy(s.shown.plan)
			}
			return nil
		}
		page := s.pageResult()
		if s.trans && s.view == dbViewGrid && s.trow < len(page.Rows) {
			row, col = s.trow, row // the cursor line is the column
		}
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

// canPlan is true when r shows a plan the page can copy.
func canPlan(r *dbRun) bool {
	return r.plan != nil && r.result.Err == nil && len(r.result.Columns) > 0
}
