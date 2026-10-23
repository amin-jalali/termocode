package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
)

// U20 AP-9: the AGENTS panel, a view of the chat's agent runs in the
// chat slot: node cards per wave with connectors, the dependency list,
// the selected node's details, follow mode and a run switcher.

// agentsDiamondSteps is a planned run in the middle of its third wave:
// the planner and Map are done, Left and Right run, Verify waits.
func agentsDiamondSteps() []ai.StoredStep {
	u := &ai.Usage{Model: "gpt-5-mini", Input: 3000, Output: 400, Requests: 2, PeakContext: 3000}
	cost := 0.004
	return []ai.StoredStep{
		{ID: "planner", Title: "Planner", Model: "claude-sonnet-4-5", Category: "deep", State: "completed", ElapsedMs: 4000,
			Usage: &ai.Usage{Requests: 1, Input: 900, Output: 300}},
		{ID: "map", Title: "Map", Model: "gpt-5-mini", Category: "quick", State: "completed", Planned: true, Deps: []string{"planner"},
			ElapsedMs: 21000, Usage: u, Cost: &cost, Tools: []string{"⚙ read_file a.go → 10 lines"}, Text: "The map is done."},
		{ID: "left", Title: "Left", Model: "gpt-5-mini", Category: "quick", State: "running", Planned: true, Deps: []string{"map"},
			Tools: []string{"⚙ read_file nav.go"}},
		{ID: "right", Title: "Right", Model: "gpt-5-mini", Category: "quick", State: "running", Planned: true, Deps: []string{"map"},
			Text: "I compare the gates with the scoped wiring"},
		{ID: "verify", Title: "Verify", Model: "claude-sonnet-4-5", Category: "deep", State: "pending", Planned: true, Deps: []string{"left", "right"}},
	}
}

// agentsModel is a chat with the given messages and the AGENTS view
// shown and focused.
func agentsModel(t *testing.T, msgs ...ai.StoredMessage) *Model {
	t.Helper()
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	m.ai.conv.Messages = msgs
	m.actionsOpen, m.actionsPinned = true, true
	m.ai.focused = true
	m.ai.agents.on = true
	return m
}

func agentsRunMsgs(id, state string, steps []ai.StoredStep) []ai.StoredMessage {
	return []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "make the parser faster"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: id, Profile: ai.ProfilePlan, State: state}, Steps: steps},
	}
}

// agentsText draws the chat slot (header included) as plain text.
func agentsText(m *Model, width, height int) string {
	return ansi.Strip(strings.Join(m.renderActionsPanelLines(width, height), "\n"))
}

func agentsUnicode(t *testing.T) {
	t.Helper()
	prev := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = prev })
}

func wantAll(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if strings.HasPrefix(w, "!") {
			if strings.Contains(got, w[1:]) {
				t.Errorf("unexpected %q in:\n%s", w[1:], got)
			}
			continue
		}
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func (m *Model) agentsKey(t *testing.T, key string) tea.Cmd {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	cmd, ok := m.routeAIPanelKey(msg)
	if !ok {
		t.Fatalf("key %q not handled by the AGENTS view", key)
	}
	return cmd
}

// Spec (§2.4): the panel shows the run, "wave i/n · done k/m", one card
// per node in wave rows with connectors, the dependency list and the
// details of the running node it follows.
func TestAgentsPanelDiamondRunning(t *testing.T) {
	agentsUnicode(t)
	m := agentsModel(t, agentsRunMsgs("r1", ai.RunStateRunning, agentsDiamondSteps())...)
	got := agentsText(m, 64, 60)
	wantAll(t, got,
		"AGENTS", "run r1 · plan", "wave 3/4 · done 2/5", "following",
		"Planner", "Map", "Left", "Right", "Verify",
		"● running · quick", "○ pending · deep", "← Left, Right", "▶ read_file nav.go", "✎ now",
		"✓ done", "1 turn", "2 turns",
		"▼", "├", "┘", "Dependencies", "Planner → Map", "Map     → Left", "Left    → Verify", "Right   → Verify",
		"Node details · Left", "quick · gpt-5-mini",
		"Tab next", "!AI CHAT")
	// Left and Right are one wave: their cards share rows.
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "Left") && strings.Contains(l, "┌") {
			if !strings.Contains(l, "Right") {
				t.Errorf("Left and Right are not side by side: %q", l)
			}
		}
	}
}

