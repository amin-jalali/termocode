//go:build unix

package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
)

// RC-5 (4e): `termocode mcp --workspace <id>` relays to the UI of a
// container workspace, whose socket is keyed on the workspace ID (a
// container's /app is not a local folder).
func TestRunMCPWorkspaceFlagRelaysToTheContainerUI(t *testing.T) {
	sessionEnv(t)
	id := "container:default/api-1:/app"
	p, err := session.PathsForID(id)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", p.MCPSocket)
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
		_, _ = io.WriteString(c, `{"jsonrpc":"2.0","id":1,"result":{"echo":1}}`+"\n")
		_, _ = br.ReadString('\n')
	}()
	inR, inW := io.Pipe()
	var out, errb syncBuf
	done := make(chan int, 1)
	go func() { done <- runMCPIn([]string{"--workspace", id}, inR, &out, &errb, "") }()
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

// With no UI open on that workspace the tool list still answers and the
// message names the workspace ID.
func TestRunMCPWorkspaceFlagNoUIIsStatic(t *testing.T) {
	sessionEnv(t)
	var out, errb bytes.Buffer
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"open_file","arguments":{}}}` + "\n")
	if c := runMCPIn([]string{"--workspace=container:default/api-1:/app"}, in, &out, &errb, ""); c != 0 {
		t.Fatalf("exit %d, stderr %q", c, errb.String())
	}
	if !strings.Contains(out.String(), "api-1") {
		t.Fatalf("the answer must name the workspace: %q", out.String())
	}
}

func TestRunMCPWorkspaceFlagBadArgs(t *testing.T) {
	for _, args := range [][]string{
		{"--workspace"},
		{"--workspace", "bogus"},
		{"--workspace", "container:default/api-1:/app", "--dir", "/tmp"},
	} {
		var out, errb bytes.Buffer
		if c := runMCPIn(args, strings.NewReader(""), &out, &errb, ""); c != mcpExitUsage {
			t.Errorf("%v: exit %d, want %d", args, c, mcpExitUsage)
		}
		if out.Len() != 0 || errb.Len() == 0 {
			t.Errorf("%v: stdout %q stderr %q", args, out.String(), errb.String())
		}
	}
	if !strings.Contains(mcpUsage, "--workspace") {
		t.Error("the usage must list --workspace")
	}
}
