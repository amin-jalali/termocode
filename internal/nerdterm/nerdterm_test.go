package nerdterm

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"
)

const gnomeID = "b1dcc9dd-5262-4d8d-a863-c897e6d979b9"

const testProfile = "org.gnome.Terminal.Legacy.Profile:/org/gnome/terminal/legacy/profiles:/:" + gnomeID + "/"

// fakeGS answers gsettings calls from a map "schema key" -> output.
func fakeGS(m map[string]string, calls *int) func(context.Context, ...string) (string, error) {
	return func(_ context.Context, args ...string) (string, error) {
		if calls != nil {
			*calls++
		}
		if len(args) != 3 || args[0] != "get" {
			return "", errors.New("bad args")
		}
		if v, ok := m[args[1]+" "+args[2]]; ok {
			return v + "\n", nil
		}
		return "", errors.New("No such schema")
	}
}

func gnomeKeys(useSystem, font, mono string) map[string]string {
	return map[string]string{
		"org.gnome.Terminal.ProfilesList default":         "'" + gnomeID + "'",
		testProfile + " use-system-font":                  useSystem,
		testProfile + " font":                             "'" + font + "'",
		"org.gnome.desktop.interface monospace-font-name": "'" + mono + "'",
	}
}

func fakeFiles(m map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if s, ok := m[p]; ok {
			return []byte(s), nil
		}
		return nil, fs.ErrNotExist
	}
}

func TestDetect(t *testing.T) {
	home := map[string]string{"HOME": "/h"}
	with := func(base map[string]string, kv ...string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}
		return out
	}
	cases := []struct {
		name     string
		env      map[string]string
		gs       map[string]string
		files    map[string]string
		terminal string
		font     string
		nerd     bool
	}{
		{"GNOME system font Ubuntu Mono", with(home, "GNOME_TERMINAL_SCREEN", "/x", "VTE_VERSION", "7600"),
			gnomeKeys("true", "Monospace 12", "Ubuntu Mono 11"), nil, GNOME, "Ubuntu Mono 11", false},
		{"GNOME custom Nerd font", with(home, "GNOME_TERMINAL_SCREEN", "/x"),
			gnomeKeys("false", "JetBrainsMono Nerd Font 11", "Ubuntu Mono 11"), nil, GNOME, "JetBrainsMono Nerd Font 11", true},
		{"GNOME system font is a Nerd Font", with(home, "VTE_VERSION", "7600"),
			gnomeKeys("true", "Monospace 12", "FiraCode NFM 10"), nil, GNOME, "FiraCode NFM 10", true},
		{"GNOME custom plain font", with(home, "VTE_VERSION", "7600"),
			gnomeKeys("false", "DejaVu Sans Mono 12", "Hack Nerd Font 11"), nil, GNOME, "DejaVu Sans Mono 12", false},
		{"GNOME without gsettings schema", with(home, "VTE_VERSION", "7600"),
			nil, nil, GNOME, "", false},
		{"GNOME over SSH: remote settings are not the terminal's", with(home, "VTE_VERSION", "7600", "SSH_CONNECTION", "1 2 3 4"),
			gnomeKeys("false", "JetBrainsMono Nerd Font 11", ""), nil, GNOME, "", false},
		{"kitty bundles the symbols", with(home, "TERM", "xterm-kitty"), nil, nil, Kitty, "", true},
		{"WezTerm bundles the symbols", with(home, "TERM_PROGRAM", "WezTerm"), nil, nil, WezTerm, "", true},
		{"Ghostty bundles the symbols", with(home, "TERM_PROGRAM", "ghostty"), nil, nil, Ghostty, "", true},
		{"kitty wins over a VTE parent", with(home, "TERM", "xterm-kitty", "VTE_VERSION", "7600"), nil, nil, Kitty, "", true},
		{"Alacritty Nerd (table)", with(home, "ALACRITTY_WINDOW_ID", "1"), nil,
			map[string]string{"/h/.config/alacritty/alacritty.toml": "[font]\nsize = 11\n\n[font.normal]\nfamily = \"JetBrainsMono Nerd Font\"\nstyle = \"Regular\"\n"},
			Alacritty, "JetBrainsMono Nerd Font", true},
		{"Alacritty inline table, XDG_CONFIG_HOME", with(home, "TERM", "alacritty", "XDG_CONFIG_HOME", "/c"), nil,
			map[string]string{"/c/alacritty/alacritty.toml": "[font]\nnormal = { family = \"Ubuntu Mono\", style = \"Regular\" }\n"},
			Alacritty, "Ubuntu Mono", false},
		{"Alacritty no config: default monospace", with(home, "ALACRITTY_SOCKET", "/s"), nil, nil, Alacritty, "monospace", false},
		{"foot Nerd with size", with(home, "TERM", "foot"), nil,
			map[string]string{"/h/.config/foot/foot.ini": "# comment\nfont=Hack Nerd Font Mono:size=11\n[colors]\nfont=nope\n"},
			Foot, "Hack Nerd Font Mono", true},
		{"foot Nerd fallback", with(home, "TERM", "foot-extra"), nil,
			map[string]string{"/h/.config/foot/foot.ini": "[main]\nfont = Ubuntu Mono:size=12, Symbols Nerd Font Mono\n"},
			Foot, "Ubuntu Mono, Symbols Nerd Font Mono", true},
		{"foot plain", with(home, "TERM", "foot"), nil,
			map[string]string{"/h/.config/foot/foot.ini": "[main]\nfont=DejaVu Sans Mono:size=12\n"},
			Foot, "DejaVu Sans Mono", false},
		{"unknown terminal", with(home, "TERM", "xterm-256color"), nil, nil, "", "", false},
		// Terminal R7: other VTE terminals keep their font elsewhere;
		// GNOME Terminal's settings say nothing about them.
		{"Tilix is not GNOME Terminal", with(home, "VTE_VERSION", "7600", "TILIX_ID", "x"),
			gnomeKeys("false", "JetBrainsMono Nerd Font 11", ""), nil, "", "", false},
		{"Terminator is not GNOME Terminal", with(home, "VTE_VERSION", "7600", "TERMINATOR_UUID", "urn:x"),
			gnomeKeys("false", "JetBrainsMono Nerd Font 11", ""), nil, "", "", false},
		{"Ptyxis is not GNOME Terminal", with(home, "VTE_VERSION", "7800", "PTYXIS_VERSION", "47"),
			gnomeKeys("false", "JetBrainsMono Nerd Font 11", ""), nil, "", "", false},
		{"GNOME Terminal inside a Tilix-started shell", with(home, "VTE_VERSION", "7600", "TILIX_ID", "x", "GNOME_TERMINAL_SCREEN", "/y"),
			gnomeKeys("false", "JetBrainsMono Nerd Font 11", ""), nil, GNOME, "JetBrainsMono Nerd Font 11", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Detect(Env{Getenv: envOf(c.env), ReadFile: fakeFiles(c.files), Gsettings: fakeGS(c.gs, nil)})
			if got.Terminal != c.terminal || got.Font != c.font || got.Nerd != c.nerd {
				t.Errorf("got %+v; want terminal=%q font=%q nerd=%v", got, c.terminal, c.font, c.nerd)
			}
		})
	}
}

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// The profile list is used when no default profile is set.
func TestDetectGNOMEFirstProfile(t *testing.T) {
	gs := gnomeKeys("false", "JetBrainsMono Nerd Font 11", "")
	delete(gs, "org.gnome.Terminal.ProfilesList default")
	gs["org.gnome.Terminal.ProfilesList list"] = "['" + gnomeID + "', 'other']"
	got := Detect(Env{Getenv: envOf(map[string]string{"VTE_VERSION": "1"}), Gsettings: fakeGS(gs, nil)})
	if !got.Nerd || got.Font != "JetBrainsMono Nerd Font 11" {
		t.Fatalf("got %+v", got)
	}
}

