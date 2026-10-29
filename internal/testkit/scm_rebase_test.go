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
