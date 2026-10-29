package main

import (
	"strings"
	"testing"
)

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

func TestAttachRefusal(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "TERMOCODE_SESSION" {
				return v
			}
			return ""
		}
	}
	if msg := attachRefusal(env(""), "abc"); msg != "" {
		t.Errorf("outside a session: %q", msg)
	}
	if msg := attachRefusal(env("abc"), "abc"); !strings.Contains(msg, "already inside this session") {
		t.Errorf("own session: %q", msg)
	}
	if msg := attachRefusal(env("def"), "abc"); !strings.Contains(msg, "inside another session") {
		t.Errorf("other session: %q", msg)
	}
}

// TestLinkArgs: the flags termocode ssh adds on the far side are taken
// out; the folder stays.
func TestLinkArgs(t *testing.T) {
	rest, link, note := linkArgs([]string{"--link", "/srv/app"})
	if !link || note != "" || len(rest) != 1 || rest[0] != "/srv/app" {
		t.Errorf("first connect: %v %v %q", rest, link, note)
	}
	rest, link, note = linkArgs([]string{"--link", "--reconnected=12s,dropped", "/srv/app"})
	if !link || note != "Reconnected after 12 s · keys typed while offline were dropped" || len(rest) != 1 {
		t.Errorf("reconnect: %v %v %q", rest, link, note)
	}
	if _, _, note := linkArgs([]string{"--link", "--reconnected=3s"}); note != "Reconnected after 3 s" {
		t.Errorf("note %q", note)
	}
	if rest, link, _ := linkArgs([]string{"/srv/app"}); link || len(rest) != 1 {
		t.Errorf("no link: %v %v", rest, link)
	}
}

func TestSSHCommandArgs(t *testing.T) {
	dir, rest, err := sshCommandArgs([]string{"--dir", "/srv/app", "-p", "2222", "me@box"})
	if err != nil || dir != "/srv/app" || strings.Join(rest, " ") != "-p 2222 me@box" {
		t.Errorf("got %q %q %v", dir, rest, err)
	}
	dir, rest, err = sshCommandArgs([]string{"--dir=~/code", "box"})
	if err != nil || dir != "~/code" || strings.Join(rest, " ") != "box" {
		t.Errorf("got %q %q %v", dir, rest, err)
	}
	if _, _, err := sshCommandArgs([]string{"--dir", "x"}); err == nil {
		t.Error("no destination, no error")
	}
}
