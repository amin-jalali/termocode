package app

import (
	"strings"
	"testing"
)

// frameWidth gives what measuring gives, the first time and from the
// memo, for single rows and for text with line breaks.
func TestFrameWidthMatchesMeasuring(t *testing.T) {
	nextFrameWidths()
	rows := []string{
		"",
		"short",
		strings.Repeat("\x1b[38;2;1;2;3mabc\x1b[0m", 20),
		strings.Repeat("漢字", 30),
		strings.Repeat("x", 60) + "\n" + strings.Repeat("y", 70),
		"\x1b[1m" + strings.Repeat("é ", 40) + "\x1b[0m",
	}
	for _, r := range rows {
		want := textWidth(r)
		if !strings.Contains(r, "\n") {
			want = cellWidth(r)
		}
		for i := 0; i < 3; i++ {
			if got := frameWidth(r); got != want {
				t.Fatalf("frameWidth(%q) call %d = %d, want %d", r, i, got, want)
			}
		}
	}
	// A row given with its width keeps it; text with a line break is
	// never taken from the memo.
	row := strings.Repeat("z", 64)
	setFrameWidth(row, 64)
	if got := frameWidth(row); got != 64 {
		t.Fatalf("set row: %d, want 64", got)
	}
	multi := strings.Repeat("a", 50) + "\n" + strings.Repeat("b", 60)
	setFrameWidth(multi, 999)
	if got := frameWidth(multi); got != textWidth(multi) {
		t.Fatalf("multi-line text: %d, want %d", got, textWidth(multi))
	}
}

// BenchmarkFrameWidthMemo measures a long row the memo already knows.
func BenchmarkFrameWidthMemo(b *testing.B) {
	row := strings.Repeat("\x1b[38;2;200;200;200;48;2;30;30;30mfunc main() {\x1b[0m", 30)
	nextFrameWidths()
	frameWidth(row)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frameWidth(row)
	}
}
