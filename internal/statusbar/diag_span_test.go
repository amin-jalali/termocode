package statusbar

import "testing"

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
