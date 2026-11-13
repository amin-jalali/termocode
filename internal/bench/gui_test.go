package bench

import (
	"bytes"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeDriver struct {
	windowAfter time.Duration
	changeAfter time.Duration
	t0          atomic.Int64 // when the window wait began
	keyAt       atomic.Int64
}

func (f *fakeDriver) WaitWindow(title string, timeout time.Duration) (time.Duration, error) {
	if f.windowAfter > timeout {
		time.Sleep(timeout)
		return timeout, errors.New("no window")
	}
	time.Sleep(f.windowAfter)
	return f.windowAfter, nil
}
func (f *fakeDriver) SendKey(k string) error { f.keyAt.Store(time.Now().UnixNano()); return nil }
func (f *fakeDriver) Frame() ([]byte, error) {
	if at := f.keyAt.Load(); at != 0 && time.Since(time.Unix(0, at)) >= f.changeAfter {
		return []byte("after"), nil
	}
	return []byte("before"), nil
}

func TestGUIStartTimeReportsWindowDelay(t *testing.T) {
	d := &fakeDriver{windowAfter: 80 * time.Millisecond}
	got, err := GUIStart(d, "main.go", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got < 80*time.Millisecond || got > 500*time.Millisecond {
		t.Fatalf("start %v", got)
	}
}

func TestGUIStartTimeoutIsAnError(t *testing.T) {
	d := &fakeDriver{windowAfter: time.Hour}
	if _, err := GUIStart(d, "x", 100*time.Millisecond); err == nil {
		t.Fatal("want an error when no window shows")
	}
}

func TestGUIKeyLatencyIsTimeToFirstChangedFrame(t *testing.T) {
	d := &fakeDriver{changeAfter: 60 * time.Millisecond}
	lat, err := GUIKeyLatency(d, "Q", time.Second, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if lat.Latency < 55*time.Millisecond || lat.Latency > 300*time.Millisecond {
		t.Fatalf("latency %v", lat.Latency)
	}
	if lat.Resolution < 5*time.Millisecond {
		t.Fatalf("the report must say how coarse the capture was: %v", lat.Resolution)
	}
}

func TestGUIKeyLatencyFailsWhenNothingChanges(t *testing.T) {
	d := &fakeDriver{changeAfter: time.Hour}
	if _, err := GUIKeyLatency(d, "Q", 100*time.Millisecond, 5*time.Millisecond); err == nil {
		t.Fatal("a frame that never changes is an error, not a number")
	}
}

func TestFramesEqual(t *testing.T) {
	if !framesEqual([]byte("a"), []byte("a")) || framesEqual([]byte("a"), []byte("b")) || framesEqual(nil, []byte("b")) {
		t.Fatal("framesEqual wrong")
	}
	_ = bytes.Equal
}

func TestDetectGUISaysWhyWhenUnavailable(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	d, skip := DetectGUI()
	if d != nil || skip == "" {
		t.Fatalf("driver %v skip %q: no display must skip with a reason", d, skip)
	}
	t.Setenv("DISPLAY", ":99")
	t.Setenv("PATH", t.TempDir())
	d, skip = DetectGUI()
	if d != nil || skip == "" {
		t.Fatal("missing xdotool must skip with a reason")
	}
}
