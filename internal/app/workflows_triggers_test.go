package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/trigger"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// CMP-38: event triggers. A trusted, on workflow with "on" gets its
// github triggers polled and its webhooks served; an event shows a
// Cancel-first card (or, with "ask": false, starts the run like a
// schedule); the run gets the event as trigger_* vars.

const wfxTriggered = `{
  "version": 1, "name": "%NAME%", "title": "Fix labeled issues",
  "on": [ { "github": "issue_labeled", "labels": ["agent"]%ASK% }, { "webhook": "%NAME%-hook", "from": "slack" } ],
  "steps": [ { "id": "docs", "type": "source", "from": "files", "glob": "docs/**/*.md" } ]
}`

func wfxAddTriggered(t *testing.T, m *Model, name string, ask bool, on bool) {
	t.Helper()
	a := ""
	if !ask {
		a = `, "ask": false`
	}
	body := strings.NewReplacer("%NAME%", name, "%ASK%", a).Replace(wfxTriggered)
	dir := wfxPrivateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".jsonc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m.wfxLoad("")
	e, ok := wire.Find(m.wfxS().list, name)
	if !ok || e.Broken() {
		t.Fatalf("%s: %+v", name, e.Problems)
	}
	if err := ledger.SaveState(m.wfxS().root, name, ledger.WorkflowState{Enabled: on}); err != nil {
		t.Fatal(err)
	}
	m.wfxLoad("")
}

func wfxTrigEvent(name string) trigger.Event {
	return trigger.Event{Workflow: name, Trigger: 0, Kind: trigger.KindIssueLabeled, Key: "labeled:12:agent", Source: "github",
		Number: 12, Title: "Login \x1b[31mbreaks", URL: "https://example.test/12", Author: "ann", Labels: []string{"agent"}}
}

func TestWorkflowTriggersWanted(t *testing.T) {
	m, _ := wfxRig(t)
	wfxAddTriggered(t, m, "fixer", true, true)
	wfxAddTriggered(t, m, "sleepy", true, false)
	jobs, hooks := m.wfxTrigWanted()
	if len(jobs) != 1 || jobs[0].workflow != "fixer" || jobs[0].t.GitHub != "issue_labeled" {
		t.Errorf("jobs %+v", jobs)
	}
	if h, ok := hooks["fixer-hook"]; !ok || h.Workflow != "fixer" || h.Trigger != 1 || h.From != "slack" {
		t.Errorf("hooks %+v", hooks)
	}
	if _, ok := hooks["sleepy-hook"]; ok {
		t.Error("an off workflow serves its hook")
	}
}

// The card is Cancel-first and shows the outside text cleaned; Cancel
// starts nothing; Start runs with the trigger vars.
func TestWorkflowTriggerAskCard(t *testing.T) {
	m, _ := wfxRig(t)
	wfxAddTriggered(t, m, "fixer", true, true)
	m.handleWfxMsg(wfxTrigEventMsg{ev: wfxTrigEvent("fixer")})
	if !m.confirmOpen || m.confirmKind != confirmKindWorkflowTrigger || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	view := m.confirm.View()
	if strings.Contains(view, "\x1b[31mbreaks") || !strings.Contains(view, "#12") {
		t.Errorf("card:\n%s", view)
	}
	m.answerWorkflowTrigger("cancel")
	if len(m.wfxS().live) != 0 {
		t.Fatal("Cancel started a run")
	}
	if r := m.wfxS().trigRecent; len(r) != 1 || r[0].outcome != "skipped" {
		t.Errorf("recent %+v", r)
	}
	ev := wfxTrigEvent("fixer")
	ev.Key = "labeled:13:agent"
	m.handleWfxMsg(wfxTrigEventMsg{ev: ev})
	m.answerWorkflowTrigger("start")
	var id string
	for k := range m.wfxS().live {
		id = k
	}
	if id == "" {
		t.Fatal("Start started no run")
	}
	rf := wfxPump(t, m, id, nil)
	if rf.State != run.RunOK || rf.Vars["trigger_key"] != "labeled:13:agent" || rf.Vars["trigger_number"] != "12" {
		t.Errorf("run %s %v", rf.State, rf.Vars)
	}
}

// "ask": false starts the run with no card.
func TestWorkflowTriggerNoAsk(t *testing.T) {
	m, _ := wfxRig(t)
	wfxAddTriggered(t, m, "auto", false, true)
	m.handleWfxMsg(wfxTrigEventMsg{ev: wfxTrigEvent("auto")})
	if m.confirmOpen {
		t.Fatal("a card for ask: false")
	}
	if len(m.wfxS().live) != 1 {
		t.Fatalf("live %d", len(m.wfxS().live))
	}
	for id, l := range m.wfxS().live {
		if !l.scheduled {
			t.Error("not marked unattended")
		}
		wfxPump(t, m, id, nil)
	}
}

// An event for a workflow that is off (or gone) starts nothing.
func TestWorkflowTriggerOffIgnored(t *testing.T) {
	m, _ := wfxRig(t)
	wfxAddTriggered(t, m, "sleepy", false, false)
	m.handleWfxMsg(wfxTrigEventMsg{ev: wfxTrigEvent("sleepy")})
	m.handleWfxMsg(wfxTrigEventMsg{ev: wfxTrigEvent("gone")})
	if m.confirmOpen || len(m.wfxS().live) != 0 {
		t.Fatal("an off workflow ran")
	}
	if r := m.wfxS().trigRecent; len(r) != 2 || !strings.HasPrefix(r[0].outcome, "ignored") {
		t.Errorf("recent %+v", r)
	}
}

// Two events wait in line: the second card comes after the first answer.
func TestWorkflowTriggerQueue(t *testing.T) {
	m, _ := wfxRig(t)
	wfxAddTriggered(t, m, "fixer", true, true)
	a, b := wfxTrigEvent("fixer"), wfxTrigEvent("fixer")
	b.Key, b.Number = "labeled:14:agent", 14
	m.handleWfxMsg(wfxTrigEventMsg{ev: a})
	m.handleWfxMsg(wfxTrigEventMsg{ev: b})
	if len(m.wfxS().trigQueue) != 2 {
		t.Fatalf("queue %d", len(m.wfxS().trigQueue))
	}
	m.confirmOpen = false
	m.answerWorkflowTrigger("cancel")
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "#14") {
		t.Errorf("the next card is not shown:\n%s", m.confirm.View())
	}
}

func TestWorkflowTriggerPaletteAndSettings(t *testing.T) {
	found := false
	for _, it := range workflowsPaletteItems() {
		found = found || it.ID == "workflow-triggers"
	}
	if !found {
		t.Error("no workflow-triggers palette item")
	}
	if _, ok := workflowSettingRowByID("setting-workflows-webhook-port"); !ok {
		t.Error("no webhook port setting")
	}
	if _, ok := workflowSettingRowByID("setting-workflows-trigger-poll"); !ok {
		t.Error("no trigger poll setting")
	}
}
