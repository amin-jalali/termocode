package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/nerdterm"
)

// TestMain keeps every test (Doctor runs a fresh detection and writes the
// cache) away from the real $XDG_STATE_HOME.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "termocode-setup-test")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_STATE_HOME", dir)
	// No real terminal: the GNOME / Alacritty / foot reads see nothing.
	termEnv = nerdterm.Env{
		Getenv:    func(string) string { return "" },
		ReadFile:  func(string) ([]byte, error) { return nil, os.ErrNotExist },
		Gsettings: func(context.Context, ...string) (string, error) { return "", errors.New("no gsettings in tests") },
		Stat:      func(string) (time.Time, bool) { return time.Time{}, false },
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fakeNerdEnv isolates state + font folders, fakes fc-list and the clock.
// It returns a counter of fc-list runs, a setter for the fc-list output,
// the fake font folder and a func that moves the clock forward.
func fakeNerdEnv(t *testing.T) (calls *int, setOut func(string), fontDir string, advance func(time.Duration)) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fontDir = t.TempDir()
	oldFC, oldDirs, oldNow := fcList, fontDirs, clock.Now
	t.Cleanup(func() { fcList, fontDirs, clock.Now = oldFC, oldDirs, oldNow })

	n := 0
	out := "JetBrainsMono Nerd Font,JetBrainsMono NF\nDejaVu Sans Mono\n"
	fcList = func() (string, error) { n++; return out, nil }
	fontDirs = func() []string { return []string{fontDir} }
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	clock.Now = func() time.Time { return now }
	return &n, func(s string) { out = s }, fontDir, func(d time.Duration) { now = now.Add(d) }
}

func TestHasNerdFontUsesCache(t *testing.T) {
	calls, setOut, _, _ := fakeNerdEnv(t)
	if !HasNerdFont() {
		t.Fatal("first call: want Nerd Font found")
	}
	setOut("DejaVu Sans Mono\n") // would flip the answer if fc-list ran again
	for i := 0; i < 3; i++ {
		if !HasNerdFont() {
			t.Fatal("cached call: want the cached answer (found)")
		}
	}
	if *calls != 1 {
		t.Fatalf("fc-list ran %d times, want 1", *calls)
	}
	if info := DetectNerdFont(false); !info.Cached || info.Family != "JetBrainsMono Nerd Font" {
		t.Fatalf("got %+v, want cached JetBrainsMono Nerd Font", info)
	}
}

func TestHasNerdFontCacheExpires(t *testing.T) {
	calls, setOut, _, advance := fakeNerdEnv(t)
	HasNerdFont()
	setOut("DejaVu Sans Mono\n")
	advance(23 * time.Hour)
	if !HasNerdFont() || *calls != 1 {
		t.Fatalf("within TTL: want cached hit, fc-list calls=%d", *calls)
	}
	advance(2 * time.Hour)
	if HasNerdFont() {
		t.Fatal("after TTL: want fresh answer (not found)")
	}
	if *calls != 2 {
		t.Fatalf("fc-list ran %d times, want 2", *calls)
	}
	// A clock that went backwards does not trust the entry either.
	advance(-48 * time.Hour)
	HasNerdFont()
	if *calls != 3 {
		t.Fatalf("clock skew: fc-list ran %d times, want 3", *calls)
	}
}

func TestHasNerdFontFontDirChangeInvalidates(t *testing.T) {
	calls, setOut, dir, _ := fakeNerdEnv(t)
	setOut("DejaVu Sans Mono\n")
	if HasNerdFont() {
		t.Fatal("want not found")
	}
	// A font install (or fc-cache) changes the folder mtime.
	setOut("Hack Nerd Font Mono\n")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(dir, future, future); err != nil {
		t.Fatal(err)
	}
	info := DetectNerdFont(false)
	if !info.Found || info.Cached || info.Family != "Hack Nerd Font Mono" {
		t.Fatalf("after font dir change: got %+v, want fresh Hack Nerd Font Mono", info)
	}
	if *calls != 2 {
		t.Fatalf("fc-list ran %d times, want 2", *calls)
	}
}

func TestDetectNerdFontFreshSkipsCache(t *testing.T) {
	calls, setOut, _, _ := fakeNerdEnv(t)
	HasNerdFont()
	setOut("")
	if DetectNerdFont(true).Found {
		t.Fatal("fresh: want not found")
	}
	// The fresh answer was written back for the next start.
	if HasNerdFont() || *calls != 2 {
		t.Fatalf("after fresh check: want cached not-found, fc-list calls=%d", *calls)
	}
}

func TestHasNerdFontNoFcList(t *testing.T) {
	calls, _, _, _ := fakeNerdEnv(t)
	fcList = func() (string, error) { *calls++; return "", errors.New("fc-list: not found") }
	if HasNerdFont() || HasNerdFont() {
		t.Fatal("want not found without fc-list")
	}
	if *calls != 1 {
		t.Fatalf("missing fc-list is cached too: calls=%d, want 1", *calls)
	}
}

func TestHasNerdFontCorruptCache(t *testing.T) {
	calls, _, _, _ := fakeNerdEnv(t)
	p := nerdCachePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasNerdFont() || *calls != 1 {
		t.Fatalf("corrupt cache: want fresh detection, calls=%d", *calls)
	}
}

func TestNerdFamily(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"JetBrainsMono Nerd Font,JetBrainsMono NF\n", "JetBrainsMono Nerd Font"},
		{"/home/u/.local/share/fonts/X.ttf: FiraCode Nerd Font:style=Regular\n", "FiraCode Nerd Font"},
		{"DejaVu Sans\nSymbols Nerd Font Mono\n", "Symbols Nerd Font Mono"},
	} {
		got, ok := nerdFamily(tc.in)
		if !ok || got != tc.want {
			t.Errorf("nerdFamily(%q) = %q, %v; want %q", tc.in, got, ok, tc.want)
		}
	}
	if _, ok := nerdFamily("DejaVu Sans\n"); ok {
		t.Error("no Nerd family: want false")
	}
}

func TestNerdFontCheck(t *testing.T) {
	found := NerdFontInfo{Found: true, Family: "JetBrainsMono Nerd Font"}
	for _, tc := range []struct {
		info  NerdFontInfo
		icons string
		want  []string
	}{
		{found, "", []string{"JetBrainsMono Nerd Font (fc-list)"}},
		{found, "unicode", []string{"icons: unicode", "set icons to nerd in Settings"}},
		{found, "nerd", []string{"icons: nerd", "boxes", "set icons to unicode in Settings"}},
		{NerdFontInfo{}, "unicode", []string{"not detected", "icons: unicode", "set icons to ascii"}},
	} {
		c := NerdFontCheck(tc.info, tc.icons)
		for _, w := range tc.want {
			if !strings.Contains(c.Detail, w) {
				t.Errorf("NerdFontCheck(%+v, %q).Detail = %q, missing %q", tc.info, tc.icons, c.Detail, w)
			}
		}
	}
	if NerdFontCheck(NerdFontInfo{}, "").Status != Warn || NerdFontCheck(found, "").Status != OK {
		t.Error("status: want OK when found, Warn otherwise")
	}
}
