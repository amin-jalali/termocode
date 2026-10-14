package theme

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/amin-jalali/termocode/internal/nerdterm"

	"github.com/muesli/termenv"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestResolveIconMode(t *testing.T) {
	yes := func() bool { return true }
	no := func() bool { return false }
	cases := []struct {
		setting string
		nerd    bool
		uni     func() bool
		env     map[string]string
		want    IconMode
	}{
		// nerd = the terminal's configured font is a Nerd Font
		// (nerdterm.Detect), not just "one is installed".
		{"auto", true, nil, nil, IconModeNerdFont},
		{"auto", true, nil, map[string]string{"TERM": "linux"}, IconModeASCII},
		{"auto", false, nil, map[string]string{"TERM_PROGRAM": "WezTerm"}, IconModeNerdFont},
		{"auto", false, nil, map[string]string{"TERM": "xterm-kitty"}, IconModeNerdFont},
		{"auto", false, nil, map[string]string{"TERM_PROGRAM": "ghostty"}, IconModeNerdFont},
		{"auto", false, yes, nil, IconModeUnicode},
		{"auto", false, nil, nil, IconModeUnicode},
		{"auto", false, no, nil, IconModeASCII},
		{"auto", false, nil, map[string]string{"TERM": "linux"}, IconModeASCII},
		{"auto", false, nil, map[string]string{"LANG": "C"}, IconModeASCII},
		{"auto", false, nil, map[string]string{"LANG": "en_US.UTF-8"}, IconModeUnicode},
		{"auto", false, nil, map[string]string{"LC_ALL": "de_DE.utf8", "LANG": "C"}, IconModeUnicode},
		{"", false, nil, map[string]string{"SSH_TTY": "x"}, IconModeUnicode},
		{"ascii", true, yes, nil, IconModeASCII},
		{"unicode", false, no, nil, IconModeUnicode},
		{"nerd", false, no, nil, IconModeNerdFont},
		{"ascii", false, no, map[string]string{"TERMOCODE_NERD_FONT": "1"}, IconModeNerdFont},
		{"nerd", false, no, map[string]string{"TERMOCODE_ICON_MODE": "ascii"}, IconModeASCII},
	}
	for i, c := range cases {
		if got := ResolveIconMode(c.setting, c.nerd, c.uni, envOf(c.env)); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

// The GNOME case that showed hex boxes: a Nerd Font is installed but the
// profile draws with the system font (Ubuntu Mono). auto must stay
// Unicode there and switch to Nerd once the profile uses the Nerd Font.
func TestAutoIconsFollowGNOMEFont(t *testing.T) {
	const id = "b1dcc9dd"
	prof := "org.gnome.Terminal.Legacy.Profile:/org/gnome/terminal/legacy/profiles:/:" + id + "/"
	env := map[string]string{"GNOME_TERMINAL_SCREEN": "/org/gnome/Terminal/screen/1", "VTE_VERSION": "7600", "LANG": "en_US.UTF-8"}
	detect := func(useSystem, font string) nerdterm.Info {
		keys := map[string]string{
			"org.gnome.Terminal.ProfilesList default":         "'" + id + "'",
			prof + " use-system-font":                         useSystem,
			prof + " font":                                    "'" + font + "'",
			"org.gnome.desktop.interface monospace-font-name": "'Ubuntu Mono 11'",
		}
		return nerdterm.Detect(nerdterm.Env{
			Getenv:   envOf(env),
			ReadFile: func(string) ([]byte, error) { return nil, os.ErrNotExist },
			Gsettings: func(_ context.Context, a ...string) (string, error) {
				if v, ok := keys[a[1]+" "+a[2]]; ok {
					return v, nil
				}
				return "", errors.New("no key")
			},
		})
	}
	for _, tc := range []struct {
		useSystem, font string
		want            IconMode
	}{
		{"true", "Monospace 12", IconModeUnicode},                 // system font: Ubuntu Mono 11
		{"false", "Ubuntu Mono 11", IconModeUnicode},              // custom, not Nerd
		{"false", "JetBrainsMono Nerd Font 11", IconModeNerdFont}, // custom Nerd Font
	} {
		info := detect(tc.useSystem, tc.font)
		if got := ResolveIconMode("auto", info.Nerd, nil, envOf(env)); got != tc.want {
			t.Errorf("use-system-font=%s font=%q (%+v): got %v want %v", tc.useSystem, tc.font, info, got, tc.want)
		}
	}
}

func TestDetectProfile(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want termenv.Profile
	}{
		{nil, termenv.TrueColor},
		{map[string]string{"TERM": "xterm-256color"}, termenv.TrueColor},
		{map[string]string{"TERM": "linux"}, termenv.ANSI},
		{map[string]string{"TERM": "dumb"}, termenv.Ascii},
		{map[string]string{"NO_COLOR": "1"}, termenv.Ascii},
		{map[string]string{"TERMOCODE_COLORS": "256"}, termenv.ANSI256},
		{map[string]string{"TERMOCODE_COLORS": "16", "TERM": "xterm-kitty"}, termenv.ANSI},
	}
	for i, c := range cases {
		if got := DetectProfile(envOf(c.env)); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}
