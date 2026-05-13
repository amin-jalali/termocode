package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"termocode/internal/picker"
)

// withConfigDir overrides XDG_CONFIG_HOME to a tempdir so loadRecents /
// saveRecents / loadWorkspaces / saveWorkspaces operate on a clean slate
// for each test. Returns the tempdir path so tests can poke at on-disk
// state directly.
func withConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestRecents_RoundTrip(t *testing.T) {
	withConfigDir(t)
	if got := loadRecents(); len(got) != 0 {
		t.Fatalf("fresh dir: got %d entries, want 0", len(got))
	}
	pushRecent("/tmp/a")
	pushRecent("/tmp/b")
	pushRecent("/tmp/a") // re-push moves to front, doesn't duplicate
	got := loadRecents()
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Path != "/tmp/a" {
		t.Errorf("most recent should be /tmp/a, got %q", got[0].Path)
	}
	if got[1].Path != "/tmp/b" {
		t.Errorf("second should be /tmp/b, got %q", got[1].Path)
	}
}

func TestRecents_CapEnforced(t *testing.T) {
	withConfigDir(t)
	for i := 0; i < recentsCap+5; i++ {
		// Use a fresh path so each push counts as a new entry.
		pushRecent(filepath.Join("/tmp", string(rune('a'+i%26)), string(rune('a'+i/26))))
	}
	got := loadRecents()
	if len(got) > recentsCap {
		t.Errorf("recents grew past cap: got %d, max %d", len(got), recentsCap)
	}
}

func TestRecents_CorruptFileFallsBackEmpty(t *testing.T) {
	dir := withConfigDir(t)
	// Write garbage where loadRecents expects JSON.
	target := filepath.Join(dir, "termocode", "recents.json")
	if err := writeFile(target, "{ this is not json"); err != nil {
		t.Fatal(err)
	}
	if got := loadRecents(); len(got) != 0 {
		t.Errorf("corrupt file should yield empty list, got %d entries", len(got))
	}
}

func TestWorkspaces_RoundTrip(t *testing.T) {
	withConfigDir(t)
	pushWorkspace("/home/me/project1")
	pushWorkspace("/home/me/project2")
	got := loadWorkspaces()
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Path != "/home/me/project2" {
		t.Errorf("most recent should be project2, got %q", got[0].Path)
	}
}

func TestWorkspaces_DedupOnRepush(t *testing.T) {
	withConfigDir(t)
	pushWorkspace("/a")
	pushWorkspace("/b")
	pushWorkspace("/a") // returning to /a should bump it to the top, not dup
	got := loadWorkspaces()
	if len(got) != 2 {
		t.Errorf("dup not removed: got %d, want 2", len(got))
	}
	if got[0].Path != "/a" {
		t.Errorf("re-pushed entry should be first: %v", got)
	}
}

func TestWorkspaces_CapEnforced(t *testing.T) {
	withConfigDir(t)
	for i := 0; i < workspacesCap+10; i++ {
		pushWorkspace(filepath.Join("/p", string(rune('a'+i%26))+string(rune('a'+i/26))))
	}
	got := loadWorkspaces()
	if len(got) > workspacesCap {
		t.Errorf("workspaces grew past cap: %d > %d", len(got), workspacesCap)
	}
}

func TestRecents_TimestampMonotonic(t *testing.T) {
	withConfigDir(t)
	before := time.Now()
	pushRecent("/x")
	got := loadRecents()
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].OpenedAt.Before(before) {
		t.Errorf("timestamp should be at-or-after the push call: opened=%v before=%v", got[0].OpenedAt, before)
	}
}

// writeFile creates the parent dirs and writes body atomically.
func writeFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func TestUserCommands_LoadEmpty(t *testing.T) {
	withConfigDir(t)
	if got := loadUserCommands(); len(got) != 0 {
		t.Errorf("fresh dir: got %d commands, want 0", len(got))
	}
}

func TestUserCommands_LoadValid(t *testing.T) {
	dir := withConfigDir(t)
	target := filepath.Join(dir, "termocode", "commands.json")
	body := `[
		{"id": "build", "title": "Build", "cmd": "go build ./..."},
		{"id": "lint",  "title": "Lint",  "cmd": "golangci-lint run"}
	]`
	if err := writeFile(target, body); err != nil {
		t.Fatal(err)
	}
	cmds := loadUserCommands()
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want 2", len(cmds))
	}
	if cmds[0].ID != "build" || cmds[0].Cmd != "go build ./..." {
		t.Errorf("first command parsed wrong: %#v", cmds[0])
	}
}

