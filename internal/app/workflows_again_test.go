package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// CMP-45: the run page has a tab per step ([ and ] pick one; its detail
// shows state, tries, error, output and items), a failed run can run
// again from its failed part (t, behind a Cancel-first card), and a plan
// step with "approve": true asks on a card before its order is used.

// wfxFailedRun puts an ended run of mine whose write was denied.
func wfxFailedRun(t *testing.T, m *Model) *run.RunFile {
	t.Helper()
	e, ok := wire.Find(m.wfxS().list, "mine")
	if !ok {
		t.Fatal("no mine")
	}
	rf := &run.RunFile{Version: run.RunFileVersion, ID: "20261106-094000-aaaa", Workflow: "mine", Hash: e.Hash, State: run.RunOK,
		StartedAt: wfxTestNow.Add(-time.Minute), EndedAt: wfxTestNow,
		Steps: map[string]*run.StepState{
			"docs": {State: run.StateDone, Items: []run.Item{{Key: "docs/a.md"}, {Key: "docs/b.md"}}, Output: &run.Output{Text: "two files"}},
			"rep":  {State: run.StateDenied, Attempts: 1, Error: "write rep: denied by you"},
		}}
	m.wfxPutRun(rf)
	st := m.wfxS()
	st.sel, st.detail, st.pageOn = "mine", "run:"+rf.ID, true
	return rf
}

func TestWorkflowsStepTabs(t *testing.T) {
	m, _ := wfxRig(t)
	wfxFailedRun(t, m)
	page := wfxPage(m, 120, 40)
	if strings.Contains(page, "STEP docs") {
		t.Fatalf("a step tab is open before [ or ]:\n%s", page)
	}
	m.handleWorkflowsPageKey(wfxKey(']'))
	page = wfxPage(m, 120, 40)
	for _, want := range []string{"STEP docs", "source", "docs/a.md", "docs/b.md", "two files", "‹docs"} {
		if !strings.Contains(page, want) {
			t.Errorf("docs tab misses %q:\n%s", want, page)
		}
	}
	m.handleWorkflowsPageKey(wfxKey(']'))
	page = wfxPage(m, 120, 40)
	for _, want := range []string{"STEP rep", "denied by you", "‹rep"} {
		if !strings.Contains(page, want) {
			t.Errorf("rep tab misses %q:\n%s", want, page)
		}
	}
	m.handleWorkflowsPageKey(wfxKey(']')) // past the last: stays
	if !strings.Contains(wfxPage(m, 120, 40), "STEP rep") {
		t.Error("] past the last step left the tab")
	}
	m.handleWorkflowsPageKey(wfxKey('['))
	if !strings.Contains(wfxPage(m, 120, 40), "STEP docs") {
		t.Error("[ did not go back")
	}
	m.handleWorkflowsPageKey(wfxKey('['))
	if strings.Contains(wfxPage(m, 120, 40), "STEP docs") {
		t.Error("[ before the first step did not close the tab (all steps)")
	}
	found := false
	for _, r := range wfxKeyRowsList() {
		found = found || (len(r) == 2 && r[0] == "[ / ]")
	}
	if !found {
		t.Error("the key card has no [ / ]")
	}
}

