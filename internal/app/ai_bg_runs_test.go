package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
)

// gateProvider answers each request with the next reply sent on steps
// (it waits for one) and records the requests.
type gateProvider struct {
	steps chan ai.Chunk
	mu    sync.Mutex
	reqs  [][]ai.Message
	level []string // each request's reasoning level
}

func newGateProvider() *gateProvider { return &gateProvider{steps: make(chan ai.Chunk, 8)} }

func (*gateProvider) Name() string        { return "gate" }
func (*gateProvider) Configured() bool    { return true }
func (*gateProvider) SupportsTools() bool { return true }
func (p *gateProvider) Stream(ctx context.Context, req ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	p.reqs = append(p.reqs, append([]ai.Message(nil), req.Messages...))
	p.level = append(p.level, req.Reasoning)
	p.mu.Unlock()
	ch := make(chan ai.Chunk, 2)
	go func() {
		defer close(ch)
		select {
		case r := <-p.steps:
			if r.Delta != "" {
				ch <- ai.Chunk{Delta: r.Delta}
			}
			ch <- ai.Chunk{Done: true, ToolCalls: r.ToolCalls, Usage: r.Usage}
		case <-ctx.Done():
			ch <- ai.Chunk{Done: true, Err: ctx.Err()}
		}
	}()
	return ch
}

func (p *gateProvider) requests() [][]ai.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]ai.Message(nil), p.reqs...)
}

// bgRig is a chat (main: chat) whose background runs run on quick (bg).
type bgRig struct {
	t        *testing.T
	m        *Model
	chat, bg *gateProvider
	chatCh   chan tea.Msg
	bgChs    []chan tea.Msg
	ended    bool
}

func newBgRig(t *testing.T) *bgRig {
	t.Helper()
	r := &bgRig{t: t, chat: newGateProvider(), bg: newGateProvider()}
	r.m, _ = aiRunModel(t, r.chat, r.bg)
	r.m.ai.profile = ""
	r.m.ai.conv.Title = "fix the parser"
	r.m.output = newOutputStore()
	return r
}

func (r *bgRig) send(text string) {
	r.m.sendAIChat(text)
	r.chatCh = r.m.ai.chatCh
}

// pump routes the chat's and the background runs' messages until ok.
func (r *bgRig) pump(ok func() bool) {
	r.t.Helper()
	deadline := time.After(10 * time.Second)
	// A provider records its request in its own goroutine after the
	// last message (the turn's start) went out: without a recheck, a
	// slow runner checked ok before the request and waited for ever.
	recheck := time.NewTicker(10 * time.Millisecond)
	defer recheck.Stop()
	for !ok() {
		for _, b := range r.m.ai.bgRuns {
			known := false
			for _, c := range r.bgChs {
				known = known || c == b.ch
			}
			if !known {
				r.bgChs = append(r.bgChs, b.ch)
			}
		}
		var bgCh chan tea.Msg
		for _, c := range r.bgChs {
			if c != nil {
				bgCh = c
				break
			}
		}
		var msg tea.Msg
		var open bool
		select {
		case msg, open = <-r.chatCh:
			if !open {
				r.chatCh = nil
				continue
			}
		case msg, open = <-bgCh:
			if !open {
				for i, c := range r.bgChs {
					if c == bgCh {
						r.bgChs[i] = nil
					}
				}
				continue
			}
		case <-recheck.C:
			continue
		case <-deadline:
			r.t.Fatal("condition never true")
		}
		if c, isCard := msg.(aiChatConfirmMsg); isCard {
			r.m.handleAIMsg(msg)
			r.m.confirmOpen = false
			r.m.answerAIToolConfirm(map[bool]string{true: "allow", false: "deny"}[c.tool.Name == "agent_run_start"])
			continue
		}
		if _, ok := r.m.handleAIMsg(msg); !ok {
			r.t.Fatalf("unrouted %T", msg)
		}
		if d, done := msg.(aiChatDoneMsg); done && d.gen == r.m.ai.chatGen {
			r.ended = true
		}
	}
}

var bgSteps = []any{map[string]any{"title": "Scan", "category": "quick", "tools": "read-only", "prompt": "find the TODOs"}}

