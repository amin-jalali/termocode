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
	for _, want := range []string{"3 commits → 2 commits", "drop 1", "git reset --hard ORIG_HEAD"} {
		if !strings.Contains(s, want) {
			t.Errorf("confirm lacks %q:%s", want, dump(d))
		}
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
