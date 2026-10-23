package theme

import "testing"

// IM-12: ASCII mode writes the arrow, the middle dot and the dash of a text
// in ASCII; the other modes leave the text alone.
func TestASCIIText(t *testing.T) {
	old := CurrentIconMode
	defer func() { CurrentIconMode = old }()
	const in = "Tab size  4 → 2 · not imported — no match"
	CurrentIconMode = IconModeASCII
	if got, want := ASCIIText(in), "Tab size  4 -> 2 - not imported - no match"; got != want {
		t.Errorf("ASCII: %q, want %q", got, want)
	}
	for _, mode := range []IconMode{IconModeUnicode, IconModeNerdFont} {
		CurrentIconMode = mode
		if got := ASCIIText(in); got != in {
			t.Errorf("mode %v changed the text: %q", mode, got)
		}
	}
}
