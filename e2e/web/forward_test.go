//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Port preview from the page (WEB-54). A localhost link in the chat or in
// a terminal row never opens as it is (on a phone, localhost is the
// phone): its sheet offers Forward port, Cancel first and focused. That
// sends web.forward with the port only; the hub's card asks (here or on
// the laptop). When fwd.ready comes, the sheet offers the hub's address
// (the link's path kept) as a real link to a new tab, no opener, no
// referrer. In SSH mode termocode ssh opens the tab itself: a toast says
// so.

const fwdAsk = `document.querySelector(".sheet .link-ask")`

// forwardFromSheet taps Forward port in the open sheet and answers the
// request with the card id, then fwd.ready.
func forwardFromSheet(t *testing.T, p *Page, c *HubConn, ready webproto.FwdReady) {
	t.Helper()
	p.WaitJS(`document.activeElement?.textContent.trim() === "Cancel"`)
	if n := Eval[int](p, fwdAsk+`.querySelectorAll('a[href*="localhost"]').length`); n != 0 {
		t.Fatal("the sheet offers to open localhost as it is")
	}
	p.TapJS(`[...` + fwdAsk + `.querySelectorAll("button")].find((b) => b.textContent === "Forward port")`)
	r := expectOp(t, c, webproto.OpWebForward, `{"port":3000}`)
	reply(t, c, r, webproto.AskRef{Card: "ask-1"})
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventFwdReady, Data: raw(ready)})
}

func TestChatLocalhostLinkAsksForward(t *testing.T) {
	link := md("link", mdText("app"))
	link["href"] = "http://localhost:3000/docs?x=1"
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openChat(t, vp, chatWith(false, aiMsg(0, md("doc", md("p", link)))))
			p.TapJS(chatView + `.querySelector("a.md-link")`)
			p.WaitJS(fwdAsk + `?.innerText.toLowerCase().includes("port 3000")`)
			forwardFromSheet(t, p, c, webproto.FwdReady{Port: 3000, Mode: "tailnet", URL: "https://box.tail1234.ts.net:8443/"})
			const want = "https://box.tail1234.ts.net:8443/docs?x=1"
			p.WaitJS(fwdAsk + `?.querySelector("a.btn")?.getAttribute("href") === ` + jsq(want))
			open := Eval[[]string](p, `(() => { const a = `+fwdAsk+`.querySelector("a.btn"); return [a.getAttribute("target"), a.getAttribute("rel")]; })()`)
			if open[0] != "_blank" || !strings.Contains(open[1], "noopener") || !strings.Contains(open[1], "noreferrer") {
				t.Errorf("the Open link is %q", open)
			}
			for _, u := range p.Requests() {
				if strings.Contains(u, "localhost:3000") || strings.Contains(u, "8443") {
					t.Fatalf("the page went to %s by itself", u)
				}
			}
		})
	}
}

// In SSH mode the tab opens on the user's computer: a toast says so and no
// sheet waits.
func TestLocalhostLinkSSHModeToast(t *testing.T) {
	link := md("link", mdText("app"))
	link["href"] = "http://127.0.0.1:3000/"
	p, c := openChat(t, Phone, chatWith(false, aiMsg(0, md("doc", md("p", link)))))
	p.TapJS(chatView + `.querySelector("a.md-link")`)
	p.WaitJS(fwdAsk + `?.innerText.toLowerCase().includes("port 3000")`)
	forwardFromSheet(t, p, c, webproto.FwdReady{Port: 3000, Mode: "ssh"})
	p.WaitJS(`!` + fwdAsk + ` && (` + toastsJS + `).includes("Port 3000")`)
}

// A viewer can not forward: the sheet says so, with no Forward button.
func TestLocalhostLinkViewerCannotForward(t *testing.T) {
	link := md("link", mdText("app"))
	link["href"] = "http://localhost:3000/"
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	m := Meta("v-1")
	m.Role = webproto.RoleViewer
	c := p.LoginMeta(m)
	c.Send(t, Snap("chat", 1, chatWith(false, aiMsg(0, md("doc", md("p", link))))))
	p.WaitJS(chatView + `?.querySelector("a.md-link")`)
	p.TapJS(chatView + `.querySelector("a.md-link")`)
	p.WaitJS(fwdAsk + `?.innerText.toLowerCase().includes("port 3000")`)
	if Eval[bool](p, `[...`+fwdAsk+`.querySelectorAll("button")].some((b) => b.textContent === "Forward port")`) {
		t.Fatal("a viewer is offered Forward port")
	}
	c.Quiet(t, 200*time.Millisecond)
}

// A long press (a right click on the desktop) on a terminal row with a
// localhost link offers it; picking it asks the same sheet.
func TestTermRowLocalhostLink(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openTerm(t, vp)
			showTerm(t, p, c, termSnap(3, 0, 80, "$ npm run dev", "  Local:   http://localhost:3000/"), termItem(3, "zsh", false, true))
			row := `[...document.querySelectorAll('[data-view="term"] .trow')].find((r) => r.textContent.includes("Local:"))`
			if vp.Mobile {
				p.press(row, rowMenu)
			} else {
				pt := Eval[struct{ X, Y float64 }](p, `(() => { const r = (`+row+`).getBoundingClientRect(); return {X: r.left + 8, Y: r.top + r.height / 2}; })()`)
				p.Do(chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonRight))
				p.WaitJS(rowMenu)
			}
			p.TapJS(ButtonByText("Open http://localhost:3000/"))
			p.WaitJS(fwdAsk + `?.innerText.toLowerCase().includes("port 3000")`)
		})
	}
}
