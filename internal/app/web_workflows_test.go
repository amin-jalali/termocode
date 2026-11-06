package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// Workflows on the phone (WFX-19, plan §5.7): the workflows topic lists
// the workflows and runs; a run's write gate is a card of kind workflow
// that the phone may answer with Deny (first, safe) or Allow once, never
// "Allow for this workflow"; one answer wins; the phone may stop a run.

// wfxPumpToGate handles a run's messages until its write card is open.
func wfxPumpToGate(t *testing.T, m *Model, id string) {
	t.Helper()
	l := m.wfxS().live[id]
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-l.ch:
			m.handleWfxMsg(msg)
			if _, ok := msg.(wfxGateMsg); ok {
				if !m.confirmOpen || m.confirmKind != confirmKindWorkflowGate {
					t.Fatal("no write card on the laptop")
				}
				return
			}
		case <-deadline:
			t.Fatal("no write card")
		}
	}
}

// webTestWorkflowCard is the phone's one workflow card.
func webTestWorkflowCard(t *testing.T, m *Model, c *webConn) webproto.Card {
	t.Helper()
	m.webAfterUpdate(nil)
	var cards []webproto.Card
	for _, f := range webTestNow(t, c) {
		switch {
		case f["t"] == "snap" && f["topic"] == "cards":
			var s webproto.CardsSnap
			webTestData(t, f, &s)
			cards = s.Cards
		case f["t"] == "delta" && f["topic"] == "cards":
			var d webproto.CardsDelta
			webTestData(t, f, &d)
			if d.Card != nil {
				cards = append(cards, *d.Card)
			}
		}
	}
	for _, c := range cards {
		if c.Kind == webproto.CardWorkflow {
			return c
		}
	}
	t.Fatalf("no workflow card in %+v", cards)
	return webproto.Card{}
}

func wfxStartMine(t *testing.T, m *Model) string {
	t.Helper()
	m.wfxS().sel = "mine"
	m.answerWorkflowCardKind(confirmKindWorkflowRun, "run")
	id := wfxOnlyLive(t, m)
	wfxPumpToGate(t, m, id)
	return id
}

func TestWorkflowsWebApproveFromPhone(t *testing.T) {
	m, ws := wfxRig(t)
	m.output = newOutputStore()
	webTestHub(t, m)
	phone := webTestSub(t, m, "cards", "workflows")
	id := wfxStartMine(t, m)

	card := webTestWorkflowCard(t, m, phone)
	if len(card.Choices) != 2 || card.Choices[0].ID != webproto.ChoiceDeny || !card.Choices[0].Safe ||
		card.Choices[1].ID != webproto.ChoiceAllow || card.Choices[1].Label != "Allow once" {
		t.Fatalf("choices %+v", card.Choices)
	}
	if !strings.Contains(card.Title, "mine") || !strings.Contains(card.Body, "rep") || !strings.Contains(card.Body, "reports/docs.md") {
		t.Fatalf("card %+v", card)
	}
	for _, choice := range []string{"workflow", "once", "always"} {
		if f, _ := webTestCall(t, m, phone, webproto.OpCardsAnswer, `{"id":"`+card.ID+`","choice":"`+choice+`"}`); f["code"] != "forbidden" {
			t.Fatalf("%s from the phone: %v", choice, f)
		}
	}
	f, _ := webTestCall(t, m, phone, webproto.OpCardsAnswer, `{"id":"`+card.ID+`","choice":"allow"}`)
	if f["t"] != "res" || m.confirmOpen {
		t.Fatalf("allow once: %v open %v", f, m.confirmOpen)
	}
	if lines := m.output.Lines(aiAuditChannel); len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "by=web:phone") || !strings.Contains(lines[len(lines)-1], "workflow=mine") {
		t.Errorf("audit %q", lines)
	}
	rf := wfxPump(t, m, id, nil)
	if b, err := os.ReadFile(filepath.Join(ws, "reports", "docs.md")); err != nil || string(b) != "2 docs" || rf.State != run.RunOK {
		t.Fatalf("after allow once: %q %v %s", b, err, rf.State)
	}
	e, _ := wire.Find(m.wfxS().list, "mine")
	if ok, _ := ledger.Allowed(m.wfxS().root, "mine", e.Hash, "rep"); ok {
		t.Fatal("an answer from the phone changed the allow-list")
	}
	if f, _ := webTestCall(t, m, phone, webproto.OpCardsAnswer, `{"id":"`+card.ID+`","choice":"deny"}`); f["code"] != "gone" {
		t.Fatalf("second answer: %v", f)
	}

	// Deny from the phone: the write is not sent.
	if err := os.Remove(filepath.Join(ws, "reports", "docs.md")); err != nil {
		t.Fatal(err)
	}
	id = wfxStartMine(t, m)
	card = webTestWorkflowCard(t, m, phone)
	if f, _ := webTestCall(t, m, phone, webproto.OpCardsAnswer, `{"id":"`+card.ID+`","choice":"deny"}`); f["t"] != "res" {
		t.Fatalf("deny: %v", f)
	}
	rf = wfxPump(t, m, id, nil)
	if _, err := os.Stat(filepath.Join(ws, "reports", "docs.md")); !os.IsNotExist(err) || rf.Steps["rep"].State != run.StateDenied {
		t.Fatalf("deny from the phone sent the write: %v %+v", err, rf.Steps["rep"])
	}
}

