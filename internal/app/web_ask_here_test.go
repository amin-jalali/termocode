package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestHereRig is a repo with a changed a.txt, a hub whose page
// approves its own actions (web_approve_on_laptop off, the default), the
// asking phone c, another browser and the gateway's control connection.
func webTestHereRig(t *testing.T) (m *Model, dir string, c, other, ctl *webConn) {
	t.Helper()
	mm := aiTestModel(t)
	m = &mm
	m.output = newOutputStore()
	h := webTestHub(t, m)
	h.approveOnLaptop = false
	dir, _ = webTestRepo(t, m)
	webTestWrite(t, dir, "a.txt", "changed\n")
	webTestGitLoad(m)
	c = webTestSub(t, m)
	other = webTestSub(t, m)
	ctl = webTestCtl(t, h)
	return m, dir, c, other, ctl
}

// webTestHereCall runs a laptop-card op as the hub does and returns its
// answer and the ask event the asking page got (nil: none).
func webTestHereCall(t *testing.T, m *Model, c *webConn, op, args string) (res map[string]any, ask *webproto.AskCard) {
	t.Helper()
	webTestNow(t, c)
	a, err := webproto.DecodeArgs(op, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s %s: %v", op, args, err)
	}
	c.lastID++
	call := &webCall{ctx: c.ctx, conn: c, id: c.lastID, op: op, args: a}
	c.inflight.Add(1)
	cmd, found := m.webRunCall(m.web, call)
	if !found {
		t.Fatalf("no handler for %s", op)
	}
	if cmd != nil {
		if am, ok := cmd().(applyMsg); ok {
			webTestGitRefresh(t, m, am(m))
		}
	}
	m.webAfterUpdate(nil)
	for _, f := range webTestNow(t, c) {
		switch {
		case f["t"] == "ev" && f["kind"] == webproto.EventAsk:
			ask = &webproto.AskCard{}
			webTestData(t, f, ask)
		case f["id"] == float64(call.id):
			res = f
		}
	}
	if res == nil {
		t.Fatalf("%s gave no answer", op)
	}
	return res, ask
}

// webTestHereAnswer answers a page card from c with choice and runs what
// the answer started; heard are the answered events c got with it.
func webTestHereAnswer(t *testing.T, m *Model, c *webConn, id, choice string) map[string]any {
	t.Helper()
	f, _ := webTestHereAnswerHeard(t, m, c, id, choice)
	return f
}

func webTestHereAnswerHeard(t *testing.T, m *Model, c *webConn, id, choice string) (map[string]any, []webproto.Answered) {
	t.Helper()
	var seen []map[string]any
	f, cmd := webTestCallSeen(t, m, c, webproto.OpCardsAnswer, `{"id":"`+id+`","choice":"`+choice+`"}`, &seen)
	webTestGitRefresh(t, m, cmd)
	m.webAfterUpdate(nil)
	var heard []webproto.Answered
	for _, x := range seen {
		if x["t"] == "ev" && x["kind"] == webproto.EventAnswered {
			var a webproto.Answered
			webTestData(t, x, &a)
			heard = append(heard, a)
		}
	}
	return f, heard
}

func webTestFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return string(b)
}

