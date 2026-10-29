package containerhost

import (
	"os"
	"testing"
)

func noShell(h *Host) {
	h.mu.Lock()
	h.caps.HasShell = false
	h.mu.Unlock()
}

func TestMkdirWithShell(t *testing.T) {
	h, d, c := newHost(t)
	if err := (containerFS{h}).Mkdir(t.Context(), "a/b c/d", 0o750); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(c.Local("/app/a/b c/d"))
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o750 {
		t.Fatalf("dir = %v, %v", fi, err)
	}
	if len(d.Received()) != 0 {
		t.Fatal("sh mkdir sent a tar")
	}
	// An existing folder keeps its mode.
	if err := os.Chmod(c.Local("/app/a"), 0o711); err != nil {
		t.Fatal(err)
	}
	if err := (containerFS{h}).Mkdir(t.Context(), "a", 0o755); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(c.Local("/app/a")); fi.Mode().Perm() != 0o711 {
		t.Fatalf("existing folder changed to %v", fi.Mode())
	}
	c.WriteFile(t, "/app/file", nil, 0o644)
	if err := (containerFS{h}).Mkdir(t.Context(), "file", 0o755); err == nil {
		t.Fatal("Mkdir over a file worked")
	}
}

func TestMkdirWithoutShellSendsOnlyMissingFolders(t *testing.T) {
	h, d, c := newHost(t)
	c.Mkdir(t, "/app/have")
	c.NoShell()
	noShell(h)
	if err := (containerFS{h}).Mkdir(t.Context(), "have/x/y", 0o755); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(c.Local("/app/have/x/y")); err != nil || !fi.IsDir() {
		t.Fatalf("dir = %v, %v", fi, err)
	}
	got := d.Received()
	if len(got) != 2 || got[0].Name != "x/" || got[1].Name != "x/y/" {
		t.Fatalf("tar = %+v", got)
	}
	uid, _, _ := h.Owner()
	if got[0].Uid != uid {
		t.Fatalf("uid = %d, want %d", got[0].Uid, uid)
	}
	last := d.Calls()[len(d.Calls())-1].Args
	if last[3] != "api-1:/app/have" {
		t.Fatalf("put into %q", last[3])
	}
	// Existing: nothing sent.
	n := len(d.Received())
	if err := (containerFS{h}).Mkdir(t.Context(), "have", 0o755); err != nil || len(d.Received()) != n {
		t.Fatalf("existing = %v, sent %d", err, len(d.Received())-n)
	}
	c.WriteFile(t, "/app/file", nil, 0o644)
	if err := (containerFS{h}).Mkdir(t.Context(), "file/x", 0o755); err == nil {
		t.Fatal("Mkdir under a file worked")
	}
}
