package dbclient

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// scriptedMemcached answers each request line with reply(line, reader)
// (the reader is for the data block of a store); an empty answer closes
// the connection. It counts the dials.
func scriptedMemcached(dials *int, reply func(line string, r *bufio.Reader) string) DialFunc {
	return func(_ context.Context, _, _ string) (net.Conn, error) {
		*dials++
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			r := bufio.NewReader(b)
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				out := reply(strings.TrimRight(line, "\r\n"), r)
				if out == "" {
					return
				}
				if _, err := b.Write([]byte(out)); err != nil {
					return
				}
			}
		}()
		return a, nil
	}
}

func TestMemcachedAddressRules(t *testing.T) {
	if _, err := NewMemcachedSession(Conn{}); err == nil || !strings.Contains(err.Error(), "no server address") {
		t.Errorf("no host = %v", err)
	}
	s, err := NewMemcachedSession(Conn{URL: "memcached://cache.example:12345"})
	if err != nil || s.addr != "cache.example:12345" {
		t.Errorf("URL address = %+v, %v", s, err)
	}
	s, err = NewMemcachedSession(Conn{Host: "h"})
	if err != nil || s.addr != "h:11211" {
		t.Errorf("default port = %+v, %v", s, err)
	}
	for k, ok := range map[string]bool{"": false, "a b": false, "a\x00b": false, "a\x7fb": false, strings.Repeat("k", 251): false, "fine:key": true, strings.Repeat("k", 250): true} {
		if got := memcachedKey(k) == nil; got != ok {
			t.Errorf("memcachedKey(%q) ok = %v", k[:min(len(k), 10)], got)
		}
	}
	if got := keyArgs("set", []string{"k", "v"}); len(got) != 1 {
		t.Errorf("keyArgs(set) = %v", got)
	}
	if keyArgs("set", nil) != nil || keyArgs("stats", []string{"x"}) != nil {
		t.Error("keyArgs of nothing")
	}
}

func TestMemcachedRefusalsBeforeTheServer(t *testing.T) {
	dials := 0
	s, _ := NewMemcachedSession(Conn{Host: "h", Dial: scriptedMemcached(&dials, func(string, *bufio.Reader) string { return "END\r\n" })})
	defer s.Close()
	ctx := context.Background()
	for line, why := range map[string]string{
		"":                  "write get",
		"   ":               "write get",
		"get":               "get KEY",
		"set k":             "set KEY VALUE",
		"set k v 1 2":       "set KEY VALUE",
		"set k v soon":      "TTL is seconds",
		"delete":            "delete KEY",
		"delete a b":        "delete KEY",
		"touch k":           "touch KEY TTL",
		"touch k soon":      "TTL is seconds",
		"stats nope":        "stats [items",
		"stats items extra": "stats [items",
		"frobnicate":        "unknown command",
		"flush_all":         "flush_all is not sent",
		`set "unterminated`: "",
	} {
		r := s.Run(ctx, line, RunOptions{})
		if r.Err == nil || (why != "" && !strings.Contains(r.Err.Error(), why)) {
			t.Errorf("%q = %v, want %q", line, r.Err, why)
		}
	}
	if dials != 0 {
		t.Errorf("a refused line must not reach the server (%d dials)", dials)
	}
	ro, _ := s.ReadOnlySession()
	for _, line := range []string{"set k v", "delete k", "touch k 1", "add k v", "replace k v"} {
		if r := ro.Run(ctx, line, RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "read-only") {
			t.Errorf("read-only %q = %v", line, r.Err)
		}
	}
	for _, f := range []func(context.Context) error{s.Begin, s.Commit, s.Rollback} {
		if f(ctx) == nil {
			t.Error("memcached has no transactions")
		}
	}
	s.Cancel() // nothing waiting
}

