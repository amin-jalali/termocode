package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/theme"
)

// DB-14: guards for the Databases views. Render invariants on poor
// terminals, wide / ambiguous-width text, and the cost of the Go paths
// (render cache, result cache, grid, schema push).

// Every Databases scene is exactly w x h cells and sends no 24-bit /
// 256-colour SGR to a 16-colour terminal and no colour SGR at all to a
// no-colour (NO_COLOR) one; in ASCII icon mode no Nerd Font glyph appears.
func TestDatabasesRenderInvariantsPoorTerminals(t *testing.T) {
	withConfigDir(t)
	oldProfile := lipgloss.ColorProfile()
	oldIcons := theme.CurrentIconMode
	defer func() {
		lipgloss.SetColorProfile(oldProfile)
		theme.CurrentIconMode = oldIcons
		theme.ApplyTheme("vscode-dark-plus")
	}()
	const w, h = 100, 30
	m0, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: w, Height: h})
	base := m0.(Model)
	scenes := []struct{ name, arg, marker string }{
		{"tree", "", "users"}, {"empty", "empty", "No databases yet"}, {"no-sqlite", "no-sqlite", "sqlite3"},
		{"stopped", "stopped", "DATABASES"}, {"redis", "redis", "session:"},
		{"ssh-host", "ssh-host", ""}, {"ssh-form", "ssh-form", ""}, {"ssl-form", "ssl-form", ""},
		{"compare-pick", "compare-pick", "SOURCE"}, {"compare", "compare", "tables.sql"},
	}
	truecolor := regexp.MustCompile(`\x1b\[[0-9;]*[34]8;[25];`)
	anyColor := regexp.MustCompile(`\x1b\[[0-9;]*([34][0-9]|9[0-7]|10[0-7])(;[0-9;]*)?m`)
	nerd := regexp.MustCompile("[-\U000F0000-\U000FFFFD]")
	for _, prof := range []termenv.Profile{termenv.ANSI, termenv.Ascii} {
		for _, icons := range []theme.IconMode{theme.IconModeASCII, theme.IconModeUnicode, theme.IconModeNerdFont} {
			for _, th := range []string{"vscode-dark-plus", "catppuccin-latte"} {
				lipgloss.SetColorProfile(prof)
				theme.CurrentIconMode = icons
				theme.ApplyTheme(th)
				for _, sc := range scenes {
					m := base
					m.runDevAction("dbdemo:" + sc.arg)
					if sc.arg == "" {
						m.runDevAction("dbdemo")
					}
					out := m.View()
					tag := fmt.Sprintf("%s/%s prof=%v icons=%v", sc.name, th, prof, icons)
					rows := strings.Split(out, "\n")
					if len(rows) != h {
						t.Fatalf("%s: %d rows, want %d", tag, len(rows), h)
					}
					for i, r := range rows {
						if got := lipgloss.Width(r); got != w {
							t.Fatalf("%s: row %d is %d cells:\n%q", tag, i, got, r)
						}
					}
					if sc.marker != "" && !strings.Contains(ansi.Strip(out), sc.marker) {
						t.Errorf("%s: scene did not render (missing %q)", tag, sc.marker)
					}
					if truecolor.MatchString(out) {
						t.Errorf("%s: 24-bit / 256-colour SGR on a 16-colour terminal: %q", tag, truecolor.FindString(out))
					}
					if prof == termenv.Ascii && anyColor.MatchString(out) {
						t.Errorf("%s: colour SGR on a no-colour terminal: %q", tag, anyColor.FindString(out))
					}
					if icons == theme.IconModeASCII && nerd.MatchString(ansi.Strip(out)) {
						t.Errorf("%s: Nerd Font glyph %q in ASCII icon mode", tag, nerd.FindString(ansi.Strip(out)))
					}
				}
			}
		}
	}
}

