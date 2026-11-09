//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Pair a Device… and Devices… in the desktop page, with no step on the
// laptop: the command list and This device open them; the dialog shows
// the hub's QR code, the link with Copy link and how long it works; the
// phone's question shows its name and code, Not now first; the answer
// goes to the hub. Devices… lists the paired devices and Remove asks
// first, Cancel first. A viewer has neither.

const pairLink = "https://box.tail1234.ts.net/#p=s3cr3t-link-for-test"

func pairPalette() webproto.PaletteList {
	return webproto.PaletteList{Items: []webproto.PaletteItem{
		{ID: "web-pair", Title: "Web: Pair a Device…", Page: true},
		{ID: "web-devices", Title: "Web: Devices…", Page: true},
		{ID: "save-all", Title: "File: Save All Files"},
	}}
}

// openPairFromPalette runs Web: Pair a Device… from the command list and
// answers web.pair_open as the hub does; first: the list asks the laptop
// for its commands (the page keeps them a while).
func openPairFromPalette(t *testing.T, p *Page, c *HubConn, first bool) {
	t.Helper()
	wbKey(p, "P", true, true)
	if first {
		reply(t, c, expectOp(t, c, "palette.list", `{}`), pairPalette())
	}
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("pair a device")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Web: Pair a Device…"`)
	if got := Eval[string](p, `document.querySelector(".quick-row.is-on .quick-detail")?.textContent ?? ""`); got == "Laptop" {
		t.Error("Pair a Device… is marked as a laptop command; the page opens it")
	}
	p.Key("\r")
	reply(t, c, expectOp(t, c, "web.pair_open", `{}`), webproto.PairLink{Link: pairLink, QR: "M0 0h7v1h-7zM0 1h1v5h-1z", Size: 21, TTL: 120, Addr: "box.tail1234.ts.net"})
	p.WaitJS(`document.querySelector(".pair-link .pair-qr path")?.getAttribute("d") === "M0 0h7v1h-7zM0 1h1v5h-1z"`)
}

// pairAsk sends the page the phone's question.
func pairAsk(t *testing.T, c *HubConn) {
	t.Helper()
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventPairAsk, Data: raw(webproto.PairAsk{Name: "iPhone · Safari", Code: "482913"})})
}

func TestDeskPairADevice(t *testing.T) {
	p, c := deskLogin(t, nil)
	openPairFromPalette(t, p, c, true)
	if got := Eval[string](p, `document.querySelector(".pair-url").value`); got != pairLink {
		t.Errorf("link %q", got)
	}
	if got := p.Text(".pair-addr"); got != "box.tail1234.ts.net" {
		t.Errorf("address %q", got)
	}
	p.WaitJS(`/^Works for [12]:[0-5][0-9] more\.$/.test(document.querySelector(".pair-left")?.textContent ?? "")`)
	if !Eval[bool](p, `[...document.querySelectorAll(".pair-link button")].some((b) => b.textContent === "Copy link")`) {
		t.Error("no Copy link")
	}

	pairAsk(t, c)
	p.WaitJS(`document.querySelector(".pair-ask .pair-code")?.textContent === "482 913"`)
	if got := p.Text(".pair-ask"); !strings.Contains(got, "iPhone · Safari wants to pair") || !strings.Contains(got, "same code") {
		t.Errorf("question %q", got)
	}
	if got := Eval[string](p, `[...document.querySelectorAll(".pair-ask .choices button")].map((b) => b.textContent).join(",")`); got != "Not now,Pair as viewer,Pair with control" {
		t.Errorf("buttons %q", got)
	}
	p.WaitJS(`document.activeElement?.textContent === "Not now"`)
	// A press before the roles arm does nothing.
	p.TapJS(`[...document.querySelectorAll(".pair-ask button")].find((b) => b.textContent === "Pair with control")`)
	c.Quiet(t, 100*time.Millisecond)
	p.WaitJS(`!document.querySelector('.pair-ask button[aria-disabled="true"]')`)
	p.TapJS(`[...document.querySelectorAll(".pair-ask button")].find((b) => b.textContent === "Pair with control")`)
	reply(t, c, expectOp(t, c, "web.pair_answer", `{"role":"control"}`), nil)
	p.WaitJS(`(document.querySelector(".sheet .ask-text")?.textContent ?? "").includes("iPhone · Safari may pair now")`)
	p.TapJS(ButtonByText("Close"))
	p.WaitJS(`!document.querySelector(".sheet")`)
	c.Quiet(t, 200*time.Millisecond) // closed after the answer: nothing more to close
}

// Not now in the page answers Not now; Esc on the link closes it.
func TestDeskPairNotNowAndClose(t *testing.T) {
	p, c := deskLogin(t, nil)
	openPairFromPalette(t, p, c, true)
	pairAsk(t, c)
	p.WaitJS(`document.activeElement?.textContent === "Not now"`)
	p.Key(kb.Enter)
	reply(t, c, expectOp(t, c, "web.pair_answer", `{}`), nil)
	p.WaitJS(`(document.querySelector(".sheet .ask-text")?.textContent ?? "").includes("Not paired")`)
	p.Key(kb.Escape)
	p.WaitJS(`!document.querySelector(".sheet")`)

	openPairFromPalette(t, p, c, false)
	p.Key(kb.Escape)
	reply(t, c, expectOp(t, c, "web.pair_close", `{}`), nil)
	p.WaitJS(`!document.querySelector(".sheet")`)
}

