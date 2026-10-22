package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// relayRig runs relayMCP over net.Pipe; srv is the server's end.
type relayRig struct {
	inW   *io.PipeWriter // the agent writes requests here (stdin)
	out   *bufio.Reader  // the agent reads replies here (stdout)
	srv   net.Conn
	srvR  *bufio.Reader
	errw  *syncBuf
	done  chan int
	outPW *io.PipeWriter
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuf) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func newRelayRig(t *testing.T) *relayRig {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	cli, srv := net.Pipe()
	r := &relayRig{inW: inW, out: bufio.NewReaderSize(outR, 1<<20), srv: srv, srvR: bufio.NewReaderSize(srv, 1<<20),
		errw: &syncBuf{}, done: make(chan int, 1), outPW: outW}
	go func() {
		code := relayMCP(inR, outW, cli, r.errw, "/x")
		_ = outW.Close()
		r.done <- code
	}()
	t.Cleanup(func() { _ = inW.Close(); _ = srv.Close() })
	return r
}

func (r *relayRig) exit(t *testing.T) int {
	t.Helper()
	select {
	case c := <-r.done:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("relay did not exit")
		return -1
	}
}

func readLine(t *testing.T, br *bufio.Reader) string {
	t.Helper()
	ch := make(chan string, 1)
	go func() { s, _ := br.ReadString('\n'); ch <- s }()
	select {
	case s := <-ch:
		return strings.TrimRight(s, "\n")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout reading a line")
		return ""
	}
}

func TestRelayBothWays(t *testing.T) {
	r := newRelayRig(t)
	go io.WriteString(r.inW, `{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n")
	if got := readLine(t, r.srvR); got != `{"jsonrpc":"2.0","id":1,"method":"ping"}` {
		t.Fatalf("server got %q", got)
	}
	go io.WriteString(r.srv, `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
	if got := readLine(t, r.out); got != `{"jsonrpc":"2.0","id":1,"result":{}}` {
		t.Fatalf("agent got %q", got)
	}
}

func TestRelayLargeMessage(t *testing.T) {
	r := newRelayRig(t)
	big := `{"jsonrpc":"2.0","id":7,"result":{"text":"` + strings.Repeat("x", 3<<20) + `"}}`
	go io.WriteString(r.srv, big+"\n")
	if got := readLine(t, r.out); got != big {
		t.Fatalf("large message changed: %d bytes, want %d", len(got), len(big))
	}
	req := `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"a":"` + strings.Repeat("y", 3<<20) + `"}}`
	go io.WriteString(r.inW, req+"\n")
	if got := readLine(t, r.srvR); got != req {
		t.Fatalf("large request changed: %d bytes, want %d", len(got), len(req))
	}
}

func TestRelayStdinEOFExitsZero(t *testing.T) {
	r := newRelayRig(t)
	_ = r.inW.Close()
	if c := r.exit(t); c != 0 {
		t.Fatalf("exit %d, want 0; stderr %q", c, r.errw.String())
	}
}

// The server closes (the UI quit): exit 1 with a note on stderr.
func TestRelayServerClosed(t *testing.T) {
	r := newRelayRig(t)
	_ = r.srv.Close()
	if c := r.exit(t); c != 1 {
		t.Fatalf("exit %d, want 1", c)
	}
	if !strings.Contains(r.errw.String(), "closed") {
		t.Fatalf("stderr %q", r.errw.String())
	}
}

// A pending request gets an error reply, so the agent does not wait
// forever; an answered one does not.
func TestRelayServerClosedAnswersPending(t *testing.T) {
	r := newRelayRig(t)
	go io.WriteString(r.inW, `{"jsonrpc":"2.0","id":1,"method":"a"}`+"\n"+`{"jsonrpc":"2.0","id":"s","method":"b"}`+"\n"+`{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	readLine(t, r.srvR)
	readLine(t, r.srvR)
	readLine(t, r.srvR)
	go io.WriteString(r.srv, `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
	readLine(t, r.out)
	_ = r.srv.Close()
	line := readLine(t, r.out)
	var m struct {
		ID    json.RawMessage `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(line, err)
	}
	if string(m.ID) != `"s"` || m.Error.Code != -32001 || m.Error.Message == "" {
		t.Fatalf("got %s", line)
	}
	r.exit(t)
}

// The server hangs up before any reply (peer check refused us): the
// error says so.
func TestRelayPeerRefusal(t *testing.T) {
	r := newRelayRig(t)
	go io.WriteString(r.inW, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`+"\n")
	readLine(t, r.srvR)
	_ = r.srv.Close()
	line := readLine(t, r.out)
	if !strings.Contains(line, "refused") || !strings.Contains(line, "-32001") {
		t.Fatalf("got %s", line)
	}
	if !strings.Contains(r.errw.String(), "refused") {
		t.Fatalf("stderr %q", r.errw.String())
	}
	r.exit(t)
}

// Stdout carries JSON-RPC only: a banner line from the socket is
// dropped to stderr.
func TestRelayStdoutOnlyJSON(t *testing.T) {
	r := newRelayRig(t)
	go io.WriteString(r.srv, "hello banner\n"+`{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
	if got := readLine(t, r.out); !strings.HasPrefix(got, "{") {
		t.Fatalf("stdout got %q", got)
	}
	if !strings.Contains(r.errw.String(), "hello banner") {
		t.Fatalf("stderr %q", r.errw.String())
	}
}

func TestUsageListsMCP(t *testing.T) {
	if !strings.Contains(mcpUsage, "termocode mcp [--dir <workspace>]") {
		t.Fatal("mcp usage text changed shape")
	}
}
