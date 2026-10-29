package app

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The SGR memo must not hand out an encoding made for another colour
// profile: a switch from true colour to 256 colours re-encodes.
func TestSGRMemoFollowsColourProfile(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	s := sgrState{fg: rgbColor{R: 12, G: 200, B: 99}, hasFg: true, bold: true}

	lipgloss.SetColorProfile(termenv.TrueColor)
	if got, want := s.render(), "\x1b[1;38;2;12;200;99m"; got != want {
		t.Fatalf("true colour: got %q, want %q", got, want)
	}
	lipgloss.SetColorProfile(termenv.ANSI256)
	if got, want := s.render(), s.encode(termenv.ANSI256); got != want || got == "\x1b[1;38;2;12;200;99m" {
		t.Fatalf("256 colours: got %q, want %q", got, want)
	}
	lipgloss.SetColorProfile(termenv.Ascii)
	if got := s.render(); got != "\x1b[1m" {
		t.Fatalf("no colour: got %q, want only bold", got)
	}
}

// The memo stays bounded: many blended colours (an open animation) do
// not grow it without end.
func TestSGRMemoBounded(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	for i := 0; i < 3*sgrMemoMax; i++ {
		s := sgrState{bg: rgbColor{R: uint8(i), G: uint8(i >> 8), B: 7}, hasBg: true}
		if got, want := s.render(), s.encode(termenv.TrueColor); got != want {
			t.Fatalf("state %d: got %q, want %q", i, got, want)
		}
	}
	sgrMemo.Lock()
	n := len(sgrMemo.m)
	sgrMemo.Unlock()
	if n > sgrMemoMax {
		t.Fatalf("memo holds %d states, max %d", n, sgrMemoMax)
	}
}
