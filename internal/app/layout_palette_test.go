package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/picker"
)

// layoutWorld is a Model in a project folder with an empty config dir.
func layoutWorld(t *testing.T) (Model, string) {
	t.Helper()
	t.Setenv("TERMOCODE_HEADLESS", "1")
	cfg := withConfigDir(t)
	_ = cfg
	root := layoutProject(t)
	return panelTestModel(), root
}

// pickerText is the plain text of the open picker.
func pickerText(m Model) string { return ansi.Strip(m.picker.Box()) }

func TestPaletteHasLayoutOpen(t *testing.T) {
	found := false
	for _, e := range PaletteCatalog() {
		if e.ID == "layout-open" && e.Title == "Layout: Open…" {
			found = true
		}
	}
	if !found {
		t.Fatal("palette has no Layout: Open…")
	}
}

func TestLayoutOpenListsBuiltInUserAndProjectLayouts(t *testing.T) {
	m, root := layoutWorld(t)
	st := m.layoutStore()
	if _, err := st.Save(layout.User, layout.Layout{Name: "mine", Description: "my own"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(layout.Project, layout.Layout{Name: "debug", Description: "the team debug"}); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, ".termocode", "layouts"); st.ProjectDir != want {
		t.Fatalf("project dir %q, want %q", st.ProjectDir, want)
	}
	m.dispatchPaletteAction("layout-open")
	if !m.pickerOpen || m.pickerKind != pickerKindLayoutOpen {
		t.Fatalf("picker open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	got := pickerText(m)
	for _, want := range []string{"default", "focus", "review", "mine", "user · my own", "debug", "project · the team debug"} {
		if !strings.Contains(got, want) {
			t.Errorf("picker lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "built-in · Editor, terminal") {
		t.Errorf("built-in debug shown next to the project one:\n%s", got)
	}
}

func TestLayoutOpenPickRunsTheLayoutAndNamesIt(t *testing.T) {
	m, _ := layoutWorld(t)
	m.showExp = true
	m.dispatchPaletteAction("layout-open")
	m, _ = m.update(picker.SelectMsg{ID: "focus"})
	if m.showExp || m.actionsOpen {
		t.Errorf("focus layout left the sidebar %v / chat %v", m.showExp, m.actionsOpen)
	}
	if !strings.Contains(statusLine(m), "Layout: focus") {
		t.Errorf("status bar: %q", statusLine(m))
	}
	m, _ = m.update(picker.SelectMsg{ID: "review"})
	if !m.showExp || m.activity.Active() != activity.ViewGit {
		t.Errorf("review: show=%v view=%v", m.showExp, m.activity.Active())
	}
}

func TestLayoutOpenPreviewCardThenOpenOrBack(t *testing.T) {
	m, _ := layoutWorld(t)
	m.dispatchPaletteAction("layout-open")
	m, _ = m.update(picker.ActionMsg{Key: "→", ID: "review", Title: "review"})
	if m.pickerOpen || !m.confirmOpen || m.confirmKind != confirmKindLayoutOpen {
		t.Fatalf("picker=%v card=%v kind=%v", m.pickerOpen, m.confirmOpen, m.confirmKind)
	}
	card := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"review", "Sidebar: Source Control", "Chat panel: open", "Open", "Close"} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:\n%s", want, card)
		}
	}
	// Nothing changed yet.
	if m.activity.Active() == activity.ViewGit {
		t.Fatal("the preview already opened the layout")
	}
	// Close: back to the list with the same row.
	back, _ := m.update(confirm.SelectMsg{ID: "close"})
	if !back.pickerOpen || back.pickerKind != pickerKindLayoutOpen || back.activity.Active() == activity.ViewGit {
		t.Fatalf("close: picker=%v", back.pickerOpen)
	}
	// Esc is the same as Close.
	esc, _ := m.update(confirm.CloseMsg{})
	if !esc.pickerOpen {
		t.Error("Esc did not go back to the list")
	}
	// Open opens it.
	opened, _ := m.update(confirm.SelectMsg{ID: "open"})
	if opened.activity.Active() != activity.ViewGit || opened.pickerOpen {
		t.Errorf("open: view=%v picker=%v", opened.activity.Active(), opened.pickerOpen)
	}
}

func TestLayoutOpenReportsABrokenFileAndStillListsTheRest(t *testing.T) {
	m, _ := layoutWorld(t)
	st := m.layoutStore()
	if err := os.MkdirAll(st.UserDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.UserDir, "bad.jsonc"), []byte(`{"focus":"moon"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := m.dispatchPaletteAction("layout-open")
	_ = cmd
	if !m.pickerOpen || !strings.Contains(pickerText(m), "focus") {
		t.Fatal("list missing")
	}
	if got := toastText(m); !strings.Contains(got, "bad.jsonc is not a valid layout") {
		t.Errorf("toast = %q", got)
	}
}

func TestAltYOpensTheLayoutList(t *testing.T) {
	m, _ := layoutWorld(t)
	m.keys = keymap.Default()
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y"), Alt: true})
	if !m.pickerOpen || m.pickerKind != pickerKindLayoutOpen {
		t.Fatalf("Alt+Y: picker open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	for _, e := range PaletteCatalog() {
		if e.ID == "layout-open" && (len(e.Keys) == 0 || e.Keys[0] != "alt+y") {
			t.Errorf("palette keys = %v", e.Keys)
		}
	}
}
