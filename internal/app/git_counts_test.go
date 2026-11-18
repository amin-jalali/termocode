package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/git"
)

// Perf (SCN-12): the STAGED / CHANGES / CONFLICTS counts are counted once
// when a git snapshot arrives, not by a walk over every changed file on
// each frame (50 000 changes, three walks a frame). A list set some other
// way (tests) is still counted right.
func TestGitFileCountsTallyOnSnapshot(t *testing.T) {
	m := Model{}
	files := []git.FileStatus{
		{Code: "M ", Path: "a"}, {Code: " M", Path: "b"}, {Code: "MM", Path: "c"},
		{Code: "??", Path: "d"}, {Code: "UU", Path: "e"}, {Code: "A ", Path: "f"},
	}
	m.applyGitSnapshot(GitMsg{IsRepo: true, Files: files})
	if !m.gitTally.of(m.gitFiles) {
		t.Fatal("the snapshot left no tally of its files")
	}
	staged, changed := m.gitFileCounts()
	if staged != 3 || changed != 3 || m.gitConflictCount() != 1 {
		t.Fatalf("staged %d changed %d conflicts %d, want 3 3 1", staged, changed, m.gitConflictCount())
	}
	m.gitFiles = []git.FileStatus{{Code: "UU", Path: "x"}, {Code: "UU", Path: "y"}, {Code: " M", Path: "z"}}
	if m.gitTally.of(m.gitFiles) {
		t.Fatal("the tally claims a list it did not count")
	}
	if s, c := m.gitFileCounts(); s != 0 || c != 1 || m.gitConflictCount() != 2 {
		t.Fatalf("a list set directly: staged %d changed %d conflicts %d, want 0 1 2", s, c, m.gitConflictCount())
	}
	m.gitFiles = m.gitFiles[:1]
	if m.gitTally.of(m.gitFiles) || m.gitConflictCount() != 1 {
		t.Fatalf("a shorter list: conflicts %d, want 1", m.gitConflictCount())
	}
}
