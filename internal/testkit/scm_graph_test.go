package testkit

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Source Control: the GRAPH section and the commit hover card.

// graphRepo builds (commit dates after scmEpoch on the right):
//
//	o   chore: after merge     (HEAD -> main)        +5h
//	o   Merge branch 'topic'                         +4h
//	|\
//	| o fix: topic change      (topic)    1 file     +2h
//	o | docs: readme                                 +3h
//	|/
//	o   feat: add parser       (tag: v1.0) 2 files   +1h
//	o   init                                         +0h
func graphRepo(r *repo) {
	r.write("parser.go", "package main\n\nfunc parse() {}\n")
	r.write("parser_test.go", "package main\n")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "feat: add parser\n\nThe parser reads the config file.\n\nIt keeps comments.")
	r.git("tag", "v1.0")
	r.clock = scmEpoch.Add(2 * time.Hour)
	r.git("checkout", "-q", "-b", "topic")
	r.write("topic.txt", "one\ntwo\n")
	r.commit("fix: topic change")
	r.git("checkout", "-q", "main")
	r.clock = scmEpoch.Add(3 * time.Hour)
	r.write("README.md", "# Demo\n\nMore.\n")
	r.commit("docs: readme")
	r.clock = scmEpoch.Add(4 * time.Hour)
	r.git("merge", "-q", "--no-ff", "-m", "Merge branch 'topic'", "topic")
	r.clock = scmEpoch.Add(5 * time.Hour)
	r.write("main.go", "package main\n\nfunc main() { parse() }\n")
	r.commit("chore: after merge")
}

// Spec: Given a history with a tag, a side branch and a merge, when Source
// Control shows the GRAPH, then the commits are newest first in git's
// topological order, HEAD's row shows the branch pill, the tagged commit
// shows the tag pill, each row shows its age, and the merge row is drawn
// in the merge colour.
func TestSCMGraphRows(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	g := section(d, "GRAPH")
	// The same order as git log --graph (the merged side branch first).
	want := strings.Split(r.git("log", "--graph", "--format=%s", "--", "."), "\n")
	want = slices.DeleteFunc(want, func(s string) bool { return !strings.ContainsAny(s, "abcdefghijklmnopqrstuvwxyz") })
	for i, s := range want {
		want[i] = strings.TrimLeft(s, "*|\\/ ")
		want[i] = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(want[i], "feat: "), "fix: "), "docs: ")
		want[i] = strings.TrimPrefix(want[i], "chore: ")
	}
	if strings.Join(want, ",") != "after merge,Merge branch 'topic',topic change,readme,add parser,init" {
		t.Fatalf("setup: unexpected git log order %q", want)
	}
	if len(g) != len(want) {
		t.Fatalf("GRAPH has %d rows, want %d:\n%s", len(g), len(want), strings.Join(g, "\n"))
	}
	for i, w := range want {
		if !strings.Contains(g[i], w) {
			t.Errorf("GRAPH row %d = %q, want %q", i, g[i], w)
		}
	}
	checks := []struct{ row, contains string }{
		{"after merge", "main"},   // HEAD -> main
		{"topic change", "topic"}, // branch label
		{"add parser", "v1.0"},    // tag label
		{"after merge", "3d"},     // ages: 67h and 72h before scmNow
		{"init", "3d"},
	}
	for _, c := range checks {
		y := mustRow(d, c.row)
		row := strings.ReplaceAll(sidebar(d)[y], c.row, "")
		if !strings.Contains(row, c.contains) {
			t.Errorf("row %q does not show %q: %q", c.row, c.contains, sidebar(d)[y])
		}
	}
	if y := mustRow(d, "Merge branch"); !hasFg(d, y, theme.AccentMagenta) {
		t.Errorf("merge row not in the merge colour: %q", rawRow(d, y))
	}
	if y := mustRow(d, "after merge"); !hasFg(d, y, theme.AccentGreen) {
		t.Errorf("HEAD row not in the HEAD colour: %q", rawRow(d, y))
	}
}

// hoverCard moves the pointer onto the graph row showing subject and
// returns the frame text once the card (which shows the short hash) is up.
func hoverCard(t *testing.T, d *Driver, r *repo, subject, rev string) string {
	t.Helper()
	y := mustRow(d, subject)
	d.Move(colOf(d, y, subject), y)
	hash := r.git("rev-parse", "--short", rev)
	if !until(d, func() bool { return strings.Count(text(d), hash) > 0 && strings.Contains(text(d), "╭") }) {
		t.Fatalf("no hover card for %s:%s", subject, dump(d))
	}
	return text(d)
}

// Spec: Given a commit with a body that touched 2 files, when the pointer
// rests on its GRAPH row, then a card shows the author, the relative and
// the absolute date, the subject, the body, "+N −M  2 files" and the
// short hash; when the pointer leaves the panel, the card goes away.
func TestSCMHoverCardContent(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	card := hoverCard(t, d, r, "add parser", "v1.0")
	for _, want := range []string{
		"Test  ·  3 days ago",         // author · relative (71h before scmNow)
		"2026-03-01 10:00",            // absolute, UTC
		"feat: add parser",            // full subject, type prefix kept
		"The parser reads the config", // body
		"It keeps comments.",
		"+4 −0  2 files",
		r.git("rev-parse", "--short", "v1.0"),
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:%s", want, dump(d))
		}
	}
	d.Move(100, 30) // over the editor
	if strings.Contains(text(d), "The parser reads the config") {
		t.Errorf("card still shown after the pointer left:%s", dump(d))
	}
}

// Spec: Given a commit that touched one file, when hovered, then the card
// says "1 file" (singular).
func TestSCMHoverCardOneFile(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	card := hoverCard(t, d, r, "topic change", "topic")
	if !strings.Contains(card, "+2 −0  1 file") {
		t.Errorf("card does not say '+2 −0  1 file':%s", dump(d))
	}
	if strings.Contains(card, "1 files") {
		t.Errorf("card says '1 files':%s", dump(d))
	}
}

// Spec: Given a merge commit that brought in one changed file, when it is
// hovered, then the card counts that file (the change against the first
// parent), not "0 files".
func TestSCMHoverCardMergeCommit(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	card := hoverCard(t, d, r, "Merge branch", "HEAD~1")
	if !strings.Contains(card, "+2 −0  1 file") {
		t.Errorf("merge card does not count the merged file:%s", dump(d))
	}
}
