//go:build unix

package clilogin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The fakes below stand in for the real CLIs: they print what the real
// login prints (as far as we know it, with escape sequences), wait,
// and touch $HOME/logged-in, which their status command reads. No test
// runs a real CLI, npm or the network.

const fakeCodex = `#!/bin/sh
if [ "$1 $2" = "login status" ]; then
  if [ -f "$HOME/logged-in" ]; then echo "Logged in using ChatGPT"; exit 0; fi
  echo "Not logged in"; exit 1
fi
[ "$1" = "login" ] || exit 2
echo $$ > "$HOME/login.pid"
echo "args:$*" > "$HOME/args"
if [ -n "$OPENAI_API_KEY$ANTHROPIC_API_KEY$TERMOCODE_AI_KEY" ]; then echo leak > "$HOME/leak"; fi
case "$FAKE_LOGIN" in
nonode) echo "/usr/bin/env: 'node': No such file or directory" >&2; exit 127;;
flood)
  i=0
  while [ $i -lt 1500 ]; do
    echo "......................................................................................... $i"
    i=$((i+1))
  done;;
esac
if [ "$2" = "--device-auth" ]; then
  printf '\033[1mFollow these steps to sign in with ChatGPT using device code authorization:\033[0m\r\n'
  printf '1. Open this link in your browser and sign in to your account\r\n   \033[94mhttps://auth.openai.com/codex/device\033[0m\r\n'
  printf '2. Enter this one-time code \033[2m(expires in 15 minutes)\033[0m\r\n   \033[94mWXYZ-98765\033[0m\r\n'
else
  printf '\033[2mStarting local login server on http://localhost:1455.\033[0m\r\n'
  printf 'If your browser did not open, navigate to this URL to authenticate:\r\n\r\n'
  printf '\033[94mhttps://auth.openai.com/oauth/authorize?response_type=code&client_id=app_FAKE&state=FAKE\033[0m\r\n'
fi
case "$FAKE_LOGIN" in
ok) sleep 0.3; touch "$HOME/logged-in"; echo "Successfully logged in";;
okstay) sleep 0.3; touch "$HOME/logged-in"; sleep 300;;
hang) sleep 300 & echo $! > "$HOME/child.pid"; wait;;
fail) sleep 0.2; echo "Login was not completed"; exit 1;;
offline) sleep 0.2; echo "error sending request: dns error: Temporary failure in name resolution"; exit 1;;
esac
`

const fakeClaude = `#!/bin/sh
if [ "$1 $2" = "auth status" ]; then
  if [ -f "$HOME/logged-in" ]; then echo '{"loggedIn": true, "authMethod": "claude.ai"}'; exit 0; fi
  echo '{"loggedIn": false}'; exit 1
fi
[ "$1 $2" = "auth login" ] || exit 2
echo $$ > "$HOME/login.pid"
printf '\033[?25l\033[2K\033[1G\033[1mOpening browser to sign in…\033[22m\r\n'
printf "Browser didn't open? Use the url below to sign in:\r\n\r\n"
printf '\033[2mhttps://claude.ai/oauth/authorize?code=true&client_id=FAKE&response_type=code&state=FAKE\033[22m\r\n\r\n'
printf 'Paste code here if prompted > '
read code
if [ "$code" = "GOOD-CODE#state" ]; then touch "$HOME/logged-in"; printf '\r\nLogin successful.\r\n'; exit 0; fi
printf '\r\nOAuth error: Invalid code\r\n'; exit 1
`

// fakeEnv writes the fake CLIs into a temp dir and returns an env whose
// PATH has them first and whose HOME is a temp dir.
func fakeEnv(t *testing.T, extra ...string) (env []string, home string) {
	t.Helper()
	bin, home := t.TempDir(), t.TempDir()
	for name, body := range map[string]string{"codex": fakeCodex, "claude": fakeClaude} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env = append([]string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home, "TERM=xterm-256color"}, extra...)
	return env, home
}

