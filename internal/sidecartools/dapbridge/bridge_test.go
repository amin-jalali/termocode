package dapbridge

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// The fake debug adapter: the test binary itself (TestMain), like
// `dlv dap -l 127.0.0.1:0`: it prints a log line and its address, then
// answers each line on its one connection with "re:<line>".
func TestMain(m *testing.M) {
	if os.Getenv("DAPBRIDGE_FAKE") == "1" {
		fakeAdapter()
		return
	}
	os.Exit(m.Run())
}

func fakeAdapter() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(3)
	}
	fmt.Println("some log line first")
	fmt.Printf("DAP server listening at: %s\n", ln.Addr())
	c, err := ln.Accept()
	if err != nil {
		os.Exit(4)
	}
	fmt.Println("adapter output after the connect")
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		fmt.Fprintf(c, "re:%s\n", sc.Text())
	}
	c.Close()
}

func fakeArgv(t *testing.T) []string {
	t.Helper()
	t.Setenv("DAPBRIDGE_FAKE", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []string{exe, "-test.run=^$"}
}

// RC-8: dap-bridge turns a debug adapter that only listens on TCP into
// one on stdin/stdout (nvim-dap starts it through `docker exec -i` in
// the sidecar): it starts the adapter, waits for its address line,
// dials it and copies both ways; the adapter's own output goes to
// stderr, never into the DAP stream.
func TestBridgeRelaysStdioToTheAdapter(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var errb bytes.Buffer
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { done <- Run(ctx, fakeArgv(t), inR, outW, &errb); _ = outW.Close() }()
	r := bufio.NewReader(outR)
	_, _ = io.WriteString(inW, "hello\n")
	line, err := r.ReadString('\n')
	if err != nil || line != "re:hello\n" {
		t.Fatalf("reply %q %v", line, err)
	}
	_ = inW.Close() // the client is gone: the bridge ends
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the bridge did not end after stdin closed")
	}
	if rest, _ := io.ReadAll(r); len(rest) != 0 {
		t.Fatalf("extra bytes in the DAP stream: %q", rest)
	}
	if !strings.Contains(errb.String(), "some log line first") {
		t.Fatalf("adapter output not on stderr: %q", errb.String())
	}
}

// An adapter that exits without an address is an error that says so.
func TestBridgeAdapterWithoutAddress(t *testing.T) {
	var out, errb bytes.Buffer
	err := Run(context.Background(), []string{"/bin/sh", "-c", "echo no address here"}, strings.NewReader(""), &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "address") {
		t.Fatalf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestBridgeNeedsACommand(t *testing.T) {
	if err := Run(context.Background(), nil, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("no command must be an error")
	}
}
