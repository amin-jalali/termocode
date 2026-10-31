package app

import (
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
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
	pumpExplorerLoads(t, &m)
	m.explorer.RevealPath("/app/pkg/new.go")
	pumpExplorerLoads(t, &m)
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
	pumpExplorerLoads(t, &m)
	m.explorer.RevealPath("/app/web/static")
	pumpExplorerLoads(t, &m)
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

func TestContainerRenameAndMoveInTheContainer(t *testing.T) {
	m, _, _, c := newContainerModel(t)
	m.promptKind, m.promptPath = promptKindRename, "/app/main.go"
	m.handlePromptSubmit("app.go")
	if _, err := os.Stat(c.Local("/app/app.go")); err != nil || m.err != "" {
		t.Fatalf("rename: %v, err %q", err, m.err)
	}
	if _, err := os.Stat(c.Local("/app/main.go")); !os.IsNotExist(err) {
		t.Fatalf("old name still there: %v", err)
	}
	// A path in the new name moves the file to another folder.
	m.promptKind, m.promptPath = promptKindRename, "/app/pkg/util.go"
	m.handlePromptSubmit("../lib/util.go")
	if b, err := os.ReadFile(c.Local("/app/lib/util.go")); err != nil || string(b) != "package pkg\n" || m.err != "" {
		t.Fatalf("move: %q %v, err %q", b, err, m.err)
	}
	pumpExplorerLoads(t, &m)
	m.explorer.RevealPath("/app/lib/util.go")
	pumpExplorerLoads(t, &m)
	if got := m.explorer.CursorPath(); got != "/app/lib/util.go" {
		t.Fatalf("explorer cursor %q", got)
	}
}

func TestContainerRenameRefusesAnExistingName(t *testing.T) {
	m, _, _, c := newContainerModel(t)
	c.WriteFile(t, "/app/b.go", []byte("keep me"), 0o644)
	m.promptKind, m.promptPath = promptKindRename, "/app/main.go"
	m.handlePromptSubmit("b.go")
	if !strings.Contains(m.err, "already exists") {
		t.Fatalf("err = %q", m.err)
	}
	if b, _ := os.ReadFile(c.Local("/app/b.go")); string(b) != "keep me" {
		t.Fatalf("b.go = %q", b)
	}
}

func TestContainerRenameWithoutShellSaysWhy(t *testing.T) {
	ws, _, _, _ := fakeContainerWS(t, func(c *fakedocker.Container) { c.NoShell() })
	setProcWorkspace(t, ws)
	t.Setenv("TERMOCODE_NO_NVIM", "1")
	m := New()
	m.promptKind, m.promptPath = promptKindRename, "/app/main.go"
	m.handlePromptSubmit("app.go")
	if m.err != "api-1 has no shell (sh), so this is not possible." {
		t.Fatalf("err = %q", m.err)
	}
}

func TestContainerDeleteRemovesInTheContainer(t *testing.T) {
	m, _, _, c := newContainerModel(t)
	m.openDeleteConfirm("/app/pkg")
	m.handleConfirmSelect("delete")
	if _, err := os.Stat(c.Local("/app/pkg")); !os.IsNotExist(err) || m.err != "" {
		t.Fatalf("pkg still there: %v, err %q", err, m.err)
	}
	if _, err := os.Stat(c.Local("/app/main.go")); err != nil {
		t.Fatalf("main.go gone: %v", err)
	}
}

func TestContainerBulkDeleteRemovesInTheContainer(t *testing.T) {
	m, _, _, c := newContainerModel(t)
	c.WriteFile(t, "/app/a.txt", []byte("a"), 0o644)
	m.openBulkDeleteConfirm([]string{"/app/a.txt", "/app/pkg"})
	m.handleConfirmSelect("delete")
	for _, p := range []string{"/app/a.txt", "/app/pkg"} {
		if _, err := os.Stat(c.Local(p)); !os.IsNotExist(err) {
			t.Fatalf("%s still there: %v", p, err)
		}
	}
}

func TestContainerDeleteWithoutShellSaysWhy(t *testing.T) {
	ws, _, _, c := fakeContainerWS(t, func(c *fakedocker.Container) { c.NoShell() })
	setProcWorkspace(t, ws)
	t.Setenv("TERMOCODE_NO_NVIM", "1")
	m := New()
	m.openDeleteConfirm("/app/main.go")
	m.handleConfirmSelect("delete")
	if m.err != "api-1 has no shell (sh), so this is not possible." {
		t.Fatalf("err = %q", m.err)
	}
	if _, err := os.Stat(c.Local("/app/main.go")); err != nil {
		t.Fatalf("main.go gone: %v", err)
	}
}

// Renaming many files at once works on the local disk only: in a
// container it says so and never opens the local bulk rename.
func TestContainerBulkRenameIsRefused(t *testing.T) {
	m, _, _, _ := newContainerModel(t)
	m.explorer.ToggleSelection("/app/main.go")
	m.explorer.ToggleSelection("/app/pkg")
	m.renameExplorerSelection()
	if m.promptOpen || m.promptKind == promptKindBulkRename {
		t.Fatal("bulk rename opened for a container")
	}
	if !strings.Contains(m.err, "one by one") {
		t.Fatalf("err = %q", m.err)
	}
}
