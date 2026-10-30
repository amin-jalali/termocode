package main

import (
	"strings"
	"testing"
	"time"
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

// TestLinkWebPort: --web=<port> (from termocode ssh --web) is taken out
// of the far side's arguments; a bad port is 0 (no web) and still taken out.
func TestLinkWebPort(t *testing.T) {
	rest, port := linkWebPort([]string{"--web=41237", "/srv/app"})
	if port != 41237 || len(rest) != 1 || rest[0] != "/srv/app" {
		t.Errorf("got %v %d", rest, port)
	}
	for _, bad := range []string{"--web=80", "--web=x", "--web=70000", "--web="} {
		if rest, port := linkWebPort([]string{bad}); port != 0 || len(rest) != 0 {
			t.Errorf("%s: %v %d", bad, rest, port)
		}
	}
	if rest, port := linkWebPort([]string{"/srv/app"}); port != 0 || len(rest) != 1 {
		t.Errorf("none: %v %d", rest, port)
	}
}

func TestSSHCommandArgs(t *testing.T) {
	c, err := sshCommandArgs([]string{"--dir", "/srv/app", "-p", "2222", "me@box"})
	if err != nil || c.dir != "/srv/app" || c.web || strings.Join(c.args, " ") != "-p 2222 me@box" {
		t.Errorf("got %+v %v", c, err)
	}
	c, err = sshCommandArgs([]string{"--dir=~/code", "box"})
	if err != nil || c.dir != "~/code" || strings.Join(c.args, " ") != "box" {
		t.Errorf("got %+v %v", c, err)
	}
	if _, err := sshCommandArgs([]string{"--dir", "x"}); err == nil {
		t.Error("no destination, no error")
	}
	// --web (WEB-29) in any order with --dir, before the ssh arguments.
	c, err = sshCommandArgs([]string{"--web", "--dir", "/w", "-p", "22", "box"})
	if err != nil || !c.web || c.dir != "/w" || strings.Join(c.args, " ") != "-p 22 box" {
		t.Errorf("web: %+v %v", c, err)
	}
	if _, err := sshCommandArgs([]string{"--web"}); err == nil {
		t.Error("--web with no destination, no error")
	}
}

func TestLinkTiming(t *testing.T) {
	if i, d := linkTiming("200ms,1s"); i != 200*time.Millisecond || d != time.Second {
		t.Errorf("got %v %v", i, d)
	}
	for _, bad := range []string{"", "1s", "x,1s", "2s,1s", "0s,1s"} {
		if i, d := linkTiming(bad); i != 0 || d != 0 {
			t.Errorf("%q: %v %v, want the defaults", bad, i, d)
		}
	}
}
