package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
)

// Spec (scm-graph §2.7): the hover card is 44 cells wide, shows at most 8
// body lines, all refs as pills on their own row, the stat line with the
// short SHA and one hint row "Enter open   y copy SHA".
func TestCommitHoverCardLayout(t *testing.T) {
	var body []string
	for i := 0; i < 20; i++ {
		body = append(body, "body line "+itoa(i))
	}
	d := git.CommitDetail{Hash: "a1b2c3d", Author: "Amin Jalali", DateRel: "2 hours ago", DateAbs: "2026-10-15 14:32",
		Subject: "fix: flaky test", Body: strings.Join(body, "\n"), Files: 4, Add: 88, Del: 24}
	refs := []git.Ref{{Name: "main"}, {Name: "v1.2", Kind: git.RefTag}, {Name: "origin/main", Kind: git.RefRemote}}
	card := renderCommitHoverCard(d, refs, commitHoverCardW)
	if commitHoverCardW != 44 {
		t.Errorf("card width %d, want 44", commitHoverCardW)
	}
	var plain []string
	for _, l := range card {
		p := ansi.Strip(l)
		if w := ansi.StringWidth(p); w != 44 {
			t.Errorf("line width %d, want 44: %q", w, p)
		}
		plain = append(plain, p)
	}
	all := strings.Join(plain, "\n")
	if n := strings.Count(all, "body line"); n > 8 {
		t.Errorf("%d body lines, want at most 8:\n%s", n, all)
	}
	if !strings.Contains(all, "…") {
		t.Errorf("a cut body has no …:\n%s", all)
	}
	refRow := -1
	for i, p := range plain {
		if strings.Contains(p, "main") && strings.Contains(p, "v1.2") && strings.Contains(p, "origin/main") {
			refRow = i
		}
	}
	if refRow < 0 {
		t.Errorf("no row with all refs:\n%s", all)
	}
	last := plain[len(plain)-2] // above the bottom border
	if !strings.Contains(last, "Enter open   y copy SHA") {
		t.Errorf("last row is not the hint: %q", last)
	}
	stat := plain[len(plain)-3]
	if !strings.Contains(stat, "+88 −24  4 files") || !strings.Contains(stat, "a1b2c3d") {
		t.Errorf("stat row: %q", stat)
	}
	if refRow >= 0 && refRow != len(plain)-4 {
		t.Errorf("refs row %d, want just above the stat row (%d):\n%s", refRow, len(plain)-4, all)
	}
	for _, l := range card {
		if strings.Contains(l, "\x1b[2m") {
			t.Errorf("faint in the hover card: %q", l)
		}
	}

	// No refs: no empty pill row.
	card = renderCommitHoverCard(d, nil, commitHoverCardW)
	if len(card) != len(plain)-1 {
		t.Errorf("no refs: %d lines, want %d", len(card), len(plain)-1)
	}
}
