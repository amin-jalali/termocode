//go:build unix

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// accessRig is a hub rig with a running tailnet panel and the gateway's
// control connection.
type accessRig struct {
	*webRig
	ctl   *webClient
	links chan string
}

func newAccessRig(t *testing.T) *accessRig {
	t.Helper()
	r := &accessRig{webRig: newWebRig(t, nil), links: make(chan string, 4)}
	writeUserConfig(t, `{"web_panel":"on","web_listen":"tailnet"}`)
	restore := SetClipboardSink(func(s string) { r.links <- s })
	t.Cleanup(restore)
	r.m.webPanel = &webPanelState{up: true, port: 41237, url: "https://box.tail1234.ts.net/",
		proc: &webGatewayProc{terminate: func() {}, alive: func() bool { return true }}}
	r.ctl = r.dial(`{"t":"ctl"}`)
	r.pumpUntil("the control connection", func() bool { return len(r.hub.ctlConns()) == 1 })
	return r
}

// ctlEvent waits for the next event on the control connection.
func (r *accessRig) ctlEvent(kind string) map[string]any {
	r.t.Helper()
	for {
		m := r.nextMsg(r.ctl)
		if m["t"] == "ev" && m["kind"] == kind {
			d, _ := m["data"].(map[string]any)
			return d
		}
	}
}

// Spec (WEB-32): pairing needs a running panel in tailnet mode; else one
// plain sentence says what to do, and nothing opens.
func TestWebPairNeedsTailnetPanel(t *testing.T) {
	r := newAccessRig(t)
	r.m.webPanel.url = ""
	r.m.openWebPair(false)
	if r.m.confirmOpen || !strings.Contains(webTestToasts(*r.m), "tailnet") {
		t.Fatalf("loopback: card %v toast %q", r.m.confirmOpen, webTestToasts(*r.m))
	}
	r.m.webPanel = nil
	r.m.openWebPair(false)
	if r.m.confirmOpen || !strings.Contains(webTestToasts(*r.m), "Web: Start Panel") {
		t.Fatalf("no panel: card %v toast %q", r.m.confirmOpen, webTestToasts(*r.m))
	}
}

// Spec (WEB-32): Web: Pair a Device… sends the gateway only the digest
// of a fresh one-time secret (120 s) and shows its QR code on the laptop;
// Copy link gives https://<tailnet>/#p=<secret>, whose digest it is.
func TestWebPairShowsQRAndSendsDigest(t *testing.T) {
	r := newAccessRig(t)
	r.m.openWebPair(false)
	ev := r.ctlEvent("pair")
	if ev["ttl_s"] != float64(120) || ev["guest"] == true || len(ev["sha256"].(string)) != 64 {
		t.Fatalf("pair event %v", ev)
	}
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebPairQR {
		t.Fatal("no QR card")
	}
	view := r.m.confirm.View()
	if !strings.Contains(view, "█") || !strings.Contains(view, "2 minutes") {
		t.Fatalf("QR card:\n%s", view)
	}
	webAccessSelect(r.m, "copy")
	link := <-r.links
	plain, ok := strings.CutPrefix(link, "https://box.tail1234.ts.net/#p=")
	sum := sha256.Sum256([]byte(plain))
	if !ok || hex.EncodeToString(sum[:]) != ev["sha256"] {
		t.Fatalf("link %q does not match the digest", link)
	}
	// The whole QR code is on the card, every row as drawn (no cut, no
	// wrap): a cut code does not scan.
	plainView := ansiStrip(view)
	for i, row := range webQRLines(link) {
		if !strings.Contains(plainView, row) {
			t.Fatalf("QR row %d is not on the card:\n%s", i, plainView)
		}
	}
	if strings.Contains(view, plain) {
		t.Fatal("the secret is printed as text on the card")
	}
}

// Closing the QR card closes the pairing link at the gateway.
func TestWebPairCloseEndsTheLink(t *testing.T) {
	r := newAccessRig(t)
	r.m.openWebPair(false)
	r.ctlEvent("pair")
	webAccessSelect(r.m, "close")
	r.ctlEvent("pair_close")
}

func pairAskLine(id int, guest bool) string {
	g := ""
	if guest {
		g = `,"guest":true`
	}
	return `{"t":"req","id":` + itoaApp(id) + `,"op":"web.pair_ask","args":{"pair_id":"0f1e2d3c4b5a69788796a5b4c3d2e1f0","name":"iPhone · Safari","code":"482913"` + g + `}}`
}

