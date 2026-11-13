package dbclient

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptResp is a scripted Redis server on 127.0.0.1: reply answers each
// command (upper-case name first) with a raw RESP value; "" closes.
type scriptResp struct {
	ln   net.Listener
	mu   sync.Mutex
	cmds []string
}

func newScriptResp(t *testing.T, reply func(args []string, w io.Writer) string) *scriptResp {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &scriptResp{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					args, err := readScriptRESP(r)
					if err != nil {
						return
					}
					s.mu.Lock()
					s.cmds = append(s.cmds, strings.Join(args, " "))
					s.mu.Unlock()
					out := reply(args, c)
					if out == "" {
						return
					}
					if _, err := io.WriteString(c, out); err != nil {
						return
					}
				}
			}()
		}
	}()
	return s
}

func (s *scriptResp) addr() string { return s.ln.Addr().String() }

func (s *scriptResp) sent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.cmds, "\n")
}

func readScriptRESP(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(line[1:]))
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		l, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, _ := strconv.Atoi(strings.TrimSpace(l[1:]))
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

func respBulk(s string) string { return fmt.Sprintf("$%d\r\n%s\r\n", len(s), s) }

func scriptConn(addr string, params map[string]string) Conn {
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	return Conn{Driver: "redis", Host: host, Port: p, Params: params}
}

func scriptBase(args []string) string {
	switch strings.ToUpper(args[0]) {
	case "PING":
		return "+PONG\r\n"
	case "AUTH", "SELECT", "CLIENT", "ASKING":
		return "+OK\r\n"
	}
	return ""
}

func TestRedisFollowsMovedAndAskRedirects(t *testing.T) {
	nodeB := newScriptResp(t, func(args []string, _ io.Writer) string {
		if r := scriptBase(args); r != "" {
			return r
		}
		return respBulk("on-b")
	})
	mode := "MOVED"
	nodeA := newScriptResp(t, func(args []string, _ io.Writer) string {
		if r := scriptBase(args); r != "" {
			return r
		}
		return "-" + mode + " 3999 " + nodeB.addr() + "\r\n"
	})
	s, err := NewRedisSession(context.Background(), scriptConn(nodeA.addr(), nil), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if r := s.Run(ctx, "GET k", RunOptions{}); r.Err != nil || len(r.Rows) == 0 || !strings.Contains(r.Rows[0][0].Text, "on-b") {
		t.Errorf("MOVED = %+v", r)
	}
	mode = "ASK"
	if r := s.Run(ctx, "GET k", RunOptions{}); r.Err != nil {
		t.Errorf("ASK = %+v", r)
	}
	if !strings.Contains(nodeB.sent(), "ASKING") {
		t.Errorf("an ASK redirect sends ASKING first; node B got:\n%s", nodeB.sent())
	}
	// The other node is gone.
	_ = nodeB.ln.Close()
	if r := s.Run(ctx, "GET k", RunOptions{}); r.Err == nil {
		t.Error("a redirect to a node that is gone: want error")
	}
}

func TestRedisFindsTheMasterThroughSentinels(t *testing.T) {
	master := newScriptResp(t, func(args []string, _ io.Writer) string {
		if r := scriptBase(args); r != "" {
			return r
		}
		return respBulk("master-value")
	})
	mh, mp, _ := net.SplitHostPort(master.addr())
	known := true
	sentinel := newScriptResp(t, func(args []string, _ io.Writer) string {
		if strings.EqualFold(args[0], "SENTINEL") {
			if !known {
				return "*-1\r\n"
			}
			return "*2\r\n" + respBulk(mh) + respBulk(mp)
		}
		return scriptBase(args)
	})
	c := scriptConn(sentinel.addr(), map[string]string{"sentinel": "mymaster"})
	s, err := NewRedisSession(context.Background(), c, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := s.Run(context.Background(), "GET k", RunOptions{}); r.Err != nil {
		t.Errorf("through the master = %+v", r)
	}
	known = false
	if _, err := NewRedisSession(context.Background(), c, "", nil); err == nil || !strings.Contains(err.Error(), "does not know") {
		t.Errorf("an unknown master = %v", err)
	}
	opts, _ := s.dialOpts(0)
	if _, err := redisSentinelMaster(context.Background(), opts, nil, "m"); err == nil {
		t.Error("no sentinels: want error")
	}
	if _, err := redisSentinelMaster(context.Background(), opts, []string{"127.0.0.1:1"}, "m"); err == nil {
		t.Error("an unreachable sentinel: want error")
	}
}

func TestRedisListenCollectsMessages(t *testing.T) {
	published := make(chan struct{})
	srv := newScriptResp(t, func(args []string, w io.Writer) string {
		switch strings.ToUpper(args[0]) {
		case "PSUBSCRIBE":
			go func() {
				time.Sleep(50 * time.Millisecond)
				for i := 0; i < 3; i++ {
					_, _ = io.WriteString(w, "*4\r\n"+respBulk("pmessage")+respBulk("news.*")+respBulk("news.a")+respBulk("m"+strconv.Itoa(i)))
				}
				close(published)
			}()
			return "*3\r\n" + respBulk("psubscribe") + respBulk(args[1]) + ":1\r\n"
		}
		return scriptBase(args)
	})
	s, err := NewRedisSession(context.Background(), scriptConn(srv.addr(), nil), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	msgs, more, err := s.Listen(context.Background(), "news.*", 2)
	if err != nil || !more || len(msgs) != 2 || msgs[0].Data != "m0" || msgs[0].Channel != "news.a" {
		t.Errorf("listen to the cap = %+v, %v, %v", msgs, more, err)
	}
	<-published
	// The end of the context is the normal end.
	quiet := newScriptResp(t, func(args []string, _ io.Writer) string {
		if strings.ToUpper(args[0]) == "PSUBSCRIBE" {
			return "*3\r\n" + respBulk("psubscribe") + respBulk(args[1]) + ":1\r\n"
		}
		return scriptBase(args)
	})
	q, err := NewRedisSession(context.Background(), scriptConn(quiet.addr(), nil), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if msgs, more, err := q.Listen(ctx, "x", 5); err != nil || more || len(msgs) != 0 {
		t.Errorf("a quiet channel until the deadline = %+v %v %v", msgs, more, err)
	}
	if res := RedisMessagesResult(nil, false); len(res.Notices) != 1 {
		t.Errorf("no messages = %+v", res)
	}
	if res := RedisMessagesResult([]RedisMessage{{Channel: "c", Pattern: "p", Data: "d"}}, true); !res.Truncated || len(res.Rows) != 1 {
		t.Errorf("messages = %+v", res)
	}
	// A server that refuses the subscription.
	deny := newScriptResp(t, func(args []string, _ io.Writer) string {
		if strings.ToUpper(args[0]) == "PSUBSCRIBE" {
			return "-ERR no pubsub here\r\n"
		}
		return scriptBase(args)
	})
	d, err := NewRedisSession(context.Background(), scriptConn(deny.addr(), nil), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, _, err := d.Listen(context.Background(), "x", 1); err == nil {
		t.Error("a refused subscription: want error")
	}
}
