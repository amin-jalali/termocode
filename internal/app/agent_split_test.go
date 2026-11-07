package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/raceview"
)

var splitTestParts = []agentrace.LaneSpec{
	{Agent: "claude", Title: "Claude Code", Prompt: "the theme tokens"},
	{Agent: "codex", Title: "Codex", Prompt: "the settings toggle"},
}

// Spec (CMP-4): the AI coordinates with agent_split_start: one goal cut
// into 2–4 parts, each part its own outside agent in its own worktree.
// It always asks (Deny first; no Always allow class), is never offered to
// a read-only step or to outside agents (so a worker cannot start
// workers), and checks its arguments before the card: installed agents
// whose screen termocode reads, a goal, a task per part, no key or token.
// agent_split_status is read only and also stays inside termocode.
func TestAIAgentSplitStartTool(t *testing.T) {
	m := aiWriteModel(t)
	fakeAgentLookPath(t, "claude", "codex", "amp")
	tools := m.aiChatTools(".", nil)
	tl, ok := ai.FindTool(tools, "agent_split_start")
	if !ok || !tl.Mutating || tl.Class != "" || tl.Gate != nil {
		t.Fatalf("tool %v mutating %v class %q", ok, tl.Mutating, tl.Class)
	}
	st, ok := ai.FindTool(tools, "agent_split_status")
	if !ok || st.Mutating || !st.ReadOnly {
		t.Fatalf("status tool %v mutating %v read-only %v", ok, st.Mutating, st.ReadOnly)
	}
	for _, name := range []string{"agent_split_start", "agent_split_status"} {
		if _, ok := mcpServeRow(name); ok {
			t.Errorf("%s is offered to outside agents", name)
		}
	}
	if _, ok := ai.FindTool(aiFilterTools(tools, agentdag.ToolRule{Preset: agentdag.PresetReadOnly}), "agent_split_start"); ok {
		t.Error("a read-only step gets agent_split_start")
	}
	var started [][]string
	defer func() { aiSplitStartHook = nil }()
	aiSplitStartHook = func(_ *Model, goal, verify string, parts []agentrace.LaneSpec) {
		got := []string{goal, verify}
		for _, p := range parts {
			got = append(got, p.Agent+":"+p.Prompt)
		}
		started = append(started, got)
	}
	args := map[string]any{"goal": "add dark mode", "verify": "go test ./...", "parts": []any{
		map[string]any{"agent": "claude", "task": "the theme tokens"},
		map[string]any{"agent": "codex", "task": "the settings toggle"},
	}}
	res, card := aiWriteTurn(t, m, "agent_split_start", args, "deny")
	for _, want := range []string{"2 parts", "add dark mode", "Claude Code: the theme tokens", "Codex: the settings toggle", "own git worktree", "go test ./..."} {
		if !strings.Contains(card.body, want) {
			t.Errorf("card has no %q: %q", want, card.body)
		}
	}
	if strings.Join(card.buttons, " · ") != "Deny · Start split" {
		t.Errorf("buttons %v", card.buttons)
	}
	if len(started) != 0 || !strings.Contains(res, "declined") {
		t.Fatalf("deny started %v; result %q", started, res)
	}
	res, _ = aiWriteTurn(t, m, "agent_split_start", args, "allow")
	if len(started) != 1 || strings.Join(started[0], "|") != "add dark mode|go test ./...|claude:the theme tokens|codex:the settings toggle" ||
		!strings.Contains(res, "agent_split_status") {
		t.Fatalf("allow started %v; result %q", started, res)
	}
	part := func(agent, task string) map[string]any { return map[string]any{"agent": agent, "task": task} }
	for name, bad := range map[string]map[string]any{
		"one part":      {"goal": "g", "parts": []any{part("claude", "x")}},
		"five parts":    {"goal": "g", "parts": []any{part("claude", "a"), part("codex", "b"), part("claude", "c"), part("codex", "d"), part("claude", "e")}},
		"not installed": {"goal": "g", "parts": []any{part("claude", "a"), part("gemini", "b")}},
		"no rules":      {"goal": "g", "parts": []any{part("claude", "a"), part("amp", "b")}},
		"no goal":       {"goal": " ", "parts": []any{part("claude", "a"), part("codex", "b")}},
		"no task":       {"goal": "g", "parts": []any{part("claude", "a"), part("codex", " ")}},
		"secret":        {"goal": "g", "parts": []any{part("claude", "use sk-"+"ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"), part("codex", "b")}},
	} {
		res, card := aiWriteTurn(t, m, "agent_split_start", bad, "allow")
		if card.opened || len(started) != 1 {
			t.Errorf("%s: card %v started %d (%q)", name, card.opened, len(started), res)
		}
		if strings.Contains(res, "abcdefghijklmnop") {
			t.Errorf("%s: the refusal repeats the secret", name)
		}
	}
}

