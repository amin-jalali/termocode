package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// A one-step profile (the owner's GPT: one Codex step) answers like a
// plain chat: the answer shows once, as the message, under the step's
// model (gpt-6-sol, not the main model); no step card repeats it, no run
// line, no "A run finished" pill. A tool call still shows as its line.
// While it runs, the step's text is the message's text.

func gptReplyMsg(state, text, stepText string, tools ...string) ai.StoredMessage {
	st := ai.StoredStep{ID: "s1", Title: "GPT", CLI: ai.CLICodex, Model: "Codex · gpt-6-sol", State: string(agentdag.StateCompleted), Text: stepText, Tools: tools}
	if state == ai.RunStateRunning {
		st.State = string(agentdag.StateRunning)
	}
	return ai.StoredMessage{Role: ai.StoredAssistant, Text: text, Run: &ai.StoredRun{ID: "r1", Profile: "gpt", State: state}, Steps: []ai.StoredStep{st}}
}

func TestChatOneStepReplyShowsOnce(t *testing.T) {
	answer := "I am termocode's AI. I can read your databases."
	cm, _ := webChatMsg(1, ptrMsg(gptReplyMsg(ai.RunStateDone, answer, answer)), false)
	if cm.MD == nil || cm.Run != nil || len(cm.Steps) != 0 || cm.Model != "gpt-6-sol" {
		t.Errorf("done reply: md %v run %+v steps %+v model %q", cm.MD != nil, cm.Run, cm.Steps, cm.Model)
	}
	// With tool calls: their lines stay, the answer is not in the card.
	cm, _ = webChatMsg(1, ptrMsg(gptReplyMsg(ai.RunStateDone, "12 tables.", "12 tables.", "⚙ db_schema → 12 tables")), false)
	if len(cm.Steps) != 1 || len(cm.Steps[0].Tools) != 1 || cm.Steps[0].MD != nil || cm.Steps[0].Text != "" || cm.MD == nil {
		t.Errorf("reply with tools: %+v", cm)
	}
	// Running: the step's text is the message's, streaming.
	// (the text ends at a space: an end that could start a registered
	// secret waits for more text)
	cm, _ = webChatMsg(1, ptrMsg(gptReplyMsg(ai.RunStateRunning, "", "I am ")), false)
	if cm.Text != "I am " || !cm.Streaming || len(cm.Steps) != 0 || cm.Run != nil {
		t.Errorf("running reply: %+v", cm)
	}
	// Two steps: the step whose answer is the message's does not repeat it.
	two := gptReplyMsg(ai.RunStateDone, "final", "final")
	two.Steps = append([]ai.StoredStep{{ID: "s0", Title: "Plan", State: "completed", Text: "a plan"}}, two.Steps...)
	cm, _ = webChatMsg(1, &two, false)
	if cm.Run == nil || len(cm.Steps) != 2 || cm.Steps[0].MD == nil || cm.Steps[1].MD != nil || cm.MD == nil {
		t.Errorf("two steps: %+v", cm)
	}
}

func ptrMsg(m ai.StoredMessage) *ai.StoredMessage { return &m }

// The composer's model chip is the one-step profile's step model.
func TestChatChipShowsProfileStepModel(t *testing.T) {
	m := aiTestModel(t)
	m.ai.cfg = ai.Config{Kind: ai.KindAnthropic, Model: "claude-opus-5-5"}
	m.ai.profFake = []ai.Profile{gptProfile()}
	m.ai.reloadProfiles()
	m.ai.profile = "gpt"
	if snap := m.webChatBuild(&webChatPub{}); snap.Model != "gpt-6-sol" {
		t.Errorf("chip %q, want gpt-6-sol", snap.Model)
	}
}

// A one-step reply that finishes is no "A run finished" pill; a failed
// one still says so.
func TestAttentionNotForOneStepReply(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	m.ai.conv.Messages = []ai.StoredMessage{gptReplyMsg(ai.RunStateRunning, "", "")}
	m.ai.runMsg = 0
	m.webAfterUpdate(nil)
	m.ai.conv.Messages[0].Run.State = ai.RunStateDone
	m.ai.conv.Messages[0].Text = "hi"
	m.webAfterUpdate(nil)
	if ev := webTestEvents(t, c); len(ev) != 0 {
		t.Fatalf("a one-step reply sent %v", ev)
	}
	m.ai.conv.Messages = append(m.ai.conv.Messages, gptReplyMsg(ai.RunStateRunning, "", ""))
	m.ai.conv.Messages[1].Run.ID = "r2"
	m.ai.runMsg = 1
	m.webAfterUpdate(nil)
	m.ai.conv.Messages[1].Run.State = ai.RunStateFailed
	m.webAfterUpdate(nil)
	if ev := webTestEvents(t, c); len(ev) != 1 || ev[0] != (webproto.Attention{Kind: "run_failed", ID: "r2"}) {
		t.Fatalf("failed reply: %v", ev)
	}
}

// A one-step reply carries what the line under the answer shows and its
// tooltip: the time it took (or when it started, while it runs), its
// tokens, its step and its CLI. A longer run's steps carry their time,
// and the run when it started (the page counts while it runs).
func TestChatReplyCarriesItsMeta(t *testing.T) {
	sm := gptReplyMsg(ai.RunStateDone, "12 tables.", "12 tables.")
	sm.Steps[0].ElapsedMs, sm.Steps[0].StartedAtMs = 54104, 1794175279370
	sm.Steps[0].Usage = &ai.Usage{Input: 3001, CacheRead: 11776, Output: 32}
	cm, _ := webChatMsg(1, &sm, false)
	want := webproto.ChatMeta{ElapsedMs: 54104, StartedMs: 1794175279370, In: 14777, Out: 32, Step: "GPT", CLI: "Codex"}
	if cm.Meta == nil || *cm.Meta != want {
		t.Errorf("done reply meta %+v, want %+v", cm.Meta, want)
	}
	run := gptReplyMsg(ai.RunStateRunning, "", "I am ")
	run.Steps[0].StartedAtMs = 1794175279370
	if cm, _ = webChatMsg(1, &run, false); cm.Meta == nil || cm.Meta.StartedMs != 1794175279370 || cm.Meta.ElapsedMs != 0 {
		t.Errorf("running reply meta %+v", cm.Meta)
	}
	two := gptReplyMsg(ai.RunStateDone, "final", "final")
	two.Run.StartedAtMs = 1794175279359
	two.Steps = append([]ai.StoredStep{{ID: "s0", Title: "Plan", State: "completed", Text: "a plan", ElapsedMs: 4000}}, two.Steps...)
	cm, _ = webChatMsg(1, &two, false)
	if cm.Meta != nil || cm.Run == nil || cm.Run.StartedMs != 1794175279359 || cm.Steps[0].ElapsedMs != 4000 {
		t.Errorf("two steps: meta %+v run %+v steps %+v", cm.Meta, cm.Run, cm.Steps)
	}
	// A change of the time or the tokens sends the message again.
	a := webChatKey(&sm)
	sm.Steps[0].Usage.Output = 40
	if webChatKey(&sm) == a {
		t.Error("the key does not change with the tokens")
	}
}
