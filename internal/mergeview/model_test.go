package mergeview

import (
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/git"
)

func lines(s string) []string { return strings.Fields(s) }

func text(s string) string { return strings.Join(lines(s), "\n") + "\n" }

// twoBlocks: theirs also changed m2 (merged cleanly) and added a line to
// the second conflict.
func twoBlocks() (Model, []string) {
	st := git.ConflictStages{
		Base:   git.ConflictSide{Present: true, Text: text("a x m1 m2 m3 b y c")},
		Ours:   git.ConflictSide{Present: true, Text: text("a X1 m1 m2 m3 b Y1 c")},
		Theirs: git.ConflictSide{Present: true, Text: text("a X2 m1 M2 m3 b Y2 Y2b c")},
	}
	result := []string{"a", "<<<<<<< HEAD", "X1", "=======", "X2", ">>>>>>> feature",
		"m1", "M2", "m3", "b", "<<<<<<< HEAD", "Y1", "=======", "Y2", "Y2b", ">>>>>>> feature", "c"}
	m := New("f.txt", st)
	m.SetResult(result)
	return m, result
}

func TestBlockRangesInEachSide(t *testing.T) {
	m, _ := twoBlocks()
	bs := m.Blocks()
	if len(bs) != 2 {
		t.Fatalf("blocks = %d", len(bs))
	}
	want := [][3][2]int{
		{{1, 2}, {1, 2}, {1, 2}},
		{{6, 7}, {6, 7}, {6, 8}},
	}
	for i, b := range bs {
		if b.Ranges != want[i] {
			t.Errorf("block %d ranges = %v, want %v", i, b.Ranges, want[i])
		}
	}
	if m.Label(PaneOurs) != "HEAD" || m.Label(PaneTheirs) != "feature" {
		t.Errorf("labels %q %q", m.Label(PaneOurs), m.Label(PaneTheirs))
	}
}

func TestTakeEachWay(t *testing.T) {
	m, _ := twoBlocks()
	cases := map[git.Resolution][]string{
		git.AcceptOurs:            {"X1"},
		git.AcceptTheirs:          {"X2"},
		git.AcceptBoth:            {"X1", "X2"},
		git.AcceptBothTheirsFirst: {"X2", "X1"},
		git.AcceptBase:            {"x"},
	}
	for r, want := range cases {
		tk, ok := m.Take(r)
		if !ok || tk.Start != 1 || tk.End != 6 || !reflect.DeepEqual(tk.Lines, want) {
			t.Errorf("take %d = %+v %v, want %q", r, tk, ok, want)
		}
	}
	m.Next()
	if tk, _ := m.Take(git.AcceptBase); tk.Start != 10 || tk.End != 16 || !reflect.DeepEqual(tk.Lines, []string{"y"}) {
		t.Errorf("block 2 base = %+v", tk)
	}
}

func TestTakeThenResultShrinks(t *testing.T) {
	m, result := twoBlocks()
	m.Next()
	tk, _ := m.Take(git.AcceptTheirs)
	next := append(append(append([]string{}, result[:tk.Start]...), tk.Lines...), result[tk.End:]...)
	m.SetResult(next)
	if len(m.Blocks()) != 1 || m.Current() != 0 {
		t.Fatalf("blocks %d cur %d", len(m.Blocks()), m.Current())
	}
	if tk, _ := m.Take(git.AcceptOurs); !reflect.DeepEqual(tk.Lines, []string{"X1"}) {
		t.Errorf("left block = %+v", tk)
	}
	m.SetResult(lines("a X1 m1 M2 m3 b Y2 Y2b c"))
	if len(m.Blocks()) != 0 {
		t.Fatal("blocks left")
	}
	if _, ok := m.Take(git.AcceptOurs); ok {
		t.Error("take with no block")
	}
}

