package shotkit

import (
	"testing"
	"time"
)

// OnFrame gets the start-up frame and one frame per step, each with the
// step's Hold (how long a recording shows it), so a demo GIF can be built
// from a scene.
func TestRunOnFrame(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the app with nvim")
	}
	type rec struct {
		frame string
		hold  time.Duration
	}
	var got []rec
	sc := Scene{Name: "rec", Steps: []Step{
		{Wait: 100 * time.Millisecond, Hold: 700 * time.Millisecond},
		Key("f1"),
	}}
	_, err := Run(sc, 80, 24, Options{Quiet: true, OnFrame: func(f string, hold time.Duration) {
		got = append(got, rec{f, hold})
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("frames: %d, want 3 (start-up + 2 steps)", len(got))
	}
	if got[1].hold != 700*time.Millisecond {
		t.Errorf("step hold %v, want 700ms", got[1].hold)
	}
	if got[0].hold <= 0 || got[2].hold <= 0 {
		t.Errorf("default holds must be > 0: %v %v", got[0].hold, got[2].hold)
	}
	if got[0].frame == "" || got[2].frame == got[1].frame {
		t.Error("frames empty or the palette key did not change the screen")
	}
}
