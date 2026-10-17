package app

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// graphRowText renders the GRAPH row of commit hash (plain and styled).
func graphRowText(t *testing.T, m *Model, hash string) (string, string) {
	t.Helper()
	for _, r := range m.gitPanelRows() {
		if r.kind == gitRowCommit && r.hash == hash {
			row := m.renderGitPanelRow(r, false, false, 30)
			return ansi.Strip(row), row
		}
	}
	t.Fatalf("no row for %s", hash)
	return "", ""
}

// markCol is the mark column of a plain GRAPH row (the cell before
// " age").
func markCol(plain string) string {
	r := []rune(plain)
	return string(r[len(r)-5])
}

// Spec (scm-graph §2.7): a GRAPH row marked for compare shows a small
// 1 / 2 in BorderFocus in the mark column; Esc clears it and so does the
// compare card opening.
func TestGraphCompareMarks(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	rgb, _ := theme.ParseHex(theme.Hex(theme.BorderFocus))
	focusFG := "38;2;" + strconv.Itoa(int(rgb.R)) + ";" + strconv.Itoa(int(rgb.G)) + ";" + strconv.Itoa(int(rgb.B))

	m := graphModel(t, 4, 0, false)
	for i := range m.gitCommits {
		m.gitCommits[i].FullHash = m.gitCommits[i].Hash + "full"
	}
	m.gitCompareMark(2)
	plain, row := graphRowText(t, m, "t2")
	if markCol(plain) != "1" {
		t.Errorf("first mark not in the mark column: %q", plain)
	}
	if !strings.Contains(row, focusFG) {
		t.Errorf("mark not in BorderFocus: %q", row)
	}
	if ansi.StringWidth(plain) != 30 {
		t.Errorf("marked row width %d: %q", ansi.StringWidth(plain), plain)
	}
	if p, _ := graphRowText(t, m, "t1"); markCol(p) != " " {
		t.Errorf("unmarked row shows a mark: %q", p)
	}

	// The second Space shows 2 until the compare card is ready.
	if cmd := m.gitCompareMark(0); cmd == nil {
		t.Fatal("second mark does not open the compare")
	}
	if p, _ := graphRowText(t, m, "t0"); markCol(p) != "2" {
		t.Errorf("second mark not shown: %q", p)
	}
	if p, _ := graphRowText(t, m, "t2"); markCol(p) != "1" {
		t.Errorf("first mark lost before the compare opened: %q", p)
	}
	m.onCommitViewReady(commitViewReadyMsg{want: m.commitView.want, compare: &commitRange{a: "t2full", b: "t0full", sa: "t2", sb: "t0"}})
	if p, _ := graphRowText(t, m, "t0"); markCol(p) != " " {
		t.Errorf("mark 2 kept after the compare opened: %q", p)
	}
	if p, _ := graphRowText(t, m, "t2"); markCol(p) != " " {
		t.Errorf("mark 1 kept after the compare opened: %q", p)
	}

	// Esc in the sidebar clears a single mark.
	m.closeCommitView()
	m.gitCompareMark(1)
	if p, _ := graphRowText(t, m, "t1"); markCol(p) != "1" {
		t.Fatalf("mark not shown: %q", p)
	}
	m.gitLoaded, m.gitIsRepo = true, true
	m2, _, _ := m.handleGitSidebarKey(tea.KeyMsg{Type: tea.KeyEsc})
	if p, _ := graphRowText(t, &m2, "t1"); markCol(p) != " " {
		t.Errorf("Esc did not clear the mark: %q", p)
	}
}
