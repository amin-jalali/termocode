package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// lgSGR is the SGR body lipgloss writes for a hex colour ("48;2;r;g;b"
// for a background), so rounding matches the renderer.
func lgSGR(kind string, c lipgloss.Color) string {
	st := lipgloss.NewStyle().Foreground(c)
	if kind == "48" {
		st = lipgloss.NewStyle().Background(c)
	}
	out := st.Render("x")
	if i, j := strings.Index(out, "["), strings.Index(out, "m"); i >= 0 && j > i {
		return out[i+1 : j]
	}
	return "no colour"
}

func sampleWelcomeRows() []welcomeRow {
	return []welcomeRow{
		{id: "card:0", title: "Open file", right: "Ctrl+P", rightIsKeycap: true, interactive: true},
		{id: "card:1", title: "Open workspace", interactive: true},
		{id: "setup:icons", glyph: "●", glyphTok: theme.BorderFocus, title: "Icons", titleW: 7,
			status: "Nerd icons on · GNOME Terminal: Ubuntu Mono 11 and a very long tail that never fits", right: "Fix…", interactive: true},
		{id: "setup:ai", glyph: "✓", glyphTok: theme.TextMuted, title: "AI", titleW: 7, status: "Claude · claude-opus-5-5", muted: true},
		{id: "recent:/a/b/model.go", glyph: "x", glyphTok: theme.AccentBlue, title: "a-very-long-file-name-that-is-cut.go", titleW: 20,
			status: "~/termocode/internal/statusbar/and/more/dirs", statusLeft: true, right: "Oct 9", interactive: true},
		{id: "recent:all", title: "All 20 recent files…", titleMuted: true, right: "Ctrl+R", rightIsKeycap: true, interactive: true},
	}
}

// TestWelcomeRowExactWidth: every row in every state is exactly W cells,
// in all icon modes and on 16 colours.
func TestWelcomeRowExactWidth(t *testing.T) {
	oldP, oldI := lipgloss.ColorProfile(), theme.CurrentIconMode
	defer func() { lipgloss.SetColorProfile(oldP); theme.CurrentIconMode = oldI }()
	for _, prof := range []termenv.Profile{termenv.TrueColor, termenv.ANSI} {
		lipgloss.SetColorProfile(prof)
		for _, icons := range []theme.IconMode{theme.IconModeNerdFont, theme.IconModeUnicode, theme.IconModeASCII} {
			theme.CurrentIconMode = icons
			for _, w := range []int{68, 60, 40, 24, 12} {
				for _, r := range sampleWelcomeRows() {
					for _, st := range []welcomeRowState{rowRest, rowCursorFocused, rowCursorBlurred} {
						out := renderWelcomeRow(r, w, st)
						if got := ansi.StringWidth(out); got != w {
							t.Fatalf("prof=%v icons=%v w=%d row %s state %d: %d cells %q", prof, icons, w, r.id, st, got, ansi.Strip(out))
						}
						if strings.Contains(out, "\n") {
							t.Fatalf("row %s spans lines", r.id)
						}
					}
				}
			}
		}
	}
}

// TestWelcomeRowCursorLook: the cursor row carries the 1-cell bar and the
// focused wash; blurred it has the quieter wash and a TextDimmer bar.
func TestWelcomeRowCursorLook(t *testing.T) {
	old := lipgloss.ColorProfile()
	oldI := theme.CurrentIconMode
	lipgloss.SetColorProfile(termenv.TrueColor)
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { lipgloss.SetColorProfile(old); theme.CurrentIconMode = oldI }()
	r := sampleWelcomeRows()[0]

	rest := renderWelcomeRow(r, 68, rowRest)
	if strings.Contains(ansi.Strip(rest), "▎") {
		t.Errorf("row at rest shows the cursor bar: %q", ansi.Strip(rest))
	}
	foc := renderWelcomeRow(r, 68, rowCursorFocused)
	if !strings.HasPrefix(ansi.Strip(foc), "▎") {
		t.Errorf("cursor row has no bar at col 0: %q", ansi.Strip(foc))
	}
	if !strings.Contains(foc, lgSGR("48", theme.Wash(theme.BgEditor, theme.WashSelectedFocused))) {
		t.Error("focused cursor row lacks the WashSelectedFocused fill")
	}
	if !strings.Contains(foc, fgSGR(theme.BorderFocus)) {
		t.Error("focused cursor bar is not BorderFocus")
	}
	blur := renderWelcomeRow(r, 68, rowCursorBlurred)
	if !strings.Contains(blur, lgSGR("48", theme.Wash(theme.BgEditor, theme.WashSelected))) {
		t.Errorf("blurred cursor row lacks the WashSelected fill %s: %q", lgSGR("48", theme.Wash(theme.BgEditor, theme.WashSelected)), blur)
	}
	if !strings.Contains(blur, fgSGR(theme.TextDimmer)) {
		t.Error("blurred cursor bar is not TextDimmer")
	}
	if strings.Contains(blur, fgSGR(theme.BorderFocus)) {
		t.Error("blurred cursor row still uses the accent")
	}
}

// TestWelcomeRowASCIIBar: without Unicode the bar is ">".
func TestWelcomeRowASCIIBar(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeASCII
	defer func() { theme.CurrentIconMode = old }()
	out := ansi.Strip(renderWelcomeRow(sampleWelcomeRows()[5], 40, rowCursorFocused))
	if !strings.HasPrefix(out, ">") || strings.Contains(out, "…") {
		t.Errorf("ASCII row: %q", out)
	}
}

// TestWelcomeRowLayout: text at col 3, glyph rows' title at col 5, the
// keycap ends at W-1 (one blank cell of margin), long text cut with "…".
func TestWelcomeRowLayout(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { theme.CurrentIconMode = old }()
	rows := sampleWelcomeRows()
	open := ansi.Strip(renderWelcomeRow(rows[0], 68, rowRest))
	if !strings.HasPrefix(open, "   Open file") {
		t.Errorf("title not at col 3: %q", open)
	}
	if !strings.HasSuffix(open, " Ctrl+P  ") {
		t.Errorf("keycap does not end at W-1: %q", open)
	}
	icons := ansi.Strip(renderWelcomeRow(rows[2], 68, rowRest))
	if !strings.HasPrefix(icons, "   ● Icons  Nerd") {
		t.Errorf("setup row layout: %q", icons)
	}
	if !strings.Contains(icons, "…") || !strings.HasSuffix(icons, "Fix… ") {
		t.Errorf("long status not cut before the action: %q", icons)
	}
	rec := ansi.Strip(renderWelcomeRow(rows[4], 68, rowRest))
	if !strings.Contains(rec, "…e/internal/statusbar/and/more/dirs") || !strings.HasSuffix(rec, "Oct 9 ") {
		t.Errorf("recent row: %q", rec)
	}
	if got := strings.Index(rec, "~"); got >= 0 {
		t.Errorf("directory should be cut from the left: %q", rec)
	}
	if rows[3].interactive {
		t.Error("a done setup row must not be interactive")
	}
}
