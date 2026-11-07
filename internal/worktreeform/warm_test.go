//go:build unix

package worktreeform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// warmTree is a worktree whose setup made node_modules (a nested folder,
// an executable and a relative link) and that has a lock file.
func warmTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "node_modules", "left-pad", "bin"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "node_modules", "left-pad", "index.js"), []byte("module.exports = 1\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "node_modules", "left-pad", "bin", "pad"), []byte("#!/bin/sh\n"), 0o755))
	must(os.MkdirAll(filepath.Join(dir, "node_modules", ".bin"), 0o755))
	must(os.Symlink("../left-pad/bin/pad", filepath.Join(dir, "node_modules", ".bin", "pad")))
	must(os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"v":1}`), 0o644))
	return dir
}

func warmCfg() Config {
	return Config{Setup: []string{"npm ci"}, Warm: &WarmConfig{Paths: []string{"node_modules"}, Key: []string{"package-lock.json"}}}
}

// CMP-96: a snapshot of what the setup made comes back, the same, in a
// new worktree: folders, file modes and links.
func TestWarmSaveRestore(t *testing.T) {
	src := warmTree(t)
	snap := filepath.Join(t.TempDir(), "termocode", "warm", "k1")
	if HasWarm(snap) {
		t.Fatal("a snapshot before saving")
	}
	n, err := SaveWarm(src, snap, []string{"node_modules", "missing-dir"})
	if err != nil || n <= 0 {
		t.Fatalf("save: %d %v", n, err)
	}
	if !HasWarm(snap) {
		t.Fatal("no snapshot after saving")
	}
	dest := t.TempDir()
	got, err := RestoreWarm(snap, dest, []string{"node_modules", "missing-dir"})
	if err != nil || strings.Join(got, ",") != "node_modules" {
		t.Fatalf("restore: %v %v", got, err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "node_modules", "left-pad", "index.js"))
	if err != nil || string(b) != "module.exports = 1\n" {
		t.Errorf("file: %q %v", b, err)
	}
	if fi, err := os.Stat(filepath.Join(dest, "node_modules", "left-pad", "bin", "pad")); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("mode: %v %v", fi, err)
	}
	if l, err := os.Readlink(filepath.Join(dest, "node_modules", ".bin", "pad")); err != nil || l != "../left-pad/bin/pad" {
		t.Errorf("link: %q %v", l, err)
	}
	// A path the new worktree has already is kept, not overwritten.
	dest2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dest2, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _ := RestoreWarm(snap, dest2, []string{"node_modules"}); len(got) != 0 {
		t.Errorf("restored over an existing folder: %v", got)
	}
}

// The key follows the config hash, the setup lines and the lock file's
// bytes; a path outside the worktree is refused everywhere.
func TestWarmKey(t *testing.T) {
	dir := warmTree(t)
	c := warmCfg()
	k1, err := WarmKey(dir, "h1", c)
	if err != nil || k1 == "" {
		t.Fatalf("key %q %v", k1, err)
	}
	if k, _ := WarmKey(dir, "h1", c); k != k1 {
		t.Error("the key is not stable")
	}
	if k, _ := WarmKey(dir, "h2", c); k == k1 {
		t.Error("a new config hash kept the key")
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if k, _ := WarmKey(dir, "h1", c); k == k1 {
		t.Error("a changed lock file kept the key")
	}
	bad := c
	bad.Warm = &WarmConfig{Paths: []string{"node_modules"}, Key: []string{"../outside.lock"}}
	if _, err := WarmKey(dir, "h1", bad); err == nil {
		t.Error("a key file outside the worktree was read")
	}
	if _, err := SaveWarm(dir, filepath.Join(t.TempDir(), "s"), []string{"../etc"}); err == nil {
		t.Error("a path outside the worktree was saved")
	}
	if !c.WarmOn() || (Config{}).WarmOn() || (Config{Warm: &WarmConfig{}}).WarmOn() {
		t.Error("WarmOn")
	}
	if d := WarmDir("/repo/.git", k1); !strings.HasPrefix(d, filepath.Join("/repo/.git", "termocode", "warm")+string(filepath.Separator)) {
		t.Errorf("dir %q", d)
	}
}

// A snapshot larger than the cap is not kept.
func TestWarmTooBig(t *testing.T) {
	old := WarmMax
	WarmMax = 8
	defer func() { WarmMax = old }()
	src := warmTree(t)
	snap := filepath.Join(t.TempDir(), "s")
	if _, err := SaveWarm(src, snap, []string{"node_modules"}); !errors.Is(err, ErrWarmTooBig) {
		t.Fatalf("err %v", err)
	}
	if HasWarm(snap) {
		t.Error("a too big snapshot was kept")
	}
	if _, err := os.Stat(snap); err == nil {
		t.Error("the partial snapshot was left")
	}
}

// Only the newest snapshots are kept.
func TestWarmPrune(t *testing.T) {
	common := t.TempDir()
	base := filepath.Join(common, "termocode", "warm")
	now := time.Now()
	for i, name := range []string{"a", "b", "c", "d"} {
		p := filepath.Join(base, name)
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		at := now.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	PruneWarm(common, 2)
	left, _ := os.ReadDir(base)
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "c,d" {
		t.Errorf("left %v", names)
	}
}

// The repo's env adds plain variables; names must be shell names and a
// ${secret:…} value is dropped (secrets never come from a repo file).
func TestSetupEnv(t *testing.T) {
	base := []string{"PATH=/bin", "NODE_ENV=production"}
	got := SetupEnv(base, map[string]string{
		"NODE_ENV":   "development",
		"PORT":       "3001",
		"bad name":   "x",
		"TOKEN":      "${secret:GITHUB_PAT}",
		"1STARTS":    "x",
		"_UNDER_OK1": "y",
	})
	want := "PATH=/bin NODE_ENV=development PORT=3001 _UNDER_OK1=y"
	if strings.Join(got, " ") != want {
		t.Errorf("env %v, want %s", got, want)
	}
	if names := EnvNames(map[string]string{"PORT": "1", "TOKEN": "${secret:X}", "bad name": ""}); strings.Join(names, ",") != "PORT" {
		t.Errorf("names %v", names)
	}
	if len(base) != 2 || base[1] != "NODE_ENV=production" {
		t.Error("the base env changed")
	}
}
