package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
)

// stepProvider answers each request with the chunks the test sends on
// steps, so the test decides when the first token comes.
type stepProvider struct{ steps chan []ai.Chunk }

func (stepProvider) Name() string        { return "fake" }
func (stepProvider) Configured() bool    { return true }
func (stepProvider) SupportsTools() bool { return true }
func (p stepProvider) Stream(ctx context.Context, _ ai.Request) <-chan ai.Chunk {
	ch := make(chan ai.Chunk, 8)
	go func() {
		defer close(ch)
		select {
		case cs := <-p.steps:
			for _, c := range cs {
				ch <- c
			}
		case <-ctx.Done():
			ch <- ai.Chunk{Done: true, Err: ctx.Err()}
		}
	}()
	return ch
}

// activityRig is a chat turn on a fake clock and a step provider.
type activityRig struct {
	t     *testing.T
	m     *Model
	p     stepProvider
	now   atomic.Int64
	start time.Time
}

func newActivityRig(t *testing.T) *activityRig {
	t.Helper()
	prevIcons := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = prevIcons })
	m := aiTestModel(t)
	m.actionsOpen, m.actionsPinned = true, true
	r := &activityRig{t: t, m: &m, p: stepProvider{steps: make(chan []ai.Chunk, 4)},
		start: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	r.now.Store(r.start.UnixNano())
	t.Cleanup(clock.Set(func() time.Time { return time.Unix(0, r.now.Load()).UTC() }))
	m.ai.provider = r.p
	m.ai.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	return r
}

func (r *activityRig) advance(d time.Duration) { r.now.Add(int64(d)) }

// next delivers the next chat message and returns it.
func (r *activityRig) next() tea.Msg {
	r.t.Helper()
	msg := nextAIMsg(r.t, waitAIChat(r.m.ai.chatCh))
	if msg == nil {
		r.t.Fatal("chat channel closed")
	}
	if _, ok := r.m.handleAIMsg(msg); !ok {
		r.t.Fatalf("unrouted %T", msg)
	}
	return msg
}

// nextEvent delivers messages until an agent event of kind k.
func (r *activityRig) nextEvent(k ai.EventKind) {
	r.t.Helper()
	for i := 0; i < 20; i++ {
		if ev, ok := r.next().(aiChatEventMsg); ok && ev.ev.Kind == k {
			return
		}
	}
	r.t.Fatalf("no event %v", k)
}

func (r *activityRig) view() string {
	return ansi.Strip(r.m.renderActionsPanel(70, r.m.h))
}

func (r *activityRig) wantView(want ...string) {
	r.t.Helper()
	v := r.view()
	for _, w := range want {
		neg := strings.HasPrefix(w, "!")
		if strings.Contains(v, strings.TrimPrefix(w, "!")) == neg {
			r.t.Fatalf("view: want %q\n%s", w, v)
		}
	}
}

func (r *activityRig) finish() {
	r.t.Helper()
	for i := 0; i < 20; i++ {
		if _, ok := r.next().(aiChatDoneMsg); ok {
			return
		}
	}
	r.t.Fatal("turn did not end")
}

// Spec (B1): while the model is silent the chat says so with the seconds;
// after 90 s without a first token it warns in the warning colour; a
// text chunk clears it; 30 s without a token while writing warns again.
func TestAIActivityStallWarning(t *testing.T) {
	r := newActivityRig(t)
	if r.m.sendAIChat("hi") == nil {
		t.Fatal("no turn")
	}
	r.nextEvent(ai.EventTurnStart)
	r.advance(4 * time.Second)
	r.wantView("◷ waiting for the model · 4 s", "!thinking", "!no answer")

	r.advance(87 * time.Second) // 91 s
	r.wantView("⚠ no answer for 1 m 31 s — Esc to stop", "!waiting for the model")
	if v := r.m.aiActivityView(clock.Now()); !v.stalled {
		t.Fatalf("not stalled: %+v", v)
	}
	warned := false
	for _, l := range r.m.aiTranscript(70) {
		if strings.Contains(l.text, "no answer") {
			warned = l.style.GetForeground() == theme.Fg(theme.DiagWarning).GetForeground()
		}
	}
	if !warned {
		t.Fatal("the stall line is not in the warning colour")
	}
	if v := ansi.Strip(r.m.renderActionsPanel(defaultActionsWidth, r.m.h)); !strings.Contains(v, "Esc to stop") {
		t.Fatalf("the warning is cut in a narrow chat:\n%s", v)
	}

	r.p.steps <- []ai.Chunk{{Delta: "Hello"}}
	r.nextEvent(ai.EventText)
	r.wantView("✎ writing", "!no answer")

	r.advance(29 * time.Second)
	r.wantView("✎ writing", "!no answer")
	r.advance(6 * time.Second)
	r.wantView("no answer for 35 s — Esc to stop")

	close(r.p.steps) // the stream ends: Done
	r.finish()
	r.wantView("!no answer", "!writing", "!waiting")
}

