package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDebugToolboxKeyAsksImageThenCancelFirstCard(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	for i, r := range m.services.rows() {
		if r.kind == svcRowHostItem && r.host == hostContainer && m.services.containerRows[r.idx].State == "running" {
			m.services.cursor = i
			break
		}
	}
	c, _ := m.selectedContainer()
	m2, _ := m.handleServicesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	m = m2
	if !m.promptOpen || m.promptKind != promptKindContainerDebugImage {
		t.Fatal("T did not ask for the toolbox image")
	}
	m.handlePromptSubmit("")
	if !m.confirmOpen || m.pendingHostOperation != hostOpRunCommand {
		t.Fatal("no card after the image")
	}
	if b := m.confirm.Buttons(); b[0].ID != "cancel" {
		t.Fatalf("buttons = %#v, want Cancel first", b)
	}
	card := cardText(&m)
	for _, want := range []string{"--pid container:" + c.ID, "--network container:" + c.ID, "busybox:stable", "--rm"} {
		if !strings.Contains(card, want) {
			t.Errorf("card %q misses %q", card, want)
		}
	}
	if strings.Contains(card, "exec") {
		t.Error("the toolbox runs inside the target")
	}
	m.runHostAction("cancel")
	if len(m.terminalTabs) != 0 {
		t.Fatal("Cancel started the toolbox")
	}
}

func TestDebugToolboxRefusesStoppedContainerAndFlagImage(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	for _, c := range m.services.containerRows {
		if c.State == "exited" {
			if cmd := m.askDebugToolbox(c); cmd == nil || m.promptOpen {
				t.Fatal("a stopped container got the toolbox prompt")
			}
		}
	}
	c := m.services.containerRows[0]
	c.State = "running"
	m.askDebugToolbox(c)
	if cmd := m.onDebugImagePrompt("--privileged"); cmd == nil || m.confirmOpen {
		t.Fatal("a flag as image was accepted")
	}
}

func TestDebugToolboxInMenuAndPalette(t *testing.T) {
	if debugMenuItem().ID != "debug-toolbox" {
		t.Fatal("menu item id")
	}
	found := false
	for _, it := range servicesPaletteItems() {
		found = found || it.ID == "containers-debug-toolbox"
	}
	if _, ok := aiActionPolicies["containers-debug-toolbox"]; !found || !ok {
		t.Fatalf("palette %v policy %v", found, ok)
	}
}
