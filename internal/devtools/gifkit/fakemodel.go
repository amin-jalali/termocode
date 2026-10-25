package gifkit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// Reply is one answer of the demo model: streamed text, then an optional
// tool call.
type Reply struct {
	Text string
	Tool string
	Args map[string]any
	// Pace is the pause between words of the text (0: none).
	Pace time.Duration
}

// FakeModel is a scripted OpenAI-compatible model on a loopback port: the
// "demo models" of the GIFs. Its n-th request gets Replies[n] (the last
// one repeats). Nothing leaves the machine.
type FakeModel struct {
	srv     *httptest.Server
	mu      sync.Mutex
	n       int
	Replies []Reply
}

// NewFakeModel starts the model; Close stops it.
func NewFakeModel(replies ...Reply) *FakeModel {
	fm := &FakeModel{Replies: replies}
	fm.srv = httptest.NewServer(http.HandlerFunc(fm.serve))
	return fm
}

// BaseURL is the value for TERMOCODE_AI_BASE_URL.
func (fm *FakeModel) BaseURL() string { return fm.srv.URL + "/v1" }

// Close stops the server.
func (fm *FakeModel) Close() { fm.srv.Close() }

func (fm *FakeModel) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.NotFound(w, r)
		return
	}
	fm.mu.Lock()
	i := min(fm.n, len(fm.Replies)-1)
	fm.n++
	fm.mu.Unlock()
	rep := fm.Replies[i]
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	words := strings.SplitAfter(rep.Text, " ")
	for _, wd := range words {
		if wd == "" {
			continue
		}
		send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": wd}}}})
		if rep.Pace > 0 {
			select {
			case <-time.After(rep.Pace):
			case <-r.Context().Done():
				return
			}
		}
	}
	if rep.Tool != "" {
		args, _ := json.Marshal(rep.Args)
		send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
			map[string]any{"index": 0, "id": fmt.Sprintf("call_%d", i), "function": map[string]any{
				"name": rep.Tool, "arguments": string(args)}}}}}}})
	}
	send(map[string]any{"choices": []any{}, "model": "demo",
		"usage": map[string]any{"prompt_tokens": 900, "completion_tokens": 200}})
	fmt.Fprint(w, "data: [DONE]\n\n")
	if fl != nil {
		fl.Flush()
	}
}
