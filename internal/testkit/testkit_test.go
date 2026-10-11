package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestFrameProblems(t *testing.T) {
	good := "\x1b[38;2;1;2;3mab\x1b[0m界\n\x1b]8;;http://x\x07link\x1b]8;;\x07"
	if p := FrameProblems(good, 4, 2); p != nil {
		t.Fatalf("good frame: %v", p)
	}
	cases := map[string]string{
		"rows":      "abcd",
		"width":     "abc\nabcd",
		"utf8":      "ab\xe7\x95\nabcd",
		"cursor":    "ab\x1b[2Jcd\nabcd",
		"tab":       "ab\tc\nabcd",
		"osc52":     "abcd\x1b]52;c;eA==\x07\nabcd",
		"apc":       "abcd\x1b_Xfoo\x1b\\\nabcd",
		"lone-esc":  "abcd\x1b\nabcd",
		"cr":        "abc\r\nabcd",
		"c1":        "abc\u0085\nabcd",
		"wide-over": "abc界\nabcd",
	}
	for name, f := range cases {
		if p := FrameProblems(f, 4, 2); len(p) == 0 {
			t.Errorf("%s: no problem found in %q", name, f)
		}
	}
}

func TestKeyMsg(t *testing.T) {
	for _, s := range []string{"ctrl+p", "enter", "f5", "shift+tab", "alt+x", "alt+ctrl+x", "a", "esc", "up"} {
		if got := KeyMsg(s).String(); got != s {
			t.Errorf("KeyMsg(%q).String() = %q", s, got)
		}
	}
}

func TestDriverSandboxAndSteps(t *testing.T) {
	realCfg, _ := os.UserConfigDir()
	d := New(t, Options{W: 100, H: 30, Git: true})
	if cfg := os.Getenv("XDG_CONFIG_HOME"); cfg == "" || strings.HasPrefix(cfg, realCfg) && realCfg != "" {
		t.Fatalf("config dir not sandboxed: %q", cfg)
	}
	if _, err := os.Stat(filepath.Join(d.Dir, ".git")); err != nil {
		t.Fatalf("no git repo: %v", err)
	}
	if got := strings.Count(d.Frame(), "\n") + 1; got != 30 {
		t.Fatalf("frame has %d rows", got)
	}
	d.Action("view:git")
	if !d.WaitForText("feature", 3*time.Second) {
		t.Errorf("Source Control did not show the branch graph:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("f1")
	if !strings.Contains(ansi.Strip(d.Frame()), "Command Palette") {
		t.Errorf("F1 did not open the palette:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("esc")
	d.Resize(60, 20)
	d.Move(10, 10)
	d.Wheel(10, 10, false)
	d.Click(5, 5)
	d.Palette("toggle-terminal")
	if len(d.History()) < 8 {
		t.Errorf("history too short: %v", d.History())
	}
}

func TestDriverWithNvim(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 100, H: 30, Nvim: true})
	d.Action("open:main.go")
	if !d.WaitForText("fmt.Println", 5*time.Second) {
		t.Errorf("main.go not shown:\n%s", ansi.Strip(d.Frame()))
	}
}
