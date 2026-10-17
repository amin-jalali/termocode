package testkit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// Spec (B1): while the model server sends nothing the chat shows
// "waiting for the model" with seconds that count up by themselves (the
// 1 s clock, no other event); after 90 s it warns "no answer … Esc to
// stop"; the first token clears it and the finished answer has no
// activity line.
func TestAIActivityLineWhileServerIsSilent(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select { // the first SSE event waits for the test
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Late hello\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	var skew atomic.Int64 // the fake clock runs ahead of the real one by skew
	t.Cleanup(clock.Set(func() time.Time { return time.Now().Add(time.Duration(skew.Load())) }))

	d := New(t, Options{W: 160, H: 40, Files: textFiles(),
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
		}})
	d.Key("alt+a")
	d.Type("hello?")
	d.Key("enter")
	if !d.WaitForText("waiting for the model", 3*time.Second) {
		t.Fatalf("no waiting line:\n%s", screen(d))
	}
	if !d.WaitForText("model · 1 s", 4*time.Second) {
		t.Fatalf("the seconds do not count:\n%s", screen(d))
	}
	skew.Store(int64(95 * time.Second))
	if !d.WaitFor(func() bool {
		s := screen(d)
		return strings.Contains(s, "no answer for 1 m ") && strings.Contains(s, "Esc to stop")
	}, 4*time.Second) {
		t.Fatalf("no stall warning:\n%s", screen(d))
	}
	close(release)
	if !d.WaitForText("Late hello", 5*time.Second) {
		t.Fatalf("no answer:\n%s", screen(d))
	}
	if !d.WaitFor(func() bool {
		s := screen(d)
		return !strings.Contains(s, "no answer") && !strings.Contains(s, "waiting for") && !strings.Contains(s, "writing")
	}, 3*time.Second) {
		t.Fatalf("activity line left after the answer:\n%s", screen(d))
	}
}
