//go:build browser

package web

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The chat view (WEB-14, web UX 5.2): the open chat of the chat topic,
// its markdown drawn from the AST with elements only, code blocks left to
// right with a copy button, links that open only after a confirm sheet.

func md(k string, kids ...map[string]any) map[string]any {
	n := map[string]any{"k": k}
	if len(kids) > 0 {
		l := make([]any, len(kids))
		for i, c := range kids {
			l[i] = c
		}
		n["c"] = l
	}
	return n
}

func mdText(s string) map[string]any { return map[string]any{"k": "text", "text": s} }

func mdPara(s string) map[string]any { return md("p", mdText(s)) }

func userMsg(i int, text string) map[string]any {
	return map[string]any{"i": i, "role": "user", "md": md("doc", mdPara(text))}
}

func aiMsg(i int, doc map[string]any) map[string]any {
	return map[string]any{"i": i, "role": "assistant", "md": doc}
}

func chatWith(busy bool, msgs ...map[string]any) map[string]any {
	l := make([]any, len(msgs))
	for i, m := range msgs {
		l[i] = m
	}
	return map[string]any{"id": "c-1", "title": "Fix the build", "busy": busy, "messages": l}
}

const chatView = `document.querySelector('[data-view="chat"]')`

// openChat logs in on vp and sends the chat snapshot; on a phone the
// chat is the first tab, on a desktop it is the right column.
func openChat(t *testing.T, vp Viewport, chat map[string]any) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	c.Send(t, Snap("chat", 1, chat))
	p.WaitJS(chatView + `?.querySelector(".msg")`)
	return p, c
}

func TestChatMarkdownNoRawHTML(t *testing.T) {
	h := md("h", mdText("Plan"))
	h["level"] = 2
	code := md("code")
	code["text"] = "go test"
	block := md("codeblock")
	block["text"], block["lang"] = "package main\n\nfunc main() {}\n", "go"
	ol := md("ol", md("li", mdPara("third")))
	ol["start"] = 3
	good := md("link", mdText("docs"))
	good["href"] = "https://example.com/a"
	bad := md("link", mdText("bad link"))
	bad["href"] = "javascript:alert(1)"
	table := md("table")
	table["rows"] = [][]map[string]any{{md("th", mdText("A")), md("th", mdText("B"))}, {md("td", mdText("1")), md("td", mdText("2"))}}
	doc := md("doc", h,
		md("p", mdText(`<img src=x onerror="window.__xss=1">`), md("strong", mdText("bold")), md("em", mdText("soft")), code),
		block, md("ul", md("li", mdPara("one"))), ol, md("blockquote", mdPara("quoted")),
		md("p", good, mdText(" and "), bad), md("hr"), table)
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, _ := openChat(t, vp, chatWith(false, userMsg(0, "hi"), aiMsg(1, doc)))
			v := chatView
			p.WaitJS(v + `.querySelector(".md strong")?.textContent === "bold"`)
			if Eval[bool](p, v+`.querySelector("img, script, iframe") !== null || window.__xss === 1`) {
				t.Fatal("markup in a text node became an element")
			}
			if !Eval[bool](p, v+`.innerText.includes('<img src=x onerror="window.__xss=1">')`) {
				t.Error("the raw HTML text is not shown as text")
			}
			for js, what := range map[string]string{
				v + `.querySelector('[role="heading"]')?.textContent === "Plan"`:                                    "heading",
				v + `.querySelector(".md em")?.textContent === "soft"`:                                              "emphasis",
				v + `.querySelector(".md code.md-code")?.textContent === "go test"`:                                 "inline code",
				v + `.querySelector(".md ol")?.getAttribute("start") === "3"`:                                       "ordered list start",
				v + `.querySelector(".md blockquote")?.textContent === "quoted"`:                                    "quote",
				v + `.querySelectorAll(".md th").length === 2 && ` + v + `.querySelectorAll(".md td").length === 2`: "table",
				v + `.querySelector(".md hr") !== null`:                                                             "rule",
			} {
				if !Eval[bool](p, js) {
					t.Errorf("%s is not drawn", what)
				}
			}
			links := Eval[[]string](p, `[...`+v+`.querySelectorAll("a")].map((a) => a.getAttribute("href"))`)
			if len(links) != 1 || links[0] != "https://example.com/a" {
				t.Errorf("links %q, want only the https one", links)
			}
			if !Eval[bool](p, v+`.innerText.includes("bad link")`) {
				t.Error("the text of a refused link is gone")
			}

			// A code block stays left to right and isolated, even in RTL.
			Eval[bool](p, `(document.documentElement.dir = "rtl", true)`)
			cs := Eval[[]string](p, `(() => { const s = getComputedStyle(`+v+`.querySelector(".codeblock pre")); return [s.direction, s.unicodeBidi]; })()`)
			if cs[0] != "ltr" || (cs[1] != "isolate" && cs[1] != "isolate-override") {
				t.Errorf("code block runs %v, want ltr isolated", cs)
			}
			Eval[bool](p, `(document.documentElement.dir = "ltr", true)`)
		})
	}
}

