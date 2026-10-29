package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// saveLayoutAs runs the Save As flow: the name, then the answer of the
// "who gets it" card.
func saveLayoutAs(t *testing.T, m Model, name, scope string) Model {
	t.Helper()
	m.dispatchPaletteAction("layout-save")
	if !m.promptOpen || m.promptKind != promptKindLayoutSave {
		t.Fatalf("no name prompt: open=%v kind=%v", m.promptOpen, m.promptKind)
	}
	m, _ = m.update(prompt.SubmitMsg{Value: name})
	if !m.confirmOpen || m.confirmKind != confirmKindLayoutScope {
		t.Fatalf("no scope card for %q: prompt=%v card=%v err=%q", name, m.promptOpen, m.confirmOpen, m.prompt.Error())
	}
	m, _ = m.update(confirm.SelectMsg{ID: scope})
	return m
}

func TestLayoutSaveAsWritesTheLiveLayoutForTheUser(t *testing.T) {
	m, _ := layoutWorld(t)
	m.explorerWidth = 37
	m.terminalRows = 11
	m = saveLayoutAs(t, m, "  my debug ", "user")
	e, ok := m.layoutStore().Find("my debug")
	if !ok || e.Source != layout.User {
		t.Fatalf("not saved as a user layout: %+v", e)
	}
	if e.Sidebar == nil || e.Sidebar.Width != 37 || e.Panel == nil || e.Panel.Rows != 11 {
		t.Errorf("saved layout = %+v / %+v", e.Sidebar, e.Panel)
	}
	if got := toastText(m); !strings.Contains(got, "Layout my debug saved for all your projects") {
		t.Errorf("toast = %q", got)
	}
	// The new layout shows in the Open list.
	m.dispatchPaletteAction("layout-open")
	if !strings.Contains(pickerText(m), "my debug") {
		t.Errorf("not in the list:\n%s", pickerText(m))
	}
}

func TestLayoutSaveAsForTheProjectWritesTheSharedFile(t *testing.T) {
	m, root := layoutWorld(t)
	m = saveLayoutAs(t, m, "team", "project")
	e, ok := m.layoutStore().Find("team")
	if !ok || e.Source != layout.Project || e.Path != filepath.Join(root, ".termocode", "layouts", "team.jsonc") {
		t.Fatalf("saved = %+v", e)
	}
	if got := toastText(m); !strings.Contains(got, ".termocode/layouts/team.jsonc") {
		t.Errorf("toast = %q", got)
	}
}

func TestLayoutSaveAsRefusesBadAndBuiltInNamesInThePrompt(t *testing.T) {
	m, _ := layoutWorld(t)
	for name, why := range map[string]string{"debug": "built-in", "a/b": "letters, digits", "": "letters, digits"} {
		m.dispatchPaletteAction("layout-save")
		m, _ = m.update(prompt.SubmitMsg{Value: name})
		if !m.promptOpen || m.confirmOpen {
			t.Fatalf("%q: prompt=%v card=%v", name, m.promptOpen, m.confirmOpen)
		}
		if !strings.Contains(m.prompt.Error(), why) {
			t.Errorf("%q: error %q lacks %q", name, m.prompt.Error(), why)
		}
		m.promptOpen = false
	}
	if es, _ := m.layoutStore().List(); len(es) != 4 {
		t.Errorf("a file was written: %d layouts", len(es))
	}
}

func TestLayoutSaveAsCancelWritesNothingAndReplaceAsksFirst(t *testing.T) {
	m, _ := layoutWorld(t)
	m = saveLayoutAs(t, m, "keep", "cancel")
	if m.layoutStore().Has(layout.User, "keep") {
		t.Fatal("cancel saved")
	}
	m.explorerWidth = 31
	m = saveLayoutAs(t, m, "keep", "user")
	m.explorerWidth = 45
	m = saveLayoutAs(t, m, "keep", "user") // same name, same place
	if !m.confirmOpen || m.confirmKind != confirmKindLayoutReplace {
		t.Fatalf("no replace card: %v %v", m.confirmOpen, m.confirmKind)
	}
	card := ansi.Strip(m.confirm.Box())
	row := ""
	for _, l := range strings.Split(card, "\n") {
		if strings.Contains(l, "Cancel") && strings.Contains(l, "Replace") {
			row = l
		}
	}
	if m.confirm.SafeID() != "cancel" || row == "" || strings.Index(row, "Cancel") > strings.Index(row, "Replace") {
		t.Errorf("Cancel must be first and focused (SafeID %q):\n%s", m.confirm.SafeID(), card)
	}
	if e, _ := m.layoutStore().Find("keep"); e.Sidebar.Width != 31 {
		t.Fatalf("file replaced before the answer: width %d", e.Sidebar.Width)
	}
	esc, _ := m.update(confirm.CloseMsg{})
	if e, _ := esc.layoutStore().Find("keep"); e.Sidebar.Width != 31 {
		t.Fatalf("Esc replaced the file: width %d", e.Sidebar.Width)
	}
	m, _ = m.update(confirm.SelectMsg{ID: "replace"})
	if e, _ := m.layoutStore().Find("keep"); e.Sidebar.Width != 45 {
		t.Fatalf("Replace did not replace: width %d", e.Sidebar.Width)
	}
}

func TestLayoutSaveAsKeepsNoSecretsOrOutput(t *testing.T) {
	m, _ := layoutWorld(t)
	t.Setenv("TERMOCODE_FAKE_TOKEN", "s3cr3t-value")
	m.terminalTabs[0].Name = "api"
	m = saveLayoutAs(t, m, "safe", "user")
	e, _ := m.layoutStore().Find("safe")
	body, _ := (e.Layout).Marshal()
	if strings.Contains(string(body), "s3cr3t-value") || strings.Contains(string(body), "TOKEN") {
		t.Errorf("secret in the file:\n%s", body)
	}
}