// wideNames hold CJK, emoji, combining marks and East Asian ambiguous
// characters (Greek, arrows, box drawing, ° ±) that a terminal may draw
// one or two cells wide.
var wideNames = []string{"日本語のテーブル", "café_naïve", "ΑΒΓ_δελτα", "→←↑↓ arrows", "°±×÷ math", "👨‍👩‍👧 family", "é́ combining", "━━┃┃ box", "ａｂｃ fullwidth"}

// The sidebar keeps every row exactly w cells with wide text in names,
// at several widths, whether the terminal counts ambiguous characters as
// one cell (default) or two (CJK locales).
func TestDatabasesSidebarWideAndAmbiguousWidthRows(t *testing.T) {
	withConfigDir(t)
	oldEA := runewidth.DefaultCondition.EastAsianWidth
	defer func() { runewidth.DefaultCondition.EastAsianWidth = oldEA }()
	for _, ea := range []bool{false, true} {
		runewidth.DefaultCondition.EastAsianWidth = ea
		for _, w := range []int{14, 20, 24, 31, 40, 60} {
			m := &Model{activity: activity.New(), showExp: true}
			m.activity.SetActive(activity.ViewDatabases)
			s := newDBState()
			s.loaded = true
			m.databases = s
			c := dbclient.Conn{ID: "wide", Name: "wide", Driver: "postgres", Host: "h", Port: 5432}
			s.conns = []dbclient.Conn{c}
			kids := []string{}
			s.nodes["wide"] = dbNode{kind: dbNodeConnection, name: "wide", path: "wide", conn: "wide", detail: "postgres  日本:5432", loaded: true}
			for i, n := range wideNames {
				p := fmt.Sprintf("wide/t%d", i)
				s.nodes[p] = dbNode{kind: dbNodeTable, name: n, path: p, conn: "wide", loaded: true}
				kids = append(kids, p)
			}
			n := s.nodes["wide"]
			n.children = kids
			s.nodes["wide"] = n
			s.touch()
			for cursor := 0; cursor < len(wideNames)+1; cursor++ {
				s.cursor = cursor
				s.touch()
				out := m.renderDatabasesSidebarRaw(w, 20, cursor%2 == 0)
				for i, r := range strings.Split(out, "\n") {
					if got := lipgloss.Width(r); got != w {
						t.Fatalf("ea=%v w=%d cursor=%d row %d is %d cells:\n%q", ea, w, cursor, i, got, r)
					}
				}
			}
		}
	}
}

func dbWideResult(rows int) dbclient.Result {
	res := dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "名前"}, {Name: "note"}, {Name: "ΑΒΓ"}}}
	for i := 0; i < rows; i++ {
		res.Rows = append(res.Rows, []dbclient.Cell{
			{Text: fmt.Sprint(i), Kind: "number"},
			{Text: wideNames[i%len(wideNames)]},
			{Text: "line one\nline two " + wideNames[(i+3)%len(wideNames)]},
			{Text: fmt.Sprintf("v%d→%d", i, i*7)},
		})
	}
	return res
}

func dbGridModel(res dbclient.Result) *Model {
	m := &Model{w: 120, h: 40}
	r := &dbRun{conn: "local", sql: "SELECT 1", by: "user", result: res}
	s := m.dbConsole()
	s.placeRun(r)
	s.cache, s.size, s.page, s.sortCol, s.filter = nil, 100, 0, -1, ""
	s.view = dbViewGrid
	return m
}

// In the result grid every row shows its column separators at the same
// display cells, with CJK, emoji and ambiguous characters in the cells.
func TestDatabasesGridColumnsAlignWithWideText(t *testing.T) {
	withConfigDir(t)
	m := dbGridModel(dbWideResult(len(wideNames) * 2))
	p := m.databasePage()
	if p.first == 0 || len(p.bounds) != 4 {
		t.Fatalf("no grid: first=%d bounds=%v", p.first, p.bounds)
	}
	bar := dbGlyphVBar.String()
	for i := p.first - 1; i < len(p.lines); i++ {
		line := p.lines[i]
		if strings.Contains(line, "next page") {
			continue
		}
		at := 0
		for col := 1; col < len(p.bounds); col++ {
			idx := strings.Index(line[at:], bar)
			if idx < 0 {
				t.Fatalf("row %d lacks separator %d: %q", i+1, col, line)
			}
			at += idx
			if got, want := ansi.StringWidth(line[:at])+1, p.bounds[col]-1; got != want {
				t.Fatalf("row %d: separator %d at cell %d, want %d: %q", i+1, col, got, want, line)
			}
			at += len(bar)
		}
		if strings.ContainsAny(line, "\n\r") {
			t.Fatalf("row %d holds a line break: %q", i+1, line)
		}
	}
}

