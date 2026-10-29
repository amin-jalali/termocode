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
