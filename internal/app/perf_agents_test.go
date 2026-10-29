package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/debounce"
	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// PERF-7: termocode's own work around an AI agent turn, with a fake
// model that answers at once (no network, no thinking time).

// perfAIProvider streams n tokens. With calls set, its first reply of a
// turn asks for those tool calls and the next one answers. sent gets
// the time each request left (Stream was called).
type perfAIProvider struct {
	n     int
	calls []ai.ToolCall
	sent  chan time.Time
}

func (perfAIProvider) Name() string        { return "fake" }
func (perfAIProvider) Configured() bool    { return true }
func (perfAIProvider) SupportsTools() bool { return true }
func (p perfAIProvider) Stream(_ context.Context, req ai.Request) <-chan ai.Chunk {
	if p.sent != nil {
		select {
		case p.sent <- time.Now():
		default:
		}
	}
	ch := make(chan ai.Chunk, 64)
	asked := len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == ai.RoleUser
	go func() {
		if len(p.calls) > 0 && asked {
			ch <- ai.Chunk{Done: true, ToolCalls: p.calls}
			close(ch)
			return
		}
		for i := 0; i < p.n; i++ {
			ch <- ai.Chunk{Delta: "word" + strconv.Itoa(i%10) + " "}
		}
		ch <- ai.Chunk{Done: true}
		close(ch)
	}()
	return ch
}

// aiTestModelB is aiTestModel for benchmarks and perf guards.
func aiTestModelB(tb testing.TB) *Model {
	tb.Helper()
	tb.Setenv("XDG_CONFIG_HOME", tb.TempDir())
	tb.Setenv("XDG_STATE_HOME", tb.TempDir())
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "TERMOCODE_AI_PROVIDER", "TERMOCODE_AI_API_KEY", "TERMOCODE_AI_MODEL", "TERMOCODE_AI_BASE_URL", "OLLAMA_HOST"} {
		tb.Setenv(k, "")
	}
	m := &Model{w: 120, h: 40, debounce: debounce.New(), ai: newAIState(), actionsWidth: defaultActionsWidth}
	m.ai.settings.PlanApproval = ai.PlanApprovalAuto
	return m
}

// perfAIChat is a model in a project of 200 files with an open chat of
// msgs messages, talking to p.
func perfAIChat(tb testing.TB, msgs int, p ai.Provider) *Model {
	tb.Helper()
	m := aiTestModelB(tb)
	dir := tb.TempDir()
	for i := 0; i < 200; i++ {
		f := filepath.Join(dir, "pkg"+strconv.Itoa(i%10), "f"+strconv.Itoa(i)+".go")
		_ = os.MkdirAll(filepath.Dir(f), 0o755)
		_ = os.WriteFile(f, []byte(fmt.Sprintf("package pkg\n\n// File %d.\nfunc F%d() int { return %d }\n", i, i, i)), 0o644)
	}
	tb.Chdir(dir)
	s := m.ai
	s.provider = p
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	answer := strings.Repeat("Here is the change:\n```go\nfunc f() int { return 42 }\n```\n", 6)
	for i := 0; i < msgs; i++ {
		switch i % 4 {
		case 0:
			s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredUser, Text: "Question " + strconv.Itoa(i)})
		case 1, 2:
			s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ Read pkg0/f" + strconv.Itoa(i) + ".go → 4 lines"})
		default:
			s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredAssistant, Text: answer})
		}
	}
	return m
}

// perfDrive runs the turn cmd started to its end, like the UI loop
// (without drawing).
func perfDrive(tb testing.TB, m *Model, cmd func() any) {
	tb.Helper()
	next := cmd
	for i := 0; next != nil && i < 10_000_000; i++ {
		msg := next()
		if msg == nil {
			return
		}
		c, ok := m.handleAIMsg(msg)
		if !ok {
			tb.Fatalf("unrouted msg %T", msg)
		}
		if _, done := msg.(aiChatDoneMsg); done {
			return
		}
		if c == nil {
			return
		}
		next = func() any { return c() }
	}
}

// perfTurn sends text and drives the turn to its end. It returns how
// long the whole turn took.
func perfTurn(tb testing.TB, m *Model, text string) time.Duration {
	tb.Helper()
	t0 := time.Now()
	cmd := m.sendAIChat(text)
	if cmd == nil {
		tb.Fatal("the turn did not start")
	}
	perfDrive(tb, m, func() any { return cmd() })
	if m.ai.streaming {
		tb.Fatal("still streaming")
	}
	return time.Since(t0)
}

// perfSend is the time from Enter (sendAIChat) to the request leaving.
func perfSend(tb testing.TB, m *Model, sent chan time.Time) time.Duration {
	tb.Helper()
	t0 := time.Now()
	cmd := m.sendAIChat("one more question")
	if cmd == nil {
		tb.Fatal("the turn did not start")
	}
	var dt time.Duration
	select {
	case t1 := <-sent:
		dt = t1.Sub(t0)
	case <-time.After(10 * time.Second):
		tb.Fatal("no request")
	}
	perfDrive(tb, m, func() any { return cmd() })
	return dt
}

