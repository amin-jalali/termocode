package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/theme"
)

func (m Model) renderDatabasesSidebar(w, h int) string {
	s := m.databases
	if s == nil {
		s = newDBState()
	}
	focused := m.focus == FocusExplorer
	hint := ""
	if rows := s.rows(); s.cursor >= 0 && s.cursor < len(rows) {
		hint, _ = m.dbStoppedHint(rows[s.cursor], max(w-2, 0))
	}
	key := fmt.Sprintf("%d|%d|%d|%d|%t|%v|%s", s.seq, s.cursor, w, h, focused, theme.CurrentRenderKey(), hint)
	s.render.Lock()
	defer s.render.Unlock()
	if s.render.key == key && s.render.out != "" {
		return s.render.out
	}
	out := m.renderDatabasesSidebarRaw(w, h, focused)
	s.render.key, s.render.out = key, out
	return out
}

func (m Model) renderDatabasesSidebarRaw(w, h int, focused bool) string {
	s := m.databases
	if h <= 0 {
		return ""
	}
	fill := func() string { return theme.Bg(theme.BgSidebar).Render(strings.Repeat(" ", max(w, 0))) }
	muted := func(v string) string { return theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(v) }
	lines := []string{dbSidebarHeader(w, s.filter, s.filterTyping), theme.PaneHairline(w, theme.BgSidebar, focused)}
	noConns := len(s.conns) == 0
	actions, _ := dbHeaderActions(w, noConns)
	second := fill()
	if noConns {
		second = testsPad(theme.FgBg(theme.TextSecondary, theme.BgSidebar).Render(dbRemoteLabel()), w)
	}
	lines = append(lines, testsPad(theme.FgBg(theme.TextSecondary, theme.BgSidebar).Render(actions), w), second)
	rows := s.rows()
	available := max(h-len(lines)-2, 0)
	s.scroll = min(s.scroll, max(len(rows)-available, 0))
	if s.cursor < s.scroll {
		s.scroll = s.cursor
	} else if available > 0 {
		// Wrapped rows take several lines: scroll until the cursor row fits.
		for s.scroll < s.cursor && dbRowsHeight(rows, s.scroll, s.cursor+1, w) > available {
			s.scroll++
		}
	}
	start := s.scroll
	redisTypes := map[string]bool{} // the key types on screen, for the legend
	if noConns && len(rows) == 0 {
		// The one pattern of an empty state: title, a plain sentence, the button.
		lines = append(lines, renderEmptyState(w, available, theme.BgSidebar, dbEmptyState())...)
	}
	if s.filter != "" && !noConns && !dbHasSelectable(rows) && available > 0 {
		// Nothing matches: say so, and how to get the rows back.
		lines = append(lines, testsPad(muted("  "+runewidth.Truncate("No names match \""+sanitizeDBText(s.filter)+"\" · Esc clears", max(w-2, 1), "…")), w))
	}
	for i := start; i < len(rows) && len(lines) < 4+available; i++ {
		r := rows[i]
		if r.kind == dbNodeSuggestionsHeader {
			lines = append(lines, testsPad(theme.FgBg(theme.TextSecondary, theme.BgSidebar).Bold(true).Render("  SUGGESTED"), w))
			continue
		}
		glyph := dbNodeGlyph(r.kind)
		if r.kind == dbNodeRedisKey {
			glyph = dbRedisTypeGlyph(s.nodes[r.path].rtype)
			redisTypes[s.nodes[r.path].rtype] = true
		}
		fold := " "
		indent := strings.Repeat(" ", min(r.depth*2, max(w/2, 0)))
		if n, ok := s.nodes[r.path]; ok && (len(n.children) > 0 || dbNodeExpandable(n.kind)) {
			foldOpen := "▾"
			if theme.CurrentIconMode == theme.IconModeASCII {
				fold, foldOpen = ">", "v"
			} else {
				fold = "▸"
			}
			if !s.folded[r.path] {
				fold = foldOpen
			}
		}
		label := sanitizeDBText(r.name)
		if n, ok := s.nodes[r.path]; ok && n.kind == dbNodeConnection && n.detail != "" {
			label += "  " + sanitizeDBText(n.detail)
		} else if n, ok := s.nodes[r.path]; ok && n.kind == dbNodeColumn && n.detail != "" {
			label += "  [" + sanitizeDBText(n.detail) + "]"
		} else if ok && (n.kind == dbNodeRedisNS || n.kind == dbNodeDatabase) && n.detail != "" && w >= 28 {
			label += "  (" + sanitizeDBText(n.detail) + ")"
		}
		width := max(w-runewidth.StringWidth(indent)-3, 1)
		line := indent + fold + glyph + " " + runewidth.Truncate(label, width, "…")
		var more []string
		if wrapped := dbRowWrap(r, w); wrapped != nil {
			line = indent + fold + glyph + " " + wrapped[0]
			for _, l := range wrapped[1:] {
				more = append(more, indent+"   "+l)
			}
		}
		if i == s.cursor && focused {
			line = theme.FgBg(theme.TextPrimary, theme.BgSelection).Render(testsPad(line, w))
		} else if r.kind == dbNodeNote {
			line = theme.FgBg(theme.DiagError, theme.BgSidebar).Render(testsPad(line, w))
		} else if r.kind == dbNodeRedisMore || r.kind == dbNodeLoading {
			line = theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(testsPad(line, w))
		} else if r.kind == dbNodeCode {
			line = theme.FgBg(theme.TextSecondary, theme.BgPanel).Render(testsPad(line, w))
		} else {
			line = theme.FgBg(theme.TextPrimary, theme.BgSidebar).Render(testsPad(line, w))
		}
		lines = append(lines, line)
		for _, l := range more {
			if len(lines) >= 4+available {
				break
			}
			if i == s.cursor && focused {
				l = theme.FgBg(theme.TextPrimary, theme.BgSelection).Render(testsPad(l, w))
			} else if r.kind == dbNodeNote {
				l = theme.FgBg(theme.DiagError, theme.BgSidebar).Render(testsPad(l, w))
			} else {
				l = theme.FgBg(theme.TextSecondary, theme.BgPanel).Render(testsPad(l, w))
			}
			lines = append(lines, l)
		}
	}
	// A key type is a mark in the row: say once what the marks mean.
	if legend := dbRedisLegend(redisTypes, w-2); len(legend) > 0 && len(lines)+len(legend) <= h-2 {
		for len(lines) < h-2-len(legend) {
			lines = append(lines, fill())
		}
		for _, l := range legend {
			lines = append(lines, testsPad(muted("  "+l), w))
		}
	}
	for len(lines) < h-2 {
		lines = append(lines, fill())
	}
	if noConns && len(rows) == 0 {
		lines = append(lines, fill(), fill()) // the empty state above says it all
	} else if len(rows) > 0 && s.cursor >= 0 && s.cursor < len(rows) {
		r := rows[s.cursor]
		name := r.name
		if n := s.nodes[r.path]; n.kind == dbNodeRedisKey || n.kind == dbNodeRedisNS {
			name = dbclient.RedisDisplay(n.key) // the whole key, not the part under its namespace
		}
		footer := s.rowFooter(r)
		if hint, ok := m.dbStoppedHint(r, max(w-2, 0)); ok {
			footer = hint
		}
		if _, stopped := m.dbStoppedHint(r, max(w-2, 0)); !stopped {
			footer = dbFitFooter(footer, max(w-2, 1))
		}
		lines = append(lines, testsPad(muted("  "+sanitizeDBText(name)), w), testsPad(muted("  "+footer), w))
	} else if s.filter != "" {
		lines = append(lines, testsPad(muted("  "+dbFitFooter("Esc clears the filter", max(w-2, 1))), w), fill())
	} else {
		lines = append(lines, testsPad(muted("  "+dbFitFooter("No databases yet · a add", max(w-2, 1))), w), testsPad(muted("  "+dbFitFooter(".db and .env URLs show here", max(w-2, 1))), w))
	}
	for len(lines) < h {
		lines = append(lines, fill())
	}
	return strings.Join(lines[:h], "\n")
}

