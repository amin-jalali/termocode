//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Terminal colours (WEB-40): the page asks term.colors once output
// settles and paints the rows the laptop's screen coloured, across a row
// the laptop wrapped too; colours stay readable on this page; a row whose
// text changed is plain; About can turn them off.
func TestTermColours(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openTerm(t, vp)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": "dark"})
			showTerm(t, p, c, termSnap(3, 0, 10, "$ ls", "aaaaaaaaaa", "bbb", "done"), termItem(3, "zsh", false, true))
			r := expectReq(t, c, "term.colors")
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{
				"tick": 3, "shown": true,
				"styles": []map[string]any{{"fg": "#00ff00", "b": true}, {"fg": "#000001"}, {"fg": "#ffffff", "bg": "#aa0000"}},
				"spans":  [][]int{{0, 0, 1, 0}, {1, 8, 10, 1}, {2, 0, 2, 2}, {3, 0, 4, 1}},
			})})
			p.WaitJS(termView + `.querySelector(".tc")`)
			first := Eval[[]string](p, `(() => { const e = `+termView+`.querySelector(".term-rows .trow .tc"); const s = getComputedStyle(e); return [e.textContent, s.color, s.fontWeight]; })()`)
			if first[0] != "$" || first[1] != "rgb(0, 255, 0)" || first[2] != "700" {
				t.Errorf("first run %v", first)
			}
			// The joined line: the end of the wrapped row and the start of the next.
			runs := Eval[[]string](p, `[...`+termView+`.querySelectorAll(".term-rows .trow")[1].querySelectorAll(".tc")].map((e) => e.textContent + "|" + getComputedStyle(e).backgroundColor)`)
			if strings.Join(runs, ",") != "aa|rgba(0, 0, 0, 0),bb|rgb(170, 0, 0)" {
				t.Errorf("joined line runs %v", runs)
			}
			// A near-black colour on a dark page is lifted until it reads.
			dark := Eval[string](p, `getComputedStyle(`+termView+`.querySelectorAll(".term-rows .trow")[2].querySelector(".tc")).color`)
			if dark == "rgb(0, 0, 1)" {
				t.Error("an unreadable colour went to the page as it was")
			}
			// New output: the changed row is plain at once; the page asks again.
			c.Send(t, Delta("term:3", 2, map[string]any{"kind": "lines", "first": 3, "last": 4, "lines": []string{"next"}, "tick": 4}))
			p.WaitJS(`!` + termView + `.querySelectorAll(".term-rows .trow")[2].querySelector(".tc")`)
			expectReq(t, c, "term.colors")
			if probs := p.Problems(); len(probs) > 0 {
				t.Errorf("problems: %v", probs)
			}
		})
	}
}

// A shell that is not on the laptop's screen has no colours; About says
// what colours are and turns them off.
func TestTermColoursNotShownAndOff(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 10, "$ ls"), termItem(3, "zsh", false, true))
	r := expectReq(t, c, "term.colors")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"tick": 3, "shown": false, "styles": []any{}, "spans": [][]int{}})})
	p.TapJS(termView + `.querySelector(".term-info-btn")`)
	p.WaitJS(`document.querySelector(".term-colors-note")?.textContent === "Colours show for the rows on the laptop's screen, best effort."`)
	p.TapJS(`[...document.querySelectorAll(".term-toggles .chip")].find((b) => b.textContent.includes("Colours"))`)
	c.Send(t, Delta("term:3", 2, map[string]any{"kind": "lines", "first": 1, "last": 1, "lines": []string{"more"}, "tick": 4}))
	noPaint(t, c, "term.colors", 600*time.Millisecond)
}
