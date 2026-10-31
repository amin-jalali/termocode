//go:build unix

package testkit

import (
	"strings"
	"testing"
)

// Interactive rebase (G-RBS): the rebase card on GRAPH commits, git run
// with termocode as its editor (this test binary re-runs as the helper,
// see TestMain).

// rebaseRepo adds c1..c4, one file each.
func rebaseRepo(r *repo) {
	for _, n := range []string{"1", "2", "3", "4"} {
		r.write("f"+n+".txt", "file "+n+"\n")
		r.commit("c" + n)
	}
}

// openRebaseCard selects GRAPH commit subject and presses R.
func openRebaseCard(t *testing.T, d *Driver, subject string) {
	t.Helper()
	scmClickText(d, subject)
	d.Key("R")
	if !until(d, func() bool { return strings.Contains(text(d), "Interactive Rebase") }) {
		t.Fatalf("no rebase card:%s", dump(d))
	}
}

// Spec: R on a GRAPH commit opens the rebase card on that commit and
// every newer one, oldest first, all "pick", with the preview line; Esc
// closes it and nothing changes.
func TestSCMRebaseCardOpens(t *testing.T) {
	d, r := scmDriver(t, rebaseRepo)
	head := r.git("rev-parse", "HEAD")
	openRebaseCard(t, d, "c2")
	s := text(d)
	base := r.git("rev-parse", "--short=7", "HEAD~3")
	for _, want := range []string{"Interactive Rebase · main onto " + base, "3 commits → 3 commits"} {
		if !strings.Contains(s, want) {
			t.Errorf("card lacks %q:%s", want, dump(d))
		}
	}
	i2, i4 := strings.Index(s, "pick    "+r.git("rev-parse", "--short", "HEAD~2")), strings.Index(s, "pick    "+r.git("rev-parse", "--short", "HEAD"))
	if i2 < 0 || i4 < 0 || i2 > i4 {
		t.Errorf("rows not oldest first:%s", dump(d))
	}
	d.Key("esc")
	if !until(d, func() bool { return !strings.Contains(text(d), "Interactive Rebase") }) {
		t.Fatalf("Esc did not close the card:%s", dump(d))
	}
	if got := r.git("rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved")
	}
}

// Spec: the GRAPH commit menu has "Rebase from Here…".
func TestSCMRebaseFromMenu(t *testing.T) {
	d, _ := scmDriver(t, rebaseRepo)
	y := mustRow(d, "c3")
	d.RightClick(colOf(d, y, "c3"), y)
	if !until(d, func() bool { return strings.Contains(text(d), "Rebase from Here…") }) {
		t.Fatalf("no menu item:%s", dump(d))
	}
	clickScreen(d, "Rebase from Here…")
	if !until(d, func() bool { return strings.Contains(text(d), "2 commits → 2 commits") }) {
		t.Fatalf("no card for c3..HEAD:%s", dump(d))
	}
}

// Spec: r on a row marks it reword and asks for the new subject (filled
// with the old one); the row then shows the new subject.
func TestSCMRebaseRewordPrompt(t *testing.T) {
	d, _ := scmDriver(t, rebaseRepo)
	openRebaseCard(t, d, "c2")
	d.Key("r")
	if !until(d, func() bool { return strings.Contains(text(d), "New subject (the body stays)") }) {
		t.Fatalf("no reword prompt:%s", dump(d))
	}
	d.Key("backspace", "backspace")
	d.Type("better")
	d.Key("enter")
	if !until(d, func() bool { s := text(d); return strings.Contains(s, "reword") && strings.Contains(s, "better") }) {
		t.Fatalf("row not reworded:%s", dump(d))
	}
	if !strings.Contains(text(d), "Interactive Rebase") {
		t.Errorf("the card closed:%s", dump(d))
	}
}

// startRebase presses Enter on the card and waits for the confirm.
func startRebase(t *testing.T, d *Driver) {
	t.Helper()
	d.Key("enter")
	if !until(d, func() bool { return strings.Contains(text(d), "Start the rebase?") }) {
		t.Fatalf("no confirm:%s", dump(d))
	}
}

