//go:build browser

package web

import (
	"fmt"
	"slices"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The owner's iPhone (2026-11-15, 430×932 Safari, light theme, after the
// web panel stopped and started again, second shell "web 2"): the
// terminal showed a dark box behind the text only and white everywhere
// else. The terminal fills the whole area between the shells strip and
// the key bar, in one background, in both page themes, and fits again
// after a reconnect, a shell switch, a tab switch and a turn.

// BigPhone is the owner's iPhone: 430×932 with touch.
var BigPhone = Viewport{Name: "bigphone", W: 430, H: 932, Scale: 3, Mobile: true}

// termFill says how the engine's screen sits in the terminal's area:
// the space left under and right of its cells (px), one cell's size, and
// the backgrounds of the area, the engine and its cells.
type termFill struct {
	Bottom, Right, CellH, CellW float64
	Rows                        int
	Area, Engine                string
	Cells                       []string
}

func readFill(p *Page) termFill {
	p.T.Helper()
	return Eval[termFill](p, `(() => {
		const v = `+termView+`, x = v.querySelector(".pterm-x"), s = x.querySelector(".xterm-screen");
		const a = x.getBoundingClientRect(), b = s.getBoundingClientRect();
		const rows = x.querySelectorAll(".xterm-rows > div").length;
		const sp = [...x.querySelectorAll(".xterm-rows span")].find((e) => e.textContent.length > 3);
		const cw = sp ? sp.getBoundingClientRect().width / sp.textContent.length : 10;
		const bg = (e) => getComputedStyle(e).backgroundColor;
		return {
			Bottom: a.bottom - b.bottom, Right: a.right - b.right,
			CellH: b.height / Math.max(1, rows), CellW: cw, Rows: rows,
			Area: bg(v.querySelector(".pterm-body")), Engine: bg(x.querySelector(".xterm-viewport")),
			Cells: [...new Set([...x.querySelectorAll(".xterm-rows span")].map(bg).filter((c) => c !== "rgba(0, 0, 0, 0)"))],
		};
	})()`)
}

// checkFill fails when the engine leaves more than a cell (and the
// area's padding) free, or the area and the engine differ in colour, or
// a cell paints a background of its own.
func checkFill(t *testing.T, p *Page, when string) {
	t.Helper()
	f := readFill(p)
	if f.Bottom > f.CellH+6 || f.Right > f.CellW+8 || f.Rows < 20 {
		t.Errorf("%s: the terminal leaves %.0f px under and %.0f px right of its %d rows (cell %.1f×%.1f), want it to fill its area", when, f.Bottom, f.Right, f.Rows, f.CellW, f.CellH)
	}
	if f.Area != f.Engine {
		t.Errorf("%s: the terminal's area is %s and the engine %s, want one background", when, f.Area, f.Engine)
	}
	if len(f.Cells) > 0 {
		t.Errorf("%s: cells paint their own background %v (the laptop's default), want the terminal's", when, f.Cells)
	}
}

// laptopColours answers a term.colors as the laptop's dark terminal
// panel paints: every run has its default background, the prompt green.
func laptopColours(t *testing.T, c *HubConn, rows int) {
	t.Helper()
	r := expectReq(t, c, "term.colors")
	spans := [][]int{}
	for i := 0; i < rows; i++ {
		spans = append(spans, []int{i, 0, 5, 0}, []int{i, 5, 14, 1})
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{
		"tick": 4, "shown": true,
		"styles": []map[string]any{{"fg": "#23d18b", "bg": "#181818", "b": true}, {"fg": "#cccccc", "bg": "#181818"}},
		"spans":  spans,
	})})
}

// subSkipping reads messages until a sub that names topic, past unsubs
// and the colour requests of the shell before.
func subSkipping(t *testing.T, c *HubConn, topic string) {
	t.Helper()
	for {
		switch m := c.Next(t).(type) {
		case *webproto.Sub:
			if slices.Contains(m.Topics, topic) {
				return
			}
		case *webproto.Unsub, *webproto.Req:
		default:
			t.Fatalf("page sent %s while a sub for %s was expected", show(m), topic)
		}
	}
}

