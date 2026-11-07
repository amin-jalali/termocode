package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
)

// GITX-39: the lens block is the run of non-blank lines around the
// cursor; on a blank line it is that line alone.
func TestGitxLineLensBlock(t *testing.T) {
	// Lines 10..18: 10 blank, 11..13 text, 14 blank, 15..16 text, 17..18 blank.
	flags := "100010011"
	for _, c := range []struct{ cur, from, to int }{
		{12, 11, 13}, {11, 11, 13}, {13, 11, 13},
		{15, 15, 16}, {16, 15, 16},
		{14, 14, 14}, {10, 10, 10},
	} {
		if f, to := lensBlock(flags, 10, c.cur); f != c.from || to != c.to {
			t.Errorf("cur %d: %d..%d want %d..%d", c.cur, f, to, c.from, c.to)
		}
	}
	// The window edge ends the block; a cursor outside the flags is its own block.
	if f, to := lensBlock("000", 5, 6); f != 5 || to != 7 {
		t.Errorf("edge: %d..%d", f, to)
	}
	if f, to := lensBlock("000", 5, 20); f != 20 || to != 20 {
		t.Errorf("outside: %d..%d", f, to)
	}
}

// GITX-39: the lens names the newest change of the block (who, when,
// commit, subject), how many authors and commits made the block, and
// the command that opens its history.
func TestGitxLineLensText(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	defer clock.Set(func() time.Time { return now })()
	old := git.BlameRangeLine{Line: 1, Hash: "1111111", Full: "1111111aaaa", Author: "Ann", When: now.AddDate(-1, 0, 0).Unix(), Subject: "start"}
	mid := git.BlameRangeLine{Line: 2, Hash: "2222222", Full: "2222222bbbb", Author: "Bo", When: now.Add(-72 * time.Hour).Unix(), Subject: "fix the parser"}
	mid2 := mid
	mid2.Line = 3
	old4 := old
	old4.Line = 4
	ls := []git.BlameRangeLine{old, mid, mid2, old4}

	got := lineLensText(ls, 1, 4)
	if !strings.HasPrefix(got, "Bo, 3 days ago · 2222222 fix the parser · 2 authors, 2 commits") {
		t.Errorf("block %q", got)
	}
	if !strings.Contains(got, "Git: Show History of Block") {
		t.Errorf("no command hint %q", got)
	}
	// One commit: no counts.
	if got := lineLensText(ls, 1, 1); !strings.HasPrefix(got, "Ann, ") || strings.Contains(got, "commits") {
		t.Errorf("one line %q", got)
	}
	// An uncommitted line is the newest change.
	u := git.BlameRangeLine{Line: 5, Hash: "uncommitted", Author: "Not Committed Yet"}
	if got := lineLensText(append(ls, u), 1, 5); !strings.HasPrefix(got, "You · Not committed yet · 2 authors, 2 commits") {
		t.Errorf("uncommitted %q", got)
	}
	// No blame for the range: no lens.
	if got := lineLensText(ls, 7, 9); got != "" {
		t.Errorf("empty range %q", got)
	}
}

// GITX-39: Toggle Line History Lens loads the blame, shows the lens for
// the cursor's block, follows the cursor, is saved, and a second toggle
// hides it.
func TestGitxLineLensToggle(t *testing.T) {
	m, _ := gitxBlameRepo(t)
	cmd := m.dispatchPaletteAction("git-line-lens")
	if cmd == nil {
		t.Fatal("no load")
	}
	m.onGitBlameLoaded(covMsg[gitBlameLoadedMsg](t, cmd))
	if !strings.Contains(m.gitBlame.lensText, " second") {
		t.Fatalf("lens %q", m.gitBlame.lensText)
	}
	if v := loadSettings().GitLineLens; v == nil || !*v {
		t.Error("not saved")
	}
	m.cursorLine = 1
	m.paintLineLens()
	if !strings.Contains(m.gitBlame.lensText, " third") || m.gitBlame.lensFrom != 1 {
		t.Errorf("lens did not follow the cursor: %q at %d", m.gitBlame.lensText, m.gitBlame.lensFrom)
	}
	m.dispatchPaletteAction("git-line-lens")
	if m.gitBlame.lensText != "" {
		t.Errorf("still painted: %q", m.gitBlame.lensText)
	}
	if v := loadSettings().GitLineLens; v == nil || *v {
		t.Error("off not saved")
	}
}

// GITX-39: Show History of Block lists the commits that changed the
// cursor's block, newest first; Enter opens one like the log.
func TestGitxBlockHistory(t *testing.T) {
	m, dir := gitxBlameRepo(t)
	m.cursorLine = 1
	m.dispatchPaletteAction("git-block-history")
	if !m.pickerOpen || m.pickerKind != pickerKindGitLog {
		t.Fatal("no picker")
	}
	third := covGit(t, dir, "rev-parse", "--short", "HEAD")
	second := covGit(t, dir, "rev-parse", "--short", "HEAD~1")
	first := covGit(t, dir, "rev-parse", "--short", "HEAD~2")
	v := ansi.Strip(m.picker.View())
	i, j := strings.Index(v, third+"  third"), strings.Index(v, first+"  first")
	// Look for the second commit's row, not the word: the hint
	// "0 seconds ago" has "second" in it too.
	if i < 0 || j < i || strings.Contains(v, second+"  second") {
		t.Errorf("rows (want third, then first, no second):\n%s", v)
	}
}
