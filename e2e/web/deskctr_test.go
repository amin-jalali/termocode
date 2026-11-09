//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The rest of the desktop's Containers view (web parity, Containers):
// images, networks, volumes, the hosts and registries with their actions.

func ctrHost() webproto.ContainersHost {
	return webproto.ContainersHost{Engine: "docker", Context: "context default",
		Images:     []webproto.ImageItem{{ID: "111111111111", Tags: []string{"postgres:16"}, Size: "421MB", InUse: true}, {ID: "222222222222", Tags: []string{}, Size: "0B"}},
		Networks:   []webproto.NetworkItem{{Name: "shop_default", Driver: "bridge"}, {Name: "bridge", Driver: "bridge", BuiltIn: true}},
		Volumes:    []webproto.VolumeItem{{Name: "pgdata", Driver: "local"}},
		Contexts:   []webproto.ContextItem{{Name: "default", Current: true}, {Name: "prod"}},
		Registries: []webproto.RegistryItem{{Host: "ghcr.io", SignedIn: true}},
	}
}

const deskCtr = `document.querySelector('.wb-side [data-view="containers"]')`

// openDeskCtr opens the desktop's Containers view and answers its lists.
func openDeskCtr(t *testing.T, meta *webproto.Meta) (*Page, *HubConn) {
	t.Helper()
	p, c := deskLogin(t, meta)
	Eval[bool](p, `(location.hash = "#/more/containers", true)`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), ctrHost())
	p.WaitJS(deskCtr + `?.querySelectorAll(".tr[data-id]").length === 2`)
	return p, c
}

// ctrClick clicks the action act of the row matched by sel in the view.
func ctrClick(p *Page, sel, act string) {
	Eval[bool](p, deskCtr+`.querySelector('`+sel+` [data-act="`+act+`"]').click() ?? true`)
}

