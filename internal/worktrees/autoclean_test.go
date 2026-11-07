package worktrees

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CMP-16: the auto clean-up picks clean, unpinned, idle worktrees: the
// ones idle for the set number of days, then the oldest ones over the
// cap. A pinned, busy, dirty, unknown or recently used worktree is never
// picked, and a cap never takes one that was active in the last day.
func TestAutoPicks(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	ago := func(d int) time.Time { return now.Add(-time.Duration(d) * 24 * time.Hour) }
	cands := []AutoCand{
		{Path: "/old", LastActive: ago(40)},
		{Path: "/pinned", Pinned: true, LastActive: ago(90)},
		{Path: "/busy", Busy: true, LastActive: ago(90)},
		{Path: "/dirty", Dirty: 2, LastActive: ago(90)},
		{Path: "/unknown", Dirty: -1, LastActive: ago(90)},
		{Path: "/mid", LastActive: ago(10)},
		{Path: "/week", LastActive: ago(5)},
		{Path: "/today", LastActive: now.Add(-time.Hour)},
		{Path: "/never"},
	}
	str := func(ps []AutoPick) string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Path+"="+p.Why)
		}
		return strings.Join(out, ";")
	}
	for _, c := range []struct {
		name            string
		live, days, max int
		want            string
	}{
		{"off", 9, 0, 0, ""},
		{"idle 30 days", 9, 30, 0, "/old=no activity for 40 days"},
		{"idle 7 days", 9, 7, 0, "/old=no activity for 40 days;/mid=no activity for 10 days"},
		{"cap only", 9, 0, 7, "/old=over the limit of 7 worktrees (oldest first);/mid=over the limit of 7 worktrees (oldest first)"},
		{"cap after idle", 9, 30, 7, "/old=no activity for 40 days;/mid=over the limit of 7 worktrees (oldest first)"},
		{"cap never takes today's", 9, 0, 1, "/old=over the limit of 1 worktree (oldest first);/mid=over the limit of 1 worktree (oldest first);/week=over the limit of 1 worktree (oldest first)"},
		{"under the cap", 5, 0, 7, ""},
	} {
		if got := str(AutoPicks(cands, c.live, c.days, c.max, now)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// CMP-16: DirSize adds up the files of a folder (what archiving frees),
// without following links.
func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, n := range map[string]int{"x": 10, "a/y": 20, "a/b/z": 30} {
		if err := os.WriteFile(filepath.Join(dir, p), make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Symlink(filepath.Join(dir, "a"), filepath.Join(dir, "link"))
	if got := DirSize(dir); got != 60 {
		t.Fatalf("DirSize = %d, want 60", got)
	}
	if got := DirSize(filepath.Join(dir, "missing")); got != 0 {
		t.Fatalf("missing folder: %d", got)
	}
}