func TestDiff3BaseFromFile(t *testing.T) {
	st := git.ConflictStages{
		Base:   git.ConflictSide{Present: true, Text: text("a x c")},
		Ours:   git.ConflictSide{Present: true, Text: text("a o c")},
		Theirs: git.ConflictSide{Present: true, Text: text("a t c")},
	}
	m := New("f", st)
	m.SetResult([]string{"a", "<<<<<<< HEAD", "o", "||||||| base", "x", "=======", "t", ">>>>>>> b", "c"})
	b, _ := m.CurrentBlock()
	if b.Ranges[PaneBase] != [2]int{1, 2} || m.Label(PaneBase) != "base" {
		t.Errorf("base range %v label %q", b.Ranges[PaneBase], m.Label(PaneBase))
	}
	if tk, _ := m.Take(git.AcceptBase); !reflect.DeepEqual(tk.Lines, []string{"x"}) {
		t.Errorf("base = %q", tk.Lines)
	}
}

func TestNoBaseAddAdd(t *testing.T) {
	st := git.ConflictStages{
		Ours:   git.ConflictSide{Present: true, Text: "o\n"},
		Theirs: git.ConflictSide{Present: true, Text: "t\n"},
	}
	m := New("f", st)
	m.SetResult([]string{"<<<<<<< HEAD", "o", "=======", "t", ">>>>>>> b"})
	if tk, ok := m.Take(git.AcceptBase); !ok || len(tk.Lines) != 0 {
		t.Errorf("no base take = %+v", tk)
	}
}

func TestNavigation(t *testing.T) {
	m, _ := twoBlocks()
	m.Prev()
	if m.Current() != 1 {
		t.Errorf("prev wraps to %d", m.Current())
	}
	m.Next()
	if m.Current() != 0 {
		t.Errorf("next wraps to %d", m.Current())
	}
	m.SetCursorLine(8) // between the blocks: the next one
	if m.Current() != 1 {
		t.Errorf("cursor between = %d", m.Current())
	}
	m.SetCursorLine(0)
	if m.Current() != 0 {
		t.Errorf("cursor top = %d", m.Current())
	}
	m.SetCursorLine(99)
	if m.Current() != 1 {
		t.Errorf("cursor past end = %d", m.Current())
	}
}

func TestManyBlocks(t *testing.T) {
	var base, ours, theirs, result []string
	for i := 0; i < 200; i++ {
		base = append(base, "ctx", "b")
		ours = append(ours, "ctx", "o")
		theirs = append(theirs, "ctx", "t")
		result = append(result, "ctx", "<<<<<<< HEAD", "o", "=======", "t", ">>>>>>> x")
	}
	j := func(ls []string) string { return strings.Join(ls, "\n") + "\n" }
	m := New("f", git.ConflictStages{
		Base:   git.ConflictSide{Present: true, Text: j(base)},
		Ours:   git.ConflictSide{Present: true, Text: j(ours)},
		Theirs: git.ConflictSide{Present: true, Text: j(theirs)},
	})
	m.SetResult(result)
	if len(m.Blocks()) != 200 {
		t.Fatalf("blocks = %d", len(m.Blocks()))
	}
	for i, b := range m.Blocks() {
		want := [2]int{2*i + 1, 2*i + 2}
		if b.Ranges[PaneBase] != want || b.Ranges[PaneOurs] != want || b.Ranges[PaneTheirs] != want {
			t.Fatalf("block %d ranges %v", i, b.Ranges)
		}
	}
}

func TestCRLFStages(t *testing.T) {
	m := New("f", git.ConflictStages{
		Base:   git.ConflictSide{Present: true, Text: "a\r\nx\r\nc\r\n"},
		Ours:   git.ConflictSide{Present: true, Text: "a\r\no\r\nc\r\n"},
		Theirs: git.ConflictSide{Present: true, Text: "a\r\nt\r\nc"},
	})
	m.SetResult([]string{"a", "<<<<<<< HEAD", "o", "=======", "t", ">>>>>>> b", "c"})
	if tk, _ := m.Take(git.AcceptBase); !reflect.DeepEqual(tk.Lines, []string{"x"}) {
		t.Errorf("crlf base = %q", tk.Lines)
	}
}
