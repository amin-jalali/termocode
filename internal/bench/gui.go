package bench

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// GUIDriver is what the GUI harness needs from a display: find a window,
// press a key, grab the screen. X11Driver is the Linux one; tests use a fake.
type GUIDriver interface {
	// WaitWindow waits until a visible window with title in its name exists.
	WaitWindow(title string, timeout time.Duration) (time.Duration, error)
	SendKey(key string) error
	// Frame grabs the screen (any bytes that change when the screen does).
	Frame() ([]byte, error)
}

// GUIStart is the time until the window shows. Call it right after starting
// the program (the clock starts at the call).
func GUIStart(d GUIDriver, title string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	if _, err := d.WaitWindow(title, timeout); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

// KeyLatency is a GUI key to screen time with how coarse the capture was.
type KeyLatency struct {
	Latency    time.Duration // key sent until the first changed frame was seen
	Resolution time.Duration // time between two captures: the error margin
}

func framesEqual(a, b []byte) bool { return a != nil && b != nil && bytes.Equal(a, b) }

// GUIKeyLatency grabs a base frame, sends key and grabs frames in a loop
// until one differs. period is the pause between grabs (0 = back to back).
// The result is the typometer idea: latency is known to within Resolution.
func GUIKeyLatency(d GUIDriver, key string, timeout, period time.Duration) (KeyLatency, error) {
	base, err := d.Frame()
	if err != nil {
		return KeyLatency{}, err
	}
	sent := time.Now()
	if err := d.SendKey(key); err != nil {
		return KeyLatency{}, err
	}
	last := sent
	var gaps time.Duration
	n := 0
	for time.Since(sent) < timeout {
		f, err := d.Frame()
		if err != nil {
			return KeyLatency{}, err
		}
		now := time.Now()
		gaps += now.Sub(last)
		n++
		last = now
		if !framesEqual(base, f) {
			return KeyLatency{Latency: now.Sub(sent), Resolution: max(gaps/time.Duration(n), period)}, nil
		}
		time.Sleep(period)
	}
	return KeyLatency{}, errors.New("the screen never changed after the key")
}

// X11Driver drives an X11 display with xdotool and ffmpeg.
type X11Driver struct{ display string }

// DetectGUI returns an X11 driver, or why GUI measures are skipped here.
func DetectGUI() (GUIDriver, string) {
	disp := os.Getenv("DISPLAY")
	if disp == "" {
		return nil, "no X11 display (DISPLAY is empty): GUI measures need a display"
	}
	for _, tool := range []string{"xdotool", "ffmpeg"} {
		if _, err := lookPath(tool); err != nil {
			return nil, fmt.Sprintf("%s not installed: GUI measures need it for window and screen capture", tool)
		}
	}
	return &X11Driver{display: disp}, ""
}

func (x *X11Driver) WaitWindow(title string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	for time.Since(start) < timeout {
		out, _ := spawn.Command("xdotool", "search", "--onlyvisible", "--name", title).Output()
		if strings.TrimSpace(string(out)) != "" {
			return time.Since(start), nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return 0, fmt.Errorf("no window named %q after %v", title, timeout)
}

func (x *X11Driver) SendKey(key string) error {
	return spawn.Command("xdotool", "key", "--clearmodifiers", key).Run()
}

func (x *X11Driver) Frame() ([]byte, error) {
	return spawn.Command("ffmpeg", "-loglevel", "error", "-f", "x11grab", "-i", x.display, "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray", "-").Output()
}
