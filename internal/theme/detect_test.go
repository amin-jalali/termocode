package theme

import (
	"testing"

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
		{"auto", true, no, nil, IconModeNerdFont},
		{"auto", false, yes, nil, IconModeUnicode},
		{"auto", false, nil, nil, IconModeUnicode},
		{"auto", false, no, nil, IconModeASCII},
		{"auto", false, nil, map[string]string{"TERM": "linux"}, IconModeASCII},
		{"auto", false, nil, map[string]string{"LANG": "C"}, IconModeASCII},
		{"auto", false, nil, map[string]string{"LANG": "en_US.UTF-8"}, IconModeUnicode},
		{"auto", false, nil, map[string]string{"LC_ALL": "de_DE.utf8", "LANG": "C"}, IconModeUnicode},
		{"", true, nil, map[string]string{"SSH_TTY": "x"}, IconModeUnicode},
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
