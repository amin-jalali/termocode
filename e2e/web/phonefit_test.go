//go:build browser

package web

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The phone composer is the common chat pill and the page always fits
// the phone (owner's iPhone, 2026-11-09): + | the box | mic | ↑ in one
// rounded bar (RTL mirrors it), ↑ dimmed while the box is empty and ■
// while the AI answers, + opens a sheet with a file and the profile and
// model pickers; nothing is ever wider than the screen (iOS zooms into a
// box under 16 px and leaves the page wide); with the keyboard open the
// strip stays at the top and the pill right above the keyboard, also
// when iOS scrolls the page itself.

// longFa is a Persian answer with long ids and inline code.
const longFa = "برای اتصال، تونل SSH به سرور baaytona-db-1.internal.example.svc.cluster.local باز می‌شود و پورت محلی redis_cache_primary_connection_identifier_0042 انتخاب می‌شود."

// draftFa is a Persian draft of two lines in the box.
const draftFa = "لطفاً اتصال پایگاه داده را از طریق SSH بررسی کن و بگو کدام پورت روی سرور باز است و چرا خطا می‌دهد"

// setDraft puts text in the composer's box as typing would.
func setDraft(p *Page, text string) {
	p.T.Helper()
	Eval[bool](p, `(() => { const b = `+chatView+`.querySelector(".composer textarea"); b.value = `+jsq(text)+`; b.dispatchEvent(new Event("input", {bubbles: true})); return true; })()`)
	p.WaitJS(chatView + `.querySelector(".composer textarea").value === ` + jsq(text))
}

// fitShot writes a screenshot to WEB_SHOTS_DIR as phone-design-<name>.png.
func fitShot(t *testing.T, p *Page, name, ready string) {
	t.Helper()
	if dir := os.Getenv(shotsEnv); dir != "" {
		snap(t, p, filepath.Join(dir, "phone-design-"+name+".png"), ready)
	}
}

func TestPhoneComposerIsThePill(t *testing.T) {
	for _, lang := range []string{"en-US", "fa-IR"} {
		t.Run(lang, func(t *testing.T) {
			p, c := openCompact(t, lang, "light", "I want to connect my db over ssh")
			row := chatView + `.querySelector(".composer-row")`
			parts := map[string]string{
				"+":    row + `.querySelector(".composer-plus")`,
				"box":  row + `.querySelector("textarea")`,
				"mic":  row + `.querySelector(".mic button")`,
				"send": row + `.querySelector(".send button")`,
			}
			x := map[string]float64{}
			for name, el := range parts {
				if !Eval[bool](p, `!!`+el) {
					t.Fatalf("the pill has no %s", name)
				}
				r := Eval[struct{ X, W, H float64 }](p, `(() => { const r = `+el+`.getBoundingClientRect(); return {X: r.x, W: r.width, H: r.height}; })()`)
				if r.W < 43.5 || r.H < 43.5 {
					t.Errorf("%s is %.0fx%.0f px, want ≥ 44", name, r.W, r.H)
				}
				x[name] = r.X
			}
			ltr := x["+"] < x["box"] && x["box"] < x["mic"] && x["mic"] < x["send"]
			rtl := x["+"] > x["box"] && x["box"] > x["mic"] && x["mic"] > x["send"]
			if lang == "en-US" && !ltr || lang == "fa-IR" && !rtl {
				t.Errorf("%s order of + box mic send: %v", lang, x)
			}
			if got := Eval[string](p, parts["send"]+`.getAttribute("aria-disabled") ?? ""`); got != "true" {
				t.Errorf("↑ with an empty box: aria-disabled %q, want true", got)
			}
			if Eval[bool](p, chatView+`.querySelector(".composer .ai-pick") !== null`) {
				t.Error("the profile and model chips still sit on the composer")
			}
			// Two lines of Persian: the box grows, the pill still fits.
			setDraft(p, draftFa)
			p.WaitJS(parts["box"] + `.getBoundingClientRect().height > 60`)
			if bad := Eval[string](p, `(() => { const w = innerWidth, out = [];
				for (const [n, e] of [["pill", `+row+`], ["send", `+parts["send"]+`], ["+", `+parts["+"]+`]]) {
					const r = e.getBoundingClientRect();
					if (r.left < 0 || r.right > w) out.push(n + " " + Math.round(r.left) + ".." + Math.round(r.right));
				}
				if (document.documentElement.scrollWidth > w) out.push("page " + document.documentElement.scrollWidth);
				return out.join(", "); })()`); bad != "" {
				t.Errorf("a 2-line draft pushes things off the %.0f px screen: %s", 390.0, bad)
			}
			if got := Eval[string](p, parts["send"]+`.getAttribute("aria-disabled") ?? ""`); got == "true" {
				t.Error("↑ stays dimmed with a draft")
			}
			fitShot(t, p, "draft-"+lang, `true`)
			// + opens the sheet: a file, the profile and the model; the one in use once, muted.
			p.TapJS(parts["+"])
			p.WaitJS(`document.querySelector(".sheet .plus-sheet .ai-pick[data-pick='model']")`)
			if got := Eval[string](p, `document.querySelector(".sheet .plus-now")?.textContent ?? ""`); got != "GPT · gpt-6-sol" {
				t.Errorf("the sheet's header line reads %q, want GPT · gpt-6-sol", got)
			}
			if bad := Eval[[]string](p, smallTargetsJS(`.sheet .plus-sheet button`)); len(bad) > 0 {
				t.Errorf("sheet targets under 44 px: %v", bad)
			}
			if !Eval[bool](p, `!!document.querySelector(".sheet .plus-sheet .plus-file")`) {
				t.Error("the sheet has no way to add a file")
			}
			fitShot(t, p, "plus-"+lang, `true`)
			p.TapJS(`document.querySelector(".sheet .plus-sheet .ai-pick[data-pick='model']")`)
			p.WaitJS(`document.querySelectorAll(".sheet .ai-menu .actsheet-row").length === 2`)
			Eval[bool](p, `([...document.querySelectorAll(".scrim")].forEach((s) => s.click()), true)`)
			p.WaitJS(`!document.querySelector(".sheet")`)
			// While the AI answers ↑ is ■.
			busy := compactChat("I want to connect my db over ssh", compactTurns...)
			busy["busy"] = true
			c.Send(t, Snap("chat", 2, busy))
			p.WaitJS(row + `.querySelector(".stop button")?.getBoundingClientRect().width >= 43.5`)
		})
	}
}

