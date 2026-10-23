package importview

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/migrate"
)

// fixturePlan builds the plan of one editor fixture in internal/migrate.
func fixturePlan(t *testing.T, src migrate.Source, cur migrate.Current) migrate.Plan {
	t.Helper()
	src.Dir = filepath.Join("..", "migrate", "testdata", src.Dir)
	doc, err := migrate.Read(src)
	if err != nil {
		t.Fatal(err)
	}
	return migrate.Build(src, doc, cur)
}

var (
	vscodeSrc    = migrate.Source{ID: "vscode", Title: "VS Code", Dir: "vscode-review"}
	jetbrainsSrc = migrate.Source{ID: "jetbrains", Title: "GoLand 2025.1", Dir: "jetbrains-linux"}
	sublimeSrc   = migrate.Source{ID: "sublime", Title: "Sublime Text 4", Dir: "sublime-linux"}
)

func current() migrate.Current {
	return migrate.Current{Config: map[string]any{"tab_size": 4.0}, ThemeID: "vscode-dark-plus"}
}

func newPanel(t *testing.T, src migrate.Source, w, h int) Model {
	t.Helper()
	m := New(fixturePlan(t, src, current()), "~/.config/x")
	m.SetSize(w, h)
	return m
}

// plain is the card as text, one string per screen row.
func plain(m Model) []string {
	var out []string
	for _, l := range strings.Split(m.Box(), "\n") {
		out = append(out, ansi.Strip(l))
	}
	return out
}

func findRow(t *testing.T, m Model, left string) (int, row) {
	t.Helper()
	for k, ln := range m.lines {
		if ln.row >= 0 && strings.HasPrefix(m.rows[ln.row].left, left) {
			return k, m.rows[ln.row]
		}
	}
	t.Fatalf("no row %q", left)
	return 0, row{}
}

func TestPanelRowsFromFixturePlans(t *testing.T) {
	for _, tc := range []struct {
		src      migrate.Source
		sections []string
		shown    []string
	}{
		{vscodeSrc, []string{"Settings", "Keys", "Theme", "Extensions"},
			[]string{"Import from VS Code", "In VS Code", "In termocode", "editor.tabSize = 2", "Tab size  4 → 2", "ctrl+alt+o", "One Dark Pro", "golang.go"}},
		{jetbrainsSrc, []string{"Settings", "Keys", "Theme", "Extensions"},
			[]string{"Import from GoLand 2025.1", "USE_SOFT_WRAPS = true", "control alt P  GotoFile", "Darcula"}},
		{sublimeSrc, []string{"Settings", "Keys", "Theme", "Extensions"},
			[]string{"Import from Sublime Text 4", "save_on_focus_lost = true", "ctrl+alt+s  show_overlay:goto:@", "Monokai"}},
	} {
		t.Run(tc.src.ID, func(t *testing.T) {
			plan := fixturePlan(t, tc.src, current())
			m := New(plan, "")
			m.SetSize(200, 200)
			var names []string
			for _, s := range m.sections {
				names = append(names, s.name)
			}
			if strings.Join(names, ",") != strings.Join(tc.sections, ",") {
				t.Errorf("sections %v, want %v", names, tc.sections)
			}
			if len(m.rows) != len(plan.Rows) {
				t.Errorf("%d panel rows for %d plan rows", len(m.rows), len(plan.Rows))
			}
			text := strings.Join(plain(m), "\n")
			for _, s := range tc.shown {
				if !strings.Contains(text, s) {
					t.Errorf("card does not show %q:\n%s", s, text)
				}
			}
			// Every row that is not imported says so, with its reason.
			for _, r := range m.rows {
				pr := plan.Rows[r.plan]
				if (pr.Equivalent == "not imported" || strings.HasPrefix(pr.Equivalent, "no equivalent")) && pr.Kind != migrate.RowExtension {
					if r.state != stateSkip || !strings.Contains(r.right, "not imported") || !strings.Contains(r.right, pr.Reason) {
						t.Errorf("%q: %+v", pr.SourceText, r)
					}
				}
			}
		})
	}
}

