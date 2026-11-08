//go:build browser

package web

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The owner's layout of the Containers view, desktop and phone:
// CONTAINERS itself is the hosts tree. The local engine is the first
// host, open, with its compose projects and containers; every other host
// follows, closed, and is read once when it first opens; + Add Server…
// is the last row of CONTAINERS. No "docker · context default" line and
// no separate CONTEXTS section.
//
//	▼ CONTAINERS
//	  ▼ default · this machine   docker   1/2
//	    ▼ shop
//	        api-1
//	    ▼ other
//	        old
//	  ▶ baaytona · deploy@65.109.212.14
//	  ▶ broken · ops@10.9.9.9:2222
//	  + Add Server…

// replyLocalHosts answers the page's host list with this machine only
// (a page that opens Containers or Databases first asks for it).
func replyLocalHosts(t *testing.T, c *HubConn) {
	t.Helper()
	reply(t, c, expectOp(t, c, webproto.OpContainersServers, `{}`), webproto.ServersList{Engine: "docker", Hosts: []webproto.ServerHost{{Name: "default", Local: true, Current: true}}})
}

// deskCtrSketch is the desktop's CONTAINERS rows, two spaces a level.
const deskCtrSketch = `[...` + deskCtr + `.querySelectorAll('[data-section="containers"] .tr')].map((r) => "  ".repeat(+r.getAttribute("aria-level") - 1) + r.querySelector(".tr-label").textContent)`

