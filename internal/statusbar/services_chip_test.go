package statusbar

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

// The services chip "☷ 3/4" shows only when ShowServices is set; "✘ n"
// joins it when something is unhealthy or exited.
func TestServicesChip(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	if renderServicesChip(State{ServicesUp: 3, ServicesTotal: 4}) != "" {
		t.Error("hidden unless ShowServices")
	}
	got := stripCSI(renderServicesChip(State{ShowServices: true, ServicesUp: 3, ServicesTotal: 4}))
	if got != "☷ 3/4" {
		t.Errorf("chip = %q", got)
	}
	got = stripCSI(renderServicesChip(State{ShowServices: true, ServicesUp: 2, ServicesTotal: 4, ServicesBad: 1}))
	if got != "☷ 2/4 · ✘ 1" {
		t.Errorf("chip with bad = %q", got)
	}
}

// The chip has its own click span, before the test chip, inside the bar.
func TestServicesChipSpan(t *testing.T) {
	m := Model{}
	m.SetWidth(160)
	s := State{Line: 1, Col: 1, Branch: "main", ShowServices: true, ServicesUp: 3, ServicesTotal: 4,
		ShowTests: true, TestsPassed: 2}
	x0, x1, ok := m.ServicesChipSpan(s)
	if !ok {
		t.Fatal("no span")
	}
	plain := []rune(stripCSI(m.View(s)))
	if got := string(plain[x0:x1]); !strings.Contains(got, "3/4") || strings.Contains(got, "✓") {
		t.Errorf("span covers %q", got)
	}
	t0, _, ok := m.TestsChipSpan(s)
	if !ok || x1 > t0 {
		t.Errorf("services chip %d..%d overlaps the test chip at %d", x0, x1, t0)
	}
	if _, _, ok := m.ServicesChipSpan(State{Line: 1, Col: 1}); ok {
		t.Error("hidden chip has no span")
	}
}