// Spec (owner 2026-11-08, "we must be able to do everything on the
// web"): a paired control device approves its own actions in the page.
// An op that used to wait for a laptop card asks only the page that
// sent it: the page gets an ask event with the card's exact text (the
// same title, body and action the laptop card shows), Cancel first. No
// dialog opens on the laptop and the card is not in the shared cards
// topic. Only the asking connection may confirm it (another browser is
// refused; a viewer may not answer at all). Confirmed: it runs, the
// page hears answered, the laptop shows a passive toast (no card), the
// Web channel and the gateway's audit say it was approved in the page.
func TestWebApprovesInPage(t *testing.T) {
	m, dir, c, other, ctl := webTestHereRig(t)

	res, ask := webTestHereCall(t, m, c, webproto.OpGitDiscard, `{"paths":["a.txt"]}`)
	var ref webproto.AskRef
	webTestData(t, res, &ref)
	if res["t"] != "res" || ref.Card == "" {
		t.Fatalf("answer %v", res)
	}
	if ask == nil {
		t.Fatal("the page got no ask event: it cannot approve the action")
	}
	if ask.ID != ref.Card || ask.Title != "Discard changes?" || ask.Action != "Discard" ||
		ask.Body != "Discard changes in a.txt? This cannot be undone." {
		t.Fatalf("ask %+v (want the laptop card's exact text)", ask)
	}
	if m.confirmOpen {
		t.Fatalf("a dialog opened on the laptop (kind %v)", m.confirmKind)
	}
	if webTestAskCard(t, m, ref.Card) != nil {
		t.Error("the page's question is in the shared cards topic")
	}
	if got := webTestFile(t, dir, "a.txt"); got != "changed\n" {
		t.Fatalf("discarded before the page said yes: %q", got)
	}

	// Another browser may not confirm it; a viewer may not answer at all.
	if f := webTestHereAnswer(t, m, other, ref.Card, webproto.ChoiceConfirm); webTestErr(f) != "forbidden" {
		t.Fatalf("another browser confirmed it: %v", f)
	}
	viewer := webPolicyConn(m.web, "c-viewer", webproto.RoleViewer)
	if code := webPolicyReq(t, viewer, webproto.OpCardsAnswer, json.RawMessage(`{"id":"`+ref.Card+`","choice":"confirm"}`)); code != "forbidden" {
		t.Fatalf("a viewer answered: %s", code)
	}
	if code := webPolicyReq(t, viewer, webproto.OpGitDiscard, json.RawMessage(`{"paths":["a.txt"]}`)); code != "forbidden" {
		t.Fatalf("a viewer asked: %s", code)
	}
	if got := webTestFile(t, dir, "a.txt"); got != "changed\n" {
		t.Fatalf("discarded by someone else: %q", got)
	}

	f, heard := webTestHereAnswerHeard(t, m, c, ref.Card, webproto.ChoiceConfirm)
	if f["t"] != "res" {
		t.Fatalf("the asking page could not confirm: %v", f)
	}
	if got := webTestFile(t, dir, "a.txt"); got != "one\ntwo\n" {
		t.Fatalf("not discarded after the page's yes: %q", got)
	}
	if a := heard; len(a) != 1 || a[0] != (webproto.Answered{ID: ref.Card, By: "phone", Choice: webproto.ChoiceConfirm}) {
		t.Fatalf("answered %+v", a)
	}
	if m.confirmOpen {
		t.Error("a dialog opened on the laptop")
	}
	seen := false
	for _, x := range m.toast.Live() {
		if x.Title == "Approved on phone" && x.Detail == "Discard changes?" {
			seen = true
		}
	}
	if !seen {
		t.Errorf("no passive laptop toast: %+v", m.toast.Live())
	}
	if a := webTestAudit(m); !strings.Contains(a, "git.discard  a.txt  by=web:phone  → confirmed on phone (page)") {
		t.Errorf("audit:\n%s", a)
	}
	notes := webTestNotes(t, ctl)
	want := []webproto.AuditNote{
		{Conn: c.id, Device: "phone", Op: "git.discard", OK: true, Why: "page_card", Summary: "a.txt"},
		{Conn: c.id, Device: "phone", Op: "git.discard", OK: true, Why: "confirmed_page", Summary: "a.txt"},
	}
	var got []webproto.AuditNote
	for _, n := range notes {
		if n.Op == "git.discard" {
			got = append(got, n)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("notes %+v", notes)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("note %d: %+v, want %+v", i, got[i], want[i])
		}
	}

	// The card is gone: a second answer finds nothing.
	if f := webTestHereAnswer(t, m, c, ref.Card, webproto.ChoiceConfirm); webTestErr(f) != "gone" {
		t.Fatalf("a second answer: %v", f)
	}
}

// Spec (owner 2026-11-08): Cancel in the page does nothing but end the
// question; the audit says it was cancelled in the page.
func TestWebApproveInPageCancel(t *testing.T) {
	m, dir, c, _, ctl := webTestHereRig(t)
	res, ask := webTestHereCall(t, m, c, webproto.OpGitDiscard, `{"paths":["a.txt"]}`)
	if ask == nil || res["t"] != "res" {
		t.Fatalf("answer %v ask %v", res, ask)
	}
	f, heard := webTestHereAnswerHeard(t, m, c, ask.ID, webproto.ChoiceCancel)
	if f["t"] != "res" {
		t.Fatalf("cancel: %v", f)
	}
	if got := webTestFile(t, dir, "a.txt"); got != "changed\n" {
		t.Fatalf("cancelled, yet discarded: %q", got)
	}
	if a := heard; len(a) != 1 || a[0] != (webproto.Answered{ID: ask.ID, By: "phone", Choice: webproto.ChoiceCancel}) {
		t.Fatalf("answered %+v", a)
	}
	if a := webTestAudit(m); !strings.Contains(a, "git.discard  a.txt  by=web:phone  → cancelled on phone (page)") {
		t.Errorf("audit:\n%s", a)
	}
	found := false
	for _, n := range webTestNotes(t, ctl) {
		if n.Op == "git.discard" && n.Why == "cancelled_page" && !n.OK {
			found = true
		}
	}
	if !found {
		t.Error("no cancelled_page note")
	}
}