func perfMedian(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}

func perfMs(d time.Duration) float64 { return float64(d.Microseconds()/10) / 100 }

// Perf guard: Enter in a 2 000-message chat to the request leaving
// (prompt, rules, memory, skills, tools list, history).
func TestPerfAISendToRequest(t *testing.T) {
	perfbudget.Require(t)
	sent := make(chan time.Time, 1)
	m := perfAIChat(t, 2000, perfAIProvider{n: 1, sent: sent})
	var ds []time.Duration
	for i := 0; i < 15; i++ {
		ds = append(ds, perfSend(t, m, sent))
	}
	t.Logf("send to request: median %v, all %v", perfMedian(ds), ds)
	perfbudget.Check(t, "ai_send_to_request", perfMs(perfMedian(ds)))
}

// Perf guard: a whole 3 000-token answer in a 2 000-message chat, every
// token applied to the chat and the chat saved at the end (no drawing).
func TestPerfAITurn3kTokens(t *testing.T) {
	perfbudget.Require(t)
	m := perfAIChat(t, 2000, perfAIProvider{n: 3000})
	var ds []time.Duration
	for i := 0; i < 7; i++ {
		ds = append(ds, perfTurn(t, m, "go on"))
	}
	t.Logf("turn: median %v, all %v", perfMedian(ds), ds)
	perfbudget.Check(t, "ai_turn_3k_tokens", perfMs(perfMedian(ds)))
}

// Perf guard: a turn whose model reads 20 files at once (tool cards,
// policy, results redacted) and then answers.
func TestPerfAIToolCalls20(t *testing.T) {
	perfbudget.Require(t)
	var calls []ai.ToolCall
	for i := 0; i < 20; i++ {
		calls = append(calls, ai.ToolCall{ID: "c" + strconv.Itoa(i), Name: "read_file",
			Args: map[string]any{"path": "pkg" + strconv.Itoa(i%10) + "/f" + strconv.Itoa(i) + ".go"}})
	}
	m := perfAIChat(t, 200, perfAIProvider{n: 20, calls: calls})
	var ds []time.Duration
	for i := 0; i < 9; i++ {
		ds = append(ds, perfTurn(t, m, "read them"))
	}
	if got := m.ai.conv.Messages[len(m.ai.conv.Messages)-2].Text; !strings.Contains(got, "4 lines") {
		t.Fatalf("tool line %q", got)
	}
	t.Logf("tool turn: median %v, all %v", perfMedian(ds), ds)
	perfbudget.Check(t, "ai_tool_calls_20", perfMs(perfMedian(ds)))
}

func BenchmarkAISendToRequest(b *testing.B) {
	sent := make(chan time.Time, 1)
	m := perfAIChat(b, 2000, perfAIProvider{n: 1, sent: sent})
	b.ReportAllocs()
	var total time.Duration
	for b.Loop() {
		total += perfSend(b, m, sent)
	}
	b.ReportMetric(float64(total.Microseconds())/float64(b.N), "us/send")
}

func BenchmarkAITurn3kTokens(b *testing.B) {
	m := perfAIChat(b, 2000, perfAIProvider{n: 3000})
	b.ReportAllocs()
	for b.Loop() {
		perfTurn(b, m, "go on")
	}
}

func BenchmarkAIToolCalls20(b *testing.B) {
	var calls []ai.ToolCall
	for i := 0; i < 20; i++ {
		calls = append(calls, ai.ToolCall{ID: "c" + strconv.Itoa(i), Name: "read_file",
			Args: map[string]any{"path": "pkg" + strconv.Itoa(i%10) + "/f" + strconv.Itoa(i) + ".go"}})
	}
	m := perfAIChat(b, 200, perfAIProvider{n: 20, calls: calls})
	b.ReportAllocs()
	for b.Loop() {
		perfTurn(b, m, "read them")
	}
}

func BenchmarkAIChatTools(b *testing.B) {
	m := perfAIChat(b, 0, perfAIProvider{})
	ch := make(chan tea.Msg, 1)
	root := aiWorkspaceRoot()
	b.ReportAllocs()
	for b.Loop() {
		m.aiChatTools(root, newAIIDECaller(1, ch))
	}
}

// perfAIServer is an OpenAI-compatible server that answers at once with
// tokens words; got gets the time each request arrived (body read).
func perfAIServer(tb testing.TB, tokens int, got chan time.Time) ai.Provider {
	tb.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case got <- time.Now():
		default:
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < tokens; i++ {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"word%d \"}}]}\n\n", i%10)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	tb.Cleanup(srv.Close)
	return ai.NewProvider(ai.Config{Kind: ai.KindOllama, Model: "fake-1", BaseURL: srv.URL + "/v1"})
}

