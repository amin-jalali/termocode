//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// SERVICES (WEBP-4) against the fake hub.

func svcRow(name string) string { return `document.querySelector('.svc[data-svc="` + name + `"]')` }

// Each service shows its state and ports; Start and Restart ask in the
// row (Cancel first and focused), then name the service; a local command
// the laptop never ran says the laptop asks; Stop goes through a laptop
// card; Logs shows the newest lines as text.
func TestServicesActions(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openRunView(t, vp, "services", "services", "services")
			c.Send(t, Snap("services", 1, servicesSnap()))
			p.WaitJS(`document.querySelectorAll(".svc").length === 3 && document.querySelectorAll(".svc-group").length === 2`)
			if got := Eval[string](p, svcRow("db")+`.querySelector(".tx-desc").textContent`); got != "healthy · 5432" {
				t.Errorf("db %q", got)
			}
			focus := func(name string) { Eval[bool](p, `(`+svcRow(name)+`.querySelector(".svc-row").focus(), true)`) }
			focus("api")
			p.TapJS(svcRow("api") + `.querySelector('[data-act="start"]')`)
			p.WaitJS(`document.activeElement?.textContent === "Cancel"`)
			p.TapJS(svcRow("api") + `.querySelector(".dbg-ask .btn-primary")`)
			reply(t, c, expectOp(t, c, webproto.OpServicesStart, `{"name":"api"}`), webproto.ServiceDone{})
			focus("web")
			p.TapJS(svcRow("web") + `.querySelector('[data-act="restart"]')`)
			p.TapJS(svcRow("web") + `.querySelector(".dbg-ask .btn-primary")`)
			reply(t, c, expectOp(t, c, webproto.OpServicesRestart, `{"name":"web"}`), webproto.ServiceDone{Asked: true})
			p.WaitJS(`document.body.textContent.includes("The laptop asks before this command runs.")`)
			focus("db")
			Eval[bool](p, `(`+svcRow("db")+`.querySelector('[data-act="logs"]').click(), true)`)
			reply(t, c, expectOp(t, c, webproto.OpServicesLogs, `{"name":"db"}`), webproto.ServiceLogs{Name: "db", Lines: []string{"ready to accept", "<i>x</i>"}})
			p.WaitJS(svcRow("db") + `.querySelectorAll(".svc-logs .trow").length === 2`)
			if Eval[int](p, `document.querySelectorAll(".svc-logs i").length`) != 0 {
				t.Fatal("logs became HTML")
			}
			focus("db")
			Eval[bool](p, `(`+svcRow("db")+`.querySelector('[data-act="stop"]').click(), true)`)
			p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
			p.TapJS(ButtonByText("Ask the laptop"))
			reply(t, c, expectOp(t, c, webproto.OpServicesStop, `{"name":"db"}`), webproto.AskRef{Card: "a-1"})
			if len(p.Problems()) > 0 {
				t.Errorf("problems: %v", p.Problems())
			}
		})
	}
}