func itoaApp(i int) string { b, _ := json.Marshal(i); return string(b) }

// Spec (WEB-32, AH-1): the gateway's pairing question is a Risky card:
// Not now first and focused, the device name and the phone's code in
// it; the owner's pick goes back as the role, Esc is Not now.
func TestWebPairAskCard(t *testing.T) {
	r := newAccessRig(t)
	r.ctl.send(pairAskLine(1, false))
	r.pumpUntil("the pair card", func() bool { return r.m.confirmOpen && r.m.confirmKind == confirmKindWebPairAsk })
	if r.m.confirm.SafeID() != "not-now" || r.m.confirm.Buttons()[0].ID != "not-now" {
		t.Fatalf("buttons %+v", r.m.confirm.Buttons())
	}
	view := r.m.confirm.View()
	for _, want := range []string{"iPhone · Safari", "482 913", "same code"} {
		if !strings.Contains(view, want) {
			t.Errorf("card has no %q:\n%s", want, view)
		}
	}
	webAccessSelect(r.m, "viewer")
	res := r.nextMsg(r.ctl)
	if d, _ := res["data"].(map[string]any); res["t"] != "res" || d["role"] != "viewer" {
		t.Fatalf("answer %v", res)
	}

	r.ctl.send(pairAskLine(2, false))
	r.pumpUntil("the pair card", func() bool { return r.m.confirmOpen && r.m.confirmKind == confirmKindWebPairAsk })
	r.m.confirmOpen = false
	r.m.onAIConfirmClosed() // Esc
	if res := r.nextMsg(r.ctl); res["t"] != "err" || res["code"] != "forbidden" {
		t.Fatalf("Esc answer %v", res)
	}
}

// A share (guest) card offers only "Let them watch".
func TestWebPairAskGuestCard(t *testing.T) {
	r := newAccessRig(t)
	r.ctl.send(pairAskLine(1, true))
	r.pumpUntil("the share card", func() bool { return r.m.confirmOpen && r.m.confirmKind == confirmKindWebPairAsk })
	for _, b := range r.m.confirm.Buttons() {
		if b.ID == "control" {
			t.Fatal("a guest can be given control")
		}
	}
	webAccessSelect(r.m, "viewer")
	if d, _ := r.nextMsg(r.ctl)["data"].(map[string]any); d["role"] != "viewer" {
		t.Fatalf("guest role %v", d)
	}
}

// While another card is open the pairing is refused (busy), never queued
// behind it or answered by a key meant for the other card.
func TestWebPairAskBusy(t *testing.T) {
	r := newAccessRig(t)
	r.m.showWebStartCard(webSettings(), nil)
	r.ctl.send(pairAskLine(1, false))
	if res := r.nextMsg(r.ctl); res["t"] != "err" || res["code"] != "busy" {
		t.Fatalf("answer %v", res)
	}
	if r.m.confirmKind != confirmKindWebStart {
		t.Fatal("the other card was replaced")
	}
}

