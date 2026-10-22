//go:build unix

package mcp

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// shortSocketDir is a 0700 temp dir short enough for sun_path on macOS.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func echoSource() ToolSource {
	return serverTestSource{
		list: []Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}},
		call: func(_ context.Context, _ string, args map[string]any) (CallResult, error) {
			s, _ := args["text"].(string)
			return CallResult{Content: []Content{{Type: "text", Text: s}}}, nil
		},
	}
}

// socketCall sends one request line and reads one answer line.
func socketCall(t *testing.T, c net.Conn, line string) (string, error) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(line + "\n")); err != nil {
		return "", err
	}
	return bufio.NewReader(c).ReadString('\n')
}

func goroutinesSettle(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	after := runtime.NumGoroutine()
	for after > before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		after = runtime.NumGoroutine()
	}
	if after > before {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines %d → %d\n%s", before, after, buf[:runtime.Stack(buf, true)])
	}
}

// Spec (MS-1): the socket is 0600, a same-uid client gets answers, and
// Close removes the file, ends every connection and leaks no goroutine.
func TestSocketServerServeAndClose(t *testing.T) {
	before := runtime.NumGoroutine()
	path := filepath.Join(shortSocketDir(t), "k.mcp.sock")
	ln, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode().Perm() != 0o600 || fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket mode: %v %v", fi.Mode(), err)
	}
	srv := Serve(ln, echoSource(), func(net.Conn) error { return nil }, nil)
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, err := socketCall(t, c, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`)
	if err != nil || !strings.Contains(got, `"text":"hi"`) {
		t.Fatalf("answer: %q %v", got, err)
	}
	if srv.Conns() != 1 {
		t.Fatalf("conns = %d", srv.Conns())
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket file left: %v", err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := bufio.NewReader(c).ReadString('\n'); err == nil {
		t.Fatal("client still connected after Close")
	}
	c.Close()
	goroutinesSettle(t, before)
}

// Spec (MS-1): a refused peer (another uid) is closed before anything is
// read or answered; a nil check refuses everyone.
func TestSocketServerRefusesPeer(t *testing.T) {
	for _, peer := range []func(net.Conn) error{
		func(net.Conn) error { return errors.New("peer uid 1001 is not 1000") },
		nil,
	} {
		path := filepath.Join(shortSocketDir(t), "k.mcp.sock")
		ln, err := ListenUnix(path)
		if err != nil {
			t.Fatal(err)
		}
		refused := make(chan error, 1)
		called := false
		src := serverTestSource{call: func(context.Context, string, map[string]any) (CallResult, error) {
			called = true
			return CallResult{}, nil
		}}
		srv := Serve(ln, src, peer, func(err error) { refused <- err })
		c, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := socketCall(t, c, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{}}}`); err == nil {
			t.Fatalf("refused peer got an answer: %q", got)
		}
		select {
		case err := <-refused:
			if !strings.Contains(err.Error(), "refused") {
				t.Fatalf("report: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("refusal not reported")
		}
		c.Close()
		_ = srv.Close()
		if called {
			t.Fatal("a refused peer reached the tools")
		}
	}
}

// Spec (MS-1): a stale socket file (nobody answers) is replaced; a live
// one is ErrSocketBusy and stays; a non-socket file is never removed.
func TestListenUnixStaleAndBusy(t *testing.T) {
	dir := shortSocketDir(t)
	path := filepath.Join(dir, "k.mcp.sock")
	old, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	old.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = old.Close() // a crashed run: the file stays, nobody listens
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("stale file missing:", err)
	}
	ln, err := ListenUnix(path)
	if err != nil {
		t.Fatalf("stale socket not recovered: %v", err)
	}
	defer ln.Close()
	go func() { // keep answering, like a live UI
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	if _, err := ListenUnix(path); !errors.Is(err, ErrSocketBusy) {
		t.Fatalf("busy socket: %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("busy socket removed:", err)
	}
	plain := filepath.Join(dir, "plain.mcp.sock")
	if err := os.WriteFile(plain, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListenUnix(plain); err == nil {
		t.Fatal("listened over a regular file")
	}
	if b, err := os.ReadFile(plain); err != nil || string(b) != "keep" {
		t.Fatal("regular file touched")
	}
}

// Spec (MS-1): connections past MaxSocketConns are closed at once.
func TestSocketServerConnCap(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "k.mcp.sock")
	ln, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := Serve(ln, echoSource(), func(net.Conn) error { return nil }, nil)
	defer srv.Close()
	var conns []net.Conn
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < MaxSocketConns; i++ {
		c, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	deadline := time.Now().Add(5 * time.Second)
	for srv.Conns() < MaxSocketConns && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	extra, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	if got, err := socketCall(t, extra, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err == nil {
		t.Fatalf("connection over the cap answered: %q", got)
	}
}
