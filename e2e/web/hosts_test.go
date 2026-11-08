//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The owner's path, desktop and phone: a docker context baaytona
// (ssh://deploy@65.109.212.14) whose Postgres container publishes no
// port. Containers shows every host as "name · where" and opens one to
// its compose projects and containers (read once; a host that does not
// answer says why under its own row); + Add Server… asks a name, then
// ssh://user@host, then in the page (Cancel first). Databases' SERVERS
// lists the ssh:// hosts and their database containers; picking
// baaytona-db-1 adds it, the first read asks to trust the unknown host,
// the next asks for the database password (masked, sent once), and the
// tables are listed.

const hostPGPassword = "pg-" + "pw-61"

func hostsCtrHost() webproto.ContainersHost {
	h := ctrHost()
	h.Contexts = []webproto.ContextItem{
		{Name: "default", Current: true, Local: true},
		{Name: "baaytona", Where: "deploy@65.109.212.14", SSH: true},
		{Name: "broken", Where: "ops@10.9.9.9:2222", SSH: true},
	}
	return h
}

func hostsList() webproto.ServersList {
	return webproto.ServersList{Engine: "docker", Hosts: []webproto.ServerHost{
		{Name: "default", Local: true, Current: true},
		{Name: "baaytona", Where: "deploy@65.109.212.14", SSH: true},
		{Name: "broken", Where: "ops@10.9.9.9:2222", SSH: true},
	}}
}

func baaytonaCtrs() webproto.ServerContainers {
	return webproto.ServerContainers{Name: "baaytona", Containers: []webproto.ContainerItem{
		{ID: "c0ffee01aa00", Name: "baaytona-db-1", Image: "postgres:16", State: "running", Status: "Up 3 days", Project: "baaytona", DB: "postgres"},
		{ID: "c0ffee03aa00", Name: "baaytona-web-1", Image: "baaytona/web:latest", State: "running", Status: "Up 3 days", Project: "baaytona"},
	}}
}

// hostFail answers r with a host that does not answer.
func hostFail(t *testing.T, c *HubConn, r *webproto.Req) {
	t.Helper()
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeConflict, Msg: "ssh: Connection timed out"})
}

var (
	hostKeyNeed = map[string]any{"need": "host_key", "host": "65.109.212.14", "fingerprint": "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8", "key_type": "ED25519"}
	pgConn      = webproto.DBConns{Conns: []webproto.DBConn{{ID: "postgres", Name: "baaytona-db-1", Driver: "postgres", Database: "postgres", SQL: true}}}
)

