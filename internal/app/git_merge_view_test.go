package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/mergeview"
)

// mergeTestStages is a one-conflict file.
func mergeTestStages() git.ConflictStages {
	return git.ConflictStages{
		Path:   "f.txt",
		Base:   git.ConflictSide{Present: true, Text: "a\nbase\nc\n"},
		Ours:   git.ConflictSide{Present: true, Text: "a\nours\nc\n"},
		Theirs: git.ConflictSide{Present: true, Text: "a\ntheirs\nc\n"},
	}
}

var mergeTestResult = []string{"a", "<<<<<<< HEAD", "ours", "=======", "theirs", ">>>>>>> feature", "c"}

// mergeTestModel is a w×h window with the merge view open (no editor).
func mergeTestModel(t *testing.T, w, h int) Model {
	t.Helper()
	m0, _ := New().Update(tea.WindowSizeMsg{Width: w, Height: h})
	m := m0.(Model)
	v := mergeview.New("f.txt", mergeTestStages())
	v.SetResult(mergeTestResult)
	m.merge = &mergeViewState{open: true, view: v, rel: "f.txt"}
	m.applyLayout()
	return m
}

func TestMergeViewTakesRowsAboveTheEditor(t *testing.T) {
	for _, sz := range [][2]int{{150, 42}, {80, 24}} {
		m0, _ := New().Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		before := m0.(Model).editorPaneLayout()
		m := mergeTestModel(t, sz[0], sz[1])
		p := m.editorPaneLayout()
		if p.Merge < 5 || p.Top != before.Top+p.Merge || p.H != before.H-p.Merge || p.H < 4 {
			t.Errorf("%v: pane %+v, before %+v", sz, p, before)
		}
		rows := strings.Split(m.View(), "\n")
		if len(rows) != sz[1] {
			t.Fatalf("%v: %d rows", sz, len(rows))
		}
		head := ansi.Strip(rows[p.Top-p.Merge])
		if !strings.Contains(head, "Merge") || !strings.Contains(head, "Conflict 1 of 1") {
			t.Errorf("%v: merge header row %q", sz, head)
		}
		narrow := strings.Contains(ansi.Strip(rows[p.Top-p.Merge+1]), "1 Base")
		if narrow != (sz[0] < mergeNarrowW) {
			t.Errorf("%v: narrow = %v", sz, narrow)
		}
	}
	// Closed: no rows.
	m := mergeTestModel(t, 150, 42)
	m.merge.open = false
	if p := m.editorPaneLayout(); p.Merge != 0 {
		t.Errorf("closed view takes %d rows", p.Merge)
	}
}

func mergeKeyMsg(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "f6":
		return tea.KeyMsg{Type: tea.KeyF6}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestMergeViewOpensFromStages(t *testing.T) {
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 150, Height: 42})
	m := m0.(Model)
	m.focus = FocusExplorer
	m.onMergeStages(mergeStagesMsg{rel: "f.txt", st: mergeTestStages()})
	if !m.mergeViewShown() || !m.mergeFocused() || m.focus != FocusEditor {
		t.Fatalf("view not open and focused: shown %v focused %v", m.mergeViewShown(), m.mergeFocused())
	}
	if m.editorPaneLayout().Merge == 0 {
		t.Error("no rows for the view")
	}
	m2 := m
	m2.merge = nil
	m2.onMergeStages(mergeStagesMsg{rel: "g.txt", err: git.ErrNotConflicted})
	if m2.mergeViewShown() || !strings.Contains(aiToastText(&m2), "No merge conflict in g.txt") {
		t.Errorf("not conflicted: toast %q", aiToastText(&m2))
	}
}

func TestMergeViewKeys(t *testing.T) {
	m := mergeTestModel(t, 80, 24)
	m.merge.view.SetFocused(true)
	m.focus = FocusEditor
	two := []string{"<<<<<<< HEAD", "o1", "=======", "t1", ">>>>>>> b", "mid", "<<<<<<< HEAD", "o2", "=======", "t2", ">>>>>>> b"}
	m.merge.view.SetResult(two)
	send := func(k string) bool {
		t.Helper()
		nm, _, ok := m.routeToMergeView(mergeKeyMsg(k))
		m = nm
		return ok
	}
	if !send("n") || m.merge.view.Current() != 1 {
		t.Errorf("n: cur %d", m.merge.view.Current())
	}
	if !send("p") || m.merge.view.Current() != 0 {
		t.Errorf("p: cur %d", m.merge.view.Current())
	}
	if !send("3") || m.merge.view.ShownPane() != mergeview.PaneTheirs {
		t.Errorf("3: pane %d", m.merge.view.ShownPane())
	}
	for _, k := range []string{"o", "t", "b", "B", "a", "u"} {
		if !send(k) {
			t.Errorf("%s was not taken", k)
		}
	}
	if send("x") {
		t.Error("unknown key was taken")
	}
	if !send("tab") || m.mergeFocused() {
		t.Error("tab did not give the editor the keys")
	}
	if send("n") {
		t.Error("keys taken while the editor has them")
	}
	// F6 stops at the view, then goes on to the sidebar.
	if !send("f6") || !m.mergeFocused() {
		t.Error("f6 did not give the view the keys")
	}
	if send("f6") || m.merge.view.Focused() {
		t.Error("second f6 kept the keys in the view")
	}
	m.mergeFocusPanes()
	if send("esc") || m.mergeViewShown() || m.editorPaneLayout().Merge != 0 {
		t.Error("esc did not close the view")
	}
}

