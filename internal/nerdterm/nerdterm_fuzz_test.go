package nerdterm

import (
	"context"
	"testing"
	"time"
)

// FuzzDetect lets every terminal config file and every gsettings answer
// be any text. Detect never panics, and Nerd follows the font found.
func FuzzDetect(f *testing.F) {
	for _, s := range []string{
		"[font]\nnormal = { family = \"JetBrainsMono Nerd Font\" }\n",
		"[main]\nfont=Hack Nerd Font Mono:size=11\n",
		"'JetBrainsMono Nerd Font 11'",
		"['b1dcc9dd-5262-4d8d-a863-c897e6d979b9']",
		"font:\n  normal:\n    family: x",
		"",
	} {
		for term := uint8(0); term < 3; term++ {
			f.Add(s, term)
		}
	}
	f.Fuzz(func(t *testing.T, text string, term uint8) {
		env := map[string]string{"HOME": "/home/u", "XDG_CONFIG_HOME": "/home/u/.config"}
		switch term % 3 {
		case 0:
			env["TERM"] = "alacritty"
		case 1:
			env["TERM"] = "foot"
		default:
			env["VTE_VERSION"] = "7000"
		}
		info := Detect(Env{
			Getenv:    func(k string) string { return env[k] },
			ReadFile:  func(string) ([]byte, error) { return []byte(text), nil },
			Gsettings: func(context.Context, ...string) (string, error) { return text, nil },
			Stat:      func(string) (time.Time, bool) { return time.Time{}, true },
		})
		if info.Nerd != IsNerdFamily(info.Font) {
			t.Fatalf("Nerd %v for font %q", info.Nerd, info.Font)
		}
		_ = FixHint(info, "Nerd")
	})
}
