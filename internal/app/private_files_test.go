package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (Group S5 / S6): on a shared server other users cannot read what
// termocode keeps about a user: the error log, the recent files, folders
// and commands, and the open tabs. Files are 0600, also when an older
// termocode left them 0644; new folders are 0700.
func TestStateFilesArePrivate(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config")
	t.Setenv("XDG_CONFIG_HOME", cfg)
	dir := filepath.Join(cfg, "termocode")

	// Files an older version wrote world-readable.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"errors.log", "recents.json", "workspaces.json", "palette_recents.json", "session.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("[]"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	recordError("state files test")
	saveRecents([]RecentEntry{{Path: "/srv/project/secret-plan.md"}})
	saveWorkspaces([]WorkspaceEntry{{Path: "/srv/project"}})
	pushPaletteRecent("copy-file-ref")
	writeSessionFile(filepath.Join(dir, "session.json"), SessionState{})

	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() && fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s has mode %o, want 0600", strings.TrimPrefix(p, cfg), fi.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A config folder termocode creates itself is private too.
	cfg2 := filepath.Join(t.TempDir(), "fresh")
	t.Setenv("XDG_CONFIG_HOME", cfg2)
	recordError("fresh folder")
	fi, err := os.Stat(filepath.Join(cfg2, "termocode"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("new config folder has mode %o, want 0700", fi.Mode().Perm())
	}
}

// Spec (Group S6): an API key in an error message never reaches the error
// log on disk or the in-app error list.
func TestErrorLogNeverHoldsAPIKeys(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	key := "tc-test-key-0123456789abcdefXYZ"
	ai.RegisterSecret(key)
	recordError("request failed: Authorization: Bearer " + key + " and sk-ant-api03-abcdefghijklmnop")
	b, err := os.ReadFile(filepath.Join(cfg, "termocode", "errors.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{key, "sk-ant-api03-abcdefghijklmnop"} {
		if strings.Contains(string(b), s) {
			t.Errorf("errors.log holds a key: %q", b)
		}
		for _, e := range errorEntries() {
			if strings.Contains(e, s) {
				t.Errorf("error list holds a key: %q", e)
			}
		}
	}
}