// Spec (plan §5.7, §6.10): the workflows topic lists workflows and runs
// (the waiting step of a run) and never the file's text; the phone may
// stop a run.
func TestWorkflowsWebTopicAndStop(t *testing.T) {
	m, _ := wfxRig(t)
	webTestHub(t, m)
	phone := webTestSub(t, m, "workflows")
	var snap webproto.WorkflowsSnap
	for _, f := range webTestNow(t, phone) {
		if f["t"] == "snap" && f["topic"] == "workflows" {
			webTestData(t, f, &snap)
		}
	}
	states := map[string]string{}
	for _, w := range snap.Workflows {
		states[w.Name] = w.State
	}
	if states["mine"] != "off" || states["docs-report"] != "untrusted" || states["broken"] != "broken" {
		t.Fatalf("workflows %+v", snap.Workflows)
	}
	id := wfxStartMine(t, m)
	m.web.wfx.checked = time.Time{} // the 500 ms wait is over
	m.webAfterUpdate(nil)
	var raw strings.Builder
	var cur webproto.WorkflowsSnap
	for _, f := range webTestNow(t, phone) {
		b, _ := json.Marshal(f)
		raw.Write(b)
		if f["t"] == "delta" && f["topic"] == "workflows" {
			webTestData(t, f, &cur)
		}
	}
	if len(cur.Runs) == 0 || cur.Runs[0].ID != id || cur.Runs[0].State != "running" || cur.Runs[0].Waiting != "rep" {
		t.Fatalf("runs %+v", cur.Runs)
	}
	if strings.Contains(raw.String(), "a small sample") || strings.Contains(raw.String(), "glob") {
		t.Fatalf("the topic carries the file's text: %s", raw.String())
	}
	if f, _ := webTestCall(t, m, phone, webproto.OpWorkflowsStop, `{"run":"nope"}`); f["t"] != "err" {
		t.Fatalf("stop of an unknown run: %v", f)
	}
	if f, _ := webTestCall(t, m, phone, webproto.OpWorkflowsStop, `{"run":"`+id+`"}`); f["t"] != "res" {
		t.Fatalf("stop: %v", f)
	}
	m.answerWorkflowGate("deny") // the laptop card goes away with the run
	m.confirmOpen = false
	if rf := wfxPump(t, m, id, nil); rf.State != run.RunCancel {
		t.Fatalf("stopped run: %s", rf.State)
	}
}

// Spec (WEB-7, WFX-19): the workflows replay golden: trusting a repo
// workflow turns its row from untrusted to off.
func TestWorkflowsReplayGolden(t *testing.T) {
	m, _ := wfxRig(t)
	h := webTestHub(t, m)
	c := webReplayRecord(t, m, h, "workflows", func() {
		e, _ := wire.Find(m.wfxS().list, "docs-report")
		if err := wire.Trust(m.wfxS().root, e); err != nil {
			t.Fatal(err)
		}
		m.wfxLoad("")
		m.web.wfx.checked = time.Time{}
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, h.topics["workflows"], c)
	webReplayGolden(t, webReplayPath("workflows", "trust_turns_off"), c)
}