// Spec (WEB-32, T30): Web: Devices… lists the paired devices; Remove asks
// on a Risky card (Keep first) and then removes the device from the
// store and tells the gateway to drop it at once.
func TestWebDevicesRemoveRevokes(t *testing.T) {
	r := newAccessRig(t)
	dir, _ := webDevicesDir()
	st := webdevices.Open(dir)
	if err := st.Add(webdevices.Device{ID: "dev-1a2b3c4d", Name: "iPad", Role: webdevices.RoleControl, Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	r.m.openWebDevices()
	if !r.m.pickerOpen || r.m.pickerKind != pickerKindWebDevices {
		t.Fatal("no device list")
	}
	if v := r.m.picker.View(); !strings.Contains(v, "iPad") || !strings.Contains(v, "control") {
		t.Fatalf("list:\n%s", v)
	}
	r.m.pickerOpen = false
	r.m.handleWebAccessPicker("dev-1a2b3c4d")
	if !r.m.confirmOpen || r.m.confirm.Buttons()[0].ID != "close" {
		t.Fatalf("device card %+v", r.m.confirm.Buttons())
	}
	webAccessSelect(r.m, "remove")
	if !r.m.confirmOpen || r.m.confirm.SafeID() != "keep" {
		t.Fatal("remove did not ask first")
	}
	webAccessSelect(r.m, "remove")
	if ev := r.ctlEvent("revoke"); ev["device"] != "dev-1a2b3c4d" {
		t.Fatalf("revoke %v", ev)
	}
	if _, ok, _ := st.Get("dev-1a2b3c4d"); ok {
		t.Fatal("still in the store")
	}
}

// webAccessSelect presses a button of the open card, as Update does with
// confirm.SelectMsg (the toast tick it returns is not run).
func webAccessSelect(m *Model, id string) {
	m.confirmOpen = false
	_ = m.handleConfirmSelect(id)
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func ansiStrip(s string) string { return ansiRe.ReplaceAllString(s, "") }

// Spec (WEB-34, AH-1): the gateway's control question is a Risky card
// (Not now first); Give control answers yes, Esc is Not now.
func TestWebGrantAskCard(t *testing.T) {
	r := newAccessRig(t)
	line := `{"t":"req","id":1,"op":"web.grant_ask","args":{"device":"dev-1a2b3c4d","name":"iPhone · Safari"}}`
	r.ctl.send(line)
	r.pumpUntil("the grant card", func() bool { return r.m.confirmOpen && r.m.confirmKind == confirmKindWebGrant })
	if r.m.confirm.SafeID() != "not-now" {
		t.Fatal("Not now is not the safe answer")
	}
	if v := r.m.confirm.View(); !strings.Contains(v, "iPhone · Safari") || !strings.Contains(v, "any command") {
		t.Fatalf("card:\n%s", v)
	}
	webAccessSelect(r.m, "give")
	if res := r.nextMsg(r.ctl); res["t"] != "res" {
		t.Fatalf("answer %v", res)
	}
	r.ctl.send(strings.Replace(line, `"id":1`, `"id":2`, 1))
	r.pumpUntil("the grant card", func() bool { return r.m.confirmOpen && r.m.confirmKind == confirmKindWebGrant })
	r.m.confirmOpen = false
	r.m.onAIConfirmClosed()
	if res := r.nextMsg(r.ctl); res["t"] != "err" || res["code"] != "forbidden" {
		t.Fatalf("Esc answer %v", res)
	}
}

// Spec (WEB-34): the device card of a controller offers Make viewer: the
// store changes and the gateway hears the new role (its links come back
// as a viewer).
func TestWebDeviceMakeViewer(t *testing.T) {
	r := newAccessRig(t)
	dir, _ := webDevicesDir()
	st := webdevices.Open(dir)
	_ = st.Add(webdevices.Device{ID: "dev-1a2b3c4d", Name: "iPad", Role: webdevices.RoleControl, Created: time.Now()})
	r.m.openWebDeviceCard("dev-1a2b3c4d")
	webAccessSelect(r.m, "viewer")
	if ev := r.ctlEvent("role"); ev["device"] != "dev-1a2b3c4d" || ev["role"] != "viewer" {
		t.Fatalf("role event %v", ev)
	}
	if d, _, _ := st.Get("dev-1a2b3c4d"); d.Role != webdevices.RoleViewer {
		t.Fatalf("stored role %q", d.Role)
	}
	r.m.openWebDeviceCard("dev-1a2b3c4d")
	for _, b := range r.m.confirm.Buttons() {
		if b.ID == "viewer" || b.ID == "control" {
			t.Fatalf("a viewer's card offers %q (control is asked for on the device)", b.ID)
		}
	}
}

// Spec (WEB-50): Web: Share as Viewer… opens a guest pairing link that
// works once for 10 minutes; the card says the person can only watch and
// that the code is checked with them; Copy link is there to send it.
func TestWebShareLink(t *testing.T) {
	r := newAccessRig(t)
	r.m.openWebPair(true)
	ev := r.ctlEvent("pair")
	if ev["guest"] != true || ev["ttl_s"] != float64(600) {
		t.Fatalf("share event %v", ev)
	}
	view := r.m.confirm.View()
	for _, want := range []string{"only watch", "10 minutes", "code"} {
		if !strings.Contains(view, want) {
			t.Errorf("share card has no %q:\n%s", want, view)
		}
	}
	webAccessSelect(r.m, "copy")
	if l := <-r.links; !strings.HasPrefix(l, "https://box.tail1234.ts.net/#p=") {
		t.Fatalf("link %q", l)
	}
	if _, ok := r.m.dispatchWebAccessPalette("web-share"); !ok {
		t.Fatal("no web-share palette id")
	}
}
