package termcaps

import (
	"strings"
	"testing"
)

// §3: the environment names the terminal. TERM_PROGRAM wins over the
// variables a nested terminal inherits (KITTY_WINDOW_ID, WT_SESSION,
// VTE_VERSION leak into VS Code's terminal started from them).
func TestFromEnv(t *testing.T) {
	cases := []struct {
		env  map[string]string
		name string
	}{
		{map[string]string{"KITTY_WINDOW_ID": "1"}, "kitty"},
		{map[string]string{"TERM": "xterm-kitty"}, "kitty"},
		{map[string]string{"TERM_PROGRAM": "ghostty"}, "ghostty"},
		{map[string]string{"TERM": "xterm-ghostty"}, "ghostty"},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, "wezterm"},
		{map[string]string{"TERM": "foot"}, "foot"},
		{map[string]string{"TERM": "foot-extra"}, "foot"},
		{map[string]string{"ALACRITTY_WINDOW_ID": "9"}, "alacritty"},
		{map[string]string{"TERM": "alacritty"}, "alacritty"},
		{map[string]string{"VTE_VERSION": "7800"}, "vte"},
		{map[string]string{"VTE_VERSION": "7800", "TILIX_ID": "x"}, "vte"},
		{map[string]string{"KONSOLE_VERSION": "230804"}, "konsole"},
		{map[string]string{"XTERM_VERSION": "XTerm(390)"}, "xterm"},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, "iterm2"},
		{map[string]string{"LC_TERMINAL": "iTerm2"}, "iterm2"},
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, "apple"},
		{map[string]string{"WT_SESSION": "x"}, "wt"},
		{map[string]string{"TERM_PROGRAM": "vscode", "KITTY_WINDOW_ID": "1"}, "vscode"},
		{map[string]string{"TERM_PROGRAM": "vscode", "VTE_VERSION": "7800"}, "vscode"},
		{map[string]string{"TERMINAL_EMULATOR": "JetBrains-JediTerm", "WT_SESSION": "x"}, "jetbrains"},
		{map[string]string{"TERM_PROGRAM": "WarpTerminal"}, "warp"},
		{map[string]string{"TERM_PROGRAM": "waveterm"}, "wave"},
		{map[string]string{"TERMUX_VERSION": "0.118"}, "termux"},
		{map[string]string{"TERM": "linux"}, "linux"},
		{map[string]string{"TERM": "xterm-256color"}, "unknown"},
		{nil, "unknown"},
	}
	for _, c := range cases {
		if got := FromEnv(envOf(c.env)).Name; got != c.name {
			t.Errorf("%v: %q, want %q", c.env, got, c.name)
		}
	}
	p := FromEnv(envOf(map[string]string{"TERM_PROGRAM": "WezTerm", "TMUX": "/t,1,0", "SSH_TTY": "/dev/pts/3"}))
	if p.Layer != "tmux" || !p.SSH {
		t.Errorf("layer / ssh: %+v", p)
	}
}

// §4: every profile FromEnv can return has a row in the table, and the
// rows say what the plan's table says for the facts code relies on.
func TestProfileTable(t *testing.T) {
	for _, name := range ProfileNames {
		q, ok := profiles[name]
		if !ok {
			t.Errorf("%s: no table row", name)
			continue
		}
		if q.Keys == "" || q.OSC52 == "" || q.Images == "" || q.Sync == "" || q.Bidi == "" {
			t.Errorf("%s: empty field in %+v", name, q)
		}
	}
	for name, want := range map[string]string{"vte": "no", "apple": "no", "xterm": "no", "linux": "no", "jetbrains": "unknown", "kitty": "yes"} {
		if got := profiles[name].OSC52; got != want {
			t.Errorf("%s osc52 = %q, want %q", name, got, want)
		}
	}
	if profiles["konsole"].Bidi != "terminal" {
		t.Error("konsole bidi is not terminal")
	}
	if !strings.Contains(strings.Join(profiles["vscode"].HostKeys, " "), "F1") {
		t.Error("vscode does not list F1 among the keys it takes")
	}
}

// The profile for an unknown terminal is a usable row, not zero values.
func TestUnknownProfile(t *testing.T) {
	p := FromEnv(envOf(nil))
	if p.Quirks.Keys == "" {
		t.Errorf("unknown profile has empty quirks: %+v", p)
	}
	if s := p.String(); !strings.Contains(s, "unknown") {
		t.Errorf("String() = %q", s)
	}
}

// SSH: the variables sshd sets.
func TestSSH(t *testing.T) {
	for _, k := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if !SSH(envOf(map[string]string{k: "x"})) {
			t.Errorf("%s set: SSH false", k)
		}
	}
	if SSH(envOf(nil)) {
		t.Error("no variables: SSH true")
	}
}

// oldOSC52Honoured is internal/app's check before it moved here, copied
// to prove the move changed nothing.
func oldOSC52Honoured(getenv func(string) string) bool {
	if getenv("TMUX") != "" || getenv("STY") != "" {
		return true
	}
	switch {
	case getenv("VTE_VERSION") != "",
		getenv("TERM_PROGRAM") == "Apple_Terminal",
		getenv("XTERM_VERSION") != "",
		getenv("TERM") == "linux",
		getenv("TERMINAL_EMULATOR") == "JetBrains-JediTerm":
		return false
	}
	return true
}

// Every combination of the variables either check reads gives the same
// answer.
func TestOSC52HonouredSameAsBefore(t *testing.T) {
	vars := []struct{ k, v string }{
		{"TMUX", "/t,1,0"}, {"STY", "1.pts"}, {"VTE_VERSION", "7800"},
		{"TERM_PROGRAM", "Apple_Terminal"}, {"XTERM_VERSION", "XTerm(390)"},
		{"TERM", "linux"}, {"TERMINAL_EMULATOR", "JetBrains-JediTerm"},
		{"KITTY_WINDOW_ID", "1"},
	}
	for mask := 0; mask < 1<<len(vars); mask++ {
		m := map[string]string{}
		for i, kv := range vars {
			if mask&(1<<i) != 0 {
				m[kv.k] = kv.v
			}
		}
		if got, want := OSC52Honoured(envOf(m)), oldOSC52Honoured(envOf(m)); got != want {
			t.Errorf("%v: %v, before %v", m, got, want)
		}
	}
}