// Spec: a narrow slot draws one column of cards; ASCII mode draws no
// box, arrow or state glyphs.
func TestAgentsPanelNarrowAndASCII(t *testing.T) {
	agentsUnicode(t)
	m := agentsModel(t, agentsRunMsgs("r1", ai.RunStateRunning, agentsDiamondSteps())...)
	got := agentsText(m, 40, 80)
	col := -1
	for _, l := range strings.Split(got, "\n") {
		if strings.Count(l, "┌") > 1 {
			t.Errorf("two cards in one row at width 40: %q", l)
		}
		if i := strings.Index(l, "┌"); i >= 0 {
			if col >= 0 && i != col {
				t.Errorf("cards not in one column: %q", l)
			}
			col = i
		}
	}
	wantAll(t, got, "Left", "Right", "Verify", "wave 3/4")

	theme.CurrentIconMode = theme.IconModeASCII
	m.ai.runGen++
	// The slot's own header and divider (shared with the chat) are not
	// part of the panel.
	got = strings.Join(strings.Split(agentsText(m, 64, 60), "\n")[2:], "\n")
	for _, bad := range []string{"┌", "┐", "└", "┘", "│", "─", "┬", "┴", "▼", "●", "○", "✓", "✘", "→", "←", "▾", "▸", "▶", "✎"} {
		if strings.Contains(got, bad) {
			t.Errorf("ASCII mode draws %q:\n%s", bad, got)
		}
	}
	wantAll(t, got, "Planner -> Map", "+-", "v", "* running")
	wantAll(t, agentsText(m, 64, 60), "* AGENTS", "< chat")
}

// Spec: Tab / arrows move the selection and stop follow mode; f follows
// the running node again; Space / Enter fold the selected card; d hides
// the details; Esc goes back to the chat.
func TestAgentsPanelKeys(t *testing.T) {
	agentsUnicode(t)
	m := agentsModel(t, agentsRunMsgs("r1", ai.RunStateRunning, agentsDiamondSteps())...)
	wantAll(t, agentsText(m, 64, 60), "Node details · Left", "following")

	m.agentsKey(t, "tab")
	wantAll(t, agentsText(m, 64, 60), "Node details · Right", "!following")
	m.agentsKey(t, "down")
	wantAll(t, agentsText(m, 64, 60), "Node details · Verify")
	m.agentsKey(t, "shift+tab")
	wantAll(t, agentsText(m, 64, 60), "Node details · Right")

	m.agentsKey(t, "f")
	wantAll(t, agentsText(m, 64, 60), "Node details · Left", "following")

	// Left is expanded while it runs; Space folds it (one body row).
	if got := agentsText(m, 64, 60); !strings.Contains(got, "▶ read_file nav.go") {
		t.Fatalf("Left not expanded:\n%s", got)
	}
	m.agentsKey(t, " ")
	if !m.ai.conv.Folded[aiCardID("r1", "left")] {
		t.Fatalf("fold not kept: %v", m.ai.conv.Folded)
	}
	if got := agentsText(m, 64, 60); strings.Contains(got, "▶ read_file nav.go") {
		t.Errorf("Left still expanded after Space:\n%s", got)
	}
	m.agentsKey(t, "enter")
	if got := agentsText(m, 64, 60); !strings.Contains(got, "▶ read_file nav.go") {
		t.Errorf("Enter did not unfold Left:\n%s", got)
	}

	m.agentsKey(t, "d")
	wantAll(t, agentsText(m, 64, 60), "!Node details")
	m.agentsKey(t, "d")
	wantAll(t, agentsText(m, 64, 60), "Node details · Left")

	m.agentsKey(t, "esc")
	if m.ai.agents.on {
		t.Fatal("Esc did not go back to the chat")
	}
	wantAll(t, agentsText(m, 64, 60), "AI CHAT", "!Dependencies")
}

