package app

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// randomColumns are three columns of styled rows; each call changes a
// few rows, sometimes a width or a row count.
func randomColumns(rng *rand.Rand, prev [][]string) [][]string {
	if prev == nil || rng.Intn(10) == 0 {
		cols := make([][]string, 3)
		h := 3 + rng.Intn(6)
		for i := range cols {
			w := 2 + rng.Intn(20)
			for y := 0; y < h-rng.Intn(2); y++ {
				cols[i] = append(cols[i], fmt.Sprintf("\x1b[3%dm%s\x1b[0m", i, strings.Repeat("x", w-rng.Intn(2))))
			}
		}
		return cols
	}
	cols := make([][]string, len(prev))
	for i, c := range prev {
		cols[i] = append([]string(nil), c...)
		if len(c) > 0 && rng.Intn(2) == 0 {
			y := rng.Intn(len(c))
			cols[i][y] = fmt.Sprintf("\x1b[1m%s\x1b[0m%s", strings.Repeat("y", 1+rng.Intn(3)), strings.Repeat(" ", rng.Intn(6)))
		}
	}
	return cols
}

// The row-reusing join gives exactly what joinColumnsLines gives, frame
// after frame.
func TestFrameJoinMemoMatchesJoin(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	var memo frameJoinMemo
	var cols [][]string
	for i := 0; i < 3000; i++ {
		cols = randomColumns(rng, cols)
		want := joinColumnsLines(cols...)
		got := memo.join(cols...)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("frame %d:\n got %q\nwant %q", i, got, want)
		}
	}
}

// normalizeFrameLines reuses rows it saw last frame, and follows a
// change of the window width, the theme and the colour profile.
func TestNormalizeFrameLinesMemoFollowsChanges(t *testing.T) {
	perfColour(t)
	m := Model{w: 30, h: 4}
	rows := []string{"abc   ", "\x1b[31mred\x1b[0m", strings.Repeat("z", 30), ""}
	reference := func(m Model, in []string) []string {
		normMemo.Lock()
		normMemo.in, normMemo.out = nil, nil
		normMemo.Unlock()
		return m.normalizeFrameLines(append([]string(nil), in...))
	}
	check := func(what string, m Model) {
		t.Helper()
		got := m.normalizeFrameLines(append([]string(nil), rows...)) // memo from before
		again := m.normalizeFrameLines(append([]string(nil), rows...))
		want := reference(m, rows)
		if strings.Join(again, "\n") != strings.Join(want, "\n") {
			t.Fatalf("%s, second call:\n got %q\nwant %q", what, again, want)
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("%s:\n got %q\nwant %q", what, got, want)
		}
	}
	check("start", m)
	_ = m.normalizeFrameLines(append([]string(nil), rows...))
	m2 := m
	m2.w = 40
	check("wider", m2)
	_ = m.normalizeFrameLines(append([]string(nil), rows...))
	theme.ApplyTheme("solarized-dark")
	check("theme", m)
	_ = m.normalizeFrameLines(append([]string(nil), rows...))
	lipgloss.SetColorProfile(termenv.ANSI256)
	check("profile", m)
}