// Spec: the result cache hands back the same rows until the filter, the
// sort or the loaded row count changes, and building it is paid once.
func TestPerfGuardDatabasesDisplayedResultCache(t *testing.T) {
	// pageIdx reads config.json; a file left in the shared config dir by
	// another test adds its read and JSON parse to the count.
	withConfigDir(t)
	m := dbGridModel(dbWideResult(20000))
	s := m.dbConsole()
	first := s.displayedResult()
	if len(first.Rows) != 20000 {
		t.Fatalf("rows = %d", len(first.Rows))
	}
	if n := testing.AllocsPerRun(50, func() { _ = s.displayedResult() }); n > 0 {
		t.Errorf("cached displayedResult allocates %.0f times", n)
	}
	if n := testing.AllocsPerRun(50, func() { _ = s.pageIdx() }); n > maxAllocsDBPageIdx {
		t.Errorf("pageIdx allocates %.0f times (max %d)", n, maxAllocsDBPageIdx)
	}
	// A filter rebuilds it; clearing the filter rebuilds again.
	s.filter = "family"
	if got := s.displayedResult(); len(got.Rows) == 0 || len(got.Rows) >= 20000 {
		t.Fatalf("filtered rows = %d", len(got.Rows))
	}
	s.filter = ""
	if got := s.displayedResult(); len(got.Rows) != 20000 {
		t.Fatalf("filter cleared: rows = %d", len(got.Rows))
	}
	s.sortCol, s.desc = 0, true
	if got := s.displayedResult(); got.Rows[0][0].Text != "9999" && got.Rows[0][0].Text != "19999" {
		t.Fatalf("sorted rows start with %q", got.Rows[0][0].Text)
	}
}

// The alloc guard of one grid page (100 rows x 4 columns) and its time
// budget with 20 000 rows loaded: only the page is formatted.
const (
	maxAllocsDBPageIdx   = 8    // measured 4
	maxAllocsDBGridPage  = 7000 // measured ~5200 for 100 rows x 4 columns (1 ms)
	maxMillisDBGridPage  = 60
	maxMillisDBSchemaFor = 50
)

func TestPerfGuardDatabasesGridPage(t *testing.T) {
	withConfigDir(t)
	m := dbGridModel(dbWideResult(20000))
	s := m.dbConsole()
	_ = m.databasePage() // warm the result cache
	if n := testing.AllocsPerRun(20, func() { _ = m.databasePage() }); n > maxAllocsDBGridPage {
		t.Errorf("one grid page: %.0f allocs (max %d)", n, maxAllocsDBGridPage)
	}
	start := time.Now()
	for i := 0; i < 10; i++ {
		_ = m.databasePage()
	}
	if ms := time.Since(start).Milliseconds() / 10; ms > maxMillisDBGridPage && !raceEnabled { // the race detector is ~20x slower
		t.Errorf("one grid page took %d ms (max %d)", ms, maxMillisDBGridPage)
	}
	p := m.databasePage()
	if rows := len(p.lines) - p.first + 1; rows > 102 {
		t.Errorf("a page formatted %d rows of 20000", rows)
	}
	// Paging to the end only formats the last rows.
	s.page = 199
	if p := m.databasePage(); len(p.lines)-p.first+1 > 102 {
		t.Errorf("last page formatted %d lines", len(p.lines)-p.first+1)
	}
}

func BenchmarkDatabasesGridPage(b *testing.B) {
	m := dbGridModel(dbWideResult(20000))
	_ = m.databasePage()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.databasePage()
	}
}

