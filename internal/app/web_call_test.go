//go:build unix

package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEB-6, never-do 19): a request runs on Update and only there. The
// op below writes a Model field; under -race any run off the test
// goroutine (which plays Update) is a data race. It has not run before
// Update takes the message, and its reply comes back as res with the id.
func TestCallRunsOnUpdateGoroutine(t *testing.T) {
	r := newWebRig(t, nil)
	r.hub.ops = map[string]webOpFunc{
		webproto.OpChatOpen: func(m *Model, call *webCall) tea.Cmd {
			m.err = "touched by " + call.args.(*webproto.ChatOpenArgs).ID
			call.reply(map[string]string{"opened": "yes"})
			return nil
		},
	}
	c, _ := r.webHello(webTestOpen)
	c.send(`{"t":"req","id":1,"op":"chat.open","args":{"id":"c7"}}`)
	deadline := time.Now().Add(10 * time.Second)
	for len(r.hub.ch) == 0 { // the call waits for Update
		if time.Now().After(deadline) {
			t.Fatal("the call never reached Update")
		}
		time.Sleep(time.Millisecond)
	}
	if r.m.err != "" {
		t.Fatalf("the op ran before Update: %q", r.m.err)
	}
	res := r.nextMsg(c)
	if res["t"] != "res" || res["id"] != float64(1) {
		t.Fatalf("answer %v", res)
	}
	if d, _ := res["data"].(map[string]any); d["opened"] != "yes" {
		t.Fatalf("data %v", res["data"])
	}
	if r.m.err != "touched by c7" {
		t.Fatalf("model %q", r.m.err)
	}
}

// Spec (WEB-6, T46): the hub checks every request again: ids go up, the
// op has a policy row and a handler, the role may call it, the args
// decode strictly within the op's size. A refused request is one err
// with its id; only a reused id ends the connection.
func TestHubRequestChecks(t *testing.T) {
	r := newWebRig(t, nil)
	ran := 0
	r.hub.ops = map[string]webOpFunc{
		webproto.OpChatOpen: func(m *Model, call *webCall) tea.Cmd { ran++; call.reply(nil); return nil },
		webproto.OpChatSend: func(m *Model, call *webCall) tea.Cmd { ran++; call.reply(nil); return nil },
	}
	viewer, _ := r.webHello(`{"t":"open","conn":"v-1","role":"viewer","device":"d"}`)
	for _, tc := range []struct{ line, code string }{
		{`{"t":"req","id":1,"op":"no.such","args":{}}`, "bad_request"},
		{`{"t":"req","id":2,"op":"chat.send","args":{"text":"hi"}}`, "forbidden"},
		{`{"t":"req","id":3,"op":"web.stop","args":{}}`, "forbidden"},
		{`{"t":"req","id":4,"op":"chat.open","args":{"id":"x","more":1}}`, "bad_request"},
		{`{"t":"req","id":5,"op":"chat.open","args":{"id":"` + strings.Repeat("a", 2<<10) + `"}}`, "limit"},
		{`{"t":"req","id":6,"op":"tree.list","args":{"path":"a"}}`, "not_found"},
	} {
		viewer.send(tc.line)
		got := r.nextMsg(viewer)
		var want struct {
			ID uint64 `json:"id"`
		}
		_ = json.Unmarshal([]byte(tc.line), &want)
		if got["t"] != "err" || got["code"] != tc.code || got["id"] != float64(want.ID) {
			t.Errorf("%s: got %v, want err %s", tc.line, got, tc.code)
		}
	}
	viewer.send(`{"t":"req","id":7,"op":"chat.open","args":{"id":"x"}}`)
	if got := r.nextMsg(viewer); got["t"] != "res" || ran != 1 {
		t.Fatalf("good read: %v (ran %d)", got, ran)
	}
	viewer.send(`{"t":"req","id":7,"op":"chat.open","args":{"id":"x"}}`)
	if got := r.nextMsg(viewer); got["t"] != "err" || got["code"] != "bad_request" {
		t.Fatalf("reused id: %v", got)
	}
	r.waitEOF(viewer)
	if ran != 1 {
		t.Fatalf("ran %d times", ran)
	}

	// The control connection may call control ops only.
	r.hub.ops[webproto.OpWebStop] = func(m *Model, call *webCall) tea.Cmd { call.reply(nil); return nil }
	ctl := r.dial(`{"t":"ctl"}`)
	ctl.send(`{"t":"req","id":1,"op":"chat.open","args":{"id":"x"}}`)
	if got := r.nextMsg(ctl); got["code"] != "forbidden" {
		t.Fatalf("ctl browser op: %v", got)
	}
	ctl.send(`{"t":"req","id":2,"op":"web.stop"}`)
	if got := r.nextMsg(ctl); got["t"] != "res" || got["id"] != float64(2) {
		t.Fatalf("ctl op: %v", got)
	}
}

