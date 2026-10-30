//go:build browser

package web

import (
	"fmt"
	"strings"
	"testing"
)

// Small phones, big fonts and thumbs (WEB-UX-6, web UX P2 and P7).

// Phone320 is the narrowest phone the page must fit.
var Phone320 = Viewport{Name: "phone320", W: 320, H: 640, Scale: 2, Mobile: true}

// overflowJS lists elements that stick out of the viewport sideways and
// are not inside a box that scrolls or clips on its own (code rows and
// the key bar may scroll inside themselves), plus the page's own
// sideways scroll.
const overflowJS = `(() => {
	const w = document.documentElement.clientWidth;
	const out = [];
	const se = document.scrollingElement;
	if (se.scrollWidth > w + 1) out.push("page scrolls sideways: " + se.scrollWidth + " > " + w);
	const clipped = (el) => {
		for (let a = el.parentElement; a && a !== document.body; a = a.parentElement) {
			const ox = getComputedStyle(a).overflowX;
			if (ox !== "visible") return true;
		}
		return false;
	};
	for (const el of document.querySelectorAll("body *")) {
		const r = el.getBoundingClientRect();
		if (r.width === 0 || r.height === 0) continue;
		if (r.right > w + 1 && !clipped(el)) {
			out.push((el.className || el.tagName) + " right " + Math.round(r.right) + " > " + w);
		}
	}
	return out.slice(0, 12);
})()`

func (p *Page) noOverflow(where string) {
	p.T.Helper()
	if bad := Eval[[]string](p, overflowJS); len(bad) > 0 {
		p.T.Errorf("%s at %d px: sticks out sideways:\n  %s", where, p.VP.W, strings.Join(bad, "\n  "))
	}
}

// smallTargetsJS lists the controls under sel that are smaller than 44 px
// in either direction.
func smallTargetsJS(sel string) string {
	return fmt.Sprintf(`[...document.querySelectorAll(%q)].filter((e) => {
		const r = e.getBoundingClientRect();
		return r.width > 0 && (r.width < 43.5 || r.height < 43.5);
	}).map((e) => (e.textContent.trim() || e.getAttribute("aria-label") || e.className) + " " + Math.round(e.getBoundingClientRect().width) + "x" + Math.round(e.getBoundingClientRect().height))`, sel)
}

// primaryTargets are the controls that finish a task in the app shell.
const primaryTargets = "nav .nav-item, .connect-foot button, .bar button, .seg-item, .toasts button, .card-foot button"

func (p *Page) bigTargets(where string) {
	p.T.Helper()
	if bad := Eval[[]string](p, smallTargetsJS(primaryTargets)); len(bad) > 0 {
		p.T.Errorf("%s: targets under 44 px: %v", where, bad)
	}
}

// Every tab fits a 320 px phone (also in Persian) and a 390 px phone
// with no sideways scroll, and every primary control is at least 44x44 px.
func TestNoSidewaysScrollAndBigTargets(t *testing.T) {
	for _, tc := range []struct {
		vp   Viewport
		lang string
	}{{Phone320, "en-US"}, {Phone, "en-US"}, {Desktop, "en-US"}, {Phone320, "fa-IR"}} {
		vp := tc.vp
		t.Run(vp.Name+"/"+tc.lang, func(t *testing.T) {
			s := StartServer(t)
			p := NewPageLang(t, s, vp, tc.lang)
			p.Open()
			p.noOverflow("connect")
			p.bigTargets("connect")
			p.LoginHere("c-1")
			for _, tab := range []string{"chat", "files", "term", "git", "more"} {
				p.GoTab(tab)
				p.noOverflow(tab)
				p.bigTargets(tab)
			}
		})
	}
}

// At 200 % font size, in English and in Persian (right to left), nothing
// sticks out, the tab bar still shows five labelled tabs, and the view
// ends above it.
func TestBigFontStillFits(t *testing.T) {
	for _, vp := range []Viewport{Phone320, Phone} {
		for _, dir := range []string{"ltr", "rtl"} {
			t.Run(vp.Name+"/"+dir, func(t *testing.T) {
				s := StartServer(t)
				p := NewPage(t, s, vp)
				p.Login("c-1")
				if dir == "rtl" {
					p.GoTab("more")
					p.Tap(`.more .field:nth-of-type(2) .seg-item:nth-child(3)`) // فارسی
					p.WaitJS(`document.documentElement.lang === "fa" && document.documentElement.dir === "rtl"`)
				}
				Eval[bool](p, `(document.documentElement.style.fontSize = "200%", true)`)
				for _, tab := range []string{"chat", "files", "term", "git", "more"} {
					p.GoTab(tab)
					p.noOverflow("200% " + tab)
					labels := Eval[int](p, `[...document.querySelectorAll(".tabbar .nav-label")].filter((e) => {
						const r = e.getBoundingClientRect();
						return r.width > 0 && r.height > 0 && e.scrollWidth <= e.clientWidth + 1;
					}).length`)
					if labels != 5 {
						t.Errorf("200%% %s: %d of 5 tab labels show in full", tab, labels)
					}
					if bad := Eval[string](p, `(() => {
						const v = document.querySelector("main").getBoundingClientRect();
						const bar = document.querySelector(".tabbar").getBoundingClientRect();
						return v.bottom > bar.top + 1 ? "main ends at " + v.bottom + ", tab bar starts at " + bar.top : "";
					})()`); bad != "" {
						t.Errorf("200%% %s: the tab bar covers the view: %s", tab, bad)
					}
				}
			})
		}
	}
}

// On a 390x844 phone the things that finish a task sit in the thumb
// zone: nothing to tap above 560 px (web UX 6.1).
func TestThumbZone(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Open()
	if top := Eval[float64](p, `document.querySelector(".connect-foot button").getBoundingClientRect().top`); top < 560 {
		t.Errorf("Open workspace starts at %.0f px; want 560 or lower", top)
	}
	p.LoginHere("c-1")
	if top := Eval[float64](p, `document.querySelector(".tabbar").getBoundingClientRect().top`); top < 560 {
		t.Errorf("the tab bar starts at %.0f px", top)
	}
}
