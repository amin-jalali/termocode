package anim

import (
	"os"
	"testing"
	"time"
)

// Tick schedules a frame message that carries the time.
func TestTickDeliversTickMsg(t *testing.T) {
	cmd := Tick()
	if cmd == nil {
		t.Fatal("no command")
	}
	done := make(chan any, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if _, ok := msg.(TickMsg); !ok {
			t.Fatalf("got %T", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tick never came")
	}
}

// Jump lands on the value at once and stops all motion.
func TestJumpStopsMotion(t *testing.T) {
	v := Value{Pos: 0, Vel: 5, Target: 3}
	v.Jump(0.5)
	if v.Pos != 0.5 || v.Vel != 0 || v.Target != 0.5 {
		t.Fatalf("%+v", v)
	}
}

// With no getenv given, Resolve reads the real environment: over SSH,
// auto mode turns animations off.
func TestResolveUsesRealEnvByDefault(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "1.2.3.4 1 5.6.7.8 22")
	if Resolve(ModeAuto, true, nil) {
		t.Fatal("auto over SSH should not animate")
	}
	os.Unsetenv("SSH_CONNECTION")
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	if !Resolve(ModeAuto, true, nil) {
		t.Fatal("auto on a local truecolor terminal should animate")
	}
}