// dbEmptyState is the Databases view without connections.
func dbEmptyState() emptyState {
	return emptyState{Title: "No databases yet", Hint: "Add a connection, or put DATABASE_URL in .env.", Key: "a", Label: "Add connection"}
}

// dbHasSelectable reports whether rows has a row to move to.
func dbHasSelectable(rows []dbTreeRow) bool {
	for _, r := range rows {
		if r.selectable {
			return true
		}
	}
	return false
}

// dbRowWrap is the label of a note or command row cut into lines that fit
// the sidebar; nil for rows that keep to one line (and are cut with "…").
func dbRowWrap(r dbTreeRow, w int) []string {
	if r.kind != dbNodeNote && r.kind != dbNodeCode {
		return nil
	}
	indent := min(r.depth*2, max(w/2, 0))
	return wrapPlain(sanitizeDBText(r.name), max(w-indent-3, 1))
}

// dbRowHeight is how many lines row r takes in a sidebar w columns wide.
func dbRowHeight(r dbTreeRow, w int) int {
	if w <= 0 {
		return 1
	}
	return max(len(dbRowWrap(r, w)), 1)
}

// dbRowsHeight is the line count of rows[from:to].
func dbRowsHeight(rows []dbTreeRow, from, to, w int) int {
	n := 0
	for i := from; i < to && i < len(rows); i++ {
		n += dbRowHeight(rows[i], w)
	}
	return n
}

