package testkit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Spec (SCN-12): Given a fake AI that streams a 1 MB answer in 4 KB
// chunks (paragraphs, a list and code blocks),
// when the user asks a question,
// then the whole answer arrives within its bound, keys and the wheel in
// the chat stay responsive while it streams and after, and the Go heap
// stays bounded.
//
// Local (race off): 1 MB streamed and shown 1.8 s, worst wheel 2 ms,
// heap +44 MB (the drawn transcript is kept for scrolling). The heap
// bound is below the 101 MB the transcript took while every row held a
// whole lipgloss.Style.
func TestScaleAIHugeStreamedAnswer(t *testing.T) {
	skipShortScale(t)
	var para strings.Builder
	for i := 0; para.Len() < 1<<20; i++ {
		switch i % 4 {
		case 0:
			fmt.Fprintf(&para, "Paragraph %d: the quick brown fox jumps over the lazy dog and keeps running far away.\n\n", i)
		case 1:
			fmt.Fprintf(&para, "- item %d with `code` and **bold** text\n", i)
		case 2:
			fmt.Fprintf(&para, "\n```go\nfunc f%d() int { return %d }\n```\n\n", i, i)
		default:
			fmt.Fprintf(&para, "### Heading %d\n\n", i)
		}
	}
	answer := para.String() + "\nEND-OF-ANSWER\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for i := 0; i < len(answer); i += 4096 {
			part := answer[i:min(i+4096, len(answer))]
			q := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(part, `\`, `\\`), "\n", `\n`), `"`, `\"`)
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"%s\"}}]}\n\n", q)
			if fl != nil {
				fl.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	d := New(t, Options{W: 160, H: 40, Files: textFiles(),
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
		}})
	d.Key("alt+a")
	d.Type("write a lot")
	base := heapMB()
	t0 := time.Now()
	d.Key("enter")
	var key time.Duration
	done := func() bool {
		d.render()
		s := screen(d)
		return strings.Contains(s, "Copy") && !strings.Contains(s, "generating")
	}
	for !done() && time.Since(t0) < 60*time.Second*raceFactor() {
		key = max(key, d.Timed(wheel(d.W-20, d.H/2, true)))
		d.Settle(50 * time.Millisecond)
	}
	if !done() {
		t.Fatalf("the answer did not finish:\n%s", screen(d))
	}
	within(t, "stream and show 1 MB", time.Since(t0), 6*time.Second)
	for i := 0; i < 10; i++ {
		key = max(key, d.Timed(wheel(d.W-20, d.H/2, i%2 == 0)))
	}
	d.Settle(100 * time.Millisecond)
	within(t, "worst wheel while streaming and after", key, 100*time.Millisecond)
	heapWithin(t, "1 MB answer", base, 80)
}
