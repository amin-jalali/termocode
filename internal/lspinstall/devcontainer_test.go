package lspinstall

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const sampleDevcontainer = `{
  // JSONC: comments and trailing commas are fine
  "name": "demo",
  "image": "mcr.microsoft.com/devcontainers/base:ubuntu",
  "features": {
    "ghcr.io/devcontainers/features/go:1": {"version": "latest"},
    "ghcr.io/devcontainers/features/python:1": {},
  },
  /* block */
  "customizations": {"vscode": {"extensions": ["rust-lang.rust-analyzer", "Some.Unknown"]}},
}`

func toolNames(ts []Tool) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

func TestDevContainerToolsParse(t *testing.T) {
	root := t.TempDir()
	if DevContainerTools(root) != nil {
		t.Fatal("no file must give no tools")
	}
	dir := filepath.Join(root, ".devcontainer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(sampleDevcontainer), 0o644); err != nil {
		t.Fatal(err)
	}
	got := toolNames(DevContainerTools(root))
	want := []string{"gopls", "pyright", "rust_analyzer", "dlv", "debugpy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDevContainerImageName(t *testing.T) {
	got := toolNames(devContainerToolsFrom([]byte(`{"image":"mcr.microsoft.com/devcontainers/typescript-node:1-20"}`)))
	if !reflect.DeepEqual(got, []string{"ts_ls", "js-debug"}) {
		t.Fatalf("got %v", got)
	}
	if devContainerToolsFrom([]byte(`not json`)) != nil {
		t.Fatal("broken file must give nil")
	}
}

func TestMissingOnly(t *testing.T) {
	ts := devContainerToolsFrom([]byte(`{"features":{"x/go:1":{}}}`))
	got := toolNames(MissingOnly(ts, func(t Tool) bool { return t.Name == "gopls" }))
	if !reflect.DeepEqual(got, []string{"dlv"}) {
		t.Fatalf("got %v", got)
	}
}

func TestDevContainerAskedPersists(t *testing.T) {
	t.Setenv(ToolsDirEnv, t.TempDir())
	if DevContainerAsked("/w/a") {
		t.Fatal("fresh state")
	}
	if err := MarkDevContainerAsked("/w/a"); err != nil {
		t.Fatal(err)
	}
	if !DevContainerAsked("/w/a") || DevContainerAsked("/w/b") {
		t.Fatal("asked must be per workspace")
	}
}
