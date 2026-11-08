//go:build unix

package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// `termocode mcp --socket <path>` relays to one exact socket: a CLI step's
// own MCP server (its termocode tools). A socket that is gone is an
// error, never the static "open termocode first" list.

func TestRunMCPSocketRelays(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "st.sock")
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
		_, _ = io.WriteString(c, `{"jsonrpc":"2.0","id":1,"result":{"step":`+`1}}`+"\n")
		_, _ = br.ReadString('\n')
	}()
	inR, inW := io.Pipe()
	var out, errb syncBuf
	done := make(chan int, 1)
	go func() { done <- runMCPIn([]string{"--socket", sock}, inR, &out, &errb, "") }()
	go func() { _, _ = io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n") }()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), `"step"`) {
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

func TestRunMCPSocketGone(t *testing.T) {
	var out, errb bytes.Buffer
	sock := filepath.Join(shortTemp(t), "gone.sock")
	if c := runMCPIn([]string{"--socket", sock}, strings.NewReader(""), &out, &errb, ""); c != mcpExitLost {
		t.Fatalf("exit %d, want %d (stderr %q)", c, mcpExitLost, errb.String())
	}
	if out.Len() != 0 || !strings.Contains(errb.String(), "step") {
		t.Errorf("stdout %q stderr %q", out.String(), errb.String())
	}
	if c := runMCPIn([]string{"--socket"}, strings.NewReader(""), &out, &errb, ""); c != mcpExitUsage {
		t.Errorf("--socket without a path: exit %d", c)
	}
}
