package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// Resume after a crash or restart (WFX-16, plan §4.3): a run file that
// says running when termocode starts is from a process that ended. It is
// marked interrupted and, with resume_on_start (the default), started
// again from the step that was running: finished steps stay finished,
// the write asks again and is sent once. The page shows ↻ Resumed.

const wfxResumable = `{
  "version": 1, "name": "%NAME%", "title": "Resume me"%RESUME%,
  "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/**/*.md" },
    { "id": "rep", "type": "write", "to": "file", "path": "reports/%NAME%.md", "content": "{{len(steps.docs.items)}} docs" }
  ]
}`

// wfxCrashAtGate starts a run of name, waits for its write card and
// leaves the run file as a crash would: state running, rep running.
func wfxCrashAtGate(t *testing.T, m *Model, name string) string {
	t.Helper()
	e, ok := wire.Find(m.wfxS().list, name)
	if !ok {
		t.Fatalf("no %s", name)
	}
	id, h, _, err := m.wfxStartID(e, false)
	if err != nil {
		t.Fatal(err)
	}
	l := m.wfxS().live[id]
	deadline := time.After(10 * time.Second)
	var snap *run.RunFile
	for snap == nil {
		select {
		case msg := <-l.ch:
			if g, ok := msg.(wfxGateMsg); ok {
				snap = h.Snapshot() // the process ends here: the card never got its answer
				g.reply <- wire.GateDeny
				continue
			}
			m.handleWfxMsg(msg)
		case <-deadline:
			t.Fatal("no write card")
		}
	}
	h.Cancel()
	<-h.Done()
	delete(m.wfxS().live, id)
	m.confirmOpen = false
	m.wfGate = nil
	if snap.State != run.RunRunning || snap.Steps["rep"] == nil || snap.Steps["rep"].State != run.StateRunning {
		t.Fatalf("crash state: %s rep %+v", snap.State, snap.Steps["rep"])
	}
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := (wire.RunFiles{Root: m.wfxS().root}).WriteRunFile(id, b); err != nil {
		t.Fatal(err)
	}
	return id
}

// wfxRestart is a new process on the same workspace and state.
func wfxRestart(t *testing.T, ws string) *Model {
	t.Helper()
	m := aiTestModel(t)
	m.wfxLoad(ws)
	return &m
}

func wfxWriteResumable(t *testing.T, m *Model, name, resume string) {
	t.Helper()
	body := strings.NewReplacer("%NAME%", name, "%RESUME%", resume).Replace(wfxResumable)
	if err := os.WriteFile(filepath.Join(wfxPrivateDir(), name+".jsonc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m.wfxLoad("")
}

func TestWorkflowsResumeAfterCrash(t *testing.T) {
	m, ws := wfxRig(t)
	wfxWriteResumable(t, m, "rs", "")
	id := wfxCrashAtGate(t, m, "rs")

	m2 := wfxRestart(t, ws)
	m2.wfxResumeOnStart()
	l := m2.wfxS().live[id]
	if l == nil {
		t.Fatalf("run %s was not resumed (live %v)", id, m2.wfxS().live)
	}
	asked := 0
	rf := wfxPump(t, m2, id, func(m *Model) { asked++; m.answerWorkflowGate("once"); m.confirmOpen = false })
	if rf.State != run.RunOK || rf.Resumed != 1 || asked != 1 {
		t.Fatalf("resumed run: %s resumed %d asked %d %s", rf.State, rf.Resumed, asked, rf.Error)
	}
	if b, err := os.ReadFile(filepath.Join(ws, "reports", "rs.md")); err != nil || string(b) != "2 docs" {
		t.Fatalf("the write: %q %v", b, err)
	}
	m2.wfxShowPage("run:"+id, false)
	if page := wfxPage(m2, 120, 30); !strings.Contains(page, "↻ Resumed") {
		t.Fatalf("the run page shows the resume:\n%s", page)
	}

	m3 := wfxRestart(t, ws)
	m3.wfxResumeOnStart()
	if len(m3.wfxS().live) != 0 {
		t.Fatal("a finished run was resumed again")
	}
}

// Spec (plan §4.3): resume_on_start false leaves the run interrupted;
// a changed file (another hash) is never resumed either.
func TestWorkflowsNoResume(t *testing.T) {
	m, ws := wfxRig(t)
	wfxWriteResumable(t, m, "off", `, "resume_on_start": false`)
	off := wfxCrashAtGate(t, m, "off")
	wfxWriteResumable(t, m, "chg", "")
	chg := wfxCrashAtGate(t, m, "chg")
	wfxWriteResumable(t, m, "chg", `, "description": "changed"`)

	m2 := wfxRestart(t, ws)
	m2.wfxResumeOnStart()
	if len(m2.wfxS().live) != 0 {
		t.Fatalf("resumed: %v", m2.wfxS().live)
	}
	for _, id := range []string{off, chg} {
		rf, err := wire.RunFiles{Root: m2.wfxS().root}.Load(id)
		if err != nil || rf.State != run.RunInterrupt {
			t.Fatalf("%s: %v %v", id, rf, err)
		}
	}
}

// Spec (WFX-16): only the owner of the workspace resumes: a second
// process while the first holds the claim resumes nothing.
func TestWorkflowsResumeNeedsOwner(t *testing.T) {
	m, ws := wfxRig(t)
	wfxWriteResumable(t, m, "rs", "")
	wfxCrashAtGate(t, m, "rs")
	o, err := ledger.ClaimOwner(m.wfxS().root)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Release()
	m2 := wfxRestart(t, ws)
	m2.wfxBootIn(ws, nil)
	if len(m2.wfxS().live) != 0 || m2.wfxS().sched != nil {
		t.Fatal("a second process resumed or scheduled")
	}
}
