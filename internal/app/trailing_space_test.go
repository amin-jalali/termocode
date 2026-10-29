package app

import (
	"math/rand"
	"strings"
	"testing"
)

// mayEndWithUnstyledSpaceFull is the old whole-row scan, kept as the
// reference for the faster version that starts at the last reset.
func mayEndWithUnstyledSpaceFull(s string) bool {
	last, _ := scanUnstyledSpace(s)
	return last
}

// scanUnstyledSpaceByRune is the first scanUnstyledSpace, one rune at a
// time: the reference for the version that jumps from escape to escape.
func scanUnstyledSpaceByRune(s string) (lastMaybe, glyph bool) {
	styled, unsure := false, false
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			sgr := s[i:j]
			switch {
			case sgr == "\x1b[0m" || sgr == "\x1b[m":
				styled, unsure = false, false
			case sgr[len(sgr)-1] == 'm':
				if styled {
					unsure = true
				}
				styled = true
			}
			i = j
			continue
		}
		start := i
		i++
		for i < len(s) && (s[i]&0xC0) == 0x80 {
			i++
		}
		lastMaybe = i == start+1 && s[start] == ' ' && (!styled || unsure)
		glyph = true
	}
	return lastMaybe, glyph
}

func TestScanUnstyledSpaceMatchesRuneScan(t *testing.T) {
	tokens := []string{
		" ", " ", "a", "é", "漢", "\x1b[0m", "\x1b[m", "\x1b[38;2;1;2;3m",
		"\x1b[48;5;236m", "\x1b[1m", "\x1b[K", "\x1b[2J", "  ", "x ",
		"\x1b", "\x1b[", "\x1b[3", "\x1bx", "\x1b ", "\xc3", "\xa9", " \xa9",
	}
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 300000; i++ {
		var b strings.Builder
		for n := rng.Intn(14); n > 0; n-- {
			b.WriteString(tokens[rng.Intn(len(tokens))])
		}
		s := b.String()
		gl, gg := scanUnstyledSpace(s)
		wl, wg := scanUnstyledSpaceByRune(s)
		if gl != wl || gg != wg {
			t.Fatalf("%q: got (%v, %v), rune scan says (%v, %v)", s, gl, gg, wl, wg)
		}
	}
}

// BenchmarkScanUnstyledSpace is a grid row's end: a long run of spaces
// in the editor background.
func BenchmarkScanUnstyledSpace(b *testing.B) {
	row := "\x1b[38;2;212;212;212;48;2;30;30;30m" + strings.Repeat(" ", 160) + "\x1b[0m"
	for i := 0; i < b.N; i++ {
		scanUnstyledSpace(row)
	}
}

func TestMayEndWithUnstyledSpaceMatchesFullScan(t *testing.T) {
	tokens := []string{
		" ", " ", "a", "é", "漢", "\x1b[0m", "\x1b[m", "\x1b[38;2;1;2;3m",
		"\x1b[48;5;236m", "\x1b[1m", "\x1b[K", "\x1b[2J", "  ", "x ",
	}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 200000; i++ {
		var b strings.Builder
		for n := rng.Intn(12); n > 0; n-- {
			b.WriteString(tokens[rng.Intn(len(tokens))])
		}
		s := b.String()
		if got, want := mayEndWithUnstyledSpace(s), mayEndWithUnstyledSpaceFull(s); got != want {
			t.Fatalf("%q: got %v, full scan says %v", s, got, want)
		}
	}
}

func BenchmarkMayEndWithUnstyledSpace(b *testing.B) {
	row := strings.Repeat("\x1b[38;2;200;200;200;48;2;30;30;30mfunc main() {\x1b[0m", 30) + "\x1b[48;2;30;30;30m    \x1b[0m"
	for i := 0; i < b.N; i++ {
		mayEndWithUnstyledSpace(row)
	}
}

func BenchmarkMayEndWithUnstyledSpaceFull(b *testing.B) {
	row := strings.Repeat("\x1b[38;2;200;200;200;48;2;30;30;30mfunc main() {\x1b[0m", 30) + "\x1b[48;2;30;30;30m    \x1b[0m"
	for i := 0; i < b.N; i++ {
		mayEndWithUnstyledSpaceFull(row)
	}
}
