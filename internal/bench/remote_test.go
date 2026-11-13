package bench

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// screenServer sends "screen<n>" on connect, and 100 bytes plus the key it got
// for every byte it receives: a stand-in for an editor over SSH.
func screenServer(t *testing.T) (addr string, conns *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	conns = new(atomic.Int32)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n := conns.Add(1)
			go func() {
				defer c.Close()
				fmt.Fprintf(c, "screen%d\n", n)
				b := make([]byte, 1)
				for {
					if _, err := io.ReadFull(c, b); err != nil {
						return
					}
					_, _ = c.Write([]byte(strings.Repeat(".", 100) + string(b)))
				}
			}()
		}
	}()
	return ln.Addr().String(), conns
}

func tcpClient(t *testing.T, addr string, loop bool) *Session {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	script := fmt.Sprintf("exec 3<>/dev/tcp/%s/%s; cat <&0 >&3 & P=$!; cat <&3; kill $P", host, port)
	if loop {
		script = fmt.Sprintf("while true; do ( %s ) ; sleep 0.2; done", script)
	}
	s, err := Start(Spec{Bin: "bash", Args: []string{"-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestBytesPerKeyCountsTheWholeFrame(t *testing.T) {
	addr, _ := screenServer(t)
	link, err := NewSlowLink(addr, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()
	s := tcpClient(t, link.Addr(), false)
	if _, err := s.WaitForText("screen1", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	n, lat, err := BytesPerKey(s, "Z\n", strings.Repeat(".", 100)+"Z", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if n < 101 {
		t.Fatalf("bytes per key %d, want >= 101", n)
	}
	if lat < 40*time.Millisecond {
		t.Fatalf("latency %v must include the round trip of the slow link", lat)
	}
}

func TestReconnectTimeAfterADropIsMeasured(t *testing.T) {
	addr, conns := screenServer(t)
	link, err := NewSlowLink(addr, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()
	s := tcpClient(t, link.Addr(), true) // reconnects by itself, like an SSH wrapper
	if _, err := s.WaitForText("screen1", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	d, _, err := Reconnect(s, link.Drop, nil, "screen2", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if d < 150*time.Millisecond || d > 5*time.Second {
		t.Fatalf("reconnect %v (client sleeps 200ms between tries)", d)
	}
	if conns.Load() < 2 {
		t.Fatal("no second connection")
	}
}

func TestReconnectRelaunchesAProgramThatExits(t *testing.T) {
	addr, _ := screenServer(t)
	link, _ := NewSlowLink(addr, 5*time.Millisecond)
	defer link.Close()
	s := tcpClient(t, link.Addr(), false) // plain client: dies with the link
	if _, err := s.WaitForText("screen1", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	var again *Session
	relaunch := func() (*Session, error) { again = tcpClient(t, link.Addr(), false); return again, nil }
	d, ns, err := Reconnect(s, link.Drop, relaunch, "screen2", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if ns != again || d <= 0 {
		t.Fatalf("d=%v ns=%v", d, ns)
	}
}

func TestReconnectTimesOut(t *testing.T) {
	addr, _ := screenServer(t)
	link, _ := NewSlowLink(addr, 5*time.Millisecond)
	defer link.Close()
	s := tcpClient(t, link.Addr(), true)
	_, _ = s.WaitForText("screen1", 5*time.Second)
	if _, _, err := Reconnect(s, link.Drop, nil, "never", 600*time.Millisecond); err == nil {
		t.Fatal("want a timeout")
	}
}

func TestSSHArgsAreSafeAndExact(t *testing.T) {
	a := SSHArgs(RemoteCfg{Host: "h.example", Port: 2222, User: "bob"}, "tmux", "attach")
	want := []string{"-tt", "-o", "BatchMode=yes", "-o", "ServerAliveInterval=2", "-p", "2222", "bob@h.example", "tmux", "attach"}
	if strings.Join(a, " ") != strings.Join(want, " ") {
		t.Fatalf("%v", a)
	}
	for _, x := range a {
		if strings.Contains(strings.ToLower(x), "password") {
			t.Fatal("no secrets in argv")
		}
	}
}

func TestMultiplexerBaselineArgs(t *testing.T) {
	if got := strings.Join(BaselineCmd("tmux", "bench", "nvim", "f.txt"), " "); got != "tmux new-session -A -s bench nvim f.txt" {
		t.Fatal(got)
	}
	if BaselineCmd("zellij", "bench", "nvim") == nil {
		t.Fatal("zellij baseline")
	}
	if BaselineCmd("screen", "b", "x") != nil {
		t.Fatal("unknown multiplexer must give nil")
	}
}

func TestDetectRemoteSaysWhySkipped(t *testing.T) {
	t.Setenv("BENCH_SSH_HOST", "")
	if c, skip := DetectRemote(); c != nil || !strings.Contains(skip, "BENCH_SSH_HOST") {
		t.Fatalf("%v %q", c, skip)
	}
	t.Setenv("BENCH_SSH_HOST", "me@box")
	t.Setenv("BENCH_SSH_PORT", "2200")
	c, skip := DetectRemote()
	if c == nil && !strings.Contains(skip, "ssh") {
		t.Fatalf("skip %q", skip)
	}
	if c != nil && (c.Host != "box" || c.User != "me" || c.Port != 2200) {
		t.Fatalf("%+v", c)
	}
}