// Spec: Enter on the card asks first. The confirm lists what will happen
// and Enter alone picks Cancel (the card stays open, nothing runs).
// Start Rebase runs it: the dropped commit leaves GRAPH, the reworded one
// has its new subject, and the toast tells how to undo.
func TestSCMRebaseConfirmAndRun(t *testing.T) {
	d, r := scmDriver(t, rebaseRepo)
	head := r.git("rev-parse", "HEAD")
	openRebaseCard(t, d, "c2")
	d.Key("j", "d") // drop c3
	startRebase(t, d)
	s := text(d)
	for _, want := range []string{"3 commits → 2 commits", "drop 1", "git reset --keep ORIG_HEAD"} {
		if !strings.Contains(s, want) {
			t.Errorf("confirm lacks %q:%s", want, dump(d))
		}
	}
	if strings.Contains(s, "--hard") {
		t.Errorf("confirm names a hard reset, which loses work:%s", dump(d))
	}
	d.Key("enter") // Cancel is focused
	if !until(d, func() bool { return !strings.Contains(text(d), "Start the rebase?") }) {
		t.Fatalf("confirm still open:%s", dump(d))
	}
	if !strings.Contains(text(d), "Interactive Rebase") || r.git("rev-parse", "HEAD") != head {
		t.Fatalf("Cancel closed the card or ran the rebase:%s", dump(d))
	}
	d.Key("k", "r", "backspace", "backspace")
	d.Type("c2 better")
	d.Key("enter")
	startRebase(t, d)
	clickTarget(d, "confirm:rebase-start")
	if !until(d, func() bool { return strings.Contains(text(d), "Rebase done: 3 commits → 2 commits") }) {
		t.Fatalf("no done toast:%s", dump(d))
	}
	if s := text(d); !strings.Contains(s, "Undo: u / Git: Undo Last Git Action") || strings.Contains(s, "--hard") {
		t.Errorf("done toast must point to the safe undo:%s", dump(d))
	}
	if got := r.git("log", "--format=%s"); got != "c4\nc2 better\nc1\ninit" {
		t.Errorf("history:\n%s", got)
	}
	if r.exists("f3.txt") {
		t.Error("the dropped commit's file is still there")
	}
	if !until(d, func() bool { return inSection(d, "GRAPH", "c2 better") && !inSection(d, "GRAPH", "c3") }) {
		t.Errorf("GRAPH not refreshed:%s", dump(d))
	}
}

// Spec: when commits of the range are on the upstream already, the
// confirm warns that a force-push will be needed (termocode never does
// it); a range of new commits only has no warning.
func TestSCMRebasePushedWarning(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		rebaseRepo(r)
		r.addOrigin()
		r.write("f5.txt", "5\n")
		r.commit("c5")
	})
	openRebaseCard(t, d, "c3")
	startRebase(t, d)
	if s := text(d); !strings.Contains(s, "2 of these commits are on origin/main already; you") || !strings.Contains(s, "force-push") {
		t.Errorf("no warning:%s", dump(d))
	}
	d.Key("esc", "esc")
	if !until(d, func() bool { return !strings.Contains(text(d), "Interactive Rebase") }) {
		t.Fatalf("card still open:%s", dump(d))
	}
	openRebaseCard(t, d, "c5")
	startRebase(t, d)
	if strings.Contains(text(d), "force-push") {
		t.Errorf("warning for unpushed commits:%s", dump(d))
	}
	_ = r
}

// Spec: with uncommitted changes the confirm offers Stash and Continue
// (not Start); the rebase runs and the changes come back.
func TestSCMRebaseStashAndContinue(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		rebaseRepo(r)
		r.write("f1.txt", "edited\n")
	})
	openRebaseCard(t, d, "c2")
	d.Key("j", "d")
	startRebase(t, d)
	if s := text(d); !strings.Contains(s, "uncommitted changes") || strings.Contains(s, "Start Rebase") {
		t.Fatalf("confirm does not offer the stash:%s", dump(d))
	}
	clickTarget(d, "confirm:rebase-stash")
	if !until(d, func() bool { return strings.Contains(text(d), "Rebase done") }) {
		t.Fatalf("no done toast:%s", dump(d))
	}
	if got := r.git("log", "--format=%s"); got != "c4\nc2\nc1\ninit" {
		t.Errorf("history:\n%s", got)
	}
	if got := r.read("f1.txt"); got != "edited\n" {
		t.Errorf("the change did not come back: %q", got)
	}
}

