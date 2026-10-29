package containerhost

import (
	"errors"
	"os"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

func TestRemove(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/d/e/f.txt", nil, 0o644)
	c.WriteFile(t, "/app/keep.txt", nil, 0o644)
	f := containerFS{h}
	if err := f.Remove(t.Context(), "d"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.Local("/app/d")); !os.IsNotExist(err) {
		t.Fatal("d still there")
	}
	if _, err := os.Stat(c.Local("/app/keep.txt")); err != nil {
		t.Fatal("removed too much")
	}
	if err := f.Remove(t.Context(), "missing"); err != nil {
		t.Fatalf("missing: %v", err)
	}
}

func TestRemoveRefusesRootAndOutside(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/outside.txt", nil, 0o644)
	f := containerFS{h}
	for _, p := range []string{"", ".", "/app", "a/.."} {
		if err := f.Remove(t.Context(), p); !errors.Is(err, workspace.ErrIsRoot) {
			t.Fatalf("Remove(%q) = %v", p, err)
		}
	}
	if err := f.Remove(t.Context(), "../outside.txt"); !errors.Is(err, workspace.ErrOutsideRoot) {
		t.Fatalf("outside: %v", err)
	}
	if _, err := os.Stat(c.Local("/outside.txt")); err != nil {
		t.Fatal("outside file removed")
	}
}

func TestRemoveWithoutShell(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/a", nil, 0o644)
	noShell(h)
	if err := (containerFS{h}).Remove(t.Context(), "a"); !errors.Is(err, ErrNoShell) {
		t.Fatalf("err = %v", err)
	}
}