// A link opens only from a confirm sheet: Cancel first and focused, Open
// is a real link to a new tab with no opener and no referrer.
func TestChatLinkAsksFirst(t *testing.T) {
	good := md("link", mdText("docs"))
	good["href"] = "https://example.com/a"
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, _ := openChat(t, vp, chatWith(false, aiMsg(0, md("doc", md("p", good)))))
			p.TapJS(chatView + `.querySelector("a.md-link")`)
			const ask = `document.querySelector(".sheet .link-ask")`
			p.WaitJS(ask + `?.innerText.includes("https://example.com/a")`)
			p.WaitJS(`document.activeElement?.textContent.trim() === "Cancel"`)
			open := Eval[[]string](p, `(() => { const a = `+ask+`.querySelector("a.btn"); return [a.getAttribute("href"), a.getAttribute("target"), a.getAttribute("rel")]; })()`)
			if open[0] != "https://example.com/a" || open[1] != "_blank" || !strings.Contains(open[2], "noopener") || !strings.Contains(open[2], "noreferrer") {
				t.Errorf("the Open link is %q", open)
			}
			p.Key(kb.Escape)
			p.WaitJS(`!` + ask)
			for _, u := range p.Requests() {
				if strings.Contains(u, "example.com") {
					t.Fatalf("the page went to %s without Open", u)
				}
			}
		})
	}
}

// Copy puts the code block's text on the clipboard and says so.
func TestChatCodeBlockCopy(t *testing.T) {
	block := md("codeblock")
	block["text"], block["lang"] = "go test ./...\n", "sh"
	p, _ := openChat(t, Phone, chatWith(false, aiMsg(0, md("doc", block))))
	// A fake clipboard: the test never touches a real one.
	Eval[bool](p, `(Object.defineProperty(navigator, "clipboard", {configurable: true, value: {writeText: (s) => { window.__copied = s; return Promise.resolve(); }}}), true)`)
	p.TapJS(chatView + `.querySelector(".codeblock .cb-copy")`)
	p.WaitJS(`window.__copied === "go test ./...\n"`)
	p.WaitJS(`(` + toastsJS + `).includes("Copied")`)
}

