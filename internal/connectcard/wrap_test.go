package connectcard

import (
	"strings"
	"testing"
	"time"
)

// Bug: wrap looped for ever on a wide character (two cells) in a line
// one cell wide, because the cut was empty. Now it puts the character on
// its own line.
func TestWrapWideRuneNarrowLine(t *testing.T) {
	done := make(chan []string, 1)
	go func() { done <- wrap("日本", 1) }()
	select {
	case got := <-done:
		if strings.Join(got, "|") != "日|本" {
			t.Fatalf("wrap = %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wrap does not end on a wide character in a narrow line")
	}
}
