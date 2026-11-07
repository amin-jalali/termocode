package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// API-16: a stream shows what has arrived while it runs, and an
// event stream's Body view lists its events.
func TestBuildHTTPPageLiveStream(t *testing.T) {
	unicodeIcons(t)
	run := testRun()
	run.resp = nil
	run.live = []byte("event: tick\ndata: n=1\n\n")
	run.progress = 2048
	p := buildHTTPPage(run, httpViewBody, false, true)
	if p.lines[1] != "↝ streaming… · 2.0 KB received" {
		t.Fatalf("row 2 %q", p.lines[1])
	}
	if got := strings.Join(p.lines[3:], "\n"); got != "event: tick\ndata: n=1\n" {
		t.Fatalf("live body %q", got)
	}
	if hs := p.head["hints"].([][]string); len(hs) != 1 || hs[0][1] != "stop" {
		t.Fatalf("a stream stops with esc: %v", hs)
	}
	run.live = nil
	if p = buildHTTPPage(run, httpViewBody, false, true); p.lines[1] != "↝ sending… · 2.0 KB received" || len(p.lines) != 3 {
		t.Fatalf("no live text yet: %q", p.lines)
	}
}

func TestBuildHTTPPageEvents(t *testing.T) {
	unicodeIcons(t)
	run := testRun()
	run.resp = &httpclient.Response{Status: 200, StatusText: "OK", ContentType: "text/event-stream", Streamed: true,
		Body:   []byte("event: tick\nid: 1\ndata: a\ndata: b\n\ndata: c\n\n"),
		Events: []httpclient.SSEEvent{{Event: "tick", ID: "1", Data: "a\nb"}, {Event: "message", ID: "1", Data: "c"}}}
	p := buildHTTPPage(run, httpViewBody, false, false)
	want := []string{"tick · id 1", "  a", "  b", "", "message · id 1", "  c"}
	if got := p.lines[3:]; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events view\n got %q\nwant %q", got, want)
	}
	if len(p.rows) != 2 || p.rows[0]["k"] != "t" || p.rows[0]["n"] != len("tick") || p.rows[1]["l"] != 8 {
		t.Fatalf("event rows %v", p.rows)
	}
	// Raw still shows the wire text.
	if p = buildHTTPPage(run, httpViewRaw, false, false); p.lines[3] != "event: tick" {
		t.Fatalf("raw %q", p.lines[3])
	}
}

func TestHTTPLiveTail(t *testing.T) {
	small := []byte("a\nb\n")
	if got := httpLiveTail(small); !bytes.Equal(got, small) || &got[0] == &small[0] {
		t.Fatal("a short body is copied whole (the client reuses its buffer)")
	}
	big := append(bytes.Repeat([]byte("x"), httpLiveMax), []byte("\nlast line\n")...)
	got := httpLiveTail(big)
	if string(got) != "last line\n" {
		t.Fatalf("tail starts at a line: %q", got)
	}
}

func TestHTTPLiveEventRedrawsThePage(t *testing.T) {
	var m Model
	s := m.httpS()
	run := testRun()
	run.resp = nil
	s.gen, s.running = 3, run
	ch := make(chan httpEvent)
	close(ch)
	cmd, ok := m.handleHTTPMsg(httpEventMsg{gen: 3, ch: ch, run: run, ev: httpEvent{progress: 9, live: []byte("data: x\n")}})
	if !ok || cmd == nil || string(run.live) != "data: x\n" || run.progress != 9 {
		t.Fatalf("ok %v cmd %v live %q progress %d", ok, cmd != nil, run.live, run.progress)
	}
}
