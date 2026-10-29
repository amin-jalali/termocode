package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveFontDelta(t *testing.T) {
	three := 3
	zero := 0
	cases := []struct {
		name string
		env  string
		cfg  *int
		want int
	}{
		{"default is off: few terminals can do it", "", nil, 0},
		{"config", "", &three, 3},
		{"config zero", "", &zero, 0},
		{"env wins", "-4", &three, -4},
		{"bad env falls back to config", "abc", &three, 3},
	}
	for _, c := range cases {
		if got := resolveFontDelta(c.env, c.cfg); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// SCN-14: `termocode <folder>` for a folder that was moved or deleted
// stops with a clear line instead of opening the current folder.
func TestMissingFolderArg(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone")
	if got := missingFolderArg(gone); !strings.Contains(got, "folder "+gone+" not found") {
		t.Errorf("missing folder: %q", got)
	}
	for _, ok := range []string{dir, "setup", "help", "-h", "--help", "--version", ""} {
		if got := missingFolderArg(ok); got != "" {
			t.Errorf("%q: %q, want none", ok, got)
		}
	}
}

// Only a start that may draw the UI here keeps the GC held for its
// first frame (bootgc); every other command releases it at once.
func TestTUICommand(t *testing.T) {
	for _, args := range [][]string{nil, {"some/folder"}} {
		if !tuiCommand(args) {
			t.Errorf("tuiCommand(%q) = false, want true", args)
		}
	}
	for _, args := range [][]string{{serverArg, "/w"}, {"sessions"}, {"ssh", "h"}, {"mcp"},
		{"attach"}, {"kill"}, {"setup"}, {"-h"}, {"--help"}, {"help"}, {"--version"}, {"-v"}} {
		if tuiCommand(args) {
			t.Errorf("tuiCommand(%q) = true, want false", args)
		}
	}
}