// The laptop answers first: the page says so and asks nothing more.
func TestDeskPairAnsweredOnTheLaptop(t *testing.T) {
	p, c := deskLogin(t, nil)
	openPairFromPalette(t, p, c, true)
	pairAsk(t, c)
	p.WaitJS(`document.querySelector(".pair-ask")`)
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventPairEnd, Data: raw(webproto.PairEnd{Why: "answered", Role: "viewer"})})
	p.WaitJS(`(document.querySelector(".sheet .ask-text")?.textContent ?? "") === "Answered on the laptop: iPhone · Safari may pair now."`)
	if Eval[bool](p, `!!document.querySelector(".pair-ask")`) {
		t.Error("the question is still open")
	}
}

// This device holds Pair a Device… and Devices… for a controller;
// Devices… lists name, role and last seen, and Remove asks first (Cancel
// first) with the hub's text; its yes removes and the list comes again.
func TestDeskDevicesListAndRemove(t *testing.T) {
	p, c := deskLogin(t, hereMeta("c-desk"))
	Eval[bool](p, `(location.hash = "#/more", true)`)
	p.WaitJS(`document.querySelector('.wb-editor [data-view="more"] [data-group="devices"]')`)
	p.TapJS(ButtonByText("Devices…"))
	now := time.Now().Unix()
	devs := webproto.WebDeviceList{Items: []webproto.WebDevice{
		{ID: "dev-1a2b3c4d", Name: "iPad", Role: "control", Created: now - 86400, LastSeen: now - 600},
		{ID: "dev-0a0b0c0d", Name: "Desk", Role: "viewer", Live: true, Self: true, Created: now},
	}}
	reply(t, c, expectOp(t, c, "web.devices", `{}`), devs)
	p.WaitJS(`document.querySelectorAll(".pair-dev").length === 2`)
	if got := p.Text(`.pair-dev[data-id="dev-1a2b3c4d"] .pair-dev-facts`); got != "Control · last seen 10 min ago" {
		t.Errorf("iPad row %q", got)
	}
	if got := p.Text(`.pair-dev[data-id="dev-0a0b0c0d"] .pair-dev-facts`); got != "View only · this browser · connected now" {
		t.Errorf("own row %q", got)
	}
	p.TapJS(`document.querySelector('.pair-dev[data-id="dev-1a2b3c4d"] button')`)
	r := expectOp(t, c, "web.device_remove", `{"id":"dev-1a2b3c4d"}`)
	const body = "It is disconnected at once and must pair again to come back."
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAsk, Data: raw(webproto.AskCard{ID: "ask-1", Title: "Remove iPad?", Body: body, Action: "Remove"})})
	reply(t, c, r, webproto.AskRef{Card: "ask-1"})
	p.WaitJS(`[...document.querySelectorAll(".ask-text")].some((e) => e.textContent === ` + jsq(body) + `) && document.activeElement?.textContent === "Cancel"`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")?.textContent === "Remove"`)
	p.TapJS(`document.querySelector(".ask .arm.is-armed")`)
	pageAnswers(t, c, "ask-1", "confirm")
	reply(t, c, expectOp(t, c, "web.devices", `{}`), webproto.WebDeviceList{Items: devs.Items[1:]})
	p.WaitJS(`document.querySelectorAll(".pair-dev").length === 1`)

	// Pair a Device… from the list.
	p.TapJS(`[...document.querySelectorAll(".pair-devices button")].find((b) => b.textContent === "Pair a Device…")`)
	reply(t, c, expectOp(t, c, "web.pair_open", `{}`), webproto.PairLink{Link: pairLink, QR: "M0 0h1v1h-1z", Size: 21, TTL: 120, Addr: "box.tail1234.ts.net"})
	p.WaitJS(`document.querySelector(".pair-link .pair-qr")`)
}

// A viewer never sees them: no button on This device, no laptop list.
func TestDeskPairViewerHasNone(t *testing.T) {
	m := Meta("c-view")
	m.Role = webproto.RoleViewer
	p, c := deskLogin(t, m)
	Eval[bool](p, `(location.hash = "#/more", true)`)
	p.WaitJS(`document.querySelector('.wb-editor [data-view="more"] [data-group="link"]')`)
	if Eval[bool](p, `!!document.querySelector('[data-group="devices"]') || [...document.querySelectorAll("button")].some((b) => b.textContent === "Pair a Device…")`) {
		t.Error("a viewer sees Pair a Device…")
	}
	wbKey(p, "P", true, true)
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("pair")
	c.Quiet(t, 300*time.Millisecond)
	if Eval[bool](p, `[...document.querySelectorAll(".quick-label")].some((e) => e.textContent.includes("Pair a Device"))`) {
		t.Error("a viewer's command list offers Pair a Device…")
	}
}
