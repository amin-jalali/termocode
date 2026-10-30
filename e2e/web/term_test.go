//go:build browser

package web

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The terminal view (WEB-20, web UX 3.3, 5.6).

const termView = `document.querySelector('[data-view="term"]')`

func openTerm(t *testing.T, vp Viewport) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	p.GoTab("term")
	c.Subs(t, "terms")
	return p, c
}

func termItem(buf int, title string, busy, active bool) map[string]any {
	return map[string]any{"buf": buf, "title": title, "cwd": "", "busy": busy, "exited": false, "active": active}
}

func termsSnap(items ...map[string]any) map[string]any {
	l := make([]any, len(items))
	for i, x := range items {
		l[i] = x
	}
	return map[string]any{"terms": l}
}

func termSnap(buf, first int, cols int, rows ...string) map[string]any {
	return map[string]any{"buf": buf, "first": first, "rows": rows, "more": first > 0, "tick": 3, "screen": false, "cols": cols, "height": 24}
}

// showTerm sends the terms list and the chosen shell's rows.
func showTerm(t *testing.T, p *Page, c *HubConn, snap map[string]any, items ...map[string]any) {
	t.Helper()
	c.Send(t, Snap("terms", 1, termsSnap(items...)))
	topic := fmt.Sprint("term:", snap["buf"])
	c.WaitSub(t, topic)
	c.Send(t, Snap(topic, 1, snap))
	p.WaitJS(termView + `.querySelector(".trow")`)
}

func termTexts(p *Page) []string {
	return Eval[[]string](p, `[...`+termView+`.querySelectorAll(".term-rows .trow-text")].map((e) => e.textContent)`)
}

// Rows the laptop wrapped at its width join again and wrap at the
// phone's; rows stay LTR; the view follows new output until the reader
// scrolls up, then Follow brings it back; a chip picks another shell.
func TestTermRowsReflowFollowAndPick(t *testing.T) {
	for _, vp := range []Viewport{Phone} { // the desktop's panel: deskterm_test.go
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openTerm(t, vp)
			showTerm(t, p, c, termSnap(3, 0, 10, "$ echo", "aaaaaaaaaa", "bbb", "done"),
				termItem(3, "zsh", false, true), termItem(9, "build", true, false))
			got := termTexts(p)
			if strings.Join(got, "|") != "$ echo|aaaaaaaaaabbb|done" {
				t.Errorf("rows %q, want the laptop-wrapped row joined", got)
			}
			if !Eval[bool](p, termView+`.querySelector('.term-chip[data-buf="9"] .chip-dot.dot-busy') !== null`) {
				t.Error("the busy shell's chip has no busy dot")
			}
			cs := Eval[[]string](p, `(() => { const s = getComputedStyle(`+termView+`.querySelector(".trow")); return [s.direction, s.unicodeBidi]; })()`)
			if cs[0] != "ltr" || (cs[1] != "isolate" && cs[1] != "isolate-override") {
				t.Errorf("terminal rows run %v, want ltr isolated", cs)
			}

			rows := make([]string, 200)
			for i := range rows {
				rows[i] = fmt.Sprint("out ", i)
			}
			c.Send(t, Delta("term:3", 2, map[string]any{"kind": "lines", "first": 4, "last": 4, "lines": rows, "tick": 4}))
			const atEnd = `(() => { const b = ` + termView + `.querySelector(".view-body"); return b.scrollHeight - b.scrollTop - b.clientHeight < 48; })()`
			p.WaitJS(`(` + termView + `.innerText.includes("out 199")) && ` + atEnd)
			Eval[bool](p, `(() => { const b = `+termView+`.querySelector(".view-body"); b.scrollTop = 0; b.dispatchEvent(new Event("scroll")); return true; })()`)
			p.WaitJS(termView + `.querySelector(".term-follow .chip")`)
			c.Send(t, Delta("term:3", 3, map[string]any{"kind": "lines", "first": 204, "last": 204, "lines": []string{"late"}, "tick": 5}))
			p.WaitJS(termView + `.innerText.includes("late")`)
			if Eval[bool](p, atEnd) {
				t.Error("new output pulled the reader down while they read above")
			}
			p.TapJS(termView + `.querySelector(".term-follow .chip")`)
			p.WaitJS(atEnd + ` && !` + termView + `.querySelector(".term-follow")`)

			p.TapJS(termView + `.querySelector('.term-chip[data-buf="9"] .chip')`)
			p.WaitJS(`location.hash === "#/term/9"`)
			var unsub, sub bool
			for !unsub || !sub {
				switch m := c.Next(t).(type) {
				case *webproto.Unsub:
					unsub = unsub || slices.Contains(m.Topics, "term:3")
				case *webproto.Sub:
					sub = sub || slices.Contains(m.Topics, "term:9")
				}
			}
		})
	}
}

