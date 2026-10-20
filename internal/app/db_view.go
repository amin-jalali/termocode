package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/activity"
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
	lines := []string{sidebarHeader(w, "DATABASES", ""), theme.PaneHairline(w, theme.BgSidebar, focused)}
	noConns := len(s.conns) == 0
	actions, _ := dbHeaderActions(w, noConns)
	second := fill()
	if noConns {
		second = testsPad(theme.FgBg(theme.TextSecondary, theme.BgSidebar).Render(dbRemoteLabel()), w)
	}
	lines = append(lines, testsPad(theme.FgBg(theme.TextSecondary, theme.BgSidebar).Render(actions), w), second)
	rows := s.rows()
	available := max(h-len(lines)-2, 0)
	if s.cursor < s.scroll {
		s.scroll = s.cursor
	} else if available > 0 && s.cursor >= s.scroll+available {
		s.scroll = s.cursor - available + 1
	}
	s.scroll = min(s.scroll, max(len(rows)-available, 0))
	start := s.scroll
	if noConns && len(rows) == 0 {
		hint := append([]string{"No databases yet"}, wrapPlain("Put DATABASE_URL in .env or start a database container.", max(w-4, 8))...)
		for i, l := range hint {
			style := theme.FgBg(theme.TextDim, theme.BgSidebar)
			if i == 0 {
				style = theme.FgBg(theme.TextSecondary, theme.BgSidebar).Bold(true)
			}
			if i < available {
				lines = append(lines, testsPad(style.Render("  "+l), w))
			}
		}
	}
	for i := start; i < len(rows) && i < start+available; i++ {
		r := rows[i]
		if r.kind == dbNodeSuggestionsHeader {
			lines = append(lines, testsPad(theme.FgBg(theme.TextSecondary, theme.BgSidebar).Bold(true).Render("  SUGGESTED"), w))
			continue
		}
		glyph := dbNodeGlyph(r.kind)
		if r.kind == dbNodeRedisKey {
			glyph = dbRedisTypeGlyph(s.nodes[r.path].rtype)
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
		if i == s.cursor && focused {
			line = theme.FgBg(theme.TextPrimary, theme.BgSelection).Render(testsPad(line, w))
		} else if r.kind == dbNodeNote {
			line = theme.FgBg(theme.DiagError, theme.BgSidebar).Render(testsPad(line, w))
		} else if r.kind == dbNodeRedisMore {
			line = theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(testsPad(line, w))
		} else if r.kind == dbNodeCode {
			line = theme.FgBg(theme.TextSecondary, theme.BgPanel).Render(testsPad(line, w))
		} else {
			line = theme.FgBg(theme.TextPrimary, theme.BgSidebar).Render(testsPad(line, w))
		}
		lines = append(lines, line)
	}
	for len(lines) < h-2 {
		lines = append(lines, fill())
	}
	if len(rows) > 0 && s.cursor >= 0 && s.cursor < len(rows) {
		r := rows[s.cursor]
		name := r.name
		if n := s.nodes[r.path]; n.kind == dbNodeRedisKey || n.kind == dbNodeRedisNS {
			name = dbclient.RedisDisplay(n.key) // the whole key, not the part under its namespace
		}
		footer := s.rowFooter(r)
		if hint, ok := m.dbStoppedHint(r, max(w-2, 0)); ok {
			footer = hint
		}
		lines = append(lines, testsPad(muted("  "+sanitizeDBText(name)), w), testsPad(muted("  "+footer), w))
	} else {
		lines = append(lines, testsPad(muted("  No databases yet · a add one"), w), testsPad(muted("  .db files and .env URLs appear here"), w))
	}
	for len(lines) < h {
		lines = append(lines, fill())
	}
	return strings.Join(lines[:h], "\n")
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
// With no connections it spells out "add connection".
func dbHeaderActions(w int, noConns bool) (string, []dbZone) {
	type seg struct{ text, id string }
	var segs []seg
	switch {
	case noConns && w >= 29:
		segs = []seg{{"a add connection", "add"}, {"R refresh", "refresh"}}
	case noConns && w >= 20:
		segs = []seg{{"a add connection", "add"}}
	case noConns || w < 18:
		segs = []seg{{"a add", "add"}}
	case w < 31:
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
		return "y copy install command"
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
	index := m.databases.scroll + localY
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
		items := []menu.Item{{ID: "connect", Title: "Connect or expand", Hint: "enter", Icon: "▸"}, {ID: "edit", Title: "Edit connection", Hint: "e", Icon: "✎"}, {ID: "password", Title: "Set password…", Hint: "p", Icon: "●"}, {ID: "txmode", Title: "Transaction mode…", Hint: "t", Icon: "⇄"}, {ID: "disconnect", Title: "Disconnect", Hint: "x", Icon: "○"}, {Sep: true}, {ID: "refresh", Title: "Refresh", Hint: "R", Icon: "↻"}}
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
