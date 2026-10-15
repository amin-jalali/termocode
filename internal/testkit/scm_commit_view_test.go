package testkit

import (
	"strings"
	"testing"
)

// The commit view (scm-graph design §3): a click on a GRAPH commit opens
// the diff card in commit mode, docked over the editor when it is wide.

// openCommitView clicks the GRAPH row showing subject and waits for the
// card's title with rev's short SHA.
func openCommitView(t *testing.T, d *Driver, r *repo, subject, rev string) string {
	t.Helper()
	scmClickText(d, subject)
	short := r.git("rev-parse", "--short", rev)
	if !until(d, func() bool { return strings.Contains(text(d), "Commit "+short) }) {
		t.Fatalf("commit view for %s did not open:%s", subject, dump(d))
	}
	return short
}

// Spec (§3.2, §3.3): Given a commit that added two files, when its GRAPH
// row is clicked, then a card shows "Commit <sha>", the subject, the
// author (no e-mail), "2 files +4 −0", both files with their counts and
// the first file's lines; the sidebar stays visible next to it.
func TestSCMCommitViewOpens(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	openCommitView(t, d, r, "add parser", "v1.0")
	if !until(d, func() bool { return strings.Contains(text(d), "func parse() {}") }) {
		t.Fatalf("first file's diff not shown:%s", dump(d))
	}
	s := text(d)
	for _, want := range []string{"feat: add parser", "The parser reads the config", "Test", "2 files", "+4 −0",
		"parser.go", "parser_test.go", "+3 −0", "+1 −0", "(this commit)"} {
		if !strings.Contains(s, want) {
			t.Errorf("commit view lacks %q:%s", want, dump(d))
		}
	}
	if strings.Contains(s, "test@example.com") {
		t.Errorf("the e-mail is on screen:%s", dump(d))
	}
	// Docked: the GRAPH rows are still there, left of the card.
	y := mustRow(d, "after merge")
	if x := colOf(d, y, "after merge"); x < 0 {
		t.Errorf("sidebar covered:%s", dump(d))
	}
	if y, x := titleAt(d); x < d.Model().ExplorerWidth() {
		t.Errorf("card title at column %d (row %d), want right of the sidebar:%s", x, y, dump(d))
	}
	checkHits(t, d, "commit view docked")
}

// titleAt is the screen cell of the card's "Commit " title.
func titleAt(d *Driver) (int, int) {
	for y, l := range strings.Split(text(d), "\n") {
		if x := strings.Index(l, "Commit "); x >= 0 && strings.Contains(l, "Unified") {
			return y, colOf(d, y, "Commit ")
		}
	}
	return -1, -1
}

// Spec (§3.1, §4.4): with the card docked, j in GRAPH moves the cursor
// and the card follows to that commit.
func TestSCMCommitViewFollowsCursor(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	openCommitView(t, d, r, "add parser", "v1.0")
	d.Key("j") // the next row down is "init"
	init := r.git("rev-parse", "--short", "v1.0~1")
	if !until(d, func() bool { return strings.Contains(text(d), "Commit "+init) }) {
		t.Fatalf("card did not follow the cursor to init:%s", dump(d))
	}
}

// Spec (§3.1): a click on another GRAPH row while the card is docked
// shows that commit (the sidebar keeps its clicks).
func TestSCMCommitViewDockKeepsSidebar(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	openCommitView(t, d, r, "add parser", "v1.0")
	openCommitView(t, d, r, "after merge", "HEAD")
	if !until(d, func() bool { return strings.Contains(text(d), "parse()") && strings.Contains(text(d), "main.go") }) {
		t.Errorf("HEAD's change not shown:%s", dump(d))
	}
}

// Spec (§3.3): Given a commit with two files, when the second file is
// clicked in the card's list, then its diff loads (file 2 of 2).
func TestSCMCommitViewLazyFile(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	openCommitView(t, d, r, "add parser", "v1.0")
	clickScreen(d, "parser_test.go")
	if !until(d, func() bool {
		s := text(d)
		return strings.Contains(s, "parser_test.go · file 2 of 2") && !strings.Contains(s, "Loading…") && strings.Contains(s, "+1 −0 · 1 change")
	}) {
		t.Fatalf("second file did not load:%s", dump(d))
	}
}

// Spec (§3.7): at 100x30 the editor is too narrow to dock: the card
// floats over the whole screen.
func TestSCMCommitViewFloatsAt100x30(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	d.Resize(100, 30)
	openCommitView(t, d, r, "add parser", "v1.0")
	if _, x := titleAt(d); x < 0 || x > 6 {
		t.Errorf("floating card title at column %d, want near the left edge:%s", x, dump(d))
	}
	d.Key("esc")
	if !until(d, func() bool { return !strings.Contains(text(d), "(this commit)") }) {
		t.Errorf("esc did not close the floating card:%s", dump(d))
	}
}

// Spec (§2.7): Esc in the sidebar closes the docked card.
func TestSCMCommitViewEsc(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	openCommitView(t, d, r, "add parser", "v1.0")
	d.Key("esc")
	if !until(d, func() bool { return !strings.Contains(text(d), "(this commit)") }) {
		t.Errorf("esc did not close the card:%s", dump(d))
	}
}
