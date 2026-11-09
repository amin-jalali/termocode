//go:build browser

package web

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// The AI chat's speed on the desktop (web perf): what a keystroke, a
// send, a streamed token and opening a long chat cost in a real Chrome,
// with a chat of 200 long messages (code blocks among them).

// chatPerfScript runs before the page: every chat frame from the hub
// keeps [script ms, ms to the next frame] in window.__tcChat (script:
// the frame's handler with the render it starts; the next frame: until
// that frame was drawn), and every key in the chat input keeps the ms
// from the key to its next frame in window.__tcKeys.
const chatPerfScript = `{
	window.__tcChat = [];
	window.__tcKeys = [];
	const after = (t0, into, proc) => requestAnimationFrame(() => setTimeout(() => into.push(proc === undefined ? Math.round((performance.now() - t0) * 10) / 10 : [proc, Math.round((performance.now() - t0) * 10) / 10])));
	const d = Object.getOwnPropertyDescriptor(WebSocket.prototype, "onmessage");
	Object.defineProperty(WebSocket.prototype, "onmessage", {
		configurable: true,
		get() { return d.get.call(this); },
		set(fn) {
			d.set.call(this, fn && function (ev) {
				const chat = typeof ev.data === "string" && ev.data.includes('"topic":"chat"');
				const t0 = performance.now();
				const r = fn.call(this, ev);
				if (chat) queueMicrotask(() => queueMicrotask(() => after(t0, window.__tcChat, Math.round((performance.now() - t0) * 10) / 10)));
				return r;
			});
		},
	});
	addEventListener("keydown", (e) => { if (e.target?.matches?.(".composer-box")) after(e.timeStamp, window.__tcKeys); }, true);
}`

// chatPerfHooks puts chatPerfScript in p before it opens.
func chatPerfHooks(p *Page) {
	p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		_, err := cdp.Call(ctx, tg, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: chatPerfScript})
		return err
	}))
}

// longChat is a chat of n messages: users ask a few lines, the AI
// answers with headings, paragraphs, a list and a 30-line code block.
func longChat(n int) []map[string]any {
	var msgs []map[string]any
	para := strings.Repeat("The build breaks because the handler reads the config before it is loaded. ", 4)
	var code strings.Builder
	for i := range 30 {
		fmt.Fprintf(&code, "\tif err := step%d(ctx, cfg); err != nil {\n\t\treturn fmt.Errorf(\"step %d: %%w\", err)\n\t}\n", i, i)
	}
	for i := range n {
		if i%2 == 0 {
			msgs = append(msgs, userMsg(i, fmt.Sprintf("Question %d: %s", i, para)))
			continue
		}
		block := md("codeblock")
		block["text"], block["lang"] = "func run() error {\n"+code.String()+"\treturn nil\n}\n", "go"
		doc := md("doc",
			md("h", mdText(fmt.Sprintf("Answer %d", i))),
			mdPara(para),
			md("ul", md("li", mdPara("Load the config first.")), md("li", md("p", mdText("Then "), md("code", mdText("run()")), mdText(" the steps."))), md("li", mdPara("Check each error."))),
			block,
			mdPara(para),
		)
		m := aiMsg(i, doc)
		m["model"] = "claude-sonnet-4-5"
		msgs = append(msgs, m)
	}
	return msgs
}

// chatPerf is one desktop page with the chat open and the hooks on.
type chatPerf struct {
	p *Page
	c *HubConn
	r *Perf
	n int // messages
}

func startChatPerf(t *testing.T, n int) *chatPerf {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	r := StartPerf(p)
	chatPerfHooks(p)
	c := p.Login("c-chat")
	return &chatPerf{p: p, c: c, r: r, n: n}
}

// open sends the chat's snap and waits for its newest message.
func (cp *chatPerf) open(t *testing.T) Span {
	t.Helper()
	msgs := longChat(cp.n)
	return cp.r.Measure("open a chat", func() {
		cp.c.Send(t, Snap("chat", 1, chatWith(false, msgs...)))
		if cp.n == 0 {
			cp.p.WaitJS(chatView + `?.querySelector(".composer textarea")`)
			return
		}
		cp.p.WaitJS(fmt.Sprintf(chatView+`?.querySelector('.msg[data-i="%d"]')`, cp.n-1))
	})
}

// typeKeys types text in the chat input; it returns the ms from each key
// to its next frame and the span.
func (cp *chatPerf) typeKeys(t *testing.T, text string) ([]float64, Span) {
	t.Helper()
	cp.p.TapJS(composer + `.querySelector("textarea")`)
	// aictx.js (Add to AI Chat) loads on the focus and then watches every
	// key: the keys are timed with it.
	cp.p.WaitJS(`performance.getEntriesByType("resource").some((e) => e.name.includes("/aictx.js"))`)
	Eval[bool](cp.p, `(window.__tcKeys.length = 0, true)`)
	sp := cp.r.Measure("type", func() {
		for _, k := range text {
			cp.p.Key(string(k))
		}
		cp.p.WaitJS(composer + `.querySelector("textarea").value.endsWith(` + jsq(text) + `)`)
	})
	return Eval[[]float64](cp.p, `window.__tcKeys`), sp
}

