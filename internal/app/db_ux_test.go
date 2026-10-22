package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func dbNoSQLiteModel(w, h int) (Model, *dbState) {
	s := newDBState()
	s.loaded = true
	s.conns = []dbclient.Conn{{ID: "app-db", Name: "app.db", Driver: "sqlite", Path: "app.db"}}
	note, code := "app-db/sqlite-note", "app-db/sqlite-install"
	s.nodes["app-db"] = dbNode{kind: dbNodeConnection, name: "app.db", path: "app-db", conn: "app-db", loaded: true, children: []string{note, code}}
	s.nodes[note] = dbNode{kind: dbNodeNote, name: "sqlite3 is not installed or is too old", path: note, conn: "app-db"}
	s.nodes[code] = dbNode{kind: dbNodeCode, name: dbclient.SQLiteInstallHintWithRelease("linux", "amd64", "ID=debian"), path: code, conn: "app-db"}
	return Model{databases: s, w: w, h: h, showExp: true, activity: activity.New(), explorerWidth: 32}, s
}

// DB-1: the install note and command wrap in the tree, and y copies the command.
func TestDatabaseSQLiteInstallHintWrapsAndCopies(t *testing.T) {
	m, s := dbNoSQLiteModel(80, 24)
	got := stripSGRForTest(m.renderDatabasesSidebarRaw(31, 20, true))
	flat := strings.Join(strings.Fields(strings.NewReplacer("\n", " ").Replace(got)), " ")
	for _, want := range []string{"sqlite3 is not installed or is too old", "sudo apt install sqlite3"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("sidebar lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "…") {
		t.Fatalf("a row is cut:\n%s", got)
	}
	for _, cursor := range []int{0, 2} {
		s.cursor = cursor
		got = stripSGRForTest(m.renderDatabasesSidebarRaw(31, 20, true))
		if !strings.Contains(got, "y copy command") {
			t.Fatalf("cursor %d: footer has no copy key:\n%s", cursor, got)
		}
	}
	s.cursor = 0
	var copied []string
	defer SetClipboardSink(func(v string) { copied = append(copied, v) })()
	_, _ = m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if len(copied) != 1 || copied[0] != "sudo apt install sqlite3" {
		t.Fatalf("copied %q, want only the command", copied)
	}
}

// A wrapped row takes several lines; a click on its second line still picks it.
func TestDatabaseWrappedRowMouseHit(t *testing.T) {
	m, s := dbNoSQLiteModel(80, 24)
	m.renderDatabasesSidebarRaw(31, 20, true)
	// lines 0-3 header; row 0 = line 4; note wraps over 2 lines (5,6); code starts at 7.
	_, _ = m.handleDatabasesSidebarMouse(3, 6, tea.MouseLeft)
	if s.cursor != 1 {
		t.Fatalf("click on the wrapped note's second line picked row %d, want 1", s.cursor)
	}
	_, _ = m.handleDatabasesSidebarMouse(3, 7, tea.MouseLeft)
	if s.cursor != 2 {
		t.Fatalf("click on the command picked row %d, want 2", s.cursor)
	}
}

// DB-2: the result page has a ? key, and its key row always ends with "? keys".
func TestDatabasePageKeyRowEndsWithKeysHint(t *testing.T) {
	withConfigDir(t)
	for _, w := range []int{40, 60, 80, 120, 150} {
		m := dbGridModel(dbWideResult(8))
		m.w = w
		p := m.databasePage()
		found := p.lines[2] // row 3 of the page holds the key hints
		if !strings.HasSuffix(strings.TrimRight(found, " "), "? keys") {
			t.Fatalf("w=%d: key row %q does not end with \"? keys\"", w, found)
		}
	}
	m := dbGridModel(dbWideResult(8))
	m.w = 80
	if cmd := m.databasePageKey("?", 1, 1); cmd == nil && !strings.Contains(strings.Join(m.toast.View(120, 30), "\n"), "Database keys") {
		t.Fatal("? did nothing on the result page")
	}
	if !strings.Contains(strings.Join(m.toast.View(120, 30), "\n"), "Database keys") {
		t.Fatal("? on the result page shows no key help")
	}
}

// DB-5: Shift+F1 and the tooltip "More info" know the Databases view and the
// result page; both point at sections that exist in the guide.
func TestDatabasesHelpTopics(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := hoverModel(t)
	m.focus, m.showExp = FocusExplorer, true
	m.activity.SetActive(activity.ViewDatabases)
	if got := m.helpTopicForFocus(); got != helpContexts["databases"] || got == "" {
		t.Fatalf("Databases view topic = %q", got)
	}
	m = hoverModel(t)
	m.focus = FocusEditor
	m.editor.SetMeta("/tmp/x/SQL Result", false, "")
	if got := m.helpTopicForFocus(); got != helpContexts["db-result"] || got == "" {
		t.Fatalf("result page topic = %q", got)
	}
	if got := tooltipHelp("activity:Databases"); got != helpContexts["databases"] {
		t.Fatalf("activity tooltip help = %q", got)
	}
	if errs := help.CheckAll([]string{helpContexts["databases"], helpContexts["db-result"]}); len(errs) > 0 {
		t.Fatal(errs)
	}
}

// DB-7: the import card shows at least 3 preview rows at small and large
// sizes, and its label counts them.
func TestDatabaseImportCardShowsPreviewRows(t *testing.T) {
	withConfigDir(t)
	for _, size := range [][2]int{{80, 24}, {100, 30}, {150, 42}} {
		m := &Model{databases: newDBState(), w: size[0], h: size[1]}
		_ = m.databaseImportDemo()
		box := ansi.Strip(m.confirm.Box())
		if !strings.Contains(box, "First 3 rows") {
			t.Fatalf("%dx%d: no \"First 3 rows\" label:\n%s", size[0], size[1], box)
		}
		for _, row := range []string{"48214", "48215", "48216"} {
			if !strings.Contains(box, row) {
				t.Fatalf("%dx%d: preview row %s is not on the card:\n%s", size[0], size[1], row, box)
			}
		}
	}
}

// DB-9: the page tab is named by what it shows, and the name is not shown a
// second time in the breadcrumbs row.
func TestDatabasePageNameByContent(t *testing.T) {
	cases := []struct {
		run  *dbRun
		want string
	}{
		{&dbRun{}, "SQL Result"},
		{&dbRun{redis: &dbRedisView{key: "session:ab12"}}, "Redis · session:ab12"},
		{&dbRun{describe: &dbTable{schema: "public", name: "users"}}, "Describe · public.users"},
		{&dbRun{tv: &dbTableView{dbTable: dbTable{schema: "public", name: "users"}}}, "Table · public.users"},
	}
	for _, c := range cases {
		if got := dbPageName(c.run); got != c.want {
			t.Errorf("dbPageName = %q, want %q", got, c.want)
		}
		if !isDBResultPath("/tmp/x/" + c.want) {
			t.Errorf("%q is not known as a result page", c.want)
		}
		m := &Model{}
		m.refreshBreadcrumbs("/tmp/x/" + c.want)
		if len(m.breadcrumbs) != 0 {
			t.Errorf("%q: breadcrumbs %v repeat the tab name", c.want, m.breadcrumbs)
		}
	}
	if isDBResultPath("/tmp/x/users") || isDBResultPath("/tmp/x/query.sql") {
		t.Error("a normal file is taken for a result page")
	}
}

// DB-10: the SSL picker shows no match count over an empty search, and
// every choice hint fits in the box without a cut.
func TestDatabaseSSLPickerHasNoCountAndNoCutHints(t *testing.T) {
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "staging", Name: "staging", Driver: "postgres"}}
	m := &Model{databases: s, w: 80, h: 24}
	_ = m.databaseSSLStart("staging")
	box := ansi.Strip(m.picker.Box())
	if strings.Contains(box, "matches") {
		t.Fatalf("match count over an empty search:\n%s", box)
	}
	if strings.Contains(box, "…") {
		t.Fatalf("a hint is cut:\n%s", box)
	}
	for _, want := range []string{"no encryption", "server not checked", "checks the CA", "safest"} {
		if !strings.Contains(box, want) {
			t.Fatalf("hint %q missing:\n%s", want, box)
		}
	}
}
