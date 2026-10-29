package git

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

func TestBlameRangeBounds(t *testing.T) {
	r := committedRepo(t)
	lines, err := BlameRange(r.dir, "a.txt", 0, 2) // start 0 means line 1
	if err != nil || len(lines) != 2 || lines[0].Line != 1 {
		t.Errorf("BlameRange(0, 2) = %+v, %v", lines, err)
	}
	if _, err := BlameRange(r.dir, "a.txt", 3, 2); err == nil || !strings.Contains(err.Error(), "before start") {
		t.Errorf("end before start = %v", err)
	}
	if _, err := BlameRange(r.dir, "a.txt", 50, 0); err == nil {
		t.Error("start past the end: want git's error")
	}
	if _, err := BlameRange(r.dir, "nope.txt", 1, 0); err == nil {
		t.Error("blame of an untracked path: want error")
	}
}

func TestBranchHelpersOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := BranchSubjects(dir, "main"); err == nil {
		t.Error("BranchSubjects outside a repo: want error")
	}
	if _, err := Dirty(dir); err == nil {
		t.Error("Dirty outside a repo: want error")
	}
	if n, ok := Unpushed(dir); ok || n != 0 {
		t.Errorf("Unpushed outside a repo = %d, %v", n, ok)
	}
}

// Ages over a year with one extra month read like git's.
func TestRelativeDateYearsAndMonths(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	for days, want := range map[int]string{
		385: "1 year, 1 month ago",
		755: "2 years, 1 month ago",
		821: "2 years, 3 months ago",
	} {
		epoch := strconv.FormatInt(now.Add(-time.Duration(days)*24*time.Hour).Unix(), 10)
		if got := relativeDate(epoch); got != want {
			t.Errorf("relativeDate(-%d days) = %q, want %q", days, got, want)
		}
	}
	for in, want := range map[string]string{"garbage": "", "3 fortnights ago": ""} {
		if got := compactAge(in); got != want {
			t.Errorf("compactAge(%q) = %q, want %q", in, got, want)
		}
	}
}

// Unstaging a hunk when nothing is staged says there is no change there.
func TestUnstageHunkNothingStaged(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "a.txt", "one\nTWO\nthree\n")
	if err := UnstageHunk(r.dir, "a.txt", 2); err == nil || !strings.Contains(err.Error(), "no change under the cursor") {
		t.Errorf("UnstageHunk with nothing staged = %v", err)
	}
}

// When git apply rejects a patch, its reason comes back in the error.
func TestRunStdinReportsGitReason(t *testing.T) {
	r := committedRepo(t)
	err := runStdin(r.dir, "not a patch\n", "git", "apply", "--cached", "-")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "patch") {
		t.Errorf("runStdin with a bad patch = %v", err)
	}
}
