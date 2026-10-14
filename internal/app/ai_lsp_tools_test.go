package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/lspinstall"
)

// Spec: with no language server attached, the LSP tools say so and name
// the installer command when termocode can install a server for the
// file type; an installed server that did not start is told apart.
func TestAINoLSPErrorNamesTheInstaller(t *testing.T) {
	t.Setenv(lspinstall.ToolsDirEnv, t.TempDir())
	t.Setenv("PATH", t.TempDir())
	got := aiNoLSPError("/w/main.go", "go").Error()
	if !strings.Contains(got, "no language server for .go files") || !strings.Contains(got, "LSP: Manage Language Servers...") ||
		!strings.Contains(got, "gopls") {
		t.Errorf("go: %q", got)
	}
	if got := aiNoLSPError("/w/x.xyz", "").Error(); !strings.Contains(got, "no language server for .xyz files") ||
		!strings.Contains(got, "no installer") {
		t.Errorf("xyz: %q", got)
	}
	if _, err := aiLSPResult("/w/a.go", "rename", "NOCAP\tfakels"); err == nil || err.Error() != "the language server (fakels) does not support rename" {
		t.Errorf("nocap: %v", err)
	}
}

// Spec: the edit report lists each changed file with its edit count and
// whether it was saved; nothing changed says so and skips the IDE sync.
func TestAILSPEditSummary(t *testing.T) {
	rel := aiRel("/w")
	got, changed := aiLSPEditSummary("renamed", "saved\t/w/a.go\t2\nkept\t/w/b.go\t1", rel)
	if !changed || !strings.HasPrefix(got, "renamed: 2 files changed (3 edits)") ||
		!strings.Contains(got, "- a.go (2 edits, saved)") || !strings.Contains(got, "- b.go (1 edit, not saved: the tab has unsaved user edits)") {
		t.Errorf("summary %q", got)
	}
	if got, changed := aiLSPEditSummary("renamed", "", rel); changed || got != "the language server returned no changes" {
		t.Errorf("empty: %q %v", got, changed)
	}
}