// Spec (B1): a tool shows its display name and seconds and never warns,
// however long it runs.
func TestAIActivityToolNeverWarns(t *testing.T) {
	r := newActivityRig(t)
	r.m.sendAIChat("read it")
	r.nextEvent(ai.EventTurnStart)
	r.p.steps <- []ai.Chunk{{Done: true, ToolCalls: []ai.ToolCall{{ID: "1", Name: "read_file", Args: map[string]any{"path": "no-such-file.txt"}}}}}
	r.nextEvent(ai.EventToolStart)
	r.advance(2 * time.Second)
	r.wantView("⚙ read_file · 2 s")
	r.advance(10 * time.Minute)
	r.wantView("⚙ read_file · 10 m 2 s", "!no answer")

	r.nextEvent(ai.EventTurnStart) // the next request after the result
	r.wantView("◷ waiting for the model · 0 s")
	r.p.steps <- []ai.Chunk{{Delta: "no file"}, {Done: true}}
	r.finish()
}

// Spec (B1): a retry shows "retry n of m" without seconds.
func TestAIActivityRetryLine(t *testing.T) {
	r := newActivityRig(t)
	r.m.sendAIChat("hi")
	r.nextEvent(ai.EventTurnStart)
	ch := r.m.ai.chatCh
	r.m.onAIChatEvent(aiChatEventMsg{gen: r.m.ai.chatGen, ev: ai.Event{Kind: ai.EventRetry, Attempt: 2, Attempts: 3}, ch: ch})
	r.advance(5 * time.Second)
	r.wantView("↻ retry 2 of 3", "!retry 2 of 3 ·")
	r.m.onAIChatEvent(aiChatEventMsg{gen: r.m.ai.chatGen, ev: ai.Event{Kind: ai.EventTurnStart}, ch: ch})
	r.wantView("↻ retry 2 of 3")
	r.p.steps <- []ai.Chunk{{Delta: "ok"}, {Done: true}}
	r.finish()
}

// Spec (B1): the 1 s clock runs only while a turn runs: one chain, re-armed
// by its own tick, and nothing armed after the turn ends (idle CPU 0).
func TestAIActivityTickOnlyWhileTurnRuns(t *testing.T) {
	r := newActivityRig(t)
	if r.m.aiActivityTick() != nil {
		t.Fatal("tick armed with no turn")
	}
	r.m.sendAIChat("hi")
	if r.m.aiActivityTick() == nil {
		t.Fatal("no tick during a turn")
	}
	if r.m.aiActivityTick() != nil {
		t.Fatal("a second tick chain")
	}
	if cmd, ok := r.m.handleAIMsg(aiActivityTickMsg{}); !ok || cmd == nil {
		t.Fatal("tick not re-armed while the turn runs")
	}
	r.p.steps <- []ai.Chunk{{Delta: "ok"}, {Done: true}}
	r.finish()
	if cmd, ok := r.m.handleAIMsg(aiActivityTickMsg{}); !ok || cmd != nil {
		t.Fatal("tick re-armed after the turn ended")
	}
	if r.m.aiActivityTick() != nil {
		t.Fatal("tick armed after the turn ended")
	}
}

// Spec (B1): Esc in the chat stops a running turn (the stall hint says
// so); without a turn it leaves the chat as before.
func TestAIActivityEscStops(t *testing.T) {
	r := newActivityRig(t)
	r.m.ai.focused = true
	r.m.sendAIChat("hi")
	r.nextEvent(ai.EventTurnStart)
	if _, ok := r.m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyEsc}); !ok {
		t.Fatal("esc not handled")
	}
	r.finish()
	if r.m.ai.streaming || !r.m.ai.focused {
		t.Fatalf("streaming=%v focused=%v", r.m.ai.streaming, r.m.ai.focused)
	}
	r.m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyEsc})
	if r.m.ai.focused {
		t.Fatal("second esc keeps focus")
	}
}
