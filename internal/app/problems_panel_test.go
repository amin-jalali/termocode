package app

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"termocode/internal/nvim"
)

func sampleDiags() []nvim.Diagnostic {
	return []nvim.Diagnostic{
		{Path: "/w/b.go", Line: 9, Col: 1, Severity: 2, Message: "warn b"},
		{Path: "/w/a.go", Line: 5, Col: 2, Severity: 3, Message: "info a"},
		{Path: "/w/b.go", Line: 3, Col: 1, Severity: 1, Message: "err b"},
		{Path: "/w/a.go", Line: 1, Col: 1, Severity: 2, Message: "warn a"},
	}
}

func TestSortProblemsGroupsByWorstSeverity(t *testing.T) {
	d := sampleDiags()
	sortProblems(d)
	var got []string
	for _, x := range d {
		got = append(got, x.Message)
	}
	// b.go has an error, so it comes first.
	want := []string{"err b", "warn b", "warn a", "info a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestProblemsRowsFilterCollapse(t *testing.T) {
	p := newProblemsPanel()
	p.setItems(sampleDiags())
	if e, w, i := p.counts(); e != 1 || w != 2 || i != 1 {
		t.Fatalf("counts = %d %d %d", e, w, i)
	}
	rows := p.rows()
	if len(rows) != 6 || !rows[0].Header || rows[0].Path != "/w/b.go" || rows[0].Count != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	// Hide warnings: b.go keeps 1, a.go keeps 1.
	p.hide[2] = true
	rows = p.rows()
	if len(rows) != 4 || rows[0].Count != 1 || rows[2].Count != 1 {
		t.Fatalf("filtered rows = %+v", rows)
	}
	p.hide[2] = false
	// Collapse the file under the selection (row 0 = b.go header).
	p.sel = 1
	p.toggleCollapse(nil)
	rows = p.rows()
	if len(rows) != 4 || !rows[0].Header || p.sel != 0 {
		t.Fatalf("collapsed rows = %+v sel=%d", rows, p.sel)
	}
	p.toggleCollapseAll()
	if len(p.rows()) != 2 {
		t.Fatalf("collapse all: %d rows", len(p.rows()))
	}
	p.toggleCollapseAll()
	if len(p.rows()) != 6 {
		t.Fatalf("expand all: %d rows", len(p.rows()))
	}
}

func TestProblemsSetItemsKeepsSelection(t *testing.T) {
	p := newProblemsPanel()
	p.viewH = 10
	p.setItems(sampleDiags())
	p.sel = 4 // "warn a" (a.go:1)
	p.setItems(append(sampleDiags(), nvim.Diagnostic{Path: "/w/0.go", Line: 1, Severity: 1, Message: "new"}))
	rows := p.rows()
	if r := rows[p.sel]; r.Header || r.Diag.Message != "warn a" {
		t.Errorf("selection moved to %+v", r)
	}
}

func TestParseProblemsJSON(t *testing.T) {
	got := parseProblemsJSON(`[["/a.go",3,2,1,"gopls","boom"],["bad"]]`)
	want := []nvim.Diagnostic{{Path: "/a.go", Line: 3, Col: 2, Severity: 1, Source: "gopls", Message: "boom"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	if parseProblemsJSON("") != nil || parseProblemsJSON("{") != nil {
		t.Error("empty / bad input should give nil")
	}
}

func TestProblemsRenderAndTitle(t *testing.T) {
	m := Model{w: 100, h: 30, problems: newProblemsPanel()}
	if got := problemsTitle(&m); got != "PROBLEMS" {
		t.Errorf("empty title = %q", got)
	}
	rows := renderProblemsTab(&m, 60, 8)
	if len(rows) != 2 {
		t.Fatalf("empty render rows = %d", len(rows))
	}
	m.problems.setItems(sampleDiags())
	if got := problemsTitle(&m); got != "PROBLEMS (4)" {
		t.Errorf("title = %q", got)
	}
	rows = renderProblemsTab(&m, 60, 5)
	if len(rows) != 5 {
		t.Fatalf("render rows = %d, want 5 (header + 4)", len(rows))
	}
	for i, r := range rows {
		if w := lipgloss.Width(r); w > 60 {
			t.Errorf("row %d is %d cells wide (> 60)", i, w)
		}
	}
	// Through the framework: exact width and height.
	m.panelActive = panelKindProblems
	for i, r := range m.renderPanelContent(60, 7) {
		if w := lipgloss.Width(r); w != 60 {
			t.Errorf("panel row %d width %d", i, w)
		}
	}
	if got := m.panelTitle(panelKindProblems); got != "PROBLEMS (4)" {
		t.Errorf("panelTitle = %q", got)
	}
}

func TestProblemsKeysAndHeaderClick(t *testing.T) {
	m := Model{w: 100, h: 30, problems: newProblemsPanel(), showExp: false}
	m.problems.viewH = 10
	m.problems.setItems(sampleDiags())
	key := func(s string) bool {
		var msg tea.KeyMsg
		switch s {
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		}
		_, ok := problemsKey(&m, msg)
		return ok
	}
	if !key("down") || m.problems.sel != 1 {
		t.Fatalf("down: sel=%d", m.problems.sel)
	}
	if !key("e") || !m.problems.hide[1] {
		t.Fatal("e should hide errors")
	}
	if key("x") {
		t.Error("unknown key should fall through")
	}
	// Click the error chip in the header row: shows errors again.
	e, w, i := m.problems.counts()
	chips := problemsHeaderLayout(e, w, i, m.editorPaneWidth())
	if len(chips) != 4 || chips[3].Bucket != 0 {
		t.Fatalf("chips = %+v", chips)
	}
	problemsClick(&m, 0, chips[0].Start)
	if m.problems.hide[1] {
		t.Error("chip click should toggle the error filter back on")
	}
}
