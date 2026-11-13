package computeruse

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRun records the programs a driver starts.
type fakeRun struct {
	calls []string
	err   error
}

func (f *fakeRun) run(_ context.Context, name string, args ...string) error {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return f.err
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func have(names ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, h := range names {
			if h == n {
				return "/usr/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestDetectX11(t *testing.T) {
	f := &fakeRun{}
	d, err := Detect(Env{OS: "linux", Getenv: env(map[string]string{"DISPLAY": ":0"}), LookPath: have("xdotool", "scrot"), Run: f.run})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := d.Screenshot(ctx, "/w/.termocode/screens/s.png"); err != nil {
		t.Fatal(err)
	}
	_ = d.Click(ctx, 10, 20, "left")
	_ = d.Click(ctx, 5, 6, "right")
	_ = d.Type(ctx, "hello world")
	_ = d.Key(ctx, "ctrl+s")
	want := []string{
		"scrot -o /w/.termocode/screens/s.png",
		"xdotool mousemove 10 20 click 1",
		"xdotool mousemove 5 6 click 3",
		"xdotool type --delay 12 -- hello world",
		"xdotool key -- ctrl+s",
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(f.calls, "\n"))
	}
	if !strings.Contains(d.Name(), "X11") {
		t.Fatalf("name %q", d.Name())
	}
}

func TestDetectX11ImportWhenNoScrot(t *testing.T) {
	f := &fakeRun{}
	d, err := Detect(Env{OS: "linux", Getenv: env(map[string]string{"DISPLAY": ":0"}), LookPath: have("xdotool", "import"), Run: f.run})
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Screenshot(context.Background(), "/s.png")
	if f.calls[0] != "import -window root /s.png" {
		t.Fatalf("%q", f.calls)
	}
}

func TestDetectWaylandAndMac(t *testing.T) {
	f := &fakeRun{}
	d, err := Detect(Env{OS: "linux", Getenv: env(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}), LookPath: have("grim", "ydotool", "xdotool", "scrot"), Run: f.run})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = d.Screenshot(ctx, "/s.png")
	_ = d.Click(ctx, 1, 2, "left")
	_ = d.Type(ctx, "hi")
	if strings.Join(f.calls, "|") != "grim /s.png|ydotool mousemove --absolute -x 1 -y 2|ydotool click 0xC0|ydotool type -- hi" {
		t.Fatalf("wayland: %q", f.calls)
	}
	if err := d.Key(ctx, "ctrl+s"); err == nil {
		t.Fatal("ydotool keys need key codes; the driver says it cannot")
	}

	f = &fakeRun{}
	d, err = Detect(Env{OS: "darwin", Getenv: env(nil), LookPath: have("screencapture", "cliclick"), Run: f.run})
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Screenshot(ctx, "/s.png")
	_ = d.Click(ctx, 3, 4, "left")
	_ = d.Type(ctx, "hi")
	_ = d.Key(ctx, "cmd+s")
	if strings.Join(f.calls, "|") != "screencapture -x /s.png|cliclick c:3,4|cliclick t:hi|cliclick kd:cmd t:s ku:cmd" {
		t.Fatalf("mac: %q", f.calls)
	}
}

func TestDetectSaysWhatIsMissing(t *testing.T) {
	_, err := Detect(Env{OS: "linux", Getenv: env(map[string]string{"DISPLAY": ":0"}), LookPath: have(), Run: (&fakeRun{}).run})
	if err == nil || !strings.Contains(err.Error(), "xdotool") {
		t.Fatalf("no tools: %v", err)
	}
	_, err = Detect(Env{OS: "linux", Getenv: env(nil), LookPath: have("xdotool", "scrot"), Run: (&fakeRun{}).run})
	if err == nil || !strings.Contains(err.Error(), "no desktop") {
		t.Fatalf("no display (ssh): %v", err)
	}
	_, err = Detect(Env{OS: "windows", Getenv: env(nil), LookPath: have(), Run: (&fakeRun{}).run})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("windows: %v", err)
	}
	// Screenshots only (no input tool) still works for looking.
	f := &fakeRun{}
	d, err := Detect(Env{OS: "linux", Getenv: env(map[string]string{"DISPLAY": ":0"}), LookPath: have("scrot"), Run: f.run})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Click(context.Background(), 1, 1, "left"); err == nil || !strings.Contains(err.Error(), "xdotool") {
		t.Fatalf("click without xdotool: %v", err)
	}
}

func TestCheckArgs(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{
		{"point ok", CheckPoint(0, 0)},
		{"button ok", CheckButton("middle")},
		{"keys ok", CheckKeys("ctrl+shift+Tab")},
		{"text ok", CheckText("line one\nline two\t!")},
	} {
		if c.err != nil {
			t.Errorf("%s: %v", c.name, c.err)
		}
	}
	for _, c := range []struct {
		name string
		err  error
	}{
		{"negative", CheckPoint(-1, 3)},
		{"huge", CheckPoint(3, MaxCoord+1)},
		{"button", CheckButton("left; rm")},
		{"keys with space", CheckKeys("ctrl s")},
		{"keys empty", CheckKeys("")},
		{"keys option", CheckKeys("--help")},
		{"escape in text", CheckText("a\x1bb")},
		{"long text", CheckText(strings.Repeat("a", MaxText+1))},
		{"empty text", CheckText("")},
	} {
		if c.err == nil {
			t.Errorf("%s: passed", c.name)
		}
	}
}

func TestRunErrorIsReturned(t *testing.T) {
	f := &fakeRun{err: errors.New("exit status 1")}
	d, _ := Detect(Env{OS: "linux", Getenv: env(map[string]string{"DISPLAY": ":0"}), LookPath: have("xdotool", "scrot"), Run: f.run})
	if err := d.Type(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "xdotool") {
		t.Fatalf("%v", err)
	}
}
