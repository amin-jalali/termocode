//go:build unix

package transfer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A named pipe in a folder (a dev server's socket-like file) is skipped
// by both the archive and the local copy instead of blocking on it.
func TestFifoIsSkipped(t *testing.T) {
	src := filepath.Join(t.TempDir(), "proj")
	must(t, os.MkdirAll(src, 0o755))
	must(t, os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644))
	must(t, syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644))
	var buf bytes.Buffer
	must(t, TarGz(&buf, src))
	names, err := ExtractTarGz(&buf, t.TempDir(), 0)
	if err != nil || len(names) != 1 {
		t.Fatalf("%v %v", names, err)
	}
	dst := t.TempDir()
	name, err := CopyInto(context.Background(), src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(filepath.Join(dst, name))
	var got []string
	for _, e := range ents {
		got = append(got, e.Name())
	}
	if strings.Join(got, ",") != "a.txt" {
		t.Fatalf("copied %v", got)
	}
}
