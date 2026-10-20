package app

import (
	"fmt"
	"strings"

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
	key := fmt.Sprintf("%d|%d|%d|%d|%t|%v", s.seq, s.cursor, w, h, focused, theme.CurrentRenderKey())
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
	if len(s.conns) == 0 && len(s.rows()) == 0 {
		lines = append(lines, renderEmptyState(w, h-2, theme.BgSidebar, emptyState{Glyph: theme.IconDatabases, Title: "No databases yet", Hint: "Add a connection or put DATABASE_URL in .env", Key: "a"})...)
		for len(lines) < h {
			lines = append(lines, fill())
		}
		return strings.Join(lines[:h], "\n")
	}
	actions := "  a add   R refresh   / filter"
	if w < 31 {
		actions = "  a add  R refresh"
	}
	if w < 18 {
		actions = "  a add"
	}
	lines = append(lines, testsPad(theme.FgBg(theme.TextSecondary, theme.BgSidebar).Render(actions), w), fill())
	rows := s.rows()
	available := max(h-len(lines)-2, 0)
	if s.cursor < s.scroll {
		s.scroll = s.cursor
	} else if available > 0 && s.cursor >= s.scroll+available {
		s.scroll = s.cursor - available + 1
	}
	s.scroll = min(s.scroll, max(len(rows)-available, 0))
	start := s.scroll
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
		lines = append(lines, testsPad(muted("  "+sanitizeDBText(name)), w), testsPad(muted("  "+s.rowFooter(r)), w))
	} else {
		lines = append(lines, testsPad(muted("  No databases yet · a add one"), w), testsPad(muted("  .db files and .env URLs appear here"), w))
	}
	for len(lines) < h {
		lines = append(lines, fill())
	}
	return strings.Join(lines[:h], "\n")
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
	localY := y - 4
	if localY < 0 {
		return m, nil
	}
	rows := m.databases.rows()
	index := m.databases.scroll + localY
	if index >= len(rows) {
		return m, nil
	}
	m.databases.cursor = index
	m.databases.touch()
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
		m.menu = menu.New(items, x+activity.Width, y)
		m.menu.SetScreenSize(m.w, m.h)
		m.menuOpen = true
		m.menuKind = menuKindDatabases
		return m, nil
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
	case "add":
		return m.databaseAddSuggestion(r.path)
	case "hide":
		m.databases.hidden[r.path] = true
		m.databases.touch()
	}
	return nil
}
