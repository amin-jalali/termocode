package termfont

import "testing"

func TestMethodAndStateStrings(t *testing.T) {
	if Kitty.String() != "kitty" || None.String() != "none" {
		t.Fatalf("%q %q", Kitty.String(), None.String())
	}
	if got := (State{method: Kitty, delta: 2}).String(); got != "kitty +2" {
		t.Fatalf("got %q", got)
	}
	if got := (State{method: Kitty, delta: -3}).String(); got != "kitty -3" {
		t.Fatalf("got %q", got)
	}
}

// A kitty answer that is not JSON is complete (stop reading) but not ok.
func TestKittyAnswerBrokenJSON(t *testing.T) {
	answered, ok := KittyAnswer([]byte("\x1bP" + kittyPrefix + "not json\x1b\\"))
	if !answered || ok {
		t.Fatalf("answered=%v ok=%v", answered, ok)
	}
}
