package containerhost

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

func TestWriteFileKeepsModeAndOwner(t *testing.T) {
	h, d, c := newHost(t)
	c.WriteFile(t, "/app/run.sh", []byte("old"), 0o750)
	c.SetOwner("/app/run.sh", 1234, 5678)
	if err := (containerFS{h}).WriteFile(t.Context(), "run.sh", []byte("new body"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(c.Local("/app/run.sh"))
	fi, _ := os.Stat(c.Local("/app/run.sh"))
	if string(data) != "new body" || fi.Mode().Perm() != 0o750 {
		t.Fatalf("file = %q %v", data, fi.Mode())
	}
	if uid, gid, _ := c.Owner("/app/run.sh"); uid != 1234 || gid != 5678 {
		t.Fatalf("owner = %d:%d", uid, gid)
	}
	got := d.Received()
	if len(got) != 1 || got[0].Name != "run.sh" {
		t.Fatalf("tar = %+v", got)
	}
	last := d.Calls()[len(d.Calls())-1].Args
	if last[0] != "cp" || last[1] != "-a" || last[2] != "-" || last[3] != "api-1:/app" {
		t.Fatalf("cp argv = %q", last)
	}
}

func TestWriteFileKeepsSpecialBits(t *testing.T) {
	h, d, c := newHost(t)
	c.WriteFile(t, "/app/tool", []byte("x"), 0o755)
	if err := os.Chmod(c.Local("/app/tool"), 0o755|fs.ModeSetgid); err != nil {
		t.Skip(err)
	}
	if err := (containerFS{h}).WriteFile(t.Context(), "tool", []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if m := d.Received()[0].Mode; m&0o2000 == 0 || m&0o777 != 0o755 {
		t.Fatalf("mode = %o", m)
	}
}

func TestWriteFileNewFileGetsTheExecUser(t *testing.T) {
	h, d, c := newHost(t)
	c.SetOwner("/app", 0, 0)
	uid, gid, ok := h.Owner()
	if !ok {
		t.Fatal("no owner from the probe")
	}
	if err := (containerFS{h}).WriteFile(t.Context(), "new.txt", []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	hd := d.Received()[0]
	if hd.Uid != uid || hd.Gid != gid || hd.Mode != 0o600 {
		t.Fatalf("header = %+v, want %d:%d", hd, uid, gid)
	}
	if data, _ := os.ReadFile(c.Local("/app/new.txt")); string(data) != "hi" {
		t.Fatalf("data = %q", data)
	}
}

func TestWriteFileNewFileUnknownUserTakesFolderOwner(t *testing.T) {
	h, d, c := newHost(t)
	c.SetOwner("/app", 501, 20)
	h.mu.Lock()
	h.idKnown = false
	h.mu.Unlock()
	if err := (containerFS{h}).WriteFile(t.Context(), "n.txt", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if hd := d.Received()[0]; hd.Uid != 501 || hd.Gid != 20 {
		t.Fatalf("header = %+v", hd)
	}
}

func TestWriteFilePodmanKeepsOwner(t *testing.T) {
	h, d, c := newHost(t, func(c *Config) { c.Binary = "podman" })
	d.Kind = "podman"
	c.WriteFile(t, "/app/f", []byte("a"), 0o644)
	c.SetOwner("/app/f", 42, 43)
	if err := (containerFS{h}).WriteFile(t.Context(), "f", []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if uid, _, _ := c.Owner("/app/f"); uid != 42 {
		t.Fatalf("podman owner = %d", uid)
	}
}

func TestWriteFileRefusesFolders(t *testing.T) {
	h, _, c := newHost(t)
	c.Mkdir(t, "/app/dir")
	for _, p := range []string{"dir", "", "/app"} {
		if err := (containerFS{h}).WriteFile(t.Context(), p, nil, 0o644); !errors.Is(err, errIsDir) {
			t.Fatalf("WriteFile(%q) = %v", p, err)
		}
	}
}

func TestWriteFileMakesMissingParents(t *testing.T) {
	for _, shell := range []bool{true, false} {
		h, _, c := newHost(t)
		if !shell {
			c.NoShell()
			noShell(h)
		}
		if err := (containerFS{h}).WriteFile(t.Context(), "a/b/c.txt", []byte("hi"), 0o644); err != nil {
			t.Fatalf("shell %v: %v", shell, err)
		}
		if data, err := os.ReadFile(c.Local("/app/a/b/c.txt")); err != nil || string(data) != "hi" {
			t.Fatalf("shell %v: %q, %v", shell, data, err)
		}
	}
}
