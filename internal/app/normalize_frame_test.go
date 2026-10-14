package app

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// legacyNormalizeFrame is normalizeFrame before the fast path: every row
// parsed cell by cell. The fast version must give the same bytes.
func legacyNormalizeFrame(m Model, s string) string {
	if m.w <= 0 || m.h <= 0 {
		return s
	}
	filler := theme.Bg(theme.BgEditor)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w < m.w {
			line += filler.Render(strings.Repeat(" ", m.w-w))
		}
		lines[i] = restyleTrailingUnstyled(line, filler)
	}
	for len(lines) < m.h {
		lines = append(lines, filler.Render(strings.Repeat(" ", m.w)))
	}
	if len(lines) > m.h {
		lines = lines[len(lines)-m.h:]
	}
	return strings.Join(lines, "\n")
}

// raggedFrame breaks a finished frame the ways the columns do: short rows,
// naked trailing spaces, styles opened and merged without a reset.
func raggedFrame(rng *rand.Rand, frame string) string {
	rows := strings.Split(frame, "\n")
	tails := []string{
		"   ", " ", "\x1b[1m  ", "\x1b[1m\x1b[22m  ", "\x1b[0m   ", "x  ",
		"\x1b[31m \x1b[0m ", "\x1b[m  \x1b[m", "  \x1b[0m", "\x1b[2 q ", "\x1b[38;2;1;2;3m\x1b[48;2;4;5;6m a",
		"界 ", "é ", "\x1b]8;;x\x1b\\ ", "", "\x1b[1m \x1b[22m", "\x1b[",
	}
	for i := range rows {
		switch rng.Intn(4) {
		case 0:
			rows[i] = truncRightVisual(rows[i], rng.Intn(lipgloss.Width(rows[i])+1))
		case 1:
			rows[i] = truncRightVisual(rows[i], lipgloss.Width(rows[i])-4) + tails[rng.Intn(len(tails))]
		case 2:
			rows[i] += tails[rng.Intn(len(tails))]
		}
	}
	if n := rng.Intn(5); n > 0 && n < len(rows) {
		if rng.Intn(2) == 0 {
			rows = rows[:len(rows)-n]
		} else {
			rows = append(rows, rows[:n]...)
		}
	}
	return strings.Join(rows, "\n")
}

// legacyRestyle is restyleTrailingUnstyled without the quick scan.
func legacyRestyle(line string, filler lipgloss.Style) string {
	cells := parseANSIRow(line)
	trailing := 0
	for i := len(cells) - 1; i >= 0 && cells[i].sgr == "" && cells[i].glyph == " "; i-- {
		trailing++
	}
	if trailing == 0 {
		return line
	}
	return renderCells(cells[:len(cells)-trailing]) + filler.Render(strings.Repeat(" ", trailing))
}

// The quick scan must never skip a row the full parse would rewrite,
// including styles that cancel out without a reset.
func TestRestyleTrailingUnstyledQuickScan(t *testing.T) {
	old := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(old)
	lipgloss.SetColorProfile(termenv.TrueColor)
	filler := theme.Bg(theme.BgEditor)
	heads := []string{"", "ab", "\x1b[31mab\x1b[0m", "\x1b[31mab", "界", "\x1b[1m\x1b[31mx"}
	tails := []string{
		"", " ", "  ", "x", "x ", " x", "\x1b[0m ", "\x1b[m  ", "  \x1b[0m", "\x1b[1m ",
		"\x1b[1m\x1b[22m  ", "\x1b[31m\x1b[39m ", "\x1b[48;2;1;2;3m\x1b[49m ", "\x1b[1m\x1b[31m ",
		"\x1b[2 q ", "\x1b[", "\x1b[3", "\x1b ", "é ", " é", "\t", " \x1b[K",
	}
	for _, h := range heads {
		for _, a := range tails {
			for _, b := range tails {
				in := h + a + b
				if got, want := restyleTrailingUnstyled(in, filler), legacyRestyle(in, filler); got != want {
					t.Errorf("%q:\n got %q\nwant %q", in, got, want)
				}
			}
		}
	}
}

func TestNormalizeFrameMatchesLegacy(t *testing.T) {
	withConfigDir(t)
	old := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(old)
	for _, p := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		lipgloss.SetColorProfile(p)
		m := dragTestModel(t)
		frame := m.View()
		rng := rand.New(rand.NewSource(int64(p)))
		inputs := []string{frame, "", "\n", strings.Repeat(" ", m.w)}
		for i := 0; i < 60; i++ {
			inputs = append(inputs, raggedFrame(rng, frame))
		}
		for i, in := range inputs {
			if got, want := m.normalizeFrame(in), legacyNormalizeFrame(m, in); got != want {
				gr, wr := strings.Split(got, "\n"), strings.Split(want, "\n")
				for r := range wr {
					if r < len(gr) && gr[r] != wr[r] {
						t.Fatalf("profile %v input %d row %d:\n got %q\nwant %q", p, i, r, gr[r], wr[r])
					}
				}
				t.Fatalf("profile %v input %d: output differs", p, i)
			}
		}
	}
}

// BenchmarkNormalizeFrame runs normalizeFrame on a 200x50 frame that is
// already the right shape (the usual case) and on a ragged one.
func BenchmarkNormalizeFrame(b *testing.B) {
	b.Setenv("XDG_CONFIG_HOME", b.TempDir())
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	m := dragTestModel(b)
	frame := m.View()
	for _, c := range []struct{ name, in string }{
		{"full", frame},
		{"ragged", raggedFrame(rand.New(rand.NewSource(1)), frame)},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = m.normalizeFrame(c.in)
			}
		})
	}
}
