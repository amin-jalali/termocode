package containerhost

import (
	"strings"
	"testing"
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
