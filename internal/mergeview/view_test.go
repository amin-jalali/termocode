package mergeview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func plainRows(t *testing.T, m Model, w, h int) []string {
	t.Helper()
	rows := strings.Split(m.View(), "\n")
	if len(rows) != h {
		t.Fatalf("rows = %d, want %d", len(rows), h)
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got != w {
			t.Fatalf("row %d width %d, want %d: %q", i, got, w, ansi.Strip(r))
		}
		rows[i] = ansi.Strip(r)
	}
	return rows
}

func TestViewWideShowsThreePanes(t *testing.T) {
	m, _ := twoBlocks()
	m.SetSize(120, 14, false)
	rows := plainRows(t, m, 120, 14)
	if !strings.Contains(rows[0], "f.txt") || !strings.Contains(rows[0], "Conflict 1 of 2") {
		t.Errorf("header %q", rows[0])
	}
	for _, s := range []string{"Base", "Ours · HEAD", "Theirs · feature"} {
		if !strings.Contains(rows[1], s) {
			t.Errorf("titles %q miss %q", rows[1], s)
		}
	}
	body := strings.Join(rows[2:], "\n")
	for _, s := range []string{"#1▎ x", "#1▎ X1", "#1▎ X2", "#2▎ Y2", "Y2b"} {
		if !strings.Contains(body, s) {
			t.Errorf("body misses %q:\n%s", s, body)
		}
	}
	if !strings.Contains(rows[13], "gm") {
		t.Errorf("unfocused hint %q", rows[13])
	}
	m.SetFocused(true)
	if rows := plainRows(t, m, 120, 14); !strings.Contains(rows[13], "ours") || !strings.Contains(rows[13], "resolved") {
		t.Errorf("focused hint %q", rows[13])
	}
}

func TestViewNarrowOnePane(t *testing.T) {
	m, _ := twoBlocks()
	m.SetSize(60, 10, true)
	m.SetFocused(true)
	rows := plainRows(t, m, 60, 10)
	if !strings.Contains(rows[1], "1 Base") || !strings.Contains(rows[1], "3 Theirs") {
		t.Errorf("tabs %q", rows[1])
	}
	body := strings.Join(rows[2:9], "\n")
	if !strings.Contains(body, "X1") || strings.Contains(body, "X2") {
		t.Errorf("narrow shows ours only:\n%s", body)
	}
	if !strings.HasPrefix(strings.TrimSpace(rows[9]), "1-3") {
		t.Errorf("narrow hint %q", rows[9])
	}
	m.CyclePane()
	if body := strings.Join(plainRows(t, m, 60, 10)[2:9], "\n"); !strings.Contains(body, "X2") {
		t.Errorf("theirs pane:\n%s", body)
	}
	// A click on the "1 Base" tab shows the base.
	if !m.Click(2, 1) || m.ShownPane() != PaneBase {
		t.Errorf("tab click: pane %d", m.ShownPane())
	}
}

func TestViewTinySizesAndEmptySide(t *testing.T) {
	m, _ := twoBlocks()
	for _, sz := range [][2]int{{1, 1}, {20, 2}, {30, 3}, {40, 4}, {200, 40}} {
		m.SetSize(sz[0], sz[1], sz[0] < 60)
		plainRows(t, m, sz[0], sz[1])
	}
	// Taking ours in block 2 of a delete-vs-change leaves an empty side.
	m2 := New("f", m2Stages())
	m2.SetResult([]string{"a", "<<<<<<< HEAD", "=======", "t", ">>>>>>> b", "c"})
	m2.SetSize(90, 8, false)
	if body := strings.Join(plainRows(t, m2, 90, 8), "\n"); !strings.Contains(body, "nothing here") {
		t.Errorf("empty ours not shown:\n%s", body)
	}
}

func TestViewClickPicksBlock(t *testing.T) {
	m, _ := twoBlocks()
	m.SetSize(120, 14, false)
	rows := plainRows(t, m, 120, 14)
	y := -1
	for i, r := range rows {
		if strings.Contains(r, "#2▎ Y1") {
			y = i
		}
	}
	if y < 0 {
		t.Fatal("block 2 not on screen")
	}
	x := strings.Index(rows[y], "#2▎ Y1")
	if !m.Click(len([]rune(rows[y][:x]))+5, y) || m.Current() != 1 {
		t.Errorf("click block 2: cur %d", m.Current())
	}
	if m.Click(0, 0) {
		t.Error("header click handled")
	}
}