// Older rows page in from the top with term.history and the reader's
// place stays.
func TestTermHistoryPages(t *testing.T) {
	p, c := openTerm(t, Phone)
	rows := make([]string, 2000)
	for i := range rows {
		rows[i] = fmt.Sprint("row ", 2000+i)
	}
	showTerm(t, p, c, termSnap(3, 2000, 80, rows...), termItem(3, "zsh", false, true))
	p.TapJS(termView + `.querySelector(".term-older")`)
	r := expectReq(t, c, "term.history")
	var a webproto.TermHistoryArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.Buf != 3 || a.Before != 2000 {
		t.Fatalf("term.history args %s", r.Args)
	}
	old := make([]string, 2000)
	for i := range old {
		old[i] = fmt.Sprint("row ", i)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"first": 0, "rows": old, "more": false})})
	p.WaitJS(`(() => { const t = [...` + termView + `.querySelectorAll(".trow-text")]; return t.length === 4000 && t[0].textContent === "row 0"; })()`)
	if Eval[bool](p, termView+`.querySelector(".term-older") !== null`) {
		t.Error("Load older rows is still there with nothing older")
	}
}

// No shell: say so and offer one (term.new), which then shows; a shell
// that ended says so with Open a new one.
func TestTermNoneNewAndEnded(t *testing.T) {
	p, c := openTerm(t, Desktop)
	c.Send(t, Snap("terms", 1, termsSnap()))
	p.WaitJS(termView + `.innerText.includes("No terminal is open on the laptop.")`)
	p.TapJS(`[...` + termView + `.querySelectorAll(".btn")].find((b) => b.textContent === "Open one")`)
	r := expectReq(t, c, "term.new")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"buf": 12})})
	ended := termItem(12, "zsh", false, true)
	ended["exited"] = true
	c.Send(t, Delta("terms", 2, termsSnap(ended)))
	c.WaitSub(t, "term:12")
	c.Send(t, Snap("term:12", 1, termSnap(12, 0, 80, "$ exit")))
	p.WaitJS(termView + `.querySelector(".term-ended")?.innerText.includes("This terminal ended.")`)
}

