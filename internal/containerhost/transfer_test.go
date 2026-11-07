package containerhost

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyOutFileAndFolder(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/a.txt", []byte("aaa"), 0o640)
	c.WriteFile(t, "/app/src/main.go", []byte("package main\n"), 0o644)
	dest := t.TempDir()
	name, err := h.CopyOut(t.Context(), "a.txt", dest, 0)
	if err != nil || name != "a.txt" {
		t.Fatalf("CopyOut(file) = %q, %v", name, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(b) != "aaa" {
		t.Fatalf("file = %q", b)
	}
	dest2 := t.TempDir()
	name, err = h.CopyOut(t.Context(), "/app/src", dest2, 0)
	if err != nil || name != "src" {
		t.Fatalf("CopyOut(folder) = %q, %v", name, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest2, "src", "main.go")); string(b) != "package main\n" {
		t.Fatalf("folder file = %q", b)
	}
}

func TestCopyOutRefusesOutsideAndCaps(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/big.bin", make([]byte, 100), 0o644)
	if _, err := h.CopyOut(t.Context(), "../etc/passwd", t.TempDir(), 0); err == nil {
		t.Fatal("copied from outside the workspace")
	}
	if _, err := h.CopyOut(t.Context(), "big.bin", t.TempDir(), 10); !errors.Is(err, ErrTransferTooLarge) {
		t.Fatalf("cap: %v", err)
	}
}

func TestCopyInNeverReplaces(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/notes.txt", []byte("old"), 0o644)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "notes.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "pkg", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "pkg", "sub", "x.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := h.CopyIn(t.Context(), []string{filepath.Join(src, "notes.txt"), filepath.Join(src, "pkg")}, "", 0)
	if err != nil || len(names) != 2 || names[0] != "notes (1).txt" || names[1] != "pkg" {
		t.Fatalf("CopyIn = %q, %v", names, err)
	}
	if b, _ := os.ReadFile(c.Local("/app/notes.txt")); string(b) != "old" {
		t.Fatalf("replaced: %q", b)
	}
	if b, _ := os.ReadFile(c.Local("/app/notes (1).txt")); string(b) != "new" {
		t.Fatalf("new file = %q", b)
	}
	if b, _ := os.ReadFile(c.Local("/app/pkg/sub/x.go")); string(b) != "x" {
		t.Fatalf("folder = %q", b)
	}
}

func TestCopyInSkipsSymlinksAndChecksTheFolder(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/file.txt", []byte("f"), 0o644)
	src := t.TempDir()
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "link")); err != nil {
		t.Skip("no symlinks:", err)
	}
	names, err := h.CopyIn(t.Context(), []string{filepath.Join(src, "link")}, "", 0)
	if err != nil || len(names) != 0 {
		t.Fatalf("symlink: %q, %v", names, err)
	}
	if _, err := h.CopyIn(t.Context(), nil, "file.txt", 0); err == nil {
		t.Fatal("uploaded into a file")
	}
	if _, err := h.CopyIn(t.Context(), nil, "../", 0); err == nil {
		t.Fatal("uploaded outside the workspace")
	}
	big := filepath.Join(src, "big")
	if err := os.WriteFile(big, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.CopyIn(t.Context(), []string{big}, "", 10); !errors.Is(err, ErrTransferTooLarge) {
		t.Fatalf("cap: %v", err)
	}
}