// An answer streams in by append deltas with a cursor, the view stays at
// the end while the reader is there, and the finished message replaces
// the plain text with its markdown. Offline the cursor freezes with a
// "paused" tag.
func TestChatStreamsOnPhone(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			long := strings.Repeat("line\n", 80)
			p, c := openChat(t, vp, chatWith(true, userMsg(0, long), map[string]any{"i": 1, "role": "assistant", "streaming": true}))
			const ai = `document.querySelector('[data-view="chat"] .msg-ai')`
			p.WaitJS(ai + `?.querySelector(".cursor")`)
			c.Send(t, Delta("chat", 2, map[string]any{"op": "append", "i": 1, "text": "Hello "}))
			c.Send(t, Delta("chat", 3, map[string]any{"op": "append", "i": 1, "text": "**world**"}))
			p.WaitJS(ai + `.querySelector(".msg-text").textContent.startsWith("Hello **world**")`)
			const body = `document.querySelector('[data-view="chat"] .view-body')`
			p.WaitJS(body + `.scrollHeight - ` + body + `.scrollTop - ` + body + `.clientHeight < 80`)

			c.Drop()
			p.WaitJS(ai + `.querySelector(".cursor.is-paused") && ` + ai + `.innerText.includes("paused")`)
			c2 := p.S.Hub.Accept(t)
			c2.Handshake(t, "c-1")
			p.WaitJS(`!` + ai + `.querySelector(".cursor.is-paused")`)

			done := aiMsg(1, md("doc", md("p", mdText("Hello "), md("strong", mdText("world")))))
			c2.Send(t, Delta("chat", 4, map[string]any{"op": "message", "msg": done}))
			c2.Send(t, Delta("chat", 5, map[string]any{"op": "state", "busy": false}))
			p.WaitJS(ai + `.querySelector(".md strong")?.textContent === "world" && !` + ai + `.querySelector(".cursor")`)
		})
	}
}

const composer = `document.querySelector('[data-view="chat"] .composer')`

// typeDraft focuses the composer and types text.
func typeDraft(p *Page, text string) {
	p.T.Helper()
	p.TapJS(composer + `.querySelector("textarea")`)
	p.Key(text)
	p.WaitJS(composer + `.querySelector("textarea").value === ` + jsq(text))
}

// expectReq reads the next request, which must be op; it returns it.
func expectReq(t *testing.T, c *HubConn, op string) *webproto.Req {
	t.Helper()
	r := nextReq(t, c)
	if r.Op != op {
		t.Fatalf("page sent %s; want %s", show(r), op)
	}
	return r
}

// Send sends the draft to the chat on the laptop and clears it; while the
// AI answers, Send becomes Stop. On a desktop Enter sends too.
func TestChatSendAndStop(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openChat(t, vp, chatWith(false, userMsg(0, "hi")))
			typeDraft(p, "run the tests")
			if vp.Mobile {
				p.TapJS(composer + `.querySelector(".send")`)
			} else {
				p.Key(kb.Enter)
			}
			r := expectReq(t, c, "chat.send")
			var a webproto.ChatSendArgs
			if err := json.Unmarshal(r.Args, &a); err != nil || a.Text != "run the tests" || a.ChatID != "c-1" {
				t.Fatalf("chat.send args %s", r.Args)
			}
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"id": "c-1"})})
			p.WaitJS(composer + `.querySelector("textarea").value === ""`)

			c.Send(t, Delta("chat", 2, map[string]any{"op": "state", "busy": true}))
			p.WaitJS(composer + `.querySelector(".stop")?.textContent === "Stop"`)
			p.TapJS(composer + `.querySelector(".stop")`)
			r = expectReq(t, c, "chat.stop")
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
			c.Send(t, Delta("chat", 3, map[string]any{"op": "state", "busy": false}))
			p.WaitJS(composer + `.querySelector(".send")`)
		})
	}
}

// The laptop refuses: another chat is open there (conflict) or the AI is
// still answering (busy). A calm line says why and the draft stays.
func TestChatConflictAndBusyKeepTheDraft(t *testing.T) {
	p, c := openChat(t, Phone, chatWith(false, userMsg(0, "hi")))
	typeDraft(p, "fix it")
	for _, tc := range []struct {
		code webproto.ErrCode
		want string
	}{
		{webproto.CodeConflict, "This chat is not open on the laptop. Open it there first."},
		{webproto.CodeBusy, "The AI is still answering. Stop it or wait."},
	} {
		p.TapJS(composer + `.querySelector(".send")`)
		r := expectReq(t, c, "chat.send")
		c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: tc.code, Msg: "laptop text"})
		p.WaitJS(composer + `.querySelector('[role="alert"]')?.textContent === ` + jsq(tc.want))
		if got := Eval[string](p, composer+`.querySelector("textarea").value`); got != "fix it" {
			t.Fatalf("after %s the draft is %q", tc.code, got)
		}
	}
}

