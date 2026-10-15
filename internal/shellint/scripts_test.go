package shellint

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// oscEvents cuts every OSC 133 / OSC 7 sequence out of a shell's output.
func oscEvents(out string) []Event {
	var evs []Event
	for {
		i := strings.Index(out, "\x1b]")
		if i < 0 {
			return evs
		}
		out = out[i:]
		j := strings.IndexByte(out, '\a')
		if j < 0 {
			return evs
		}
		if e, ok := Parse(out[:j+1]); ok {
			evs = append(evs, e)
		}
		out = out[j+1:]
	}
}

// runShell starts an interactive shell (stdin is a pipe, so no line
// editor) with the integration, types input and returns all output.
func runShell(t *testing.T, shell string, home string, extraEnv map[string]string, input string) string {
	t.Helper()
	bin, err := spawn.Command("sh", "-c", "command -v "+shell).Output()
	if err != nil || strings.TrimSpace(string(bin)) == "" {
		t.Skipf("%s is not installed", shell)
	}
	dir := t.TempDir()
	if err := Install(dir); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOME": home, "ZDOTDIR": "", "XDG_DATA_DIRS": ""}
	for k, v := range extraEnv {
		env[k] = v
	}
	get := func(k string) string { return env[k] }
	spec, ok := For(strings.TrimSpace(string(bin)), dir, get)
	if !ok {
		t.Fatalf("no spec for %s", shell)
	}
	argv := append(spec.Argv, "-i")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := spawn.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = home
	c.Env = []string{"PATH=" + os.Getenv("PATH"), "TERM=dumb"}
	for k, v := range env {
		if v != "" {
			c.Env = append(c.Env, k+"="+v)
		}
	}
	for k, v := range spec.Env {
		c.Env = append(c.Env, k+"="+v)
	}
	c.Stdin = strings.NewReader(input)
	out, _ := c.CombinedOutput()
	return string(out)
}

func kinds(evs []Event) string {
	var b strings.Builder
	for _, e := range evs {
		b.WriteByte(" ABCD7"[e.Kind])
	}
	return b.String()
}

// Spec (T1): bash with the integration loads the user's ~/.bashrc, and
// reports `false` as command start "false" then end with exit 1, a `cd`
// through OSC 7, and keeps $? for the user's own PROMPT_COMMAND.
func TestBashScript(t *testing.T) {
	home := t.TempDir()
	sub := filepath.Join(home, "my dir")
	_ = os.Mkdir(sub, 0o755)
	_ = os.WriteFile(filepath.Join(home, ".bashrc"),
		[]byte("USER_RC=loaded\nPROMPT_COMMAND='echo user-pc=$?'\n"), 0o644)
	out := runShell(t, "bash", home, nil, "echo rc=$USER_RC\nfalse\ncd 'my dir'\nexit\n")
	if !strings.Contains(out, "rc=loaded") {
		t.Errorf("~/.bashrc was not loaded:\n%q", out)
	}
	if !strings.Contains(out, "user-pc=1") {
		t.Errorf("the user's PROMPT_COMMAND lost $? of `false`:\n%q", out)
	}
	evs := oscEvents(out)
	var cmds []string
	var exits []int
	cwd := ""
	for _, e := range evs {
		switch e.Kind {
		case CommandStart:
			cmds = append(cmds, e.Cmd)
		case CommandEnd:
			exits = append(exits, e.Exit)
		case Cwd:
			cwd = e.Cwd
		}
	}
	if strings.Join(cmds, "|") != "echo rc=$USER_RC|false|cd 'my dir'|exit" {
		t.Errorf("commands %q (events %s)", cmds, kinds(evs))
	}
	if len(exits) < 3 || exits[0] != 0 || exits[1] != 1 || exits[2] != 0 {
		t.Errorf("exit codes %v", exits)
	}
	if cwd != sub {
		t.Errorf("cwd %q, want %q", cwd, sub)
	}
	if strings.Contains(out, "TERMOCODE_SHELL_INTEGRATION=1") {
		t.Error("marker leaked")
	}
}

