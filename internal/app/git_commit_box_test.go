package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"termocode/internal/git"
)

func TestGitCommitBoxEditing(t *testing.T) {
	m := &Model{}
	m.gitFocusCommitBox()

	m.gitCommitInsert("fix bug")
	if m.gitCommitMsg != "fix bug" || m.gitCommitCaret != 7 {
		t.Fatalf("after insert: msg=%q caret=%d", m.gitCommitMsg, m.gitCommitCaret)
	}

	// Move to the start and insert a prefix.
	m.gitCommitCaret = 0
	m.gitCommitInsert("WIP: ")
	if m.gitCommitMsg != "WIP: fix bug" {
		t.Fatalf("after prefix insert: %q", m.gitCommitMsg)
	}
	if m.gitCommitCaret != 5 {
		t.Fatalf("caret should sit after the inserted prefix, got %d", m.gitCommitCaret)
	}

	// Caret sits at index 5 (after "WIP: "). Backspace deletes index 4 (space).
	m.gitCommitBackspace()
	if m.gitCommitMsg != "WIP:fix bug" || m.gitCommitCaret != 4 {
		t.Fatalf("after backspace: msg=%q caret=%d", m.gitCommitMsg, m.gitCommitCaret)
	}

	// Forward delete at caret 4 removes 'f'.
	m.gitCommitDeleteFwd()
	if m.gitCommitMsg != "WIP:ix bug" || m.gitCommitCaret != 4 {
		t.Fatalf("after delete-fwd: msg=%q caret=%d", m.gitCommitMsg, m.gitCommitCaret)
	}

	// Caret clamps at both ends.
	m.gitCommitMoveCaret(-100)
	if m.gitCommitCaret != 0 {
		t.Fatalf("caret should clamp to 0, got %d", m.gitCommitCaret)
	}
	m.gitCommitMoveCaret(100)
	if m.gitCommitCaret != len([]rune(m.gitCommitMsg)) {
		t.Fatalf("caret should clamp to len, got %d", m.gitCommitCaret)
	}

	m.clearCommitBox()
	if m.gitCommitMsg != "" || m.gitCommitFocused || m.gitCommitCaret != 0 {
		t.Fatalf("clearCommitBox left state: %+v", m)
	}
}

func TestGitActionButtonsZonesNonOverlapping(t *testing.T) {
	m := Model{gitBranch: git.Branch{Name: "main", Ahead: 2, Behind: 1}}
	btns := m.gitActionButtons(29)
	if len(btns) != 3 {
		t.Fatalf("want 3 chips, got %d", len(btns))
	}
	prevEnd := -1
	for _, b := range btns {
		if b.x0 <= prevEnd {
			t.Errorf("chip %q overlaps previous (x0=%d prevEnd=%d)", b.id, b.x0, prevEnd)
		}
		if b.x1 < b.x0 {
			t.Errorf("chip %q has inverted span %d..%d", b.id, b.x0, b.x1)
		}
		prevEnd = b.x1
	}
}

// The commit box + action bar are fixed chrome above the scrollable accordion;
// every row they render must stay within the content width or the sidebar
// grows and shoves the editor right.
func TestGitCommitChromeFitsWidth(t *testing.T) {
	const w = 29
	m := Model{
		gitIsRepo:        true,
		gitBranch:        git.Branch{Name: "main", Ahead: 2, Behind: 1},
		gitCommitMsg:     "a fairly long commit message that should scroll within the field",
		gitCommitFocused: true,
		gitCommitCaret:   64,
	}
	for _, row := range []string{m.renderGitCommitBox(w), m.renderGitActionBar(w)} {
		if got := lipgloss.Width(row); got != w {
			t.Errorf("chrome row width = %d, want exactly %d: %q", got, w, row)
		}
	}
}

func TestGitCommitPlaceholderShownWhenEmptyAndBlurred(t *testing.T) {
	m := Model{}
	row := m.renderGitCommitBox(29)
	if !strings.Contains(row, "Message") {
		t.Errorf("empty unfocused box should show placeholder, got %q", row)
	}
	m.gitFocusCommitBox()
	if strings.Contains(m.renderGitCommitBox(29), "Message") {
		t.Errorf("focused box should not show placeholder text")
	}
}
