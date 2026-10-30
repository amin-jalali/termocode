//go:build browser

package web

import (
	"strings"
	"testing"
	"time"
)

// Persian and right to left (WEB-UX-5, web UX P7).

const faTabs = "گفتگو|فایل‌ها|ترمینال|گیت|بیشتر"

func (p *Page) tabLabels() string {
	return Eval[string](p, `[...document.querySelectorAll("nav .nav-label")].map((e) => e.textContent).join("|")`)
}

// actLabels are the desktop activity bar's names (icons; the names are labels).
func (p *Page) actLabels() []string {
	return Eval[[]string](p, `[...document.querySelectorAll("nav.act-bar .nav-item")].map((e) => e.getAttribute("aria-label"))`)
}

// Choosing فارسی on More turns the whole UI Persian and right to left at
// once; the strip's facts stay left to right, and English comes back.
func TestPersianFromMore(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			c := p.Login("c-1")
			p.GoTab("more")
			desk := vp.W >= 1200
			var en []string
			if desk {
				en = p.actLabels()
			}
			p.Tap(`.more .field:nth-of-type(2) .seg-item:nth-child(3)`) // فارسی
			p.WaitJS(`document.documentElement.lang === "fa" && document.documentElement.dir === "rtl"`)
			if desk {
				fa := p.actLabels()
				if len(fa) != len(en) || len(fa) == 0 {
					t.Errorf("activity bar %q, in English %q", fa, en)
				}
				for _, l := range fa {
					if !strings.ContainsFunc(l, func(r rune) bool { return r >= 0x0600 && r <= 0x06FF }) || strings.ContainsAny(l, "abcdefghijklmnopqrstuvwxyz") {
						t.Errorf("activity bar item %q is not Persian", l)
					}
				}
			} else if got := p.tabLabels(); got != faTabs {
				t.Errorf("tab labels %q, want %q", got, faTabs)
			}
			if got := p.Text(`[data-view="more"] .view-title`); got != "بیشتر" {
				t.Errorf("More title %q", got)
			}
			if got := Eval[string](p, `getComputedStyle(document.querySelector(".strip")).direction`); got != "ltr" {
				t.Errorf("the strip runs %s; its facts stay ltr", got)
			}
			// Persian digits in sentences, Latin digits in data.
			c.Send(t, Delta("status", 2, statusData("main", 12)))
			p.WaitJS(`document.querySelector(".strip .dirty")?.textContent.includes("12")`)
			if got := Eval[string](p, `document.querySelector(".strip .web-label")?.textContent ?? ""`); got != "وب ۱" {
				t.Errorf("web chip label %q, want وب ۱", got)
			}
			c.Drop()
			p.WaitJS(`document.querySelector(".bar-lost")`)
			if got := p.Text(".bar-lost .bar-text"); !strings.Contains(got, "اتصال قطع شد") || strings.ContainsAny(got, "0123456789") {
				t.Errorf("offline bar %q: want Persian text with Persian digits", got)
			}
			p.Tap(`.more .field:nth-of-type(2) .seg-item:nth-child(2)`) // English
			p.WaitJS(`document.documentElement.lang === "en" && document.documentElement.dir === "ltr"`)
			if desk {
				// Source Control now counts the 12 changes: "Source Control (12)".
				got := p.actLabels()
				if len(got) != len(en) {
					t.Errorf("activity bar after English %q, want %q", got, en)
				}
				for i := range min(len(got), len(en)) {
					if got[i] != en[i] && !strings.HasPrefix(got[i], en[i]+" (") {
						t.Errorf("activity bar item %d after English %q, want %q", i, got[i], en[i])
					}
				}
			} else if got := p.tabLabels(); got != "Chat|Files|Term|Git|More" {
				t.Errorf("tab labels after English: %q", got)
			}
		})
	}
}

// With the language on Auto, a browser that asks for Persian gets the
// Persian UI from the first screen, connect screen included.
func TestPersianFollowsTheBrowser(t *testing.T) {
	s := StartServer(t)
	p := NewPageLang(t, s, Phone, "fa-IR")
	p.Open()
	p.WaitJS(`document.documentElement.lang === "fa" && document.documentElement.dir === "rtl"`)
	if got := p.Text(".connect-foot button"); got != "باز کردن فضای کار" {
		t.Errorf("connect button %q", got)
	}
	p.LoginHere("c-1")
	if got := p.tabLabels(); got != faTabs {
		t.Errorf("tab labels %q", got)
	}
	p.Holds("true", 100*time.Millisecond)
	if got := p.Problems(); len(got) > 0 {
		t.Errorf("problems: %v", got)
	}
}