// New chat asks the laptop; while the AI answers it says to stop first.
func TestChatNewChat(t *testing.T) {
	p, c := openChat(t, Desktop, chatWith(true, userMsg(0, "hi")))
	p.TapJS(`document.querySelector('[data-view="chat"] .chat-new')`)
	r := expectReq(t, c, "chat.new")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeBusy, Msg: "laptop text"})
	p.WaitJS(`(` + toastsJS + `).includes("The AI is answering. Stop it first.")`)
	p.TapJS(`document.querySelector('[data-view="chat"] .chat-new')`)
	r = expectReq(t, c, "chat.new")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"id": "c-2"})})
	c.Send(t, Delta("chat", 2, map[string]any{"op": "switched", "chat": map[string]any{"id": "c-2", "title": "New chat", "busy": false, "messages": []any{}}}))
	p.WaitJS(`document.querySelector('[data-view="chat"] .empty-state')`)
}

// Offline the composer locks (Send reads Offline and does nothing) but
// typing goes on; after the reconnect nothing was sent by itself and the
// draft is all there (web UX P5).
func TestChatDraftSurvivesOffline(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openChat(t, vp, chatWith(false, userMsg(0, "hi")))
			typeDraft(p, "half a thought")
			c.Drop()
			const send = `[data-view="chat"] .composer .send .btn`
			p.WaitJS(`document.querySelector(` + jsq(send) + `)?.textContent === "Offline"`)
			if got := Eval[string](p, `document.querySelector(`+jsq(send)+`).getAttribute("aria-disabled")`); got != "true" {
				t.Errorf("offline Send aria-disabled = %q", got)
			}
			p.Tap(send)
			p.TapJS(composer + `.querySelector("textarea")`)
			// A tap puts the caret where it lands (the box's middle); with
			// a wider font the draft reaches past it. Go to the end first.
			p.Key(kb.End)
			p.Key(" more")
			c2 := p.S.Hub.Accept(t)
			c2.Handshake(t, "c-1")
			p.WaitJS(`document.querySelector(` + jsq(send) + `)?.textContent === "Send"`)
			noReq(t, c2, 500*time.Millisecond)
			if got := Eval[string](p, composer+`.querySelector("textarea").value`); got != "half a thought more" {
				t.Errorf("after the reconnect the draft is %q", got)
			}
		})
	}
}

// A draft is kept on this device across a reload, but never one that
// looks like a secret.
func TestChatDraftKeptAcrossReloadButNoSecrets(t *testing.T) {
	for _, tc := range []struct{ draft, want string }{
		{"check the quit test", "check the quit test"},
		{"use key sk-" + "test1234567890abcdefXYZ please", ""},
	} {
		s := StartServer(t)
		p := NewPage(t, s, Phone)
		c := p.Login("c-1")
		c.Send(t, Snap("chat", 1, chatWith(false, userMsg(0, "hi"))))
		typeDraft(p, tc.draft)
		p.Do(chromedp.Reload())
		c2 := s.Hub.Accept(t)
		c2.Handshake(t, "c-2")
		c2.Subs(t, "chat")
		c2.Send(t, Snap("chat", 1, chatWith(false, userMsg(0, "hi"))))
		p.WaitJS(composer + `?.querySelector("textarea")`)
		if got := Eval[string](p, composer+`.querySelector("textarea").value`); got != tc.want {
			t.Errorf("draft %q after reload is %q, want %q", tc.draft, got, tc.want)
		}
		if tc.want == "" && Eval[bool](p, `Object.keys(localStorage).some((k) => (localStorage.getItem(k) || "").includes("sk-"))`) {
			t.Error("a secret-looking draft is in localStorage")
		}
	}
}

// noReq checks that the page sends nothing but subs for d.
func noReq(t *testing.T, c *HubConn, d time.Duration) {
	t.Helper()
	end := time.After(d)
	for {
		select {
		case m := <-c.in:
			if _, ok := m.(*webproto.Sub); !ok {
				t.Fatalf("page sent %s; want no request", show(m))
			}
		case <-end:
			return
		}
	}
}

