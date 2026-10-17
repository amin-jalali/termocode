package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// kinds lists the event kinds in order.
func kinds(evs []Event) []EventKind {
	out := make([]EventKind, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

func sameKinds(a, b []EventKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Spec (B1): every request to the model starts with EventTurnStart, so
// the chat can show "waiting for the model" before the first token.
func TestAgentEmitsTurnStartPerRequest(t *testing.T) {
	tool := Tool{Name: "noop", Run: func(context.Context, map[string]any) (string, error) { return "ok", nil }}
	p := &scripted{tools: true, replies: []Chunk{
		{ToolCalls: []ToolCall{{ID: "1", Name: "noop"}}},
		{Delta: "fine"},
	}}
	a := &Agent{Provider: p, Tools: []Tool{tool}, Native: true}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "go"}})
	want := []EventKind{EventTurnStart, EventToolStart, EventToolResult, EventTurnStart, EventText, EventDone}
	if got := kinds(evs); !sameKinds(got, want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
}

// flaky fails with an HTTP status before any token for its first fails
// calls, then answers.
type flaky struct {
	fails int
	code  int
	calls int
}

func (f *flaky) Name() string        { return "flaky" }
func (f *flaky) Configured() bool    { return true }
func (f *flaky) SupportsTools() bool { return true }
func (f *flaky) Stream(context.Context, Request) <-chan Chunk {
	ch := make(chan Chunk, 2)
	f.calls++
	if f.calls <= f.fails {
		ch <- Chunk{Done: true, Err: httpError(f.code, []byte(`{"error":{"message":"busy"}}`))}
	} else {
		ch <- Chunk{Delta: "hello"}
		ch <- Chunk{Done: true}
	}
	close(ch)
	return ch
}

func noWait(context.Context, int) error { return nil }

// Spec (B1): a busy server (429 / 5xx) before the first token is tried
// again, up to 3 times, and each retry is reported as EventRetry with
// "attempt of max".
func TestAgentRetriesBusyServer(t *testing.T) {
	p := &flaky{fails: 2, code: http.StatusServiceUnavailable}
	a := &Agent{Provider: p, Native: true, RetryWait: noWait}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "hi"}})
	want := []EventKind{EventTurnStart, EventRetry, EventTurnStart, EventRetry, EventTurnStart, EventText, EventDone}
	if got := kinds(evs); !sameKinds(got, want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
	if evs[1].Attempt != 1 || evs[3].Attempt != 2 || evs[3].Attempts != DefaultRetries {
		t.Errorf("retry events %+v %+v", evs[1], evs[3])
	}
	if p.calls != 3 {
		t.Errorf("calls %d", p.calls)
	}
}

// Spec (B1): after the last retry the error is shown; a client error
// (400) is never tried again.
func TestAgentRetryGivesUp(t *testing.T) {
	p := &flaky{fails: 99, code: http.StatusTooManyRequests}
	a := &Agent{Provider: p, Native: true, RetryWait: noWait}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "hi"}})
	if p.calls != DefaultRetries+1 || evs[len(evs)-1].Kind != EventError {
		t.Fatalf("calls %d, events %v", p.calls, kinds(evs))
	}

	p = &flaky{fails: 99, code: http.StatusBadRequest}
	a = &Agent{Provider: p, Native: true, RetryWait: noWait}
	evs, _ = collectEvents(a, []Message{{Role: RoleUser, Content: "hi"}})
	if p.calls != 1 || !sameKinds(kinds(evs), []EventKind{EventTurnStart, EventError}) {
		t.Fatalf("400: calls %d, events %v", p.calls, kinds(evs))
	}
}

// Spec (B1): a stopped turn does not wait out the retry pause.
func TestAgentRetryStopsWithContext(t *testing.T) {
	p := &flaky{fails: 99, code: http.StatusBadGateway}
	ctx, cancel := context.WithCancel(context.Background())
	a := &Agent{Provider: p, Native: true, RetryWait: func(context.Context, int) error {
		cancel()
		return context.Canceled
	}}
	var evs []Event
	a.Run(ctx, []Message{{Role: RoleUser, Content: "hi"}}, func(e Event) { evs = append(evs, e) })
	if p.calls != 1 || evs[len(evs)-1].Kind != EventError {
		t.Fatalf("calls %d, events %v", p.calls, kinds(evs))
	}
}

// Spec (B1): the real providers' HTTP status errors are retryable.
func TestRetryableHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"overloaded"}}`))
	}))
	defer srv.Close()
	p := NewProvider(Config{Kind: KindOpenAI, APIKey: "k", BaseURL: srv.URL, Model: "m"})
	var last Chunk
	for c := range p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "x"}}}) {
		if c.Done {
			last = c
		}
	}
	if last.Err == nil || !Retryable(last.Err) {
		t.Fatalf("err %v not retryable", last.Err)
	}
	if Retryable(httpError(401, nil)) || Retryable(context.Canceled) {
		t.Error("401 / cancel must not be retryable")
	}
}