// Spec (CMP-4): a split makes one agent worktree per part, like a race,
// but types each lane its own part (with the goal), never another lane's.
// The page says SPLIT and shows the selected lane's part. Merging one part
// asks nothing about the others (they are not losers): each part is
// merged on its own with m, and no winner is set.
func TestAgentSplitTypesAndMergesEachPart(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	e.feed(e.m.startSplit("add dark mode", "", splitTestParts))
	r := e.race(t)
	if !r.Split() || r.Name != "split/add-dark-mode" || len(r.Lanes) != 2 {
		t.Fatalf("split %+v", r)
	}
	if page := ansi.Strip(e.m.raceS().page.Box()); !strings.Contains(page, "SPLIT") || !strings.Contains(page, "the theme tokens") {
		t.Errorf("page:\n%s", page)
	}
	e.finishLane(t, 0, map[string]string{"theme.txt": "dark\n"})
	e.finishLane(t, 1, map[string]string{"toggle.txt": "on\n"})
	for i, l := range e.race(t).Lanes {
		typed := strings.Join(e.typed[e.m.raceS().bufOf(l.ID)], "|")
		own, other := splitTestParts[i].Prompt, splitTestParts[1-i].Prompt
		if !strings.Contains(typed, own) || !strings.Contains(typed, "add dark mode") || strings.Contains(typed, other) {
			t.Errorf("lane %d typed %q", i, typed)
		}
	}
	l0, l1 := e.race(t).Lanes[0], e.race(t).Lanes[1]
	e.act(raceview.ActMerge, l0.ID)
	if e.m.confirmOpen {
		t.Fatalf("merging a part opened a card (%v)", e.m.confirmKind)
	}
	if r := e.race(t); r.Lanes[0].State != agentrace.Merged || r.Winner != "" || r.Lanes[1].State != agentrace.Done {
		t.Errorf("after the first part: %s winner %q, lane 1 %s", r.Lanes[0].State, r.Winner, r.Lanes[1].State)
	}
	e.act(raceview.ActMerge, l1.ID)
	for _, f := range []string{"theme.txt", "toggle.txt"} {
		if _, err := os.Stat(filepath.Join(e.dir, f)); err != nil {
			t.Errorf("%s not merged: %v", f, err)
		}
	}
	if r := e.race(t); r.Lanes[1].State != agentrace.Merged {
		t.Errorf("lane 1 %s", r.Lanes[1].State)
	}
	if rs, _ := agentrace.Load(filepath.Join(e.dir, ".git", "termocode")); len(rs) != 1 || rs[0].Kind != agentrace.KindSplit || rs[0].Lanes[1].Prompt != "the settings toggle" {
		t.Errorf("records %+v", rs)
	}
}

// Spec (CMP-4): agent_split_status gives the coordinator the newest split
// (or the one named): each lane's state, part and files; with a lane it
// adds that finished lane's diff (redacted, cut). A lane that has not
// finished has no diff yet; no split is said plainly.
func TestAIAgentSplitStatus(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	if _, err := e.m.aiSplitStatus("", ""); err == nil || !strings.Contains(err.Error(), "no split") {
		t.Errorf("no split: %v", err)
	}
	e.feed(e.m.startRace("a plain race", "", []string{"claude", "codex"}))
	e.feed(e.m.startSplit("add dark mode", "", splitTestParts))
	e.finishLane(t, 0, map[string]string{"theme.txt": "dark\n"})
	r := e.race(t)
	out, err := e.m.aiSplitStatus("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{r.ID, "add dark mode", "done", "starting", "the theme tokens", "theme.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("status has no %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "a plain race") {
		t.Errorf("status shows the race:\n%s", out)
	}
	out, err = e.m.aiSplitStatus(r.ID, r.Lanes[0].ID)
	if err != nil || !strings.Contains(out, "+dark") {
		t.Errorf("lane diff %v:\n%s", err, out)
	}
	if _, err := e.m.aiSplitStatus(r.ID, r.Lanes[1].ID); err == nil || !strings.Contains(err.Error(), "still") {
		t.Errorf("diff of a working lane: %v", err)
	}
	if _, err := e.m.aiSplitStatus("race-1", ""); err == nil {
		t.Error("a plain race was read as a split")
	}
}

// Spec (CMP-4): F1 → Agent: Split Task… opens the AI chat with a short
// draft to finish (the AI then proposes the parts on a card); text the
// user already typed stays. The AI never opens it (it has the tool).
func TestAgentSplitPalette(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	if p, ok := aiActionPolicyFor("agent-split"); !ok || p.Gate != aiGateNever {
		t.Errorf("agent-split policy %+v %v", p, ok)
	}
	titles := map[string]string{}
	for _, it := range tasksPaletteItems() {
		titles[it.ID] = it.Title
	}
	if titles["agent-split"] != "Agent: Split Task…" {
		t.Errorf("palette row %q", titles["agent-split"])
	}
	if _, ok := e.m.dispatchTasksPalette("agent-split"); !ok {
		t.Fatal("not dispatched")
	}
	if s := e.m.aiS(); !s.focused || string(s.input) != agentSplitDraft {
		t.Errorf("chat focused %v input %q", s.focused, string(s.input))
	}
	e.m.aiSetInput("my own words")
	e.m.dispatchTasksPalette("agent-split")
	if got := string(e.m.aiS().input); got != "my own words" {
		t.Errorf("the draft replaced the user's text: %q", got)
	}
}