// The chat list opens a stored chat to read here: the page pins it (it
// stops following the laptop's chat), cannot write to it, and Follow
// goes back to the laptop's chat, whatever it is by then.
func TestChatListOpensAndFollows(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openChat(t, vp, chatWith(false, userMsg(0, "hi")))
			c.Send(t, Snap("chats", 1, map[string]any{"chats": []any{
				map[string]any{"id": "c-1", "title": "Fix the build", "updated": 3000, "msgs": 1, "open": true},
				map[string]any{"id": "c-2", "title": "Old idea", "updated": 2000, "msgs": 1},
			}}))
			const view = `document.querySelector('[data-view="chat"]')`
			p.TapJS(view + `.querySelector(".chat-list-btn")`)
			p.WaitJS(view + `.querySelectorAll(".chat-row").length === 2`)
			if !Eval[bool](p, view+`.querySelector(".chat-row .on-laptop") !== null`) {
				t.Error("the laptop's chat has no mark")
			}
			p.TapJS(`[...` + view + `.querySelectorAll(".chat-row")].find((r) => r.textContent.includes("Old idea"))`)
			r := expectReq(t, c, "chat.open")
			if !strings.Contains(string(r.Args), `"c-2"`) {
				t.Fatalf("chat.open args %s", r.Args)
			}
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"id": "c-2", "title": "Old idea", "busy": false,
				"messages": []any{userMsg(0, "old question")}})})
			r = expectReq(t, c, "chat.pin")
			if !strings.Contains(string(r.Args), `true`) {
				t.Fatalf("chat.pin args %s", r.Args)
			}
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
			p.WaitJS(view + `.innerText.includes("old question") && ` + view + `.querySelector(".chat-pinned")`)
			if Eval[bool](p, composer+`?.querySelector("textarea") != null`) {
				t.Error("a chat not open on the laptop can be written to")
			}

			c.Send(t, Delta("chat", 2, map[string]any{"op": "switched", "chat": chatWith(false, userMsg(0, "laptop moved on"))}))
			p.Holds(view+`.innerText.includes("old question")`, 300*time.Millisecond)

			p.TapJS(view + `.querySelector(".chat-follow")`)
			r = expectReq(t, c, "chat.pin")
			if !strings.Contains(string(r.Args), `false`) {
				t.Fatalf("chat.pin args %s", r.Args)
			}
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
			p.WaitJS(view + `.innerText.includes("laptop moved on") && !` + view + `.querySelector(".chat-pinned")`)
			p.WaitJS(composer + `?.querySelector("textarea")`)
		})
	}
}

