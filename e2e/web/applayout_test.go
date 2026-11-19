//go:build browser

package web

import (
	"fmt"
	"testing"
)

// The termocode app on a tablet asks for the phone layout (owner,
// 2026-11-19, Galaxy Tab S11: "just like the mobile view"): its bridge
// says layout "phone", and the page is the phone's at any width: the
// Home, ✦, the tab bar, the phone's sizes, no rail and no workbench.
// A browser without the app's bridge keeps its layout by width.

// appTablet is the Tab S11 in landscape, with touch.
var appTablet = Viewport{Name: "app-tablet", W: 1280, H: 800, Scale: 1, Mobile: true, AppPhone: true}

// appBridge is what the app's bridge puts in the page before it loads.
const appBridge = `window.Capacitor = { Plugins: { TermocodeAppKey: { layout: "phone" } } };`

// probeJS is the style a probe element of class cls gets: prop's value.
func probeJS(cls, prop string) string {
	return fmt.Sprintf(`(() => { const e = document.createElement("span"); e.className = %q; document.body.append(e); const v = getComputedStyle(e).%s; e.remove(); return v; })()`, cls, prop)
}

const gutterJS = `getComputedStyle(document.documentElement).getPropertyValue("--gutter").trim()`

func TestAppPhoneLayoutOnTablet(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, appTablet)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	p.onNewDocument(appBridge)
	p.Login("c-app")
	p.WaitJS(`document.documentElement.dataset.layout === "phone" && !!document.querySelector(".shell.layout-phone")`)
	Eval[bool](p, `(location.hash = "#/home", true)`)
	p.WaitJS(`!!document.querySelector(".home-top")`)
	p.WaitJS(`!!document.querySelector(".rm-fab")`)
	if Eval[bool](p, `!!document.querySelector(".rail, .act-item, .wb")`) {
		t.Error("a rail or the workbench shows in the app's phone layout")
	}
	if !Eval[bool](p, `!!document.querySelector("nav.tabbar")`) {
		t.Error("no tab bar in the app's phone layout")
	}
	// The phone's sizes and sheets, not the wide screen's.
	if g := Eval[string](p, gutterJS); g != "1rem" {
		t.Errorf("--gutter %q, want the phone's 1rem", g)
	}
	if d := Eval[string](p, probeJS("desk-only", "display")); d != "none" {
		t.Errorf(".desk-only is %q, want none", d)
	}
	if d := Eval[string](p, probeJS("grip", "display")); d == "none" {
		t.Error("the sheet's grip is hidden, as on a wide screen")
	}
	if Eval[bool](p, `matchMedia("(min-width: 720px)").matches || !matchMedia("(max-width: 1199.98px)").matches`) {
		t.Error("the page's own width checks still see a wide screen")
	}
	p.noOverflow("app home")
}

// Without the app's bridge the same widths keep their layouts: a touch
// tablet has the rail, a 1280 px window the workbench.
func TestNoAppKeepsWidthLayouts(t *testing.T) {
	for _, tc := range []struct {
		vp     Viewport
		layout string
		gutter string
		rail   string
	}{
		{TabletTouch, "tablet", "1.5rem", ".rail"},
		{Viewport{Name: "desktop-touch", W: 1280, H: 800, Scale: 1, Mobile: true}, "desktop", "2rem", ".act-item"},
	} {
		t.Run(tc.vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, tc.vp)
			p.Login("c-web")
			p.WaitJS(`!!document.querySelector("` + tc.rail + `")`)
			if Eval[bool](p, `!!document.querySelector("nav.tabbar") || document.documentElement.dataset.layout === "phone"`) {
				t.Errorf("%s: the phone layout without the app", tc.vp.Name)
			}
			if g := Eval[string](p, gutterJS); g != tc.gutter {
				t.Errorf("%s: --gutter %q, want %q", tc.vp.Name, g, tc.gutter)
			}
		})
	}
}