// askLaptop waits for the laptop question with text, then asks.
func askLaptop(t *testing.T, p *Page, text string) {
	t.Helper()
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === ` + jsq(text))
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
}

// Images, networks, volumes and registries list in their own
// sections; Inspect opens an editor tab with the laptop's masked text;
// Pause asks in the view (Cancel first); Pull takes a reference in a box
// and asks the laptop; every prune asks the laptop.
func TestDeskContainersHost(t *testing.T) {
	p, c := openDeskCtr(t, nil)
	for _, sec := range []string{"images", "networks", "volumes", "registries"} {
		if !Eval[bool](p, `!!`+deskCtr+`.querySelector('[data-section="`+sec+`"]')`) {
			t.Errorf("no %s section", sec)
		}
	}
	if !Eval[bool](p, `!!`+deskCtr+`.querySelector('.tr[data-image="postgres:16"]') && !`+deskCtr+`.querySelector('.tr[data-image="postgres:16"] [data-act="remove"]')`) {
		t.Error("an image in use offers Remove, or is missing")
	}
	ctrClick(p, `.tr[data-image="postgres:16"]`, "inspect")
	reply(t, c, expectOp(t, c, webproto.OpContainersInspect, `{"kind":"image","id":"postgres:16"}`),
		webproto.ContainerInspect{Kind: "image", ID: "postgres:16", Text: "{\n  \"Env\": [\"POSTGRES_PASSWORD=…\"]\n}", Masked: 1})
	p.WaitJS(`document.querySelector('[data-view="ctr-tab"] .ctrt-pre')?.textContent.includes("POSTGRES_PASSWORD=…")`)
	if !strings.Contains(p.Text(`[data-view="ctr-tab"]`), "Env values and secrets hidden: 1.") {
		t.Errorf("tab %q", p.Text(`[data-view="ctr-tab"]`))
	}

	ctrClick(p, `.tr[data-id="9f3c2e1a1234"]`, "pause")
	p.WaitJS(deskCtr + `.querySelector(".pv-ask")?.textContent.includes("Pause api-1?") && document.activeElement?.textContent === "Cancel"`)
	p.TapJS(`[...` + deskCtr + `.querySelectorAll(".pv-ask button")].find((b) => b.textContent === "Pause")`)
	reply(t, c, expectOp(t, c, webproto.OpContainersPause, `{"id":"9f3c2e1a1234"}`), webproto.ContainerDone{State: "paused"})
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())

	ctrClick(p, `[data-section="images"] .ps-head`, "pull")
	p.WaitJS(`document.activeElement === ` + deskCtr + `.querySelector('.ctr-form input')`)
	p.Do(chromedp.SendKeys(`.ctr-form input`, "redis:7"))
	p.Do(chromedp.KeyEvent(kb.Enter))
	askLaptop(t, p, "Pull redis:7 in a laptop terminal?")
	reply(t, c, expectOp(t, c, webproto.OpContainersImage, `{"verb":"pull","ref":"redis:7"}`), webproto.AskRef{Card: "card-1"})
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-1", By: "laptop", Choice: "cancel"})})
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), ctrHost())

	ctrClick(p, `[data-section="images"] .ps-head`, "prune-images")
	askLaptop(t, p, "Remove the unused untagged images?")
	reply(t, c, expectOp(t, c, webproto.OpContainersPrune, `{"kind":"images"}`), webproto.AskRef{Card: "card-2"})
}

// A host row of CONTAINERS: Use Here asks in the view and switches this session; Make
// Default and a registry's Sign Out ask the laptop; built-in networks
// offer no Remove; the filter narrows the rows.
func TestDeskContainersContextsAndFilter(t *testing.T) {
	p, c := openDeskCtr(t, nil)
	ctrClick(p, `.tr[data-context="prod"]`, "use")
	p.WaitJS(deskCtr + `.querySelector(".pv-ask")?.textContent.includes("Use prod in this session?")`)
	p.TapJS(`[...` + deskCtr + `.querySelectorAll(".pv-ask button")].find((b) => b.textContent === "Use Here")`)
	reply(t, c, expectOp(t, c, webproto.OpContainersContext, `{"name":"prod"}`), webproto.HostDone{})
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), ctrHost())
	ctrClick(p, `.tr[data-context="prod"]`, "default")
	askLaptop(t, p, "Make prod the default for every terminal on the laptop?")
	reply(t, c, expectOp(t, c, webproto.OpContainersContextDefault, `{"name":"prod"}`), webproto.AskRef{Card: "card-3"})
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-3", By: "laptop", Choice: "cancel"})})
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), ctrHost())

	Eval[bool](p, deskCtr+`.querySelector('[data-section="networks"] .ps-head').click() ?? true`)
	p.WaitJS(deskCtr + `.querySelector('.tr[data-network="bridge"]') !== null`)
	if Eval[bool](p, `!!`+deskCtr+`.querySelector('.tr[data-network="bridge"] [data-act="remove"]')`) {
		t.Error("a built-in network offers Remove")
	}
	Eval[bool](p, deskCtr+`.querySelector('[data-act="filter"]').click() ?? true`)
	p.WaitJS(deskCtr + `.querySelector(".ctr-filter input") !== null`)
	p.Do(chromedp.SendKeys(`.ctr-filter input`, "old"))
	p.WaitJS(deskCtr + `.querySelectorAll(".tr[data-id]").length === 1 && !!` + deskCtr + `.querySelector('.tr[data-id="aabbccdd1122"]')`)
}

// A right click on a container opens the actions its buttons and the
// laptop's keys have: the reads run at once, Copy ID puts the ID on the
// clipboard, Restart asks in the view (Cancel first), Stop and Remove ask
// the laptop. A compose project offers its own actions, and a viewer
// gets only the reads. Add to AI Chat comes last (not for a viewer, who
// cannot send in the chat).
func TestDeskContainersContextMenu(t *testing.T) {
	p, c := openDeskCtr(t, nil)
	fakeClipboard(p, "")
	row := func(sel string) string { return deskCtr + `.querySelector('.tr` + sel + `')` }
	api, old := row(`[data-id="9f3c2e1a1234"]`), row(`[data-id="aabbccdd1122"]`)

	rightClick(p, api)
	if got, want := menuLabels(p), "Logs|Inspect|Stats|Open Published Port|Copy ID|Copy Name|Open Shell…|Attach…|Pause|Restart|Stop…|Add to AI Chat"; got != want {
		t.Errorf("running container menu %q, want %q", got, want)
	}
	menuPick(p, "Copy ID")
	p.WaitJS(`window.__clip === "9f3c2e1a1234"`)

	rightClick(p, api)
	menuPick(p, "Restart")
	p.WaitJS(deskCtr + `.querySelector(".pv-ask")?.textContent.includes("Restart api-1?") && document.activeElement?.textContent === "Cancel"`)
	p.TapJS(`[...` + deskCtr + `.querySelectorAll(".pv-ask button")].find((b) => b.textContent === "Cancel")`)
	p.WaitJS(`!` + deskCtr + `.querySelector(".pv-ask")`)

	rightClick(p, old)
	if got, want := menuLabels(p), "Logs|Inspect|Copy ID|Copy Name|Start|Remove…|Add to AI Chat"; got != want {
		t.Errorf("stopped container menu %q, want %q", got, want)
	}
	menuPick(p, "Remove…")
	askLaptop(t, p, "Remove the stopped container old? This cannot be undone.")
	reply(t, c, expectOp(t, c, webproto.OpContainersRemove, `{"id":"aabbccdd1122"}`), webproto.AskRef{Card: "card-1"})
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-1", By: "laptop", Choice: "cancel"})})
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), ctrHost())

	rightClick(p, row(`[data-project="shop"]`))
	if got, want := menuLabels(p), "Start Project|Restart Project|Stop Project…|Remove Project Containers…|Add to AI Chat"; got != want {
		t.Errorf("project menu %q, want %q", got, want)
	}
	menuPick(p, "Remove Project Containers…")
	askLaptop(t, p, "Stop and remove the containers of shop? Volumes stay.")
	reply(t, c, expectOp(t, c, webproto.OpContainersComposeAsk, `{"project":"shop","verb":"down"}`), webproto.AskRef{Card: "card-2"})
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-2", By: "laptop", Choice: "cancel"})})
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), ctrHost())

	rightClick(p, api)
	menuPick(p, "Stop…")
	askLaptop(t, p, "Stop api-1? What runs in it ends.")
	reply(t, c, expectOp(t, c, webproto.OpContainersStop, `{"id":"9f3c2e1a1234"}`), webproto.AskRef{Card: "card-3"})

	meta := Meta("c-view-menu")
	meta.Role = webproto.RoleViewer
	v, _ := openDeskCtr(t, meta)
	rightClick(v, api)
	if got, want := menuLabels(v), "Logs|Inspect|Stats|Open Published Port|Copy ID|Copy Name"; got != want {
		t.Errorf("viewer menu %q, want %q", got, want)
	}
}

// A viewer sees the lists but no action that changes the host.
func TestDeskContainersViewer(t *testing.T) {
	meta := Meta("c-view")
	meta.Role = webproto.RoleViewer
	p, _ := openDeskCtr(t, meta)
	for _, act := range []string{"pause", "shell", "prune-images", "pull", "prune-system"} {
		if Eval[bool](p, `!!`+deskCtr+`.querySelector('[data-act="`+act+`"]')`) {
			t.Errorf("a viewer has %s", act)
		}
	}
}

// Start All asks in the view and starts every service; Stop All asks
// the laptop; Configure opens services.json the laptop named.
func TestServicesAll(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openRunView(t, vp, "services", "services", "services")
			c.Send(t, Snap("services", 1, servicesSnap()))
			p.WaitJS(`document.querySelectorAll(".svc").length === 3`)
			p.TapJS(`document.querySelector('[data-act="start-all"]')`)
			p.WaitJS(`document.activeElement?.textContent === "Cancel"`)
			p.TapJS(`document.querySelector(".dbg-ask .btn-primary")`)
			reply(t, c, expectOp(t, c, webproto.OpServicesStartAll, `{}`), webproto.HostDone{})
			p.TapJS(`document.querySelector('[data-act="stop-all"]')`)
			askLaptop(t, p, "Stop every service on the laptop? What runs in them ends.")
			reply(t, c, expectOp(t, c, webproto.OpServicesStopAll, `{}`), webproto.AskRef{Card: "a-9"})
			if len(p.Problems()) > 0 {
				t.Errorf("problems: %v", p.Problems())
			}
		})
	}
}
