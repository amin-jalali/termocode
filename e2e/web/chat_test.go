//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp/kb"
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
				v + `.querySelector('[role="heading"]')?.textContent === "Plan"`:                     "heading",
				v + `.querySelector(".md em")?.textContent === "soft"`:                                "emphasis",
				v + `.querySelector(".md code.md-code")?.textContent === "go test"`:                   "inline code",
				v + `.querySelector(".md ol")?.getAttribute("start") === "3"`:                         "ordered list start",
				v + `.querySelector(".md blockquote")?.textContent === "quoted"`:                      "quote",
				v + `.querySelectorAll(".md th").length === 2 && ` + v + `.querySelectorAll(".md td").length === 2`: "table",
				v + `.querySelector(".md hr") !== null`:                                               "rule",
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
