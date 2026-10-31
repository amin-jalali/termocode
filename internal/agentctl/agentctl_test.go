package agentctl

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spec: one JSON request per line gets one reply; an error reply is an
// error for the caller; a peer that fails the check is cut off; a bad
// line gets an error reply and the connection stays.
func TestServeAndCall(t *testing.T) {
	dir, err := os.MkdirTemp("", "actl") // short: macOS caps socket paths at 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Skip("no unix sockets:", err)
	}
	defer ln.Close()
	go Serve(ln, nil, func(r Request) Reply {
		switch r.Op {
		case OpList:
			return Reply{OK: true, Tabs: []Tab{{ID: "t1", Name: "zsh", Agent: "claude", State: "waiting"}}}
		case OpWait:
			time.Sleep(50 * time.Millisecond)
			return Reply{OK: true, State: r.Until}
		}
		return Reply{Error: "unknown op " + r.Op}
	})
	rep, err := Call(path, Request{Op: OpList}, 5*time.Second)
	if err != nil || len(rep.Tabs) != 1 || rep.Tabs[0].State != "waiting" {
		t.Fatalf("list %+v %v", rep, err)
	}
	if rep, err := Call(path, Request{Op: OpWait, Until: "done"}, 5*time.Second); err != nil || rep.State != "done" {
		t.Fatalf("wait %+v %v", rep, err)
	}
	if _, err := Call(path, Request{Op: "nope"}, 5*time.Second); err == nil || !strings.Contains(err.Error(), "unknown op") {
		t.Fatalf("error reply: %v", err)
	}
	if _, err := Call(filepath.Join(dir, "none.sock"), Request{Op: OpList}, time.Second); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("no socket: %v", err)
	}

	path2 := filepath.Join(dir, "b.sock")
	ln2, _ := net.Listen("unix", path2)
	defer ln2.Close()
	go Serve(ln2, func(net.Conn) error { return net.ErrClosed }, func(Request) Reply { return Reply{OK: true} })
	if _, err := Call(path2, Request{Op: OpList}, 2*time.Second); err == nil {
		t.Fatal("a refused peer got an answer")
	}
	if !ValidState("done") || ValidState("idle") {
		t.Error("ValidState")
	}
}
