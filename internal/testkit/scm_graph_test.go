package testkit

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
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

// Spec (scm-graph design): Given a history with a tag, a side branch and
// a merge, when Source Control shows the GRAPH, then the commits are
// newest first (commit date), under a date group; HEAD's row shows the
// HEAD node and the branch, the tagged commit shows the tag, each row
// shows its age, a narrow sidebar hides the "fix:" prefix, and no
// row uses the old per-type rainbow (magenta merges, green HEAD).
func TestSCMGraphRows(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	g := section(d, "GRAPH")
	want := strings.Split(r.git("log", "--format=%s"), "\n")
	if strings.Join(want, ",") != "chore: after merge,Merge branch 'topic',docs: readme,fix: topic change,feat: add parser,init" {
		t.Fatalf("setup: unexpected git log order %q", want)
	}
	if len(g) != len(want) {
		t.Fatalf("GRAPH has %d commit rows, want %d:\n%s", len(g), len(want), strings.Join(sidebar(d), "\n"))
	}
	for i, w := range want {
		// The sidebar is narrow here: the "fix: " prefix is hidden.
		if _, rest, ok := strings.Cut(w, ": "); ok {
			w = rest
		}
		if !strings.Contains(g[i], w) {
			t.Errorf("GRAPH row %d = %q, want %q", i, g[i], w)
		}
	}
	if !strings.Contains(strings.Join(sidebar(d), "\n"), "This week") {
		t.Errorf("no date group:\n%s", strings.Join(sidebar(d), "\n"))
	}
	checks := []struct{ row, contains string }{
		{"after merge", "main"},   // HEAD -> main
		{"topic change", "topic"}, // branch label
		{"add parser", "v1.0"},    // tag label
		{"after merge", "3d"},     // ages: 67h and 72h before scmNow
		{"init", "3d"},
		{"after merge", theme.GlyphHead.String() + " "},   // the HEAD node
		{"Merge branch", theme.GlyphMerge.String() + " "}, // a merge node
		{"init", theme.GlyphCommit.String() + " "},        // a plain node
	}
	for _, c := range checks {
		y := mustRow(d, c.row)
		row := strings.ReplaceAll(sidebar(d)[y], c.row, "")
		if !strings.Contains(row, c.contains) {
			t.Errorf("row %q does not show %q: %q", c.row, c.contains, sidebar(d)[y])
		}
	}
	if y := mustRow(d, "Merge branch"); hasFg(d, y, theme.AccentMagenta) {
		t.Errorf("merge row still in the old merge colour: %q", rawRow(d, y))
	}
	if y := mustRow(d, "after merge"); !hasFg(d, y, theme.BorderFocus) {
		t.Errorf("HEAD node not in the accent: %q", rawRow(d, y))
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

// Spec (scm-graph §2.2, §2.7): Given 20 commits, GRAPH shows 8 and a
// "Show 15 more" row; a click on it shows the rest; a click on a date
// group folds it and a second click opens it again.
func TestSCMGraphShowMoreAndFoldByClick(t *testing.T) {
	d, _ := scmDriver(t, func(r *repo) {
		for i := 1; i <= 19; i++ {
			r.commit("change " + itoa2(i))
		}
	})
	if n := len(section(d, "GRAPH")); n != 8 {
		t.Fatalf("GRAPH shows %d commits, want 8:\n%s", n, strings.Join(sidebar(d), "\n"))
	}
	y := mustRow(d, "Show 15 more")
	d.Click(colOf(d, y, "Show 15 more"), y)
	if !until(d, func() bool { return len(section(d, "GRAPH")) == 20 }) {
		t.Fatalf("after Show more: %d commits:\n%s", len(section(d, "GRAPH")), strings.Join(sidebar(d), "\n"))
	}
	if inSection(d, "GRAPH", "Show 15 more") {
		t.Error("Show more still offered with every commit on show")
	}
	y = mustRow(d, "This week")
	d.Click(colOf(d, y, "This week"), y)
	if !until(d, func() bool { return !inSection(d, "GRAPH", "change 19") }) {
		t.Fatalf("click on the group did not fold it:\n%s", strings.Join(sidebar(d), "\n"))
	}
	y = mustRow(d, "This week")
	d.Click(colOf(d, y, "This week"), y)
	if !until(d, func() bool { return inSection(d, "GRAPH", "change 19") }) {
		t.Fatalf("second click (y=%d) did not unfold:\n%s", y, strings.Join(sidebar(d), "\n"))
	}
}

// itoa2 is a two-digit number ("01").
func itoa2(i int) string { return string(rune('0'+i/10)) + string(rune('0'+i%10)) }

// Spec: Given main tracking origin and one local commit, the newest row
// shows the unpushed mark; after a push it is gone.
func TestSCMGraphUnpushedMark(t *testing.T) {
	var bare string
	d, r := scmDriver(t, func(r *repo) {
		bare = r.addOrigin()
		r.write("local.txt", "x\n")
		r.commit("local work")
	})
	_ = bare
	mark := theme.GlyphUnpushed.String()
	if row := sidebar(d)[mustRow(d, "local work")]; !strings.Contains(row, mark) {
		t.Fatalf("no unpushed mark: %q", row)
	}
	if row := sidebar(d)[mustRow(d, "init")]; strings.Contains(row, mark) {
		t.Errorf("pushed commit marked: %q", row)
	}
	r.git("push", "-q")
	d.Action("palette:git-refresh")
	if !until(d, func() bool { return !strings.Contains(sidebar(d)[mustRow(d, "local work")], mark) }) {
		t.Errorf("mark stays after push: %q", sidebar(d)[mustRow(d, "local work")])
	}
}

// sinkClipboard catches copies for the rest of the test.
func sinkClipboard(t *testing.T) *fakeClipboard {
	clip := &fakeClipboard{}
	t.Cleanup(app.SetClipboardSink(func(s string) {
		clip.mu.Lock()
		clip.last, clip.n = s, clip.n+1
		clip.mu.Unlock()
	}))
	return clip
}

// Spec (scm-graph §2.7): with the cursor on a GRAPH commit, y copies the
// full SHA and a toast names the short one; the right-click menu also
// offers Copy Commit Message.
func TestSCMCopyShaAndMessage(t *testing.T) {
	clip := sinkClipboard(t)
	d, r := scmDriver(t, graphRepo)
	y := mustRow(d, "add parser")
	d.Click(colOf(d, y, "add parser"), y)
	d.Key("y")
	full := r.git("rev-parse", "v1.0")
	if !until(d, func() bool { got, _ := clip.get(); return got == full }) {
		got, _ := clip.get()
		t.Fatalf("y copied %q, want %q", got, full)
	}
	if short := r.git("rev-parse", "--short", "v1.0"); !until(d, func() bool { return strings.Contains(text(d), "Copied "+short) }) {
		t.Errorf("no toast for the copy:%s", dump(d))
	}
	y = mustRow(d, "add parser")
	d.RightClick(colOf(d, y, "add parser"), y)
	if !until(d, func() bool { return strings.Contains(text(d), "Copy Commit Message") }) {
		t.Fatalf("menu lacks Copy Commit Message:%s", dump(d))
	}
	clickText(d, "Copy Commit Message")
	if !until(d, func() bool { got, _ := clip.get(); return strings.HasPrefix(got, "feat: add parser\n\nThe parser reads") }) {
		got, _ := clip.get()
		t.Errorf("message copied = %q", got)
	}
}
