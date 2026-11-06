package app

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/raceview"
)

// raceRecProvider records the request and answers with reply.
type raceRecProvider struct {
	mu    sync.Mutex
	reply string
	reqs  []ai.Request
}

func (*raceRecProvider) Name() string        { return "rec" }
func (*raceRecProvider) Configured() bool    { return true }
func (*raceRecProvider) SupportsTools() bool { return false }
func (p *raceRecProvider) Stream(_ context.Context, req ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan ai.Chunk, 2)
	ch <- ai.Chunk{Delta: p.reply}
	ch <- ai.Chunk{Done: true, Usage: &ai.Usage{Model: "fake-1", Input: 900, Output: 40}}
	close(ch)
	return ch
}

// Spec (AGX-6, plan 2.5): s asks the quick model for a 3-line difference
// summary of the finished lanes' diffs (capped, redacted). The answer and
// its cost are stored in the race record and shown on the page. Without
// a quick model nothing is sent and the page says how to set one.
func TestAgentRaceSummary(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	e.start(t, "change a", "", "claude", "codex")
	e.finishLane(t, 0, map[string]string{"a.txt": "one\nTWO\nthree\n", "key.txt": "token sk-" + "ant-api03-abcdefghijklmnopqrstuvwxyz0123456789\n"})
	e.finishLane(t, 1, map[string]string{"b.txt": "BEE\n"})

	// No quick model: nothing is sent.
	oldSlot := raceQuickSlot
	t.Cleanup(func() { raceQuickSlot = oldSlot })
	raceQuickSlot = func(*aiState) (aiSlot, error) { return aiSlot{}, ai.ErrNotConfigured }
	e.act(raceview.ActSummary, "")
	if page := ansi.Strip(e.m.raceS().page.Box()); !strings.Contains(page, "set a quick model") {
		t.Errorf("no hint:\n%s", page)
	}
	raceQuickSlot = oldSlot

	p := &raceRecProvider{reply: "- Claude Code edits a.txt.\n- Codex edits b.txt only.\n- Claude Code looks more complete.\n- extra line\n"}
	e.m.aiS().slotFake = map[string]aiSlot{ai.SlotQuick: {p: p, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}}
	e.act(raceview.ActSummary, "")
	if len(p.reqs) != 1 {
		t.Fatalf("%d requests", len(p.reqs))
	}
	var all strings.Builder
	for _, m := range p.reqs[0].Messages {
		all.WriteString(m.Content)
	}
	sent := all.String()
	if !strings.Contains(sent, "Lane Claude Code") || !strings.Contains(sent, "+TWO") || !strings.Contains(sent, "Lane Codex") {
		t.Errorf("prompt:\n%s", sent)
	}
	if strings.Contains(sent, "abcdefghijklmnop") {
		t.Error("a key went to the model")
	}
	r := e.race(t)
	if strings.Join(r.Summary, "|") != "Claude Code edits a.txt.|Codex edits b.txt only.|Claude Code looks more complete." {
		t.Errorf("summary %q", r.Summary)
	}
	if !strings.Contains(r.SummaryCost, "fake-1") {
		t.Errorf("cost %q", r.SummaryCost)
	}
	if page := ansi.Strip(e.m.raceS().page.Box()); !strings.Contains(page, "SUMMARY") || !strings.Contains(page, "Codex edits b.txt only.") {
		t.Errorf("page:\n%s", page)
	}
	rs, _ := agentrace.Load(filepath.Join(e.dir, ".git", "termocode"))
	if len(rs) != 1 || len(rs[0].Summary) != 3 {
		t.Errorf("not stored: %+v", rs)
	}
}

// Spec (AGX-6): fewer than two finished lanes: nothing to compare yet.
func TestAgentRaceSummaryNeedsTwoLanes(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	e.start(t, "change a", "", "claude", "codex")
	e.finishLane(t, 0, map[string]string{"a.txt": "x\n"})
	p := &raceRecProvider{reply: "x"}
	e.m.aiS().slotFake = map[string]aiSlot{ai.SlotQuick: {p: p, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}}
	e.act(raceview.ActSummary, "")
	if len(p.reqs) != 0 || !strings.Contains(ansi.Strip(e.m.raceS().page.Box()), "two finished lanes") {
		t.Errorf("sent %d", len(p.reqs))
	}
}
