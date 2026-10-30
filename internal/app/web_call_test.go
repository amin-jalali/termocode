//go:build unix

package app

import (
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
