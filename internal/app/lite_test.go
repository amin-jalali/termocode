package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/toast"
)

// CMP-99: a light install without the editor. `termocode --lite` (or
// TERMOCODE_LITE=1) runs without nvim on purpose; a machine without
// nvim runs Lite mode by itself instead of an "nvim:" error.
func TestLiteReason(t *testing.T) {
	found := func(string) (string, error) { return "/usr/bin/nvim", nil }
	missing := func(string) (string, error) { return "", errors.New("not found") }
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == EnvLite {
				return v
			}
			return ""
		}
	}
	cases := []struct {
		env  string
		look func(string) (string, error)
		want string
	}{
		{"", found, ""},
		{"0", found, ""},
		{"1", found, liteAsked},
		{"true", found, liteAsked},
		{"", missing, liteNoNvim},
		{"1", missing, liteAsked},
	}
	for _, c := range cases {
		if got := liteReason(env(c.env), c.look); got != c.want {
			t.Errorf("env %q: %q, want %q", c.env, got, c.want)
		}
	}
}

func TestLiteTip(t *testing.T) {
	if liteTipCmd("") != nil {
		t.Error("the full editor got a Lite tip")
	}
	for reason, want := range map[string]string{liteAsked: "Lite mode", liteNoNvim: "Neovim was not found"} {
		cmd := liteTipCmd(reason)
		if cmd == nil {
			t.Fatalf("%s: no tip", reason)
		}
		msg, ok := cmd().(ToastMsg)
		if !ok || msg.Level != toast.Info || !strings.Contains(msg.Title, want) || !strings.Contains(msg.Title, "termocode setup") {
			t.Errorf("%s: tip %+v", reason, msg)
		}
	}
}

func TestLiteModelHasNoNvimError(t *testing.T) {
	t.Setenv("TERMOCODE_NO_NVIM", "")
	t.Setenv(EnvLite, "1")
	m := New()
	if m.nvim != nil || m.err != "" || m.lite != liteAsked {
		t.Errorf("lite model: nvim %v, err %q, lite %q", m.nvim != nil, m.err, m.lite)
	}
}