// dbInstallCommand is the shell command inside an install hint such as
// "Install sqlite3 with: sudo apt install sqlite3". The generic hint has none.
func dbInstallCommand(hint string) (string, bool) {
	_, cmd, ok := strings.Cut(hint, " with: ")
	cmd = strings.TrimSpace(cmd)
	return cmd, ok && cmd != ""
}

// dbRedisLegend lists the key types in view as `{ hash  " string`, packed
// into lines of at most width cells.
func dbRedisLegend(types map[string]bool, width int) []string {
	var lines []string
	cur := ""
	for _, t := range []string{"string", "hash", "list", "set", "zset", "stream"} {
		if !types[t] {
			continue
		}
		item := dbRedisTypeGlyph(t) + " " + t
		switch {
		case cur == "":
			cur = item
		case runewidth.StringWidth(cur)+2+runewidth.StringWidth(item) <= width:
			cur += "  " + item
		default:
			lines = append(lines, cur)
			cur = item
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// dbFitFooter cuts a footer made of " · " parts to width cells: whole parts
// are dropped from the end and "? keys" takes their place, so the full key
// list stays one key away. A footer that fits is left alone.
func dbFitFooter(text string, width int) string {
	if runewidth.StringWidth(text) <= width {
		return text
	}
	const tail = "? keys"
	parts := strings.Split(text, " · ")
	for n := len(parts) - 1; n >= 1; n-- {
		if s := strings.Join(parts[:n], " · ") + " · " + tail; runewidth.StringWidth(s) <= width {
			return s
		}
	}
	if runewidth.StringWidth(parts[0]) <= width {
		return parts[0]
	}
	return runewidth.Truncate(text, width, "…")
}

// dbRemoteLabel is the "Add remote…" line of the empty state.
func dbRemoteLabel() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "  Add remote... (SSH)"
	}
	return "  Add remote… (SSH)"
}

// dbZone is a clickable part of the header line.
type dbZone struct {
	x0, x1 int // columns [x0, x1)
	id     string
}

// dbHeaderActions is the action line under the title and its click zones.
// With no connections there is no "/ filter".
func dbHeaderActions(w int, noConns bool) (string, []dbZone) {
	type seg struct{ text, id string }
	var segs []seg
	switch {
	case w < 18:
		segs = []seg{{"a add", "add"}}
	case noConns || w < 31:
		segs = []seg{{"a add", "add"}, {"R refresh", "refresh"}}
	default:
		segs = []seg{{"a add", "add"}, {"R refresh", "refresh"}, {"/ filter", "filter"}}
	}
	sep := "   "
	if w < 31 {
		sep = "  " // the narrow header keeps its older look
	}
	out, x := "  ", 2
	var zones []dbZone
	for i, sg := range segs {
		if i > 0 {
			out += sep
			x += len(sep)
		}
		zones = append(zones, dbZone{x0: x, x1: x + len(sg.text), id: sg.id})
		out += sg.text
		x += len(sg.text)
	}
	return out, zones
}

func dbNodeExpandable(kind dbNodeKind) bool {
	switch kind {
	case dbNodeConnection, dbNodeDatabase, dbNodeSchema, dbNodeTable, dbNodeView, dbNodeRedisNS:
		return true
	default:
		return false
	}
}

func dbNodeGlyph(kind dbNodeKind) string {
	if theme.CurrentIconMode == theme.IconModeNerdFont {
		switch kind {
		case dbNodeConnection, dbNodeDatabase:
			return "\uf1c0"
		case dbNodeSchema, dbNodeGroup:
			return "\uf07b"
		case dbNodeTable, dbNodeView:
			return "\uf0ce"
		case dbNodeRoutine:
			return "\uf121"
		case dbNodeSequence:
			return "\uf292"
		case dbNodeColumn, dbNodeDetail:
			return "\uf0f6"
		case dbNodeSuggestion:
			return "\uf067"
		case dbNodeCode:
			return "$"
		case dbNodeRedisNS:
			return "\uf07b"
		case dbNodeRedisMore:
			return " "
		case dbNodeLoading:
			return "\uf110" // a spinner
		default:
			return "!"
		}
	}
	if theme.CurrentIconMode == theme.IconModeASCII {
		switch kind {
		case dbNodeConnection:
			return "*"
		case dbNodeDatabase:
			return "d"
		case dbNodeSchema:
			return "s"
		case dbNodeGroup, dbNodeTable:
			return "t"
		case dbNodeView:
			return "v"
		case dbNodeRoutine:
			return "f"
		case dbNodeSequence:
			return "#"
		case dbNodeColumn:
			return "."
		case dbNodeDetail:
			return "f"
		case dbNodeSuggestion:
			return "+"
		case dbNodeRedisNS:
			return "n"
		case dbNodeRedisMore:
			return " "
		case dbNodeLoading:
			return "~"
		default:
			return "!"
		}
	}
	switch kind {
	case dbNodeConnection:
		return theme.IconDatabases.String()
	case dbNodeDatabase:
		return theme.IconDatabases.String()
	case dbNodeSchema:
		return theme.IconFolderClosed.String()
	case dbNodeGroup:
		return theme.IconFolderClosed.String()
	case dbNodeTable:
		return theme.IconFile.String()
	case dbNodeView:
		return theme.IconFile.String()
	case dbNodeRoutine:
		return "ƒ"
	case dbNodeSequence:
		return "#"
	case dbNodeColumn:
		return "·"
	case dbNodeDetail:
		return theme.GlyphMore.String()
	case dbNodeSuggestion:
		return "+"
	case dbNodeSuggestionsHeader:
		return " "
	case dbNodeCode:
		return "$"
	case dbNodeRedisNS:
		return theme.IconFolderClosed.String()
	case dbNodeRedisMore:
		return " "
	case dbNodeLoading:
		return "◌"
	default:
		return "!"
	}
}

// rowFooter is the full-text hint for the row. Short details such as "PK"
// are spelled out here because the row itself is narrow.
func (s *dbState) rowFooter(r dbTreeRow) string {
	if n, ok := s.nodes[r.path]; ok && n.kind == dbNodeColumn && n.detail == "PK" {
		return "primary key"
	}
	if n, ok := s.nodes[r.path]; ok && s.isRedis(n.conn) {
		if f := s.dbRedisFooter(n); f != "" {
			return f
		}
	}
	if r.kind == dbNodeConnection {
		if _, ok := s.installRow(r); ok {
			return "Enter retry · y copy command"
		}
		if c, ok := s.connection(r.conn); ok && c.SSH != nil {
			state := "via " + c.SSH.Host
			if s.tunnel(c.ID).Alive() {
				state = "tunnel up · " + c.SSH.Host
			}
			return state + " · Enter connect · T test"
		}
	}
	return dbRowFooter(r.kind)
}

// installRow is the install-command row under a connection, if it has one.
func (s *dbState) installRow(r dbTreeRow) (dbTreeRow, bool) {
	for _, c := range s.nodes[r.path].children {
		if n := s.nodes[c]; n.kind == dbNodeCode {
			return dbTreeRow{kind: n.kind, path: c, name: n.name, conn: n.conn}, true
		}
	}
	return dbTreeRow{}, false
}

func dbRowFooter(kind dbNodeKind) string {
	switch kind {
	case dbNodeConnection:
		return "Enter connect · n console · e edit · x disconnect"
	case dbNodeSuggestion:
		return "a add suggestion · x hide"
	case dbNodeTable, dbNodeView:
		return "Enter expand · ? help"
	case dbNodeDatabase, dbNodeSchema, dbNodeGroup:
		return "Enter expand · h fold"
	case dbNodeCode:
		return "y copy command"
	default:
		return "Enter expand · ? help"
	}
}

func (m Model) databaseCurrentRow() (dbTreeRow, bool) {
	if m.databases == nil {
		return dbTreeRow{}, false
	}
	rows := m.databases.rows()
	i := m.databases.cursor
	if i < 0 || i >= len(rows) {
		return dbTreeRow{}, false
	}
	return rows[i], true
}

func (m Model) handleDatabasesSidebarMouse(x, y int, t tea.MouseEventType) (Model, tea.Cmd) {
	if m.databases == nil {
		return m, nil
	}
	if t == tea.MouseWheelUp || t == tea.MouseWheelDown {
		delta := -1
		if t == tea.MouseWheelDown {
			delta = 1
		}
		m.databaseMove(delta)
		m.databases.touch()
		return m, nil
	}
	if t != tea.MouseLeft && t != tea.MouseRight {
		return m, nil
	}
	if t == tea.MouseLeft && (y == 2 || y == 3) {
		return m.handleDatabasesHeaderClick(x, y)
	}
	localY := y - 4
	if localY < 0 {
		return m, nil
	}
	rows := m.databases.rows()
	if len(m.databases.conns) == 0 && len(rows) == 0 && t == tea.MouseLeft {
		return m, m.openDatabaseTypePicker() // the empty state's [Add connection (a)]
	}
	index := m.databases.scroll
	for line := 0; index < len(rows); index++ {
		line += dbRowHeight(rows[index], m.explorerWidth-1)
		if localY < line {
			break
		}
	}
	if index >= len(rows) {
		return m, nil
	}
	double := t == tea.MouseLeft && rows[index].path == m.databases.lastClickPath && time.Since(m.databases.lastClickAt) < 400*time.Millisecond
	m.databases.cursor = index
	m.databases.touch()
	if t == tea.MouseLeft {
		m.databases.lastClickPath, m.databases.lastClickAt = rows[index].path, time.Now()
		if double && rows[index].selectable {
			m.databases.lastClickAt = time.Time{}
			return m.databaseDoubleClick(rows[index])
		}
		return m, nil
	}
	if t == tea.MouseRight {
		r := rows[index]
		m.databases.menuRow = r
		items := []menu.Item{{ID: "connect", Title: "Connect or expand", Hint: "enter", Icon: "▸"}, {ID: "edit", Title: "Edit connection", Hint: "e", Icon: "✎"}, {ID: "password", Title: "Set password…", Hint: "p", Icon: "●"}, {ID: "txmode", Title: "Transaction mode…", Hint: "t", Icon: "⇄"}, {ID: "disconnect", Title: "Disconnect…", Hint: "x", Icon: "○"}, {Sep: true}, {ID: "refresh", Title: "Refresh", Hint: "R", Icon: "↻"}}
		if r.kind == dbNodeTable || r.kind == dbNodeView {
			items = []menu.Item{{ID: "data", Title: "Open data", Hint: "o", Icon: "▦"}, {ID: "describe", Title: "Describe", Hint: "d", Icon: "ⓘ"}, {ID: "ddl", Title: "Show DDL", Hint: "D", Icon: "≡"}, {ID: "select", Title: "Select 100 rows in console", Hint: "s", Icon: "▸"}, {ID: "count", Title: "Count rows", Hint: "#", Icon: "#"}, {ID: "copyname", Title: "Copy name", Hint: "y", Icon: "⧉"}, {Sep: true}, {ID: "refresh", Title: "Refresh", Hint: "R", Icon: "↻"}}
		}
		if r.kind == dbNodeSuggestion {
			items = []menu.Item{{ID: "add", Title: "Add connection", Hint: "a", Icon: "+"}, {ID: "hide", Title: "Hide suggestion", Hint: "x", Icon: theme.GlyphClose.String()}}
		}
		if r.kind == dbNodeRedisKey {
			items = []menu.Item{{ID: "connect", Title: "Open value", Hint: "enter", Icon: "▸"}, {ID: "redis-ttl", Title: "Set TTL…", Hint: "t", Icon: "◷"}, {ID: "redis-delete", Title: "Delete key…", Hint: "D", Icon: theme.GlyphClose.String()}, {ID: "copyname", Title: "Copy key", Hint: "y", Icon: "⧉"}, {Sep: true}, {ID: "refresh", Title: "Refresh", Hint: "R", Icon: "↻"}}
		}
		if _, stopped := m.dbStoppedHint(r, 200); stopped && r.kind == dbNodeConnection {
			items = append(items, menu.Item{ID: "start-service", Title: "Start service", Hint: "S", Icon: "▸"})
		}
		m.menu = menu.New(items, x+activity.Width, y)
		m.menu.SetScreenSize(m.w, m.h)
		m.menuOpen = true
		m.menuKind = menuKindDatabases
		return m, nil
	}
	return m, nil
}

// databaseDoubleClick: a connection connects or expands, a folder folds or
// unfolds, a table or view opens its data, a suggestion opens the Add card.
// Each one does what its Enter / o key does.
func (m Model) databaseDoubleClick(r dbTreeRow) (Model, tea.Cmd) {
	key := tea.KeyMsg{Type: tea.KeyEnter}
	if r.kind == dbNodeTable || r.kind == dbNodeView {
		key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}}
	}
	return m.handleDatabasesSidebarKey(key)
}

