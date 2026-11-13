//go:build unix

package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webFwdRig is a hub with a running tailnet panel and the asking phone c.
func webFwdRig(t *testing.T) (*Model, *webConn, *fakeFwdServe, *[]string) {
	t.Helper()
	mm := aiTestModel(t)
	m := &mm
	m.output = newOutputStore()
	writeUserConfig(t, `{"web_panel":"on","web_listen":"tailnet"}`)
	webTestHub(t, m)
	m.webPanel = &webPanelState{up: true, port: 41237, url: "https://box.tail1234.ts.net/",
		proc: &webGatewayProc{terminate: func() {}, alive: func() bool { return true }}}
	sv, wrote, _ := swapForward(t)
	c := webTestSub(t, m)
	return m, c, sv, wrote
}

// webFwdReady is the fwd.ready event c got (nil: none).
func webFwdReady(t *testing.T, c *webConn) map[string]any {
	t.Helper()
	return webFwdReadyIn(webTestNow(t, c))
}

func webFwdReadyIn(frames []map[string]any) map[string]any {
	for _, f := range frames {
		if f["t"] == "ev" && f["kind"] == "fwd.ready" {
			d, _ := f["data"].(map[string]any)
			return d
		}
	}
	return nil
}

// webFwdCall runs web.forward as the hub does and returns its answer and
// the fwd.ready event that came with it (nil: none).
func webFwdCall(t *testing.T, m *Model, c *webConn, args string) (res, ready map[string]any) {
	t.Helper()
	webTestNow(t, c)
	a, err := webproto.DecodeArgs("web.forward", json.RawMessage(args))
	if err != nil {
		t.Fatalf("web.forward %s: %v", args, err)
	}
	c.lastID++
	call := &webCall{ctx: c.ctx, conn: c, id: c.lastID, op: "web.forward", args: a}
	c.inflight.Add(1)
	cmd, found := m.webRunCall(m.web, call)
	if !found {
		t.Fatal("no handler for web.forward")
	}
	webDrain(t, m, cmd)
	m.webAfterUpdate(nil)
	frames := webTestNow(t, c)
	for _, f := range frames {
		if f["id"] == float64(call.id) {
			res = f
		}
	}
	if res == nil {
		t.Fatal("web.forward gave no answer")
	}
	return res, webFwdReadyIn(frames)
}

