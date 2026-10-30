package app

import (
	"os"
	"path/filepath"
	"testing"
)

// The status bar and the explorer name the Model's workspace. They read
// it from the Model on every frame, not from the process folder (which
// cost a getcwd and a stat per frame when no workspace was set).
func TestProjectLabelIsTheModelsWorkspace(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	base := t.TempDir()
	a, b := filepath.Join(base, "alpha"), filepath.Join(base, "beta")
	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(a)
	m := newTestModel(t)
	t.Chdir(b)
	if got := m.statusState().Project; got != "alpha" {
		t.Fatalf("status bar project %q, want alpha", got)
	}
	if got := m.projectLabel(); got != "ALPHA" {
		t.Fatalf("projectLabel %q, want ALPHA", got)
	}
}
