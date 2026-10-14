package textdiff

import (
	"fmt"
	"strings"
	"testing"
)

// Ported from mobocode test/text_diff_hunks_test.dart and
// test/diff_viewer_test.dart, plus the word-level and edge cases.

func all(d *Diff) map[int]bool {
	m := map[int]bool{}
	for _, h := range d.Hunks {
		m[h.Index] = true
	}
	if len(d.Hunks) == 0 {
		m[0] = true
	}
	return m
}

func TestDetectsAddedAndRemovedLines(t *testing.T) {
	d := Compute("a\nb\nc\n", "a\nB\nc\nd\n")
	if !d.HasChanges() || d.Added != 2 || d.Removed != 1 {
		t.Fatalf("added %d removed %d", d.Added, d.Removed)
	}
	var kinds []string
	for _, r := range d.Rows {
		kinds = append(kinds, fmt.Sprintf("%d:%s", r.Kind, r.Text))
	}
	want := "0:a 2:b 1:B 0:c 1:d"
	if got := strings.Join(kinds, " "); got != want {
		t.Errorf("rows %q, want %q", got, want)
	}
	if !d.Rows[1].Paired || !d.Rows[2].Paired || d.Rows[4].Paired {
		t.Errorf("pairing wrong: %+v", d.Rows)
	}
	if d.Rows[1].OldLine != 2 || d.Rows[1].NewLine != 0 || d.Rows[2].NewLine != 2 || d.Rows[4].NewLine != 4 {
		t.Errorf("line numbers wrong: %+v", d.Rows)
	}
}

func TestIdenticalTextHasNoChanges(t *testing.T) {
	d := Compute("x\ny\n", "x\ny")
	if d.HasChanges() {
		t.Fatal("changes reported")
	}
	if !d.EOLOnly() {
		t.Error("the missing final newline is not reported")
	}
	if d := Compute("x\n", "x\n"); d.HasChanges() || d.EOLOnly() {
		t.Error("same text reported as changed")
	}
}

func TestHunksSplitByContext(t *testing.T) {
	// a b c d e → a B c d E: two single-line hunks split by "c d".
	d := Compute("a\nb\nc\nd\ne\n", "a\nB\nc\nd\nE\n")
	if len(d.Hunks) != 2 {
		t.Fatalf("hunks %+v", d.Hunks)
	}
	h0, h1 := d.Hunks[0], d.Hunks[1]
	if h0.Index != 0 || h1.Index != 1 {
		t.Errorf("indices %d %d", h0.Index, h1.Index)
	}
	if h0.OldStart != 1 || h0.OldCount != 1 || h0.NewStart != 1 || h0.NewCount != 1 {
		t.Errorf("hunk 0 %+v", h0)
	}
	if h1.OldStart != 4 || h1.NewStart != 4 {
		t.Errorf("hunk 1 %+v", h1)
	}
	if d.Rows[h0.First].Text != "b" || h0.End-h0.First != 2 {
		t.Errorf("hunk 0 rows %d..%d", h0.First, h0.End)
	}
}

func TestPureInsertIsOwnHunk(t *testing.T) {
	d := Compute("a\nb\n", "a\nx\nb\n")
	if len(d.Hunks) != 1 || d.Hunks[0].OldCount != 0 || d.Hunks[0].NewCount != 1 {
		t.Fatalf("hunks %+v", d.Hunks)
	}
}

func TestApplyHunks(t *testing.T) {
	const o, n = "a\nb\nc\nd\ne\n", "a\nB\nc\nd\nE\n"
	if got := Apply(o, n, nil); got != o {
		t.Errorf("none: %q", got)
	}
	if got := Apply(o, n, all(Compute(o, n))); got != n {
		t.Errorf("all: %q", got)
	}
	if got := Apply(o, n, map[int]bool{0: true}); got != "a\nB\nc\nd\ne\n" {
		t.Errorf("hunk 0: %q", got)
	}
	if got := Apply(o, n, map[int]bool{1: true}); got != "a\nb\nc\nd\nE\n" {
		t.Errorf("hunk 1: %q", got)
	}
}

func TestApplyInsertAndDeleteHunks(t *testing.T) {
	const o, n = "keep1\ndropme\nkeep2\n", "keep1\nkeep2\nadded\n"
	d := Compute(o, n)
	if got := d.Apply(nil); got != o {
		t.Errorf("none: %q", got)
	}
	if got := d.Apply(all(d)); got != n {
		t.Errorf("all: %q", got)
	}
	if len(d.Hunks) != 2 {
		t.Fatalf("hunks %+v", d.Hunks)
	}
	if got := d.Apply(map[int]bool{1: true}); got != "keep1\ndropme\nkeep2\nadded\n" {
		t.Errorf("only the insert: %q", got)
	}
}

