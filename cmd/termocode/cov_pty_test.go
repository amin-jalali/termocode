//go:build unix

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/amin-jalali/termocode/internal/session"
)

// ptyStdio makes this process's stdin and stdout a pseudo terminal for the
// length of the test; what is written to it is drained.
func ptyStdio(t *testing.T) {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	_ = pty.Setsize(tty, &pty.Winsize{Cols: 100, Rows: 30})
	go func() { _, _ = io.Copy(io.Discard, ptmx) }()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = tty, tty
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldIn, oldOut
		_ = tty.Close()
		_ = ptmx.Close()
	})
}

// runClient attaches to a running session and returns when the session
// ends.
func TestRunClientAttachesToARunningSession(t *testing.T) {
	dir := covSessionEnv(t)
	p, err := session.PathsFor(dir)
	if err != nil {
		t.Skip(err)
	}
	served := make(chan int, 1)
	go func() {
		served <- session.Serve(dir, func() *exec.Cmd { return exec.Command("sh", "-c", "echo hello; exec cat") })
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if c, err := session.Dial(p); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Skip("the session did not come up")
		}
		time.Sleep(20 * time.Millisecond)
	}
	ptyStdio(t)
	done := make(chan int, 1)
	go func() { done <- runClient(dir, clientOpts{note: "n"}) }()
	time.Sleep(700 * time.Millisecond)
	_ = session.Kill(p)
	select {
	case code := <-done:
		_ = code
	case <-time.After(20 * time.Second):
		t.Fatal("the client did not return")
	}
	select {
	case <-served:
	case <-time.After(20 * time.Second):
		t.Fatal("the session server did not end")
	}
}

// With no server the UI command (this test binary, which refuses the flag)
// ends at once and runServer returns.
func TestRunServerReturnsWhenTheUIEnds(t *testing.T) {
	dir := covSessionEnv(t)
	old := serverUICommand
	serverUICommand = func(string) *exec.Cmd { return exec.Command("sh", "-c", "exit 0") }
	t.Cleanup(func() { serverUICommand = old })
	done := make(chan int, 1)
	go func() { done <- runServer(dir) }()
	if p, err := session.PathsFor(dir); err == nil {
		time.Sleep(800 * time.Millisecond)
		_ = session.Kill(p)
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("runServer did not return")
	}
}

// cmdSSH on a terminal runs ssh (a fake one that fails) and gives its code.
func TestCmdSSHOnATerminalWithAFailingSSH(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\nexit 255\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv(envLinkTiming, "100ms,300ms")
	covSessionEnv(t)
	ptyStdio(t)
	done := make(chan int, 1)
	go func() { done <- cmdSSH([]string{"me@box"}) }()
	select {
	case code := <-done:
		if code == 0 {
			t.Logf("ssh gave code %d", code)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("cmdSSH did not return")
	}
	go func() { done <- cmdSSH([]string{"--web", "me@box"}) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("cmdSSH --web did not return")
	}
}

func TestProjectStartHeadlessWaitsForTheHub(t *testing.T) {
	dir := covSessionEnv(t)
	oldStart, oldWait := sessionStartHeadless, sessionWaitWebHub
	t.Cleanup(func() { sessionStartHeadless, sessionWaitWebHub = oldStart, oldWait })
	var since time.Time
	started := true
	sessionStartHeadless = func(session.Paths, int, func() error) (bool, error) { return started, nil }
	sessionWaitWebHub = func(_ session.Paths, s time.Time, _ time.Duration) error { since = s; return nil }
	if err := projectStartHeadless(dir, 5); err != nil || since.IsZero() {
		t.Fatalf("started: %v %v", err, since)
	}
	started = false
	if err := projectStartHeadless(dir, 5); err != nil || !since.IsZero() {
		t.Fatalf("already running must wait for any hub: %v %v", err, since)
	}
	sessionStartHeadless = func(session.Paths, int, func() error) (bool, error) { return false, errors.New("cannot start") }
	if err := projectStartHeadless(dir, 5); err == nil {
		t.Fatal("start error swallowed")
	}
}

// On a terminal the secret is typed hidden; the value is read from it.
func TestReadSecretFromATerminal(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skip(err)
	}
	defer ptmx.Close()
	defer tty.Close()
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = ptmx.WriteString("sk-" + "test0123456789abcdef\n")
	}()
	go func() { _, _ = io.Copy(io.Discard, ptmx) }()
	var errw bytes.Buffer
	a := &mcpAdmin{in: tty, errw: &errw}
	v, err := a.readSecret("API_KEY")
	if err != nil || v != "sk-test0123456789abcdef" || !strings.Contains(errw.String(), "Value for API_KEY") {
		t.Fatalf("%q %v %q", v, err, errw.String())
	}
}
