package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// OMX-4: a busy server after the retries moves the turn to the next
// model of the chain; the chat is told.
func TestAgentFallbackAfterRetries(t *testing.T) {
	busy := statusError{code: 529, msg: "HTTP 529: overloaded"}
	a1 := &recorder{name: "anthropic", replies: []Chunk{{Err: busy}, {Err: busy}, {Err: busy}, {Err: busy}}}
	b := &recorder{name: "openai", replies: []Chunk{{Delta: "answer from b"}}}
	a := &Agent{Provider: a1, Model: "m1", Native: true, RetryWait: noWait,
		Fallbacks: []Fallback{{Name: "backup", Provider: b, Model: "m2"}}}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "q"}})
	if n := len(a1.requests()); n != DefaultRetries+1 {
		t.Errorf("first model tried %d times, want %d", n, DefaultRetries+1)
	}
	rb := b.requests()
	if len(rb) != 1 || rb[0].Model != "m2" {
		t.Fatalf("backup requests: %+v", rb)
	}
	var fb *Event
	for i := range evs {
		if evs[i].Kind == EventFallback {
			fb = &evs[i]
		}
		if evs[i].Kind == EventError {
			t.Errorf("turn failed: %s", evs[i].Detail)
		}
	}
	if fb == nil || !strings.Contains(fb.Detail, "backup") || !strings.Contains(fb.Detail, "529") || fb.Label != "m2" {
		t.Fatalf("no fallback event: %+v", fb)
	}
}

// A sign-in that is gone moves on at once (retrying will not help); a
// bad request (400) does not move: the next model would fail the same.
func TestAgentFallbackReasons(t *testing.T) {
	for _, c := range []struct {
		err  error
		move bool
	}{
		{statusError{code: 401, msg: "HTTP 401: invalid x-api-key"}, true},
		{statusError{code: 403, msg: "HTTP 403: forbidden"}, true},
		{statusError{code: 402, msg: "HTTP 402: insufficient credits"}, true},
		{ErrNotConfigured, true},
		{signInError{errors.New("refresh failed")}, true},
		{statusError{code: 400, msg: "HTTP 400: bad"}, false},
		{context.Canceled, false},
		{errors.New("some other error"), false},
	} {
		p := &recorder{name: "p", replies: []Chunk{{Err: c.err}}}
		b := &recorder{name: "b"}
		a := &Agent{Provider: p, Native: true, RetryWait: noWait, Fallbacks: []Fallback{{Name: "b", Provider: b}}}
		collectEvents(a, []Message{{Role: RoleUser, Content: "q"}})
		if moved := len(b.requests()) > 0; moved != c.move {
			t.Errorf("%v: moved %v, want %v", c.err, moved, c.move)
		}
		if c.move && len(p.requests()) != 1 {
			t.Errorf("%v: retried %d times before moving", c.err, len(p.requests()))
		}
	}
}

// Every model of the chain failing ends the turn with the last error;
// a model that already wrote text is never replaced mid-answer.
func TestAgentFallbackChainEnds(t *testing.T) {
	busy := func(n int) []Chunk {
		out := make([]Chunk, n)
		for i := range out {
			out[i] = Chunk{Err: statusError{code: 503, msg: fmt.Sprintf("HTTP 503: down %d", i)}}
		}
		return out
	}
	p1 := &recorder{name: "a", replies: busy(4)}
	p2 := &recorder{name: "b", replies: busy(4)}
	a := &Agent{Provider: p1, Native: true, RetryWait: noWait, Fallbacks: []Fallback{{Name: "b", Provider: p2}}}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "q"}})
	if evs[len(evs)-1].Kind != EventError {
		t.Fatalf("last event %+v", evs[len(evs)-1])
	}
	half := &recorder{name: "h", replies: []Chunk{{Delta: "partial", Err: statusError{code: 500, msg: "HTTP 500"}}}}
	b := &recorder{name: "b"}
	a = &Agent{Provider: half, Native: true, RetryWait: noWait, Fallbacks: []Fallback{{Name: "b", Provider: b}}}
	collectEvents(a, []Message{{Role: RoleUser, Content: "q"}})
	if len(b.requests()) != 0 {
		t.Error("a half answer was sent again to the next model")
	}
}

// The next step of the turn stays on the model that answered.
func TestAgentFallbackStaysForTheTurn(t *testing.T) {
	p1 := &recorder{name: "a", replies: []Chunk{{Err: statusError{code: 401, msg: "HTTP 401"}}}}
	call := ToolCall{ID: "1", Name: "nope"}
	p2 := &recorder{name: "b", replies: []Chunk{{ToolCalls: []ToolCall{call}}, {Delta: "done"}}}
	a := &Agent{Provider: p1, Native: true, RetryWait: noWait, Fallbacks: []Fallback{{Name: "b", Provider: p2}}}
	collectEvents(a, []Message{{Role: RoleUser, Content: "q"}})
	if len(p1.requests()) != 1 || len(p2.requests()) != 2 {
		t.Errorf("requests: a %d b %d", len(p1.requests()), len(p2.requests()))
	}
}

func TestFallbackNames(t *testing.T) {
	s := Settings{Fallback: []string{"gpt", "openai/gpt-5"}, Models: map[string]ModelSlot{
		"gpt":  {Provider: KindOpenAI, Fallback: []string{"local"}},
		"self": {Provider: KindOpenAI, Fallback: []string{"self"}},
		"bad":  {Provider: KindOpenAI, Fallback: []string{"no spaces!"}},
	}}
	if got := s.FallbackNames(SlotMain); strings.Join(got, ",") != "gpt,openai/gpt-5" {
		t.Errorf("main: %v", got)
	}
	if got := s.FallbackNames(""); len(got) != 2 {
		t.Errorf("empty name is main: %v", got)
	}
	if got := s.FallbackNames("gpt"); strings.Join(got, ",") != "local" {
		t.Errorf("gpt: %v", got)
	}
	_, errs := ResolveSlots(s, Config{Kind: KindAnthropic, APIKey: "k"}, Credentials{}, func(string) string { return "" })
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, `"self"`) || !strings.Contains(joined, `"bad"`) || strings.Contains(joined, `"gpt"`) {
		t.Errorf("errors: %v", errs)
	}
	if cfgs, _ := ResolveSlots(s, Config{Kind: KindAnthropic, APIKey: "k"}, Credentials{}, func(string) string { return "" }); cfgs["self"].Kind == "" {
		t.Error("a bad fallback list drops only the list, not the slot")
	}
	if s.FallbackNames("self") != nil {
		t.Error("a bad list gives no fallbacks")
	}
	if err := CheckFallbacks(SlotMain, []string{"main"}); err == nil {
		t.Error("main may not fall back to itself")
	}
}
