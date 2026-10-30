package app

import (
	"os"
	"path/filepath"
	"testing"
)

// Without a config folder the session reads empty and writes nothing.
func TestCovCoreSessionNoConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := sessionPath(); err == nil {
		t.Fatal("a session path without HOME")
	}
	if _, err := workspaceSessionPath(); err == nil {
		t.Error("a workspace session path without HOME")
	}
	saveSession(SessionState{TerminalRows: 9}) // nowhere to write: no panic
	if s := loadSession(); s.TerminalRows != 0 || len(s.OpenFiles) != 0 {
		t.Errorf("loaded %+v", s)
	}
}

// A broken session file reads as no session.
func TestCovCoreSessionCorrupt(t *testing.T) {
	root := withConfigDir(t)
	p := filepath.Join(root, "termocode", "session.json")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readSessionFile(p); ok {
		t.Error("corrupt file read as ok")
	}
	if s := loadSession(); s.TerminalRows != 0 {
		t.Errorf("loaded %+v", s)
	}
	// Saving the same state twice writes once (the second is a no-op).
	saveSession(SessionState{TerminalRows: 7})
	fi1, _ := os.Stat(p)
	saveSession(SessionState{TerminalRows: 7})
	fi2, _ := os.Stat(p)
	if fi1 == nil || fi2 == nil || !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Error("an unchanged session was written again")
	}
	if loadSession().TerminalRows != 7 {
		t.Error("terminal rows not kept")
	}
}

// Scratch buffers never come back after a restart.
func TestCovCoreSessionZombiePaths(t *testing.T) {
	for p, want := range map[string]bool{
		"":                                  false,
		"/w/main.go":                        false,
		"term://~//123:/bin/sh":             true,
		"/w/README.md PREVIEW":              true,
		"/tmp/x/termocode-find-results.txt": true,
	} {
		if got := isPersistedZombiePath(p); got != want {
			t.Errorf("isPersistedZombiePath(%q) = %v, want %v", p, got, want)
		}
	}
	withConfigDir(t)
	s := SessionState{OpenFiles: []string{"term://~//1:/bin/sh", "/w/a.go"}, ActivePath: "/w/b.md PREVIEW",
		Cursors: map[string][2]int{"term://~//1:/bin/sh": {1, 1}, "/w/a.go": {2, 3}}}
	saveSession(s)
	got := loadSession()
	if len(got.OpenFiles) != 1 || got.OpenFiles[0] != "/w/a.go" || got.ActivePath != "/w/a.go" {
		t.Errorf("open files %q active %q", got.OpenFiles, got.ActivePath)
	}
	if _, ok := got.Cursors["term://~//1:/bin/sh"]; ok {
		t.Error("a terminal cursor came back")
	}
	if !pathWithin("/w/a/b", "/w") || pathWithin("/w2", "/w") || !pathWithin("/w", "/w") {
		t.Error("pathWithin")
	}
}
