package preview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TestViewRowsNeverExceedWidth guards the alt-screen frame: every rendered
// row must be exactly m.w cells wide. A row wider than m.w wraps in the
// terminal and corrupts the frame — that was the stray right-edge column /
// scrollbar gutter the user saw after closing a wide diff.
func TestViewRowsNeverExceedWidth(t *testing.T) {
	const w, h = 120, 12 // full-screen-ish width, where chrome fits
	body := strings.Join([]string{
		"short line",
		// An over-wide line with ANSI colour, like a long diff line.
		"\x1b[32m+" + strings.Repeat("x", 200) + "\x1b[0m",
		"another short",
	}, "\n")

	m := New("diff · "+strings.Repeat("deep/path/", 20)+"file.go", body)
	m.SetSize(w, h)

	for i, row := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(row); got > w {
			t.Errorf("row %d width = %d exceeds %d (wraps the frame): %q", i, got, w, row)
		}
	}
}

func TestNewSplitsBodyAndTrimsTrailingNewlines(t *testing.T) {
	m := New("title", "line1\nline2\nline3\n\n")
	if got, want := len(m.lines), 3; got != want {
		t.Fatalf("lines: want %d, got %d (%v)", want, got, m.lines)
	}
	if m.title != "title" {
		t.Fatalf("title not stored")
	}
}

func TestSetSizeClampsTop(t *testing.T) {
	m := New("", "a\nb\nc\nd\ne\nf\ng")
	m.top = 1000
	m.SetSize(10, 6) // contentHeight 2 → max top = 5
	if m.top != 5 {
		t.Fatalf("top want clamped to 5, got %d", m.top)
	}
}

func TestUpdateScrollKeys(t *testing.T) {
	body := strings.Repeat("x\n", 20)
	cases := []struct {
		name     string
		msg      tea.Msg
		wantTop  int
		startTop int
	}{
		{"down", tea.KeyMsg{Type: tea.KeyDown}, 1, 0},
		{"up bounded at 0", tea.KeyMsg{Type: tea.KeyUp}, 0, 0},
		{"j", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, 1, 0},
		{"home", tea.KeyMsg{Type: tea.KeyHome}, 0, 5},
		{"g", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}, 0, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New("", body)
			m.SetSize(20, 10)
			m.top = tc.startTop
			m, _ = m.Update(tc.msg)
			if m.top != tc.wantTop {
				t.Fatalf("top: want %d, got %d", tc.wantTop, m.top)
			}
		})
	}
}

func TestUpdatePageDownAndEndClampToMax(t *testing.T) {
	m := New("", strings.Repeat("y\n", 30))
	m.SetSize(20, 10) // contentHeight 6 → max top = 30-6 = 24
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.top != 24 {
		t.Fatalf("End: want top 24, got %d", m.top)
	}
}

func TestUpdateCloseKeys(t *testing.T) {
	cases := []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
	}
	for _, k := range cases {
		m := New("", "x")
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("%v: expected close cmd", k)
		}
		if _, ok := cmd().(CloseMsg); !ok {
			t.Fatalf("%v: want CloseMsg, got %T", k, cmd())
		}
	}
}

func TestUpdateMouseWheel(t *testing.T) {
	m := New("", strings.Repeat("z\n", 50))
	m.SetSize(20, 10)
	m, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	if m.top != 3 {
		t.Fatalf("wheel down: want 3, got %d", m.top)
	}
	m, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelUp})
	if m.top != 0 {
		t.Fatalf("wheel up: want 0, got %d", m.top)
	}
}

func TestViewEmptyAtZeroSize(t *testing.T) {
	m := New("t", "x")
	if v := m.View(); v != "" {
		t.Fatalf("zero size view should be empty, got %q", v)
	}
}

func TestViewIncludesTitleAndScrollIndicator(t *testing.T) {
	m := New("MyTitle", "a\nb\nc\nd\ne\nf")
	m.SetSize(40, 8)
	out := m.View()
	if !strings.Contains(out, "MyTitle") {
		t.Fatalf("title missing in view: %s", out)
	}
	// scrollIndicator should produce one of: Top, Bot, or "N%" — at top.
	if !strings.Contains(out, "Top") && !strings.Contains(out, "Bot") && !strings.Contains(out, "%") {
		t.Fatalf("no scroll indicator in view: %s", out)
	}
}

func TestFormatPctBoundaries(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "Top"},
		{-5, "Top"},
		{100, "Bot"},
		{150, "Bot"},
		{50, "50%"},
	}
	for _, tc := range cases {
		if got := formatPct(tc.in); got != tc.want {
			t.Fatalf("formatPct(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 1: "1", 42: "42", -7: "-7", 1234: "1234"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}