// expectSend reads a term.send to shell 3 and answers it; it returns the text.
func expectSend(t *testing.T, c *HubConn) string {
	t.Helper()
	r := expectReq(t, c, "term.send")
	var a webproto.TermSendArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.Buf != 3 {
		t.Fatalf("term.send args %s", r.Args)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
	return a.Text
}

func tapKey(p *Page, label string) {
	p.T.Helper()
	p.TapJS(`[...` + termView + `.closest(".view").querySelectorAll(".keybar .key")].find((k) => k.textContent === ` + jsq(label) + `)`)
}

const termLine = `document.querySelector('[data-view="term"] .term-line')`

// The command line sends a line with Enter (or Send); the key bar sends
// Esc and arrows; Ctrl and Alt are sticky for one key, locked on a second
// tap; ^C is term.interrupt; every key is at least 44 px.
func TestTermTypeKeysAndInterrupt(t *testing.T) {
	for _, vp := range []Viewport{Phone} { // the desktop types into the shell: deskterm_test.go
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openTerm(t, vp)
			showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
			p.TapJS(termLine)
			p.Key("echo hi")
			p.WaitJS(termLine + `.value === "echo hi"`)
			if vp.Mobile {
				p.TapJS(`document.querySelector('[data-view="term"] .term-input .send .btn')`)
			} else {
				p.Key("\r")
			}
			if got := expectSend(t, c); got != "echo hi\r" {
				t.Fatalf("sent %q, want the line and Enter", got)
			}
			p.WaitJS(termLine + `.value === ""`)

			// Thumb-sized keys on a phone; a desktop's are a mouse's (22 px).
			min := 44
			if !vp.Mobile {
				min = 22
			}
			small := Eval[[]float64](p, fmt.Sprintf(`[...document.querySelectorAll('[data-view="term"] .keybar .key')].map((k) => Math.min(k.getBoundingClientRect().width, k.getBoundingClientRect().height)).filter((s) => s < %d)`, min))
			if len(small) > 0 {
				t.Errorf("keys smaller than %d px: %v", min, small)
			}
			for _, k := range []struct{ key, want string }{{"Esc", "\x1b"}, {"↑", "\x1b[A"}, {"Tab", "\t"}} {
				tapKey(p, k.key)
				if got := expectSend(t, c); got != k.want {
					t.Errorf("%s sent %q, want %q", k.key, got, k.want)
				}
			}
			// Ctrl for one key.
			tapKey(p, "Ctrl")
			p.TapJS(termLine)
			p.Key("c")
			if got := expectSend(t, c); got != "\x03" {
				t.Errorf("Ctrl then c sent %q", got)
			}
			if Eval[string](p, termLine+`.value`) != "" {
				t.Error("the Ctrl key's letter went into the line")
			}
			p.Key("x")
			p.WaitJS(termLine + `.value === "x"`)
			setInput(p, termLine, "")
			// Ctrl locked: every letter is a control key until Ctrl is tapped again.
			tapKey(p, "Ctrl")
			tapKey(p, "Ctrl")
			p.WaitJS(`document.querySelector('[data-view="term"] .key.is-locked')`)
			p.TapJS(termLine)
			p.Key("a")
			p.Key("b")
			if a, b := expectSend(t, c), expectSend(t, c); a != "\x01" || b != "\x02" {
				t.Errorf("locked Ctrl sent %q %q", a, b)
			}
			tapKey(p, "Ctrl")
			// Alt with an arrow.
			tapKey(p, "Alt")
			tapKey(p, "←")
			if got := expectSend(t, c); got != "\x1b[1;3D" {
				t.Errorf("Alt ← sent %q", got)
			}
			tapKey(p, "^C")
			r := expectReq(t, c, "term.interrupt")
			var a webproto.BufArgs
			if err := json.Unmarshal(r.Args, &a); err != nil || a.Buf != 3 {
				t.Errorf("term.interrupt args %s", r.Args)
			}
		})
	}
}

// Paste opens a sheet (the page never reads the clipboard): a short paste
// sends as it is; a big one asks first how many lines it sends.
func TestTermPasteSheet(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	tapKey(p, "Paste")
	const sheet = `document.querySelector(".sheet .paste")`
	p.WaitJS(sheet + `?.innerText.includes("Paste here, then Send")`)
	setInput(p, sheet+`.querySelector("textarea")`, "ls\npwd")
	p.TapJS(sheet + `.querySelector(".paste-send .btn")`)
	if got := expectSend(t, c); got != "ls\npwd" {
		t.Fatalf("pasted %q", got)
	}
	p.WaitJS(`!` + sheet)

	tapKey(p, "Paste")
	big := strings.Repeat(strings.Repeat("x", 49)+"\n", 39) + "end"
	setInput(p, sheet+`.querySelector("textarea")`, big)
	p.TapJS(sheet + `.querySelector(".paste-send .btn")`)
	p.WaitJS(sheet + `.querySelector(".paste-ask")?.textContent === "Send 40 lines?"`)
	noReq(t, c, 300*time.Millisecond)
	p.TapJS(sheet + `.querySelector(".paste-send .btn")`)
	if got := expectSend(t, c); got != big {
		t.Fatalf("big paste sent %d bytes, want %d", len(got), len(big))
	}
}

// Offline every key locks and says so; nothing typed then is sent later.
func TestTermOfflineKeysDropped(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	c.Drop()
	p.WaitJS(`document.querySelector('[data-view="term"] .keybar-off')?.textContent === "Offline"`)
	if got := Eval[string](p, `document.querySelector('[data-view="term"] .keybar .key').getAttribute("aria-disabled")`); got != "true" {
		t.Errorf("offline key aria-disabled %q", got)
	}
	tapKey(p, "Esc")
	tapKey(p, "^C")
	c2 := p.S.Hub.Accept(t)
	c2.Handshake(t, "c-1")
	p.WaitJS(`!document.querySelector('[data-view="term"] .keybar-off')`)
	noReq(t, c2, 500*time.Millisecond)
}

