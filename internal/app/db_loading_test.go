package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/theme"
)

// DB-17: the "Loading…" row of the Databases tree is a quiet row, not a
// problem: no "!" glyph and not the error colour, in every icon mode.
func TestDatabaseLoadingRowIsQuiet(t *testing.T) {
	withConfigDir(t)
	oldProfile, oldIcons := lipgloss.ColorProfile(), theme.CurrentIconMode
	defer func() { lipgloss.SetColorProfile(oldProfile); theme.CurrentIconMode = oldIcons }()
	lipgloss.SetColorProfile(termenv.TrueColor)

	s := newDBState()
	s.loaded = true
	s.conns = []dbclient.Conn{{ID: "pg", Name: "pg", Driver: "postgres"}}
	s.nodes["pg"] = dbNode{kind: dbNodeConnection, name: "pg", path: "pg", conn: "pg", loading: true}
	var loading *dbTreeRow
	rows := s.rows()
	for i := range rows {
		if strings.HasPrefix(rows[i].name, "Loading") {
			loading = &rows[i]
		}
	}
	if loading == nil || loading.kind != dbNodeLoading || loading.selectable {
		t.Fatalf("loading row = %+v (rows %+v)", loading, rows)
	}
	// What an error row starts with, to tell it from a quiet row.
	errStart, _, _ := strings.Cut(theme.FgBg(theme.DiagError, theme.BgSidebar).Render("x"), "x")
	for _, mode := range []theme.IconMode{theme.IconModeUnicode, theme.IconModeASCII, theme.IconModeNerdFont} {
		theme.CurrentIconMode = mode
		m := Model{databases: s, w: 60, h: 14}
		var line string
		for _, l := range strings.Split(m.renderDatabasesSidebarRaw(40, 14, false), "\n") {
			if strings.Contains(stripSGRForTest(l), "Loading") {
				line = l
			}
		}
		if line == "" {
			t.Fatalf("mode %v: no Loading line", mode)
		}
		if strings.Contains(line, errStart) {
			t.Errorf("mode %v: the Loading row is drawn in the error colour: %q", mode, line)
		}
		if plain := strings.TrimSpace(stripSGRForTest(line)); strings.HasPrefix(plain, "!") {
			t.Errorf("mode %v: the Loading row has the error glyph: %q", mode, plain)
		}
	}
	// The first load (no connection list yet) is quiet too.
	theme.CurrentIconMode = theme.IconModeUnicode
	first := newDBState()
	if r := first.rows(); len(r) != 1 || r[0].kind != dbNodeLoading {
		t.Errorf("first load row = %+v", r)
	}
}