// next waits for the session's next event.
func next(t *testing.T, s *Session) Event {
	t.Helper()
	select {
	case ev, ok := <-s.Events():
		if !ok {
			t.Fatal("events closed")
		}
		return ev
	case <-time.After(10 * time.Second):
		t.Fatal("no event")
	}
	return Event{}
}

// waitFor waits for the first event of kind k (earlier ones are skipped).
func waitFor(t *testing.T, s *Session, k EventKind) Event {
	t.Helper()
	for {
		if ev := next(t, s); ev.Kind == k {
			return ev
		} else if ev.Kind == EventExit && k != EventExit {
			t.Fatalf("exited (%v) before event %d", ev.Err, k)
		}
	}
}

// pidFile reads a pid the fake wrote into home.
func pidFile(t *testing.T, home, name string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(home, name))
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s", name)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// gone waits until pid no longer runs (a zombie still answers kill 0
// until its parent reaps it; the pty's leader is reaped by Wait, its
// child by init).
func gone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("process %d still runs", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSessionCodexURLAndExit(t *testing.T) {
	env, home := fakeEnv(t, "FAKE_LOGIN=ok", "OPENAI_API_KEY=sk-should-not-pass", "TERMOCODE_AI_KEY=x")
	s, err := Start(context.Background(), Options{CLI: ai.CLICodex, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	ev := waitFor(t, s, EventURL)
	if want := "https://auth.openai.com/oauth/authorize?response_type=code&client_id=app_FAKE&state=FAKE"; ev.URL != want {
		t.Fatalf("url %q, want %q (escape sequences must be gone)", ev.URL, want)
	}
	if ex := waitFor(t, s, EventExit); ex.Err != nil {
		t.Fatalf("exit %v", ex.Err)
	}
	if _, ok := <-s.Events(); ok {
		t.Fatal("events stay open after exit")
	}
	if _, err := os.Stat(filepath.Join(home, "leak")); err == nil {
		t.Fatal("the login saw an API key")
	}
	if b, _ := os.ReadFile(filepath.Join(home, "args")); strings.TrimSpace(string(b)) != "args:login" {
		t.Fatalf("args %q", b)
	}
}

func TestSessionClaudeNeedsCodeAndWrite(t *testing.T) {
	env, home := fakeEnv(t)
	s, err := Start(context.Background(), Options{CLI: ai.CLIClaude, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Cancel()
	if ev := waitFor(t, s, EventURL); !strings.HasPrefix(ev.URL, "https://claude.ai/oauth/authorize?code=true") {
		t.Fatalf("url %q", ev.URL)
	}
	waitFor(t, s, EventNeedsCode)
	if err := s.Write("bad\ncode"); err == nil {
		t.Fatal("a code with a line break must be refused")
	}
	if err := s.Write("  GOOD-CODE#state "); err != nil {
		t.Fatal(err)
	}
	if ex := waitFor(t, s, EventExit); ex.Err != nil {
		t.Fatalf("exit %v", ex.Err)
	}
	if _, err := os.Stat(filepath.Join(home, "logged-in")); err != nil {
		t.Fatal("the pasted code did not reach the CLI")
	}
}

func TestSessionDeviceAuth(t *testing.T) {
	env, home := fakeEnv(t, "FAKE_LOGIN=ok")
	s, err := Start(context.Background(), Options{CLI: ai.CLICodex, Env: env, DeviceAuth: true})
	if err != nil {
		t.Fatal(err)
	}
	if ev := waitFor(t, s, EventURL); ev.URL != "https://auth.openai.com/codex/device" {
		t.Fatalf("url %q", ev.URL)
	}
	if ev := waitFor(t, s, EventCode); ev.Code != "WXYZ-98765" {
		t.Fatalf("code %q", ev.Code)
	}
	waitFor(t, s, EventExit)
	if b, _ := os.ReadFile(filepath.Join(home, "args")); strings.TrimSpace(string(b)) != "args:login --device-auth" {
		t.Fatalf("args %q", b)
	}
}

// Cancel kills the whole process group: the CLI and what it started.
func TestSessionCancelKillsGroup(t *testing.T) {
	env, home := fakeEnv(t, "FAKE_LOGIN=hang")
	s, err := Start(context.Background(), Options{CLI: ai.CLICodex, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, EventURL)
	leader, child := pidFile(t, home, "login.pid"), pidFile(t, home, "child.pid")
	s.Cancel()
	s.Cancel() // twice is fine
	waitFor(t, s, EventExit)
	gone(t, leader)
	gone(t, child)
}

func TestSessionContextCancelKillsGroup(t *testing.T) {
	env, home := fakeEnv(t, "FAKE_LOGIN=hang")
	ctx, cancel := context.WithCancel(context.Background())
	s, err := Start(ctx, Options{CLI: ai.CLICodex, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, EventURL)
	leader, child := pidFile(t, home, "login.pid"), pidFile(t, home, "child.pid")
	cancel()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not end")
	}
	gone(t, leader)
	gone(t, child)
}

func TestSessionNodeMissing(t *testing.T) {
	env, _ := fakeEnv(t, "FAKE_LOGIN=nonode")
	s, err := Start(context.Background(), Options{CLI: ai.CLICodex, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	ex := waitFor(t, s, EventExit)
	var e *Error
	if !errors.As(ex.Err, &e) || e.Kind != FailNoNode || !strings.Contains(e.Msg, "Codex needs Node.js") {
		t.Fatalf("exit %v", ex.Err)
	}
}

func TestSessionOfflineExit(t *testing.T) {
	env, _ := fakeEnv(t, "FAKE_LOGIN=offline")
	s, err := Start(context.Background(), Options{CLI: ai.CLICodex, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	ex := waitFor(t, s, EventExit)
	var e *Error
	if !errors.As(ex.Err, &e) || e.Kind != FailOffline {
		t.Fatalf("exit %v", ex.Err)
	}
}

// A long output keeps only a capped tail, and the link after it is
// still found.
func TestSessionTailCapped(t *testing.T) {
	env, _ := fakeEnv(t, "FAKE_LOGIN=flood")
	s, err := Start(context.Background(), Options{CLI: ai.CLICodex, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Cancel()
	if ev := waitFor(t, s, EventURL); !strings.HasPrefix(ev.URL, "https://auth.openai.com/oauth/authorize") {
		t.Fatalf("url %q", ev.URL)
	}
	if n := s.tailLen(); n > maxTail {
		t.Fatalf("tail %d bytes, cap %d", n, maxTail)
	}
	waitFor(t, s, EventExit)
}

func TestStartNotInstalled(t *testing.T) {
	_, err := Start(context.Background(), Options{CLI: ai.CLICodex, Env: []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir()}})
	if err == nil || !strings.Contains(err.Error(), "Codex") {
		t.Fatalf("got %v", err)
	}
	if _, err := Start(context.Background(), Options{CLI: "other"}); err == nil {
		t.Fatal("an unknown CLI must not start")
	}
}

func TestUseDeviceAuth(t *testing.T) {
	cases := []struct {
		env  []string
		want bool
	}{
		{[]string{"SSH_CONNECTION=1 2 3 4"}, true},
		{[]string{"SSH_TTY=/dev/pts/1"}, true},
		{[]string{"SSH_CONNECTION=1 2 3 4", "DISPLAY=:0"}, false},
		{[]string{"SSH_CONNECTION=1 2 3 4", "WAYLAND_DISPLAY=wayland-0"}, false},
		{[]string{"SSH_CONNECTION=1 2 3 4", "BROWSER=w3m"}, false},
		{[]string{"DISPLAY=:0"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := UseDeviceAuth(c.env); got != c.want {
			t.Errorf("UseDeviceAuth(%v) = %v", c.env, got)
		}
	}
}