func BenchmarkDatabasesSidebarRender(b *testing.B) {
	m := &Model{}
	m.runDevAction("dbdemo")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.databases.touch()
		_ = m.renderDatabasesSidebar(40, 30)
	}
}

// Spec: the sidebar is rendered once per state. The same state, size and
// theme returns the cached frame; a change (cursor, size, theme) draws a
// new one.
func TestPerfGuardDatabasesSidebarRenderCache(t *testing.T) {
	withConfigDir(t)
	m := &Model{}
	m.runDevAction("dbdemo")
	s := m.databases
	first := m.renderDatabasesSidebar(40, 30)
	if !strings.Contains(ansi.Strip(first), "users") {
		t.Fatalf("sidebar did not render the tree:\n%s", ansi.Strip(first))
	}
	if n := testing.AllocsPerRun(100, func() { _ = m.renderDatabasesSidebar(40, 30) }); n > maxAllocsDBSidebarCached {
		t.Errorf("cached sidebar: %.0f allocs (max %d)", n, maxAllocsDBSidebarCached)
	}
	// A poisoned cache entry is served while the key is unchanged ...
	s.render.Lock()
	s.render.out = "CACHED"
	s.render.Unlock()
	if got := m.renderDatabasesSidebar(40, 30); got != "CACHED" {
		t.Fatal("same state was rendered again")
	}
	// ... and dropped for every input that changes the picture.
	s.cursor++
	if got := m.renderDatabasesSidebar(40, 30); got == "CACHED" {
		t.Error("cursor moved but the cached frame was served")
	}
	if got := m.renderDatabasesSidebar(41, 30); got == "CACHED" || lipgloss.Width(strings.Split(got, "\n")[0]) != 41 {
		t.Error("width changed but the cached frame was served")
	}
	s.touch()
	m.renderDatabasesSidebar(40, 30)
	s.render.Lock()
	s.render.out = "CACHED"
	s.render.Unlock()
	s.touch()
	if got := m.renderDatabasesSidebar(40, 30); got == "CACHED" {
		t.Error("tree changed (touch) but the cached frame was served")
	}
}

const maxAllocsDBSidebarCached = 16 // the cache key and the stopped hint (measured 7)

// Spec: a hidden Databases view draws nothing and an idle visible one
// asks for no work.
func TestPerfGuardDatabasesViewHiddenAndIdle(t *testing.T) {
	withConfigDir(t)
	m0, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := m0.(Model)
	m.runDevAction("dbdemo")
	s := m.databases
	_ = m.View()
	s.render.Lock()
	drawn := s.render.out != ""
	s.render.out, s.render.key = "", ""
	s.render.Unlock()
	if !drawn {
		t.Fatal("a visible Databases view did not draw")
	}
	seq := s.seq
	// Idle: ticks and size-less messages change nothing and start nothing.
	for _, msg := range []tea.Msg{aiActivityTickMsg{}, struct{}{}} {
		next, cmd := m.Update(msg)
		m = next.(Model)
		if cmd != nil {
			if out := cmd(); out != nil {
				t.Errorf("idle Databases view answered %T with %T", msg, out)
			}
		}
	}
	if s.seq != seq {
		t.Errorf("idle messages changed the tree (seq %d -> %d)", seq, s.seq)
	}
	// Hidden: another view is active, so the sidebar is never rendered.
	next, _ := m.Update(activity.SwitchMsg{View: activity.ViewFiles})
	m = next.(Model)
	s.render.Lock()
	s.render.out, s.render.key = "", ""
	s.render.Unlock()
	_ = m.View()
	s.render.Lock()
	out := s.render.out
	s.render.Unlock()
	if out != "" {
		t.Error("a hidden Databases view was rendered")
	}
}

