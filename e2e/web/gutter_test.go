//go:build browser

package web

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop editor's glyph margin (WEBP-4): breakpoints, tests and the
// stopped row from the laptop; clicks there set breakpoints and run
// tests.

// waitSubAnyReq reads the page's messages until a sub for topic; the
// editor's own requests on the way go unanswered.
func waitSubAnyReq(t *testing.T, c *HubConn, topic string) {
	t.Helper()
	for {
		if m, ok := c.Next(t).(*webproto.Sub); ok {
			for _, x := range m.Topics {
				if x == topic {
					return
				}
			}
		}
	}
}

// glyphAt is the page point of the glyph margin on a 1-based row.
func glyphAt(p *Page, line int) (float64, float64) {
	pt := Eval[[]float64](p, fmt.Sprintf(`(() => {
		const f = %s, d = f.contentDocument, fr = f.getBoundingClientRect();
		const n = [...d.querySelectorAll(".margin .line-numbers")].find((e) => e.textContent.trim() === "%d");
		const g = d.querySelector(".glyph-margin").getBoundingClientRect(), r = n.getBoundingClientRect();
		return [fr.left + g.left + g.width / 2, fr.top + r.top + r.height / 2];
	})()`, deskFrame, line))
	return pt[0], pt[1]
}

// reqFor is the page's next request for op, past the editor's own reads.
func reqFor(t *testing.T, c *HubConn, op, args string) *webproto.Req {
	t.Helper()
	for {
		if r, ok := c.Next(t).(*webproto.Req); ok && r.Op == op {
			if args != "" && canonJSON(t, r.Args) != canonJSON(t, []byte(args)) {
				t.Fatalf("%s args %s; want %s", op, r.Args, args)
			}
			return r
		}
	}
}

func TestGutterBreakpointsAndTests(t *testing.T) {
	p, c := openFiles(t, Desktop)
	lines := make([]string, 12)
	for i := range lines {
		lines[i] = fmt.Sprintf("// row %d", i+1)
	}
	openDesk(t, p, c, 2, "pkg/a_test.go", lines...)
	waitSubAnyReq(t, c, "debug")
	c.Send(t, Snap("tests", 1, testsTree("idle", 1)))
	d := debugPaused()
	d.Frames[0].Path, d.Frames[0].Line = "pkg/a_test.go", 5
	d.Breakpoints = []webproto.DebugBreakpoint{{Path: "pkg/a_test.go", Line: 5, Verified: "yes"}, {Path: "pkg/a_test.go", Line: 11, Condition: "i > 2"}}
	c.Send(t, Snap("debug", 1, d))
	p.WaitJS(`(() => { const d = ` + deskDoc + `; return d.querySelector(".tc-g-test-passed") && d.querySelector(".tc-g-test-failed") && d.querySelector(".tc-g-bp") && d.querySelector(".tc-g-bp-cond") && d.querySelector(".tc-stop-line"); })()`)
	p.WaitJS(`document.querySelector(".statusbar.is-debugging")`)

	x, y := glyphAt(p, 7)
	p.Do(chromedp.MouseClickXY(x, y))
	reply(t, c, reqFor(t, c, webproto.OpDebugBp, `{"op":"toggle","path":"pkg/a_test.go","line":7}`), nil)
	x, y = glyphAt(p, 3)
	p.Do(chromedp.MouseClickXY(x, y))
	reply(t, c, reqFor(t, c, webproto.OpTestsRun, `{"id":"c:pkg/a_test.go::TestA"}`), nil)

	x, y = glyphAt(p, 9)
	p.Do(chromedp.MouseClickXY(x, y, chromedp.ButtonRight))
	p.WaitJS(ButtonByText("Debug Test"))
	p.TapJS(ButtonByText("Add Conditional Breakpoint…"))
	p.WaitJS(`document.activeElement === document.querySelector(".bp-ask-input")`)
	p.Do(chromedp.KeyEvent("n == 3" + kb.Enter))
	reply(t, c, reqFor(t, c, webproto.OpDebugBp, `{"op":"set","path":"pkg/a_test.go","line":9,"condition":"n == 3"}`), nil)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}
