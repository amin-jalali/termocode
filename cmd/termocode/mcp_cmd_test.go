//go:build unix

package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunMCPRelaysToUI(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	sock, _ := mcpPathsFor(ws)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		_, _ = br.ReadString('\n')
		_, _ = io.WriteString(c, `{"jsonrpc":"2.0","id":1,"result":{"echo":`+`1}}`+"\n")
		_, _ = br.ReadString('\n') // until the relay half-closes
	}()
	inR, inW := io.Pipe()
	var out, errb syncBuf
	done := make(chan int, 1)
	go func() { done <- runMCPIn([]string{"--dir", ws}, inR, &out, &errb, "") }()
	_, _ = io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n")
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), `"echo"`) {
		if time.Now().After(deadline) {
			t.Fatalf("no reply; stdout %q stderr %q", out.String(), errb.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = inW.Close()
	select {
	case c := <-done:
		if c != 0 {
			t.Fatalf("exit %d, stderr %q", c, errb.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not exit")
	}
}

func TestRunMCPNoUIIsStatic(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var errb syncBuf
	done := make(chan int, 1)
	go func() { done <- runMCPIn([]string{"--dir=" + ws}, inR, outW, &errb, ""); _ = outW.Close() }()
	go io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n")
	line := readLine(t, bufio.NewReader(outR))
	if !strings.Contains(line, `"tools"`) {
		t.Fatalf("got %s", line)
	}
	_ = inW.Close()
	if c := <-done; c != 0 {
		t.Fatalf("exit %d", c)
	}
	if !strings.Contains(errb.String(), "not open in "+ws) {
		t.Fatalf("stderr %q", errb.String())
	}
}

// A socket file nobody listens on (a crashed UI): static mode, not a crash.
func TestRunMCPStaleSocketIsStatic(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	sock, _ := mcpPathsFor(ws)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close() // the file stays, nobody answers
	inR, inW := io.Pipe()
	var out, errb bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- runMCPIn([]string{"--dir", ws}, inR, &out, &errb, "") }()
	_ = inW.Close()
	if c := <-done; c != 0 {
		t.Fatalf("exit %d", c)
	}
	if !strings.Contains(errb.String(), "cannot be reached") {
		t.Fatalf("stderr %q", errb.String())
	}
}

func TestRunMCPWalkUpFromCwd(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	listenMCP(t, ws)
	sub := filepath.Join(ws, "a")
	_ = os.Mkdir(sub, 0o755)
	var errb bytes.Buffer
	inR, inW := io.Pipe()
	_ = inW.Close()
	// The listener never accepts: the dial still succeeds (backlog), and
	// stdin EOF ends the relay. The note must not say "not open".
	var out bytes.Buffer
	if c := runMCPIn(nil, inR, &out, &errb, sub); c != 0 {
		t.Fatalf("exit %d: %s", c, errb.String())
	}
	if strings.Contains(errb.String(), "not open") {
		t.Fatalf("walk-up missed the socket: %q", errb.String())
	}
}

func TestRunMCPBadArgs(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"--dir"}, {"extra"}, {"--dir", "/nonexistent-tc-dir"}} {
		var out, errb bytes.Buffer
		if c := runMCPIn(args, strings.NewReader(""), &out, &errb, ""); c != mcpExitUsage {
			t.Errorf("%v: exit %d, want %d", args, c, mcpExitUsage)
		}
		if out.Len() != 0 {
			t.Errorf("%v: stdout %q must stay empty", args, out.String())
		}
		if errb.Len() == 0 {
			t.Errorf("%v: no message on stderr", args)
		}
	}
}

func TestRunMCPHelp(t *testing.T) {
	var out, errb bytes.Buffer
	if c := runMCPIn([]string{"--help"}, strings.NewReader(""), &out, &errb, ""); c != 0 {
		t.Fatalf("exit %d", c)
	}
	if !strings.Contains(errb.String()+out.String(), "termocode mcp [--dir") {
		t.Fatalf("usage missing: %q", out.String())
	}
}