func BenchmarkAISendToServer(b *testing.B) {
	got := make(chan time.Time, 1)
	m := perfAIChat(b, 2000, nil)
	m.ai.provider = perfAIServer(b, 1, got)
	b.ReportAllocs()
	var total time.Duration
	for b.Loop() {
		total += perfSend(b, m, got)
	}
	b.ReportMetric(float64(total.Microseconds())/float64(b.N), "us/send")
}

func BenchmarkAISendToServerShortChat(b *testing.B) {
	got := make(chan time.Time, 1)
	m := perfAIChat(b, 20, nil)
	m.ai.provider = perfAIServer(b, 1, got)
	b.ReportAllocs()
	var total time.Duration
	for b.Loop() {
		total += perfSend(b, m, got)
		m.ai.conv.Messages = m.ai.conv.Messages[:20]
	}
	b.ReportMetric(float64(total.Microseconds())/float64(b.N), "us/send")
}

// perfRunsStore stores 50 chats of 100 messages with 4 pipeline runs
// each (200 runs) from the last few days, and opens one more chat.
func perfRunsStore(tb testing.TB) *Model {
	tb.Helper()
	m := perfAIChat(tb, 0, perfAIProvider{})
	s := m.ai
	now := time.Now()
	cost := 0.01
	for c := 0; c < 50; c++ {
		conv := ai.NewConversation()
		for r := 0; r < 4; r++ {
			at := now.Add(-time.Duration(c*4+r) * 20 * time.Minute).UnixMilli()
			for k := 0; k < 23; k++ {
				conv.Messages = append(conv.Messages, ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ Read a.go → 10 lines"})
			}
			conv.Messages = append(conv.Messages,
				ai.StoredMessage{Role: ai.StoredUser, Text: "task " + strconv.Itoa(c) + "/" + strconv.Itoa(r) + ": fix the flaky test"},
				ai.StoredMessage{Role: ai.StoredAssistant, Text: "done", Run: &ai.StoredRun{ID: fmt.Sprintf("r%d-%d", c, r), Profile: ai.ProfileReview,
					State: ai.RunStateDone, StartedAtMs: at, ElapsedMs: 60000},
					Steps: []ai.StoredStep{
						{ID: "reviewer", Title: "Reviewer", Model: "claude-sonnet-4-5", State: "completed", StartedAtMs: at, ElapsedMs: 20000,
							Usage: &ai.Usage{Model: "claude-sonnet-4-5", Input: 4000, Output: 300, Requests: 1}, Cost: &cost, Text: "two problems",
							Tools: []string{"⚙ Read a.go → 10 lines"}},
						{ID: "fixer", Title: "Fixer", Model: "gpt-5-mini", State: "completed", StartedAtMs: at + 20000, ElapsedMs: 40000, Deps: []string{"reviewer"},
							Usage: &ai.Usage{Model: "gpt-5-mini", Input: 6000, Output: 500, Requests: 2}, Cost: &cost, Text: "fixed",
							Tools: []string{"⚙ Edit a.go", "⚙ Edit a_test.go"}},
					}})
		}
		if err := s.store.Save(&conv); err != nil {
			tb.Fatal(err)
		}
	}
	s.conv = ai.NewConversation()
	return m
}

// perfRunsOpen opens Runs and draws the sidebar list; perfRunsRedraw
// draws it again after a run event (the cache key changed).
func perfRunsOpen(m *Model) {
	m.runs = nil
	m.revealRuns(false)
	_ = m.renderRunsSidebar(40, 50)
}

func perfRunsRedraw(m *Model) {
	m.ai.runGen++
	_ = m.renderRunsSidebar(40, 50)
}

// Perf guard: open Runs with 200 stored runs, and draw it again after a
// run event.
func TestPerfRunsView200(t *testing.T) {
	perfbudget.Require(t)
	m := perfRunsStore(t)
	m.w, m.h = 160, 50
	perfRunsOpen(m)
	if n := len(m.runsCollect(runsNow())); n != 200 {
		t.Fatalf("%d runs", n)
	}
	var open, redraw []time.Duration
	for i := 0; i < 9; i++ {
		t0 := time.Now()
		perfRunsOpen(m)
		open = append(open, time.Since(t0))
		t1 := time.Now()
		perfRunsRedraw(m)
		redraw = append(redraw, time.Since(t1))
	}
	t.Logf("open %v, redraw %v", perfMedian(open), perfMedian(redraw))
	perfbudget.Check(t, "runs_view_open_200", perfMs(perfMedian(open)))
	perfbudget.Check(t, "runs_view_redraw_200", perfMs(perfMedian(redraw)))
}

func BenchmarkRunsViewOpen200(b *testing.B) {
	m := perfRunsStore(b)
	m.w, m.h = 160, 50
	b.ReportAllocs()
	for b.Loop() {
		perfRunsOpen(m)
	}
}

func BenchmarkRunsViewRedraw200(b *testing.B) {
	m := perfRunsStore(b)
	m.w, m.h = 160, 50
	perfRunsOpen(m)
	b.ReportAllocs()
	for b.Loop() {
		perfRunsRedraw(m)
	}
}