// A bell from the laptop shows a dot in the open terminal view.
func TestTermBellDot(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	c.Send(t, attention("bell", ""))
	p.WaitJS(termView + `.querySelector(".bell-dot")?.getAttribute("aria-label") === "A shell rang"`)
}

// A full-screen program: the rows become a fixed grid at the laptop's
// size with a banner that says so, Open on laptop and Rows (the guess can
// be wrong); keys still work. When the program ends the rows come back.
func TestTermScreenModeGrid(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openTerm(t, vp)
			showTerm(t, p, c, termSnap(3, 0, 80, "$ top"), termItem(3, "zsh", true, true))
			c.Send(t, Delta("term:3", 2, map[string]any{"kind": "lines", "first": 0, "last": 1, "lines": []string{"load 2", "cpu 4%", "mem 2G", "q quit"}, "tick": 4}))
			c.Send(t, Delta("term:3", 3, map[string]any{"kind": "mode", "screen": true, "cols": 100, "height": 4}))
			p.WaitJS(termView + `.querySelector(".term-banner")?.innerText.includes("You see the laptop's size (100×4).")`)
			if got := Eval[string](p, termView+`.querySelector(".term-screen").textContent`); got != "load 2\ncpu 4%\nmem 2G\nq quit" {
				t.Errorf("screen grid %q", got)
			}
			if Eval[bool](p, `document.documentElement.scrollWidth > document.documentElement.clientWidth`) {
				t.Error("the 100-column grid makes the page scroll sideways")
			}
			if vp.Mobile {
				tapKey(p, "Esc")
			} else {
				p.WaitJS(`document.activeElement === document.querySelector('[data-view="term"] .dterm-input')`)
				p.Key(kb.Escape)
			}
			if got := expectSend(t, c); got != "\x1b" {
				t.Errorf("a key in screen mode sent %q", got)
			}
			p.TapJS(`[...` + termView + `.querySelectorAll(".term-banner .btn")].find((b) => b.textContent === "Open on laptop")`)
			r := expectReq(t, c, "term.show_on_laptop")
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
			p.WaitJS(`(` + toastsJS + `).includes("Shown on the laptop")`)

			p.TapJS(`[...` + termView + `.querySelectorAll(".term-banner .btn")].find((b) => b.textContent === "Rows")`)
			p.WaitJS(`!` + termView + `.querySelector(".term-screen") && ` + termView + `.querySelector(".term-rows")`)
			// The about sheet turns screen mode back on.
			p.TapJS(termView + `.querySelector(".term-info-btn")`)
			p.WaitJS(`document.querySelector(".sheet .term-about")?.innerText.includes("the laptop's (100 columns)")`)
			p.TapJS(`[...document.querySelectorAll(".sheet .term-toggles .chip")].find((b) => b.textContent === "Screen mode")`)
			p.WaitJS(termView + `.querySelector(".term-screen")`)
			p.TapJS(`[...document.querySelectorAll(".sheet .btn")].find((b) => b.textContent === "Close")`)

			c.Send(t, Delta("term:3", 4, map[string]any{"kind": "mode", "screen": false, "cols": 100, "height": 4}))
			p.WaitJS(`!` + termView + `.querySelector(".term-screen") && ` + termView + `.querySelector(".term-rows")`)
		})
	}
}

// Wrap off: long rows scroll sideways inside the rows box, not the page.
func TestTermWrapToggle(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 0, strings.Repeat("long-row-", 30)), termItem(3, "zsh", false, true))
	h0 := Eval[float64](p, termView+`.querySelector(".trow").getBoundingClientRect().height`)
	p.TapJS(termView + `.querySelector(".term-info-btn")`)
	p.TapJS(`[...document.querySelectorAll(".sheet .term-toggles .chip")].find((b) => b.textContent === "Wrap")`)
	p.WaitJS(termView + `.querySelector(".term-rows.is-nowrap")`)
	if h1 := Eval[float64](p, termView+`.querySelector(".trow").getBoundingClientRect().height`); h1 >= h0 {
		t.Errorf("row height %.0f with wrap off, %.0f with wrap on; want one line", h1, h0)
	}
	if Eval[bool](p, `document.documentElement.scrollWidth > document.documentElement.clientWidth`) {
		t.Error("a long row makes the page scroll sideways")
	}
}
