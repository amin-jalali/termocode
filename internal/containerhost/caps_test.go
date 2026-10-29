package containerhost

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

func TestCapsProbe(t *testing.T) {
	h, _, c := newHost(t)
	if !h.Caps().HasShell || h.HasTool("git") {
		t.Fatalf("caps = %+v", h.Caps())
	}
	if uid, _, ok := h.Owner(); !ok || uid < 0 {
		t.Fatalf("owner = %d %v", uid, ok)
	}
	if h.Caps().User == "" || h.Caps().Arch == "" {
		t.Fatalf("caps = %+v", h.Caps())
	}

	// A second container with tools and musl.
	c.AddTool(t, "git")
	c.AddTool(t, "gopls")
	c.WriteFile(t, "/lib/ld-musl-x86_64.so.1", nil, 0o755)
	h2, err := New(t.Context(), h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	caps := h2.Caps()
	if !h2.HasTool("git") || !h2.HasTool("gopls") || h2.HasTool("rg") || caps.Libc != "musl" {
		t.Fatalf("caps = %+v", caps)
	}
	// The probe gets its paths as positional arguments.
	last := c.Execs()[len(c.Execs())-1]
	if last.Argv[0] != "sh" || strings.Contains(last.Argv[2], " /") || last.Argv[4] != "/lib" || last.Argv[5] != "/lib64" {
		t.Fatalf("paths are not positional: %q", last.Argv[3:6])
	}
}

func TestCapsGlibc(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/lib64/ld-linux-x86-64.so.2", nil, 0o755)
	h2, err := New(t.Context(), h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if h2.Caps().Libc != "glibc" {
		t.Fatalf("libc = %q", h2.Caps().Libc)
	}
}

func TestNormArch(t *testing.T) {
	for in, want := range map[string]string{"x86_64": "amd64", "aarch64": "arm64", "riscv64": "riscv64"} {
		if got := normArch(in); got != want {
			t.Errorf("normArch(%q) = %q", in, got)
		}
	}
}

func TestCapsWithoutShell(t *testing.T) {
	d := fakedocker.New()
	c := d.Add(t, "distroless")
	c.Mkdir(t, "/app")
	c.NoShell()
	c.User, c.Image, c.Arch = "65532:65533", "gcr.io/x", "arm64"
	h, err := New(t.Context(), Config{Container: "distroless", Root: "/app", X: d})
	if err != nil {
		t.Fatal(err)
	}
	caps := h.Caps()
	if caps.HasShell || caps.User != "65532:65533" || caps.Arch != "arm64" || len(caps.Tools) != 0 {
		t.Fatalf("caps = %+v", caps)
	}
	if uid, gid, ok := h.Owner(); !ok || uid != 65532 || gid != 65533 {
		t.Fatalf("owner = %d:%d %v", uid, gid, ok)
	}
}

func TestNewMissingRootFolder(t *testing.T) {
	d := fakedocker.New()
	d.Add(t, "api-1")
	_, err := New(t.Context(), Config{Container: "api-1", Root: "/nope", X: d})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
}

func TestNumericUser(t *testing.T) {
	cases := map[string][3]int{"": {0, 0, 1}, "1000": {1000, 1000, 1}, "1:2": {1, 2, 1}, "node": {0, 0, 0}, "1:x": {0, 0, 0}}
	for in, want := range cases {
		uid, gid, ok := numericUser(in)
		if uid != want[0] || gid != want[1] || ok != (want[2] == 1) {
			t.Errorf("numericUser(%q) = %d %d %v", in, uid, gid, ok)
		}
	}
}
