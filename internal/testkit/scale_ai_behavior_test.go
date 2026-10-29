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
// then the whole answer arrives within its bound, a frame while it
// streams costs about the new text (not the whole answer), the wheel
// stays responsive and the Go heap stays bounded.
//
// Local (race off): 1 MB streamed in 5.3 s (20 ms between 4 KB chunks,
// 200 KB/s: far faster than a model writes), worst wheel 2–11 ms, worst
// frame 45–95 ms (1.7 s when every chunk redrew the whole answer), heap
// +46 MB (101 MB while every row held a whole lipgloss.Style).
func TestScaleAIHugeStreamedAnswer(t *testing.T) {
	skipShortScale(t)
	// Under the race detector drawing is several times slower than the
	// stream: a quarter of the answer keeps the test about the same.
	size := 1 << 20
	if raceOn() {
		size /= 4
	}
	var para strings.Builder
	for i := 0; para.Len() < size; i++ {
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
			time.Sleep(20 * time.Millisecond * raceFactor()) // a model writes over time (200 KB/s here)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	d := New(t, Options{W: 160, H: 40, Files: textFiles(), StepTimeout: 20 * time.Second,
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
		}})
	d.Key("alt+a")
	d.Type("write a lot")
	base := heapMB()
	t0 := time.Now()
	d.Timed(KeyMsg("enter")) // no long settle: frames follow the stream
	// While it streams, each short settle handles the chunks that came
	// and draws one frame: that frame is what a key waits for.
	var key, frame time.Duration
	done := func() bool {
		d.render()
		s := screen(d)
		return strings.Contains(s, "Copy") && !strings.Contains(s, "generating")
	}
	for !done() && time.Since(t0) < 60*time.Second*raceFactor() {
		key = max(key, d.Timed(wheel(d.W-20, d.H/2, true)))
		tf := time.Now()
		d.Settle(20 * time.Millisecond)
		frame = max(frame, time.Since(tf))
	}
	if !done() {
		t.Fatalf("the answer did not finish:\n%s", screen(d))
	}
	within(t, "stream and show 1 MB", time.Since(t0), 10*time.Second)
	for i := 0; i < 10; i++ {
		key = max(key, d.Timed(wheel(d.W-20, d.H/2, i%2 == 0)))
	}
	d.Settle(100 * time.Millisecond)
	within(t, "worst wheel while streaming and after", key, 100*time.Millisecond)
	within(t, "worst frame while streaming (20 ms of chunks + draw)", frame, 200*time.Millisecond)
	heapWithin(t, "1 MB answer", base, 80)
}
