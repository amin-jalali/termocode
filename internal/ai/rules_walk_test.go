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
	root := rulesBigWorkspace(t)
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

// rulesBigWorkspace is a workspace of 20 000 files in 200 folders.
func rulesBigWorkspace(t *testing.T) string {
	t.Helper()
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
	return root
}

// The first turn need not walk either: the page's chat warms the list
// when it opens (WarmProjectRules walks in the background), and a turn
// while that walk still runs waits for it instead of missing rules.
func TestPerfGuardRulesFirstTurnAfterWarm(t *testing.T) {
	root := rulesBigWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "pkg009", "AGENTS.md"), []byte("Keep it short.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A turn while the warm walk runs: the rules are there.
	WarmProjectRules(root)
	if r := LoadProjectRules(root); len(r.Scoped) != 1 {
		t.Fatalf("a turn during the warm walk has scoped rules %+v", r.Scoped)
	}
	other := rulesBigWorkspace(t)
	if err := os.WriteFile(filepath.Join(other, "pkg003", "CLAUDE.md"), []byte("Use go vet.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	WarmProjectRules(other)
	if d := time.Since(t0); d > 5*time.Millisecond {
		t.Errorf("WarmProjectRules took %v; it must not walk on the caller", d)
	}
	time.Sleep(500 * time.Millisecond) // the user opens the chat and types
	t1 := time.Now()
	r := LoadProjectRules(other)
	first := time.Since(t1)
	if len(r.Scoped) != 1 || !strings.Contains(r.Scoped[0].Text, "vet") {
		t.Fatalf("first turn scoped rules %+v", r.Scoped)
	}
	if first > 8*time.Millisecond {
		t.Errorf("the first turn after the warm walk took %v, want no walk (under 8 ms)", first)
	}
}
