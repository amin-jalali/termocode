package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/rebaseview"
	"github.com/amin-jalali/termocode/internal/toast"
)

// These tests never run git's rebase with termocode as its editor (the
// test binary is no rebase helper): a stopped rebase is made with plain
// git, and the runs that need the helper are only built.

// covThreeCommits is a repo with commits first, second, third on main.
func covThreeCommits(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "second")
	covWrite(t, dir, "c.txt", "c\n")
	covCommit(t, dir, "third")
	covRefresh(m)
	return m, dir
}

// Rebase from Here…: the picker lists the commits, the pick opens the
// card; Start asks first (Stash and Continue when the tree is dirty) and
// Cancel goes back to the card; Esc closes it.
func TestCovGitRebaseCardFlow(t *testing.T) {
	m, dir := covThreeCommits(t)
	m.dispatchPaletteAction("git-rebase")
	if !m.pickerOpen || m.pickerKind != pickerKindRebaseFrom || !strings.Contains(m.picker.View(), "second") {
		t.Fatal("no commit picker")
	}
	full := covGit(t, dir, "rev-parse", "HEAD~1")
	todo := covMsg[rebaseTodoMsg](t, m.handlePickerSelect(picker.SelectMsg{ID: full}))
	if todo.err != nil || !strings.HasPrefix(todo.title, "Interactive Rebase · main onto ") {
		t.Fatalf("todo: %+v", todo)
	}
	m.onRebaseMsg(todo)
	if !m.rebaseCardOpen() || !strings.Contains(ansi.Strip(m.rebaseLayer(strings.Repeat("\n", 39))), "second") {
		t.Fatal("rebase card not drawn")
	}

	// Resize and mouse go to the card.
	m2, _ := m.routeToRebaseCard(tea.WindowSizeMsg{Width: 120, Height: 30})
	*m = m2
	if m.w != 120 || m.h != 30 {
		t.Error("resize not applied")
	}
	m2, _ = m.routeToRebaseCard(tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	*m = m2
	m2, _ = m.routeToRebaseCard(tea.FocusMsg{})
	*m = m2

	// Reword the newest row: the prompt is prefilled with its subject; the
	// typed subject keeps the body.
	m2, _ = m.routeToRebaseCard(tea.KeyMsg{Type: tea.KeyEnd})
	*m = m2
	m2, _ = m.routeToRebaseCard(covRunes("r"))
	*m = m2
	if !m.promptOpen || m.promptKind != promptKindRebaseReword || !strings.Contains(m.prompt.View(), "third") {
		t.Fatalf("no reword prompt: %q", m.prompt.View())
	}
	m.promptOpen = false
	m.handlePromptSubmit("third, reworded")

	// Enter on the card: check the state, ask first.
	covWrite(t, dir, "a.txt", "dirty\n")
	m2, cmd := m.routeToRebaseCard(tea.KeyMsg{Type: tea.KeyEnter})
	*m = m2
	check := covMsg[rebaseCheckMsg](t, cmd)
	if !check.dirty || len(check.todo.Items) != 2 || check.todo.Items[1].Message != "third, reworded" {
		t.Fatalf("check: %+v", check)
	}
	m.onRebaseMsg(check)
	checkCardRule(t, "start rebase", *m, true)
	if b := m.confirm.Buttons(); b[1].ID != "rebase-stash" {
		t.Errorf("dirty tree: buttons %+v", b)
	}
	m.confirmOpen = false
	if m.handleConfirmSelect("cancel") != nil || !m.rebaseCardOpen() {
		t.Fatal("Cancel left the rebase card")
	}
	if m.onRebaseConfirm("rebase-start") != nil {
		t.Error("a second answer ran the rebase")
	}

	// Esc closes the card; a late check opens no confirm card.
	m2, _ = m.routeToRebaseCard(tea.KeyMsg{Type: tea.KeyEsc})
	*m = m2
	if m.rebaseCardOpen() {
		t.Fatal("Esc did not close the card")
	}
	m.openRebaseConfirm(check)
	if m.confirmOpen {
		t.Error("confirm opened with no card")
	}
	m.onRebaseReword("late")
	if m.rebaseLayer("base") != "base" {
		t.Error("closed card still drawn")
	}
}

// A detached HEAD's card title says only what it rebases onto; the
// reword of a row with a message keeps its body; an empty subject keeps
// the old message.
func TestCovGitRebaseTitleAndReword(t *testing.T) {
	if got := rebaseTitle("", "abcdef1234"); got != "Interactive Rebase · onto abcdef1" {
		t.Errorf("title %q", got)
	}
	if got := rebaseTitle("main", ""); got != "Interactive Rebase · main onto the root" {
		t.Errorf("root title %q", got)
	}
	m, dir := covThreeCommits(t)
	m.onRebaseMsg(covMsg[rebaseTodoMsg](t, m.gitRebaseFromCmd(covGit(t, dir, "rev-parse", "HEAD"))))
	item := git.RebaseItem{Hash: "abc1234", Subject: "old", Message: "old subject\n\nthe body"}
	m.openRebaseRewordPrompt(rebaseview.RewordMsg{Index: 0, Item: item})
	if !strings.Contains(m.prompt.View(), "old subject") {
		t.Errorf("prompt not prefilled with the message subject: %q", m.prompt.View())
	}
	m.onRebaseReword("   ")
	m.onRebaseReword("new subject")
	m2, cmd := m.routeToRebaseCard(tea.KeyMsg{Type: tea.KeyEnter})
	*m = m2
	if got := covMsg[rebaseCheckMsg](t, cmd).todo.Items[0].Message; got != "new subject\n\nthe body" {
		t.Errorf("reworded message %q", got)
	}
}

// Why the card does not open, and what the confirm card warns about.
func TestCovGitRebaseRefusalsAndWarnings(t *testing.T) {
	m, _ := covThreeCommits(t)
	if m.gitRebaseFromCmd("") != nil {
		t.Error("empty commit gave a command")
	}
	for _, c := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("x: %w", git.ErrRebaseMerge), "Can't rebase over a merge commit"},
		{git.ErrRebaseEmpty, "No commits to rebase"},
		{errors.New("boom"), "Rebase failed"},
	} {
		m.onRebaseTodo(rebaseTodoMsg{err: c.err})
		if !toastHas(m, c.want) || m.rebaseCardOpen() {
			t.Errorf("%v: no %q toast", c.err, c.want)
		}
	}

	todo := git.RebaseTodo{Base: "0123456789", Items: []git.RebaseItem{
		{Action: git.RebaseEdit, Hash: "a"}, {Action: git.RebasePick, Hash: "b"}, {Action: git.RebaseDrop, Hash: "c"}}}
	txt := rebaseConfirmText(rebaseCheckMsg{todo: todo, upstream: "origin/main", pushed: 2})
	for _, want := range []string{"onto 0123456", "edit 1", "drop 1", "It stops at each edit row", "2 of these commits are on origin/main"} {
		if !strings.Contains(txt, want) {
			t.Errorf("confirm text misses %q:\n%s", want, txt)
		}
	}
}