// stream adds a streaming answer that reads pre so far and sends the
// rest in tokens of a few characters, gap apart; it returns the [script,
// next frame] ms of each chat frame and the span.
func (cp *chatPerf) stream(t *testing.T, seq *uint64, pre string, tokens int, gap time.Duration) ([][2]float64, Span) {
	t.Helper()
	i := cp.n
	*seq++
	cp.c.Send(t, Delta("chat", *seq, map[string]any{"op": "message", "msg": map[string]any{"i": i, "role": "assistant", "streaming": true, "text": pre}}))
	cp.p.WaitJS(fmt.Sprintf(chatView+`?.querySelector('.msg[data-i="%d"] .cursor')`, i))
	Eval[bool](cp.p, `(window.__tcChat.length = 0, true)`)
	words := strings.Fields("Load the config before the handler reads it, then run each step and wrap its error so the log says which step broke the build.")
	sp := cp.r.Measure("stream", func() {
		for k := range tokens {
			*seq++
			w := words[k%len(words)] + " "
			if k%40 == 39 {
				w += "\n\n"
			}
			cp.c.Send(t, Delta("chat", *seq, map[string]any{"op": "append", "i": i, "text": w}))
			time.Sleep(gap)
		}
		cp.p.WaitJS(fmt.Sprintf(`window.__tcChat.length >= %d`, tokens))
	})
	return Eval[[][2]float64](cp.p, `window.__tcChat`), sp
}

// send types text, sends it with Enter, plays the laptop (the answer to
// chat.send, then the user's message) and returns the ms from the Enter
// to the message drawn.
func (cp *chatPerf) send(t *testing.T, seq *uint64, text string) float64 {
	t.Helper()
	cp.p.TapJS(composer + `.querySelector("textarea")`)
	cp.p.Key(text)
	i := cp.n
	Eval[bool](cp.p, fmt.Sprintf(`(window.__tcSent = new Promise((ok) => {
		let t0 = 0;
		addEventListener("keydown", (e) => { if (e.key === "Enter" && !t0) t0 = e.timeStamp; }, true);
		const look = () => document.querySelector('[data-view="chat"] .msg[data-i="%d"]') ? requestAnimationFrame(() => setTimeout(() => ok(performance.now() - t0))) : requestAnimationFrame(look);
		look();
	}), true)`, i))
	cp.p.Key(kb.Enter)
	r := expectReq(t, cp.c, "chat.send")
	reply(t, cp.c, r, map[string]any{"id": "c-1"})
	*seq++
	cp.c.Send(t, Delta("chat", *seq, map[string]any{"op": "message", "msg": userMsg(i, text)}))
	cp.n++
	return evalAwait[float64](cp.p, `window.__tcSent`)
}

// evalAwait is Eval of a promise's value.
func evalAwait[T any](p *Page, js string) T {
	p.T.Helper()
	ctx, cancel := context.WithTimeout(p.Ctx, wait)
	defer cancel()
	v, err := chromedp.Run(ctx, chromedp.Evaluate[T](js, chromedp.EvalAwaitPromise))
	if err != nil {
		p.T.Fatalf("eval %s: %v", short(js), err)
	}
	return v
}

// longAnswer is an answer of about n bytes so far: paragraphs and code.
func longAnswer(n int) string {
	var b strings.Builder
	for k := 0; b.Len() < n; k++ {
		if k%4 == 3 {
			b.WriteString("```go\nfunc step(ctx context.Context) error {\n\treturn load(ctx)\n}\n```\n\n")
			continue
		}
		b.WriteString("The handler reads the config before it is loaded, so the first request fails and the build breaks.\n\n")
	}
	return b.String()
}

type stats struct{ med, p95, max float64 }

