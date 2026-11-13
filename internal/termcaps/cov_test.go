package termcaps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLayerScreenAndZellij(t *testing.T) {
	if Layer(env(map[string]string{"STY": "1.pts"})) != "screen" {
		t.Fatal("screen")
	}
	if Layer(env(map[string]string{"ZELLIJ": "0"})) != "zellij" {
		t.Fatal("zellij")
	}
	if Layer(env(nil)) != "" {
		t.Fatal("no layer")
	}
}

func TestCachePathWithoutHome(t *testing.T) {
	if CachePath(env(nil)) != "" {
		t.Fatal("no home, no path")
	}
	if got := CachePath(env(map[string]string{"HOME": "/h"})); got != "/h/.local/state/termocode/termcaps.json" {
		t.Fatalf("got %q", got)
	}
}

// If the cache folder cannot be made, Remember gives up quietly.
func TestRememberFolderBlocked(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "f")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	Remember(filepath.Join(blocker, "sub", "c.json"), "k", Entry{}, time.Now())
	if _, err := os.Stat(filepath.Join(blocker, "sub")); err == nil {
		t.Fatal("made a folder under a file?")
	}
}

func TestProfileNameWezTermAndString(t *testing.T) {
	if got := name(env(map[string]string{"WEZTERM_EXECUTABLE": "/x"})); got != "wezterm" {
		t.Fatalf("got %q", got)
	}
	s := Profile{Name: "kitty", Layer: "tmux", SSH: true}.String()
	if !strings.Contains(s, "kitty · in tmux · over SSH") {
		t.Fatalf("got %q", s)
	}
}
