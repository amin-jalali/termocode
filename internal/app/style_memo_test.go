package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// The memo gives what lipgloss gives, and follows a theme switch and a
// colour profile change.
func TestPaintMemoFollowsThemeAndProfile(t *testing.T) {
	perfColour(t)
	check := func(what string) {
		t.Helper()
		if got, want := bgSpaces(theme.BgEditor, 7), theme.Bg(theme.BgEditor).Render(strings.Repeat(" ", 7)); got != want {
			t.Fatalf("%s: bgSpaces %q, want %q", what, got, want)
		}
		if got, want := paintFgBg(theme.TextMuted, theme.BgTitleBar, true, "⋮"), theme.FgBg(theme.TextMuted, theme.BgTitleBar).Bold(true).Render("⋮"); got != want {
			t.Fatalf("%s: paintFgBg %q, want %q", what, got, want)
		}
	}
	check("start")
	check("again")
	theme.ApplyTheme("solarized-dark")
	check("other theme")
	lipgloss.SetColorProfile(termenv.ANSI256)
	check("other profile")
	if a := testing.AllocsPerRun(20, func() { _ = bgSpaces(theme.BgEditor, 7) }); a != 0 {
		t.Fatalf("bgSpaces: %v allocs, want 0", a)
	}
}

func BenchmarkBgSpaces(b *testing.B) {
	perfColour(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bgSpaces(theme.BgEditor, 40)
	}
}
