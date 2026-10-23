package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/toast"
)

// keyCardText is the open card as plain text.
func keyCardText(m Model) string { return ansi.Strip(m.confirm.View()) }

// wantKeyCard checks that "?" opened a key card (not a toast) that shows
// every text in want, then closes it with Esc and checks it stays closed.
func wantKeyCard(t *testing.T, name string, m Model, want ...string) Model {
	t.Helper()
	if !m.confirmOpen || m.confirmKind != confirmKindKeyCard {
		t.Fatalf("%s: ? did not open a key card (confirmOpen=%v)", name, m.confirmOpen)
	}
	if !m.toast.Empty() {
		t.Errorf("%s: ? also pushed a toast", name)
	}
	m.confirm.SetSize(100, 50)
	text := keyCardText(m)
	for _, w := range append(want, "Open guide", "Close") {
		if !strings.Contains(text, w) {
			t.Errorf("%s: card lacks %q:\n%s", name, w, text)
		}
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if msg, ok := findMsg[confirm.CloseMsg](cmd); ok {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	if m.confirmOpen {
		t.Errorf("%s: Esc did not close the card", name)
	}
	return m
}

// findMsg runs cmd (and batches) until a message of type T comes out; a
// command that takes longer than a second (a tick) is skipped.
func findMsg[T any](cmd tea.Cmd) (T, bool) {
	var zero T
	if cmd == nil {
		return zero, false
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case out := <-ch:
		switch msg := out.(type) {
		case T:
			return msg, true
		case tea.BatchMsg:
			for _, c := range msg {
				if v, ok := findMsg[T](c); ok {
					return v, true
				}
			}
		}
	case <-time.After(time.Second):
	}
	return zero, false
}

// guideOf runs the card's Open guide button and returns the page it opens.
func guideOf(t *testing.T, name string, m Model) help.Loc {
	t.Helper()
	next, cmd := m.Update(confirm.SelectMsg{ID: "guide"})
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("%s: Open guide did nothing", name)
	}
	msg := findPreviewMsg(cmd)
	if msg == nil || msg.help == nil {
		t.Fatalf("%s: Open guide did not load a guide page", name)
	}
	if errs := help.CheckAll([]string{msg.help.loc.Page + "#" + msg.help.loc.Anchor}); len(errs) > 0 {
		t.Errorf("%s: the guide place does not exist: %v", name, errs)
	}
	return msg.help.loc
}

// X-1: "?" in the Databases tree is a card with a key table, not a toast.
func TestDatabasesQuestionMarkOpensKeyCard(t *testing.T) {
	withConfigDir(t)
	s := newDBState()
	s.loaded = true
	m := dbTestModel(s)
	m.w, m.h, m.toast = 100, 40, toast.New()
	next, _ := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m2 := wantKeyCard(t, "tree", next, "Database keys", "connect, expand", "open the data", "filter by name")
	_ = m2
	if loc := guideOf(t, "tree", next); loc.Page != databasesGuidePage || loc.Anchor != "open-the-view" {
		t.Errorf("tree guide = %+v", loc)
	}
}

// X-1: "?" on a result page too, with the page's own keys.
func TestDatabasePageQuestionMarkOpensKeyCard(t *testing.T) {
	withConfigDir(t)
	m := dbGridModel(dbWideResult(8))
	m.w, m.h, m.toast = 100, 40, toast.New()
	m.databasePageKey("?", 1, 1)
	wantKeyCard(t, "page", *m, "Result keys", "next / previous page", "sort by a column", "submit the changes")
	if loc := guideOf(t, "page", *m); loc.Page != databasesGuidePage || loc.Anchor != "results" {
		t.Errorf("page guide = %+v", loc)
	}
}

// X-1: Containers "?" is the same card, with the keys of the row kind.
func TestContainersQuestionMarkOpensKeyCard(t *testing.T) {
	st := svcHostState()
	st.loaded = true
	for sec := range st.collapsed {
		st.collapsed[sec] = false
	}
	for i, r := range st.rows() {
		if r.kind == svcRowHostItem && r.host == hostContainer {
			st.cursor = i
			break
		}
	}
	m := svcTestModel(st, 100, 40)
	m.toast = toast.New()
	m, _ = contKey(m, "?")
	wantKeyCard(t, "containers", m, "Container keys", "start / stop / restart", "filter the rows")
	if loc := guideOf(t, "containers", m); loc.Page != containersGuidePage || loc.Anchor != containersKeysAnchor {
		t.Errorf("containers guide = %+v", loc)
	}
}

// X-1: the import panel has "?" and opens the card; Esc keeps the panel.
func TestImportPanelQuestionMarkOpensKeyCard(t *testing.T) {
	importSandbox(t, "vscode")
	m := openImportPanel(t, importModel(150, 42), "VS Code")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m, _ = importFeed(t, next.(Model), cmd)
	m = wantKeyCard(t, "import", m, "Import keys", "turn a row on or off", "go on to the final check")
	if !m.importOpen {
		t.Error("Esc on the card closed the import panel")
	}
	if loc := guideOf(t, "import", m); loc.Page != importGuidePage || loc.Anchor != "step-by-step" {
		t.Errorf("import guide = %+v", loc)
	}
}

// X-1: the MCP status screen has "?" too; Esc brings the screen back.
func TestMCPServeStatusQuestionMarkOpensKeyCard(t *testing.T) {
	m := mcpServeStatusModel(t)
	m.w, m.h = 100, 40
	m.openMCPServeStatus()
	m.toast = toast.New()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	_ = cmd
	mm := next.(Model)
	if mm.pickerOpen {
		t.Error("the status screen stays open under the card")
	}
	mm = wantKeyCard(t, "mcp", mm, "MCP server status keys", "disconnect an agent")
	if !mm.pickerOpen || mm.pickerKind != pickerKindMCPServeStatus {
		t.Error("Esc on the card did not bring the status screen back")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if loc := guideOf(t, "mcp", next.(Model)); loc.Page != "guides/agents.md" || loc.Anchor != "see-what-is-connected" {
		t.Errorf("mcp guide = %+v", loc)
	}
}

// X-1: the card is the one shared component: every view's rows are a
// two-column table (a key and a text) with no row left half empty.
func TestKeyCardRowsAreTables(t *testing.T) {
	_, hostRows := hostKeysHelp(svcRow{kind: svcRowHostItem, host: hostContainer})
	for name, rows := range map[string][]confirm.KeyRow{
		"tree": dbTreeKeyRows(), "page": dbPageKeyRows(), "redis": dbRedisKeyRows(), "containers": hostRows,
	} {
		if len(rows) < 5 {
			t.Errorf("%s: only %d rows", name, len(rows))
		}
		for _, r := range rows {
			if r.Key == "" && r.What == "" || r.Key != "" && r.What == "" {
				t.Errorf("%s: half empty row %+v", name, r)
			}
		}
	}
}
