package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The connection colour (DBX-21) outside the tree: a bar at the start of
// the result page's row 1 with the connection name in the colour, and a
// bar before the status-bar database chip.

// dbConnBar is the colour bar ("|" in ASCII mode).
func dbConnBar() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "|"
	}
	return "▌"
}

// dbConnTintGroup is the nvim highlight group of a connection colour
// ("blue" → "TermocodeDbConnBlue").
func dbConnTintGroup(color string) string {
	if color == "" {
		return ""
	}
	return "TermocodeDbConn" + strings.ToUpper(color[:1]) + color[1:]
}

// dbConnTintLua defines one highlight group per connection colour from
// the active palette; themeLua appends it, so a theme switch recolours it.
func dbConnTintLua() string {
	var b strings.Builder
	for _, c := range dbclient.ConnColors() {
		col, ok := dbConnTint(c)
		if !ok {
			continue
		}
		b.WriteString("hi('" + dbConnTintGroup(c) + "', { fg = '" + theme.ToRGB(col).Hex() + "', bold = true })\n")
	}
	return b.String()
}

// dbRunTint is the colour of the connection a run went to ("" when it has
// none or the connection is gone).
func (m *Model) dbRunTint(conn string) string {
	if m.databases == nil {
		return ""
	}
	c, ok := m.databases.connection(conn)
	if !ok {
		return ""
	}
	if _, ok := dbConnTint(c.Color); !ok {
		return ""
	}
	return c.Color
}
