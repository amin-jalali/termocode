package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// Workflows view (WFX-7): the list, the pages, the cards and the run
// plumbing, on a workspace in a temp folder.

const wfxSample = `{
  // a small sample: read the docs, write a report
  "version": 1, "name": "%s", "title": "Docs report",
  "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/**/*.md" },
    { "id": "rep", "type": "write", "to": "file", "path": "reports/docs.md", "content": "{{len(steps.docs.items)}} docs" }
  ]
}`

var wfxTestNow = time.Date(2026, 11, 6, 9, 41, 0, 0, time.UTC)

// wfxRig is a model with a workspace that has docs-report (repo), broken
// (repo, bad step) and mine (private).
func wfxRig(t *testing.T) (*Model, string) {
	t.Helper()
	m := aiTestModel(t)
	t.Cleanup(clock.Set(func() time.Time { return wfxTestNow }))
	ws := t.TempDir()
	proj := filepath.Join(ws, ".termocode", "workflows")
	priv := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "workflows")
	for dir, files := range map[string]map[string]string{
		proj: {"docs-report": fmt.Sprintf(wfxSample, "docs-report"), "broken": `{ "version": 1, "name": "broken", "steps": [ { "type": "nope" } ] }`},
		priv: {"mine": fmt.Sprintf(wfxSample, "mine")},
		filepath.Join(ws, "docs"): {},
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for n, body := range files {
			if err := os.WriteFile(filepath.Join(dir, n+".jsonc"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, n := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(ws, "docs", n), []byte("# "+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.wfxLoad(ws)
	return &m, ws
}

func wfxKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func wfxSide(m *Model, w, h int) string { return ansi.Strip(m.renderWorkflowsSidebar(w, h)) }
func wfxPage(m *Model, w, h int) string { return ansi.Strip(m.renderWorkflowsPage(w, h)) }

// wfxPump handles a run's messages until it ends (gate cards answered by
// answer; nil leaves them open and stops).
func wfxPump(t *testing.T, m *Model, id string, answer func(m *Model)) *run.RunFile {
	t.Helper()
	l := m.wfxS().live[id]
	if l == nil {
		t.Fatalf("no live run %s", id)
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-l.ch:
			if _, ok := m.handleWfxMsg(msg); !ok {
				t.Fatalf("not a workflow message: %T", msg)
			}
			if _, ok := msg.(wfxGateMsg); ok && answer != nil {
				answer(m)
			}
			if _, ok := msg.(wfxDoneMsg); ok {
				rf, _ := m.wfxRunByID(id)
				return rf
			}
		case <-deadline:
			t.Fatal("the run did not end")
		}
	}
}

func wfxOnlyLive(t *testing.T, m *Model) string {
	t.Helper()
	for id := range m.wfxS().live {
		return id
	}
	t.Fatal("no run started")
	return ""
}

// Spec (plan §5.1): the list shows every workflow with its state: ◌ not
// trusted (repo file), ✗ file error, ○ off (the private file is trusted).
func TestWorkflowsListStates(t *testing.T) {
	m, _ := wfxRig(t)
	side := wfxSide(m, 44, 12)
	for _, want := range []string{"WORKFLOWS", "broken", "✗ file error", "docs-report", "◌ not trusted", "mine", "○ off"} {
		if !strings.Contains(side, want) {
			t.Errorf("sidebar misses %q:\n%s", want, side)
		}
	}
	if m.wfxS().sel != "broken" {
		t.Fatalf("first row selected, got %q", m.wfxS().sel)
	}
	m.handleWorkflowsSidebarKey(wfxKey('j'))
	if m.wfxS().sel != "docs-report" {
		t.Fatalf("j moves: %q", m.wfxS().sel)
	}
}

// Spec (plan §6.1): r on a repo file shows the trust card first (Cancel
// first and focused, the file shown); after Trust the run card asks,
// again with Cancel first; Enter on a fresh card does nothing risky.
func TestWorkflowsTrustThenRunCard(t *testing.T) {
	m, _ := wfxRig(t)
	m.wfxS().sel = "docs-report"
	m.handleWorkflowsSidebarKey(wfxKey('r'))
	if !m.confirmOpen || m.confirmKind != confirmKindWorkflowTrust || m.confirm.SafeID() != "cancel" {
		t.Fatalf("trust card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, ".termocode/workflows/docs-report.jsonc") || !strings.Contains(box, "a small sample") {
		t.Fatalf("the card shows the file:\n%s", box)
	}
	m.answerWorkflowCard(m.confirm.SafeID())
	if e, _ := wire.Find(m.wfxS().list, "docs-report"); e.Trusted {
		t.Fatal("Cancel trusted the file")
	}
	m.handleWorkflowsSidebarKey(wfxKey('r'))
	m.answerWorkflowCard("trust")
	if !m.confirmOpen || m.confirmKind != confirmKindWorkflowRun || m.confirm.SafeID() != "cancel" {
		t.Fatalf("run card: kind %v safe %q", m.confirmKind, m.confirm.SafeID())
	}
	if e, _ := wire.Find(m.wfxS().list, "docs-report"); !e.Trusted {
		t.Fatal("trust not stored")
	}
	m.answerWorkflowCard("cancel")
	if len(m.wfxS().live) != 0 {
		t.Fatal("Cancel started a run")
	}
}

// Spec (plan §4.5, §5.4): a dry run reads for real, lists the write with
// its text on the dry-run page and writes nothing.
func TestWorkflowsDryRunPage(t *testing.T) {
	m, ws := wfxRig(t)
	m.wfxS().sel = "mine"
	m.wfxAskRun(true)
	if m.confirmKind != confirmKindWorkflowDry || m.confirm.SafeID() != "cancel" {
		t.Fatalf("dry card: kind %v safe %q", m.confirmKind, m.confirm.SafeID())
	}
	m.answerWorkflowCard("dry")
	rf := wfxPump(t, m, wfxOnlyLive(t, m), nil)
	if rf.State != run.RunOK || !rf.Dry {
		t.Fatalf("state %s dry %v err %s", rf.State, rf.Dry, rf.Error)
	}
	page := wfxPage(m, 120, 40)
	for _, want := range []string{"Dry run · mine", "read 2", "would write 1", "WOULD WRITE", "file reports/docs.md", `"2 docs"`, "Run for real (r)"} {
		if !strings.Contains(page, want) {
			t.Errorf("page misses %q:\n%s", want, page)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, "reports", "docs.md")); !os.IsNotExist(err) {
		t.Fatal("a dry run wrote")
	}
}

// Spec (plan §5.5): a real run's write opens the gate card (Deny first);
// Deny sends nothing and the run page shows it denied; Allow once writes.
func TestWorkflowsGateCardRouting(t *testing.T) {
	m, ws := wfxRig(t)
	m.wfxS().sel = "mine"
	m.answerWorkflowCardKind(confirmKindWorkflowRun, "run")
	rf := wfxPump(t, m, wfxOnlyLive(t, m), func(m *Model) {
		if !m.confirmOpen || m.confirmKind != confirmKindWorkflowGate || m.confirm.SafeID() != "deny" {
			t.Fatalf("gate card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
		}
		m.answerWorkflowGate("deny")
		m.confirmOpen = false
	})
	if _, err := os.Stat(filepath.Join(ws, "reports", "docs.md")); !os.IsNotExist(err) {
		t.Fatal("a denied write was sent")
	}
	if st := rf.Steps["rep"]; st == nil || st.State != run.StateDenied {
		t.Fatalf("rep: %+v", rf.Steps["rep"])
	}
	if page := wfxPage(m, 120, 40); !strings.Contains(page, "rep") || !strings.Contains(page, "⊘") {
		t.Fatalf("page:\n%s", page)
	}

	m.answerWorkflowCardKind(confirmKindWorkflowRun, "run")
	var id string
	for k, l := range m.wfxS().live {
		if l != nil {
			id = k
		}
	}
	rf = wfxPump(t, m, id, func(m *Model) { m.answerWorkflowGate("once"); m.confirmOpen = false })
	if b, err := os.ReadFile(filepath.Join(ws, "reports", "docs.md")); err != nil || string(b) != "2 docs" || rf.State != run.RunOK {
		t.Fatalf("allow once: %q %v %s", b, err, rf.State)
	}
}

// answerWorkflowCardKind answers a view card of kind for the selected
// workflow (as if it had been shown).
func (m *Model) answerWorkflowCardKind(kind confirmKindEnum, id string) {
	st := m.wfxS()
	st.pending, st.pendingDry = st.sel, kind == confirmKindWorkflowDry
	m.confirmKind = kind
	m.answerWorkflowCard(id)
}

// Spec: a broken file's problems go to Problems with their line (errors
// severity 1).
func TestWorkflowsProblemsRows(t *testing.T) {
	m, _ := wfxRig(t)
	payload := wfxProblemsPayload(m.wfxS().list)
	if len(payload) != 1 {
		t.Fatalf("one file with problems: %v", payload)
	}
	g := payload[0].([]any)
	rows := g[1].([]any)
	first := rows[0].([]any)
	if !strings.HasSuffix(g[0].(string), "broken.jsonc") || first[0].(int) < 1 || first[2].(int) != 1 || !strings.HasPrefix(first[3].(string), "workflow: ") {
		t.Fatalf("payload %v", payload)
	}
	page := wfxPage(func() *Model { m.wfxS().detail = "broken"; return m }(), 120, 40)
	if !strings.Contains(page, "PROBLEMS") || !strings.Contains(page, "line ") {
		t.Fatalf("broken page:\n%s", page)
	}
}

// Spec: every palette id of the view is in the palette and dispatches.
func TestWorkflowsPaletteIDs(t *testing.T) {
	m, _ := wfxRig(t)
	for _, it := range workflowsPaletteItems() {
		if _, ok := aiActionPolicies[it.ID]; !ok {
			t.Errorf("%s has no AI policy entry", it.ID)
		}
		if _, ok := m.dispatchWorkflowsPalette(it.ID); !ok {
			t.Errorf("%s does not dispatch", it.ID)
		}
		m.confirmOpen = false
	}
}
