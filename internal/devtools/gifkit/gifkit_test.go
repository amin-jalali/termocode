package gifkit

import (
	"strings"
	"testing"
	"time"
)

// The sidecar has one line per caption with its time range; the last one
// runs to the end of the clip.
func TestCaptionText(t *testing.T) {
	c := Clip{Name: "x", Note: "demo models", Frames: []Frame{{Hold: 3 * time.Second}, {Hold: 2500 * time.Millisecond}},
		Captions: []Caption{{At: 0, Text: "One."}, {At: 3 * time.Second, Text: "Two."}}}
	got := CaptionText(c)
	for _, want := range []string{"x.gif - 5.5 s", "demo models", " 0.0- 3.0 s  One.", " 3.0- 5.5 s  Two."} {
		if !strings.Contains(got, want) {
			t.Errorf("caption file lacks %q:\n%s", want, got)
		}
	}
}

// Every screen is exactly Rows lines, so frames of one clip have one size
// (a terminal emulator drops trailing blank rows).
func TestNormalizePadsToRows(t *testing.T) {
	for _, in := range []string{"a", "a\n\n\n", strings.Repeat("x\n", Rows+5)} {
		if got := strings.Count(Normalize(in), "\n") + 1; got != Rows {
			t.Errorf("Normalize(%.10q): %d lines, want %d", in, got, Rows)
		}
	}
}

// The title frame fills the screen and holds its text.
func TestTitleFrame(t *testing.T) {
	f := TitleFrame("termocode", "tagline")
	if n := strings.Count(f, "\n") + 1; n != Rows {
		t.Errorf("%d lines, want %d", n, Rows)
	}
	if !strings.Contains(f, "tagline") {
		t.Error("tagline missing")
	}
}