// covStoppedRebase stops a plain-git rebase in dir: on a conflict (b.txt
// changed on both sides) or, with edit, at an edit row.
func covStoppedRebase(t *testing.T, m *Model, dir string, edit bool) {
	t.Helper()
	if edit {
		t.Setenv("GIT_SEQUENCE_EDITOR", "sed -i -e 1s/^pick/edit/")
		covGit(t, dir, "rebase", "-q", "-i", "HEAD~1")
		covRefresh(m)
		return
	}
	covGit(t, dir, "checkout", "-q", "-b", "side", "HEAD~1")
	covWrite(t, dir, "b.txt", "side\n")
	covCommit(t, dir, "side change")
	covGit(t, dir, "checkout", "-q", "main")
	covWrite(t, dir, "b.txt", "main\n")
	covCommit(t, dir, "main change")
	c := covGitCmd(dir, "rebase", "side")
	if out, err := c.CombinedOutput(); err == nil {
		t.Fatalf("rebase did not stop: %s", out)
	}
	covRefresh(m)
}

// A stopped rebase: the banner row, Enter on it opens the control card
// (Close first); a new rebase is refused; Continue with markers left is
// refused; Abort puts the branch back.
func TestCovGitRebaseStoppedOnConflict(t *testing.T) {
	m, dir := covThreeCommits(t)
	head := covGit(t, dir, "rev-parse", "HEAD")
	covStoppedRebase(t, m, dir, false)
	if !m.gitRebase.InProgress || len(m.gitRebase.Conflicts) != 1 {
		t.Fatalf("rebase state %+v", m.gitRebase)
	}
	if row := ansi.Strip(m.renderGitRebaseRow(false, false, 60)); !strings.Contains(row, "1 conflict") {
		t.Errorf("banner %q", row)
	}
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowRebase })
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirm.SafeID() != "rebase-close" || focusedCardID(t, *m) != "rebase-close" {
		t.Errorf("control card: Close is not first and focused (%+v)", m.confirm.Buttons())
	}
	if !strings.Contains(m.confirm.View(), "Fix them in CONFLICTS") {
		t.Error("control card does not say to fix the conflicts")
	}
	m.confirmOpen = false
	if m.handleConfirmSelect("rebase-close") != nil {
		t.Error("Close ran something")
	}

	// Enter on a conflicted file is the panel's (the editor opens it).
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowFile && r.section == gitSecConflicts })
	if _, ok := covKey(m, tea.KeyMsg{Type: tea.KeyEnter}); !ok {
		t.Error("Enter on a conflict was not taken")
	}

	for name, run := range map[string]func(){
		"rebase palette": func() { m.dispatchPaletteAction("git-rebase") },
		"R":              func() { m.gitRebaseFromCmd(head) },
		"fixup":          func() { m.gitFixupIntoCmd(head) },
	} {
		m.toast = toast.New()
		run()
		if !toastHas(m, "A rebase is in progress") {
			t.Errorf("%s during a rebase: no busy toast", name)
		}
	}

	m.dispatchPaletteAction("git-rebase-status")
	if !m.confirmOpen || m.confirmKind != confirmKindRebaseControl {
		t.Fatal("rebase status opened no card")
	}
	m.confirmOpen = false
	done := covMsg[rebaseDoneMsg](t, m.handleConfirmSelect("rebase-continue"))
	m.onRebaseMsg(done)
	if !toastHas(m, "Can't continue yet") {
		t.Error("continue with markers: no warning")
	}
	if m.onRebaseControl("rebase-skip") == nil {
		t.Error("Skip gave no command")
	}
	m.onRebaseMsg(covMsg[rebaseDoneMsg](t, m.onRebaseControl("rebase-abort")))
	if !toastHas(m, "Rebase aborted") || covGit(t, dir, "rev-parse", "HEAD") == head {
		t.Fatal("abort did not report")
	}
	covRefresh(m)
	if m.gitRebase.InProgress {
		t.Fatal("still rebasing after abort")
	}
	m.dispatchPaletteAction("git-rebase-status")
	if !toastHas(m, "No rebase in progress") {
		t.Error("no No rebase toast")
	}
}

