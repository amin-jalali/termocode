package search

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func altKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true}
}

func typeText(m Model, s string) Model {
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return m
}

func TestModel_ToggleKeys(t *testing.T) {
	m := New()
	m, _ = m.Update(altKey('c'))
	m, _ = m.Update(altKey('w'))
	m, _ = m.Update(altKey('r'))
	o := m.Options()
	if !o.CaseSensitive || !o.WholeWord || !o.Regex {
		t.Errorf("toggles not set: %+v", o)
	}
	if m.input != "" {
		t.Errorf("alt keys must not type into the query, got %q", m.input)
	}
	m, _ = m.Update(altKey('c'))
	if m.Options().CaseSensitive {
		t.Error("Alt+C should toggle case back off")
	}
}

func TestModel_FilterFields(t *testing.T) {
	m := New()
	m = typeText(m, "needle")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(m, "*.go, *.md")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(m, "vendor")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(m, "x3") // non-digits are ignored
	o := m.Options()
	if m.input != "needle" {
		t.Errorf("query = %q", m.input)
	}
	if len(o.Include) != 2 || o.Include[1] != "*.md" {
		t.Errorf("include = %q", o.Include)
	}
	if len(o.Exclude) != 1 || o.Exclude[0] != "vendor" {
		t.Errorf("exclude = %q", o.Exclude)
	}
	if o.Context != 3 {
		t.Errorf("context = %d", o.Context)
	}
	// Tab wraps back to the query field.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != fieldQuery {
		t.Errorf("focus = %d, want query", m.focus)
	}
}

func TestModel_MaxResultsAndSummary(t *testing.T) {
	m := New()
	if m.Options().MaxResults != DefaultMaxResults {
		t.Errorf("default cap = %d", m.Options().MaxResults)
	}
	m.SetMaxResults(5)
	m.SetSize(120, 40)
	m = typeText(m, "x")
	m, _ = m.Update(resultsMsg{
		version: m.queryVer,
		results: []Result{{Path: "a", Line: 1, Col: 1, Preview: "x"}},
		summary: Summary{Shown: 5, Total: 12, Files: 1},
	})
	if v := m.View(); !strings.Contains(v, "showing 5 of 12 results") {
		t.Error("capped summary missing from view")
	}
}

func TestModel_ClickToggle(t *testing.T) {
	m := New()
	m.SetSize(120, 40)
	bx, by, bw, _ := m.boxRect()
	c, _, r := toggleCols(bw - 2)
	m, _ = m.HandleMouse(bx+1+c, by+1, tea.MouseActionPress, tea.MouseButtonLeft)
	m, _ = m.HandleMouse(bx+1+r+1, by+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if !m.caseSensitive || !m.useRegex || m.wholeWord {
		t.Errorf("click toggles: case=%v word=%v regex=%v", m.caseSensitive, m.wholeWord, m.useRegex)
	}
}

// TestModel_BoxShape checks every rendered line of the box has the same
// width (including rows with context lines) and the height matches the
// size boxRect reports, so mouse hit-testing lines up.
func TestModel_BoxShape(t *testing.T) {
	m := New()
	m.SetSize(100, 30)
	m = typeText(m, "hit")
	m, _ = m.Update(resultsMsg{
		version: m.queryVer,
		results: []Result{{
			Path: "a.go", Line: 2, Col: 1, Preview: "hit here",
			Matches: []MatchRange{{0, 3}},
			Before:  []ContextLine{{1, "before"}},
			After:   []ContextLine{{3, "after"}},
		}},
		summary: Summary{Shown: 1, Total: 1, Files: 1},
	})
	w, h := m.boxSize()
	lines := strings.Split(m.renderBox(w, h), "\n")
	if len(lines) != h {
		t.Errorf("box height = %d, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := lipgloss.Width(l); got != w {
			t.Errorf("line %d width %d, want %d", i, got, w)
		}
	}
}