// Spec: ← / → switch between this chat's runs (the newest first).
func TestAgentsPanelRunSwitcher(t *testing.T) {
	agentsUnicode(t)
	done := agentsDiamondSteps()
	for i := range done {
		done[i].State = "completed"
	}
	msgs := append(agentsRunMsgs("r1", ai.RunStateDone, done), agentsRunMsgs("r2", ai.RunStateRunning, agentsDiamondSteps())...)
	m := agentsModel(t, msgs...)
	wantAll(t, agentsText(m, 64, 60), "run r2 · plan (2/2)", "wave 3/4 · done 2/5")
	m.agentsKey(t, "left")
	wantAll(t, agentsText(m, 64, 60), "run r1 · plan (1/2)", "wave 4/4 · done 5/5", "✓ run")
	m.agentsKey(t, "left") // stays on the first
	wantAll(t, agentsText(m, 64, 60), "run r1 · plan (1/2)")
	m.agentsKey(t, "right")
	wantAll(t, agentsText(m, 64, 60), "run r2 · plan (2/2)")
}

// Spec: with no runs in the chat the panel says how to start one.
func TestAgentsPanelEmpty(t *testing.T) {
	agentsUnicode(t)
	m := agentsModel(t)
	wantAll(t, agentsText(m, 64, 30), "AGENTS", "No agent runs in this chat yet")
}

// Spec (§3.5): after a restart the panel replays the run's last
// snapshot: the states at the last step boundary, marked as such.
func TestAgentsPanelSnapshotReplay(t *testing.T) {
	agentsUnicode(t)
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	steps := agentsDiamondSteps()
	run := agentdag.Run{ID: "r7", Profile: ai.ProfilePlan}
	for _, st := range steps {
		run.Nodes = append(run.Nodes, agentdag.Node{ID: st.ID, Title: st.Title, Category: st.Category, State: agentdag.State(st.State)})
		for _, d := range st.Deps {
			run.Edges = append(run.Edges, agentdag.Edge{From: d, To: st.ID})
		}
	}
	aiSaveRunSnapshot("chat", ai.RunStateRunning, run)

	c := ai.NewConversation()
	c.Messages = agentsRunMsgs("r7", ai.RunStateRunning, steps)
	if err := m.ai.store.Save(&c); err != nil {
		t.Fatal(err)
	}
	m.onAIHistoryPicked(c.ID) // marks the run interrupted
	m.actionsOpen, m.actionsPinned = true, true
	cmd := m.toggleAgentsView()
	if !m.ai.agents.on {
		t.Fatal("AGENTS view not shown")
	}
	if cmd == nil {
		t.Fatal("no snapshot load for an interrupted run")
	}
	m.handleAIMsg(cmd())
	got := agentsText(m, 64, 60)
	wantAll(t, got, "run r7", "interrupted", "last snapshot 09:05", "was running", "○ pending", "Left    → Verify")
}

// Spec: the chat's run row of a running planned run says its wave and
// progress; a plain two-step pipeline has no wave text.
func TestAgentsChatRunRowWaves(t *testing.T) {
	agentsUnicode(t)
	m := agentsModel(t, agentsRunMsgs("r1", ai.RunStateRunning, agentsDiamondSteps())...)
	m.ai.agents.on = false
	wantAll(t, aiTranscriptText(m, 90), "wave 3/4 · done 2/5 · 2 running")

	chain := []ai.StoredStep{{ID: "reviewer", Title: "Reviewer", State: "completed"}, {ID: "fixer", Title: "Fixer", State: "running"}}
	m.ai.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "go"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r2", Profile: "review", State: ai.RunStateRunning}, Steps: chain}}
	wantAll(t, aiTranscriptText(m, 90), "!wave")

	// A click on the planned run's row opens it in the AGENTS view.
	m.ai.conv.Messages = agentsRunMsgs("r1", ai.RunStateRunning, agentsDiamondSteps())
	m.onAIButton("agents:r1")
	if !m.ai.agents.on || m.ai.agents.runID != "r1" {
		t.Fatalf("agents %+v", m.ai.agents)
	}
}

