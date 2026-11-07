package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// client talks to a Server over pipes, like an editor does.
type client struct {
	t    *testing.T
	in   *io.PipeWriter
	out  *bufio.Scanner
	done chan error
}

func startClient(t *testing.T, s *Server) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &client{t: t, in: inW, out: bufio.NewScanner(outR), done: make(chan error, 1)}
	go func() { c.done <- s.Serve(context.Background(), inR, outW); outW.Close() }()
	t.Cleanup(func() {
		inW.Close()
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not end after stdin closed")
		}
	})
	return c
}

func (c *client) send(v string) {
	c.t.Helper()
	if _, err := io.WriteString(c.in, v+"\n"); err != nil {
		c.t.Fatal(err)
	}
}

// next reads one message from the server.
func (c *client) next() map[string]any {
	c.t.Helper()
	ch := make(chan map[string]any, 1)
	go func() {
		if !c.out.Scan() {
			ch <- nil
			return
		}
		var m map[string]any
		if err := json.Unmarshal(c.out.Bytes(), &m); err != nil {
			c.t.Errorf("bad json %q: %v", c.out.Bytes(), err)
		}
		ch <- m
	}()
	select {
	case m := <-ch:
		if m == nil {
			c.t.Fatal("server closed the stream")
		}
		return m
	case <-time.After(5 * time.Second):
		c.t.Fatal("no message from the server")
	}
	return nil
}

func fakeStream(reply ...string) (StreamFunc, *[][]ai.Message) {
	var mu sync.Mutex
	var seen [][]ai.Message
	return func(ctx context.Context, msgs []ai.Message) <-chan ai.Chunk {
		mu.Lock()
		seen = append(seen, append([]ai.Message(nil), msgs...))
		mu.Unlock()
		ch := make(chan ai.Chunk, len(reply)+1)
		for _, r := range reply {
			ch <- ai.Chunk{Delta: r}
		}
		ch <- ai.Chunk{Done: true}
		close(ch)
		return ch
	}, &seen
}

// CMP-84: initialize, session/new, and a prompt that streams
// agent_message_chunk updates and ends with end_turn; the next prompt
// carries the history.
func TestServeChat(t *testing.T) {
	stream, seen := fakeStream("Hel", "lo")
	c := startClient(t, &Server{Name: "termocode", Version: "1.2.3", Stream: stream})

	c.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}`)
	r := c.next()
	res, _ := r["result"].(map[string]any)
	if r["id"].(float64) != 1 || res["protocolVersion"].(float64) != 1 {
		t.Fatalf("initialize %v", r)
	}
	if info, _ := res["agentInfo"].(map[string]any); info["name"] != "termocode" || info["version"] != "1.2.3" {
		t.Fatalf("agentInfo %v", res)
	}

	c.send(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/work/p","mcpServers":[]}}`)
	r = c.next()
	sid, _ := r["result"].(map[string]any)["sessionId"].(string)
	if sid == "" {
		t.Fatalf("session/new %v", r)
	}

	c.send(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"` + sid + `","prompt":[{"type":"text","text":"hi"},{"type":"resource","resource":{"uri":"file:///work/p/a.go","text":"package a"}}]}}`)
	var text strings.Builder
	for {
		r = c.next()
		if r["method"] == "session/update" {
			p := r["params"].(map[string]any)
			u := p["update"].(map[string]any)
			if p["sessionId"] != sid || u["sessionUpdate"] != "agent_message_chunk" {
				t.Fatalf("update %v", r)
			}
			text.WriteString(u["content"].(map[string]any)["text"].(string))
			continue
		}
		break
	}
	if r["id"].(float64) != 3 || r["result"].(map[string]any)["stopReason"] != "end_turn" || text.String() != "Hello" {
		t.Fatalf("prompt end %v, text %q", r, text.String())
	}
	first := (*seen)[0]
	if first[0].Role != ai.RoleSystem || !strings.Contains(first[0].Content, "/work/p") {
		t.Fatalf("system message %+v", first[0])
	}
	if u := first[len(first)-1]; u.Role != ai.RoleUser || !strings.Contains(u.Content, "hi") || !strings.Contains(u.Content, "package a") || !strings.Contains(u.Content, "a.go") {
		t.Fatalf("user message %+v", u)
	}

	c.send(`{"jsonrpc":"2.0","id":4,"method":"session/prompt","params":{"sessionId":"` + sid + `","prompt":[{"type":"text","text":"again"}]}}`)
	for r = c.next(); r["method"] == "session/update"; r = c.next() {
	}
	second := (*seen)[1]
	if len(second) != 4 || second[2].Role != ai.RoleAssistant || second[2].Content != "Hello" {
		t.Fatalf("history %+v", second)
	}
}

// session/cancel stops a running prompt with stopReason cancelled.
func TestServeCancel(t *testing.T) {
	started := make(chan struct{})
	stream := func(ctx context.Context, msgs []ai.Message) <-chan ai.Chunk {
		ch := make(chan ai.Chunk, 1)
		go func() {
			close(started)
			<-ctx.Done()
			ch <- ai.Chunk{Done: true, Err: ctx.Err()}
			close(ch)
		}()
		return ch
	}
	c := startClient(t, &Server{Name: "termocode", Stream: stream})
	c.send(`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/w"}}`)
	sid := c.next()["result"].(map[string]any)["sessionId"].(string)
	c.send(`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"` + sid + `","prompt":[{"type":"text","text":"long"}]}}`)
	<-started
	c.send(`{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"` + sid + `"}}`)
	r := c.next()
	if r["id"].(float64) != 2 || r["result"].(map[string]any)["stopReason"] != "cancelled" {
		t.Fatalf("cancel %v", r)
	}
}

// Errors: an unknown method, an unknown session and a failing provider
// answer with JSON-RPC errors; a bad line does not stop the server.
func TestServeErrors(t *testing.T) {
	fail := func(ctx context.Context, msgs []ai.Message) <-chan ai.Chunk {
		ch := make(chan ai.Chunk, 1)
		ch <- ai.Chunk{Done: true, Err: errors.New("no AI provider configured")}
		close(ch)
		return ch
	}
	c := startClient(t, &Server{Name: "termocode", Stream: fail})
	c.send(`not json`)
	if r := c.next(); r["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Fatalf("parse error %v", r)
	}
	c.send(`{"jsonrpc":"2.0","id":1,"method":"nope"}`)
	if r := c.next(); r["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatalf("unknown method %v", r)
	}
	c.send(`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"x","prompt":[]}}`)
	if r := c.next(); r["error"] == nil {
		t.Fatalf("unknown session %v", r)
	}
	c.send(`{"jsonrpc":"2.0","id":3,"method":"session/new","params":{"cwd":"/w"}}`)
	sid := c.next()["result"].(map[string]any)["sessionId"].(string)
	c.send(`{"jsonrpc":"2.0","id":4,"method":"session/prompt","params":{"sessionId":"` + sid + `","prompt":[{"type":"text","text":"x"}]}}`)
	r := c.next()
	if e, _ := r["error"].(map[string]any); e == nil || !strings.Contains(e["message"].(string), "no AI provider") {
		t.Fatalf("provider error %v", r)
	}
}