func TestMergeViewMouse(t *testing.T) {
	m := mergeTestModel(t, 150, 42)
	p := m.editorPaneLayout()
	press := func(x, y int) bool {
		nm, _, ok := m.routeToMergeView(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		m = nm
		return ok
	}
	if press(p.X+2, p.Top+2) || m.merge.view.Focused() {
		t.Error("a click in the editor kept the view's focus")
	}
	if !press(p.X+2, p.Top-p.Merge+3) || !m.mergeFocused() {
		t.Error("a click on the view did not give it the keys")
	}
}

func TestMergePaletteWithoutView(t *testing.T) {
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m := m0.(Model)
	if _, ok := m.dispatchMergePalette("git-merge-next"); !ok || !strings.Contains(aiToastText(&m), "not open") {
		t.Errorf("next without view: %q", aiToastText(&m))
	}
	if _, ok := m.dispatchMergePalette("git-merge-open"); !ok || !strings.Contains(aiToastText(&m), "No merge conflicts") {
		t.Errorf("open without conflicts: %q", aiToastText(&m))
	}
	if _, ok := m.dispatchMergePalette("git-commit"); ok {
		t.Error("took a foreign id")
	}
}

func TestMergeResolvedClosesOrRefuses(t *testing.T) {
	m := mergeTestModel(t, 150, 42)
	m.onMergeResolved(mergeResolvedMsg{rel: "f.txt", err: git.ErrMarkersLeft})
	if !m.mergeViewShown() || !strings.Contains(aiToastText(&m), "Can't mark as resolved") {
		t.Errorf("markers left: shown %v toast %q", m.mergeViewShown(), aiToastText(&m))
	}
	m.gitFiles = []git.FileStatus{{Code: "UU", Path: "f.txt"}, {Code: "UU", Path: "g.txt"}}
	m.onMergeResolved(mergeResolvedMsg{rel: "f.txt"})
	if m.mergeViewShown() || !strings.Contains(aiToastText(&m), "Marked as resolved: f.txt") || !strings.Contains(aiToastText(&m), "1 more file") {
		t.Errorf("resolved: shown %v toast %q", m.mergeViewShown(), aiToastText(&m))
	}
	m.gitFiles = nil
	if got := m.mergeNextStep("f.txt"); got != "Commit to finish." {
		t.Errorf("next step = %q", got)
	}
	m.gitRebase.InProgress = true
	if got := m.mergeNextStep("f.txt"); !strings.Contains(got, "Continue the rebase") {
		t.Errorf("rebase next step = %q", got)
	}
}

func TestMergeWholeFileCardIsSafeFirst(t *testing.T) {
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m := m0.(Model)
	st := git.ConflictStages{Path: "logo.png",
		Base:   git.ConflictSide{Present: true, Binary: true},
		Ours:   git.ConflictSide{Present: true, Binary: true},
		Theirs: git.ConflictSide{Present: false}}
	m.onMergeStages(mergeStagesMsg{rel: "logo.png", st: st})
	if m.mergeViewShown() || !m.confirmOpen || m.confirmKind != confirmKindMergeWholeFile {
		t.Fatalf("card not open: shown %v confirm %v", m.mergeViewShown(), m.confirmOpen)
	}
	bs := m.confirm.Buttons()
	if len(bs) != 4 || bs[0].ID != "merge-cancel" || m.confirm.SafeID() != "merge-cancel" || bs[3].ID != "merge-delete" {
		t.Errorf("buttons %+v safe %q", bs, m.confirm.SafeID())
	}
	if c := m.onMergeWholeCard("merge-cancel"); c != nil {
		t.Error("cancel ran something")
	}
	if c := m.onMergeWholeCard("merge-delete"); c != nil {
		t.Error("an answer after the card was used ran something")
	}
}
