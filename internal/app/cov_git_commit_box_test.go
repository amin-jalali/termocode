package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Typing in the commit box: c focuses it; letters, Space, the arrows,
// Home / End, Backspace and Delete edit at the caret; other keys are
// swallowed; Esc gives the keys back to the list; Enter commits.
func TestCovGitCommitBoxTyping(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "x\n")
	covGit(t, dir, "add", "a.txt")
	covRefresh(m)
	covKey(m, covRunes("c"))
	if !m.gitCommitFocused {
		t.Fatal("c did not focus the box")
	}
	for _, k := range []tea.KeyMsg{covRunes("fix"), {Type: tea.KeySpace}, covRunes("tyop"),
		{Type: tea.KeyLeft}, {Type: tea.KeyBackspace}, {Type: tea.KeyRight}, covRunes("o"),
		{Type: tea.KeyHome}, {Type: tea.KeyDelete}, covRunes("F"), {Type: tea.KeyEnd}, covRunes("!"),
		{Type: tea.KeyF5}} {
		if _, ok := covKey(m, k); !ok {
			t.Fatalf("key %v not taken while typing", k)
		}
	}
	if m.gitCommitMsg != "Fix typo!" {
		t.Fatalf("message %q", m.gitCommitMsg)
	}
	// Edits at the ends do nothing.
	m.gitCommitCaret = 0
	covKey(m, tea.KeyMsg{Type: tea.KeyBackspace})
	covKey(m, tea.KeyMsg{Type: tea.KeyLeft})
	m.gitCommitCaret = len(m.gitCommitMsg)
	covKey(m, tea.KeyMsg{Type: tea.KeyDelete})
	covKey(m, tea.KeyMsg{Type: tea.KeyRight})
	m.gitCommitCaret = 99
	m.gitCommitInsert("")
	if m.gitCommitMsg != "Fix typo!" || m.gitCommitCaret != len("Fix typo!") {
		t.Fatalf("edge edits changed %q caret %d", m.gitCommitMsg, m.gitCommitCaret)
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.gitCommitFocused {
		t.Fatal("Esc kept the focus")
	}
	covKey(m, covRunes("c"))
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if covGit(t, dir, "log", "-1", "--format=%s") != "Fix typo!" || m.gitCommitMsg != "" {
		t.Fatal("Enter did not commit")
	}
}

// The box draws a placeholder (cut when narrow), the text with the caret
// in the middle, and scrolls a long message; a tiny box is blank.
func TestCovGitCommitBoxDraws(t *testing.T) {
	m := &Model{}
	if got := ansi.Strip(m.renderGitCommitBox(3)); got != "   " {
		t.Errorf("tiny box %q", got)
	}
	if got := ansi.Strip(m.renderGitCommitBox(10)); !strings.HasSuffix(strings.TrimSpace(got), "…") {
		t.Errorf("narrow placeholder %q", got)
	}
	m.gitCommitMsg, m.gitCommitFocused, m.gitCommitCaret = "hello", true, 2
	if got := ansi.Strip(m.renderGitCommitBox(20)); !strings.Contains(got, "hello") {
		t.Errorf("box %q", got)
	}
	m.gitCommitMsg = strings.Repeat("a", 50) + "END"
	m.gitCommitCaret = len(m.gitCommitMsg)
	if got := ansi.Strip(m.renderGitCommitBox(20)); !strings.Contains(got, "END") || ansi.StringWidth(got) != 20 {
		t.Errorf("scrolled box %q", got)
	}
	if got := ansi.Strip(m.renderGitActionBar(4)); ansi.StringWidth(got) != 4 {
		t.Errorf("narrow bar %q", got)
	}
	if centerLabel("x", 0) != "" || centerLabel("Commit", 3) != "Co…" {
		t.Error("centerLabel")
	}
	if itoa(-42) != "-42" || itoa(0) != "0" {
		t.Error("itoa")
	}
	if got := padBgRight("abc", 2, sidebarBg); got != "abc" {
		t.Errorf("pad %q", got)
	}
}

// Clicks: on the box the caret goes to the clicked column; Sync on the
// bar syncs; a click on a gap of the bar does nothing; other rows are not
// the box's.
func TestCovGitCommitBoxClicks(t *testing.T) {
	m, dir := covGitRepo(t)
	covGit(t, dir, "remote", "add", "origin", covBare(t))
	m.gitCommitMsg = "abc"
	m2, _, ok := m.gitCommitAreaClick(0, gitCommitInputRow, 40)
	if !ok || !m2.gitCommitFocused || m2.gitCommitCaret != 0 {
		t.Fatalf("click at the rail: caret %d", m2.gitCommitCaret)
	}
	m2, _, _ = m.gitCommitAreaClick(30, gitCommitInputRow, 40)
	if m2.gitCommitCaret != 3 {
		t.Errorf("click past the text: caret %d", m2.gitCommitCaret)
	}
	var sync gitActionBtn
	for _, b := range m.gitActionButtons(40) {
		if b.id == gitBtnSync {
			sync = b
		}
	}
	m2, cmd, ok := m.gitCommitAreaClick(sync.x0, gitActionBarRow, 40)
	if !ok || cmd == nil || m2.gitRemoteBusy == "" {
		t.Fatal("Sync click did not sync")
	}
	covMsg[gitRemoteDoneMsg](t, cmd)
	if _, _, ok := m.gitCommitAreaClick(39, gitActionBarRow, 40); !ok {
		t.Error("click on the bar's gap not taken")
	}
	if _, _, ok := m.gitCommitAreaClick(1, gitActionBarRow+3, 40); ok {
		t.Error("click below the bar taken")
	}
	if _, _, ok := m.gitActionBarDispatch("nope"); !ok {
		t.Error("unknown chip not taken")
	}
}
