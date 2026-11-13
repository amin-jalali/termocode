//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// streamModel is a scripted OpenAI-compatible model that streams chunks
// words of text, one every gap, and ends with end.
type streamModel struct{ srv *httptest.Server }

func newStreamModel(t *testing.T, chunks int, gap time.Duration, end string) *streamModel {
	t.Helper()
	sm := &streamModel{}
	sm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		send := func(s string) {
			out, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": s}}}})
			fmt.Fprintf(w, "data: %s\n\n", out)
			if fl != nil {
				fl.Flush()
			}
		}
		for i := 0; i < chunks; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(gap):
			}
			send(fmt.Sprintf("word%03d ", i))
		}
		send(end)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(sm.srv.Close)
	return sm
}

// Spec (SCN-36): over a slow link (150 ms each way, 1 Mbit) an AI answer
// that streams, a large diff and a log that keeps printing in a terminal all
// reach the end the user is waiting for (the last words, the last changed
// line, the last log line) and then the screen is still: no run-on, and no
// backlog that grows while the link is busy. The DB results view uses the
// same redraw path as the log view and has no case of its own here.
func TestSCN36SlowLinkLongLivedViews(t *testing.T) {
	const latency = 150 * time.Millisecond
	const slack = 1500 * time.Millisecond

	boot := func(t *testing.T, r *sessionRig) *Term {
		tm := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Link: linkFor()})
		tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
			return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
		})
		tm.settle()
		return tm
	}

	t.Run("AI answer streaming", func(t *testing.T) {
		const chunks, gap, end = 300, 15 * time.Millisecond, "stream-end-36"
		model := newStreamModel(t, chunks, gap, end)
		r := newSessionRig(t)
		r.env = append(r.env, "TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake", "TERMOCODE_AI_BASE_URL="+model.srv.URL+"/v1")
		tm := boot(t, r)
		tm.Send("\x1ba")
		tm.WaitText("AI CHAT", uiTimeout)
		tm.Type("tell me a long story")
		tm.Send(KeyEnter)
		started := time.Now()
		tm.WaitText(end, 30*time.Second)
		late := time.Since(started) - time.Duration(chunks)*gap
		t.Logf("end of stream on screen %v after it was sent", late.Round(time.Millisecond))
		if late > 2*latency+slack {
			t.Errorf("the end of the answer came %v after the model sent it", late)
		}
		off := tm.rawLen()
		seen := time.Now()
		last := tm.lastChange(3 * time.Second)
		t.Logf("after the end: %s", tm.statsSince(off))
		if !last.IsZero() && last.Sub(seen) > 2*latency+slack {
			t.Errorf("screen kept changing %v after the answer ended", last.Sub(seen))
		}
		tm.Quit()
	})

	t.Run("large diff", func(t *testing.T) {
		r := newSessionRig(t)
		var a, b strings.Builder
		for i := 1; i <= 3000; i++ {
			fmt.Fprintf(&a, "line %04d: the quick brown fox jumps over the lazy dog\n", i)
			if i%10 == 0 {
				fmt.Fprintf(&b, "line %04d: CHANGED-36 the quick brown fox jumps over the lazy dog\n", i)
			} else {
				fmt.Fprintf(&b, "line %04d: the quick brown fox jumps over the lazy dog\n", i)
			}
		}
		p := filepath.Join(r.ws, "big.txt")
		writeFile(t, p, a.String())
		gitOut(t, r.ws, "add", "big.txt")
		gitOut(t, r.ws, "-c", "user.name=e2e", "-c", "user.email=e2e@example.com", "commit", "-q", "-m", "add big")
		writeFile(t, p, b.String())
		tm := boot(t, r)
		tm.OpenFile("big.txt")
		tm.WaitText("line 0001:", uiTimeout)
		tm.Send(KeyEsc)
		tm.settle()
		off := tm.rawLen()
		tm.Palette("Git: Show Diff for File")
		tm.WaitText("CHANGED-36", 30*time.Second)
		tm.settle()
		t.Logf("diff opened: %s", tm.statsSince(off))
		if last := tm.lastChange(2 * time.Second); !last.IsZero() {
			t.Errorf("diff view still changing after it settled")
		}
	})

	t.Run("log keeps printing", func(t *testing.T) {
		r := newSessionRig(t)
		writeFile(t, filepath.Join(r.ws, ".termocode", "tasks.json"), `{"version": "2.0.0", "tasks": [{"label": "logs", "command": "sh",
  "args": ["-c", "i=0; while [ $i -lt 300 ]; do echo log-line-$i; i=$((i+1)); sleep 0.01; done; echo logs-done-36"]}]}`)
		tm := boot(t, r)
		tm.Palette("Tasks: Run Task...")
		tm.Type("logs")
		tm.settle()
		tm.Send(KeyEnter)
		start := time.Now()
		tm.WaitText("logs-done-36", 40*time.Second)
		t.Logf("last log line on screen %v after the task started", time.Since(start).Round(time.Millisecond))
		seen := time.Now()
		last := tm.lastChange(3 * time.Second)
		if !last.IsZero() && last.Sub(seen) > 2*latency+slack {
			t.Errorf("screen kept changing %v after the last log line", last.Sub(seen))
		}
		tm.Quit()
	})
}

func linkFor() *SlowLink {
	if os.Getenv("SCN36_NOLINK") != "" {
		return nil
	}
	return &SlowLink{Latency: 150 * time.Millisecond, BytesPerSec: 125_000}
}