func statsOf(v []float64) stats {
	if len(v) == 0 {
		return stats{}
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return stats{s[len(s)/2], s[len(s)*95/100], s[len(s)-1]}
}

func (s stats) String() string {
	return fmt.Sprintf("median %.1f ms, p95 %.1f ms, max %.1f ms", s.med, s.p95, s.max)
}

// TestPerfChatReport logs what the chat costs (no budgets; the guards
// below hold them). WEB_PERF_PROFILE=<step> profiles one step.
func TestPerfChatReport(t *testing.T) {
	for _, n := range []int{0, 200} {
		for _, cpu := range []float64{1, 4} {
			t.Run(fmt.Sprintf("%dmsgs-%gx", n, cpu), func(t *testing.T) {
				cp := startChatPerf(t, n)
				cp.r.Throttle(cpu)
				sp := cp.open(t)
				t.Logf("open: %s", sp)
				drain(cp.c, 200*time.Millisecond)
				keys, sp := cp.typeKeys(t, "why does the build break")
				t.Logf("keys: %s (%d keys) task/key %.1f ms; %s", statsOf(keys), len(keys), sp.TaskMs/float64(max(1, len(keys))), sp)
				seq := uint64(1)
				Eval[bool](cp.p, `(document.querySelector('[data-view="chat"] .composer-box').value = "", true)`)
				t.Logf("send→shown: %.1f ms", cp.send(t, &seq, "fix it"))
				for _, pre := range []int{0, 16 << 10, 48 << 10} {
					frames, sp := cp.stream(t, &seq, longAnswer(pre), 300, 10*time.Millisecond)
					cp.n++
					var proc, paint []float64
					for _, f := range frames {
						proc, paint = append(proc, f[0]), append(paint, f[1])
					}
					t.Logf("stream after %d B: script/frame: %s; frame→drawn: %s; task/token %.1f ms; %s", pre, statsOf(proc), statsOf(paint), sp.TaskMs/300, sp)
				}
			})
		}
	}
}

// chatAtEnd is true when the chat shows its end.
const chatAtEnd = `(() => { const b = document.querySelector('[data-view="chat"] .view-body'); return b.scrollHeight - b.scrollTop - b.clientHeight < 80; })()`

// A long answer streams at the cost of its end: a token lays out the
// last block of the text, not all of it. Before, the streaming answer
// was one text node: at 48 KB each token took about 6 ms of layout at
// 1x CPU (all 48 KB again), and at 4x the page fell behind the stream.
func TestPerfGuardChatStreamLaysOutItsEnd(t *testing.T) {
	cp := startChatPerf(t, 200)
	cp.open(t)
	drain(cp.c, 200*time.Millisecond)
	seq := uint64(1)
	pre := longAnswer(48 << 10)
	frames, sp := cp.stream(t, &seq, pre, 300, 10*time.Millisecond)
	var paint []float64
	for _, f := range frames {
		paint = append(paint, f[1])
	}
	per := sp.LayoutMs / 300
	t.Logf("a token after 48 KB: layout %.2f ms, frame→drawn %s; %s", per, statsOf(paint), sp)
	if per > 2 {
		t.Errorf("a token of a 48 KB answer took %.2f ms of layout, budget 2 ms", per)
	}
	if m := statsOf(paint).med; m > 34 {
		t.Errorf("a token was drawn %.1f ms after it came (median), budget 34 ms (two frames)", m)
	}
	const text = `document.querySelector('[data-view="chat"] .msg[data-i="200"] .msg-text')`
	if n := Eval[int](cp.p, `Math.max(...[...`+text+`.querySelectorAll("*"), `+text+`].flatMap((e) => [...e.childNodes].filter((n) => n.nodeType === 3).map((n) => n.data.length)))`); n > 4<<10 {
		t.Errorf("the streaming answer's text node holds %d characters, want at most 4 KiB", n)
	}
	// It reads as the one text it is and looks it (the height of the text
	// in one piece), and the view stays at its end.
	if got := Eval[string](cp.p, text+`.textContent`); !strings.HasPrefix(got, pre) || len(got) < len(pre)+300 {
		t.Errorf("the answer reads %d bytes, want the %d sent first and the tokens", len(got), len(pre))
	}
	if d := Eval[float64](cp.p, `(() => { const a = `+text+`; const b = a.cloneNode(false); b.textContent = a.textContent; a.after(b); const d = a.offsetHeight - b.offsetHeight; b.remove(); return d; })()`); d < -2 || d > 2 {
		t.Errorf("the answer in blocks is %.0f px taller than in one piece", d)
	}
	if !Eval[bool](cp.p, chatAtEnd) {
		t.Error("the view left the end of the streaming answer")
	}
}

// Opening a chat of long answers lays out only what shows: the messages
// out of view wait until they scroll in (content-visibility), and the
// view is at the end. Before, all 60 drawn messages were laid out: 39 ms
// of layout and a 55 ms task at 1x, a 139 ms task at 4x CPU.
func TestPerfGuardChatOpenLaysOutWhatShows(t *testing.T) {
	cp := startChatPerf(t, 200)
	t.Log(cp.open(t))
	cp.r.Measure("settle", func() {})
	const far = `document.querySelector('[data-view="chat"] .msg[data-i="141"] .msg-body')`
	const near = `document.querySelector('[data-view="chat"] .msg[data-i="199"] .msg-body')`
	if !Eval[bool](cp.p, near+`.checkVisibility({contentVisibilityAuto: true})`) {
		t.Fatal("the newest message is not drawn")
	}
	if Eval[bool](cp.p, far+`.checkVisibility({contentVisibilityAuto: true})`) {
		t.Error("a message 58 up is laid out while out of view")
	}
	if !Eval[bool](cp.p, chatAtEnd) {
		t.Error("the opened chat does not show its end")
		t.Log(Eval[string](cp.p, `(() => { const b = document.querySelector('[data-view="chat"] .view-body'); return `+"`top ${b.scrollTop}, height ${b.scrollHeight}, view ${b.clientHeight}`"+`; })()`))
	}
	// It is there for a reader, and drawn once it scrolls in.
	if !strings.Contains(Eval[string](cp.p, far+`.textContent`), "Answer 141") {
		t.Errorf("the far message reads %q", Eval[string](cp.p, far+`.textContent`))
	}
	Eval[bool](cp.p, `(`+far+`.scrollIntoView({block: "center"}), true)`)
	cp.p.WaitJS(far + `.checkVisibility({contentVisibilityAuto: true})`)
}
