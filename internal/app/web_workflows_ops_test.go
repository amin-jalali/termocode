package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// Spec (MOB-60, mobile plan §3.9): the phone dry runs a trusted workflow
// at once and gets its run id; nothing is written. An untrusted repo file
// is forbidden (trust stays on the laptop), a broken file is a conflict,
// an unknown name is not_found, and a workflow that runs already is a
// conflict.
func TestWorkflowsWebDryRunFromPhone(t *testing.T) {
	m, ws := wfxRig(t)
	webTestHub(t, m)
	phone := webTestSub(t, m, "workflows")

	for name, code := range map[string]string{"docs-report": "forbidden", "broken": "conflict", "nope": "not_found"} {
		f, _ := webTestCall(t, m, phone, webproto.OpWorkflowsDryRun, `{"name":"`+name+`"}`)
		if f["t"] != "err" || f["code"] != code {
			t.Errorf("dry run %s: %v", name, f)
		}
	}
	if len(m.wfxS().live) != 0 {
		t.Fatal("a refused dry run started")
	}

	f, _ := webTestCall(t, m, phone, webproto.OpWorkflowsDryRun, `{"name":"mine"}`)
	var res webproto.WorkflowRunArgs
	if f["t"] != "res" {
		t.Fatalf("dry run: %v", f)
	}
	webTestData(t, f, &res)
	l := m.wfxS().live[res.Run]
	if res.Run == "" || l == nil || !l.dry {
		t.Fatalf("dry run id %q live %+v", res.Run, l)
	}
	if f, _ := webTestCall(t, m, phone, webproto.OpWorkflowsDryRun, `{"name":"mine"}`); f["code"] != "conflict" {
		t.Errorf("second dry run while one runs: %v", f)
	}
	rf := wfxPump(t, m, res.Run, nil)
	if !rf.Dry || rf.State != run.RunOK {
		t.Fatalf("dry run %+v", rf)
	}
	if _, err := os.Stat(filepath.Join(ws, "reports", "docs.md")); !os.IsNotExist(err) {
		t.Fatalf("a dry run from the phone wrote: %v", err)
	}
}

// Spec (MOB-60): workflows.run from the phone is a laptop card, Cancel
// first; nothing runs before the laptop says yes, Cancel runs nothing,
// and the confirmed run still asks before its write (Deny first).
func TestWorkflowsWebRunThroughTheLaptop(t *testing.T) {
	m, ws := wfxRig(t)
	webTestHub(t, m)
	phone := webTestSub(t, m, "workflows", "cards")

	if f := webTestCallBg(t, m, phone, webproto.OpWorkflowsRun, `{"name":"docs-report"}`); f["code"] != "forbidden" || m.confirmOpen {
		t.Fatalf("untrusted run: %v open %v", f, m.confirmOpen)
	}
	f := webTestCallBg(t, m, phone, webproto.OpWorkflowsRun, `{"name":"mine"}`)
	if f["t"] != "res" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.Buttons()[0].ID != webproto.ChoiceCancel {
		t.Fatalf("no laptop card: %v", f)
	}
	if len(m.wfxS().live) != 0 {
		t.Fatal("ran before the laptop said yes")
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceCancel)
	if len(m.wfxS().live) != 0 {
		t.Fatal("Cancel ran it")
	}

	webTestCallBg(t, m, phone, webproto.OpWorkflowsRun, `{"name":"mine"}`)
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	id := wfxOnlyLive(t, m)
	if m.wfxS().live[id].dry {
		t.Fatal("a real run started as a dry run")
	}
	wfxPumpToGate(t, m, id)
	m.answerWorkflowGate("deny")
	m.confirmOpen = false
	if rf := wfxPump(t, m, id, nil); rf.Dry || rf.Steps["rep"].State != run.StateDenied {
		t.Fatalf("run %+v", rf.Steps["rep"])
	}
	if _, err := os.Stat(filepath.Join(ws, "reports", "docs.md")); !os.IsNotExist(err) {
		t.Fatalf("the denied write was sent: %v", err)
	}
}

// Spec (MOB-60): workflows.save from the phone checks the file like
// workflow_draft (a bad file never reaches a card), then asks on the
// laptop (Cancel first); only Create writes it and trusts that version.
// A file that changed while the card was open is not overwritten.
func TestWorkflowsWebSaveThroughTheLaptop(t *testing.T) {
	m, ws := wfxRig(t)
	webTestHub(t, m)
	phone := webTestSub(t, m, "workflows", "cards")
	path := filepath.Join(ws, ".termocode", "workflows", "fresh.jsonc")
	body := fmt.Sprintf(wfxSample, "fresh")
	args := func(name, content string) string {
		return fmt.Sprintf(`{"name":%q,"content":%q}`, name, content)
	}

	if f := webTestCallBg(t, m, phone, webproto.OpWorkflowsSave, args("fresh", `{ "version": 1, "name": "fresh", "steps": [ { "type": "nope" } ] }`)); f["code"] != "bad_request" || m.confirmOpen {
		t.Fatalf("broken file: %v open %v", f, m.confirmOpen)
	}
	f := webTestCallBg(t, m, phone, webproto.OpWorkflowsSave, args("fresh", body))
	if f["t"] != "res" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.Buttons()[0].ID != webproto.ChoiceCancel {
		t.Fatalf("no laptop card: %v", f)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("saved before the laptop said yes")
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceCancel)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Cancel saved")
	}

	webTestCallBg(t, m, phone, webproto.OpWorkflowsSave, args("fresh", body))
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	if b, err := os.ReadFile(path); err != nil || strings.TrimSpace(string(b)) != strings.TrimSpace(body) {
		t.Fatalf("saved %q %v", b, err)
	}
	e, ok := wire.Find(m.wfxS().list, "fresh")
	if !ok || !e.Trusted || e.Broken() {
		t.Fatalf("saved workflow %+v", e)
	}

	// Changed on disk while the card was open: the laptop's file stays.
	changed := strings.Replace(body, "Docs report", "Docs report v2", 1)
	webTestCallBg(t, m, phone, webproto.OpWorkflowsSave, args("fresh", changed))
	hand := strings.Replace(body, "Docs report", "Edited by hand", 1)
	if err := os.WriteFile(path, []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	if b, _ := os.ReadFile(path); string(b) != hand {
		t.Fatalf("overwrote a file that changed: %q", b)
	}
}
