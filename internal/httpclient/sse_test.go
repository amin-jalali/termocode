package httpclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseSSE(t *testing.T) {
	in := ": a comment line\n" +
		"data: one\n\n" +
		"event: update\r\nid: 7\r\ndata: {\"n\":1}\r\ndata:second line\r\n\r\n" +
		"retry: 3000\ndata\n\n" +
		"event: empty\n\n" + // no data: not dispatched
		"id: 8\rdata:  two spaces\r\r" +
		"data: cut off at the end" // no blank line: not dispatched
	got := ParseSSE([]byte(in))
	want := []SSEEvent{
		{Event: "message", Data: "one"},
		{Event: "update", ID: "7", Data: "{\"n\":1}\nsecond line"},
		{Event: "message", ID: "7", Data: "", Retry: 3000},
		{Event: "message", ID: "8", Data: " two spaces"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %#v\nwant %#v", got, want)
	}
	if ParseSSE(nil) != nil || len(ParseSSE([]byte("\n\n:only comments\n\n"))) != 0 {
		t.Fatal("no events in an empty stream")
	}
}

func TestSendSSEStreamsLiveAndOutlivesTheTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(200)
		for i := 1; i <= 3; i++ {
			fmt.Fprintf(w, "event: tick\nid: %d\ndata: n=%d\n\n", i, i)
			w.(http.Flusher).Flush()
			time.Sleep(150 * time.Millisecond) // the whole stream takes longer than the timeout
		}
	}))
	defer srv.Close()
	var mu sync.Mutex
	var live []string
	hooks := Hooks{Live: func(body []byte) {
		mu.Lock()
		live = append(live, string(body))
		mu.Unlock()
	}}
	r := Request{Method: "GET", URL: srv.URL, Directives: map[string]string{"timeout": "200ms"}}
	resp := New(Options{}).Send(context.Background(), Resolved{Request: r}, hooks)
	if resp.Err != "" {
		t.Fatalf("a stream that keeps sending is not a timeout: %q", resp.Err)
	}
	if !resp.Streamed || resp.Kind() != "sse" || len(resp.Events) != 3 || resp.Events[2] != (SSEEvent{Event: "tick", ID: "3", Data: "n=3"}) {
		t.Fatalf("streamed %v kind %q events %#v", resp.Streamed, resp.Kind(), resp.Events)
	}
	if !strings.Contains(resp.Summary(), " · sse · 3 events") {
		t.Fatalf("summary %q", resp.Summary())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(live) < 2 || !strings.HasPrefix(live[0], "event: tick\nid: 1\n") || strings.Contains(live[0], "n=3") {
		t.Fatalf("live hook must see the stream as it arrives, got %q", live)
	}
}

func TestSendSSEWaitsForHeadersOnlyUntilTheTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	r := Request{Method: "GET", URL: srv.URL, Directives: map[string]string{"timeout": "100ms"}}
	resp := New(Options{}).Send(context.Background(), Resolved{Request: r}, Hooks{})
	if resp.ErrKind != ErrKindTimeout || resp.Err != "timed out after 100 ms" {
		t.Fatalf("kind %v err %q", resp.ErrKind, resp.Err)
	}
}

func TestSendSSECancelKeepsTheEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	hooks := Hooks{Live: func(body []byte) {
		if strings.Contains(string(body), "\n\n") {
			once.Do(cancel)
		}
	}}
	resp := New(Options{}).Send(ctx, Resolved{Request: Request{Method: "GET", URL: srv.URL}}, hooks)
	if resp.ErrKind != ErrKindCancelled {
		t.Fatalf("kind %v err %q", resp.ErrKind, resp.Err)
	}
	if len(resp.Events) != 1 || resp.Events[0].Data != "first" {
		t.Fatalf("events kept after Esc: %#v", resp.Events)
	}
}

func TestSendChunkedIsLiveButKeepsTheTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "part 1\n")
		w.(http.Flusher).Flush()
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	var got string
	hooks := Hooks{Live: func(body []byte) { got = string(body) }}
	r := Request{Method: "GET", URL: srv.URL, Directives: map[string]string{"timeout": "200ms"}}
	resp := New(Options{}).Send(context.Background(), Resolved{Request: r}, hooks)
	if resp.ErrKind != ErrKindTimeout || !resp.Streamed || got != "part 1\n" || string(resp.Body) != "part 1\n" {
		t.Fatalf("kind %v streamed %v live %q body %q", resp.ErrKind, resp.Streamed, got, resp.Body)
	}
	// A plain answer with a length is not streamed and never calls Live.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2")
		fmt.Fprint(w, "ok")
	}))
	defer plain.Close()
	called := false
	resp = New(Options{}).Send(context.Background(), Resolved{Request: Request{Method: "GET", URL: plain.URL}}, Hooks{Live: func([]byte) { called = true }})
	if resp.Streamed || called || resp.Events != nil {
		t.Fatalf("plain answer: streamed %v live %v events %v", resp.Streamed, called, resp.Events)
	}
}