// A long chat arrives with its older messages as stubs. The page draws
// the newest 60, fetches a stub with chat.get_message when it scrolls
// into view, draws 60 more with Load older without moving what the
// reader sees, and Show the rest fetches the whole of a cut message.
func TestChatLongHistoryPages(t *testing.T) {
	msgs := []map[string]any{}
	for i := 0; i < 70; i++ {
		msgs = append(msgs, map[string]any{"i": i, "role": "user", "truncated": true})
	}
	for i := 70; i < 99; i++ {
		msgs = append(msgs, userMsg(i, fmt.Sprint("m", i)))
	}
	msgs = append(msgs, map[string]any{"i": 99, "role": "assistant", "text": "short part", "truncated": true})
	p, c := openChat(t, Phone, chatWith(false, msgs...))
	const view = `document.querySelector('[data-view="chat"]')`
	if n := Eval[int](p, view+`.querySelectorAll(".msg").length`); n != 60 {
		t.Errorf("drew %d messages, want the newest 60", n)
	}

	// Show the rest of the cut answer.
	p.TapJS(view + `.querySelector('.msg[data-i="99"] .msg-rest')`)
	for {
		r := expectReq(t, c, "chat.get_message")
		var a webproto.ChatGetMessageArgs
		_ = json.Unmarshal(r.Args, &a)
		m := userMsg(a.I, fmt.Sprint("old ", a.I))
		if a.I == 99 {
			m = map[string]any{"i": 99, "role": "assistant", "text": "short part and the long rest"}
		}
		c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(m)})
		if a.I == 99 {
			break
		}
	}
	p.WaitJS(view + `.querySelector('.msg[data-i="99"]').innerText.includes("the long rest")`)

	// Scroll to the top: the stubs there are fetched.
	Eval[bool](p, `(document.querySelector('[data-view="chat"] .view-body').scrollTop = 0, true)`)
	for seen := false; !seen; {
		r := expectReq(t, c, "chat.get_message")
		var a webproto.ChatGetMessageArgs
		_ = json.Unmarshal(r.Args, &a)
		c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(userMsg(a.I, fmt.Sprint("old ", a.I)))})
		seen = a.I == 40
	}
	p.WaitJS(view + `.querySelector('.msg[data-i="40"]').innerText.includes("old 40")`)

	top := `document.querySelector('.msg[data-i="40"]').getBoundingClientRect().top`
	before := Eval[float64](p, top)
	p.TapJS(view + `.querySelector(".chat-older")`)
	p.WaitJS(view + `.querySelectorAll(".msg").length === 100`)
	if after := Eval[float64](p, top); after < before-8 || after > before+8 {
		t.Errorf("Load older moved the reader's place: %.0f → %.0f px", before, after)
	}
	if Eval[bool](p, view+`.querySelector(".chat-older") !== null`) {
		t.Error("Load older is still there with nothing older")
	}
}

// A Persian message reads right to left with its code still left to
// right; an English one stays left to right in a Persian page.
func TestChatPersianRTL(t *testing.T) {
	fa := md("doc", md("p", mdText("تست‌ها رد شدند. فایل "), func() map[string]any { c := md("code"); c["text"] = "quit_test.go"; return c }(), mdText(" خط ۸۸ را ببین.")))
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, _ := openChat(t, vp, chatWith(false, userMsg(0, "why does it fail?"), aiMsg(1, fa)))
			const ai = `document.querySelector('[data-view="chat"] .msg-ai .md p')`
			p.WaitJS(ai)
			got := Eval[[]string](p, `[getComputedStyle(`+ai+`).direction, getComputedStyle(`+ai+`.querySelector("code")).direction, getComputedStyle(document.querySelector('[data-view="chat"] .msg-user p')).direction]`)
			if got[0] != "rtl" || got[1] != "ltr" || got[2] != "ltr" {
				t.Errorf("directions (persian, its code, english) = %v", got)
			}
		})
	}
}

// A pipeline answer shows its run and its steps; a step opens to show
// its tool lines and its answer.
func TestChatRunSteps(t *testing.T) {
	msg := map[string]any{"i": 1, "role": "assistant", "text": "Done.",
		"run": map[string]any{"id": "r-1", "profile": "review", "state": "done", "elapsed_ms": 125000},
		"steps": []any{
			map[string]any{"id": "s-1", "title": "Planner", "state": "done", "model": "m-small", "tools": []any{"⚙ Read a.go → 10 lines"}, "md": md("doc", mdPara("plan text"))},
			map[string]any{"id": "s-2", "title": "Coder", "state": "failed", "error": "tool refused"},
		}}
	p, _ := openChat(t, Phone, chatWith(false, userMsg(0, "go"), msg))
	const run = `document.querySelector('[data-view="chat"] .msg-ai .run')`
	p.WaitJS(run + `?.innerText.includes("review") && ` + run + `.innerText.includes("2m 05s")`)
	if n := Eval[int](p, run+`.querySelectorAll("details.step").length`); n != 2 {
		t.Fatalf("%d steps, want 2", n)
	}
	p.TapJS(run + `.querySelector("details.step summary")`)
	p.WaitJS(run + `.querySelector("details.step[open]")?.innerText.includes("Read a.go") && ` + run + `.querySelector("details.step[open]").innerText.includes("plan text")`)
	if !Eval[bool](p, run+`.innerText.includes("tool refused")`) {
		t.Error("the failed step's error is not shown")
	}
}
