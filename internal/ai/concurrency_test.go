package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls cond (no fixed sleep: slow CI machines).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// OMX-26: a stream waits for a free slot of its provider kind before it
// sends anything; it goes when the slot is released.
func TestConcurrencyStreamWaitsForItsSlot(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	defer SetConcurrencyLimits(nil)
	SetConcurrencyLimits(map[string]int{KindCompatible: 1})
	held, err := acquireLimit(context.Background(), KindCompatible, "other")
	if err != nil {
		t.Fatal(err)
	}
	p := NewProvider(Config{Kind: KindCompatible, BaseURL: srv.URL, Model: "m"})
	done := make(chan Chunk, 1)
	go func() {
		_, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "q"}}}))
		done <- last
	}()
	waitFor(t, "the stream to wait", func() bool { return limitWaiting() == 1 })
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("sent %d requests over the limit", n)
	}
	held()
	if last := <-done; last.Err != nil || atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("after release: %v, %d requests", last.Err, hits)
	}
}

// The per-model limit counts only that model; the kind limit counts all.
func TestConcurrencyLimitKeys(t *testing.T) {
	defer SetConcurrencyLimits(nil)
	SetConcurrencyLimits(map[string]int{KindOpenAI + "/gpt-5": 1, KindAnthropic: 2})
	ctx := context.Background()
	a, err := acquireLimit(ctx, KindOpenAI, "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := acquireLimit(ctx, KindOpenAI, "gpt-4.1"); err != nil { // another model: free
		t.Fatal(err)
	} else {
		b()
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := acquireLimit(short, KindOpenAI, "gpt-5"); err == nil {
		t.Error("a second gpt-5 request got a slot")
	}
	a()
	r1, _ := acquireLimit(ctx, KindAnthropic, "x")
	r2, _ := acquireLimit(ctx, KindAnthropic, "y")
	short2, cancel2 := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel2()
	if _, err := acquireLimit(short2, KindAnthropic, "z"); err == nil {
		t.Error("a third anthropic request got a slot")
	}
	r1()
	r2()
	none, err := acquireLimit(ctx, KindOllama, "q") // no limit: never waits
	if err != nil {
		t.Fatal(err)
	}
	none()
}

// A request waiting for its turn stops with its context.
func TestConcurrencyWaitCancels(t *testing.T) {
	defer SetConcurrencyLimits(nil)
	SetConcurrencyLimits(map[string]int{KindAnthropic: 1})
	rel, err := acquireLimit(context.Background(), KindAnthropic, "m")
	if err != nil {
		t.Fatal(err)
	}
	defer rel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireLimit(ctx, KindAnthropic, "m"); err == nil {
		t.Error("a cancelled wait got a slot")
	}
}

func TestConcurrencySettingChecked(t *testing.T) {
	s := Settings{Concurrency: map[string]int{"anthropic": 2, "openai/gpt-5": 1, "bogus": 3, "ollama": 0}}
	lim, problems := s.ConcurrencyLimits()
	if lim["anthropic"] != 2 || lim["openai/gpt-5"] != 1 || len(lim) != 2 || len(problems) != 2 {
		t.Errorf("limits %v problems %v", lim, problems)
	}
}
