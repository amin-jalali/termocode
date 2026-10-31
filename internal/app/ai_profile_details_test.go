package app

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Spec: the profile picker ends with a "Show details" row that opens
// AI: Show Profile for the active profile.
func TestAIProfilePickerShowDetailsRow(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.openAIProfilePicker()
	m.handleAIPickerSelect(aiProfileDetailsID)
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileShow {
		t.Fatal("the row did not open the profile details")
	}
	if m.ai.activeProfile().Name != ai.ProfileDefault {
		t.Error("the row switched the profile")
	}
}

// pressInAIProfilePicker moves the picker's highlight onto id and presses
// key; the resulting message goes through the app.
func pressInAIProfilePicker(t *testing.T, m *Model, id string, key tea.KeyMsg) {
	t.Helper()
	m.picker.SetCursorID(id)
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(key)
	if cmd == nil {
		t.Fatalf("key %v did nothing on %q", key, id)
	}
	nm, _ := m.Update(cmd())
	*m = nm.(Model)
}

// Spec: Right in the profile picker opens the details of the HIGHLIGHTED
// profile and never switches.
func TestAIProfilePickerDetailsKeyOnOtherRow(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.openAIProfilePicker()
	pressInAIProfilePicker(t, m, "review", tea.KeyMsg{Type: tea.KeyRight})
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileShow {
		t.Fatal("no details card")
	}
	if !strings.Contains(ansi.Strip(m.confirm.View()), "Profile review") {
		t.Errorf("card is not about review:\n%s", ansi.Strip(m.confirm.View()))
	}
	if m.ai.activeProfile().Name != ai.ProfileDefault {
		t.Error("the key switched the profile")
	}
	if m.pickerOpen {
		t.Error("picker still open")
	}
}

// Spec: the footer names the key.
func TestAIProfilePickerFooterNamesDetailsKey(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.openAIProfilePicker()
	if !strings.Contains(ansi.Strip(m.picker.Box()), "details") {
		t.Error("footer lacks the details hint")
	}
}

// Spec: New / Edit rows run their flows and never switch the profile.
func TestAIProfilePickerNewAndEditRows(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.openAIProfilePicker()
	before := m.ai.activeProfile().Name
	pressInAIProfilePicker(t, m, aiProfileNewID, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.profileFormOpen {
		t.Fatal("New row did not open the step form")
	}
	if m.ai.activeProfile().Name != before {
		t.Error("New row switched")
	}
	m.profileFormOpen = false
	m.openAIProfilePicker()
	pressInAIProfilePicker(t, m, aiProfileEditID, tea.KeyMsg{Type: tea.KeyEnter})
	path, _ := ai.ProfilesPath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Edit row did not create profiles.json: %v", err)
	}
	if m.ai.activeProfile().Name != before {
		t.Error("Edit row switched")
	}
}

func TestAIProfilePickerShowsTitleAndKeepsID(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.openAIProfilePicker()
	m.picker.SetCursorID("house")
	if got := ansi.Strip(m.picker.Box()); !strings.Contains(got, "House review") {
		t.Fatalf("profile title missing: %s", got)
	}
	_, cmd := m.picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no selection")
	}
	sel, ok := cmd().(picker.SelectMsg)
	if !ok || sel.ID != "house" {
		t.Fatalf("title changed profile identity: %#v", sel)
	}
}
