package setup

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/nerdterm"
)

const testProfileID = "b1dcc9dd"

// fakeGNOME makes termEnv a GNOME Terminal whose font is *font (system
// monospace font). It returns the gsettings call counter and a setter
// for the dconf mtime (the cache key).
// calls counts gsettings runs; detection runs them in parallel, so the
// counter is atomic.
func fakeGNOME(t *testing.T, font *string) (calls *atomic.Int32, touch func(time.Time)) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	old := termEnv
	t.Cleanup(func() { termEnv = old })
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	var n atomic.Int32
	mtime := time.Unix(100, 0)
	termEnv = nerdterm.Env{
		Getenv: func(k string) string {
			switch k {
			case "VTE_VERSION":
				return "7600"
			case "HOME":
				return "/h"
			}
			return ""
		},
		ReadFile: func(string) ([]byte, error) { return nil, errors.New("no file") },
		Stat:     func(string) (time.Time, bool) { return mtime, true },
		Gsettings: func(_ context.Context, args ...string) (string, error) {
			n.Add(1)
			switch args[2] {
			case "default":
				return "'" + testProfileID + "'", nil
			case "use-system-font":
				return "true", nil
			case "monospace-font-name":
				return "'" + *font + "'", nil
			}
			return "", errors.New("no key")
		},
	}
	return &n, func(t time.Time) { mtime = t }
}

func TestDetectTerminalFontCache(t *testing.T) {
	font := "Ubuntu Mono 11"
	calls, touch := fakeGNOME(t, &font)
	if got := DetectTerminalFont(false); got.Nerd || got.Font != "Ubuntu Mono 11" {
		t.Fatalf("first: %+v", got)
	}
	first := calls.Load()
	font = "JetBrainsMono Nerd Font 11"
	if got := DetectTerminalFont(false); got.Nerd || calls.Load() != first {
		t.Fatalf("cached call ran gsettings (%d -> %d) or changed: %+v", first, calls.Load(), got)
	}
	// A settings change rewrites dconf's user file: the cache is stale.
	touch(time.Unix(200, 0))
	if got := DetectTerminalFont(false); !got.Nerd || got.Font != "JetBrainsMono Nerd Font 11" {
		t.Fatalf("after dconf change: %+v", got)
	}
	font = "Ubuntu Mono 11"
	if got := DetectTerminalFont(true); got.Nerd {
		t.Fatalf("fresh: want Ubuntu Mono, got %+v", got)
	}
}

func TestTerminalFontCheck(t *testing.T) {
	plain := nerdterm.Info{Terminal: nerdterm.GNOME, Font: "Ubuntu Mono 11", Source: "system monospace font", System: true}
	c := TerminalFontCheck(plain, "JetBrainsMono Nerd Font")
	if c.Status != Warn || c.Name != "terminal font" {
		t.Fatalf("plain font: %+v", c)
	}
	for _, w := range []string{"Ubuntu Mono 11", "not a Nerd Font", "Custom font", "JetBrainsMono Nerd Font"} {
		if !strings.Contains(c.Detail, w) {
			t.Errorf("detail %q missing %q", c.Detail, w)
		}
	}
	nerd := nerdterm.Info{Terminal: nerdterm.GNOME, Font: "JetBrainsMono Nerd Font 11", Nerd: true}
	if c := TerminalFontCheck(nerd, "x"); c.Status != OK || !strings.Contains(c.Detail, "a Nerd Font") {
		t.Fatalf("nerd font: %+v", c)
	}
	if c := TerminalFontCheck(nerdterm.Info{}, "x"); c.Status != Warn || !strings.Contains(c.Detail, "unknown") {
		t.Fatalf("unknown: %+v", c)
	}
}

func TestPrintTerminalFont(t *testing.T) {
	var b bytes.Buffer
	printTerminalFont(&b, nerdterm.Info{Terminal: nerdterm.GNOME, Font: "Ubuntu Mono 11", System: true}, "JetBrainsMono Nerd Font")
	out := b.String()
	for _, w := range []string{"terminal font", "Ubuntu Mono 11", "Preferences", `"JetBrainsMono Nerd Font"`} {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
	b.Reset()
	printTerminalFont(&b, nerdterm.Info{Terminal: nerdterm.GNOME, Font: "JetBrainsMono Nerd Font 11", Nerd: true}, "JetBrainsMono Nerd Font")
	if out := b.String(); !strings.Contains(out, "✓") || strings.Contains(out, "Preferences") {
		t.Errorf("nerd font: no steps wanted:\n%s", out)
	}
}

// The Doctor in unit tests must not see the real machine's terminal.
func TestDoctorTerminalFontRowIsFake(t *testing.T) {
	for _, c := range Doctor() {
		if c.Name == "terminal font" {
			if !strings.Contains(c.Detail, "unknown terminal") {
				t.Fatalf("Doctor read the real terminal: %q", c.Detail)
			}
			return
		}
	}
	t.Fatal("Doctor has no terminal font row")
}
