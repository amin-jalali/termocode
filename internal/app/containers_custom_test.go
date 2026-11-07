package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/charmbracelet/x/ansi"
)

func writeCustomCommands(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "termocode", customCommandsFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// svcCursorOnContainer puts the cursor on the first container row.
func svcCursorOnContainer(t *testing.T, m *Model) containers.Container {
	t.Helper()
	for i, r := range m.services.rows() {
		if r.kind == svcRowHostItem && r.host == hostContainer {
			m.services.cursor = i
			return m.services.containerRows[r.idx]
		}
	}
	t.Fatal("no container row")
	return containers.Container{}
}

func TestCustomCommandsTemplateParses(t *testing.T) {
	cmds, err := parseCustomCommands([]byte(customCommandsTemplate))
	if err != nil || len(cmds) != 2 {
		t.Fatalf("template = %v, %v", cmds, err)
	}
}

func TestCustomCommandsRefuseViewKeys(t *testing.T) {
	_, err := parseCustomCommands([]byte(`[{"name":"x","view":"containers","key":"s","argv":["docker","ps"]}]`))
	if err == nil || !strings.Contains(err.Error(), "taken") {
		t.Fatalf("key s of the view was accepted: %v", err)
	}
	if _, err := parseCustomCommands([]byte(`[{"name":"x","view":"pods","key":"s","argv":["kubectl"]}]`)); err == nil {
		t.Fatal("kube key s was accepted")
	}
}

func TestCustomCommandBangPickerThenCancelFirstCard(t *testing.T) {
	writeCustomCommands(t, `{"commands":[{"name":"Top","view":"containers","argv":["docker","top","{{id}}"]}]}`)
	m := svcTestModel(svcHostState(), 120, 40)
	c := svcCursorOnContainer(t, &m)
	m2, _ := m.handleServicesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	m = m2
	if !m.pickerOpen || m.pickerKind != pickerKindTasks {
		t.Fatal("! did not open the custom command picker")
	}
	m.handleTasksPickerSelect("ctr:custom:0")
	if !m.confirmOpen || m.pendingHostOperation != hostOpRunCommand {
		t.Fatal("no card after the pick")
	}
	if b := m.confirm.Buttons(); b[0].ID != "cancel" {
		t.Fatalf("buttons = %#v, want Cancel first", b)
	}
	if card := ansi.Strip(m.confirm.View()); !strings.Contains(card, "docker top "+c.ID) {
		t.Fatalf("card = %q", card)
	}
	m.runHostAction("cancel")
	if len(m.terminalTabs) != 0 {
		t.Fatal("Cancel started the command")
	}
}

func TestCustomCommandHotkeyOpensCard(t *testing.T) {
	writeCustomCommands(t, `[{"name":"Top","view":"containers","key":"f","argv":["docker","top","{{name}}"]}]`)
	m := svcTestModel(svcHostState(), 120, 40)
	c := svcCursorOnContainer(t, &m)
	m2, _ := m.handleServicesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if !m2.confirmOpen || !strings.Contains(ansi.Strip(m2.confirm.View()), "docker top "+containerShownName(c)) {
		t.Fatal("the hotkey did not open the card of its command")
	}
}

func TestCustomCommandsPaletteEntriesHavePolicies(t *testing.T) {
	ids := map[string]bool{}
	for _, c := range servicesPaletteItems() {
		ids[c.ID] = true
	}
	for _, id := range []string{"containers-custom-command", "containers-custom-commands-edit"} {
		if !ids[id] {
			t.Errorf("palette misses %s", id)
		}
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("%s has no AI policy", id)
		}
	}
}