func TestDeskHostsOwnerPath(t *testing.T) {
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/containers", true)`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), hostsCtrHost())
	p.WaitJS(deskCtr + `?.querySelectorAll(".tr[data-id]").length === 2`)

	// CONTAINERS: every host at once, "name · where" (no CONTEXTS section).
	label := func(name string) string {
		return Eval[string](p, deskCtr+`.querySelector('.tr[data-context="`+name+`"] .tr-label')?.textContent ?? ""`)
	}
	p.WaitJS(deskCtr + `.querySelector('.tr[data-context="broken"]') !== null`)
	for name, want := range map[string]string{"default": "default · this machine", "baaytona": "baaytona · deploy@65.109.212.14", "broken": "broken · ops@10.9.9.9:2222"} {
		if got := label(name); got != want {
			t.Errorf("host %s reads %q, want %q", name, got, want)
		}
	}
	// A host opens to its compose projects and containers, read once.
	p.TapJS(deskCtr + `.querySelector('.tr[data-context="baaytona"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersServer, `{"name":"baaytona"}`), baaytonaCtrs())
	p.WaitJS(deskCtr + `.querySelector('.tr[data-host-project="baaytona/baaytona"]') && ` + deskCtr + `.querySelector('.tr[data-host-ctr="c0ffee01aa00"]') && ` + deskCtr + `.querySelector('.tr[data-host-ctr="c0ffee03aa00"]')`)
	// A host that does not answer says why under its own row; the others stay.
	p.TapJS(deskCtr + `.querySelector('.tr[data-context="broken"]')`)
	hostFail(t, c, expectOp(t, c, webproto.OpContainersServer, `{"name":"broken"}`))
	p.WaitJS(deskCtr + `.querySelector('[data-host-err="broken"]')?.textContent === "ssh: Connection timed out" && !!` + deskCtr + `.querySelector('.tr[data-host-ctr="c0ffee01aa00"]')`)
	p.TapJS(deskCtr + `.querySelector('.tr[data-context="baaytona"]')`)
	p.WaitJS(`!` + deskCtr + `.querySelector('.tr[data-host-ctr="c0ffee01aa00"]')`)
	p.TapJS(deskCtr + `.querySelector('.tr[data-context="baaytona"]')`)
	p.WaitJS(deskCtr + `.querySelector('.tr[data-host-ctr="c0ffee01aa00"]') !== null`)
	c.Quiet(t, 200*time.Millisecond) // read once

	// + Add Server…: a name, then ssh://user@host, then Cancel first.
	p.TapJS(deskCtr + `.querySelector('.tr[data-add-server]')`)
	p.WaitJS(`document.activeElement === ` + deskCtr + `.querySelector('.ctr-form input')`)
	setInput(p, deskCtr+`.querySelector('.ctr-form input')`, "box2")
	p.Key(kb.Enter)
	p.WaitJS(deskCtr + `.querySelector('.ctr-form input')?.value === "ssh://" && document.activeElement === ` + deskCtr + `.querySelector('.ctr-form input')`)
	setInput(p, deskCtr+`.querySelector('.ctr-form input')`, "ssh://deploy@10.0.0.2")
	p.Key(kb.Enter)
	p.WaitJS(deskCtr + `.querySelector(".pv-ask")?.textContent.includes("Add server box2 (ssh://deploy@10.0.0.2) for every terminal") && document.activeElement?.textContent === "Cancel"`)
	c.Quiet(t, 200*time.Millisecond)
	p.TapJS(`[...` + deskCtr + `.querySelectorAll(".pv-ask button")].find((b) => b.textContent === "Add Server")`)
	reply(t, c, expectOp(t, c, webproto.OpContainersAddServer, `{"name":"box2","host":"ssh://deploy@10.0.0.2"}`), webproto.HostDone{})
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), hostsCtrHost())

	// Databases › SERVERS: the same hosts (ssh only), database containers only.
	Eval[bool](p, `(location.hash = "#/more/db", true)`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), webproto.DBConns{Conns: []webproto.DBConn{}})
	p.WaitJS(dbxSide + `?.querySelector('.tr[data-servers]') !== null`)
	p.WaitJS(dbxSide + `.querySelector(".tr[data-servers]").getAttribute("aria-expanded") === "true"`) // open: there are ssh hosts
	p.WaitJS(dbxSide + `.querySelector('.tr[data-host="baaytona"]') && ` + dbxSide + `.querySelector('.tr[data-host="broken"]') && !` + dbxSide + `.querySelector('.tr[data-host="default"]')`)
	p.TapJS(dbxSide + `.querySelector('.tr[data-host="baaytona"]')`)
	p.WaitJS(dbxSide + `.querySelector('.tr[data-host-ctr="c0ffee01aa00"]') && !` + dbxSide + `.querySelector('.tr[data-host-ctr="c0ffee03aa00"]')`)
	c.Quiet(t, 200*time.Millisecond) // one host list, one cache

	// Pick it: added, then trust the host, then the password, then the tables.
	p.TapJS(dbxSide + `.querySelector('.tr[data-host-ctr="c0ffee01aa00"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBAddContainer, `{"host":"baaytona","id":"c0ffee01aa00"}`), webproto.DBAdded{Conn: "postgres", Name: "baaytona-db-1"})
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), pgConn)
	dbFail(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"postgres"}`), "Unknown SSH host 65.109.212.14. Check its key before you trust it.", hostKeyNeed)
	const ask = dbxSide + `.querySelector('.db-need')`
	p.WaitJS(ask + `?.textContent.includes("65.109.212.14") && document.activeElement?.textContent === "Cancel"`)
	p.TapJS(`[...` + ask + `.querySelectorAll("button")].find((b) => b.textContent === "Trust and save")`)
	reply(t, c, expectOp(t, c, webproto.OpDBTrustHost, `{"conn":"postgres","fingerprint":"SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8"}`), webproto.DBDone{Text: "Host key of 65.109.212.14 saved."})
	dbFail(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"postgres"}`), "The database needs a password.", map[string]any{"need": "password"})
	p.WaitJS(`document.activeElement?.type === "password"`)
	p.Do(chromedp.SendKeys(`.wb-side [data-view="db"] .db-need input[type="password"]`, hostPGPassword+kb.Enter))
	reply(t, c, expectOp(t, c, webproto.OpDBSecret, `{"conn":"postgres","kind":"password","value":"`+hostPGPassword+`"}`), webproto.DBDone{Text: "Saved in the laptop's secret store."})
	reply(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"postgres"}`), webproto.DBTables{Conn: "postgres", Schema: "public", Tables: []webproto.DBTable{{Name: "users"}}})
	p.WaitJS(dbxSide + `.querySelector('.tr[data-table="users"]') && !` + ask)
	if Eval[bool](p, `document.body.innerHTML.includes(`+jsq(hostPGPassword)+`)`) {
		t.Error("the page keeps the password")
	}
	p.noOverflow("databases servers")
}

func TestPhoneHostsOwnerPath(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.LoginMeta(Meta("c-ctr"))
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="containers"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	// The hosts are in the tree at once (no Show).
	reply(t, c, expectOp(t, c, webproto.OpContainersServers, `{}`), hostsList())
	p.WaitJS(`document.querySelector('.srv-host[data-host="baaytona"] .db-name')?.textContent === "baaytona · deploy@65.109.212.14" && document.querySelector('.srv-host[data-host="default"] .db-name')?.textContent === "default · this machine"`)
	p.TapJS(`document.querySelector('.srv-host[data-host="baaytona"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersServer, `{"name":"baaytona"}`), baaytonaCtrs())
	p.WaitJS(`document.querySelector('[data-host-project="baaytona/baaytona"]') && document.querySelector('[data-host-ctr="c0ffee01aa00"]') && document.querySelector('[data-host-ctr="c0ffee03aa00"]')`)
	p.TapJS(`document.querySelector('.srv-host[data-host="broken"]')`)
	hostFail(t, c, expectOp(t, c, webproto.OpContainersServer, `{"name":"broken"}`))
	p.WaitJS(`document.querySelector('[data-host-err="broken"]')?.textContent === "ssh: Connection timed out" && !!document.querySelector('[data-host-ctr="c0ffee01aa00"]')`)

	// + Add Server…: name and ssh://user@host, then Cancel first.
	p.TapJS(ButtonByText("+ Add Server…"))
	p.WaitJS(`document.activeElement === document.querySelector('.srv-add input')`)
	setInput(p, `document.querySelectorAll('.srv-add input')[0]`, "box2")
	setInput(p, `document.querySelectorAll('.srv-add input')[1]`, "ssh://deploy@10.0.0.2")
	p.TapJS(`document.querySelector('.srv-add button[type="submit"]')`)
	p.WaitJS(`document.querySelector('.srv-add .row-ask-text')?.textContent.includes("Add server box2 (ssh://deploy@10.0.0.2) for every terminal") && document.activeElement?.textContent === "Cancel"`)
	c.Quiet(t, 200*time.Millisecond)
	p.TapJS(`[...document.querySelectorAll('.srv-add .btn')].find((b) => b.textContent === "Add Server")`)
	reply(t, c, expectOp(t, c, webproto.OpContainersAddServer, `{"name":"box2","host":"ssh://deploy@10.0.0.2"}`), webproto.HostDone{})
	reply(t, c, expectOp(t, c, webproto.OpContainersServers, `{}`), hostsList())
	p.noOverflow("containers hosts")

	// More › Databases: Servers shows the ssh hosts and database containers only.
	Eval[bool](p, `(location.hash = "#/more/db", true)`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), webproto.DBConns{Conns: []webproto.DBConn{}})
	p.WaitJS(`document.querySelector('.srv-host[data-host="baaytona"]') && !document.querySelector('.srv-host[data-host="default"]') && document.querySelector('button[data-host-ctr="c0ffee01aa00"]') && !document.querySelector('[data-host-ctr="c0ffee03aa00"]')`)
	p.TapJS(`document.querySelector('button[data-host-ctr="c0ffee01aa00"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBAddContainer, `{"host":"baaytona","id":"c0ffee01aa00"}`), webproto.DBAdded{Conn: "postgres", Name: "baaytona-db-1"})
	dbFail(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"postgres"}`), "Unknown SSH host 65.109.212.14. Check its key before you trust it.", hostKeyNeed)
	p.WaitJS(`document.querySelector('.db-need')?.textContent.includes("65.109.212.14") && document.activeElement?.textContent === "Cancel"`)
	p.TapJS(`[...document.querySelectorAll('.db-need .btn')].find((b) => b.textContent === "Trust and save")`)
	reply(t, c, expectOp(t, c, webproto.OpDBTrustHost, `{"conn":"postgres","fingerprint":"SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8"}`), webproto.DBDone{Text: "Host key of 65.109.212.14 saved."})
	dbFail(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"postgres"}`), "The database needs a password.", map[string]any{"need": "password"})
	p.WaitJS(`document.activeElement?.type === "password"`)
	p.Do(chromedp.SendKeys(`.db-need input[type="password"]`, hostPGPassword+kb.Enter))
	reply(t, c, expectOp(t, c, webproto.OpDBSecret, `{"conn":"postgres","kind":"password","value":"`+hostPGPassword+`"}`), webproto.DBDone{Text: "Saved in the laptop's secret store."})
	reply(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"postgres"}`), webproto.DBTables{Conn: "postgres", Schema: "public", Tables: []webproto.DBTable{{Name: "users"}}})
	p.WaitJS(`document.querySelector('.db-table[data-table="users"]') && !document.querySelector('.db-need')`)
	if Eval[bool](p, `document.body.innerHTML.includes(`+jsq(hostPGPassword)+`)`) {
		t.Error("the page keeps the password")
	}
	p.noOverflow("databases tables")
}
