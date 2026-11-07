package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// GITX-39: a git refresh hands the file statuses to the editor tab bar,
// so an open modified file shows its M badge on its tab.
func TestGitSnapshotGivesTabsStatusBadges(t *testing.T) {
	m := &Model{}
	cwd, err := m.rootDir()
	if err != nil {
		t.Skip("no workspace root")
	}
	m.tabs.SetBuffers([]nvim.BufferInfo{{ID: 1, Path: filepath.Join(cwd, "tabbadge.go")}}, 1)
	m.tabs.SetWidth(80)
	m.applyGitSnapshot(GitMsg{IsRepo: true, Files: []git.FileStatus{{Code: " M", Path: "tabbadge.go"}}})
	if out := ansi.Strip(m.tabs.View()); !strings.Contains(out, "tabbadge.go M") {
		t.Fatalf("tab bar %q lacks the M badge", out)
	}
	m.applyGitSnapshot(GitMsg{IsRepo: true})
	if out := ansi.Strip(m.tabs.View()); strings.Contains(out, "tabbadge.go M") {
		t.Fatalf("badge stayed after the file became clean: %q", out)
	}
}