func TestPanelToggleRowAndSection(t *testing.T) {
	m := newPanel(t, vscodeSrc, 150, 42)
	k, r := findRow(t, m, "editor.wordWrap")
	if !r.on {
		t.Fatalf("word wrap should start on: %+v", r)
	}
	m.cursor = k
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if m.rows[m.lines[k].row].on {
		t.Fatal("space did not turn the row off")
	}
	for _, pr := range m.Plan().Rows {
		if strings.HasPrefix(pr.SourceText, "editor.wordWrap") && pr.Apply {
			t.Fatal("Plan applies a row that is off")
		}
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if !m.rows[m.lines[k].row].on {
		t.Fatal("space did not turn the row on again")
	}
	// Space on a section header turns the whole section off (all were
	// on), then on again.
	m.cursor = 0 // the Settings header
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if sel, n, _ := m.tally(m.sections[0].rows); sel != 0 || n != 3 {
		t.Fatalf("section off: %d of %d on", sel, n)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if sel, n, _ := m.tally(m.sections[0].rows); sel != n {
		t.Fatalf("section on: %d of %d", sel, n)
	}
}

func TestPanelNotImportedRowsCannotBeToggled(t *testing.T) {
	m := newPanel(t, vscodeSrc, 150, 42)
	for _, left := range []string{"editor.fontSize", "ctrl+k ctrl+s", "ctrl+p  (VS Code default)", "acme.sparkles", "golang.go", "humao.rest-client"} {
		k, r := findRow(t, m, left)
		if r.state == stateToggle {
			t.Errorf("%s is toggleable: %+v", left, r)
		}
		m.cursor = k
		m.Toggle()
		if m.rows[m.lines[k].row].on {
			t.Errorf("%s turned on", left)
		}
		// A click on it changes nothing either.
		m.click("line:" + strconv.Itoa(k))
		if m.rows[m.lines[k].row].on {
			t.Errorf("%s turned on by a click", left)
		}
	}
	// Select all never applies notes, tool suggestions or skipped rows.
	m.SelectAll()
	for i, pr := range m.Plan().Rows {
		if pr.Apply && (pr.Kind == migrate.RowExtension || pr.Equivalent == "not imported" || strings.HasPrefix(pr.Equivalent, "no equivalent")) {
			t.Errorf("row %d %q applied", i, pr.SourceText)
		}
	}
	if c := m.Counts(); strings.Join(c.Tools, ",") != "dlv,gopls" {
		t.Errorf("tools %v", c.Tools)
	}
}

func TestPanelShowsConflictsWithCurrentValues(t *testing.T) {
	m := newPanel(t, vscodeSrc, 150, 42)
	if _, r := findRow(t, m, "editor.tabSize"); !r.conflict || r.right != "Tab size  4 → 2" || !r.on {
		t.Errorf("tab size: %+v", r)
	}
	if _, r := findRow(t, m, "One Dark Pro"); !r.conflict || r.right != "VSCode Dark+ → One Dark" {
		t.Errorf("theme: %+v", r)
	}
	// A familiar key that is taken by another command: shown, off, and
	// the user may turn it on to replace the binding.
	k, r := findRow(t, m, "ctrl+shift+e  (VS Code default)")
	if r.state != stateToggle || r.on || !r.conflict || !strings.Contains(r.right, "conflicts with") {
		t.Errorf("conflict key: %+v", r)
	}
	m.cursor = k
	if !strings.Contains(m.status(), "replaces") {
		t.Errorf("status %q", m.status())
	}
	m.Toggle()
	found := false
	for _, pr := range m.Plan().Rows {
		if pr.Key == "ctrl+shift+e" && pr.Apply {
			found = true
		}
	}
	if !found {
		t.Error("turned-on conflict row is not applied")
	}
	// Settings already at the imported value are not offered.
	m2 := New(fixturePlan(t, jetbrainsSrc, current()), "")
	m2.SetSize(150, 42)
	if _, r := findRow(t, m2, "TAB_SIZE = 4"); r.state != stateSkip || r.right != "already set to this value" {
		t.Errorf("already set: %+v", r)
	}
}

func TestPanelOneSelectedRowPerKey(t *testing.T) {
	plan := migrate.Plan{Source: migrate.Source{ID: "vscode", Title: "VS Code"}, Rows: []migrate.ReviewRow{
		{Kind: migrate.RowKey, SourceText: "a", Key: "ctrl+alt+k", Action: "QuickOpen", Equivalent: "ctrl+alt+k → QuickOpen", Conflict: true},
		{Kind: migrate.RowKey, SourceText: "b", Key: "ctrl+alt+k", Action: "Save", Equivalent: "ctrl+alt+k → Save", Conflict: true},
		{Kind: migrate.RowSetting, SourceText: "c", Target: "tab_size", Value: 2.0, Apply: true},
		{Kind: migrate.RowSetting, SourceText: "d", Target: "tab_size", Value: 8.0, Apply: true},
	}}
	m := New(plan, "")
	m.SetSize(120, 40)
	on := func(left string) bool { _, r := findRow(t, m, left); return r.on }
	if on("a") || on("b") || !on("c") || on("d") {
		t.Fatalf("start: %+v", m.rows)
	}
	ka, _ := findRow(t, m, "a")
	kb, _ := findRow(t, m, "b")
	m.toggleRow(m.lines[ka].row)
	m.toggleRow(m.lines[kb].row)
	if on("a") || !on("b") {
		t.Fatalf("second key row should replace the first: %+v", m.rows)
	}
	m.SelectAll()
	n := 0
	for _, r := range m.rows {
		if r.on {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("select all: %d rows on, want one per key and one per setting", n)
	}
	m.SelectNone()
	if m.Counts().Total() != 0 {
		t.Fatal("select none left rows on")
	}
}

func TestPanelKeysAndCounts(t *testing.T) {
	m := newPanel(t, vscodeSrc, 150, 42)
	c := m.Counts()
	if c.Settings != 3 || c.Keys != 4 || c.Theme != "one-dark" || c.Total() != 8 {
		t.Fatalf("counts %+v", c)
	}
	if c.Summary() != "3 settings · 4 keys · theme" {
		t.Errorf("summary %q", c.Summary())
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if m.Counts().Total() != 0 {
		t.Error("n did not clear the selection")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if m.Counts().Total() <= c.Total() {
		t.Errorf("a selected %d", m.Counts().Total())
	}
	for _, tc := range []struct {
		key  tea.KeyMsg
		want tea.Msg
	}{{tea.KeyMsg{Type: tea.KeyEnter}, ApplyMsg{}}, {tea.KeyMsg{Type: tea.KeyEsc}, CloseMsg{}}} {
		_, cmd := m.Update(tc.key)
		if cmd == nil || cmd() != tc.want {
			t.Errorf("%s: want %T", tc.key, tc.want)
		}
	}
	// The cursor moves and the view follows it.
	m.SetSize(150, 16)
	for i := 0; i < 40; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursor != len(m.lines)-1 || m.scroll == 0 {
		t.Errorf("cursor %d scroll %d of %d lines", m.cursor, m.scroll, len(m.lines))
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if m.cursor != 0 || m.scroll != 0 {
		t.Errorf("home: cursor %d scroll %d", m.cursor, m.scroll)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	if m.lines[m.cursor].row != -1 || m.cursor == 0 {
		t.Errorf("] did not go to the next section: %d", m.cursor)
	}
}

func TestPanelMouse(t *testing.T) {
	m := newPanel(t, vscodeSrc, 150, 42)
	k, _ := findRow(t, m, "editor.wordWrap")
	ox, oy := m.Origin()
	// Body line k is box row 4+k (border, title, labels, hairline).
	m, _ = m.HandleMouse(ox+20, oy+4+k, tea.MouseActionPress, tea.MouseButtonLeft)
	if m.cursor != k || m.rows[m.lines[k].row].on {
		t.Fatalf("click did not toggle the row: cursor %d", m.cursor)
	}
	var apply, cancel [2]int
	rows := plain(m)
	for y, l := range rows {
		if x := strings.Index(l, " Apply… "); x >= 0 {
			apply = [2]int{len([]rune(l[:x])) + 1, y}
		}
		if x := strings.Index(l, " Cancel "); x >= 0 {
			cancel = [2]int{len([]rune(l[:x])) + 1, y}
		}
	}
	if id := m.ButtonAt(ox+apply[0], oy+apply[1]); id != "apply" {
		t.Fatalf("Apply button: %q at %v", id, apply)
	}
	if _, cmd := m.HandleMouse(ox+apply[0], oy+apply[1], tea.MouseActionPress, tea.MouseButtonLeft); cmd == nil || cmd() != (ApplyMsg{}) {
		t.Error("Apply click")
	}
	if _, cmd := m.HandleMouse(ox+cancel[0], oy+cancel[1], tea.MouseActionPress, tea.MouseButtonLeft); cmd == nil || cmd() != (CloseMsg{}) {
		t.Error("Cancel click")
	}
	// A click outside the card does nothing (no accidental close).
	if _, cmd := m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Error("click outside returned a command")
	}
	m.SetSize(150, 16)
	m, _ = m.HandleMouse(ox+5, oy+6, tea.MouseActionPress, tea.MouseButtonWheelDown)
	if m.scroll == 0 {
		t.Error("wheel did not scroll")
	}
}

func TestPanelNarrowSizesNeverPanic(t *testing.T) {
	plan := fixturePlan(t, vscodeSrc, current())
	for _, sz := range [][2]int{{0, 0}, {1, 1}, {3, 2}, {10, 5}, {20, 8}, {25, 9}, {30, 10}, {39, 11}, {44, 12}, {60, 14}, {80, 24}, {100, 30}, {150, 42}, {300, 90}} {
		m := New(plan, "~/.config/Code/User")
		m.SetSize(sz[0], sz[1])
		m.SelectAll()
		for i := 0; i < 30; i++ {
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		}
		m, _ = m.HandleMouse(sz[0]/2, sz[1]/2, tea.MouseActionPress, tea.MouseButtonLeft)
		m, _ = m.HandleMouse(sz[0]/2, sz[1]/2, tea.MouseActionPress, tea.MouseButtonWheelDown)
		box := m.Box()
		if sz[0] == 0 {
			if box != "" {
				t.Errorf("0x0 drew %q", box)
			}
			continue
		}
		lines := strings.Split(box, "\n")
		w := lipgloss.Width(lines[0])
		if len(lines) > sz[1] || w > sz[0] {
			t.Errorf("%v: card %dx%d is bigger than the screen", sz, w, len(lines))
		}
		for i, l := range lines {
			if lipgloss.Width(l) != w {
				t.Errorf("%v: row %d is %d wide, want %d", sz, i, lipgloss.Width(l), w)
			}
		}
	}
}

func TestPanelCleansHostileText(t *testing.T) {
	plan := migrate.Plan{Source: migrate.Source{ID: "vscode", Title: "Evil\x1b]0;x\x07"}, Rows: []migrate.ReviewRow{
		{Kind: migrate.RowExtension, SourceText: "bad\x1b[2J.ext\nnext", Equivalent: "no equivalent", Reason: "nope\x1b[31m"},
	}}
	m := New(plan, "")
	m.SetSize(100, 30)
	for _, l := range plain(m) {
		if strings.ContainsAny(l, "\x1b\x07\n") {
			t.Fatalf("control characters reached the card: %q", l)
		}
	}
	if !strings.Contains(strings.Join(plain(m), ""), "bad[2J.ext next") {
		t.Errorf("cleaned text missing:\n%s", strings.Join(plain(m), "\n"))
	}
}

func TestPanelTitleShortensALongFolder(t *testing.T) {
	m := New(fixturePlan(t, vscodeSrc, current()), "/tmp/a-very-long-sandbox-folder-name/with/many/parts/Code/User")
	m.SetSize(80, 24)
	title := plain(m)[1]
	if !strings.Contains(title, "…") || !strings.Contains(title, "Code/User") || !strings.Contains(title, "selected") {
		t.Errorf("title row %q: want the folder's end, an ellipsis and the count", title)
	}
}

// IM-1: the card shows titles, not the ids the files use.
func TestPanelShowsTitlesNotIds(t *testing.T) {
	plan := fixturePlan(t, vscodeSrc, current())
	// A key the user has bound to another command: both names are shown as titles.
	plan.Current.Overrides = nil
	m := New(plan, "", WithActionTitle(func(name string) string {
		return map[string]string{"QuickOpen": "Go to File", "RevealFile": "Reveal File in Explorer"}[name]
	}))
	m.SetSize(200, 60)
	text := strings.Join(plain(m), "\n")
	for _, want := range []string{"Tab size  4 → 2", "Auto save = on", "Go to File", "VSCode Dark+ → One Dark", "Reveal File in Explorer", "Command palette", "Focus explorer"} {
		if !strings.Contains(text, want) {
			t.Errorf("card does not show %q:\n%s", want, text)
		}
	}
	for _, id := range []string{"tab_size", "auto_save", "QuickOpen", "CommandPalette", "FocusExplorer", "RevealFile", "DebugStartContinue", "one-dark", "vscode-dark-plus"} {
		if strings.Contains(text, id) {
			t.Errorf("card still shows the id %q:\n%s", id, text)
		}
	}
	// What is written stays the ids.
	for _, pr := range m.Plan().Rows {
		if pr.Kind == migrate.RowTheme && pr.Apply && pr.Target != "one-dark" {
			t.Errorf("theme target %q", pr.Target)
		}
	}
}

// IM-2: at 80 columns the columns cut long text; the lines under the body
// shows the focused row in full, so a reason is never lost.
func TestPanelDetailLineShowsTheFullReason(t *testing.T) {
	m := New(fixturePlan(t, vscodeSrc, current()), "", WithActionTitle(func(name string) string {
		return map[string]string{"RevealFile": "Reveal File in Explorer"}[name]
	}))
	m.SetSize(80, 24)
	for _, tc := range []struct {
		left string
		want []string
	}{
		{"ctrl+alt+d", []string{"termocode has no command for this action", "editor.action.deleteLines"}},
		{"cmd+shift+k", []string{"workbench.action.showCommands"}},
		{"ctrl+shift+e  (VS Code default)", []string{"turning this on replaces Reveal File in Explorer on this key", "ctrl+shift+e  (VS Code default)"}},
		{"editor.fontSize", []string{"termocode has no matching setting", "editor.fontSize = 14"}},
	} {
		k, _ := findRow(t, m, tc.left)
		m.cursor = k
		m.follow()
		rows := plain(m)
		// The two detail lines sit above the footer's hairline, summary and hints.
		detail := rows[len(rows)-6] + "\n" + rows[len(rows)-5]
		for _, w := range tc.want {
			if !strings.Contains(detail, w) {
				t.Errorf("%q: detail line %q lacks %q", tc.left, detail, w)
			}
		}
	}
	// On a section header the line says what Space does there.
	m.cursor = 0
	if rows := plain(m); !strings.Contains(rows[len(rows)-6], "every row in this section") {
		t.Errorf("header detail %q", rows[len(rows)-6])
	}
	// A card too short for the line keeps the reason in the footer.
	m.SetSize(80, 15)
	k, _ := findRow(t, m, "editor.fontSize")
	m.cursor = k
	m.follow()
	if got := strings.Join(plain(m), "\n"); !strings.Contains(got, "termocode has no matching setting") {
		t.Errorf("short card lost the reason:\n%s", got)
	}
}