// Spec: the schema Go builds for completion is cheap for thousands of
// tables and is not pushed again until the tree changes.
func TestPerfGuardDatabasesSchemaPayload(t *testing.T) {
	s := newDBState()
	c := dbclient.Conn{ID: "big", Driver: "postgres"}
	s.conns = []dbclient.Conn{c}
	root := dbNode{kind: dbNodeConnection, path: "big", conn: "big", loaded: true}
	for i := 0; i < 5000; i++ {
		p := fmt.Sprintf("big/t%d", i)
		s.nodes[p] = dbNode{kind: dbNodeTable, name: fmt.Sprintf("table_%04d", i), path: p, conn: "big", loaded: true}
		root.children = append(root.children, p)
	}
	s.nodes["big"] = root
	start := time.Now()
	var payload map[string]any
	for i := 0; i < 5; i++ {
		payload = s.schemaPayload(c)
	}
	if ms := time.Since(start).Milliseconds() / 5; ms > maxMillisDBSchemaFor && !raceEnabled {
		t.Errorf("schema payload for 5000 tables took %d ms (max %d)", ms, maxMillisDBSchemaFor)
	}
	if tables, _ := payload["tables"].([]map[string]any); len(tables) != 5000 {
		if alt, ok := payload["tables"].([]any); !ok || len(alt) != 5000 {
			t.Errorf("payload tables = %T len?", payload["tables"])
		}
	}
}

// A saved SQLite connection with a relative path ("data.db", what the Add
// flow writes) must not be suggested again for the same file.
func TestDatabasesSuggestionSkipsSavedRelativeSQLitePath(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"app.db", "other.db"} {
		if err := os.WriteFile(filepath.Join(root, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := newDBState()
	s.root = root
	s.conns = []dbclient.Conn{{ID: "app", Driver: "sqlite", Path: "app.db"}}
	s.findSuggestions()
	if _, dup := s.nodes["suggest-app.db"]; dup {
		t.Error("app.db is saved as a connection but still suggested")
	}
	if _, ok := s.nodes["suggest-other.db"]; !ok {
		t.Error("an unsaved .db file is no longer suggested")
	}
}

// Spec (DB-15): the compare scenes are exactly w x h at every golden size
// in every icon mode, and an open compare card asks for no work while
// idle (no timer, no poll).
func TestDatabasesCompareScenesFitAndIdle(t *testing.T) {
	withConfigDir(t)
	oldIcons := theme.CurrentIconMode
	defer func() { theme.CurrentIconMode = oldIcons }()
	nerd := regexp.MustCompile("[\ue000-\uf8ff\U000F0000-\U000FFFFD]")
	for _, size := range [][2]int{{80, 24}, {100, 30}, {150, 42}} {
		for _, icons := range []theme.IconMode{theme.IconModeASCII, theme.IconModeNerdFont} {
			theme.CurrentIconMode = icons
			for _, sc := range []string{"compare-pick", "compare"} {
				m0, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m := m0.(Model)
				m.runDevAction("dbdemo:" + sc)
				out := m.View()
				tag := fmt.Sprintf("%s %dx%d icons=%v", sc, size[0], size[1], icons)
				rows := strings.Split(out, "\n")
				if len(rows) != size[1] {
					t.Fatalf("%s: %d rows", tag, len(rows))
				}
				for i, r := range rows {
					if got := lipgloss.Width(r); got != size[0] {
						t.Fatalf("%s: row %d is %d cells:\n%q", tag, i, got, r)
					}
				}
				plain := ansi.Strip(out)
				if icons == theme.IconModeASCII && nerd.MatchString(plain) {
					t.Errorf("%s: Nerd Font glyph %q in ASCII mode", tag, nerd.FindString(plain))
				}
				if sc == "compare" {
					for _, want := range []string{"Compare schemas", "target: stage-pg", "Copy migration script", "copy script"} {
						if !strings.Contains(plain, want) {
							t.Errorf("%s: lacks %q:\n%s", tag, want, plain)
						}
					}
					for _, msg := range []tea.Msg{aiActivityTickMsg{}, struct{}{}} {
						next, cmd := m.Update(msg)
						m = next.(Model)
						if cmd != nil {
							if got := cmd(); got != nil {
								t.Errorf("%s: idle compare card answered %T with %T", tag, msg, got)
							}
						}
					}
				}
			}
		}
	}
}
