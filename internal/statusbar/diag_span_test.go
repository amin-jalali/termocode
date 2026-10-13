package statusbar

import (
	"strings"
	"testing"
)

// Group C: the diagnostic counters report a clickable span.
func TestDiagSpan(t *testing.T) {
	m := Model{}
	m.SetWidth(120)
	if _, _, ok := m.DiagSpan(State{Project: "p"}); ok {
		t.Fatal("no diagnostics: want no span")
	}
	s := State{Project: "p", Branch: "main", Errors: 3, Warnings: 2}
	x0, x1, ok := m.DiagSpan(s)
	if !ok || x1-x0 < 7 {
		t.Fatalf("span = %d..%d ok=%v", x0, x1, ok)
	}
	if _, _, ok := m.DiagSpan(State{Err: "boom", Errors: 1}); ok {
		t.Error("error message hides the counters")
	}
	if a, _, _ := m.DiagSpan(State{Project: "p", Errors: 1}); a >= x0 {
		t.Errorf("without a branch the counters should start further left: %d vs %d", a, x0)
	}
}

// Regression: the counters' span ran to the end of the center cluster, so
// a click on an extension item or the test chip opened the Problems
// panel; with no diagnostics at all an extension item was the "counters".
func TestDiagSpanOnlyCoversCounters(t *testing.T) {
	m := Model{}
	m.SetWidth(160)
	s := State{Line: 1, Col: 1, Branch: "main", Ext: []string{"TODO 3"}, ShowTests: true, TestsPassed: 2}
	if x0, x1, ok := m.DiagSpan(s); ok {
		t.Errorf("no diagnostics: want no span, got %d..%d", x0, x1)
	}
	s.Errors, s.Warnings = 3, 2
	x0, x1, ok := m.DiagSpan(s)
	if !ok {
		t.Fatal("no span for the counters")
	}
	plain := []rune(stripCSI(m.View(s)))
	if got := strings.TrimSpace(string(plain[x0:x1])); strings.Contains(got, "TODO") || strings.Contains(got, "✓") {
		t.Errorf("the counters' span covers %q", got)
	}
	ext := m.ExtItemSpans(s)
	t0, _, _ := m.TestsChipSpan(s)
	if len(ext) != 1 || x1 > ext[0].X0 || x1 > t0 {
		t.Errorf("counters %d..%d overlap the extension item %+v or the test chip at %d", x0, x1, ext, t0)
	}
}
