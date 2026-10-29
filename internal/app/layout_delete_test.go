package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/picker"
)

func TestLayoutDeleteListsOnlySavedLayouts(t *testing.T) {
	m, _ := layoutWorld(t)
	st := m.layoutStore()
	st.Save(layout.User, layout.Layout{Name: "mine"})
	st.Save(layout.Project, layout.Layout{Name: "team"})
	m.dispatchPaletteAction("layout-delete")
	got := pickerText(m)
	if !m.pickerOpen || m.pickerKind != pickerKindLayoutDelete || !strings.Contains(got, "mine") || !strings.Contains(got, "team") {
		t.Fatalf("picker open=%v:\n%s", m.pickerOpen, got)
	}
	for _, builtin := range []string{"focus", "review", "default"} {
		if strings.Contains(got, builtin) {
			t.Errorf("built-in %q can be deleted:\n%s", builtin, got)
		}
	}
}

func TestLayoutDeleteWithNothingSavedSaysSo(t *testing.T) {
	m, _ := layoutWorld(t)
	m.dispatchPaletteAction("layout-delete")
	if m.pickerOpen {
		t.Fatal("empty picker")
	}
	if got := toastText(m); !strings.Contains(got, "No saved layouts to delete") {
		t.Errorf("toast = %q", got)
	}
}

func TestLayoutDeleteCardHasCancelFirstAndFocused(t *testing.T) {
	m, _ := layoutWorld(t)
	m.layoutStore().Save(layout.User, layout.Layout{Name: "mine"})
	m.dispatchPaletteAction("layout-delete")
	m, _ = m.update(picker.SelectMsg{ID: "mine"})
	if !m.confirmOpen || m.confirmKind != confirmKindLayoutDelete {
		t.Fatalf("no card: %v %v", m.confirmOpen, m.confirmKind)
	}
	card := ansi.Strip(m.confirm.Box())
	row := ""
	for _, l := range strings.Split(card, "\n") {
		if strings.Contains(l, "Cancel") && strings.Contains(l, "Delete") {
			row = l
		}
	}
	if m.confirm.SafeID() != "cancel" || row == "" || strings.Index(row, "Cancel") > strings.Index(row, "Delete") {
		t.Errorf("Cancel must come first and have the focus (SafeID %q):\n%s", m.confirm.SafeID(), card)
	}
	if !strings.Contains(card, "mine") {
		t.Errorf("card does not name the layout:\n%s", card)
	}
	// Nothing is deleted by showing the card, by Cancel, or by Esc.
	for name, msg := range map[string]tea.Msg{"cancel": confirm.SelectMsg{ID: "cancel"}, "esc": confirm.CloseMsg{}} {
		after, _ := m.update(msg)
		if _, ok := after.layoutStore().Find("mine"); !ok {
			t.Fatalf("%s deleted the layout", name)
		}
	}
	done, _ := m.update(confirm.SelectMsg{ID: "delete"})
	if _, ok := done.layoutStore().Find("mine"); ok {
		t.Fatal("Delete kept the layout")
	}
	if got := toastText(done); !strings.Contains(got, "Layout mine deleted") {
		t.Errorf("toast = %q", got)
	}
}

func TestLayoutDeleteOfAProjectLayoutSaysItIsShared(t *testing.T) {
	m, _ := layoutWorld(t)
	m.layoutStore().Save(layout.Project, layout.Layout{Name: "team"})
	m.dispatchPaletteAction("layout-delete")
	m, _ = m.update(picker.SelectMsg{ID: "team"})
	if card := ansi.Strip(m.confirm.Box()); !strings.Contains(card, ".termocode/layouts/team.jsonc") {
		t.Errorf("card:\n%s", card)
	}
}