// Nothing is wider than the screen: a long Persian answer with ids and
// inline code wraps, a 2-line Persian draft fits, and no box has text
// under 16 px (iOS would zoom the page into it).
func TestPhoneNeverWiderThanTheScreen(t *testing.T) {
	s := StartServer(t)
	p := NewPageLang(t, s, Phone, "fa-IR")
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	code := md("code")
	code["text"] = "ssh -L 15432:baaytona-db-1.internal.example.svc.cluster.local:5432 deploy@bastion"
	doc := md("doc", mdPara(longFa), md("p", mdText("دستور: "), code, mdText(" را اجرا کن.")))
	chat := compactChat("اتصال پایگاه داده", "پایگاه داده را با ssh وصل کن")
	chat["messages"] = append(chat["messages"].([]any), aiMsg(1, doc))
	c.Send(t, Snap("chat", 1, chat))
	p.WaitJS(chatView + `?.querySelector(".msg-ai .md-code")`)
	setDraft(p, draftFa)
	if w := Eval[float64](p, `document.documentElement.scrollWidth`); w > 390 {
		t.Errorf("the page is %.0f px wide on a 390 px phone", w)
	}
	p.noOverflow("persian answer and draft")
	small := Eval[[]string](p, `[...document.querySelectorAll("input, textarea, select")].filter((e) => parseFloat(getComputedStyle(e).fontSize) < 16).map((e) => (e.className || e.tagName) + " " + getComputedStyle(e).fontSize)`)
	if len(small) > 0 {
		t.Errorf("boxes under 16 px (iOS zooms the page into them): %v", small)
	}
	fitShot(t, p, "fa-wrap", `true`)
}

// fakeViewport makes the page see an iOS visual viewport: the keyboard
// covers the bottom, the layout viewport stays the phone's.
func fakeViewport(p *Page, h, top float64) {
	p.T.Helper()
	Eval[bool](p, `(() => {
		const vv = {width: innerWidth, height: `+fmt.Sprint(h)+`, offsetTop: `+fmt.Sprint(top)+`, scale: 1, addEventListener() {}};
		Object.defineProperty(window, "visualViewport", {configurable: true, get: () => vv});
		dispatchEvent(new Event("resize"));
		return true;
	})()`)
}

// The keyboard: the strip stays at the top of what shows, the pill right
// above the keyboard, the messages between; also when iOS scrolls the
// page itself (it lets the page scroll by the keyboard's height), and
// after a finger scrolled the page with no keyboard.
func TestPhoneKeyboardKeepsThePillAboveIt(t *testing.T) {
	for _, tc := range []struct {
		name          string
		kb, top, page float64
	}{
		{"ios-scrolls-the-page", 480, 0, 364},
		{"ios-pans-the-view", 480, 364, 0},
		{"finger-scrolls-the-page", 0, 0, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := openCompact(t, "en-US", "dark", "I want to connect my db over ssh")
			// iOS lets the page scroll under the keyboard: room to scroll.
			Eval[bool](p, `(document.body.style.paddingBottom = "900px", true)`)
			vis := 844.0
			if tc.kb > 0 {
				Eval[bool](p, `(`+chatView+`.querySelector(".composer textarea").focus(), true)`)
				fakeViewport(p, tc.kb, tc.top)
				p.WaitJS(`document.documentElement.dataset.kb === "up"`)
				vis = tc.kb
			}
			if tc.page > 0 {
				Eval[bool](p, `(scrollTo(0, `+fmt.Sprint(tc.page)+`), true)`)
			}
			r := Eval[struct{ Strip, Pill, Body float64 }](p, `(() => ({
				Strip: document.querySelector(".strip").getBoundingClientRect().top,
				Pill: `+chatView+`.querySelector(".composer").getBoundingClientRect().bottom,
				Body: `+chatView+`.querySelector(".view-body").getBoundingClientRect().height,
			}))()`)
			top := tc.top - tc.page // where the visible part starts, in the page's viewport
			if tc.page > 0 {
				top = 0 // the layout viewport scrolled: what shows starts at its top
			}
			if r.Strip < top-1 || r.Strip > top+1 {
				t.Errorf("the strip is at %.0f px, want at the top of what shows (%.0f)", r.Strip, top)
			}
			if tc.kb > 0 && (r.Pill > top+vis+1 || r.Pill < top+vis-70) {
				t.Errorf("the pill ends at %.0f px, want right above the keyboard (%.0f)", r.Pill, top+vis)
			}
			if r.Body < 150 {
				t.Errorf("the messages get %.0f px", r.Body)
			}
			fitShot(t, p, "kb-"+tc.name, `true`)
		})
	}
}
