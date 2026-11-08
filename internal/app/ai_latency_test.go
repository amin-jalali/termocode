package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
)

// Our own time on a chat message of the GPT profile (one Codex step),
// with a fake Codex: from send until the CLI is started, and from the
// CLI's first text until the page has it. The rest is Codex's own.
// Each part is logged; together they stay under aiOverheadBudget.

const aiOverheadBudget = 300 * time.Millisecond

// A one-step chat profile's CLI step thinks low unless the message says
// "think" (or the chat's level is chosen); a pipeline step keeps the
// CLI's own default.
func TestAIOneStepChatReasoning(t *testing.T) {
	f := newFakeCLI(t, map[string][]fakeCLIReply{"s1": {{text: "hi"}}}, "")
	m, _ := gptRunModel(t)
	m.output = newOutputStore()
	driveAIChat(t, m, m.sendAIChat("hey"), nil)
	driveAIChat(t, m, m.sendAIChat("think: how is the schema built?"), nil)
	_, specs := f.calls("s1")
	if len(specs) != 2 || specs[0].Reasoning != ai.ReasoningLow || specs[1].Reasoning != ai.ReasoningHigh {
		t.Fatalf("reasoning %+v", specs)
	}
	f = newFakeCLI(t, map[string][]fakeCLIReply{"work": {{text: "x"}}, "review": {{text: "VERDICT: PASS"}}}, "")
	m, _ = cliRunModel(t)
	m.output = newOutputStore()
	driveAIChat(t, m, m.sendAIChat("go"), nil)
	if _, specs := f.calls("work"); len(specs) == 0 || specs[0].Reasoning != "" {
		t.Errorf("a pipeline step: %+v", specs)
	}
}

func TestAICLIStepOverhead(t *testing.T) {
	m, _ := gptRunModel(t)
	m.output = newOutputStore()
	webTestHub(t, m)
	c := webTestSub(t, m, "chat")
	webTestNow(t, c) // the snapshot

	type stamp struct{ exec, text time.Time }
	stamps := make(chan stamp, 1)
	seen := make(chan struct{})
	old := aiCLIRun
	t.Cleanup(func() { aiCLIRun = old })
	aiCLIRun = func(ctx context.Context, s clistep.Spec, _ string, emit func(ai.Event)) (clistep.Result, error) {
		var st stamp
		st.exec = time.Now()
		emit(ai.Event{Kind: ai.EventTurnStart})
		st.text = time.Now()
		emit(ai.Event{Kind: ai.EventText, Text: "hello-token"})
		stamps <- st
		select { // the answer ends once the page has the first text
		case <-seen:
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
		return clistep.Result{Text: "hello-token"}, nil
	}

	sent := time.Now()
	cmd := m.sendAIChat("hey")
	m.webAfterUpdate(nil)
	var st stamp
	var page time.Time
	for i := 0; cmd != nil && i < 200; i++ {
		msg := nextAIMsg(t, cmd)
		if msg == nil {
			break
		}
		if _, ok := m.handleAIMsg(msg); !ok {
			t.Fatalf("unrouted %T", msg)
		}
		m.webAfterUpdate(msg)
		if page.IsZero() {
			for _, f := range webTestNow(t, c) {
				if b, _ := json.Marshal(f); strings.Contains(string(b), "hello-token") {
					page = time.Now()
					close(seen)
					break
				}
			}
		}
		if _, done := msg.(aiChatDoneMsg); done {
			break
		}
		cmd = waitAIChat(m.ai.chatCh)
	}
	select {
	case st = <-stamps:
	default:
		t.Fatal("the CLI never started")
	}
	if page.IsZero() {
		t.Fatal("the page never got the first text")
	}
	start, first := st.exec.Sub(sent), page.Sub(st.text)
	t.Logf("send → CLI start %v · first text → page %v", start, first)
	if start+first > aiOverheadBudget {
		t.Errorf("our overhead %v (send → CLI %v, text → page %v) is over %v", start+first, start, first, aiOverheadBudget)
	}
}
