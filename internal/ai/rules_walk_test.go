package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Perf guard (web perf): the rules are read at the start of every chat
// turn, on the UI thread (a send from the laptop or the page). The
// folders' AGENTS.md / CLAUDE.md were found by a walk of the whole
// workspace each time: 13-17 ms in this repo, 51 ms for a chat.send in
// the owner's log. A turn soon after the last one reuses the list of
// rules files the last walk found (their text is read fresh), and a new
// walk runs in the background, so a new rules file counts a turn later.
func TestPerfGuardRulesDoNotWalkTheWorkspaceEachTurn(t *testing.T) {
	root := t.TempDir()
	for d := range 200 {
		dir := filepath.Join(root, fmt.Sprintf("pkg%03d", d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f := range 100 {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.go", f)), []byte("package x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	nested := filepath.Join(root, "pkg007", "AGENTS.md")
	if err := os.WriteFile(nested, []byte("Use tabs in pkg007.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	first := LoadProjectRules(root)
	walk := time.Since(t0)
	if len(first.Scoped) != 1 || !strings.Contains(first.Scoped[0].Text, "tabs") {
		t.Fatalf("first turn scoped rules %+v", first.Scoped)
	}
	// The next turn: the same rules, read fresh, without a walk.
	if err := os.WriteFile(nested, []byte("Use spaces in pkg007.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1 := time.Now()
	next := LoadProjectRules(root)
	again := time.Since(t1)
	if len(next.Scoped) != 1 || !strings.Contains(next.Scoped[0].Text, "spaces") {
		t.Fatalf("next turn scoped rules %+v, want the new text", next.Scoped)
	}
	t.Logf("first turn %v (a walk of 20 000 files), next turn %v", walk, again)
	if again*10 > walk {
		t.Errorf("the next turn took %v, the walk %v: want under a tenth (no walk)", again, walk)
	}
	// A rules file added later counts once the background walk saw it.
	if err := os.WriteFile(filepath.Join(root, "pkg150", "CLAUDE.md"), []byte("Run go vet.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if r := LoadProjectRules(root); len(r.Scoped) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a new folder rules file never counted")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
