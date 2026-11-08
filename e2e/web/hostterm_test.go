//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Open Terminal on a host row (CONTAINERS and Databases' SERVERS, the
// desktop's right-click menu and hover button, the phone's host rows)
// asks the laptop (Cancel first), then the laptop opens the terminal tab:
// ssh to an ssh:// host, a plain shell for this machine.

func TestDeskHostOpenTerminal(t *testing.T) {
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/containers", true)`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), hostsCtrHost())
	const remote = deskCtr + `.querySelector('[data-section="containers"] .tr[data-context="baaytona"]')`
	const local = deskCtr + `.querySelector('[data-section="containers"] .tr[data-context="default"]')`
	p.WaitJS(remote + ` !== null`)

	rightClick(p, remote)
	if got := menuLabels(p); !strings.Contains(got, "Open Terminal…") {
		t.Fatalf("remote host menu %q has no Open Terminal…", got)
	}
	menuPick(p, "Open Terminal…")
	askLaptop(t, p, "Open a terminal on baaytona (deploy@65.109.212.14)? It runs there.")
	reply(t, c, expectOp(t, c, webproto.OpContainersHostTerminal, `{"name":"baaytona"}`), webproto.AskRef{Card: "card-1"})
	laptopSays(t, c, "card-1", "confirm") // the laptop opened it; the view reads its lists again
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), hostsCtrHost())

	Eval[bool](p, local+`.querySelector('[data-act="terminal"]').click() ?? true`)
	askLaptop(t, p, "Open a shell in the project folder?")
	reply(t, c, expectOp(t, c, webproto.OpContainersHostTerminal, `{"name":"default"}`), webproto.AskRef{Card: "card-2"})
	laptopSays(t, c, "card-2", "confirm")
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), hostsCtrHost())

	// Databases' SERVERS: the same on its host rows.
	Eval[bool](p, `(location.hash = "#/more/db", true)`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), webproto.DBConns{Conns: []webproto.DBConn{}})
	p.WaitJS(dbxSide + `?.querySelector('.tr[data-host="baaytona"] [data-act="terminal"]') != null`)
	Eval[bool](p, dbxSide+`.querySelector('.tr[data-host="baaytona"] [data-act="terminal"]').click() ?? true`)
	askLaptop(t, p, "Open a terminal on baaytona (deploy@65.109.212.14)? It runs there.")
	reply(t, c, expectOp(t, c, webproto.OpContainersHostTerminal, `{"name":"baaytona"}`), webproto.AskRef{Card: "card-3"})
}

func TestPhoneHostOpenTerminal(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.LoginMeta(Meta("c-ctr"))
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="containers"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersServers, `{}`), hostsList())
	p.WaitJS(`!!document.querySelector('.srv-host[data-host="baaytona"]')`)
	p.TapJS(`document.querySelector('.srv-host[data-host="baaytona"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersServer, `{"name":"baaytona"}`), baaytonaCtrs())
	p.WaitJS(`!!document.querySelector('[data-host-term="baaytona"] button')`)
	p.TapJS(`document.querySelector('[data-host-term="baaytona"] button')`)
	askLaptop(t, p, "Open a terminal on baaytona (deploy@65.109.212.14)? It runs there.")
	reply(t, c, expectOp(t, c, webproto.OpContainersHostTerminal, `{"name":"baaytona"}`), webproto.AskRef{Card: "card-1"})
	laptopSays(t, c, "card-1", "cancel")
	p.WaitJS(`!!document.querySelector('[data-host-term="default"] button')`)
	p.TapJS(`document.querySelector('[data-host-term="default"] button')`)
	askLaptop(t, p, "Open a shell in the project folder?")
	reply(t, c, expectOp(t, c, webproto.OpContainersHostTerminal, `{"name":"default"}`), webproto.AskRef{Card: "card-2"})
	p.noOverflow("host terminal")
}

// laptopSays answers a laptop card as the laptop.
func laptopSays(t *testing.T, c *HubConn, card, choice string) {
	t.Helper()
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: card, By: "laptop", Choice: choice})})
}