// Stopped at an edit row: the banner and the card say what to do.
func TestCovGitRebaseStoppedAtEdit(t *testing.T) {
	m, dir := covThreeCommits(t)
	covStoppedRebase(t, m, dir, true)
	if m.gitRebase.Action != "edit" {
		t.Fatalf("state %+v", m.gitRebase)
	}
	if row := ansi.Strip(m.renderGitRebaseRow(true, true, 60)); !strings.Contains(row, "edit "+m.gitRebase.Hash) {
		t.Errorf("banner %q", row)
	}
	m.openRebaseControl()
	if !strings.Contains(m.confirm.View(), "Change the commit") {
		t.Error("card does not say to change the commit")
	}
	// Any other stop says only "stopped".
	m.gitRebase.Action = "break"
	if row := ansi.Strip(m.renderGitRebaseRow(false, false, 60)); !strings.Contains(row, "stopped") {
		t.Errorf("banner %q", row)
	}
	m.onRebaseMsg(covMsg[rebaseDoneMsg](t, m.onRebaseControl("rebase-abort")))
}

// How a finished run is told.
func TestCovGitRebaseDoneToasts(t *testing.T) {
	m := &Model{w: 160, h: 40}
	for _, c := range []struct {
		msg  rebaseDoneMsg
		want string
	}{
		{rebaseDoneMsg{err: errors.New("boom")}, "Rebase failed"},
		{rebaseDoneMsg{out: git.RebaseOutcome{Stopped: true, Conflicts: []string{"a", "b", "c"}}}, "Rebase stopped: 3 conflicts"},
		{rebaseDoneMsg{out: git.RebaseOutcome{Stopped: true}}, "Rebase stopped to edit a commit"},
		{rebaseDoneMsg{}, "Rebase done"},
		{rebaseDoneMsg{summary: "2 picks"}, "Rebase done: 2 picks"},
	} {
		m.onRebaseDone(c.msg)
		if !toastHas(m, c.want) {
			t.Errorf("no %q toast", c.want)
		}
	}
	if m.onRebaseConfirm("rebase-start") != nil {
		t.Error("confirm with no card ran something")
	}
	if m.onRebaseControl("rebase-close") != nil {
		t.Error("Close ran something")
	}
	m.gitRebase = git.RebaseState{}
	m.openRebaseControl()
	if m.confirmOpen {
		t.Error("control card with no rebase")
	}
}

func TestRebaseDonePointsToUndoAction(t *testing.T) {
	for _, summary := range []string{"", "2 picks"} {
		m := &Model{w: 160, h: 40}
		m.onRebaseDone(rebaseDoneMsg{summary: summary})
		if !toastHas(m, "u / Git: Undo Last Git Action") {
			t.Fatal("missing safe undo action")
		}
		if toastHas(m, "ORIG_HEAD") {
			t.Fatal("raw reset advice remains")
		}
	}
}
