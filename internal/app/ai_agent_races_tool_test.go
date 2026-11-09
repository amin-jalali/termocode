package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/ai"
)

// AI parity (races): agent_races does the race compare page's list,
// summary (s), verify (v) and stop (x), as the web's ai.race_* ops do.
// None of them has a card in the GUI, so none asks.

func aiAgentRacesRun(t *testing.T, m *Model, args map[string]any) (string, error) {
	t.Helper()
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	tl, ok := ai.FindTool(m.aiChatTools(aiWorkspaceRoot(), call), "agent_races")
	if !ok {
		t.Fatal("no agent_races tool in the chat")
	}
	if tl.Mutating || tl.Gate != nil {
		t.Errorf("agent_races asks (mutating %v, gate %v); the compare page does not", tl.Mutating, tl.Gate != nil)
	}
	if err := tl.Validate(args); err != nil {
		return "", err
	}
	return tl.Run(context.Background(), args)
}

func TestAIAgentRacesListSummaryVerifyStop(t *testing.T) {
	e, _ := webRaceRig(t, "claude", "codex")
	race := e.start(t, "change a", "go test ./...", "claude", "codex")
	out, err := aiAgentRacesRun(t, e.m, map[string]any{"op": "list"})
	if err != nil || !strings.Contains(out, race.ID) || !strings.Contains(out, `"change a"`) || !strings.Contains(out, "verify: go test ./...") {
		t.Fatalf("list: %q %v", out, err)
	}
	e.finishLane(t, 0, map[string]string{"a.txt": "x\n"})
	if _, err := aiAgentRacesRun(t, e.m, map[string]any{"op": "summary", "race": race.ID}); err == nil || !strings.Contains(err.Error(), "two finished lanes") {
		t.Errorf("summary of one finished lane: %v", err)
	}
	var jobs []string
	old := raceSpawnVerify
	t.Cleanup(func() { raceSpawnVerify = old })
	raceSpawnVerify = func(_ *Model, l agentrace.Lane, line string) (int, error) {
		jobs = append(jobs, l.ID+" "+line)
		return 700 + len(jobs), nil
	}
	out, err = aiAgentRacesRun(t, e.m, map[string]any{"op": "verify", "race": race.ID})
	if err != nil || !strings.Contains(out, "Verifying 1 lane") || len(jobs) != 1 {
		t.Fatalf("verify: %q %v jobs %v", out, err, jobs)
	}
	e.finishLane(t, 1, map[string]string{"b.txt": "BEE\n"})
	p := &raceRecProvider{reply: "- Claude Code edits a.txt.\n- Codex adds b.txt.\n"}
	e.m.aiS().slotFake = map[string]aiSlot{ai.SlotQuick: {p: p, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}}
	out, err = aiAgentRacesRun(t, e.m, map[string]any{"op": "summary", "race": race.ID})
	if err != nil || len(p.reqs) != 1 || !strings.Contains(out, "Claude Code edits a.txt.") || !strings.Contains(out, "fake-1") {
		t.Fatalf("summary: %q %v (%d sent)", out, err, len(p.reqs))
	}
	if got := e.race(t).Summary; len(got) != 2 {
		t.Errorf("the summary is not stored on the race: %q", got)
	}
	other := e.start(t, "change b", "", "claude", "codex")
	out, err = aiAgentRacesRun(t, e.m, map[string]any{"op": "stop", "race": other.ID})
	if err != nil || !strings.Contains(out, "Stopped 2 lane") || e.race(t).Lanes[0].State != agentrace.Stopped {
		t.Fatalf("stop: %q %v lanes %+v", out, err, e.race(t).Lanes)
	}
	if _, err := aiAgentRacesRun(t, e.m, map[string]any{"op": "stop", "race": other.ID}); err == nil {
		t.Error("a race with no working lane stopped again")
	}
	if _, err := aiAgentRacesRun(t, e.m, map[string]any{"op": "verify", "race": "race-99"}); err == nil {
		t.Error("an unknown race was verified")
	}
}

// The races of a session that never opened the compare page are read
// from disk first.
func TestAIAgentRacesReadsTheRecords(t *testing.T) {
	e, _ := webRaceRig(t, "claude", "codex")
	race := e.start(t, "change a", "", "claude", "codex")
	e.m.race = nil
	out, err := aiAgentRacesRun(t, e.m, map[string]any{"op": "list"})
	if err != nil || !strings.Contains(out, race.ID) {
		t.Fatalf("list from disk: %q %v", out, err)
	}
}