func TestMemcachedServerAnswersAndFailures(t *testing.T) {
	dials := 0
	var s *MemcachedSession
	mode := ""
	dial := scriptedMemcached(&dials, func(line string, r *bufio.Reader) string {
		f := strings.Fields(line)
		switch {
		case f[0] == "version":
			return "VERSION 1.6\r\n"
		case mode == "badvalue":
			return "VALUE k\r\nEND\r\n"
		case mode == "badlen":
			return "VALUE k 0 -5\r\nEND\r\n"
		case mode == "hugelen":
			return "VALUE k 0 4000000000\r\nEND\r\n"
		case mode == "servererr":
			return "SERVER_ERROR out of memory\r\n"
		case mode == "clienterr":
			return "CLIENT_ERROR bad command line format\r\n"
		case mode == "drop":
			return ""
		case f[0] == "get":
			return "VALUE k 5 3\r\nabc\r\nEND\r\n"
		case f[0] == "add" || f[0] == "replace" || f[0] == "set":
			_, _ = r.ReadString('\n')
			if mode == "notstored" {
				return "NOT_STORED\r\n"
			}
			return "STORED\r\n"
		case f[0] == "touch":
			return "TOUCHED\r\n"
		case f[0] == "stats":
			return "STAT a 1\r\nSTAT b x y\r\nEND\r\n"
		}
		return "ERROR\r\n"
	})
	s, _ = NewMemcachedSession(Conn{Host: "h", Dial: dial})
	defer s.Close()
	ctx := context.Background()
	if r := s.Run(ctx, "get k", RunOptions{}); r.Err != nil || len(r.Rows) != 1 || r.Rows[0][2].Text != "5" || r.Rows[0][3].Text != "3" {
		t.Errorf("get = %+v", r)
	}
	if r := s.Run(ctx, "gets k", RunOptions{}); r.Err != nil {
		t.Errorf("gets = %v", r.Err)
	}
	for _, line := range []string{"add k v", "replace k v 30"} {
		if r := s.Run(ctx, line, RunOptions{}); r.Err != nil || r.Affected != 1 {
			t.Errorf("%s = %+v", line, r)
		}
	}
	if r := s.Run(ctx, "touch k 10", RunOptions{}); r.Err != nil || r.Affected != 1 {
		t.Errorf("touch = %+v", r)
	}
	if r := s.Run(ctx, "stats items", RunOptions{}); r.Err != nil || len(r.Rows) != 2 || r.Rows[1][1].Text != "x y" {
		t.Errorf("stats items = %+v", r)
	}
	mode = "notstored"
	if r := s.Run(ctx, "add k v", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "NOT_STORED") {
		t.Errorf("a refused add = %v", r.Err)
	}
	for _, m := range []string{"badvalue", "badlen", "hugelen", "servererr", "clienterr"} {
		mode = m
		if r := s.Run(ctx, "get k", RunOptions{}); r.Err == nil {
			t.Errorf("%s: want an error", m)
		}
	}
	// A dropped connection is an error, and the next command redials.
	mode = "drop"
	before := dials
	if r := s.Run(ctx, "get k", RunOptions{}); r.Err == nil {
		t.Error("a dropped connection: want error")
	}
	mode = ""
	if r := s.Run(ctx, "get k", RunOptions{}); r.Err != nil {
		t.Errorf("after a drop the session reconnects: %v", r.Err)
	}
	if dials <= before {
		t.Error("the connection should have been dialed again")
	}
	// A closed session.
	_ = s.Close()
	if r := s.Run(ctx, "get k", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "closed") {
		t.Errorf("closed = %v", r.Err)
	}
}

func TestMemcachedConnectFailuresAndCancel(t *testing.T) {
	down := errors.New("connection refused")
	s, _ := NewMemcachedSession(Conn{Host: "h", Dial: func(context.Context, string, string) (net.Conn, error) { return nil, down }})
	if err := s.Ping(context.Background()); !errors.Is(err, down) {
		t.Errorf("an unreachable server = %v", err)
	}
	// TLS on a server that speaks plain text: the handshake fails.
	dials := 0
	tlsS, _ := NewMemcachedSession(Conn{Host: "h", TLS: true, Dial: scriptedMemcached(&dials, func(string, *bufio.Reader) string { return "ERROR\r\n" })})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := tlsS.Ping(ctx); err == nil {
		t.Error("TLS to a plain server: want error")
	}
	// Cancel breaks a command that waits for the server.
	hang := scriptedMemcached(&dials, func(line string, _ *bufio.Reader) string {
		if strings.HasPrefix(line, "version") {
			return "VERSION 1\r\n"
		}
		time.Sleep(3 * time.Second)
		return "END\r\n"
	})
	w, _ := NewMemcachedSession(Conn{Host: "h", Dial: hang})
	defer w.Close()
	if err := w.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(100*time.Millisecond, w.Cancel)
	start := time.Now()
	if r := w.Run(context.Background(), "get k", RunOptions{}); r.Err == nil {
		t.Error("a cancelled command: want error")
	}
	if time.Since(start) > 2*time.Second {
		t.Error("Cancel should end the wait at once")
	}
	// The context ends the wait too.
	c2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	if r := w.Run(c2, "get k", RunOptions{}); r.Err == nil {
		t.Error("a timed out command: want error")
	}
}