// A hung gsettings must not stall startup: Detect gives up at its deadline.
func TestDetectGNOMETimeout(t *testing.T) {
	slow := func(ctx context.Context, _ ...string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	start := time.Now()
	got := Detect(Env{Getenv: envOf(map[string]string{"VTE_VERSION": "1"}), Gsettings: slow})
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("Detect took %v", d)
	}
	if got.Nerd || got.Font != "" || got.Terminal != GNOME {
		t.Fatalf("got %+v", got)
	}
}

// Kitty / unknown terminals need no gsettings call.
func TestDetectNoGsettingsOutsideVTE(t *testing.T) {
	n := 0
	Detect(Env{Getenv: envOf(map[string]string{"TERM": "xterm-kitty"}), Gsettings: fakeGS(nil, &n)})
	Detect(Env{Getenv: envOf(map[string]string{"TERM": "xterm"}), Gsettings: fakeGS(nil, &n)})
	if n != 0 {
		t.Fatalf("gsettings ran %d times", n)
	}
}

func TestIsNerdFamily(t *testing.T) {
	for name, want := range map[string]bool{
		"JetBrainsMono Nerd Font 11":   true,
		"JetBrainsMono Nerd Font Mono": true,
		"Symbols Nerd Font":            true,
		"FiraCode NF":                  true,
		"FiraCode NFM Bold 10":         true,
		"Hack NFP":                     true,
		"Ubuntu Mono 11":               false,
		"Monospace":                    false,
		"Nerdy Sans":                   false,
		"INFO Mono":                    false,
		"":                             false,
	} {
		if got := IsNerdFamily(name); got != want {
			t.Errorf("IsNerdFamily(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestStamp(t *testing.T) {
	mt := map[string]time.Time{"/h/.config/dconf/user": time.Unix(100, 0)}
	stat := func(p string) (time.Time, bool) { v, ok := mt[p]; return v, ok }
	e := Env{Getenv: envOf(map[string]string{"HOME": "/h", "VTE_VERSION": "1"}), Stat: stat}
	a := Stamp(e)
	mt["/h/.config/dconf/user"] = time.Unix(200, 0)
	if b := Stamp(e); a == b || !strings.Contains(b, GNOME) {
		t.Fatalf("stamp did not change with dconf: %q -> %q", a, b)
	}
	k := Stamp(Env{Getenv: envOf(map[string]string{"TERM": "foot"}), Stat: stat})
	if k == a || !strings.Contains(k, Foot) {
		t.Fatalf("foot stamp %q", k)
	}
}

func TestFixHint(t *testing.T) {
	for _, tc := range []struct {
		info Info
		want []string
	}{
		{Info{Terminal: GNOME, Font: "Ubuntu Mono 11", System: true}, []string{"Preferences", "Custom font", `"JetBrainsMono Nerd Font"`}},
		{Info{Terminal: Alacritty, Font: "monospace"}, []string{"alacritty.toml", "[font.normal]", `family = "JetBrainsMono Nerd Font"`}},
		{Info{Terminal: Foot, Font: "monospace"}, []string{"foot.ini", "font=JetBrainsMono Nerd Font"}},
		{Info{}, []string{"JetBrainsMono Nerd Font"}},
	} {
		got := strings.Join(FixHint(tc.info, "JetBrainsMono Nerd Font"), "\n")
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("FixHint(%+v) missing %q:\n%s", tc.info, w, got)
			}
		}
	}
	if FixHint(Info{Terminal: Kitty, Nerd: true}, "X") != nil {
		t.Error("no hint wanted when the terminal already draws Nerd glyphs")
	}
}
