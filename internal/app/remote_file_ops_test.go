package app

import (
	"os"
	"strings"
	"testing"
)

func TestContainerNewFileIsMadeInTheContainer(t *testing.T) {
	m, _, _, c := newContainerModel(t)
	m.promptKind, m.promptPath = promptKindNewFile, "/app/pkg"
	m.handlePromptSubmit("new.go")
	if fi, err := os.Stat(c.Local("/app/pkg/new.go")); err != nil || fi.Size() != 0 {
		t.Fatalf("new file in the container: %v %v", fi, err)
	}
	if m.err != "" {
		t.Fatalf("err = %q", m.err)
	}
	m.explorer.RevealPath("/app/pkg/new.go")
	if got := m.explorer.CursorPath(); got != "/app/pkg/new.go" {
		t.Fatalf("explorer does not show the new file: cursor %q", got)
	}
}

func TestContainerNewFileRefusesAnExistingFile(t *testing.T) {
	m, _, _, c := newContainerModel(t)
	m.promptKind, m.promptPath = promptKindNewFile, "/app"
	m.handlePromptSubmit("main.go")
	if !strings.Contains(m.err, "already exists") {
		t.Fatalf("err = %q", m.err)
	}
	if b, _ := os.ReadFile(c.Local("/app/main.go")); len(b) == 0 {
		t.Fatal("main.go was emptied")
	}
}

func TestContainerNewFileOutsideTheRootFails(t *testing.T) {
	m, _, _, _ := newContainerModel(t)
	m.promptKind, m.promptPath = promptKindNewFile, "/app"
	m.handlePromptSubmit("../etc/x")
	if m.err != "The path is outside the workspace." {
		t.Fatalf("err = %q", m.err)
	}
}

func TestContainerNewFolderIsMadeInTheContainer(t *testing.T) {
	m, _, _, c := newContainerModel(t)
	m.promptKind, m.promptPath = promptKindNewFolder, "/app"
	m.handlePromptSubmit("web/static")
	if fi, err := os.Stat(c.Local("/app/web/static")); err != nil || !fi.IsDir() {
		t.Fatalf("new folder in the container: %v %v", fi, err)
	}
	if m.err != "" {
		t.Fatalf("err = %q", m.err)
	}
	m.explorer.RevealPath("/app/web/static")
	if got := m.explorer.CursorPath(); got != "/app/web/static" {
		t.Fatalf("explorer does not show the new folder: cursor %q", got)
	}
}

func TestContainerNewFolderGoneContainerSaysSo(t *testing.T) {
	m, _, _, c := newContainerModel(t)
	c.Remove()
	m.promptKind, m.promptPath = promptKindNewFolder, "/app"
	m.handlePromptSubmit("web")
	if m.err != "api-1 is not running." {
		t.Fatalf("err = %q", m.err)
	}
}
