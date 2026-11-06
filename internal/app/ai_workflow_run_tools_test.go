package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// WFX-9: workflow_dry_run and workflow_run ask on a card every time,
// Deny first; a repo file the user has not trusted never gets a card.

// Spec: both cards put Deny first (with the focus) and name the action;
// neither has an Always allow class.
func TestAIWorkflowRunToolsGates(t *testing.T) {
	m, ws := wfxRig(t)
	tools := m.aiChatTools(ws, nil)
	for name, want := range map[string]string{"workflow_dry_run": "Deny · Dry run", "workflow_run": "Deny · Run"} {
		tl, ok := ai.FindTool(tools, name)
		if !ok {
			t.Errorf("no tool %s", name)
			continue
		}
		if !tl.Mutating || tl.ReadOnly || tl.Class != "" {
			t.Errorf("%s: mutating %v read-only %v class %q", name, tl.Mutating, tl.ReadOnly, tl.Class)
		}
		reply := make(chan bool, 1)
		m.onAIChatConfirm(aiChatConfirmMsg{gen: m.ai.chatGen, tool: tl, human: "x", reply: reply, ch: make(chan tea.Msg, 1)})
		var titles []string
		for _, b := range m.confirm.Buttons() {
			titles = append(titles, b.Title)
		}
		if got := strings.Join(titles, " · "); got != want {
			t.Errorf("%s buttons %q, want %q", name, got, want)
		}
		m.answerAIToolConfirm("deny")
		if <-reply {
			t.Errorf("%s: Deny allowed", name)
		}
	}
}

// Spec (plan §4.5): the dry-run card says what it reads and what the plan
// steps cost; Deny starts nothing; Dry run waits for the dry run and
// tells the model what it would write (nothing is written).
func TestAIWorkflowDryRunTool(t *testing.T) {
	m, ws := wfxDraftRig(t)
	res, card := aiWriteTurn(t, m, "workflow_dry_run", map[string]any{"name": "mine"}, "deny")
	for _, want := range []string{"Dry run mine · Docs report", "reads 1 source(s) for real", "Plan cost: none", "never sent"} {
		if !strings.Contains(card.body, want) {
			t.Errorf("card misses %q:\n%s", want, card.body)
		}
	}
	if len(m.wfxS().live) != 0 || strings.Contains(res, "Dry run finished") {
		t.Fatalf("Deny started a dry run: %q", res)
	}
	res, _ = aiWriteTurn(t, m, "workflow_dry_run", map[string]any{"name": "mine"}, "allow")
	for _, want := range []string{"Dry run finished (nothing was written)", "· mine · dry run · done", "rep → file reports/docs.md (create): recorded · 2 docs"} {
		if !strings.Contains(res, want) {
			t.Errorf("result misses %q:\n%s", want, res)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, "reports", "docs.md")); !os.IsNotExist(err) {
		t.Error("a dry run wrote the file")
	}
}

// Spec: the plan cost line counts the plan steps and names their model.
func TestAIWorkflowPlanCost(t *testing.T) {
	m, ws := wfxRig(t)
	body := `{ "version": 1, "name": "planned", "limits": { "max_items": 7 }, "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/**/*.md" },
    { "id": "order", "type": "plan", "model": "deep" } ] }`
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "workflows")
	if err := os.WriteFile(filepath.Join(dir, "planned.jsonc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tl, _ := ai.FindTool(m.aiChatTools(ws, nil), "workflow_dry_run")
	got := tl.Describe(map[string]any{"name": "planned"})
	if !strings.Contains(got, "Plan cost: 1 plan step(s) call the AI once each (order: deep), with up to 7 item(s) per call. This costs AI money") {
		t.Errorf("card:\n%s", got)
	}
}

// Spec (§6.1): a repo file the user has not trusted, or a broken one,
// never reaches a card; the model is told the user trusts it in the view.
func TestAIWorkflowRunRefusesUntrusted(t *testing.T) {
	m, _ := wfxDraftRig(t)
	for _, tool := range []string{"workflow_dry_run", "workflow_run"} {
		res, card := aiWriteTurn(t, m, tool, map[string]any{"name": "docs-report"}, "allow")
		if card.opened || !strings.Contains(res, "not trusted yet") || !strings.Contains(res, "Workflows view") {
			t.Errorf("%s: card %v result %q", tool, card.opened, res)
		}
		res, card = aiWriteTurn(t, m, tool, map[string]any{"name": "broken"}, "allow")
		if card.opened || !strings.Contains(res, "problems") {
			t.Errorf("%s broken: card %v result %q", tool, card.opened, res)
		}
	}
	if len(m.wfxS().live) != 0 {
		t.Error("a run started")
	}
}

// Spec: the run card lists the writes; Run starts the run and answers at
// once; the write then asks on its gate card, and Deny there is what the
// run file records.
func TestAIWorkflowRunTool(t *testing.T) {
	m, ws := wfxDraftRig(t)
	res, card := aiWriteTurn(t, m, "workflow_run", map[string]any{"name": "mine"}, "allow")
	for _, want := range []string{"Run mine now · Docs report", "may write 1 time(s)", "• rep → file reports/docs.md (create) · asks each time", "Deny first"} {
		if !strings.Contains(card.body, want) {
			t.Errorf("card misses %q:\n%s", want, card.body)
		}
	}
	if !strings.Contains(res, "started") {
		t.Fatalf("result %q", res)
	}
	id := wfxOnlyLive(t, m)
	if !strings.Contains(res, id) {
		t.Errorf("result %q has no run id %s", res, id)
	}
	gates := 0
	rf := wfxPump(t, m, id, func(m *Model) { gates++; m.answerWorkflowGate("deny"); m.confirmOpen = false })
	if gates != 1 || len(rf.Writes) != 1 || rf.Writes[0].State != run.StateDenied {
		t.Fatalf("gates %d writes %+v", gates, rf.Writes)
	}
	if _, err := os.Stat(filepath.Join(ws, "reports", "docs.md")); !os.IsNotExist(err) {
		t.Error("a denied write was written")
	}
	root := m.wfxS().root
	saved, err := wire.RunFiles{Root: root}.Load(id)
	if err != nil || saved.Writes[0].State != run.StateDenied {
		t.Errorf("the run file: %v %+v", err, saved)
	}
}