// conflictRebase: c1 sets f.txt to x, c2 to a, c3 to b. The test swaps
// c2 and c3 in the card and starts: git stops on a conflict.
func conflictRebase(t *testing.T) (*Driver, *repo) {
	t.Helper()
	d, r := scmDriver(t, func(r *repo) {
		for i, body := range []string{"x\n", "a\n", "b\n"} {
			r.write("f.txt", body)
			r.commit("c" + string(rune('1'+i)))
		}
	})
	openRebaseCard(t, d, "c2")
	d.Key("J")
	startRebase(t, d)
	clickTarget(d, "confirm:rebase-start")
	if !until(d, func() bool { return strings.Contains(text(d), "Rebase stopped: 1 conflict") }) {
		t.Fatalf("no stop toast:%s", dump(d))
	}
	if !until(d, func() bool { return rowWith(d, "Rebasing 1/2") >= 0 && inSection(d, "CONFLICTS", "f.txt") }) {
		t.Fatalf("no banner or CONFLICTS:%s", dump(d))
	}
	return d, r
}

// openBanner clicks the rebase banner and waits for its card.
func openBanner(t *testing.T, d *Driver) {
	t.Helper()
	scmClickText(d, "Rebasing")
	if !until(d, func() bool { return strings.Contains(text(d), "Rebase in progress") }) {
		t.Fatalf("no rebase card:%s", dump(d))
	}
}

// Spec: a rebase stopped on a conflict shows a banner in Source Control
// and the file in CONFLICTS. The banner's card has Close first and
// focused: Enter and Esc change nothing (conflict fixes are kept). Abort
// puts the branch back and the banner goes.
func TestSCMRebaseConflictBannerAbort(t *testing.T) {
	d, r := conflictRebase(t)
	head := r.git("rev-parse", "ORIG_HEAD")
	stopped := r.git("rev-parse", "HEAD")
	openBanner(t, d)
	if s := text(d); !strings.Contains(s, "1 conflict: f.txt") || !strings.Contains(s, `"c3"`) {
		t.Errorf("card does not say where it stopped:%s", dump(d))
	}
	d.Key("enter") // Close
	d.Settle(0)
	if got := r.git("rev-parse", "HEAD"); got != stopped || rowWith(d, "Rebasing") < 0 {
		t.Fatalf("Enter changed the rebase (HEAD %s, want %s):%s", got, stopped, dump(d))
	}
	openBanner(t, d)
	d.Key("esc")
	d.Settle(0)
	if rowWith(d, "Rebasing") < 0 {
		t.Fatalf("Esc ended the rebase:%s", dump(d))
	}
	openBanner(t, d)
	clickTarget(d, "confirm:rebase-abort")
	if !until(d, func() bool { return strings.Contains(text(d), "Rebase aborted") }) {
		t.Fatalf("Abort did not abort:%s", dump(d))
	}
	if got := r.git("rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD %s, want %s", got, head)
	}
	if !until(d, func() bool { return rowWith(d, "Rebasing") < 0 }) {
		t.Errorf("banner still shown:%s", dump(d))
	}
}

// Spec: Continue refuses while the file has conflict markers; once fixed
// it goes on (here to the next conflict), and Skip drops that commit and
// finishes the rebase.
func TestSCMRebaseContinueAndSkip(t *testing.T) {
	d, r := conflictRebase(t)
	openBanner(t, d)
	clickTarget(d, "confirm:rebase-continue")
	if !until(d, func() bool { return strings.Contains(text(d), "Can't continue yet") }) {
		t.Fatalf("continue with markers:%s", dump(d))
	}
	r.write("f.txt", "b\n")
	openBanner(t, d)
	clickTarget(d, "confirm:rebase-continue")
	if !until(d, func() bool { return rowWith(d, "Rebasing 2/2") >= 0 }) {
		t.Fatalf("no second stop:%s", dump(d))
	}
	openBanner(t, d)
	clickTarget(d, "confirm:rebase-skip")
	if !until(d, func() bool { return strings.Contains(text(d), "Rebase done") && rowWith(d, "Rebasing") < 0 }) {
		t.Fatalf("skip did not finish:%s", dump(d))
	}
	if got := r.git("log", "--format=%s"); got != "c3\nc1\ninit" {
		t.Errorf("history:\n%s", got)
	}
}

