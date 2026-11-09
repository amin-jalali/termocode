//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The AI's database questions in the page (owner 2026-11-08: a paired
// control device is the owner; the page asks wherever the TUI asks).
// When an AI db_* tool waits on the user, the hub sends every controller
// page an ask ev with the connection and its need. The page shows the
// Databases view's own card in a sheet, on the phone and the desktop:
// an unknown SSH host key with its type and fingerprint (Cancel first
// and focused; Trust and save sends db.trust_host with exactly that key)
// or a masked field (db.secret with the typed value, the field emptied
// as it is sent). Cancel sends cards.answer cancel; an answer on the
// laptop (the answered ev) closes the sheet.

func aiDBAsk(id string, need webproto.DBNeed, body string) *webproto.Event {
	return &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAsk, Data: raw(webproto.AskCard{
		ID: id, Title: "AI chat · BaaytonaDB", Body: body, Action: "Save and connect", Conn: "baaytonadb", Need: &need})}
}

func aiDBAnswered(t *testing.T, c *HubConn, id, by string) {
	t.Helper()
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: id, By: by})})
}

func TestAIDBAskInPage(t *testing.T) {
	for _, vp := range []Viewport{Phone, Desktop} {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
			c := p.LoginMeta(hereMeta("c-aidb"))

			// An unknown SSH host key: Cancel first, then Trust and save
			// sends exactly the key shown.
			c.Send(t, aiDBAsk("ask-1", webproto.DBNeed{Need: "host_key", Host: "65.109.212.14", KeyType: "ED25519", Fingerprint: "SHA256:aidbKey1"},
				"Unknown SSH host 65.109.212.14. Check its key before you trust it."))
			p.WaitJS(`document.querySelector(".sheet .dbn-key")?.textContent === "ED25519 SHA256:aidbKey1" && document.activeElement?.textContent === "Cancel"`)
			if got := p.Text(".sheet .ask-note"); got != "AI chat · BaaytonaDB" {
				t.Fatalf("the sheet says %q", got)
			}
			if got := p.Text(".sheet .row-ask-text"); got != "Unknown SSH host 65.109.212.14." {
				t.Fatalf("the question is %q", got)
			}
			p.TapJS(`[...document.querySelectorAll(".sheet button")].find((b) => b.textContent === "Trust and save")`)
			reply(t, c, expectOp(t, c, webproto.OpDBTrustHost, `{"conn":"baaytonadb","fingerprint":"SHA256:aidbKey1"}`), webproto.DBDone{Text: "Host key of 65.109.212.14 saved."})
			aiDBAnswered(t, c, "ask-1", "phone")
			p.WaitJS(`!document.querySelector(".sheet .db-need")`)

			// The database's password: a masked field, focused; the value
			// goes once with db.secret and the field is emptied.
			const pw = "pw-e2e-aidb-1"
			c.Send(t, aiDBAsk("ask-2", webproto.DBNeed{Need: "password"}, "The database needs a password."))
			p.WaitJS(`document.activeElement?.matches('.sheet .db-need input[type="password"][autocomplete="off"]') && document.querySelector(".sheet .row-ask-text")?.textContent === "The database needs a password."`)
			p.Do(chromedp.SendKeys(`.sheet .db-need input[type="password"]`, pw+kb.Enter))
			r := expectOp(t, c, webproto.OpDBSecret, `{"conn":"baaytonadb","kind":"password","value":"`+pw+`"}`)
			if v := Eval[string](p, `document.querySelector('.sheet .db-need input[type="password"]')?.value ?? ""`); v != "" {
				t.Fatalf("the field still holds %q after it was sent", v)
			}
			reply(t, c, r, webproto.DBDone{Text: "Saved in the laptop's secret store."})
			aiDBAnswered(t, c, "ask-2", "phone")
			p.WaitJS(`!document.querySelector(".sheet .db-need")`)
			if Eval[bool](p, `document.body.innerHTML.includes(`+jsq(pw)+`)`) {
				t.Fatal("the password is still in the page")
			}

			// Cancel answers the card with cancel (the tool hears a cancel).
			c.Send(t, aiDBAsk("ask-3", webproto.DBNeed{Need: "ssh_passphrase", Key: "id_ed25519"}, "The SSH key id_ed25519 needs its passphrase."))
			p.WaitJS(`document.querySelector('.sheet .db-need input[type="password"]')?.getAttribute("aria-label") === "Passphrase for id_ed25519"`)
			p.TapJS(`[...document.querySelectorAll(".sheet .db-need button")].find((b) => b.textContent === "Cancel")`)
			pageAnswers(t, c, "ask-3", "cancel")
			p.WaitJS(`!document.querySelector(".sheet .db-need")`)

			// Answered on the laptop first: the sheet closes, nothing is sent.
			c.Send(t, aiDBAsk("ask-4", webproto.DBNeed{Need: "ssh_password"}, "The SSH server needs a password."))
			p.WaitJS(`!!document.querySelector('.sheet .db-need input[type="password"]')`)
			aiDBAnswered(t, c, "ask-4", "laptop")
			p.WaitJS(`!document.querySelector(".sheet .db-need")`)
			c.Quiet(t, 300*time.Millisecond)
			if pr := p.Problems(); len(pr) > 0 {
				t.Errorf("problems: %q", pr)
			}
		})
	}
}
