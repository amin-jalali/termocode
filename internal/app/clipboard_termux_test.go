package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeClipHost builds a clipHost with no real environment, PATH or
// processes: env holds the variables, onPath the commands that exist,
// and every command run is recorded (argv + stdin) and answered by out.
type fakeRun struct {
	argv  []string
	stdin string
}

func fakeClipHost(goos string, env map[string]string, onPath []string, out map[string]string, runs *[]fakeRun) clipHost {
	return clipHost{
		goos:   goos,
		getenv: func(k string) string { return env[k] },
		lookPath: func(name string) (string, error) {
			for _, p := range onPath {
				if p == name {
					return "/fake/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
		run: func(_ context.Context, stdin string, argv []string) (string, error) {
			if runs != nil {
				*runs = append(*runs, fakeRun{argv: append([]string(nil), argv...), stdin: stdin})
			}
			if o, ok := out[argv[0]]; ok {
				return o, nil
			}
			return "", errors.New("failed")
		},
	}
}

func TestClipHostTermuxDetect(t *testing.T) {
	cases := []struct {
		name string
		goos string
		env  map[string]string
		want bool
	}{
		{"plain linux", "linux", map[string]string{"DISPLAY": ":0"}, false},
		{"macOS", "darwin", nil, false},
		{"android build", "android", nil, true},
		{"TERMUX_VERSION", "linux", map[string]string{"TERMUX_VERSION": "0.118.0"}, true},
		{"Termux PREFIX", "linux", map[string]string{"PREFIX": "/data/data/com.termux/files/usr"}, true},
		{"other PREFIX", "linux", map[string]string{"PREFIX": "/usr/local"}, false},
	}
	for _, c := range cases {
		h := fakeClipHost(c.goos, c.env, nil, nil, nil)
		if got := h.termux(); got != c.want {
			t.Errorf("%s: termux() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClipToolsOrderTermuxFirst(t *testing.T) {
	// Even with an X display (Termux:X11), Termux's own clipboard comes first.
	h := fakeClipHost("android", map[string]string{"DISPLAY": ":0"}, nil, nil, nil)
	tools := h.tools()
	if len(tools) == 0 || tools[0][0] != "termux-clipboard-set" {
		t.Fatalf("android tools = %v, want termux-clipboard-set first", tools)
	}
}

func TestClipToolsOrderLinuxUnchanged(t *testing.T) {
	h := fakeClipHost("linux", map[string]string{"DISPLAY": ":0", "WAYLAND_DISPLAY": "wayland-0"}, nil, nil, nil)
	var names []string
	for _, a := range h.tools() {
		names = append(names, a[0])
	}
	if got := strings.Join(names, ","); got != "wl-copy,xclip,xsel" {
		t.Fatalf("linux tools = %s, want wl-copy,xclip,xsel", got)
	}
}

func TestClipToolsTermuxCommandOnPathOutsideTermux(t *testing.T) {
	// termux-clipboard-set on PATH (e.g. a proot distro inside Termux) is
	// tried after the desktop tools.
	h := fakeClipHost("linux", map[string]string{"DISPLAY": ":0"}, []string{"termux-clipboard-set"}, nil, nil)
	tools := h.tools()
	if last := tools[len(tools)-1][0]; last != "termux-clipboard-set" {
		t.Fatalf("tools = %v, want termux-clipboard-set last", tools)
	}
}

func TestClipToolWriteTermuxPipesStdin(t *testing.T) {
	var runs []fakeRun
	h := fakeClipHost("android", nil, []string{"termux-clipboard-set"},
		map[string]string{"termux-clipboard-set": ""}, &runs)
	if !h.toolWrite("héllo\nworld") {
		t.Fatal("toolWrite = false, want true")
	}
	if len(runs) != 1 || runs[0].argv[0] != "termux-clipboard-set" || runs[0].stdin != "héllo\nworld" {
		t.Fatalf("runs = %+v, want one termux-clipboard-set with the text on stdin", runs)
	}
}

func TestClipToolWriteTermuxMissingAPI(t *testing.T) {
	var runs []fakeRun
	h := fakeClipHost("android", nil, nil, nil, &runs)
	if h.toolWrite("x") {
		t.Fatal("toolWrite = true without termux-clipboard-set")
	}
	if len(runs) != 0 {
		t.Fatalf("ran %v, want nothing (no command on PATH)", runs)
	}
}

func TestClipReadTermux(t *testing.T) {
	var runs []fakeRun
	h := fakeClipHost("android", nil, []string{"termux-clipboard-get"},
		map[string]string{"termux-clipboard-get": "from android"}, &runs)
	got, ok := h.termuxRead()
	if !ok || got != "from android" {
		t.Fatalf("termuxRead = %q, %v; want %q, true", got, ok, "from android")
	}
	if len(runs) != 1 || runs[0].argv[0] != "termux-clipboard-get" {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestClipReadTermuxMissingAPI(t *testing.T) {
	h := fakeClipHost("android", nil, nil, nil, nil)
	if got, ok := h.termuxRead(); ok || got != "" {
		t.Fatalf("termuxRead = %q, %v; want \"\", false", got, ok)
	}
}

func TestClipReadNotTermuxSkipsTermuxGet(t *testing.T) {
	var runs []fakeRun
	h := fakeClipHost("linux", map[string]string{"DISPLAY": ":0"}, []string{"termux-clipboard-get"},
		map[string]string{"termux-clipboard-get": "x"}, &runs)
	if _, ok := h.termuxRead(); ok || len(runs) != 0 {
		t.Fatalf("termuxRead ran on desktop linux: ok=%v runs=%v", ok, runs)
	}
}

func TestClipHelpTermuxMissingAPI(t *testing.T) {
	h := fakeClipHost("android", nil, nil, nil, nil)
	help := h.help()
	for _, want := range []string{"Termux:API", "pkg install termux-api"} {
		if !strings.Contains(help, want) {
			t.Errorf("help = %q, want it to mention %q", help, want)
		}
	}
}

func TestClipHelpDesktopUnchanged(t *testing.T) {
	h := fakeClipHost("linux", map[string]string{"DISPLAY": ":0"}, nil, nil, nil)
	if got := h.help(); got != clipboardHelp {
		t.Fatalf("help = %q, want the desktop help", got)
	}
}

func TestClipWatcherSkippedOnTermux(t *testing.T) {
	// No polling of termux-clipboard-get: it starts a Java call each time
	// and would drain the battery.
	if fakeClipHost("android", nil, []string{"termux-clipboard-get"}, nil, nil).watch() {
		t.Fatal("watch() = true on Termux, want false")
	}
	if !fakeClipHost("linux", map[string]string{"DISPLAY": ":0"}, nil, nil, nil).watch() {
		t.Fatal("watch() = false on desktop linux, want true")
	}
}