// Spec (T1): a line that history does not keep (HISTCONTROL=ignorespace)
// starts with an empty command text, not the previous command.
func TestBashScriptIgnoreSpace(t *testing.T) {
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, ".bashrc"), []byte("HISTCONTROL=ignorespace\n"), 0o644)
	out := runShell(t, "bash", home, nil, "echo one\n echo secret\nexit\n")
	var cmds []string
	for _, e := range oscEvents(out) {
		if e.Kind == CommandStart {
			cmds = append(cmds, e.Cmd)
		}
	}
	if len(cmds) < 2 || cmds[0] != "echo one" || cmds[1] != "" {
		t.Errorf("commands %q", cmds)
	}
}

// Spec (T1): zsh with the integration loads the user's .zshenv and
// .zshrc (from $HOME, and from a ZDOTDIR the user set), reports commands
// and exit codes, and gives ZDOTDIR back to programs it starts.
func TestZshScript(t *testing.T) {
	for _, ownZdotdir := range []bool{false, true} {
		home := t.TempDir()
		zd := home
		extra := map[string]string{}
		if ownZdotdir {
			zd = filepath.Join(home, "zdot")
			_ = os.Mkdir(zd, 0o755)
			extra["ZDOTDIR"] = zd
		}
		_ = os.WriteFile(filepath.Join(zd, ".zshenv"), []byte("ZENV=yes\n"), 0o644)
		_ = os.WriteFile(filepath.Join(zd, ".zshrc"), []byte("ZRC=yes\nprecmd() { echo user-precmd=$? }\n"), 0o644)
		out := runShell(t, "zsh", home, extra, "echo env=$ZENV rc=$ZRC zd=${ZDOTDIR-unset}\nfalse\nexit\n")
		wantZD := "unset"
		if ownZdotdir {
			wantZD = zd
		}
		if !strings.Contains(out, "env=yes rc=yes zd="+wantZD) {
			t.Errorf("own ZDOTDIR=%v: user files or ZDOTDIR wrong:\n%q", ownZdotdir, out)
		}
		if !strings.Contains(out, "user-precmd=1") {
			t.Errorf("the user's precmd lost $?:\n%q", out)
		}
		var cmds []string
		var exits []int
		for _, e := range oscEvents(out) {
			switch e.Kind {
			case CommandStart:
				cmds = append(cmds, e.Cmd)
			case CommandEnd:
				exits = append(exits, e.Exit)
			}
		}
		if len(cmds) < 2 || cmds[1] != "false" || len(exits) < 2 || exits[1] != 1 {
			t.Errorf("own ZDOTDIR=%v: commands %q exits %v\n%q", ownZdotdir, cmds, exits, out)
		}
	}
}

// Spec (T1): the shells without a script start as before; missing
// script files mean no integration (never a shell without its rc).
func TestForFallbacks(t *testing.T) {
	dir := t.TempDir()
	get := func(string) string { return "" }
	for _, sh := range []string{"/bin/sh", "/usr/bin/dash", "/usr/bin/nu", ""} {
		if _, ok := For(sh, dir, get); ok {
			t.Errorf("%q got a spec", sh)
		}
	}
	if _, ok := For("/bin/bash", dir, get); ok {
		t.Error("bash got a spec before Install")
	}
	if err := Install(dir); err != nil {
		t.Fatal(err)
	}
	s, ok := For("/bin/bash", dir, get)
	if !ok || len(s.Argv) != 3 || s.Argv[1] != "--init-file" {
		t.Errorf("bash spec %+v", s)
	}
	s, ok = For("/usr/bin/fish", dir, func(k string) string {
		if k == "XDG_DATA_DIRS" {
			return "/opt/share"
		}
		return ""
	})
	if !ok || !strings.HasSuffix(s.Env["XDG_DATA_DIRS"], ":/opt/share") || s.Env["TERMOCODE_XDG_DATA_DIRS"] != "/opt/share" {
		t.Errorf("fish spec %+v", s)
	}
	// Install twice: the second run leaves the files alone.
	p := filepath.Join(dir, "termocode.bash")
	st, _ := os.Stat(p)
	if err := Install(dir); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(p)
	if !st.ModTime().Equal(st2.ModTime()) {
		t.Error("Install rewrote an unchanged file")
	}
}