func TestDeskContainersIsTheHostTree(t *testing.T) {
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/containers", true)`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), hostsCtrHost())
	p.WaitJS(deskCtr + `?.querySelectorAll('[data-section="containers"] .tr[data-id]').length === 2 && !!` + deskCtr + `.querySelector('[data-section="containers"] .tr[data-context="broken"]')`)

	want := []string{
		"default · this machine",
		"  shop",
		"    api-1",
		"  other",
		"    old",
		"baaytona · deploy@65.109.212.14",
		"broken · ops@10.9.9.9:2222",
		"+ Add Server…",
	}
	if got := Eval[[]string](p, deskCtrSketch); !reflect.DeepEqual(got, want) {
		t.Fatalf("CONTAINERS:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	const local = deskCtr + `.querySelector('[data-section="containers"] .tr[data-context="default"]')`
	if got := Eval[string](p, local+`.getAttribute("aria-expanded") + " " + `+local+`.querySelector(".count")?.textContent`); got != "true 1/2" {
		t.Errorf("the local host is %q, want open with its counts (true 1/2)", got)
	}
	if got := Eval[string](p, deskCtr+`.querySelector('[data-section="containers"] .tr[data-context="baaytona"]').getAttribute("aria-expanded")`); got != "false" {
		t.Errorf("a remote host starts %q, want closed", got)
	}
	if Eval[bool](p, `!!`+deskCtr+`.querySelector('[data-section="contexts"]')`) {
		t.Error("a CONTEXTS section is still drawn: the hosts are in CONTAINERS")
	}
	if s := Eval[string](p, deskCtr+`.querySelector('[data-section="containers"]').textContent`); strings.Contains(s, "context default") {
		t.Errorf("the old engine line is still drawn: %q", s)
	}
	// Use Here is a host row's action (nothing of CONTEXTS is lost); the
	// host in use offers none.
	if !Eval[bool](p, `!!`+deskCtr+`.querySelector('.tr[data-context="baaytona"] [data-act="use"]') && !`+local+`.querySelector('[data-act="use"]')`) {
		t.Error("Use Here is not on the remote host row, or is on the host in use")
	}
	c.Quiet(t, 200*time.Millisecond) // the remote hosts are read only when opened

	// The local host folds like the others (nothing is read again).
	p.TapJS(local)
	p.WaitJS(deskCtr + `.querySelectorAll('.tr[data-id]').length === 0`)
	p.TapJS(local)
	p.WaitJS(deskCtr + `.querySelectorAll('.tr[data-id]').length === 2`)
	c.Quiet(t, 200*time.Millisecond)

	// A remote host opens in place, under its own row, before the next host.
	p.TapJS(deskCtr + `.querySelector('.tr[data-context="baaytona"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersServer, `{"name":"baaytona"}`), baaytonaCtrs())
	p.WaitJS(deskCtr + `.querySelector('[data-section="containers"] .tr[data-host-ctr="c0ffee03aa00"]') !== null`)
	want = []string{
		"baaytona · deploy@65.109.212.14",
		"  baaytona",
		"    baaytona-db-1",
		"    baaytona-web-1",
		"broken · ops@10.9.9.9:2222",
		"+ Add Server…",
	}
	if got := Eval[[]string](p, deskCtrSketch); len(got) < len(want) || !reflect.DeepEqual(got[len(got)-len(want):], want) {
		t.Fatalf("CONTAINERS after opening baaytona:\n%s\nwant it to end with\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !Eval[bool](p, `!!`+deskCtr+`.querySelector('.tr[data-host-ctr="c0ffee01aa00"] [data-act="open-db"]')`) {
		t.Error("the remote database container has no Open in Databases")
	}
	p.TapJS(deskCtr + `.querySelector('.tr[data-context="broken"]')`)
	hostFail(t, c, expectOp(t, c, webproto.OpContainersServer, `{"name":"broken"}`))
	p.WaitJS(deskCtr + `.querySelector('[data-section="containers"] [data-host-err="broken"]')?.textContent === "ssh: Connection timed out"`)
	p.noOverflow("containers host tree")
}

// phoneCtrSketch is the phone's Containers tree, two spaces a level.
const phoneCtrSketch = `[...document.querySelectorAll(".ctrs .srv-host, .ctrs .srv-project, .ctrs .ctr, .ctrs [data-host-ctr], .ctrs [data-add-server]")].map((e) =>
	e.matches(".srv-host") ? e.querySelector(".db-name").textContent :
	e.matches(".srv-project") ? "  " + e.textContent :
	e.matches(".ctr") ? "    " + e.querySelector(".ctr-name").textContent :
	e.matches("[data-host-ctr]") ? "    " + e.querySelector(".db-name").textContent : e.textContent)`

func TestPhoneContainersIsTheHostTree(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.LoginMeta(Meta("c-ctr"))
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="containers"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersServers, `{}`), hostsList())
	p.WaitJS(`document.querySelectorAll(".ctrs .ctr").length === 2 && !!document.querySelector('.ctrs .srv-host[data-host="broken"]')`)

	want := []string{
		"default · this machine",
		"  shop",
		"    api-1",
		"  other",
		"    old",
		"baaytona · deploy@65.109.212.14",
		"broken · ops@10.9.9.9:2222",
		"+ Add Server…",
	}
	if got := Eval[[]string](p, phoneCtrSketch); !reflect.DeepEqual(got, want) {
		t.Fatalf("Containers:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	const local = `document.querySelector('.ctrs .srv-host[data-host="default"]')`
	if got := Eval[string](p, local+`.getAttribute("aria-expanded") + " " + `+local+`.querySelector(".srv-count")?.textContent`); got != "true 1/2" {
		t.Errorf("the local host is %q, want open with its counts (true 1/2)", got)
	}
	if s := p.Text(".ctrs"); strings.Contains(s, "context default") {
		t.Errorf("the old engine line is still drawn: %q", s)
	}
	if Eval[bool](p, `[...document.querySelectorAll(".ctrs button")].some((b) => b.textContent === "Show")`) {
		t.Error("the hosts still hide behind Show")
	}
	c.Quiet(t, 200*time.Millisecond) // the remote hosts are read only when opened

	p.TapJS(`document.querySelector('.ctrs .srv-host[data-host="baaytona"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersServer, `{"name":"baaytona"}`), baaytonaCtrs())
	p.WaitJS(`!!document.querySelector('.ctrs [data-host-ctr="c0ffee03aa00"]')`)
	want = []string{
		"baaytona · deploy@65.109.212.14",
		"  baaytona",
		"    baaytona-db-1",
		"    baaytona-web-1",
		"broken · ops@10.9.9.9:2222",
		"+ Add Server…",
	}
	if got := Eval[[]string](p, phoneCtrSketch); len(got) < len(want) || !reflect.DeepEqual(got[len(got)-len(want):], want) {
		t.Fatalf("Containers after opening baaytona:\n%s\nwant it to end with\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The local host folds; its containers come back without a new read.
	p.TapJS(local)
	p.WaitJS(`document.querySelectorAll(".ctrs .ctr").length === 0`)
	p.TapJS(local)
	p.WaitJS(`document.querySelectorAll(".ctrs .ctr").length === 2`)
	c.Quiet(t, 200*time.Millisecond)
	p.noOverflow("containers host tree")
}

// Databases' SERVERS is open as soon as there is a remote host, on the
// desktop and the phone, without a tap.
func TestDeskDBServersOpenWithARemoteHost(t *testing.T) {
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/db", true)`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), webproto.DBConns{Conns: []webproto.DBConn{}})
	reply(t, c, expectOp(t, c, webproto.OpContainersServers, `{}`), hostsList())
	p.WaitJS(dbxSide + `?.querySelector('.tr[data-servers]')?.getAttribute("aria-expanded") === "true" && !!` + dbxSide + `.querySelector('.tr[data-host="baaytona"]')`)
}

func TestPhoneDBServersOpenWithARemoteHost(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.LoginMeta(Meta("c-db"))
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="db"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), webproto.DBConns{Conns: []webproto.DBConn{}})
	reply(t, c, expectOp(t, c, webproto.OpContainersServers, `{}`), hostsList())
	p.WaitJS(`!!document.querySelector('.srv-host[data-host="baaytona"]') && !document.querySelector('.srv-host[data-host="default"]')`)
}