// OMX-6: a run whose steps only read starts at once and runs beside the
// turn that started it; when it ends, its answer is handed to that turn
// (the model reads it after its next reply), and when the turn ends the
// run joins the chat, marked as started by the AI.
func TestAIBackgroundRunRunsBesideTheTurn(t *testing.T) {
	r := newBgRig(t)
	r.send("count the TODOs")
	r.chat.steps <- toolCall("agent_run_start", map[string]any{"task": "Find the TODOs in the repo", "steps": bgSteps})
	r.pump(func() bool { return len(r.m.ai.bgRuns) == 1 && len(r.chat.requests()) == 2 })
	res := r.chat.requests()[1]
	got := aiToolJSON(t, res[len(res)-1].Content)
	id, _ := got["run_id"].(string)
	if got["state"] != "running" || id == "" {
		t.Fatalf("tool result %v", got)
	}
	if !r.m.ai.streaming {
		t.Fatal("the turn must still go")
	}
	// AGENTS shows the run while the turn goes.
	if d, ok := r.m.ai.agentsData(); !ok || d.msg.Run.ID != id {
		t.Fatalf("AGENTS does not show %s", id)
	}
	r.bg.steps <- ai.Chunk{Delta: "3 TODOs: a.go:1, b.go:2, c.go:3"}
	r.pump(func() bool { return r.m.ai.aiBgFind(id) != nil && r.m.ai.aiBgFind(id).done })
	r.chat.steps <- ai.Chunk{Delta: "I started a scan."}
	r.pump(func() bool { return len(r.chat.requests()) == 3 })
	reqs := r.chat.requests()
	note := reqs[2][len(reqs[2])-1].Content
	for _, want := range []string{aiBgNotePrefix, "background run " + id, "ended: done", "3 TODOs: a.go:1"} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q:\n%s", want, note)
		}
	}
	r.chat.steps <- ai.Chunk{Delta: "There are 3 TODOs."}
	r.pump(func() bool { return r.ended })
	rm := aiRunMsg(t, r.m)
	if rm.Run.ID != id || rm.Run.State != ai.RunStateDone || rm.Run.By == nil || rm.Text != "3 TODOs: a.go:1, b.go:2, c.go:3" {
		t.Fatalf("run in the chat %+v text %q", rm.Run, rm.Text)
	}
	if len(r.m.ai.bgRuns) != 0 {
		t.Fatalf("held runs left: %d", len(r.m.ai.bgRuns))
	}
}

// OMX-6: agent_run_wait waits for a background run and returns its
// result (no second note then); agent_run_send steers its running step.
func TestAIBackgroundRunWaitAndSend(t *testing.T) {
	r := newBgRig(t)
	r.send("count the TODOs")
	r.chat.steps <- toolCall("agent_run_start", map[string]any{"task": "Find the TODOs", "steps": bgSteps})
	r.pump(func() bool { return len(r.chat.requests()) == 2 })
	id := r.m.ai.bgRuns[0].id
	r.pump(func() bool { return len(r.bg.requests()) == 1 }) // the step works
	r.chat.steps <- toolCall("agent_run_send", map[string]any{"run_id": id, "message": "also count FIXME"})
	r.pump(func() bool { return len(r.chat.requests()) == 3 })
	if got := r.chat.requests()[2]; !strings.Contains(got[len(got)-1].Content, "Sent to step step-1") {
		t.Fatalf("send result %q", got[len(got)-1].Content)
	}
	r.chat.steps <- toolCall("agent_run_wait", map[string]any{"run_id": id, "timeout_s": 30})
	r.bg.steps <- ai.Chunk{Delta: "looking"}
	r.pump(func() bool { return len(r.bg.requests()) == 2 })
	if got := r.bg.requests()[1]; !strings.Contains(got[len(got)-1].Content, "also count FIXME") {
		t.Fatalf("the step did not get the message: %q", got[len(got)-1].Content)
	}
	// The wait must hold the run before it ends: a run that ends with no
	// waiter hands the chat a note (a slow runner ended it first).
	r.pump(func() bool { b := r.m.ai.aiBgFind(id); return b != nil && b.waiters.Load() == 1 })
	r.bg.steps <- ai.Chunk{Delta: "2 TODOs, 1 FIXME"}
	r.pump(func() bool { return len(r.chat.requests()) == 4 })
	wait := r.chat.requests()[3]
	got := aiToolJSON(t, wait[len(wait)-1].Content)
	if got["ended"] != true || !strings.Contains(wait[len(wait)-1].Content, "2 TODOs, 1 FIXME") {
		t.Fatalf("wait result %v", got)
	}
	r.chat.steps <- ai.Chunk{Delta: "Done."}
	r.pump(func() bool { return r.ended })
	if n := len(r.chat.requests()); n != 4 {
		t.Fatalf("the turn made %d requests: a note came after the wait", n)
	}
}

