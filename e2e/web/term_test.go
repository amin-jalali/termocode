//go:build browser

package web

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

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
	for _, vp := range Both {
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
	p.WaitJS(`location.hash === "#/term/12"`)
	ended := termItem(12, "zsh", false, true)
	ended["exited"] = true
	c.Send(t, Delta("terms", 2, termsSnap(ended)))
	c.WaitSub(t, "term:12")
	c.Send(t, Snap("term:12", 1, termSnap(12, 0, 80, "$ exit")))
	p.WaitJS(termView + `.querySelector(".term-ended")?.innerText.includes("This terminal ended.")`)
}
