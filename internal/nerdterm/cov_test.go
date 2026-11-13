package nerdterm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestInfoKnownAndDescribe(t *testing.T) {
	cases := []struct {
		info  Info
		known bool
		want  string
	}{
		{Info{Terminal: Kitty, Builtin: true}, true, "kitty — Nerd symbols built in"},
		{Info{}, false, "unknown terminal — font not readable"},
		{Info{Terminal: Foot}, false, "foot — font not readable"},
		{Info{Terminal: Foot, Source: "over SSH"}, false, "foot — font not readable (over SSH)"},
		{Info{Terminal: Foot, Font: "Mono"}, true, "foot: Mono — not a Nerd Font"},
		{Info{Terminal: Foot, Font: "Hack NF", Nerd: true, Source: "/f.ini"}, true, "foot: Hack NF (/f.ini) — a Nerd Font"},
	}
	for _, c := range cases {
		if c.info.Known() != c.known || c.info.Describe() != c.want {
			t.Errorf("%+v: known=%v describe=%q", c.info, c.info.Known(), c.info.Describe())
		}
	}
}

// Nil Env fields use the real system: env, file read and file stat.
func TestEnvWithDefaultsUsesTheRealSystem(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "x-test")
	e := Env{}.withDefaults()
	if e.Getenv("TERM_PROGRAM") != "x-test" {
		t.Fatal("Getenv is not os.Getenv")
	}
	if _, err := e.ReadFile("/definitely/not/here"); err == nil {
		t.Fatal("ReadFile is not os.ReadFile")
	}
	if _, ok := e.Stat("/definitely/not/here"); ok {
		t.Fatal("Stat found a missing file")
	}
	if mt, ok := e.Stat(t.TempDir()); !ok || mt.IsZero() {
		t.Fatal("Stat did not find a real folder")
	}
	if e.Gsettings == nil {
		t.Fatal("no Gsettings default")
	}
}

// The default gsettings runner returns an error (not a hang) when the
// tool is missing or the call fails.
func TestRunGsettingsDefault(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := runGsettings(ctx, "get", "a", "b"); err == nil {
		t.Fatal("expected an error without gsettings on PATH")
	}
}

func TestBundlesNerdSymbolsByTERM(t *testing.T) {
	for term, want := range map[string]string{
		"xterm-kitty": Kitty, "xterm-ghostty": Ghostty, "wezterm": WezTerm, "xterm": "",
	} {
		if got := BundlesNerdSymbols(envOf(map[string]string{"TERM": term})); got != want {
			t.Errorf("TERM=%s: %q, want %q", term, got, want)
		}
	}
}

// foot: the first config path is missing, the second is read; a file with
// no font line falls back to monospace and says where it looked.
func TestDetectFootPathFallbackAndDefaults(t *testing.T) {
	env := map[string]string{"TERM": "foot", "XDG_CONFIG_HOME": "/x", "HOME": "/h"}
	files := map[string]string{"/h/.config/foot/foot.ini": "[main]\nfont=Hack Nerd Font:size=11, Noto\n[colors]\nfont=ignored\n"}
	info := Detect(Env{Getenv: envOf(env), ReadFile: fakeFiles(files)})
	if info.Font != "Hack Nerd Font, Noto" || !info.Nerd || info.Source != "/h/.config/foot/foot.ini" {
		t.Fatalf("%+v", info)
	}
	files = map[string]string{"/x/foot/foot.ini": "# nothing\n"}
	info = Detect(Env{Getenv: envOf(env), ReadFile: fakeFiles(files)})
	if info.Font != "monospace" || !strings.HasSuffix(info.Source, ", default") {
		t.Fatalf("%+v", info)
	}
	info = Detect(Env{Getenv: envOf(env), ReadFile: fakeFiles(nil)})
	if info.Font != "monospace" || info.Source != "foot default" {
		t.Fatalf("%+v", info)
	}
}

func TestInlineFieldMissingKey(t *testing.T) {
	if inlineField(`{ family = "A", style = "B" }`, "size") != "" {
		t.Fatal("found a key that is not there")
	}
	if inlineField(`{ family = "A" }`, "family") != "A" {
		t.Fatal("lost a key that is there")
	}
}

// The stamp of Alacritty lists its config files; over SSH it says so; a
// missing file shows as "-".
func TestStampAlacrittyAndSSH(t *testing.T) {
	stat := func(string) (time.Time, bool) { return time.Time{}, false }
	got := Stamp(Env{Getenv: envOf(map[string]string{"TERM": "alacritty", "HOME": "/h", "SSH_TTY": "/dev/pts/1"}), Stat: stat})
	if !strings.HasPrefix(got, Alacritty+";ssh;") || !strings.Contains(got, "alacritty.toml=-") {
		t.Fatalf("stamp %q", got)
	}
}

func TestDetectGnomeGsettingsError(t *testing.T) {
	gs := func(context.Context, ...string) (string, error) { return "", errors.New("no") }
	info := Detect(Env{Getenv: envOf(map[string]string{"VTE_VERSION": "1"}), Gsettings: gs})
	if info.Terminal != GNOME || info.Nerd {
		t.Fatalf("%+v", info)
	}
}
