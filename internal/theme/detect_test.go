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
		uni     func() bool
		env     map[string]string
		want    IconMode
	}{
		{"auto", nil, map[string]string{"TERM_PROGRAM": "WezTerm"}, IconModeNerdFont},
		{"auto", nil, map[string]string{"TERM": "xterm-kitty"}, IconModeNerdFont},
		{"auto", nil, map[string]string{"TERM_PROGRAM": "ghostty"}, IconModeNerdFont},
		{"auto", yes, nil, IconModeUnicode},
		{"auto", nil, nil, IconModeUnicode},
		{"auto", no, nil, IconModeASCII},
		{"auto", nil, map[string]string{"TERM": "linux"}, IconModeASCII},
		{"auto", nil, map[string]string{"LANG": "C"}, IconModeASCII},
		{"auto", nil, map[string]string{"LANG": "en_US.UTF-8"}, IconModeUnicode},
		{"auto", nil, map[string]string{"LC_ALL": "de_DE.utf8", "LANG": "C"}, IconModeUnicode},
		{"", nil, map[string]string{"SSH_TTY": "x"}, IconModeUnicode},
		{"ascii", yes, nil, IconModeASCII},
		{"unicode", no, nil, IconModeUnicode},
		{"nerd", no, nil, IconModeNerdFont},
		{"ascii", no, map[string]string{"TERMOCODE_NERD_FONT": "1"}, IconModeNerdFont},
		{"nerd", no, map[string]string{"TERMOCODE_ICON_MODE": "ascii"}, IconModeASCII},
	}
	for i, c := range cases {
		if got := ResolveIconMode(c.setting, c.uni, envOf(c.env)); got != c.want {
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