// Spec (F5): a real planned diamond run, when done, shows every node
// done in its wave with turns from its usage.
func TestAgentsPanelLiveDiamond(t *testing.T) {
	agentsUnicode(t)
	p := &funcAIProvider{reply: planReply(appDiamondPlan)}
	m := aiPlanModel(t, p)
	m.actionsOpen, m.actionsPinned = true, true
	driveAIChat(t, m, m.sendAIChat("make the parser faster"), nil)
	m.toggleAgentsView()
	got := agentsText(m, 64, 70)
	wantAll(t, got, "wave 4/4 · done 5/5", "Planner", "Map", "Left", "Right", "Verify", "Map     → Left", "✓ run")
}

// Spec: Alt+Shift+A shows the AGENTS view in the chat slot and switches
// it with the chat; Alt+A then shows the chat; the palette has
// "ai-agents".
func TestAgentsToggleKeys(t *testing.T) {
	km := keymap.Default()
	for _, k := range []string{"alt+A", "alt+shift+a"} {
		if a := km.Bindings()[k]; a != keymap.ActionAIToggleAgents {
			t.Errorf("%s → %v", k, a)
		}
	}
	found := false
	for _, e := range aiPaletteItems() {
		found = found || e.id == "ai-agents"
	}
	if !found {
		t.Error("no ai-agents palette entry")
	}
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	m.toggleAgentsView()
	if !m.actionsPanelVisible() || !m.ai.agents.on || !m.ai.focused {
		t.Fatal("Alt+Shift+A did not open the AGENTS view")
	}
	m.toggleAgentsView()
	if !m.actionsPanelVisible() || m.ai.agents.on {
		t.Fatal("Alt+Shift+A did not switch to the chat")
	}
	m.toggleAgentsView()
	m.toggleActionsPanel()
	if !m.actionsPanelVisible() || m.ai.agents.on {
		t.Fatal("Alt+A in the AGENTS view did not show the chat")
	}
	m.toggleActionsPanel()
	if m.actionsPanelVisible() {
		t.Fatal("Alt+A in the chat did not close the panel")
	}
}

// Spec: the snapshot file a replay reads is the one the run wrote.
func TestAgentsLoadSnapshot(t *testing.T) {
	_, _ = aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	r := agentdag.Run{ID: "x/1", Nodes: []agentdag.Node{{ID: "a", State: agentdag.StateRunning}}}
	aiSaveRunSnapshot("c", ai.RunStateRunning, r)
	snap := aiLoadRunSnapshot("x/1")
	if snap == nil || snap.Run.ID != "x/1" || snap.Run.Nodes[0].State != agentdag.StateRunning {
		t.Fatalf("snapshot %+v", snap)
	}
	if aiLoadRunSnapshot("nope") != nil {
		t.Error("a missing snapshot loaded")
	}
	dir, _ := aiRunsDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o600)
	if aiLoadRunSnapshot("bad") != nil {
		t.Error("a broken snapshot loaded")
	}
	b, _ := json.Marshal(snap)
	if strings.Contains(string(b), "prompt") {
		t.Errorf("snapshot has a prompt: %s", b)
	}
}

// Spec: a click on a card selects it (follow mode stops); the header
// chip goes back to the chat.
func TestAgentsPanelMouse(t *testing.T) {
	agentsUnicode(t)
	m := agentsModel(t, agentsRunMsgs("r1", ai.RunStateRunning, agentsDiamondSteps())...)
	w := m.actionsColumnWidth()
	_ = m.renderActionsPanelLines(w, m.h)
	x0 := m.w - w
	var hit *agentsHit
	for i, h := range m.ai.agents.cache.hits {
		if h.id == "verify" {
			hit = &m.ai.agents.cache.hits[i]
		}
	}
	if hit == nil {
		t.Fatal("no card hit for verify")
	}
	y := hit.y0 - m.ai.agents.scroll + aiPanelTopRow + agentsHeadH + 1
	if id := m.aiPanelTargetAt(x0+hit.x0+2, y); id != "ai:agents:verify" {
		t.Fatalf("target %q", id)
	}
	m.handleAIPanelMouse(tea.MouseMsg{X: x0 + hit.x0 + 2, Y: y, Type: tea.MouseLeft, Action: tea.MouseActionPress})
	if m.ai.agents.sel != "verify" || !m.ai.agents.manual {
		t.Fatalf("agents %+v", m.ai.agents)
	}
	for x := x0; x < m.w; x++ {
		if m.aiPanelTargetAt(x, 0) == "ai:chat" {
			m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 0, Type: tea.MouseLeft, Action: tea.MouseActionPress})
			if m.ai.agents.on {
				t.Fatal("the chat chip did not go back to the chat")
			}
			return
		}
	}
	t.Fatal("no chat chip in the header")
}

