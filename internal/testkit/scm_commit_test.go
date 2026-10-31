package testkit

import (
	"strings"
	"testing"
	"time"
)

// Source Control: the commit message box and the commit actions.

// Spec: Given a staged file, when the user types a message in the box and
// presses Enter, then a commit with that subject exists, only the staged
// file is in it, the box is empty again and the graph shows the new commit.
func TestSCMCommitStagedWithMessage(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// staged\n")
		r.git("add", "main.go")
		r.write("README.md", "# Demo\nnot staged\n")
	})
	d.WaitForText("main.go", 3*time.Second)
	typeCommitMessage(d, "feat: add a line")
	if !strings.Contains(text(d), "feat: add a line") {
		t.Fatalf("typed text not in the box:%s", dump(d))
	}
	d.Key("enter")
	until(d, func() bool { return inSection(d, "GRAPH", "add a line") })
	if got := r.git("log", "-1", "--format=%s"); got != "feat: add a line" {
		t.Errorf("HEAD subject = %q", got)
	}
	if got := r.git("show", "--name-only", "--format=", "HEAD"); got != "main.go" {
		t.Errorf("commit files = %q, want only main.go", got)
	}
	if got := r.porcelain(); got != " M README.md" {
		t.Errorf("git status = %q, want the unstaged file left", got)
	}
	if !strings.Contains(text(d), "Message…") {
		t.Errorf("message box not cleared:%s", dump(d))
	}
	if !strings.Contains(text(d), "Committed") {
		t.Errorf("no Committed toast:%s", dump(d))
	}
	// Newest commit first in the graph.
	g := section(d, "GRAPH")
	if len(g) < 2 || !strings.Contains(g[0], "add a line") || !strings.Contains(g[1], "init") {
		t.Errorf("graph order: %q", g)
	}
}

// Spec: Given a staged file, when the user presses Enter (or the Commit
// button) with an empty message box, then no commit is made, a warning
// says to enter a message and the box gets the focus.
func TestSCMCommitEmptyMessageRefused(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// staged\n")
		r.git("add", "main.go")
	})
	d.WaitForText("Commit (1)", 3*time.Second)
	before := r.git("rev-parse", "HEAD")
	scmClickText(d, "Commit (1)")
	if !strings.Contains(text(d), "Enter a commit message") {
		t.Errorf("no warning for an empty message:%s", dump(d))
	}
	// The box has the focus now: typed keys go into it, not to the list.
	d.Type("   ")
	d.Key("enter")
	if got := r.git("rev-parse", "HEAD"); got != before {
		t.Errorf("a commit was made with a blank message")
	}
	if got := r.porcelain(); got != "M  main.go" {
		t.Errorf("git status = %q, want the staged file untouched", got)
	}
}

// Spec: Given only unstaged changes, when the user commits with a message,
// then a dialog offers to stage everything; Cancel is first and focused, so
// Enter makes no commit; "Stage All & Commit" (one step right) commits every
// change (also the untracked file).
func TestSCMCommitNothingStagedOffersStageAll(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// edit\n")
		r.write("notes.txt", "n\n")
	})
	d.WaitForText("notes.txt", 3*time.Second)
	before := r.git("rev-parse", "HEAD")
	typeCommitMessage(d, "chore: all")
	d.Key("enter")
	if !strings.Contains(text(d), "Stage all changes?") {
		t.Fatalf("no stage-all dialog:%s", dump(d))
	}
	d.Key("enter") // Cancel is focused (safe first)
	if got := r.git("rev-parse", "HEAD"); got != before {
		t.Fatalf("Cancel made a commit")
	}
	if !strings.Contains(text(d), "chore: all") {
		t.Errorf("Cancel lost the typed message:%s", dump(d))
	}
	scmClickText(d, "Commit")
	d.Key("right", "enter") // "Stage All & Commit"
	until(d, func() bool { return inSection(d, "GRAPH", "all") })
	if got := r.git("log", "-1", "--format=%s"); got != "chore: all" {
		t.Errorf("HEAD subject = %q", got)
	}
	if got := r.git("show", "--name-only", "--format=", "HEAD"); got != "main.go\nnotes.txt" {
		t.Errorf("commit files = %q", got)
	}
	if got := r.porcelain(); got != "" {
		t.Errorf("git status not clean: %q", got)
	}
}

