package statusbar

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

func TestLSPChipRendersClientsOrNone(t *testing.T) {
	m := New(theme.Theme{})
	m.SetWidth(120)
	got := stripCSI(m.View(State{Path: "/a.go", Lang: "Go", ShowLSP: true, LSP: []string{"gopls"}}))
	if !strings.Contains(got, "{} gopls") {
		t.Fatalf("missing attached chip: %q", got)
	}
	got = stripCSI(m.View(State{Path: "/a.go", Lang: "Go", ShowLSP: true}))
	if !strings.Contains(got, "{} none") {
		t.Fatalf("missing none chip: %q", got)
	}
	got = stripCSI(m.View(State{Path: "/a.go", Lang: "Go"}))
	if strings.Contains(got, "{}") {
		t.Fatalf("chip must be hidden without ShowLSP: %q", got)
	}
}

func TestLSPChipSpanMatchesRenderedText(t *testing.T) {
	m := New(theme.Theme{})
	m.SetWidth(120)
	s := State{Path: "/a.go", Project: "proj", Lang: "Go", Line: 1, Col: 1,
		Branch: "main", Term: true, ShowLSP: true, LSP: []string{"gopls"}}
	x0, x1, ok := m.LSPChipSpan(s)
	if !ok {
		t.Fatal("chip should be visible")
	}
	row := []rune(stripCSI(m.View(s)))
	if got := string(row[x0:x1]); got != "{} gopls" {
		t.Fatalf("span [%d,%d) = %q", x0, x1, got)
	}
	if _, _, ok := m.LSPChipSpan(State{Path: "/a.go"}); ok {
		t.Fatal("no chip → not ok")
	}
}
