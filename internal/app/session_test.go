package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// twoWorkspaces makes a clean config dir and two project folders.
func twoWorkspaces(t *testing.T) (cfg, a, b string) {
	t.Helper()
	cfg = withConfigDir(t)
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	a, b = filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return cfg, a, b
}

// Each workspace keeps its own tabs; layout preferences are shared.
func TestSessionPerWorkspace(t *testing.T) {
	_, a, b := twoWorkspaces(t)
	t.Chdir(a)
	saveSession(SessionState{
		OpenFiles:    []string{filepath.Join(a, "x.go")},
		ActivePath:   filepath.Join(a, "x.go"),
		Roots:        []string{"/extra"},
		TerminalRows: 17,
	})

	t.Chdir(b)
	got := loadSession()
	if len(got.OpenFiles) != 0 || got.ActivePath != "" || len(got.Roots) != 0 {
		t.Fatalf("workspace b sees a's state: %+v", got)
	}
	if got.TerminalRows != 17 {
		t.Errorf("layout not shared: TerminalRows = %d", got.TerminalRows)
	}
	got.OpenFiles = []string{filepath.Join(b, "y.go")}
	got.TerminalRows = 20
	saveSession(got)

	t.Chdir(a)
	got = loadSession()
	if len(got.OpenFiles) != 1 || got.OpenFiles[0] != filepath.Join(a, "x.go") {
		t.Errorf("workspace a lost its files: %v", got.OpenFiles)
	}
	if len(got.Roots) != 1 || got.Roots[0] != "/extra" {
		t.Errorf("workspace a lost its extra roots: %v", got.Roots)
	}
	if got.TerminalRows != 20 {
		t.Errorf("TerminalRows = %d, want 20", got.TerminalRows)
	}
	if got.Workspace != a {
		t.Errorf("Workspace = %q, want %q", got.Workspace, a)
	}
}

func writeLegacySession(t *testing.T, cfg string, s SessionState) {
	t.Helper()
	data, _ := json.Marshal(s)
	dir := filepath.Join(cfg, "termocode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// An old global session.json moves to the workspace it belongs to, once;
// other workspaces never see it.
func TestSessionLegacyMigration(t *testing.T) {
	cfg, a, b := twoWorkspaces(t)
	writeLegacySession(t, cfg, SessionState{
		OpenFiles:    []string{filepath.Join(a, "main.go")},
		ActivePath:   filepath.Join(a, "main.go"),
		TerminalRows: 12,
	})

	// b opens first: not its session, so nothing is migrated...
	t.Chdir(b)
	got := loadSession()
	if len(got.OpenFiles) != 0 {
		t.Fatalf("b got a's legacy files: %v", got.OpenFiles)
	}
	if got.TerminalRows != 12 {
		t.Errorf("legacy layout lost: %d", got.TerminalRows)
	}
	saveSession(got) // ...and saving b must keep a's legacy state

	t.Chdir(a)
	got = loadSession()
	if len(got.OpenFiles) != 1 || got.OpenFiles[0] != filepath.Join(a, "main.go") {
		t.Fatalf("a did not get its legacy files: %v", got.OpenFiles)
	}
	// Migrated once: session.json no longer holds workspace state.
	global, _ := readSessionFile(filepath.Join(cfg, "termocode", "session.json"))
	if hasWorkspaceState(global) {
		t.Errorf("session.json still has workspace state: %+v", global)
	}
	if global.TerminalRows != 12 {
		t.Errorf("layout dropped by migration: %d", global.TerminalRows)
	}
	t.Chdir(b)
	if got := loadSession(); len(got.OpenFiles) != 0 {
		t.Errorf("b sees a's files after migration: %v", got.OpenFiles)
	}
}

// A legacy session without any path can not be placed: the first
// workspace takes it.
func TestSessionLegacyFallback(t *testing.T) {
	cfg, a, _ := twoWorkspaces(t)
	writeLegacySession(t, cfg, SessionState{AILastChat: "chat-1"})
	t.Chdir(a)
	if got := loadSession(); got.AILastChat != "chat-1" {
		t.Errorf("AILastChat = %q, want chat-1", got.AILastChat)
	}
}

func TestLegacyBelongsTo(t *testing.T) {
	cases := []struct {
		s    SessionState
		root string
		want bool
	}{
		{SessionState{OpenFiles: []string{"/p/a/x.go"}}, "/p/a", true},
		{SessionState{OpenFiles: []string{"/p/ab/x.go"}}, "/p/a", false},
		{SessionState{ExpandedDirs: []string{"/p/a"}}, "/p/a", true},
		{SessionState{OpenFiles: []string{"rel.go"}}, "/p/a", true},
		{SessionState{}, "/p/a", true},
	}
	for i, c := range cases {
		if got := legacyBelongsTo(c.s, c.root); got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}

func TestWorkspaceSessionFileStable(t *testing.T) {
	x := workspaceSessionFile("/home/u/proj")
	if x != workspaceSessionFile("/home/u/proj/") {
		t.Error("trailing slash changes the session file")
	}
	if x == workspaceSessionFile("/home/u/proj2") {
		t.Error("two workspaces share a session file")
	}
}