func TestApplyNoTrailingNewline(t *testing.T) {
	const o, n = "x\ny", "x\nY"
	if got := Apply(o, n, nil); got != o {
		t.Errorf("none: %q", got)
	}
	if got := Apply(o, n, all(Compute(o, n))); got != n {
		t.Errorf("all: %q", got)
	}
}

func TestApplyNoChanges(t *testing.T) {
	if got := Apply("same\n", "same\n", map[int]bool{0: true}); got != "same\n" {
		t.Errorf("%q", got)
	}
}

func TestApplyEOLOnly(t *testing.T) {
	if got := Apply("a\n", "a", map[int]bool{0: true}); got != "a" {
		t.Errorf("accepted: %q", got)
	}
	if got := Apply("a\n", "a", nil); got != "a\n" {
		t.Errorf("rejected: %q", got)
	}
}

func TestApplyNewAndDeletedFile(t *testing.T) {
	if got := Apply("", "a\nb\n", map[int]bool{0: true}); got != "a\nb\n" {
		t.Errorf("new file: %q", got)
	}
	if got := Apply("", "a\nb\n", nil); got != "" {
		t.Errorf("new file rejected: %q", got)
	}
	if got := Apply("a\n", "", map[int]bool{0: true}); got != "" {
		t.Errorf("emptied: %q", got)
	}
}

// Every subset of hunks rebuilds a file whose diff to old is exactly
// those hunks.
func TestApplyEverySubset(t *testing.T) {
	o := "1\n2\n3\n4\n5\n6\n7\n8\n9\n"
	n := "1\nTWO\n3\n4\n4.5\n5\n6\n8\n9\nten\n"
	d := Compute(o, n)
	k := len(d.Hunks)
	if k != 4 {
		t.Fatalf("hunks %+v", d.Hunks)
	}
	for mask := 0; mask < 1<<k; mask++ {
		acc := map[int]bool{}
		for i := 0; i < k; i++ {
			if mask&(1<<i) != 0 {
				acc[i] = true
			}
		}
		got := d.Apply(acc)
		g := Compute(o, got)
		if len(g.Hunks) != len(acc) {
			t.Errorf("mask %b: %d hunks in result %q", mask, len(g.Hunks), got)
		}
	}
}

func TestWordLevelSpans(t *testing.T) {
	d := Compute("\tfmt.Println(\"hi\", name)\n", "\tfmt.Println(\"hello\", name)\n")
	if len(d.Rows) != 2 {
		t.Fatalf("rows %+v", d.Rows)
	}
	spans := func(ss []Span) string {
		var b strings.Builder
		for _, s := range ss {
			if s.Changed {
				b.WriteString("[" + s.Text + "]")
			} else {
				b.WriteString(s.Text)
			}
		}
		return b.String()
	}
	if got := spans(d.Rows[0].Spans); got != "\tfmt.Println(\"[hi]\", name)" {
		t.Errorf("old spans %q", got)
	}
	if got := spans(d.Rows[1].Spans); got != "\tfmt.Println(\"[hello]\", name)" {
		t.Errorf("new spans %q", got)
	}
}

func TestTokenize(t *testing.T) {
	got := strings.Join(Tokenize("foo_bar(x, 1)  é"), "|")
	if want := "foo_bar|(|x|,| |1|)|  |é"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}

// A huge, fully different input must not build the n·m table: the
// fallback is "delete all, insert all".
func TestHugeInputFallsBackToLinear(t *testing.T) {
	var a, b []string
	for i := 0; i < 4000; i++ {
		a = append(a, fmt.Sprintf("old line %d", i))
		b = append(b, fmt.Sprintf("new line %d", i))
	}
	d := Compute(strings.Join(a, "\n"), strings.Join(b, "\n"))
	if d.Added != 4000 || d.Removed != 4000 || len(d.Hunks) != 1 {
		t.Fatalf("added %d removed %d hunks %d", d.Added, d.Removed, len(d.Hunks))
	}
	if d.Rows[0].Kind != Removed || !d.Rows[0].Paired {
		t.Errorf("first row %+v", d.Rows[0])
	}
}

func TestLCSCapIsAllocationBound(t *testing.T) {
	a := make([]string, 2000)
	b := make([]string, 2000)
	for i := range a {
		a[i], b[i] = fmt.Sprint("a", i), fmt.Sprint("b", i)
	}
	if n := testing.AllocsPerRun(3, func() { lcsDiff(a, b) }); n > 2 {
		t.Errorf("fallback allocated %.0f times", n)
	}
}
