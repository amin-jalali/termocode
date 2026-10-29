package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// recordingAIProvider replays one reply per request (like
// scriptedAIProvider) and records every whole request.
type recordingAIProvider struct {
	mu      sync.Mutex
	replies []ai.Chunk
	reqs    []ai.Request
}

func (*recordingAIProvider) Name() string        { return "fake" }
func (*recordingAIProvider) Configured() bool    { return true }
func (*recordingAIProvider) SupportsTools() bool { return true }
func (p *recordingAIProvider) Stream(_ context.Context, req ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	r := ai.Chunk{Delta: "done"}
	if n := len(p.reqs); n < len(p.replies) {
		r = p.replies[n]
	}
	req.Messages = append([]ai.Message(nil), req.Messages...)
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan ai.Chunk, 2)
	if r.Delta != "" {
		ch <- ai.Chunk{Delta: r.Delta}
	}
	ch <- ai.Chunk{Done: true, ToolCalls: r.ToolCalls, Usage: r.Usage, Err: r.Err}
	close(ch)
	return ch
}

// aiDefaultTurnDump runs one chat turn on the default profile (an
// attachment, a tool call, an answer) and returns what the user sees
// and what the model was sent, with the temp root masked.
func aiDefaultTurnDump(t *testing.T) string {
	t.Helper()
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 18, 9, 5, 0, 0, time.UTC) }))
	// No container engine on PATH: the system prompt's Containers line
	// (CP-9) depends on the machine, the golden must not.
	t.Setenv("PATH", t.TempDir())
	p := &recordingAIProvider{replies: []ai.Chunk{
		{Delta: "Let me look.", ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_dir", Args: map[string]any{"path": "."}}},
			Usage: &ai.Usage{Model: "fake-1", Input: 1200, Output: 30, Requests: 1, PeakContext: 1200}},
		{Delta: "There is **one** file:\n\n- `a.txt`\n\n```go\nfunc main() {}\n```",
			Usage: &ai.Usage{Model: "fake-1", Input: 1300, Output: 40, Requests: 1, PeakContext: 1300}},
	}}
	m, dir := aiIDETestModel(t, p)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ai.attachments = []aiAttachment{{Label: "a.txt", Text: "hello"}}
	driveAIChat(t, m, m.sendAIChat("what files are here?"), nil)

	var b strings.Builder
	b.WriteString("== transcript (60 columns)\n")
	for _, l := range m.buildAITranscript(60) {
		b.WriteString(strings.TrimRight(ansi.Strip(renderAILine(l, 60)), " ") + "\n")
	}
	b.WriteString("== stored messages\n")
	js, _ := json.MarshalIndent(m.ai.conv.Messages, "", "  ")
	b.Write(js)
	b.WriteString("\n== requests\n")
	p.mu.Lock()
	for i, r := range p.reqs {
		var names []string
		for _, tl := range r.Tools {
			names = append(names, tl.Name)
		}
		b.WriteString("-- request " + strconv.Itoa(i+1) + " tools: " + strings.Join(names, ",") + "\n")
		for _, msg := range r.Messages {
			c := msg.Content
			js, _ := json.Marshal(struct {
				Role    string
				Content string
				Tool    string `json:",omitempty"`
				Calls   int    `json:",omitempty"`
			}{string(msg.Role), c, msg.ToolCallID, len(msg.ToolCalls)})
			b.Write(js)
			b.WriteString("\n")
		}
	}
	p.mu.Unlock()
	return strings.ReplaceAll(b.String(), dir, "<root>")
}

// Spec (U20 AP-4): the default profile (one step) is today's chat: the
// transcript, the stored chat and what the model is sent are byte for
// byte what they were before agent pipelines. The golden file was
// written by the code before AP-4; update it (UPDATE_GOLDEN=1) only for
// an intended change of the plain chat.
func TestAIDefaultProfileTurnUnchanged(t *testing.T) {
	wd, _ := os.Getwd() // the turn runs in a temp workspace
	golden := filepath.Join(wd, "testdata", "ai_default_turn.golden")
	got := aiDefaultTurnDump(t)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("no golden %s: %v", golden, err)
	}
	if string(want) != got {
		t.Errorf("the default profile's turn changed.\n--- want\n%s\n--- got\n%s", want, got)
	}
}