func TestWorkflowsRetryCard(t *testing.T) {
	m, _ := wfxRig(t)
	st := m.wfxS()
	st.sel = "mine"
	if cmd, ok := m.wfxVerb("t"); !ok || cmd == nil || m.confirmOpen {
		t.Fatalf("t with no run: ok %v card %v, want a short note", ok, m.confirmOpen)
	}
	wfxFailedRun(t, m)
	if page := wfxPage(m, 120, 40); !strings.Contains(page, "[Retry (t)]") {
		t.Fatalf("the failed run's page offers no retry:\n%s", page)
	}
	m.wfxVerb("t")
	if !m.confirmOpen || m.confirmKind != confirmKindWorkflowRetry || m.confirm.SafeID() != "cancel" {
		t.Fatalf("retry card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	var titles []string
	for _, b := range m.confirm.Buttons() {
		titles = append(titles, b.Title)
	}
	if strings.Join(titles, "|") != "Cancel|Retry" {
		t.Fatalf("buttons %v", titles)
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"rep", "docs", "kept"} {
		if !strings.Contains(box, want) {
			t.Errorf("card misses %q:\n%s", want, box)
		}
	}
	m.answerWorkflowCard("cancel")
	if len(st.live) != 0 {
		t.Fatal("Cancel started a run")
	}
}

// End to end: a denied write, then t and Retry run the write again (the
// gate asks again) in the same run; the source is kept.
func TestWorkflowsRetryRunsAgain(t *testing.T) {
	m, ws := wfxRig(t)
	st := m.wfxS()
	st.sel = "mine"
	m.answerWorkflowCardKind(confirmKindWorkflowRun, "run")
	id := wfxOnlyLive(t, m)
	rf := wfxPump(t, m, id, func(m *Model) { m.answerWorkflowGate("deny"); m.confirmOpen = false })
	if rf.Steps["rep"].State != run.StateDenied {
		t.Fatalf("rep %q", rf.Steps["rep"].State)
	}
	st.detail, st.pageOn = "run:"+id, true
	m.wfxVerb("t")
	if m.confirmKind != confirmKindWorkflowRetry {
		t.Fatalf("no retry card: kind %v", m.confirmKind)
	}
	m.answerWorkflowCard("retry")
	if st.live[id] == nil {
		t.Fatalf("Retry did not continue run %s: live %v", id, st.live)
	}
	asked := 0
	end := wfxPump(t, m, id, func(m *Model) { asked++; m.answerWorkflowGate("once"); m.confirmOpen = false })
	if end.State != run.RunOK || end.Resumed != 1 || end.Steps["rep"].State != run.StateDone || asked != 1 {
		t.Fatalf("after retry %q resumed %d rep %q asked %d (%s)", end.State, end.Resumed, end.Steps["rep"].State, asked, end.Error)
	}
	if b, err := os.ReadFile(filepath.Join(ws, "reports", "docs.md")); err != nil || string(b) != "2 docs" {
		t.Fatalf("the retried write: %q %v", b, err)
	}
}

// A plan approval is a gate card of its own words: Stop here is first,
// then Approve plan; no "for this workflow" (each plan is new).
func TestWorkflowsPlanApprovalCard(t *testing.T) {
	m := aiTestModel(t)
	var got []WorkflowGateDecision
	m.AskWorkflowWrite(WorkflowWriteAsk{Workflow: "triage", Step: "order", Plan: true, FileHash: "h",
		Target: "plan step order · 3 items", Preview: "1. b\n2. a\nskip c: dup",
		Answer: func(d WorkflowGateDecision) { got = append(got, d) }})
	if !m.confirmOpen || m.confirmKind != confirmKindWorkflowGate || m.confirm.SafeID() != "deny" {
		t.Fatalf("open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	var titles []string
	for _, b := range m.confirm.Buttons() {
		titles = append(titles, b.Title)
	}
	if strings.Join(titles, "|") != "Stop here|Approve plan" {
		t.Fatalf("buttons %v", titles)
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"triage: approve the plan?", "order", "skip c: dup"} {
		if !strings.Contains(box, want) {
			t.Errorf("card misses %q:\n%s", want, box)
		}
	}
	if strings.Contains(box, "cannot be undone") {
		t.Errorf("a plan card talks about undo:\n%s", box)
	}
	card := webWorkflowCard(m.wfGate.cur, webproto.CardChoice{ID: webproto.ChoiceDeny, Label: "Deny"}, webproto.CardChoice{ID: webproto.ChoiceAllow, Label: "Allow"})
	if !strings.Contains(card.Title, "approve the plan") || strings.Contains(card.Body, "Allow for this workflow") || card.Choices[1].Label != "Approve plan" {
		t.Fatalf("web card %+v", card)
	}
	m.answerWorkflowGate("once")
	if len(got) != 1 || got[0] != WorkflowGateAllowOnce {
		t.Fatalf("answers %v", got)
	}
}

// The new palette ids have AI policies (TestWorkflowsPaletteIDs checks
// all) and the retry and pause ids are listed.
func TestWorkflowsRetryPauseInPalette(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range workflowsPaletteItems() {
		ids[it.ID] = true
	}
	for _, id := range []string{"workflow-retry", "workflow-pause"} {
		if !ids[id] {
			t.Errorf("%s is not in the palette", id)
		}
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("%s has no AI policy", id)
		}
	}
}
