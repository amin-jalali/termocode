package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDockerLensRowsMarkFromLinesAndServices(t *testing.T) {
	rows := dockerLensRows("/w/Dockerfile", []string{"FROM golang AS build", "RUN x", "FROM alpine"})
	if len(rows) != 2 || rows[0]["l"] != 1 || rows[1]["l"] != 3 {
		t.Fatalf("rows = %v", rows)
	}
	lens := rows[0]["lens"].([][]string)[0][0]
	if !strings.Contains(lens, "build to build") {
		t.Fatalf("first stage lens = %q", lens)
	}
	if len(dockerLensRows("/w/main.go", []string{"FROM x"})) != 0 {
		t.Fatal("a Go file got a lens")
	}
}

func TestRunTargetCardIsCancelFirstAndCancelRunsNothing(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	lines := []string{"services:", "  api:", "    build: ."}
	m.askRunTarget("/w/compose.yaml", lines, 3, false)
	if !m.confirmOpen || m.pendingHostOperation != hostOpRunCommand {
		t.Fatalf("no card: open=%v op=%q", m.confirmOpen, m.pendingHostOperation)
	}
	if b := m.confirm.Buttons(); len(b) != 2 || b[0].ID != "cancel" {
		t.Fatalf("buttons = %#v, want Cancel first", b)
	}
	card := cardText(&m)
	if !strings.Contains(card, "compose") || !strings.Contains(card, "up api") {
		t.Fatalf("card = %q", card)
	}
	m.runHostAction("cancel")
	if len(m.terminalTabs) != 0 || len(m.services.devrun.cmd.Argv) != 0 {
		t.Fatal("Cancel started or kept the job")
	}
}

func TestRunTargetClickOffMarkDoesNothing(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	if cmd := m.askRunTarget("/w/Dockerfile", []string{"FROM a", "RUN b"}, 2, true); cmd != nil || m.confirmOpen {
		t.Fatal("a click on a line without a mark opened a card")
	}
}

func TestScaffoldCardListsCreatedAndKeptFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_ = os.WriteFile(filepath.Join(root, ".dockerignore"), []byte("x\n"), 0o644)
	m := svcTestModel(svcHostState(), 120, 40)
	m.services.root = root
	t.Chdir(root)
	m.askScaffoldTemplate("generic")
	if !m.confirmOpen || m.pendingHostOperation != hostOpScaffold {
		t.Fatalf("no scaffold card: %v %q", m.confirmOpen, m.pendingHostOperation)
	}
	if b := m.confirm.Buttons(); b[0].ID != "cancel" {
		t.Fatalf("buttons = %#v, want Cancel first", b)
	}
	card := ansi.Strip(m.confirm.View())
	if !strings.Contains(card, "+ Dockerfile") || !strings.Contains(card, "= .dockerignore") {
		t.Fatalf("card = %q", card)
	}
	m.runHostAction("create")
	if !fileExists(filepath.Join(root, "Dockerfile")) {
		t.Fatal("Create did not write the Dockerfile")
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".dockerignore")); string(b) != "x\n" {
		t.Fatal("an existing file was overwritten")
	}
}

func TestDevrunPaletteEntriesHavePolicies(t *testing.T) {
	ids := map[string]bool{}
	for _, c := range servicesPaletteItems() {
		ids[c.ID] = true
	}
	for _, id := range []string{"containers-run-at-cursor", "containers-scaffold", "containers-compose-snippet", "containers-templates-folder"} {
		if !ids[id] {
			t.Errorf("palette misses %s", id)
		}
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("%s has no AI policy", id)
		}
	}
}
