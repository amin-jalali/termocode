package anim

import "testing"

func TestValueSnapsWhenDisabled(t *testing.T) {
	Enabled = false
	var v Value
	v.Set(10)
	if v.Pos != 10 || v.Moving() {
		t.Fatalf("disabled Set should snap: %+v", v)
	}
	if v.Step() {
		t.Fatal("Step on a settled value must report false")
	}
}

func TestValueSettles(t *testing.T) {
	Enabled = true
	defer func() { Enabled = false }()
	var v Value
	v.Set(1)
	frames := 0
	for v.Step() {
		frames++
		if frames > FPS*2 {
			t.Fatalf("did not settle in 2s: %+v", v)
		}
		if v.Pos > 1.05 {
			t.Fatalf("overshoot: %v", v.Pos)
		}
	}
	if v.Pos != 1 {
		t.Fatalf("final pos %v", v.Pos)
	}
	// ~150-250 ms at 60 FPS.
	if frames < 5 || frames > 20 {
		t.Errorf("settled in %d frames", frames)
	}
	s := NewScroll()
	s.Set(20)
	n := 0
	for s.Step() {
		n++
	}
	if s.Int() != 20 || n == 0 || n > 30 {
		t.Errorf("scroll settle: pos=%v frames=%d", s.Pos, n)
	}
}

func TestResolve(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	none := env(nil)
	ssh := env(map[string]string{"SSH_CONNECTION": "1 2 3 4"})
	cases := []struct {
		mode Mode
		tc   bool
		get  func(string) string
		want bool
	}{
		{ModeOn, false, ssh, true},
		{ModeOff, true, none, false},
		{ModeAuto, true, none, true},
		{ModeAuto, false, none, false},
		{ModeAuto, true, ssh, false},
		{ModeAuto, true, env(map[string]string{"SSH_TTY": "/dev/pts/1"}), false},
	}
	for _, c := range cases {
		if got := Resolve(c.mode, c.tc, c.get); got != c.want {
			t.Errorf("Resolve(%s, %v) = %v", c.mode, c.tc, got)
		}
	}
	if ParseMode("bogus") != ModeAuto || ParseMode("off") != ModeOff {
		t.Error("ParseMode")
	}
}