func TestUserCommands_CorruptFallsBack(t *testing.T) {
	dir := withConfigDir(t)
	if err := writeFile(filepath.Join(dir, "termocode", "commands.json"), "not json"); err != nil {
		t.Fatal(err)
	}
	if got := loadUserCommands(); len(got) != 0 {
		t.Errorf("corrupt file should yield empty list, got %d", len(got))
	}
}

func TestUserCommandsItems_Empty(t *testing.T) {
	withConfigDir(t)
	if items := userCommandsItems(); items != nil {
		t.Errorf("empty config should return nil, got %#v", items)
	}
}

func TestUserCommandsItems_FormatsTitleAndHint(t *testing.T) {
	dir := withConfigDir(t)
	body := `[{"id": "x", "title": "Run X", "cmd": "echo hello"}]`
	if err := writeFile(filepath.Join(dir, "termocode", "commands.json"), body); err != nil {
		t.Fatal(err)
	}
	items := userCommandsItems()
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Title != "User: Run X" {
		t.Errorf("title = %q, want 'User: Run X'", items[0].Title)
	}
	if items[0].ID != "user-x" {
		t.Errorf("id = %q, want 'user-x'", items[0].ID)
	}
}

func TestURLPattern(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"see https://example.com for details", "https://example.com"},
		{"http://localhost:8080/api", "http://localhost:8080/api"},
		{"plain text no url here", ""},
		{"link in parens (https://x.io)", "https://x.io"},
		{"in markdown [text](https://md.com/path)", "https://md.com/path"},
		{"trailing punct: https://punc.com.", "https://punc.com."},
	}
	for _, c := range cases {
		got := urlPattern.FindString(c.in)
		if got != c.want {
			t.Errorf("urlPattern.FindString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPaletteRecents_RoundTrip(t *testing.T) {
	withConfigDir(t)
	if got := loadPaletteRecents(); len(got) != 0 {
		t.Fatalf("fresh dir: got %d entries, want 0", len(got))
	}
	pushPaletteRecent("save")
	pushPaletteRecent("git-push")
	pushPaletteRecent("save") // re-push moves to front
	got := loadPaletteRecents()
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0] != "save" {
		t.Errorf("most recent should be 'save', got %q", got[0])
	}
}

func TestPaletteRecents_CapEnforced(t *testing.T) {
	withConfigDir(t)
	for i := 0; i < paletteRecentsCap+5; i++ {
		pushPaletteRecent(string(rune('a' + i)))
	}
	got := loadPaletteRecents()
	if len(got) > paletteRecentsCap {
		t.Errorf("recents grew past cap: %d > %d", len(got), paletteRecentsCap)
	}
}

func TestPromoteRecents_Empty(t *testing.T) {
	items := []picker.Item{{ID: "a"}, {ID: "b"}}
	out := promoteRecents(items, nil)
	if len(out) != 2 || out[0].ID != "a" {
		t.Errorf("no recents should pass through unchanged: %v", out)
	}
}

func TestPromoteRecents_PromotesByRecency(t *testing.T) {
	items := []picker.Item{
		{ID: "alpha"},
		{ID: "beta"},
		{ID: "gamma"},
	}
	// "gamma" was used most recently, then "alpha" — should appear in
	// that order at the front.
	out := promoteRecents(items, []string{"gamma", "alpha"})
	if out[0].ID != "gamma" || out[1].ID != "alpha" {
		t.Errorf("promote order wrong: %v", out)
	}
	if out[2].ID != "beta" {
		t.Errorf("non-recent should keep relative order: %v", out)
	}
	// The promoted entries should be tagged with "recent" in their hint.
	if out[0].Hint != "recent" || out[1].Hint != "recent" {
		t.Errorf("promoted hint not set: %v", out)
	}
}

func TestPromoteRecents_PreservesOriginalHint(t *testing.T) {
	items := []picker.Item{{ID: "x", Hint: "Ctrl+S"}}
	out := promoteRecents(items, []string{"x"})
	if out[0].Hint != "recent · Ctrl+S" {
		t.Errorf("hint should be prefixed with 'recent ·', got %q", out[0].Hint)
	}
}

func TestTruncateHint(t *testing.T) {
	if got := truncateHint("short", 20); got != "short" {
		t.Errorf("under-cap: got %q, want short", got)
	}
	got := truncateHint("aaaaaaaaaaaaaaaaaaaa", 10)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("over-cap should end with …; got %q", got)
	}
}
