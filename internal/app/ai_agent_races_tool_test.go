package app

import (
	"context"
	"os"
	"path/filepath"
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
	if ask, class := aiEffGate(tl, args); ask != (args["op"] == "discard" || args["op"] == "restack") || class != "" {
		t.Errorf("agent_races %v: ask=%v class=%q; only discard and restack have a card on the page", args["op"], ask, class)
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

// The page's lane keys: merge (no card) writes a finished lane into the
// user's files; discard (its card asks) removes a lane; restack asks;
// judge needs two finished lanes and a main model.
func TestAIAgentRacesLaneOps(t *testing.T) {
	e, _ := webRaceRig(t, "claude", "codex")
	race := e.start(t, "change a", "", "claude", "codex")
	if _, err := aiAgentRacesRun(t, e.m, map[string]any{"op": "merge", "race": race.ID, "lane": race.Lanes[0].ID}); err == nil || !strings.Contains(err.Error(), "finished") {
		t.Errorf("merge of a working lane: %v", err)
	}
	if _, err := aiAgentRacesRun(t, e.m, map[string]any{"op": "restack", "race": race.ID, "lane": race.Lanes[0].ID}); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Errorf("restack without a mode: %v", err)
	}
	e.finishLane(t, 0, map[string]string{"new.txt": "from claude\n"})
	if _, err := aiAgentRacesRun(t, e.m, map[string]any{"op": "judge", "race": race.ID}); err == nil || !strings.Contains(err.Error(), "two finished lanes") {
		t.Errorf("judge of one finished lane: %v", err)
	}
	e.finishLane(t, 1, map[string]string{"other.txt": "from codex\n"})
	out, err := aiAgentRacesRun(t, e.m, map[string]any{"op": "merge", "race": race.ID, "lane": race.Lanes[0].ID})
	if err != nil || !strings.Contains(out, "new.txt") {
		t.Fatalf("merge: %q %v", out, err)
	}
	if b, err := os.ReadFile(filepath.Join(e.dir, "new.txt")); err != nil || string(b) != "from claude\n" {
		t.Fatalf("new.txt after the merge: %q %v", b, err)
	}
	if e.race(t).Lanes[0].State != agentrace.Merged {
		t.Errorf("lane 0 is %s", e.race(t).Lanes[0].State)
	}
	e.m.confirmOpen = false // the page may offer to discard the other lanes
	out, err = aiAgentRacesRun(t, e.m, map[string]any{"op": "discard", "race": race.ID, "lane": race.Lanes[1].ID})
	if err != nil || !strings.Contains(out, "Discarded") {
		t.Fatalf("discard: %q %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "other.txt")); err == nil {
		t.Error("a discarded lane's file reached the user's tree")
	}
}