// handleDatabasesHeaderClick: the action line (a add, R refresh, / filter)
// and, with no connections, the Add remote line.
func (m Model) handleDatabasesHeaderClick(x, y int) (Model, tea.Cmd) {
	s := m.databases
	noConns := len(s.conns) == 0
	if y == 3 {
		if noConns && x >= 2 && x < 2+runewidth.StringWidth(strings.TrimSpace(dbRemoteLabel())) {
			return m, m.databaseAddRemote()
		}
		return m, nil
	}
	_, zones := dbHeaderActions(m.explorerWidth-1, noConns)
	for _, z := range zones {
		if x < z.x0 || x >= z.x1 {
			continue
		}
		switch z.id {
		case "add":
			return m, m.openDatabaseTypePicker()
		case "refresh":
			return m, m.databaseRefresh()
		case "filter":
			return m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		}
	}
	return m, nil
}

func (m *Model) handleDatabasesMenuSelect(id string) tea.Cmd {
	r := m.databases.menuRow
	switch id {
	case "connect":
		_, cmd := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
		return cmd
	case "edit":
		return m.databaseEdit(r.conn)
	case "password":
		updated, cmd := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
		*m = updated
		return cmd
	case "disconnect":
		updated, cmd := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
		*m = updated
		return cmd
	case "refresh":
		return m.databaseRefresh()
	case "txmode":
		return m.databaseTxModePicker(r.conn)
	case "data", "describe", "ddl", "select", "count", "copyname":
		key := map[string]rune{"data": 'o', "describe": 'd', "ddl": 'D', "select": 's', "count": '#', "copyname": 'y'}[id]
		updated, cmd := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		*m = updated
		return cmd
	case "redis-ttl", "redis-delete":
		key := map[string]rune{"redis-ttl": 't', "redis-delete": 'D'}[id]
		updated, cmd := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		*m = updated
		return cmd
	case "start-service":
		return m.databaseStartService(r.conn)
	case "add":
		return m.databaseAddSuggestion(r.path)
	case "hide":
		m.databases.hidden[r.path] = true
		m.databases.touch()
	}
	return nil
}
