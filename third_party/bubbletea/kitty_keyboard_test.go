package tea

import (
	"strings"
	"testing"
)

// termocode patch: with WithKittyKeyboard the renderer pushes the kitty
// keyboard flags right after entering the alt screen (each screen has its
// own flag stack) and pops them before leaving it, so the shell gets its
// legacy keys back. Without the option nothing is sent.
func TestKittyKeyboardPushPop(t *testing.T) {
	out := &syncBuf{}
	r := newRenderer(out, false, 60).(*standardRenderer)
	r.kittyFlags = 1
	r.enterAltScreen()
	r.exitAltScreen()
	s := out.String()
	enter, push, pop, leave := strings.Index(s, "\x1b[?1049h"), strings.Index(s, "\x1b[>1u"), strings.Index(s, "\x1b[<u"), strings.Index(s, "\x1b[?1049l")
	if enter < 0 || push < 0 || pop < 0 || leave < 0 || !(enter < push && push < pop && pop < leave) {
		t.Fatalf("want enter < push < pop < leave, got %d %d %d %d in %q", enter, push, pop, leave, s)
	}

	out = &syncBuf{}
	r = newRenderer(out, false, 60).(*standardRenderer)
	r.enterAltScreen()
	r.exitAltScreen()
	if strings.Contains(out.String(), "u") && (strings.Contains(out.String(), "\x1b[>") || strings.Contains(out.String(), "\x1b[<u")) {
		t.Fatalf("kitty flags sent without the option: %q", out.String())
	}
}

func TestWithKittyKeyboardOption(t *testing.T) {
	p := NewProgram(nil, WithKittyKeyboard(1))
	if p.kittyFlags != 1 {
		t.Fatalf("kittyFlags = %d", p.kittyFlags)
	}
}
