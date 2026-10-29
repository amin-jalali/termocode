package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Go project without go.mod (or with tests at its root) names the root
// package "." like every other framework, never an empty row.
func TestDiscoverGoRootPackageWithoutModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a_test.go"), []byte("package a\n\nfunc TestA(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b_test.go"), []byte("package sub\n\nfunc TestB(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgs, err := Discover(root, FrameworkGo)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pkgs {
		got = append(got, p.Name)
	}
	if strings.Join(got, ",") != ".,sub" {
		t.Fatalf("package names %q", got)
	}
	if GoImportPath("example.com/m", ".") != "example.com/m" || GoImportPath("example.com/m", "x/y") != "example.com/m/x/y" {
		t.Fatal("with a module the import path is used")
	}
}
