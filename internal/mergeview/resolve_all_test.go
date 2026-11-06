package mergeview

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/git"
)

// GITX-1 (I10): ResolveAll takes one side for every conflict of the file
// and keeps the lines git merged on its own.
func TestResolveAll(t *testing.T) {
	m, _ := twoBlocks()
	if got := m.ResolveAll(git.AcceptOurs); !reflect.DeepEqual(got, lines("a X1 m1 M2 m3 b Y1 c")) {
		t.Errorf("all ours %q", got)
	}
	if got := m.ResolveAll(git.AcceptTheirs); !reflect.DeepEqual(got, lines("a X2 m1 M2 m3 b Y2 Y2b c")) {
		t.Errorf("all theirs %q", got)
	}
	var none Model
	if none.ResolveAll(git.AcceptOurs) != nil {
		t.Error("no conflicts: want nil")
	}
}

// GITX-34 (I9, I13): ResolveSimple settles the conflicts where only one
// side changed or both made the same change; with ignoreSpace a change
// that is only spaces counts as the same. The others stay.
func TestResolveSimple(t *testing.T) {
	st := git.ConflictStages{
		Base:   git.ConflictSide{Present: true, Text: "a\nx\nm\ny\nn\nz\nb\n"},
		Ours:   git.ConflictSide{Present: true, Text: "a\nx\nm\nY1\nn\nz  w\nb\n"},
		Theirs: git.ConflictSide{Present: true, Text: "a\nX2\nm\nY2\nn\nz w\nb\n"},
	}
	res := []string{"a",
		"<<<<<<< HEAD", "x", "=======", "X2", ">>>>>>> feature", "m",
		"<<<<<<< HEAD", "Y1", "=======", "Y2", ">>>>>>> feature", "n",
		"<<<<<<< HEAD", "z  w", "=======", "z w", ">>>>>>> feature", "b"}
	m := New("f.txt", st)
	m.SetResult(res)
	got, n := m.ResolveSimple(false)
	if n != 1 || strings.Join(got, ",") != "a,X2,m,<<<<<<< HEAD,Y1,=======,Y2,>>>>>>> feature,n,<<<<<<< HEAD,z  w,=======,z w,>>>>>>> feature,b" {
		t.Fatalf("simple: %d %q", n, got)
	}
	got, n = m.ResolveSimple(true)
	if n != 2 || strings.Join(got, ",") != "a,X2,m,<<<<<<< HEAD,Y1,=======,Y2,>>>>>>> feature,n,z  w,b" {
		t.Fatalf("simple, ignoring spaces: %d %q", n, got)
	}
}

// GITX-34 (I14): in the current conflict, the words that differ between
// ours and theirs are underlined.
func TestViewWordHighlight(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	st := git.ConflictStages{
		Base:   git.ConflictSide{Present: true, Text: "a\nlet x = 1\nb\n"},
		Ours:   git.ConflictSide{Present: true, Text: "a\nlet x = 2\nb\n"},
		Theirs: git.ConflictSide{Present: true, Text: "a\nlet x = 3\nb\n"},
	}
	m := New("f.txt", st)
	m.SetResult([]string{"a", "<<<<<<< HEAD", "let x = 2", "=======", "let x = 3", ">>>>>>> feature", "b"})
	m.SetSize(120, 10, false)
	var ours string
	for _, r := range m.renderPane(PaneOurs, 40, 6) {
		if strings.Contains(ansi.Strip(r), "let x = 2") {
			ours = r
		}
	}
	if !strings.Contains(ours, "\x1b[4") && !strings.Contains(ours, ";4m") && !strings.Contains(ours, ";4;") {
		t.Fatalf("no underline in %q", ours)
	}
	if w := ansi.StringWidth(ours); w != 40 {
		t.Errorf("width %d", w)
	}
}