// OMX-6: a run that may change files still waits for the chat.
func TestAIBackgroundRunOnlyForReadOnlySteps(t *testing.T) {
	r := newBgRig(t)
	r.send("fix it")
	r.chat.steps <- toolCall("agent_run_start", map[string]any{"task": "Fix the TODOs",
		"steps": []any{map[string]any{"title": "Fix", "category": "quick", "prompt": "fix"}}})
	r.pump(func() bool { return len(r.chat.requests()) == 2 })
	res := r.chat.requests()[1]
	if got := aiToolJSON(t, res[len(res)-1].Content); got["state"] != "queued" || len(r.m.ai.bgRuns) != 0 {
		t.Fatalf("tool result %v, background runs %d", got, len(r.m.ai.bgRuns))
	}
	r.chat.steps <- ai.Chunk{Delta: "queued"}
	r.pump(func() bool { return r.m.ai.run != nil }) // the turn ended: the queued run starts in the chat
	r.m.aiCancelAll()
}

// OMX-6: Ctrl+C (stop the AI) stops background runs too.
func TestAIBackgroundRunStopsWithTheAI(t *testing.T) {
	r := newBgRig(t)
	r.send("count")
	r.chat.steps <- toolCall("agent_run_start", map[string]any{"task": "Find the TODOs", "steps": bgSteps})
	r.pump(func() bool { return len(r.chat.requests()) == 2 })
	id := r.m.ai.bgRuns[0].id
	r.m.aiCancelAll()
	r.pump(func() bool { b := r.m.ai.aiBgFind(id); return b == nil || b.done })
	r.pump(func() bool { return r.ended })
	rm := aiRunMsg(t, r.m)
	if rm.Run.ID != id || rm.Run.State != ai.RunStateCancelled {
		t.Fatalf("run %+v", rm.Run)
	}
}

// OMX-6: a background step that runs once more (a stall, ESCALATE) adds
// the second try's tokens and cost to the first's.
func TestAIBackgroundStepUsageAddsUp(t *testing.T) {
	r := newBgRig(t)
	b := &aiBgRun{id: "r1", slots: map[string]aiSlot{ai.SlotQuick: {cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}}}}
	st := &ai.StoredStep{ID: "a", Slot: ai.SlotQuick, Model: "gpt-5-mini"}
	u := &ai.Usage{Model: "gpt-5-mini", Input: 100000, Output: 10000, Requests: 1}
	r.m.aiBgStepUsage(b, st, agentrun.Event{Usage: u})
	if st.Cost == nil {
		t.Fatal("no price for gpt-5-mini")
	}
	first := *st.Cost
	r.m.aiBgStepUsage(b, st, agentrun.Event{Usage: u})
	if st.Cost == nil || *st.Cost < 1.99*first || st.Usage.Input != 200000 {
		t.Fatalf("cost %v (first %v) usage %+v", st.Cost, first, st.Usage)
	}
}

// A background step gets the reasoning level of its slot, and its
// category's level when the category sets one.
func TestAIBackgroundRunReasoning(t *testing.T) {
	for _, tc := range []struct {
		cat, want string
	}{{"", ai.ReasoningHigh}, {ai.ReasoningXHigh, ai.ReasoningXHigh}} {
		r := newBgRig(t)
		quick := r.m.ai.slotFake[ai.SlotQuick]
		quick.cfg.Reasoning = ai.ReasoningHigh
		r.m.ai.slotFake[ai.SlotQuick] = quick
		if tc.cat != "" {
			r.m.ai.settings.Categories = map[string]ai.CategoryConfig{"quick": {Reasoning: tc.cat}}
		}
		r.send("count the TODOs")
		r.chat.steps <- toolCall("agent_run_start", map[string]any{"task": "Find the TODOs in the repo", "steps": bgSteps})
		r.pump(func() bool { return len(r.m.ai.bgRuns) == 1 && len(r.bg.requests()) == 1 })
		r.bg.mu.Lock()
		got := r.bg.level[0]
		r.bg.mu.Unlock()
		if got != tc.want {
			t.Errorf("category %q: background step reasoning %q, want %q", tc.cat, got, tc.want)
		}
		// The run ends first; the turn then reads its note in one more
		// request before it ends.
		r.bg.steps <- ai.Chunk{Delta: "done"}
		r.pump(func() bool { b := r.m.ai.bgRuns[0]; return b.done })
		r.chat.steps <- ai.Chunk{Delta: "ok"}
		r.pump(func() bool { return len(r.chat.requests()) == 3 })
		r.chat.steps <- ai.Chunk{Delta: "fine"}
		r.pump(func() bool { return r.ended && len(r.m.ai.bgRuns) == 0 })
	}
}
