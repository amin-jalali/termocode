package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// PERF-2: a typed key is not written to errors.log. It cost an open, a
// write and a close of that file on the UI goroutine for every key, and
// put what the user types (a password in a terminal tab too) on disk.
func TestTypedKeysNeverReachTheErrorLog(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	m := newTestModel(t)
	for _, r := range "hunter2" {
		m, _ = m.handleGlobalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if b, err := os.ReadFile(filepath.Join(cfg, "termocode", "errors.log")); err == nil && strings.Contains(string(b), "[key]") {
		t.Errorf("errors.log holds typed keys:\n%s", b)
	}
	for _, e := range errorEntries() {
		if strings.Contains(e, "[key]") {
			t.Fatalf("error list holds a typed key: %q", e)
		}
	}
}