// Spec (WEB-6): a panic in a handler is err internal for that request;
// the editor goes on and the next request works.
func TestCallPanicIsError(t *testing.T) {
	r := newWebRig(t, nil)
	r.hub.ops = map[string]webOpFunc{
		webproto.OpChatOpen: func(m *Model, call *webCall) tea.Cmd {
			if call.args.(*webproto.ChatOpenArgs).ID == "boom" {
				panic("boom")
			}
			call.reply("ok")
			return nil
		},
	}
	c, _ := r.webHello(webTestOpen)
	c.send(`{"t":"req","id":1,"op":"chat.open","args":{"id":"boom"}}`)
	if got := r.nextMsg(c); got["t"] != "err" || got["code"] != "internal" || got["id"] != float64(1) {
		t.Fatalf("panic: %v", got)
	}
	c.send(`{"t":"req","id":2,"op":"chat.open","args":{"id":"fine"}}`)
	if got := r.nextMsg(c); got["t"] != "res" || got["data"] != "ok" {
		t.Fatalf("after the panic: %v", got)
	}
}

// Spec (WEB-6): every request gets exactly one answer: a handler that
// returns without one is err internal; one that says later answers from
// another goroutine, and a second answer is dropped.
func TestCallAnswersOnce(t *testing.T) {
	r := newWebRig(t, nil)
	r.hub.ops = map[string]webOpFunc{
		webproto.OpChatOpen: func(m *Model, call *webCall) tea.Cmd { return nil },
		webproto.OpChatPin: func(m *Model, call *webCall) tea.Cmd {
			call.later()
			go func() { // slow work off Update, answered from there
				call.reply("later")
				call.reply("twice")
			}()
			return nil
		},
	}
	c, _ := r.webHello(webTestOpen)
	c.send(`{"t":"req","id":1,"op":"chat.open","args":{"id":"x"}}`)
	if got := r.nextMsg(c); got["t"] != "err" || got["code"] != "internal" {
		t.Fatalf("no answer: %v", got)
	}
	c.send(`{"t":"req","id":2,"op":"chat.pin","args":{"on":true}}`)
	if got := r.nextMsg(c); got["t"] != "res" || got["data"] != "later" {
		t.Fatalf("later: %v", got)
	}
	c.send(`{"t":"ping","n":"x"}`)
	if got := r.nextMsg(c); got["t"] != "pong" {
		t.Fatalf("a second answer went out: %v", got)
	}
}

// Spec (WEB-6): a request whose connection is gone before Update takes it
// does not run; a running slow request sees its context end when the
// connection ends, and every context ends when the hub stops.
func TestCallCtxCancelled(t *testing.T) {
	r := newWebRig(t, nil)
	ran := 0
	slow := make(chan context.Context, 1)
	r.hub.ops = map[string]webOpFunc{
		webproto.OpChatOpen: func(m *Model, call *webCall) tea.Cmd { ran++; call.reply(nil); return nil },
		webproto.OpChatPin: func(m *Model, call *webCall) tea.Cmd {
			call.later()
			slow <- call.ctx
			return nil
		},
	}
	c, _ := r.webHello(webTestOpen)
	c.send(`{"t":"req","id":1,"op":"chat.open","args":{"id":"x"}}`)
	deadline := time.Now().Add(10 * time.Second)
	for len(r.hub.ch) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the call never reached Update")
		}
		time.Sleep(time.Millisecond)
	}
	_ = c.c.Close()
	for r.hub.conns() != 0 { // the hub saw the close (no Update needed)
		if time.Now().After(deadline) {
			t.Fatal("the connection was not dropped")
		}
		time.Sleep(time.Millisecond)
	}
	r.m.handleWebMsg(<-r.hub.ch)
	if ran != 0 {
		t.Fatal("a call of a closed connection ran")
	}

	c2, _ := r.webHello(`{"t":"open","conn":"c-2","role":"controller","device":"d"}`)
	c2.send(`{"t":"req","id":1,"op":"chat.pin","args":{"on":true}}`)
	var ctx context.Context
	r.pumpUntil("the slow call", func() bool {
		select {
		case ctx = <-slow:
			return true
		default:
			return false
		}
	})
	if ctx.Err() != nil {
		t.Fatal("context ended too early")
	}
	r.hub.stop()
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the slow call's context did not end with the hub")
	}
}
