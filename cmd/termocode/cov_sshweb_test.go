//go:build unix

package main

import (
	"errors"
	"github.com/amin-jalali/termocode/internal/session"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFreeLocalPortAndPortAnswers(t *testing.T) {
	port, err := freeLocalPort()
	if err != nil || port <= 0 {
		t.Fatalf("%d %v", port, err)
	}
	if portAnswers(port, 150*time.Millisecond) {
		t.Fatal("a free port answered")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if !portAnswers(ln.Addr().(*net.TCPAddr).Port, 2*time.Second) {
		t.Fatal("an open port did not answer")
	}
}

func TestStartFwdProcRunsAndKills(t *testing.T) {
	if _, err := startFwdProc("/definitely/not/a/program", nil); err == nil {
		t.Fatal("a missing program started")
	}
	p, err := startFwdProc("sleep", []string{"30"})
	if err != nil {
		t.Skip(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.Wait() }()
	p.Kill()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the process was not killed")
	}
}

// openInBrowser starts the system opener (a fake one here) and returns.
func TestOpenInBrowserUsesTheSystemOpener(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(bin, "ran")
	script := "#!/bin/sh\n: > " + marker + "\n"
	for _, name := range []string{"xdg-open", "open"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	if err := openInBrowser("/tmp/x.html"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the opener did not run")
}

func TestOpenInBrowserWithoutAnOpener(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := openInBrowser("/tmp/x.html"); err == nil {
		t.Fatal("no opener, no error")
	}
	_ = exec.Command
}

func TestPortForwardFailureModes(t *testing.T) {
	// No free local port, ssh cannot start: a note each.
	f, ff, _ := newPortTestForward(t)
	f.Handoff(portTestHandoff)
	waitCond(t, "the panel", func() bool { _, o, _, _ := ff.snapshot(); return len(o) == 1 })
	f.free = func() (int, error) { return 0, errors.New("no ports") }
	f.Handoff(session.WebHandoff{Fwd: &session.PortForward{Open: true, Port: 3000, ID: "a"}})
	f.free = func() (int, error) { return 50200, nil }
	f.start = func(string, []string) (webFwdProc, error) { return nil, errors.New("no ssh") }
	f.Handoff(session.WebHandoff{Fwd: &session.PortForward{Open: true, Port: 3001, ID: "b"}})
	waitCond(t, "two notes", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.notes) >= 2 })
	if notes := f.Close(); len(notes) != 2 || !strings.Contains(notes[0], "no port preview") {
		t.Errorf("notes %q", notes)
	}
}

// A forward that never answers is killed and reported; a preview whose
// browser cannot open says where to go.
func TestPortForwardNotReadyAndBrowserFailure(t *testing.T) {
	f, ff, _ := newPortTestForward(t)
	f.Handoff(portTestHandoff)
	waitCond(t, "the panel", func() bool { _, o, _, _ := ff.snapshot(); return len(o) == 1 })
	f.ready = func(int, time.Duration) bool { return false }
	f.Handoff(session.WebHandoff{Fwd: &session.PortForward{Open: true, Port: 3000, ID: "slow"}})
	waitCond(t, "the kill", func() bool { _, _, _, k := ff.snapshot(); return k >= 1 })
	f.ready = func(int, time.Duration) bool { return true }
	f.open = func(string) error { return errors.New("no browser") }
	f.Handoff(session.WebHandoff{Fwd: &session.PortForward{Open: true, Port: 3001, ID: "ok"}})
	waitCond(t, "the browser note", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, n := range f.notes {
			if strings.Contains(n, "could not open a browser") {
				return true
			}
		}
		return false
	})
	notes := f.Close()
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "could not forward port 3000") && !strings.Contains(joined, "SSH could not forward port 3000") {
		t.Errorf("notes %q", notes)
	}
}

// After Close a hand-off is dropped; a changed port restarts the forward; a
// browser that cannot open and a page that cannot be written say so.
func TestWebForwardClosedRestartAndOpenFailures(t *testing.T) {
	f, ff := newTestForward(t)
	f.Handoff(session.WebHandoff{Port: 41001, Sock: "/s1", Secret: "x"})
	waitCond(t, "the page", func() bool { _, o, _, _ := ff.snapshot(); return len(o) == 1 })
	// A new port: the old forward is killed and a new one starts.
	f.Handoff(session.WebHandoff{Port: 41002, Sock: "/s2", Secret: "x"})
	waitCond(t, "the restart", func() bool { argv, _, _, k := ff.snapshot(); return len(argv) == 2 && k >= 1 })
	f.Close()
	f.Handoff(session.WebHandoff{Port: 41003, Sock: "/s3"}) // dropped
	if argv, _, _, _ := ff.snapshot(); len(argv) != 2 {
		t.Errorf("a closed forward started %d", len(argv))
	}

	g, gg := newTestForward(t)
	g.open = func(string) error { return errors.New("no browser") }
	g.Handoff(session.WebHandoff{Port: 41010, Sock: "/s", Secret: "x"})
	waitCond(t, "the note", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return len(g.notes) >= 1 })
	if notes := g.Close(); len(notes) == 0 || !strings.Contains(notes[0], "could not open a browser") {
		t.Errorf("notes %q", notes)
	}
	_ = gg

	h, _ := newTestForward(t)
	h.dir = "/definitely/not/a/folder"
	h.Handoff(session.WebHandoff{Port: 41020, Sock: "/s", Secret: "x"})
	waitCond(t, "the write note", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.notes) >= 1 })
	if notes := h.Close(); !strings.Contains(notes[0], "could not open the web panel") {
		t.Errorf("notes %q", notes)
	}
}
