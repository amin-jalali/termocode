package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/lspinstall"
)

func devContainerWorkspace(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".devcontainer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const devContainerGo = `{
  // Go project
  "image": "mcr.microsoft.com/devcontainers/go:1",
}`

func TestDevContainerSuggestOnceAndNotNowRemembered(t *testing.T) {
	t.Setenv(lspinstall.ToolsDirEnv, t.TempDir())
	t.Setenv("PATH", t.TempDir()) // nothing installed
	root := devContainerWorkspace(t, devContainerGo)

	var m Model
	if m.maybeSuggestDevContainerTools(root) == nil {
		t.Fatal("first open should suggest")
	}
	cards := m.toast.Cards(80)
	if len(cards) != 1 || cards[0].Click == nil {
		t.Fatalf("want one clickable toast, got %+v", cards)
	}
	for _, bad := range []string{"VS Code", "VSCode", "Visual Studio"} {
		if strings.Contains(strings.Join(cards[0].Lines, " "), bad) {
			t.Errorf("toast names %q", bad)
		}
	}
	// Same session: no second toast.
	if m.maybeSuggestDevContainerTools(root) != nil {
		t.Fatal("must not repeat in a session")
	}
	// New session (fresh model), toast ignored = "Not now": still silent.
	var m2 Model
	if m2.maybeSuggestDevContainerTools(root) != nil {
		t.Fatal("Not now must be remembered across sessions")
	}
}

func TestDevContainerSuggestSkipsWithoutFileOrWhenInstalled(t *testing.T) {
	t.Setenv(lspinstall.ToolsDirEnv, t.TempDir())
	t.Setenv("PATH", t.TempDir())
	var m Model
	if m.maybeSuggestDevContainerTools(t.TempDir()) != nil {
		t.Fatal("no dev container file, no toast")
	}

	// Every implied tool is on PATH: nothing to suggest.
	bin := t.TempDir()
	for _, n := range []string{"gopls", "dlv"} {
		tool, _ := lspinstall.Lookup(n)
		if err := os.WriteFile(filepath.Join(bin, tool.Bin), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	var m2 Model
	if m2.maybeSuggestDevContainerTools(devContainerWorkspace(t, devContainerGo)) != nil {
		t.Fatal("tools present, no toast")
	}
}

func TestDevContainerClickOpensInstallerInstallsNothing(t *testing.T) {
	t.Setenv(lspinstall.ToolsDirEnv, t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := devContainerWorkspace(t, devContainerGo)
	m := Model{w: 120, h: 40}
	m.maybeSuggestDevContainerTools(root)
	click, ok := m.toast.Cards(80)[0].Click.(devContainerClickMsg)
	if !ok {
		t.Fatal("toast click is not a devContainerClickMsg")
	}
	if got := strings.Join(click.tools, ","); got != "gopls,dlv" {
		t.Fatalf("tools = %s", got)
	}
	m.openDevContainerTools(click.tools)
	if !m.pickerOpen || m.pickerKind != pickerKindToolManager {
		t.Fatalf("pickerOpen=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	if len(m.lspMgr.installing) != 0 {
		t.Fatalf("opening the list must install nothing: %v", m.lspMgr.installing)
	}
	items := devContainerPickerItems(click.tools, nil, lspinstall.StatusOf)
	if len(items) != 2 || items[0].ID != "gopls" || items[1].ID != "dlv" {
		t.Fatalf("items = %+v", items)
	}
}
