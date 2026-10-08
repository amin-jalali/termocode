package statusbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/ext"
)

func TestExtItemSpans(t *testing.T) {
	m := Model{}
	m.SetWidth(160)
	s := State{Line: 1, Col: 1, Branch: "main", ShowTests: true, TestsPassed: 3, Ext: []string{"{{muted}}12 words{{/}}", "", "TODO 3"}}
	view := m.View(s)
	if lipgloss.Width(view) != 160 {
		t.Fatalf("bar width = %d", lipgloss.Width(view))
	}
	spans := m.ExtItemSpans(s)
	if len(spans) != 2 || spans[0].Index != 0 || spans[1].Index != 2 {
		t.Fatalf("spans = %+v", spans)
	}
	plain := stripCSI(view)
	for _, sp := range spans {
		got := string([]rune(plain)[sp.X0:sp.X1])
		want := ext.PlainText(s.Ext[sp.Index])
		if got != want {
			t.Errorf("span %d covers %q, want %q", sp.Index, got, want)
		}
	}
	if strings.Contains(view, "{{") {
		t.Error("markup leaked into the bar")
	}
}

func TestExtItemSpansDroppedCenter(t *testing.T) {
	m := Model{}
	m.SetWidth(30)
	s := State{Line: 1, Col: 1, Project: "a-very-long-project-name", Ext: []string{"words"}}
	if spans := m.ExtItemSpans(s); spans != nil {
		t.Errorf("spans with no room = %+v", spans)
	}
}
