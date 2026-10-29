package containerhost

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

// mounted is a host whose /app is a bind mount of the fake's own /app
// folder (both views are the same files, as with a real bind mount).
func mounted(t *testing.T, local bool, mounts func(c *fakedocker.Container) []fakedocker.Mount) (*Host, *fakedocker.Docker, *fakedocker.Container) {
	t.Helper()
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/app")
	c.Mounts = mounts(c)
	h, err := New(t.Context(), Config{Container: "api-1", Root: "/app", X: d, LocalEngine: local})
	if err != nil {
		t.Fatal(err)
	}
	return h, d, c
}

func bindApp(c *fakedocker.Container) []fakedocker.Mount {
	return []fakedocker.Mount{{Type: "bind", Source: c.Local("/app"), Destination: "/app", RW: true}}
}

func engineCalls(d *fakedocker.Docker) int { return len(d.Calls()) }

func TestMountFSUsesTheLocalDisk(t *testing.T) {
	h, d, c := mounted(t, true, bindApp)
	if h.Caps().MountOf != c.Local("/app") {
		t.Fatalf("MountOf = %q", h.Caps().MountOf)
	}
	before := engineCalls(d)
	f, ctx := h.FS(), t.Context()
	if err := f.WriteFile(ctx, "a/b.txt", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := f.ReadFile(ctx, "/app/a/b.txt"); err != nil || string(data) != "hi" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	if ents, err := f.ReadDir(ctx, "."); err != nil || names(ents) != "a/" {
		t.Fatalf("ReadDir = %v, %v", ents, err)
	}
	if err := f.Rename(ctx, "a/b.txt", "c.txt"); err != nil {
		t.Fatal(err)
	}
	if err := f.Remove(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat(ctx, "c.txt"); err != nil {
		t.Fatal(err)
	}
	if err := f.Mkdir(ctx, "m", 0o755); err != nil {
		t.Fatal(err)
	}
	if n := engineCalls(d) - before; n != 0 {
		t.Fatalf("%d engine calls on a bind mount", n)
	}
}

func TestMountFSAbsoluteSymlinkGoesThroughTheEngine(t *testing.T) {
	h, d, c := mounted(t, true, bindApp)
	c.WriteFile(t, "/etc/app.conf", []byte("container file"), 0o644)
	if err := os.Symlink("/etc/app.conf", c.Local("/app/conf")); err != nil {
		t.Skip("no symlinks:", err)
	}
	before := engineCalls(d)
	data, err := h.FS().ReadFile(t.Context(), "conf")
	if err != nil || string(data) != "container file" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	if engineCalls(d) == before {
		t.Fatal("an absolute link was read on the local disk")
	}
}

func TestMountFSNotUsed(t *testing.T) {
	cases := map[string]struct {
		local  bool
		mounts func(c *fakedocker.Container) []fakedocker.Mount
	}{
		"remote engine": {false, bindApp},
		"read-only": {true, func(c *fakedocker.Container) []fakedocker.Mount {
			return []fakedocker.Mount{{Type: "bind", Source: c.Local("/app"), Destination: "/app"}}
		}},
		"volume": {true, func(c *fakedocker.Container) []fakedocker.Mount {
			return []fakedocker.Mount{{Type: "volume", Source: c.Local("/app"), Destination: "/app", RW: true}}
		}},
		"mount inside": {true, func(c *fakedocker.Container) []fakedocker.Mount {
			return append(bindApp(c), fakedocker.Mount{Type: "volume", Source: "/x", Destination: "/app/node_modules", RW: true})
		}},
		"source missing": {true, func(c *fakedocker.Container) []fakedocker.Mount {
			return []fakedocker.Mount{{Type: "bind", Source: filepath.Join(c.Root, "nope"), Destination: "/app", RW: true}}
		}},
		"no mount": {true, func(*fakedocker.Container) []fakedocker.Mount { return nil }},
	}
	for name, tc := range cases {
		h, _, _ := mounted(t, tc.local, tc.mounts)
		if h.Caps().MountOf != "" {
			t.Errorf("%s: MountOf = %q", name, h.Caps().MountOf)
		}
		if _, ok := h.FS().(containerFS); !ok {
			t.Errorf("%s: FS = %T", name, h.FS())
		}
	}
}

func TestMountFSParentMount(t *testing.T) {
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/src/app")
	c.Mounts = []fakedocker.Mount{{Type: "bind", Source: c.Local("/src"), Destination: "/src", RW: true}}
	h, err := New(t.Context(), Config{Container: "api-1", Root: "/src/app", X: d, LocalEngine: true})
	if err != nil {
		t.Fatal(err)
	}
	if h.Caps().MountOf != c.Local("/src/app") {
		t.Fatalf("MountOf = %q", h.Caps().MountOf)
	}
}