// Spec: Given a clean tree, when the user commits with a message, then
// nothing happens except an info toast "Nothing to commit".
func TestSCMCommitCleanTree(t *testing.T) {
	d, r := scmDriver(t, nil)
	before := r.git("rev-parse", "HEAD")
	typeCommitMessage(d, "nothing")
	d.Key("enter")
	if !strings.Contains(text(d), "Nothing to commit") {
		t.Errorf("no 'Nothing to commit' toast:%s", dump(d))
	}
	if got := r.git("rev-parse", "HEAD"); got != before {
		t.Error("a commit was made on a clean tree")
	}
}

// Spec: Given a last commit and a newly staged file, when the user picks
// "Commit (Amend)" from the ⋯ menu with an empty box, then HEAD is
// replaced (same parent, same message) and now has the staged file too;
// with a message, the message is replaced.
func TestSCMCommitAmend(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("a.txt", "a\n")
		r.commit("feat: a")
		r.write("b.txt", "b\n")
		r.git("add", "b.txt")
	})
	d.WaitForText("b.txt", 3*time.Second)
	parent := r.git("rev-parse", "HEAD~1")
	count := r.git("rev-list", "--count", "HEAD")

	openMoreMenu(d)
	clickScreen(d, "Commit (Amend)")
	until(d, func() bool { return r.porcelain() == "" })
	if got := r.git("rev-parse", "HEAD~1"); got != parent {
		t.Errorf("amend changed the parent")
	}
	if got := r.git("rev-list", "--count", "HEAD"); got != count {
		t.Errorf("commit count %s, want %s (amend must not add a commit)", got, count)
	}
	if got := r.git("log", "-1", "--format=%s"); got != "feat: a" {
		t.Errorf("amend with an empty box changed the message to %q", got)
	}
	if got := r.git("show", "--name-only", "--format=", "HEAD"); got != "a.txt\nb.txt" {
		t.Errorf("amended commit files = %q", got)
	}

	typeCommitMessage(d, "feat: a and b")
	d.Key("esc")
	openMoreMenu(d)
	clickScreen(d, "Commit (Amend)")
	until(d, func() bool { return inSection(d, "GRAPH", "a and b") })
	if got := r.git("log", "-1", "--format=%s"); got != "feat: a and b" {
		t.Errorf("amend message = %q", got)
	}
	if got := r.git("rev-list", "--count", "HEAD"); got != count {
		t.Errorf("commit count %s, want %s", got, count)
	}
}

// Spec: Given a staged file, when the user picks "Commit (Sign-off)", then
// the commit message ends with a Signed-off-by trailer for the user.
func TestSCMCommitSignoff(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// s\n")
		r.git("add", "main.go")
	})
	d.WaitForText("main.go", 3*time.Second)
	typeCommitMessage(d, "fix: signed")
	d.Key("esc")
	openMoreMenu(d)
	clickScreen(d, "Commit (Sign-off)")
	until(d, func() bool { return r.porcelain() == "" })
	if got := r.git("log", "-1", "--format=%B"); !strings.Contains(got, "fix: signed") || !strings.Contains(got, "Signed-off-by: Test <test@example.com>") {
		t.Errorf("commit message = %q", got)
	}
}

// Spec: Given a staged file, when the user commits from the "Commit…"
// palette prompt, then the commit has the typed message.
func TestSCMCommitFromPalettePrompt(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// p\n")
		r.git("add", "main.go")
	})
	d.WaitForText("main.go", 3*time.Second)
	d.Palette("git-commit")
	if !strings.Contains(text(d), "1 staged file") {
		t.Errorf("prompt does not say how many files are staged:%s", dump(d))
	}
	d.Type("docs: from prompt")
	d.Key("enter")
	until(d, func() bool { return r.porcelain() == "" })
	if got := r.git("log", "-1", "--format=%s"); got != "docs: from prompt" {
		t.Errorf("HEAD subject = %q", got)
	}
}

// openMoreMenu clicks the ⋯ (more) chip at the right of the action bar.
func openMoreMenu(d *Driver) {
	d.T.Helper()
	y := mustRow(d, "Commit")
	row := sidebar(d)[y]
	x := strings.LastIndex(row, "v") // ASCII icon mode: the chip shows "v"
	if x < 0 {
		d.T.Fatalf("no ⋯ chip on %q", row)
	}
	d.Click(len([]rune(row[:x])), y)
	if !strings.Contains(text(d), "Commit (Amend)") {
		d.T.Fatalf("⋯ menu did not open:%s", dump(d))
	}
}
