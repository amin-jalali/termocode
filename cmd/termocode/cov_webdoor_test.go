//go:build unix

package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
)

const validWS = "0123456789abcdef"

func TestWebDoorDialRefusals(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	for _, ws := range []string{"", "../etc", "short", validWS + "0"} {
		if _, err := webDoorDial(ctx, "home", ws); err != errNoProject {
			t.Errorf("%q: %v", ws, err)
		}
	}
	if _, err := webDoorDial(ctx, validWS, validWS); err != errNoProject {
		t.Errorf("the home project itself: %v", err)
	}
	if _, err := webDoorDial(ctx, "home", validWS); err != errNoProject {
		t.Errorf("no socket: %v", err)
	}
	dir, err := session.Dir()
	if err != nil {
		t.Skip(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A plain file with the right name is not a socket.
	if err := os.WriteFile(filepath.Join(dir, validWS+".web.sock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := webDoorDial(ctx, "home", validWS); err != errNoProject {
		t.Errorf("file instead of socket: %v", err)
	}
	// A socket nobody listens on.
	os.Remove(filepath.Join(dir, validWS+".web.sock"))
	ln, err := net.Listen("unix", filepath.Join(dir, validWS+".web.sock"))
	if err != nil {
		t.Skip(err)
	}
	ln.Close() // the socket file stays, nobody answers
	if _, err := webDoorDial(ctx, "home", validWS); err != errNoProject {
		t.Errorf("dead socket: %v", err)
	}
	// A live one connects.
	os.Remove(filepath.Join(dir, validWS+".web.sock"))
	live, err := net.Listen("unix", filepath.Join(dir, validWS+".web.sock"))
	if err != nil {
		t.Skip(err)
	}
	defer live.Close()
	go func() {
		if c, err := live.Accept(); err == nil {
			c.Close()
		}
	}()
	c, err := webDoorDial(ctx, "home", validWS)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestWebGatewayConfigVariants(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	base := []string{"--dir", dir, "--audit", filepath.Join(dir, "audit.log")}
	for _, c := range []struct {
		extra []string
		check func(t *testing.T)
	}{
		{[]string{"--tailnet", "--totp", "--app-key", "--phone-watch", "--resume", "--viewers", "3"}, nil},
		{[]string{"--lan", "--devices", filepath.Join(dir, "dev")}, nil},
		{[]string{"--devices", filepath.Join(dir, "dev")}, nil},
	} {
		var errw bytes.Buffer
		cfg, ok := webGatewayConfig(append(append([]string{}, base...), c.extra...), &errw)
		if !ok {
			t.Errorf("%v: %s", c.extra, errw.String())
			continue
		}
		if cfg.Dial == nil || cfg.AuditPath == "" {
			t.Errorf("%v: %+v", c.extra, cfg)
		}
		if cfg.DialWS != nil {
			if _, err := cfg.DialWS(context.Background(), "nope"); err == nil {
				t.Error("DialWS accepted a bad workspace")
			}
		}
		if cfg.Dial != nil {
			if _, err := cfg.Dial(context.Background()); err == nil {
				t.Error("Dial reached a socket that is not there")
			}
		}
	}
	for _, bad := range [][]string{
		{"--dir", dir}, append(append([]string{}, base...), "--tailnet", "--ssh-port", "2222"),
		append(append([]string{}, base...), "--lan"), append(append([]string{}, base...), "extra"),
	} {
		var errw bytes.Buffer
		if _, ok := webGatewayConfig(bad, &errw); ok || !strings.Contains(errw.String(), "started by termocode itself") {
			t.Errorf("%v accepted: %s", bad, errw.String())
		}
	}
}

func TestRunWebGatewayInBadArgs(t *testing.T) {
	var errw bytes.Buffer
	if code := runWebGatewayIn([]string{"--bogus"}, nil, &errw); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

// A real gateway starts on a free port, hands over its address and stops on
// SIGTERM.
func TestRunWebGatewayInStartsAndStops(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	paths, err := session.PathsFor(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.WebSocket), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", paths.WebSocket)
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	go func() { _, _ = io.Copy(io.Discard, r) }()
	done := make(chan int, 1)
	var errw bytes.Buffer
	go func() {
		done <- runWebGatewayIn([]string{"--dir", dir, "--audit", filepath.Join(dir, "audit.log"), "--port", "0"}, w, &errw)
	}()
	select {
	case code := <-done:
		t.Skipf("the gateway ended at once (%d): %s", code, errw.String())
	case <-time.After(1500 * time.Millisecond):
	}
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	select {
	case code := <-done:
		if code != 0 && !strings.Contains(errw.String(), "context canceled") {
			t.Fatalf("exit %d: %s", code, errw.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the gateway did not stop")
	}
}