// Spec (owner 2026-11-08): a page question lives only as long as the
// connection that asked: when it ends, the question is cancelled and
// nothing runs.
func TestWebApproveInPageAskerGone(t *testing.T) {
	m, dir, c, _, _ := webTestHereRig(t)
	_, ask := webTestHereCall(t, m, c, webproto.OpGitDiscard, `{"paths":["a.txt"]}`)
	if ask == nil {
		t.Fatal("no ask event")
	}
	m.onWebGone(webGoneMsg{hub: m.web, conn: c})
	if len(m.web.asks.here) != 0 {
		t.Fatalf("questions left: %d", len(m.web.asks.here))
	}
	if !m.web.idle() && len(m.web.live) == 0 {
		t.Error("the hub is not idle after the asker left")
	}
	if got := webTestFile(t, dir, "a.txt"); got != "changed\n" {
		t.Fatalf("ran after the asker left: %q", got)
	}
	if a := webTestAudit(m); !strings.Contains(a, "→ cancelled on phone (page)") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec (owner 2026-11-08): the setting web_approve_on_laptop (default
// off) keeps the old way: the laptop shows its Risky card, the page gets
// no ask event and may only cancel.
func TestWebApproveOnLaptopKeepsLaptopCard(t *testing.T) {
	m, dir, c, _, _ := webTestHereRig(t)
	m.web.approveOnLaptop = true
	res, ask := webTestHereCall(t, m, c, webproto.OpGitDiscard, `{"paths":["a.txt"]}`)
	var ref webproto.AskRef
	webTestData(t, res, &ref)
	if ask != nil {
		t.Fatalf("an ask event with the laptop setting on: %+v", ask)
	}
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != webproto.ChoiceCancel {
		t.Fatalf("laptop dialog open=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	if card := webTestAskCard(t, m, ref.Card); card == nil || !card.Choices[1].NeedsLaptop {
		t.Fatalf("card %+v", card)
	}
	if f := webTestHereAnswer(t, m, c, ref.Card, webproto.ChoiceConfirm); webTestErr(f) != "forbidden" {
		t.Fatalf("the web confirmed a laptop card: %v", f)
	}
	if got := webTestFile(t, dir, "a.txt"); got != "changed\n" {
		t.Fatalf("discarded without the laptop: %q", got)
	}
}

// Spec (owner 2026-11-08): web_approve_on_laptop is a file-only setting,
// off by default; true turns the laptop cards back on. The AI and the
// web can never change it.
func TestWebApproveOnLaptopSetting(t *testing.T) {
	withConfigDir(t)
	if webSettings().ApproveOnLaptop || webApproveOnLaptopSetting() {
		t.Fatal("approve on laptop is on by default")
	}
	writeUserConfig(t, `{"web_approve_on_laptop":true}`)
	if !webSettings().ApproveOnLaptop || !webApproveOnLaptopSetting() {
		t.Fatal("web_approve_on_laptop true is not read")
	}
	found := false
	for _, d := range fileOnlySettings {
		if d.Key == "web_approve_on_laptop" {
			found = d.Default == "false" && strings.Contains(d.Category, "file only") && d.Description != ""
		}
	}
	if !found {
		t.Error("web_approve_on_laptop is not documented as a file-only setting, default false")
	}
	if p, ok := aiSettingPolicies["web_approve_on_laptop"]; !ok || p.Gate != aiGateNever {
		t.Error("the AI may change web_approve_on_laptop")
	}
}

// Spec (owner 2026-11-08): meta tells a controller page that it approves
// its own actions (approve_here); a viewer, or a hub with the laptop
// setting on, does not get it.
func TestWebMetaApproveHere(t *testing.T) {
	m := aiTestModel(t)
	h := webTestHub(t, &m)
	meta := func(role webproto.Role) webproto.Meta {
		c := webPolicyConn(h, "c-"+string(role), role)
		m.onWebHello(webHelloMsg{hub: h, conn: c})
		for _, f := range webTestNow(t, c) {
			if f["t"] == "meta" {
				b, _ := json.Marshal(f)
				var out webproto.Meta
				if err := json.Unmarshal(b, &out); err != nil {
					t.Fatal(err)
				}
				return out
			}
		}
		t.Fatal("no meta")
		return webproto.Meta{}
	}
	h.approveOnLaptop = false
	if !meta(webproto.RoleController).ApproveHere {
		t.Error("a controller page does not approve here")
	}
	if meta(webproto.RoleViewer).ApproveHere {
		t.Error("a viewer approves here")
	}
	h.approveOnLaptop = true
	if meta(webproto.RoleController).ApproveHere {
		t.Error("approve_here with the laptop setting on")
	}
}
