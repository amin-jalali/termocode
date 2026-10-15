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
