//go:build browser

package web

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Run and Debug on the phone (UI page 11) against the fake hub, 390×844
// touch: the page changes shape with the session instead of showing
// empty "Not debugging." panes.

func debugIdle() webproto.DebugSnap {
	s := debugPaused()
	s.Active, s.Stopped, s.Status, s.Name, s.Thread, s.Frame = false, false, "", "", 0, 0
	s.Frames, s.Scopes, s.Threads = []webproto.DebugFrame{}, []webproto.DebugScope{}, []webproto.DebugThread{}
	return s
}

// Idle: the configuration is one pill and a split ▶ Start ▾; the
// breakpoints list with + Add breakpoint; Watch, Variables and Call
// stack fold into one "While debugging" group.
func TestPhoneDebugIdle(t *testing.T) {
	p, c := openRunView(t, Phone, "run", "debug", "debug")
	c.Send(t, Snap("debug", 1, debugIdle()))
	p.WaitJS(`document.querySelector('.rd-cfg')?.textContent.includes("Debug covreport")`)
	if Eval[bool](p, `!!document.querySelector('.rd-pill, select, .dbg-cfg') || document.querySelector('[data-view="debug"]').textContent.includes("Not debugging")`) {
		t.Error("idle shows a control pill, a select or empty Not debugging panes")
	}
	if got := Eval[string](p, `[...document.querySelectorAll('.pg-sec')].map(s => s.dataset.sec).join()`); got != "bps,while" {
		t.Errorf("sections %q, want bps,while", got)
	}
	if !Eval[bool](p, `!!document.querySelector('.rd-bp[data-bp="cmd/covreport/filter.go:13"] .rd-ok.on') && document.querySelector('.rd-bp[data-bp="cmd/covreport/filter.go:13"] .t b').textContent === "filter.go:13" && !document.querySelector('.rd-bp[data-bp="cmd/covreport/filter.go:18"] .rd-ok.on')`) {
		t.Error("breakpoint rows: file:line with a tick when verified")
	}
	if got := Eval[string](p, `document.querySelector('.pg-row[data-act="watch"] .r').textContent`); got != "2 · +" {
		t.Errorf("watch row %q", got)
	}
	p.Tap(`.rd-split [data-act="start"]`)
	reply(t, c, expectOp(t, c, webproto.OpDebugStart, `{"config":"Debug covreport"}`), nil)
	p.Tap(`.rd-split [data-act="start-more"]`)
	p.WaitJS(`[...document.querySelectorAll('.pg-menu button')].map(b => b.dataset.act).join() === "run,console"`)
	p.Tap(`.pg-menu [data-act="run"]`)
	reply(t, c, expectOp(t, c, webproto.OpDebugRun, `{"config":"Debug covreport"}`), nil)

	p.Tap(`.rd-cfg`)
	p.WaitJS(`document.querySelectorAll('.pg-picker[data-picker="config"] [role="radio"]').length === 2`)
	p.Tap(`.pg-picker [data-choice="Serve docs"]`)
	reply(t, c, expectOp(t, c, webproto.OpDebugSelect, `{"config":"Serve docs"}`), nil)

	p.Tap(`[data-act="bp-add"]`)
	p.Do(chromedp.SendKeys(`.pg-picker[data-picker="bp"] input[name="path"]`, "cart/cart.go"))
	p.Do(chromedp.SendKeys(`.pg-picker[data-picker="bp"] input[name="line"]`, "27"))
	p.Tap(`[data-act="bp-save"]`)
	reply(t, c, expectOp(t, c, webproto.OpDebugBp, `{"op":"set","path":"cart/cart.go","line":27}`), nil)
	if Eval[bool](p, `document.documentElement.scrollWidth > innerWidth`) {
		t.Error("the page scrolls sideways")
	}
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

// Paused: a floating control pill (continue and the steps), where it
// stopped, the variables and the call stack as rows; a long press on a
// value gives Ask AI / Add to chat / Watch it / Copy value; the Debug
// console docked at the bottom evaluates in the paused frame.
func TestPhoneDebugPaused(t *testing.T) {
	p, c := openRunView(t, Phone, "run", "debug", "debug")
	c.Send(t, Snap("debug", 1, debugPaused()))
	p.WaitJS(`document.querySelectorAll(".rd-frame").length === 2`)
	if got := Eval[string](p, `[...document.querySelectorAll('.rd-pill button')].map(b => b.dataset.act).join()`); got != "continue,step_over,step_into,step_out,restart,stop" {
		t.Errorf("pill %q", got)
	}
	if got := Eval[string](p, `document.querySelector('.rd-status').textContent`); got != "Paused on breakpoint · filter.go:13" {
		t.Errorf("status %q", got)
	}
	if got := Eval[string](p, `[...document.querySelectorAll('.pg-sec')].map(s => s.dataset.sec).join()`); got != "vars,stack,watch,bps" {
		t.Errorf("sections %q", got)
	}
	p.Tap(`.rd-pill [data-act="step_over"]`)
	reply(t, c, expectOp(t, c, webproto.OpDebugControl, `{"op":"step_over"}`), nil)
	p.Tap(`.rd-var[data-key="s:Locals/lf"]`)
	reply(t, c, expectOp(t, c, webproto.OpDebugExpand, `{"key":"s:Locals/lf","open":true}`), nil)
	p.Tap(`.rd-frame[data-frame="1002"]`)
	reply(t, c, expectOp(t, c, webproto.OpDebugFrame, `{"id":1002}`), nil)

	Eval[bool](p, `(document.querySelector('.rd-var[data-key="s:Locals/i"]').dispatchEvent(new MouseEvent("contextmenu", {bubbles: true, cancelable: true, clientX: 200, clientY: 300})), true)`)
	p.WaitJS(`[...document.querySelectorAll('.pg-menu button')].map(b => b.dataset.act).join() === "ask,chat,watch,copy"`)
	p.Tap(`.pg-menu [data-act="watch"]`)
	reply(t, c, expectOp(t, c, webproto.OpDebugWatch, `{"op":"add","expr":"i"}`), nil)

	p.Tap(`.rd-con`)
	p.WaitJS(`document.querySelectorAll('.pg-picker[data-picker="console"] .rd-log > div').length === 4`)
	p.Do(chromedp.SendKeys(`.pg-picker[data-picker="console"] input`, "i+1"+kb.Enter))
	reply(t, c, expectOp(t, c, webproto.OpDebugEval, `{"expr":"i+1"}`), nil)
	if Eval[bool](p, `document.documentElement.scrollWidth > innerWidth`) {
		t.Error("the page scrolls sideways")
	}
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

// A frame goes to the chat as its source line: Ask AI about this puts
// it into the next message with the question in the box.
func TestPhoneDebugFrameToChat(t *testing.T) {
	p, c := openRunView(t, Phone, "run", "debug", "debug")
	c.Send(t, Snap("debug", 1, debugPaused()))
	p.WaitJS(`document.querySelectorAll(".rd-frame").length === 2`)
	c.Send(t, Snap("chat", 1, chatWith(false)))
	Eval[bool](p, `(document.querySelector('.rd-frame[data-frame="1001"]').dispatchEvent(new MouseEvent("contextmenu", {bubbles: true, cancelable: true, clientX: 200, clientY: 400})), true)`)
	p.Tap(`.pg-menu [data-act="ask"]`)
	p.WaitJS(`location.hash.startsWith("#/chat") && [...document.querySelectorAll('.composer-attach .chat-chip')].some(c => c.textContent.includes("main.shortenCoverLine")) && document.querySelector('.composer textarea')?.value.includes("filter.go:13")`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}
