package httpclient

import (
	"context"
	"testing"
	"time"
)

func TestWSExchangeSendsAndCollects(t *testing.T) {
	srv := echoWS(t)
	c := New(Options{})
	rr := Resolved{Request: Request{Method: "WEBSOCKET", URL: wsURL(srv), Headers: []Header{{Name: "X-Token", Value: "k"}}}}
	start := time.Now()
	log, err := c.WSExchange(context.Background(), rr, Hooks{}, []string{"a", "b"}, 300*time.Millisecond, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 4 || log[0].Dir != WSOut || log[0].Text != "a" {
		t.Fatalf("log = %+v", log)
	}
	in := 0
	for _, m := range log {
		if m.Dir == WSIn {
			in++
		}
	}
	if in != 2 {
		t.Fatalf("received %d, log = %+v", in, log)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("the exchange did not stop after the wait")
	}
	// maxMsgs stops early.
	log, err = c.WSExchange(context.Background(), rr, Hooks{}, []string{"a", "b", "c"}, 2*time.Second, 1)
	if err != nil || countDir(log, WSIn) != 1 {
		t.Fatalf("max: log = %+v err = %v", log, err)
	}
}

func countDir(log []WSMessage, d WSDir) int {
	n := 0
	for _, m := range log {
		if m.Dir == d {
			n++
		}
	}
	return n
}
