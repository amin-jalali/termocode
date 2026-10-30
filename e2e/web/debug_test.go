//go:build browser

package web

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// RUN AND DEBUG and the DEBUG CONSOLE (WEBP-4) against the fake hub.

const dbgView = `document.querySelector('[data-view="debug"]')`

// The paused session shows its configuration, stack, variables, watches
// and breakpoints; the toolbar's steps, a frame, an opened variable, a
// new watch and a removed breakpoint each send the laptop one fixed op;
// removing every breakpoint asks first, Cancel first and focused.
func TestDebugViewSteps(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openRunView(t, vp, "run", "debug", "debug")
			c.Send(t, Snap("debug", 1, debugPaused()))
			p.WaitJS(`document.querySelectorAll(".dv-frame").length === 2`)
			if got := Eval[string](p, `document.querySelector(".dbg-cfg").value`); got != "Debug covreport" {
				t.Errorf("config %q", got)
			}
			if !Eval[bool](p, `!!document.querySelector('.dbg-bar [data-act="continue"]') && !document.querySelector('.dbg-bar [data-act="pause"]')`) {
				t.Error("a paused session offers Continue, not Pause")
			}
			p.Tap(`.dbg-bar [data-act="step_over"]`)
			reply(t, c, expectOp(t, c, webproto.OpDebugControl, `{"op":"step_over"}`), nil)
			p.Tap(`.dv-frame[data-frame="1002"]`)
			reply(t, c, expectOp(t, c, webproto.OpDebugFrame, `{"id":1002}`), nil)
			p.Tap(`.dv-row[data-key="s:Locals/lf"]`)
			reply(t, c, expectOp(t, c, webproto.OpDebugExpand, `{"key":"s:Locals/lf","open":true}`), nil)
			open := debugPaused()
			open.Open = []string{"s:Locals/lf"}
			open.Children = []webproto.DebugChildren{{Key: "s:Locals/lf", Vars: []webproto.DebugVar{{Name: "buf", Value: "[]uint8 len: 0", Type: "[]uint8"}}}}
			c.Send(t, Delta("debug", 2, open))
			p.WaitJS(`document.querySelector('.dv-row[data-key="s:Locals/lf/buf"]')?.textContent.includes("[]uint8 len: 0")`)

			p.Tap(`[data-act="watch-add"]`)
			p.Do(chromedp.SendKeys(".dw-input", "n*2"+kb.Enter))
			reply(t, c, expectOp(t, c, webproto.OpDebugWatch, `{"op":"add","expr":"n*2"}`), nil)

			Eval[bool](p, `(document.querySelector('.dv-bp[data-bp="cmd/covreport/filter.go:18"] .tx-acts').style.display = "flex", true)`)
			p.Tap(`.dv-bp[data-bp="cmd/covreport/filter.go:18"] [data-act="bp-remove"]`)
			reply(t, c, expectOp(t, c, webproto.OpDebugBp, `{"op":"remove","path":"cmd/covreport/filter.go","line":18}`), nil)
			Eval[bool](p, `(document.querySelector('[data-pane="bps"] .rp-actions').style.display = "flex", true)`)
			p.Tap(`[data-act="bp-clear"]`)
			p.WaitJS(`document.activeElement?.textContent === "Cancel"`)
			p.Key(kb.Enter)
			p.WaitJS(`!document.querySelector(".dbg-ask")`)
			p.Tap(`[data-act="bp-clear"]`)
			p.TapJS(ButtonByText("Remove All Breakpoints"))
			reply(t, c, expectOp(t, c, webproto.OpDebugBp, `{"op":"clear"}`), nil)
			if len(p.Problems()) > 0 {
				t.Errorf("problems: %v", p.Problems())
			}
		})
	}
}

// With no session the head starts the chosen configuration (a command
// configuration only runs) and the steps are not there.
func TestDebugViewStarts(t *testing.T) {
	p, c := openRunView(t, Desktop, "run", "debug", "debug")
	idle := debugPaused()
	idle.Active, idle.Stopped, idle.Frames, idle.Scopes, idle.Threads = false, false, []webproto.DebugFrame{}, []webproto.DebugScope{}, []webproto.DebugThread{}
	c.Send(t, Snap("debug", 1, idle))
	p.WaitJS(`document.querySelector(".dbg-start") && !document.querySelector(".dbg-bar")`)
	p.Tap(`.dbg-start [data-act="start"]`)
	reply(t, c, expectOp(t, c, webproto.OpDebugStart, `{"config":"Debug covreport"}`), nil)
	p.Tap(`.dbg-start [data-act="run"]`)
	reply(t, c, expectOp(t, c, webproto.OpDebugRun, `{"config":"Debug covreport"}`), nil)
	Eval[bool](p, `(document.querySelector(".dbg-cfg").value = "Serve docs", document.querySelector(".dbg-cfg").dispatchEvent(new Event("change", {bubbles: true})), true)`)
	reply(t, c, expectOp(t, c, webproto.OpDebugSelect, `{"config":"Serve docs"}`), nil)
}

// The DEBUG CONSOLE shows the laptop's console as text and evaluates a
// typed line in the paused frame; Up brings back the last line.
func TestDebugConsoleEvaluates(t *testing.T) {
	p, c := openRunView(t, Desktop, "run", "debug", "debug")
	s := debugPaused()
	s.Console = append(s.Console, webproto.DebugLine{Cat: "stderr", Text: "<b>panic</b>"})
	c.Send(t, Snap("debug", 1, s))
	p.Tap(`.dbg-start [data-act="console"]`)
	p.WaitJS(`document.querySelectorAll(".wb-panel .dcon-row").length === 5 && document.querySelector('.wb-panel-title.is-on')?.dataset.panel === "debug-console"`)
	if Eval[int](p, `document.querySelectorAll(".dcon-rows b").length`) != 0 {
		t.Fatal("console output became HTML")
	}
	p.Do(chromedp.SendKeys(".dcon-input", "user.Name"+kb.Enter))
	reply(t, c, expectOp(t, c, webproto.OpDebugEval, `{"expr":"user.Name"}`), nil)
	p.Do(chromedp.SendKeys(".dcon-input", kb.ArrowUp))
	p.WaitJS(`document.querySelector(".dcon-input").value === "user.Name"`)
}
