//go:build unix

package extproc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ext"
)

func shExt(name, script string) ext.Extension {
	return ext.Extension{Name: name, Dir: "/", Manifest: ext.Manifest{
		Run:              &ext.RunSpec{Command: "/bin/sh", Args: []string{"-c", script}},
		ActivationEvents: []string{"onCommand"},
	}}
}

func lifeHost(t *testing.T, cfg Config) *Host {
	t.Helper()
	if cfg.Root == "" {
		cfg.Root = t.TempDir()
	}
	if cfg.Backoff == nil {
		cfg.Backoff = func(int) time.Duration { return 0 }
	}
	h := NewHost(cfg)
	t.Cleanup(h.Close)
	return h
}

func waitFor(t *testing.T, h *Host, what string, match func(Event) bool) Event {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev := <-h.Events():
			if match(ev) {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s; log:\n%s", what, strings.Join(h.Log(), "\n"))
		}
	}
}

func stateOf(h *Host, name string) Status {
	for _, s := range h.Status() {
		if s.Name == name {
			return s
		}
	}
	return Status{}
}

func TestStartFailureIsReported(t *testing.T) {
	h := lifeHost(t, Config{})
	bad := ext.Extension{Name: "bad", Dir: "/", Manifest: ext.Manifest{Run: &ext.RunSpec{Command: "/nonexistent/ext-bin"}}}
	lua := ext.Extension{Name: "lua", Init: "/x/init.lua"}
	h.Set([]ext.Extension{bad, lua})
	if st := h.Status(); len(st) != 1 || st[0].Name != "bad" || st[0].State != StateIdle {
		t.Fatalf("status %+v", st)
	}
	h.Start("nobody") // unknown: nothing happens
	err := h.Execute(context.Background(), "bad", "bad.x", nil)
	if err == nil || !strings.Contains(err.Error(), "extension bad failed") {
		t.Fatalf("err = %v", err)
	}
	ev := waitFor(t, h, "failed event", func(e Event) bool { return e.Kind == EvFailed })
	if ev.Ext != "bad" || !strings.Contains(ev.Err, "no such file") {
		t.Fatalf("event %+v", ev)
	}
	if s := stateOf(h, "bad"); s.State != StateFailed || s.Err == "" {
		t.Fatalf("status %+v", s)
	}
	// A failed extension is not started again by a plain call.
	if err := h.Execute(context.Background(), "bad", "bad.x", nil); err == nil {
		t.Fatal("failed extension ran")
	}
}

func TestSilentExtensionTimesOutAndGivesUp(t *testing.T) {
	h := lifeHost(t, Config{InitTimeout: 200 * time.Millisecond, MaxRestarts: 1})
	// Never answers initialize; says something on stderr.
	h.Set([]ext.Extension{shExt("mute", "echo 'booting slowly' >&2; exec sleep 30")})
	err := h.Execute(context.Background(), "mute", "mute.x", nil)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	ev := waitFor(t, h, "give up", func(e Event) bool { return e.Kind == EvFailed })
	if !strings.Contains(ev.Err, "crashed 2 times") {
		t.Fatalf("event %+v", ev)
	}
	log := strings.Join(h.Log(), "\n")
	for _, want := range []string{"stderr: booting slowly", "initialize failed", "restart 1 of 1"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

func TestStopDuringRestartWait(t *testing.T) {
	h := lifeHost(t, Config{Backoff: func(int) time.Duration { return time.Hour }})
	h.Set([]ext.Extension{shExt("crashy", "exit 3")})
	h.Start("crashy")
	ev := waitFor(t, h, "crash", func(e Event) bool { return e.Kind == EvExited })
	if !ev.Restarting || !strings.Contains(ev.Err, "exit status 3") {
		t.Fatalf("event %+v", ev)
	}
	if s := stateOf(h, "crashy"); s.State != StateRestarting || s.Restarts != 1 {
		t.Fatalf("status %+v", s)
	}
	done := make(chan struct{})
	go func() { h.StopAll(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("StopAll waited for the restart delay")
	}
	if s := stateOf(h, "crashy"); s.State != StateStopped {
		t.Fatalf("status %+v", s)
	}
	// Notify to a stopped extension is dropped without error.
	h.Notify("crashy", "event/notify", nil)
	h.Notify("nobody", "event/notify", nil)
}
