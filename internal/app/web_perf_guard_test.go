//go:build unix

package app

import (
	"encoding/json"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/perfbudget"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// WEB-27 perf guards (web design 5.5). They run with TERMOCODE_PERF=1
// (the perf job), not in the -race run.

// webRawFrames takes the raw frames queued on c (no waiting).
func webRawFrames(c *webConn) [][]byte {
	c.out.mu.Lock()
	empty := len(c.out.items) == 0 && len(c.out.owed) == 0
	c.out.mu.Unlock()
	if empty {
		return nil
	}
	fs, _ := c.out.take()
	return fs
}

// web_hub_call_p99: the time a request spends on the UI goroutine, for
// a mix of real ops (typing into a real nvim shell, chat, files, git),
// from 4 browsers at once, 320 calls. Budget: 20 ms (T41).
func TestPerfWebHubCallBudget(t *testing.T) {
	perfbudget.Require(t)
	r := newWebFilesRig(t)
	r.withNvim()
	buf := webTestShell(t, r, "sh")
	r.write("a.txt", "one\ntwo\n")
	conns := []*webConn{r.c}
	for i := 2; i <= 4; i++ {
		conns = append(conns, r.conn("c-"+strconv.Itoa(i), webproto.RoleController))
	}
	mix := []struct {
		op   string
		args any
	}{
		{webproto.OpTermSend, &webproto.TermSendArgs{Buf: buf, Text: "x"}},
		{webproto.OpChatPin, &webproto.ChatPinArgs{On: true}},
		{webproto.OpTreeList, &webproto.PathArgs{Path: ""}},
		{webproto.OpBufHead, &webproto.PathArgs{Path: "a.txt"}},
		{webproto.OpGitDiff, &webproto.GitDiffArgs{Path: "a.txt"}},
	}
	var took []time.Duration
	var id uint64
	for round := 0; round < 16; round++ {
		for _, c := range conns {
			for _, x := range mix {
				id++
				c.inflight.Add(1)
				call := &webCall{ctx: c.ctx, conn: c, id: id, op: x.op, args: x.args}
				t0 := time.Now()
				cmd, _ := r.m.webRunCall(r.h, call)
				took = append(took, time.Since(t0))
				webDrain(t, r.m, cmd) // the background part is not on the UI goroutine
				r.takeOn(c)
			}
		}
		_ = r.call(webproto.OpTermSend, &webproto.TermSendArgs{Buf: buf, Text: "\x15"}) // clear the line
	}
	slices.Sort(took)
	p99 := took[len(took)*99/100]
	perfbudget.Check(t, "web_hub_call_p99", float64(p99.Microseconds())/1000)
}

// web_publish_worst: publish to a browser whose gateway stopped reading
// never waits: the worst of 5 000 1 KiB publishes on the UI goroutine.
func TestPerfWebPublishNeverBlocks(t *testing.T) {
	perfbudget.Require(t)
	r := newWebRig(t, nil)
	n, log := 0, ""
	webTestTopics(r, &n, &log)
	nc, err := net.Dial("unix", r.path)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	if _, err := nc.Write([]byte(webTestOpen + "\n" + `{"t":"hello","v":2,"token":"x"}` + "\n" + `{"t":"sub","topics":["status"]}` + "\n")); err != nil {
		t.Fatal(err)
	}
	r.pumpUntil("the subscription", func() bool {
		r.hub.mu.Lock()
		hc := r.hub.byID["c-1"]
		r.hub.mu.Unlock()
		return hc != nil && hc.subs["status"] != nil
	})
	pad := strings.Repeat("p", 1024)
	var worst time.Duration
	for i := 1; i <= 5000; i++ {
		n = i
		t0 := time.Now()
		r.m.webPublish("status", map[string]any{"n": i, "pad": pad})
		worst = max(worst, time.Since(t0))
	}
	perfbudget.Check(t, "web_publish_worst", float64(worst.Microseconds())/1000)
}

// web_chat_append_overhead: bytes a streamed chat token costs on the
// wire beyond its text (the worst of 50 tokens), measured in a long chat:
// the answer is message 1 000 and the connection already took 100 000
// deltas, so the seq and the message index have the digits a long
// session really has (a fresh chat would hide up to 7 bytes of them).
// Design: <= 80 B for a fresh chat; the budget adds those digits.
func TestPerfWebChatAppendBytes(t *testing.T) {
	perfbudget.Require(t)
	const longChat = 1000
	msgs := make([]ai.StoredMessage, 0, longChat)
	for len(msgs) < longChat-1 {
		msgs = append(msgs, ai.StoredMessage{Role: ai.StoredUser, Text: "q"}, ai.StoredMessage{Role: ai.StoredAssistant, Text: "a"})
	}
	msgs = append(msgs[:longChat-1], ai.StoredMessage{Role: ai.StoredAssistant})
	m := webTestChatModel(t, msgs...)
	m.ai.streaming = true
	webTestHub(t, &m)
	c := webTestSub(t, &m, "chat")
	webRawFrames(c)
	c.subs["chat"].seq = 100_000 // as after 100 000 earlier deltas
	last := len(m.ai.conv.Messages) - 1
	worst := 0
	for i := 0; i < 50; i++ {
		tok := "word" + strconv.Itoa(i) + " "
		m.ai.conv.Messages[last].Text += tok
		m.webAfterUpdate(nil)
		size := 0
		for _, f := range webRawFrames(c) {
			size += len(f) + 1 // the newline on the hub link
		}
		worst = max(worst, size-len(tok))
	}
	perfbudget.Check(t, "web_chat_append_overhead", float64(worst))
}

// web_term_key_bytes: bytes on the wire per key typed into a real shell
// from the browser (the row's lines delta), median of 24 keys.
func TestPerfWebTermKeyBytes(t *testing.T) {
	perfbudget.Require(t)
	r := newWebFilesRig(t)
	r.withNvim()
	buf := webTestShell(t, r, "sh")
	topic := "term:" + strconv.Itoa(buf)
	r.sub(topic)
	r.pumpUntil("the prompt", func() bool { return strings.Contains(webTermText(r, buf), "$") })
	r.takeOn(r.c)
	var sizes []int
	typed := ""
	for _, ch := range "echo hello web perf 0123" {
		typed += string(ch)
		if f := r.call(webproto.OpTermSend, &webproto.TermSendArgs{Buf: buf, Text: string(ch)}); f["t"] != "res" {
			t.Fatal(f)
		}
		r.pumpUntil("the echo of "+typed, func() bool { return strings.Contains(webTermText(r, buf), typed) })
		size := 0
		for _, f := range r.takeOn(r.c) {
			if f["t"] == "delta" && f["topic"] == topic {
				b, _ := json.Marshal(f)
				size += len(b) + 1
			}
		}
		if size > 0 {
			sizes = append(sizes, size)
		}
	}
	if len(sizes) == 0 {
		t.Fatal("no deltas")
	}
	slices.Sort(sizes)
	perfbudget.Check(t, "web_term_key_bytes", float64(sizes[len(sizes)/2]))
}
