package toast

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (UI review #13): a toast is as wide as its text needs (28 cells at
// least, the given maximum at most), not always 52 cells for six words.
func TestFitWidth(t *testing.T) {
	m := New()
	m, _ = m.PushDetail(Info, "Saved", "a.go")
	if got := m.FitWidth(52); got != 28 {
		t.Errorf("short toast: width %d, want 28", got)
	}
	m, _ = m.PushDetail(Info, "Saved", "internal/statusbar/model.go · 480 lines")
	w := m.FitWidth(52)
	if want := runewidth.StringWidth("internal/statusbar/model.go · 480 lines") + 8; w != want {
		t.Errorf("long detail: width %d, want %d", w, want)
	}
	m, _ = m.PushDetail(Info, strings.Repeat("x", 80), "")
	if got := m.FitWidth(52); got != 52 {
		t.Errorf("very long: width %d, want the max 52", got)
	}
	if got := New().FitWidth(52); got != 0 {
		t.Errorf("no toasts: %d", got)
	}
}

// Spec (UI review #13): the frame is the normal card border; the severity
// shows as the left edge only (not a tinted frame all around).
func TestToastSeverityOnLeftEdge(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	lines := renderToast(Toast{Severity: Errr, Title: "Failed", Detail: "x"}, 30)
	sev, _ := theme.ParseHex(theme.Hex(theme.DiagError))
	sevFG := "38;2;" + itoa(int(sev.R)) + ";" + itoa(int(sev.G)) + ";" + itoa(int(sev.B))
	mid := lines[1]
	if !strings.Contains(mid, sevFG) {
		t.Errorf("left edge not in the error colour: %q", mid)
	}
	if strings.Contains(lines[0], sevFG) {
		t.Errorf("top border tinted with the severity: %q", lines[0])
	}
	if got := ansi.StringWidth(mid); got != 30 {
		t.Errorf("row width %d, want 30", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// A compact card is never narrower than its title needs.
func TestCompactTitleWidth(t *testing.T) {
	m := New()
	m, _ = m.PushDetail(Warn, "Tests: 1 failed, 1 passed", "go test ./...")
	w := m.CompactTitleWidth()
	lines := renderToastCompact(m.toasts[0], w)
	if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "Tests: 1 failed, 1 passed") {
		t.Errorf("title cut at width %d:\n%s", w, ansi.Strip(strings.Join(lines, "\n")))
	}
}
