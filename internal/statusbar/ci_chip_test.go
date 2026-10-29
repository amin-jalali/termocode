package statusbar

import (
	"strings"
	"testing"
)

// G-CI: the chip shows the CI state of the branch and hides when off.
func TestCIChip(t *testing.T) {
	for _, c := range []struct {
		s    State
		want string
	}{
		{State{}, ""},
		{State{CI: CIPending}, "CI …"},
		{State{CI: CIPass}, "CI ✓"},
		{State{CI: CIFail, CIFailed: 2}, "CI ✗ 2"},
	} {
		if got := stripCSI(renderCIChip(c.s)); got != c.want {
			t.Errorf("%+v: %q want %q", c.s, got, c.want)
		}
	}
}

// The chip sits right after the branch and has its own click span; the
// problem counters after it keep theirs.
func TestCIChipSpan(t *testing.T) {
	m := Model{}
	m.SetWidth(160)
	s := State{Line: 1, Col: 1, Branch: "feat/x", CI: CIFail, CIFailed: 1, Errors: 3}
	x0, x1, ok := m.CIChipSpan(s)
	if !ok {
		t.Fatal("no span")
	}
	plain := []rune(stripCSI(m.View(s)))
	if got := string(plain[x0:x1]); got != "CI ✗ 1" {
		t.Errorf("span covers %q", got)
	}
	d0, d1, ok := m.DiagSpan(s)
	if !ok || d0 < x1 || !strings.Contains(string(plain[d0:d1]), "3") {
		t.Errorf("diag span %d..%d after CI %d..%d: %q", d0, d1, x0, x1, string(plain[d0:d1]))
	}
	if _, _, ok := m.CIChipSpan(State{Line: 1, Col: 1, Branch: "x"}); ok {
		t.Error("CI off has a span")
	}
}
