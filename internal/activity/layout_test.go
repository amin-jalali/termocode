package activity

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

func withIcons(t *testing.T, m theme.IconMode) {
	t.Helper()
	old := theme.CurrentIconMode
	theme.CurrentIconMode = m
	t.Cleanup(func() { theme.CurrentIconMode = old })
}

// Spec (UI review #9 + signature #2): the bar starts with the termocode
// mark (2 rows, the pixel "T" of the logo); clicking it opens the command
// palette. The icons follow below it.
func TestMarkOpensPalette(t *testing.T) {
	withIcons(t, theme.IconModeNerdFont)
	m := New()
	m.SetHeight(40)
	for _, y := range []int{0, 1} {
		_, cmd := m.HandleMouse(1, y, tea.MouseLeft)
		if cmd == nil {
			t.Fatalf("click on the mark row %d did nothing", y)
		}
		if _, ok := cmd().(OpenPaletteMsg); !ok {
			t.Fatalf("row %d: got %T, want OpenPaletteMsg", y, cmd())
		}
	}
	if it := m.Items(); len(it) == 0 || it[0].Y < markRows {
		t.Fatalf("first icon at row %v, want below the mark", it)
	}
	lines := strings.Split(m.View(), "\n")
	if !strings.ContainsAny(lines[0]+lines[1], "▀▄█▌▐▖▗▘▝▙▛▜▟") {
		t.Errorf("no pixel mark in the first rows: %q", lines[:2])
	}
}

// Spec (UI review #9): with single-cell icons (Nerd / Unicode) the icons
// sit one pitch apart, without the empty gap row; ASCII art keeps it.
func TestIconsTighterWithGlyphs(t *testing.T) {
	for _, c := range []struct {
		mode theme.IconMode
		gap  int
	}{{theme.IconModeNerdFont, 0}, {theme.IconModeUnicode, 0}, {theme.IconModeASCII, 1}} {
		withIcons(t, c.mode)
		m := New()
		m.SetHeight(40)
		it := m.Items()
		if len(it) < 2 {
			t.Fatal("fewer than 2 items")
		}
		if d := it[1].Y - it[0].Y; d != itemPitch+c.gap {
			t.Errorf("mode %v: icons %d rows apart, want %d", c.mode, d, itemPitch+c.gap)
		}
		// A click on each item's rows reaches it.
		for _, x := range it {
			for y := x.Y; y < x.Y+x.H; y++ {
				if got, ok := m.ItemAt(y); !ok || got.View != x.View {
					t.Errorf("mode %v: row %d hits %v, want %v", c.mode, y, got.View, x.View)
				}
			}
		}
	}
}

// Spec (UI review #9): the active icon is drawn in the accent colour.
func TestActiveIconInAccent(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	withIcons(t, theme.IconModeUnicode)
	m := New()
	m.SetHeight(40)
	row := strings.Split(m.View(), "\n")[m.Items()[0].Y]
	c, _ := theme.ParseHex(theme.Hex(theme.BorderFocus))
	want := "38;2;" + itoa3(int(c.R)) + ";" + itoa3(int(c.G)) + ";" + itoa3(int(c.B))
	if !strings.Contains(row, want+"m"+theme.IconFiles.ASCII) && !strings.Contains(row, want+";") {
		t.Errorf("active icon not in the accent colour (%s): %q", want, row)
	}
}

func itoa3(n int) string {
	s := ""
	if n == 0 {
		return "0"
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