// Spec (WEB-54): a click on a localhost link in the page sends
// web.forward with the port only. It waits for the same Risky card as
// Web: Forward Port… (Cancel first, "can see what you type", naming the
// device); nothing opens before the yes. After the yes, tailnet mode
// serves the port on its own https port, and the page that asked gets
// fwd.ready with the address to open (built by the hub, never from input).
func TestWebForwardOpAsksThenSendsURL(t *testing.T) {
	m, c, sv, _ := webFwdRig(t)
	res, _ := webTestHereCall(t, m, c, "web.forward", `{"port":3000}`)
	var ref webproto.AskRef
	webResData(t, res, &ref)
	if ref.Card == "" {
		t.Fatalf("no card id: %v", res)
	}
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != webproto.ChoiceCancel {
		t.Fatalf("laptop card open=%v kind=%v safe=%q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	if v := ansiStrip(m.confirm.View()); !strings.Contains(v, "Open port 3000") || !strings.Contains(v, "can see what you type") || !strings.Contains(v, "phone") {
		t.Fatalf("card:\n%s", v)
	}
	if len(sv.starts) != 0 || webFwdReady(t, c) != nil {
		t.Fatalf("opened before the yes: %v", sv.starts)
	}
	webTestLaptopSelect(t, m, webproto.ChoiceConfirm)
	if len(sv.starts) != 1 || sv.starts[0] != "8443 http://127.0.0.1:3000" {
		t.Fatalf("serve %v", sv.starts)
	}
	d := webFwdReady(t, c)
	if d == nil || d["port"] != float64(3000) || d["url"] != "https://box.tail1234.ts.net:8443/" || d["mode"] != "tailnet" {
		t.Fatalf("fwd.ready %v", d)
	}
	if fw := m.webAccess().fwds; len(fw) != 1 || fw[0].port != 3000 {
		t.Fatalf("forwards %+v", fw)
	}
	// The same port again: no second forward, no card; the page gets the
	// address it has.
	res, d = webFwdCall(t, m, c, `{"port":3000}`)
	if res["t"] != "res" || m.confirmOpen || len(sv.starts) != 1 {
		t.Fatalf("again: %v card %v serve %v", res, m.confirmOpen, sv.starts)
	}
	if d == nil || d["url"] != "https://box.tail1234.ts.net:8443/" {
		t.Fatalf("again: fwd.ready %v", d)
	}
}

// Spec (WEB-54): Cancel opens nothing and sends no address.
func TestWebForwardOpCancelOpensNothing(t *testing.T) {
	m, c, sv, _ := webFwdRig(t)
	webTestHereCall(t, m, c, "web.forward", `{"port":3000}`)
	webTestLaptopSelect(t, m, webproto.ChoiceCancel)
	if len(sv.starts) != 0 || len(m.webAccess().fwds) != 0 || webFwdReady(t, c) != nil {
		t.Fatalf("cancel opened: %v", sv.starts)
	}
}

// Spec (WEB-54): only a port; no host field (strict args), 1..65535, not
// the panel's own port; a viewer can not ask; one per 5 s.
func TestWebForwardOpRefuses(t *testing.T) {
	for _, bad := range []string{`{"port":0}`, `{"port":70000}`, `{"port":3000,"host":"evil.example"}`, `{"host":"127.0.0.1"}`, `{"port":"3000"}`} {
		if _, err := webproto.DecodeArgs("web.forward", []byte(bad)); err == nil {
			t.Errorf("%s passed", bad)
		}
	}
	p, ok := webproto.Lookup("web.forward")
	if !ok || p.Role != webproto.RoleController || !p.LaptopCard || p.Audit != webproto.AuditYes || p.Rate.N != 1 || p.Rate.Per.Seconds() < 5 {
		t.Fatalf("policy %+v", p)
	}
	m, c, _, _ := webFwdRig(t)
	res, _ := webTestHereCall(t, m, c, "web.forward", `{"port":41237}`)
	if webErrCode(res) != "bad_request" || m.confirmOpen {
		t.Fatalf("own port: %v", res)
	}
	// LAN mode has no forward (a phone's 127.0.0.1 is not this computer).
	m.webPanel.fp = "ab:cd"
	m.webPanel.url = "https://192.168.1.5:41237/"
	res, _ = webTestHereCall(t, m, c, "web.forward", `{"port":3000}`)
	if webErrCode(res) == "" || m.confirmOpen {
		t.Fatalf("LAN mode: %v", res)
	}
}

// Spec (WEB-54): in SSH mode the yes asks termocode ssh for the forward;
// it opens the tab there, so fwd.ready has no address (mode ssh).
func TestWebForwardOpSSHMode(t *testing.T) {
	m, c, _, wrote := webFwdRig(t)
	m.webPanel.url, m.webPanel.sshPort = "", 41237
	webTestHereCall(t, m, c, "web.forward", `{"port":3000}`)
	webTestLaptopSelect(t, m, webproto.ChoiceConfirm)
	fw := m.webAccess().fwds
	if len(fw) != 1 || len(*wrote) != 1 || (*wrote)[0] != session.PortForwardAPC(session.PortForward{Open: true, Port: 3000, ID: fw[0].id}) {
		t.Fatalf("forwards %+v wrote %q", fw, *wrote)
	}
	if d := webFwdReady(t, c); d == nil || d["mode"] != "ssh" || d["url"] != nil {
		t.Fatalf("fwd.ready %v", d)
	}
}

// Spec (WEB-54): with the panel on this computer nothing is forwarded and
// the page's own link sheet was the confirm: no card, fwd.ready at once
// with http://127.0.0.1:<port>/.
func TestWebForwardOpLocalModeNoCard(t *testing.T) {
	m, c, sv, wrote := webFwdRig(t)
	m.webPanel.url = ""
	res, d := webFwdCall(t, m, c, `{"port":3000}`)
	if res["t"] != "res" || m.confirmOpen || len(sv.starts) != 0 || len(*wrote) != 0 {
		t.Fatalf("local: %v card %v", res, m.confirmOpen)
	}
	if d == nil || d["url"] != "http://127.0.0.1:3000/" || d["mode"] != "local" {
		t.Fatalf("fwd.ready %v", d)
	}
}

// Spec (WEB-54, owner 2026-11-08): when the page approves its own
// actions, the asking page gets the card's text and confirms it there.
func TestWebForwardOpPageApproves(t *testing.T) {
	m, c, sv, _ := webFwdRig(t)
	m.web.approveOnLaptop = false
	res, ask := webTestHereCall(t, m, c, "web.forward", `{"port":3000}`)
	var ref webproto.AskRef
	webResData(t, res, &ref)
	if ask == nil || ask.ID != ref.Card || !strings.Contains(ask.Title, "Open port 3000") || m.confirmOpen {
		t.Fatalf("ask %+v laptop card %v", ask, m.confirmOpen)
	}
	var seen []map[string]any
	webTestCallSeen(t, m, c, webproto.OpCardsAnswer, `{"id":"`+ref.Card+`","choice":"confirm"}`, &seen)
	if len(sv.starts) != 1 {
		t.Fatalf("serve %v", sv.starts)
	}
	if d := webFwdReadyIn(seen); d == nil || d["url"] != "https://box.tail1234.ts.net:8443/" {
		t.Fatalf("fwd.ready %v", d)
	}
}