// agentsCardRows draws node id of the shown run as plain rows, w wide.
func agentsCardRows(t *testing.T, m *Model, id string, w int, now time.Time) []string {
	t.Helper()
	d, ok := m.ai.agentsData()
	if !ok {
		t.Fatal("no run")
	}
	for _, n := range d.nodes {
		if n.id == id {
			rows := m.ai.agentsCard(d, n, w, false, now, agentsGlyphs())
			for i := range rows {
				rows[i] = ansi.Strip(rows[i])
				if got := ansi.StringWidth(rows[i]); got != w {
					t.Errorf("card %s at %d: row %d is %d cells: %q", id, w, i, got, rows[i])
				}
			}
			return rows
		}
	}
	t.Fatalf("no node %q", id)
	return nil
}

// Spec (AU-3): a finished card shows its model, tokens and cost under the
// status row; the least important part goes first when the card is
// narrow.
func TestAgentsCardShowsModelTokensCost(t *testing.T) {
	agentsUnicode(t)
	m := agentsModel(t, agentsRunMsgs("r1", "running", agentsDiamondSteps())...)
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	// Folded (done): status row, then the model line.
	rows := strings.Join(agentsCardRows(t, m, "map", 36, now), "\n")
	wantAll(t, rows, "✓ done · 21 s · 2 turns · 1 tool", "gpt-5-mini · 3.4k tok · $0.004")
	// Narrow: the tokens go, model and cost stay.
	rows = strings.Join(agentsCardRows(t, m, "map", 23, now), "\n")
	wantAll(t, rows, "gpt-5-mini · $0.004", "!tok")
	// Very narrow: only what fits.
	rows = strings.Join(agentsCardRows(t, m, "map", 14, now), "\n")
	wantAll(t, rows, "!tok")
	// Unknown price: no cost, the rest stays.
	rows = strings.Join(agentsCardRows(t, m, "planner", 36, now), "\n")
	wantAll(t, rows, "claude-sonnet-4-5 · 1.2k tok", "!$")
	// Unfolded: the same line is the second row, the stats the third.
	m.ai.conv.Folded = map[string]bool{aiCardID("r1", "map"): false}
	got := agentsCardRows(t, m, "map", 36, now)
	if len(got) != 5 || !strings.Contains(got[2], "gpt-5-mini · 3.4k tok · $0.004") || !strings.Contains(got[3], "21 s · 2 turns · 1 tool") {
		t.Errorf("unfolded card:\n%s", strings.Join(got, "\n"))
	}
}

// Spec (AU-3): a running card shows its model and the live time.
func TestAgentsCardRunningShowsModelAndTime(t *testing.T) {
	agentsUnicode(t)
	m := agentsModel(t, agentsRunMsgs("r1", "running", agentsDiamondSteps())...)
	start := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	m.ai.agentsNodeStart("r1", "left", start)
	rows := strings.Join(agentsCardRows(t, m, "left", 36, start.Add(12*time.Second)), "\n")
	wantAll(t, rows, "gpt-5-mini · 12 s", "1 tool")
	// Narrow: the model is kept before the tools, the time stays.
	rows = strings.Join(agentsCardRows(t, m, "left", 22, start.Add(12*time.Second)), "\n")
	wantAll(t, rows, "gpt-5-mini · 12 s")
	rows = strings.Join(agentsCardRows(t, m, "left", 16, start.Add(12*time.Second)), "\n")
	wantAll(t, rows, "12 s")
}
