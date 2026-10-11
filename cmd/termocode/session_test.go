package main

import "testing"

func TestWantSession(t *testing.T) {
	ssh := map[string]string{"SSH_CONNECTION": "1.2.3.4 5 6.7.8.9 22"}
	cases := []struct {
		name      string
		setting   string
		noSession bool
		env       map[string]string
		tty       bool
		want      bool
	}{
		{"local auto", "", false, nil, true, false},
		{"ssh auto", "", false, ssh, true, true},
		{"ssh tty only", "auto", false, map[string]string{"SSH_TTY": "/dev/pts/1"}, true, true},
		{"ssh in tmux", "", false, map[string]string{"SSH_CONNECTION": "x", "TMUX": "/tmp/t,1,0"}, true, false},
		{"local on", "on", false, nil, true, true},
		{"ssh off", "off", false, ssh, true, false},
		{"flag wins", "on", true, ssh, true, false},
		{"no tty", "on", false, ssh, false, false},
		{"inside a session", "on", false, map[string]string{"TERMOCODE_SESSION": "abc"}, true, false},
		{"--no-session before a re-exec", "on", false, map[string]string{"TERMOCODE_NO_SESSION": "1"}, true, false},
	}
	for _, c := range cases {
		getenv := func(k string) string { return c.env[k] }
		if got := wantSession(c.setting, c.noSession, getenv, c.tty); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestExtractFlag(t *testing.T) {
	args, ok := extractFlag([]string{"--no-session", "dir", "--no-session"}, "--no-session")
	if !ok || len(args) != 1 || args[0] != "dir" {
		t.Errorf("got %v %v", args, ok)
	}
	if _, ok := extractFlag([]string{"dir"}, "--no-session"); ok {
		t.Error("found a flag that is not there")
	}
}
