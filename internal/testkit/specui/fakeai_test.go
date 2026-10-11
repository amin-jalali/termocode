package specui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAI is a local OpenAI-compatible chat server (POST
// /chat/completions, SSE). Each request gets the next reply; a reply is
// a list of text deltas, and a delta "<wait>" blocks until release is
// closed, so a test can look at a half-streamed answer.
type fakeAI struct {
	srv     *httptest.Server
	mu      sync.Mutex
	replies [][]string
	status  int // non-200: answer every request with this status
	bodies  []string
	release chan struct{}
}

func newFakeAI(t *testing.T) *fakeAI {
	t.Helper()
	f := &fakeAI{release: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() {
		f.mu.Lock()
		select {
		case <-f.release:
		default:
			close(f.release)
		}
		f.mu.Unlock()
		f.srv.Close()
	})
	return f
}

// reply queues the deltas of the next answer.
func (f *fakeAI) reply(deltas ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, deltas)
}

// requests returns the request bodies seen so far.
func (f *fakeAI) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.bodies...)
}

func (f *fakeAI) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.bodies = append(f.bodies, string(body))
	status := f.status
	var deltas []string
	if len(f.replies) > 0 {
		deltas, f.replies = f.replies[0], f.replies[1:]
	}
	f.mu.Unlock()
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.NotFound(w, r)
		return
	}
	if status != 0 {
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"error":{"message":"fake error %d"}}`, status)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	for _, d := range deltas {
		if d == "<wait>" {
			<-f.release
			continue
		}
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": d}}}})
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// config is a config.json that points the app at this server.
func (f *fakeAI) config() string {
	return fmt.Sprintf(`{"animations": "off", "images": "blocks", "ai_provider": "openai-compatible",
		"ai_base_url": %q, "ai_model": "fake-model", "ai_inline": false}`, f.srv.URL)
}