func TestPhoneTermFillsItsAreaAfterReconnect(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			p, c := openTerm(t, BigPhone)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": theme})
			items := []map[string]any{termItem(3, "fu 1", false, true), termItem(9, "web 2", false, false)}
			showTerm(t, p, c, termSnap(3, 0, 120, "fu@fu:~$ ls", "a.go  b.go", "fu@fu:~$ "), items...)
			// The second shell.
			p.TapJS(termView + `.querySelector('.term-chip[data-buf="9"] .chip')`)
			subSkipping(t, c, "term:9")
			rows := []string{"fu@fu:~$ htop", "fu@fu:~$ ls", "fu@fu:~$ "}
			c.Send(t, Snap("term:9", 1, termSnap(9, 0, 120, rows...)))
			p.WaitJS(termView + `.querySelector(".pterm-x .xterm-rows")?.textContent.includes("htop")`)
			// The web panel stops and starts: the page comes back on a new link.
			c2, _ := reconnect(t, p, c, "c-1")
			c2.Send(t, Snap("terms", 1, termsSnap(items...)))
			again := termSnap(9, 0, 120, rows...)
			again["tick"] = 4 // new output since: the page asks the colours
			c2.Send(t, Snap("term:9", 1, again))
			laptopColours(t, c2, len(rows))
			p.WaitJS(termView + `.querySelector('.pterm-x .xterm-rows [class*="xterm-fg-"]')`)
			checkFill(t, p, "after the reconnect")
			light := Eval[bool](p, `(() => { const m = getComputedStyle(`+termView+`.querySelector(".xterm-viewport")).backgroundColor.match(/\d+/g).map(Number); return (m[0] + m[1] + m[2]) / 3 > 128; })()`)
			if light != (theme == "light") {
				t.Errorf("the %s page's terminal is light=%v", theme, light)
			}
			termShot(t, p, "fill-"+theme)
			// Another tab and back, then the phone turns: it fits again.
			p.GoTab("files")
			Eval[bool](p, `(location.hash = "#/term/9", true)`)
			subSkipping(t, c2, "term:9")
			c2.Send(t, Snap("term:9", 2, again))
			p.WaitJS(termView + `.querySelector(".pterm-x .xterm-rows")?.textContent.includes("htop")`)
			checkFill(t, p, "back from another tab")
			p.resize(BigPhone.H, BigPhone.W)
			p.WaitJS(fmt.Sprintf(`(() => { const x = %s.querySelector(".pterm-x"), s = x.querySelector(".xterm-screen"); const d = x.getBoundingClientRect().right - s.getBoundingClientRect().right; return d >= 0 && d < 20; })()`, termView))
			p.resize(BigPhone.W, BigPhone.H)
			p.WaitJS(fmt.Sprintf(`(() => { const x = %s.querySelector(".pterm-x"), s = x.querySelector(".xterm-screen"); const a = x.getBoundingClientRect(), b = s.getBoundingClientRect(); return a.bottom - b.bottom >= 0 && a.bottom - b.bottom < 30 && a.right - b.right >= 0 && a.right - b.right < 20; })()`, termView))
			checkFill(t, p, "after a turn")
			// The system theme flips: the terminal follows at once.
			other := map[string]string{"light": "dark", "dark": "light"}[theme]
			p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": other})
			p.WaitJS(`getComputedStyle(` + termView + `.querySelector(".pterm-body")).backgroundColor === getComputedStyle(` + termView + `.querySelector(".xterm-viewport")).backgroundColor`)
			if probs := p.Problems(); len(probs) > 0 {
				t.Errorf("problems: %v", probs)
			}
		})
	}
}