// Spec: F on a GRAPH commit folds the staged changes into it after a
// confirm (Cancel first and focused); the history keeps its subjects.
// With unstaged changes too, the button is Stash and Fixup and they come
// back after.
func TestSCMFixupInto(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		rebaseRepo(r)
		r.write("f2.txt", "fixed 2\n")
		r.git("add", "f2.txt")
		r.write("f4.txt", "unstaged\n")
	})
	scmClickText(d, "c2")
	d.Key("F")
	if !until(d, func() bool { return strings.Contains(text(d), "Fixup into") }) {
		t.Fatalf("no fixup confirm:%s", dump(d))
	}
	if s := text(d); !strings.Contains(s, "Fold the staged changes (1 file)") || !strings.Contains(s, "Stash and Fixup") {
		t.Errorf("confirm text:%s", dump(d))
	}
	if s := text(d); !strings.Contains(s, "git reset --keep ORIG_HEAD") || strings.Contains(s, "--hard") {
		t.Errorf("fixup confirm must point to the safe undo:%s", dump(d))
	}
	head := r.git("rev-parse", "HEAD")
	d.Key("enter") // Cancel
	d.Settle(0)
	if r.git("rev-parse", "HEAD") != head {
		t.Fatal("Enter ran the fixup")
	}
	scmClickText(d, "c2")
	d.Key("F")
	if !until(d, func() bool { return strings.Contains(text(d), "Fixup into") }) {
		t.Fatalf("no fixup confirm:%s", dump(d))
	}
	clickTarget(d, "confirm:fixup-stash")
	if !until(d, func() bool { return strings.Contains(text(d), "staged changes folded into") }) {
		t.Fatalf("no done toast:%s", dump(d))
	}
	if got := r.git("log", "--format=%s"); got != "c4\nc3\nc2\nc1\ninit" {
		t.Errorf("history:\n%s", got)
	}
	if got := r.git("show", "HEAD~2:f2.txt"); got != "fixed 2" {
		t.Errorf("c2's f2.txt %q", got)
	}
	if got := r.read("f4.txt"); got != "unstaged\n" {
		t.Errorf("unstaged change lost: %q", got)
	}
}

// Spec: with nothing staged, F says so and runs nothing.
func TestSCMFixupNothingStaged(t *testing.T) {
	d, _ := scmDriver(t, rebaseRepo)
	scmClickText(d, "c2")
	d.Key("F")
	if !until(d, func() bool { return strings.Contains(text(d), "Nothing is staged") }) {
		t.Fatalf("no toast:%s", dump(d))
	}
}

// Spec: the palette's "Git: Interactive Rebase…" asks for the oldest
// commit to rebase (a list of HEAD's commits) and opens the card there.
func TestSCMRebaseFromPalette(t *testing.T) {
	d, _ := scmDriver(t, rebaseRepo)
	d.Palette("git-rebase")
	if !until(d, func() bool { return strings.Contains(text(d), "Rebase from which commit?") }) {
		t.Fatalf("no picker:%s", dump(d))
	}
	d.Type("c3")
	d.Key("enter")
	if !until(d, func() bool { return strings.Contains(text(d), "2 commits → 2 commits") }) {
		t.Fatalf("no card from c3:%s", dump(d))
	}
}

// Spec: "Git: Fixup Staged Changes Into…" lists commits and folds the
// staged changes into the chosen one; "Git: Rebase in Progress…" says
// when none is running.
func TestSCMFixupFromPalette(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		rebaseRepo(r)
		r.write("f3.txt", "fixed 3\n")
		r.git("add", "f3.txt")
	})
	d.Palette("git-rebase-status")
	if !until(d, func() bool { return strings.Contains(text(d), "No rebase in progress") }) {
		t.Fatalf("no toast:%s", dump(d))
	}
	d.Palette("git-fixup")
	// The picker cuts its long title to one row: match its start.
	if !until(d, func() bool { return strings.Contains(text(d), "Fixup the staged changes into") }) {
		t.Fatalf("no picker:%s", dump(d))
	}
	d.Type("c3")
	d.Key("enter")
	if !until(d, func() bool { return strings.Contains(text(d), "Fixup into") }) {
		t.Fatalf("no confirm:%s", dump(d))
	}
	clickTarget(d, "confirm:fixup")
	if !until(d, func() bool { return strings.Contains(text(d), "staged changes folded into") }) {
		t.Fatalf("no done toast:%s", dump(d))
	}
	if got := r.git("show", "HEAD~1:f3.txt"); got != "fixed 3" {
		t.Errorf("c3's f3.txt %q", got)
	}
}
