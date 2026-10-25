package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
)

// OM-5: a running node card shows the chat's activity line and warns
// "possibly stalled" with the chat's thresholds (30 s without a token
// while writing, 90 s without a first token, never during a tool).

type stallRig struct {
	t   *testing.T
	m   *Model
	now time.Time
}

func newStallRig(t *testing.T) *stallRig {
	t.Helper()
	agentsUnicode(t)
	perfColour(t)
	r := &stallRig{t: t, now: time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)}
	r.m = agentsModel(t, agentsRunMsgs("r1", ai.RunStateRunning, agentsDiamondSteps())...)
	t.Cleanup(clock.Set(func() time.Time { return r.now })) // after the model: it sets its own clock
	r.m.ai.runMsg = 1
	r.m.ai.streaming = true
	r.m.ai.runFiles = map[string]int{}
	r.ev(agentrun.NodeStart, ai.Event{}, "left")
	return r
}

func (r *stallRig) ev(k agentrun.EventKind, a ai.Event, node string) {
	r.t.Helper()
	r.m.onAIRunEvent(aiRunEventMsg{gen: r.m.ai.chatGen, ev: agentrun.Event{Kind: k, RunID: "r1", NodeID: node, AI: a}})
}

func (r *stallRig) advance(d time.Duration) { r.now = r.now.Add(d) }

func (r *stallRig) card() string {
	return strings.Join(agentsCardRows(r.t, r.m, "left", 40, r.now), "\n")
}

// borderWarn reports whether the card's border is drawn in the warning colour.
func (r *stallRig) borderWarn() bool {
	d, _ := r.m.ai.agentsData()
	for _, n := range d.nodes {
		if n.id == "left" {
			top := r.m.ai.agentsCard(d, n, 40, false, r.now, agentsGlyphs())[0]
			return strings.Contains(top, theme.FgBg(theme.DiagWarning, theme.BgPanel).Render("┌ "))
		}
	}
	return false
}

func (r *stallRig) chatStep() string {
	var b strings.Builder
	for _, l := range r.m.buildAITranscriptAt(80, aiActivityView{}) {
		b.WriteString(l.text + ansi.Strip(l.styled) + "\n")
	}
	return b.String()
}

func TestAgentsNodeStalledWhileWriting(t *testing.T) {
	r := newStallRig(t)
	r.ev(agentrun.NodeText, ai.Event{Kind: ai.EventText}, "left")
	r.advance(10 * time.Second)
	if c := r.card(); strings.Contains(c, "stalled") || r.borderWarn() {
		t.Fatalf("flagged too early:\n%s", c)
	}
	r.advance(21 * time.Second) // 31 s without a token
	wantAll(t, r.card(), "⚠ possibly stalled · 31s")
	if !r.borderWarn() {
		t.Error("border is not the warning colour")
	}
	if !strings.Contains(r.chatStep(), "possibly stalled") {
		t.Errorf("chat step card does not warn:\n%s", r.chatStep())
	}
	// a token clears it
	r.ev(agentrun.NodeText, ai.Event{Kind: ai.EventText}, "left")
	if c := r.card(); strings.Contains(c, "stalled") || r.borderWarn() {
		t.Errorf("token did not clear the warning:\n%s", c)
	}
	if strings.Contains(r.chatStep(), "possibly stalled") {
		t.Error("chat step still warns")
	}
}

func TestAgentsNodeStalledNoFirstToken(t *testing.T) {
	r := newStallRig(t)
	r.advance(31 * time.Second)
	if strings.Contains(r.card(), "stalled") {
		t.Error("flagged before the first-token limit")
	}
	r.advance(60 * time.Second)
	wantAll(t, r.card(), "⚠ possibly stalled · 1m 31s")
}

func TestAgentsNodeToolNeverStalled(t *testing.T) {
	r := newStallRig(t)
	r.ev(agentrun.NodeTool, ai.Event{Kind: ai.EventToolStart, Tool: "run_tests", Label: "Run tests", Detail: "go test"}, "left")
	r.advance(5 * time.Minute)
	if c := r.card(); strings.Contains(c, "stalled") || r.borderWarn() {
		t.Fatalf("a tool was flagged:\n%s", c)
	}
}

func TestAgentsNodeFinishedNoActivity(t *testing.T) {
	r := newStallRig(t)
	r.ev(agentrun.NodeText, ai.Event{Kind: ai.EventText}, "left")
	r.advance(40 * time.Second)
	r.ev(agentrun.NodeDone, ai.Event{}, "left")
	if c := r.card(); strings.Contains(c, "stalled") || r.borderWarn() {
		t.Fatalf("finished node flagged:\n%s", c)
	}
	if _, ok := r.m.ai.agentsNodeView("r1", "left", r.now); ok {
		t.Error("finished node still has an activity")
	}
}

// Parallel nodes stall on their own: Right writes, Left is silent.
func TestAgentsNodeStallIsPerNode(t *testing.T) {
	r := newStallRig(t)
	r.ev(agentrun.NodeStart, ai.Event{}, "right")
	r.ev(agentrun.NodeText, ai.Event{Kind: ai.EventText}, "left")
	r.advance(25 * time.Second)
	r.ev(agentrun.NodeText, ai.Event{Kind: ai.EventText}, "right")
	r.advance(10 * time.Second)
	if v, _ := r.m.ai.agentsNodeView("r1", "right", r.now); v.stalled {
		t.Error("right is stalled")
	}
	wantAll(t, r.card(), "possibly stalled")
}
