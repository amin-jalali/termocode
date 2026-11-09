//go:build unix

package app

import (
	"crypto/sha256"
	"encoding/json"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// Pair a Device… and Devices… in the web page (desktop web), with no step
// on the laptop: the page opens the link, sees the phone's code and
// answers; the laptop's card still works, and the first answer closes
// the other.

const (
	pairPageOpen   = `{"t":"open","conn":"c-1","role":"controller","device":"dev-0a0b0c0d","name":"Desk"}`
	pairPageOther  = `{"t":"open","conn":"c-2","role":"controller","device":"dev-0e0f0a0b","name":"Other"}`
	pairPageViewer = `{"t":"open","conn":"c-3","role":"viewer","device":"dev-0c0d0e0f","name":"Watcher"}`
)

// pairPageRig is an access rig whose pages approve their own actions.
func pairPageRig(t *testing.T) *accessRig {
	t.Helper()
	r := newAccessRig(t)
	r.hub.approveOnLaptop = false
	return r
}

// reqLine is one request line of a page.
func reqLine(id int, op, args string) string {
	return `{"t":"req","id":` + itoaApp(id) + `,"op":"` + op + `","args":` + args + `}`
}

// pageAnswer waits for the res or err of request id on c, keeping every
// line it passes in seen.
func (r *accessRig) pageAnswer(c *webClient, id int, seen *[]string) map[string]any {
	r.t.Helper()
	for {
		line, ok := r.next(c)
		if !ok {
			r.t.Fatal("connection closed")
		}
		if seen != nil {
			*seen = append(*seen, line)
		}
		m := decodeLine(r.t, line)
		if (m["t"] == "res" || m["t"] == "err") && m["id"] == float64(id) {
			return m
		}
	}
}

// pageEvent waits for the next ev of kind on c.
func (r *accessRig) pageEvent(c *webClient, kind string) map[string]any {
	r.t.Helper()
	for {
		m := r.nextMsg(c)
		if m["t"] == "ev" && m["kind"] == kind {
			d, _ := m["data"].(map[string]any)
			return d
		}
	}
}

// ctlAnswer waits for the gateway's answer to its pair_ask id.
func (r *accessRig) ctlAnswer(id int) map[string]any {
	r.t.Helper()
	for {
		m := r.nextMsg(r.ctl)
		if (m["t"] == "res" || m["t"] == "err") && m["id"] == float64(id) {
			return m
		}
	}
}

// pairOpenPage opens a pairing link from page c: its answer and the
// gateway's pair event.
func (r *accessRig) pairOpenPage(c *webClient, id int) (res map[string]any, ev map[string]any) {
	r.t.Helper()
	c.send(reqLine(id, "web.pair_open", `{}`))
	res = r.pageAnswer(c, id, nil)
	if res["t"] != "res" {
		r.t.Fatalf("web.pair_open: %v", res)
	}
	return res, r.ctlEvent("pair")
}

// Spec: Pair a Device… from the page: the link (whose digest the gateway
// got, for the laptop's 2 minutes), its QR code and the address go to
// that page; nothing opens on the laptop. When the phone asks, the page
// gets its name and code and the laptop its card; the page's answer
// pairs with that role and closes the laptop's card.
func TestWebPairFromThePage(t *testing.T) {
	r := pairPageRig(t)
	page, _ := r.webHello(pairPageOpen)
	res, ev := r.pairOpenPage(page, 1)
	d, _ := res["data"].(map[string]any)
	link, _ := d["link"].(string)
	plain, ok := strings.CutPrefix(link, "https://box.tail1234.ts.net/#p=")
	sum := sha256.Sum256([]byte(plain))
	if !ok || hex.EncodeToString(sum[:]) != ev["sha256"] || ev["ttl_s"] != float64(120) || ev["guest"] == true {
		t.Fatalf("link %q does not match the gateway's pair event %v", link, ev)
	}
	if d["ttl_s"] != float64(120) || d["addr"] != "box.tail1234.ts.net" {
		t.Fatalf("pair_open %v", d)
	}
	if qr, _ := d["qr"].(string); !strings.HasPrefix(qr, "M") || d["size"].(float64) < 21 {
		t.Fatalf("no QR code: %v", d)
	}
	if r.m.confirmOpen {
		t.Fatal("the laptop shows a card for the page's link")
	}

	r.ctl.send(pairAskLine(7, false))
	ask := r.pageEvent(page, "pair.ask")
	if ask["name"] != "iPhone · Safari" || ask["code"] != "482913" || ask["laptop"] == true {
		t.Fatalf("pair.ask %v", ask)
	}
	r.pumpUntil("the laptop's card", func() bool { return r.m.confirmOpen && r.m.confirmKind == confirmKindWebPairAsk })

	page.send(reqLine(2, "web.pair_answer", `{"role":"control"}`))
	if a := r.pageAnswer(page, 2, nil); a["t"] != "res" {
		t.Fatalf("page answer %v", a)
	}
	if a := r.ctlAnswer(7); a["t"] != "res" || a["data"].(map[string]any)["role"] != "control" {
		t.Fatalf("gateway answer %v", a)
	}
	if r.m.confirmOpen {
		t.Fatal("the laptop's card is still open after the page answered")
	}
	if !strings.Contains(webTestToasts(*r.m), "Approved on") {
		t.Errorf("laptop toasts %q", webTestToasts(*r.m))
	}
}

// Spec: the laptop answers first: the gateway gets its role, the page
// hears pair.end with it, and a late answer from the page is gone.
func TestWebPairLaptopAnswersFirst(t *testing.T) {
	r := pairPageRig(t)
	page, _ := r.webHello(pairPageOpen)
	r.pairOpenPage(page, 1)
	r.ctl.send(pairAskLine(7, false))
	r.pageEvent(page, "pair.ask")
	r.pumpUntil("the laptop's card", func() bool { return r.m.confirmOpen && r.m.confirmKind == confirmKindWebPairAsk })
	webAccessSelect(r.m, "viewer")
	if a := r.ctlAnswer(7); a["data"].(map[string]any)["role"] != "viewer" {
		t.Fatalf("gateway answer %v", a)
	}
	if end := r.pageEvent(page, "pair.end"); end["why"] != "answered" || end["role"] != "viewer" {
		t.Fatalf("pair.end %v", end)
	}
	page.send(reqLine(2, "web.pair_answer", `{"role":"control"}`))
	if a := r.pageAnswer(page, 2, nil); a["t"] != "err" || a["code"] != "gone" {
		t.Fatalf("late answer %v", a)
	}
}

// Spec: Not now in the page refuses the phone and closes the laptop's
// card.
func TestWebPairPageNotNow(t *testing.T) {
	r := pairPageRig(t)
	page, _ := r.webHello(pairPageOpen)
	r.pairOpenPage(page, 1)
	r.ctl.send(pairAskLine(7, false))
	r.pageEvent(page, "pair.ask")
	page.send(reqLine(2, "web.pair_answer", `{}`))
	r.pageAnswer(page, 2, nil)
	if a := r.ctlAnswer(7); a["t"] != "err" || a["code"] != "forbidden" || !strings.Contains(a["msg"].(string), "Not now") {
		t.Fatalf("gateway answer %v", a)
	}
	if r.m.confirmOpen {
		t.Fatal("the laptop's card is still open")
	}
}

// Spec: a laptop busy with another card does not stop the page: the
// page asks, the other card stays.
func TestWebPairPageWhileTheLaptopIsBusy(t *testing.T) {
	r := pairPageRig(t)
	page, _ := r.webHello(pairPageOpen)
	r.pairOpenPage(page, 1)
	r.m.showWebStartCard(webSettings(), nil)
	r.ctl.send(pairAskLine(7, false))
	r.pageEvent(page, "pair.ask")
	page.send(reqLine(2, "web.pair_answer", `{"role":"viewer"}`))
	r.pageAnswer(page, 2, nil)
	if a := r.ctlAnswer(7); a["t"] != "res" || a["data"].(map[string]any)["role"] != "viewer" {
		t.Fatalf("gateway answer %v", a)
	}
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebStart {
		t.Fatal("the laptop's other card was closed or replaced")
	}
}

// Security: the link (its secret) goes only to the page that asked:
// never to another page, the gateway (only the digest) or the audit.
// Another page cannot answer the question.
func TestWebPairLinkOnlyInItsPage(t *testing.T) {
	r := pairPageRig(t)
	page, _ := r.webHello(pairPageOpen)
	other, _ := r.webHello(pairPageOther)
	res, _ := r.pairOpenPage(page, 1)
	link := res["data"].(map[string]any)["link"].(string)
	plain := link[strings.Index(link, "#p=")+3:]
	r.ctl.send(pairAskLine(7, false))
	r.pageEvent(page, "pair.ask")
	var seen []string
	other.send(reqLine(1, "web.pair_answer", `{"role":"control"}`))
	if a := r.pageAnswer(other, 1, &seen); a["t"] != "err" {
		t.Fatalf("another page answered: %v", a)
	}
	for _, l := range seen {
		if strings.Contains(l, "pair.ask") || strings.Contains(l, plain) {
			t.Fatalf("another page got %s", l)
		}
	}
	page.send(reqLine(2, "web.pair_answer", `{"role":"viewer"}`))
	r.pageAnswer(page, 2, nil)
	for {
		line, _ := r.next(r.ctl)
		if strings.Contains(line, plain) {
			t.Fatalf("the gateway got the secret: %s", line)
		}
		if m := decodeLine(t, line); m["id"] == float64(7) {
			break
		}
	}
}

// Spec: a page that leaves (or closes its dialog) closes its unused link
// at the gateway.
func TestWebPairPageLeavesClosesItsLink(t *testing.T) {
	r := pairPageRig(t)
	page, _ := r.webHello(pairPageOpen)
	r.pairOpenPage(page, 1)
	page.send(reqLine(2, "web.pair_close", `{}`))
	r.pageAnswer(page, 2, nil)
	r.ctlEvent("pair_close")

	r.pairOpenPage(page, 3)
	_ = page.c.Close()
	r.ctlEvent("pair_close")
}

// Spec: the laptop's own Pair a Device… replaces the page's link: the
// page hears pair.end (closed).
func TestWebPairLaptopLinkEndsThePagesLink(t *testing.T) {
	r := pairPageRig(t)
	page, _ := r.webHello(pairPageOpen)
	r.pairOpenPage(page, 1)
	r.m.openWebPair(false)
	if end := r.pageEvent(page, "pair.end"); end["why"] != "closed" {
		t.Fatalf("pair.end %v", end)
	}
}

// Spec: on the loopback panel a phone cannot reach the laptop: the page
// is told what to set, and no link opens.
func TestWebPairFromThePageNeedsLANOrTailnet(t *testing.T) {
	r := pairPageRig(t)
	r.m.webPanel.url = ""
	page, _ := r.webHello(pairPageOpen)
	page.send(reqLine(1, "web.pair_open", `{}`))
	if a := r.pageAnswer(page, 1, nil); a["t"] != "err" || !strings.Contains(a["msg"].(string), "web_listen") {
		t.Fatalf("answer %v", a)
	}
}

// Security: a viewer may open, answer, close, list or remove nothing,
// and its palette offers neither command; a controller's palette offers
// both, for the page to open itself.
func TestWebPairViewerGetsErrors(t *testing.T) {
	r := pairPageRig(t)
	viewer, _ := r.webHello(pairPageViewer)
	for i, op := range []string{"web.pair_open", "web.pair_close", "web.pair_answer", "web.devices"} {
		viewer.send(reqLine(i+1, op, `{}`))
		if a := r.pageAnswer(viewer, i+1, nil); a["t"] != "err" || a["code"] != "forbidden" {
			t.Errorf("%s from a viewer: %v", op, a)
		}
	}
	viewer.send(reqLine(9, "web.device_remove", `{"id":"dev-1a2b3c4d"}`))
	if a := r.pageAnswer(viewer, 9, nil); a["t"] != "err" || a["code"] != "forbidden" {
		t.Errorf("web.device_remove from a viewer: %v", a)
	}
	viewer.send(reqLine(10, "palette.list", `{}`))
	if a := r.pageAnswer(viewer, 10, nil); strings.Contains(jsonText(t, a), "web-pair") || strings.Contains(jsonText(t, a), "web-devices") {
		t.Errorf("a viewer's palette: %v", a)
	}
	page, _ := r.webHello(pairPageOpen)
	page.send(reqLine(1, "palette.list", `{}`))
	got := jsonText(t, r.pageAnswer(page, 1, nil))
	for _, want := range []string{`{"id":"web-pair","page":true,"title":"Web: Pair a Device…"}`, `{"id":"web-devices","page":true,"title":"Web: Devices…"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("a controller's palette has no %s: %s", want, got)
		}
	}
}

// Spec: Devices… in the page lists the paired devices (name, role, last
// seen, this page's own); Remove asks in the page (Cancel first) with the
// laptop card's text, and its yes removes the device and drops it at the
// gateway.
func TestWebDevicesFromThePage(t *testing.T) {
	r := pairPageRig(t)
	dir, _ := webDevicesDir()
	st := webdevices.Open(dir)
	seen := time.Unix(1_800_000_000, 0)
	for _, d := range []webdevices.Device{
		{ID: "dev-1a2b3c4d", Name: "iPad", Role: webdevices.RoleControl, Created: seen, LastSeen: seen},
		{ID: "dev-0a0b0c0d", Name: "Desk", Role: webdevices.RoleViewer, Created: seen},
	} {
		if err := st.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	page, _ := r.webHello(pairPageOpen)
	page.send(reqLine(1, "web.devices", `{}`))
	got := jsonText(t, r.pageAnswer(page, 1, nil))
	for _, want := range []string{`"id":"dev-1a2b3c4d","last_seen":1800000000,"name":"iPad","role":"control"`, `"id":"dev-0a0b0c0d","live":true,"name":"Desk","role":"viewer","self":true`} {
		if !strings.Contains(got, want) {
			t.Errorf("devices have no %s: %s", want, got)
		}
	}
	page.send(reqLine(2, "web.device_remove", `{"id":"dev-1a2b3c4d"}`))
	ask := r.pageEvent(page, "ask")
	if ask["title"] != "Remove iPad?" || ask["body"] != "It is disconnected at once and must pair again to come back." || ask["action"] != "Remove" {
		t.Fatalf("ask %v", ask)
	}
	if r.m.confirmOpen {
		t.Fatal("the laptop asks too")
	}
	page.send(reqLine(3, "cards.answer", `{"id":"`+ask["id"].(string)+`","choice":"confirm"}`))
	if ev := r.ctlEvent("revoke"); ev["device"] != "dev-1a2b3c4d" {
		t.Fatalf("revoke %v", ev)
	}
	if _, ok, _ := st.Get("dev-1a2b3c4d"); ok {
		t.Fatal("still in the store")
	}
	page.send(reqLine(4, "web.device_remove", `{"id":"dev-1a2b3c4d"}`))
	if a := r.pageAnswer(page, 4, nil); a["t"] != "err" || a["code"] != "not_found" {
		t.Fatalf("removing it again: %v", a)
	}
}

func decodeLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(line), &v); err != nil {
		t.Fatalf("bad line %q: %v", line, err)
	}
	return v
}

func jsonText(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
