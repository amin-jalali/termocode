//go:build browser

package web

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The phone's Term page as proposed (page-designs 01-term): the shells
// are pills with ›_, the title, the number and × on the shown one, + in
// the strip, and ⋯ at its end (the ⓘ moved into it); suggestion chips
// that follow the last output (a FAIL brings Fix it, Explain this and
// Run <the test>) and an "Ask AI to write a command" field above the
// keys; a long press on a line offers Ask AI about this, Add to chat,
// Copy and Run again, and the lines go to the chat as a reference.

var failRows = []string{"~/shop-api main $ go test ./...", "--- FAIL: TestDiscount (0.00s)", "    cart_test.go:24: Total = 10, want 9", "FAIL", "~/shop-api main $ "}

// chatSend reads a chat.send and answers it.
func chatSend(t *testing.T, c *HubConn) webproto.ChatSendArgs {
	t.Helper()
	r := expectReq(t, c, "chat.send")
	var a webproto.ChatSendArgs
	if err := json.Unmarshal(r.Args, &a); err != nil {
		t.Fatal(err)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
	return a
}

// menuOn opens the line menu with a long press on the terminal's row k.
func menuOn(p *Page, k int) {
	p.T.Helper()
	p.press(termView+`.querySelectorAll(".xterm-rows > div")[`+fmt.Sprint(k)+`]`, rowMenu)
}

func menuItems(p *Page) []string {
	return Eval[[]string](p, `[...document.querySelectorAll(".row-menu .row-menu-list .row-menu-item")].map((b) => b.textContent.trim())`)
}

func pickItem(p *Page, label string) {
	p.T.Helper()
	p.TapJS(`[...document.querySelectorAll(".row-menu .row-menu-item")].find((b) => b.textContent.trim() === ` + jsq(label) + `)`)
}

func TestPhoneTermPageStripMenuAndPills(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "shop-api", false, true), termItem(9, "go run", true, false))
	pills := Eval[[]string](p, `[...`+termView+`.querySelectorAll(".pterm-strip .term-pill")].map((e) => [...e.querySelectorAll(".pterm-ic, .term-chip-title, .term-chip-n, .pterm-close")].map((c) => c.textContent.trim()).join(" "))`)
	if strings.Join(pills, "|") != "›_ shop-api 1 ×|›_ go run 2" {
		t.Errorf("pills %q, want ›_ title number, × on the shown one", pills)
	}
	if !Eval[bool](p, termView+`.querySelector(".pterm-strip .term-pills .pterm-new")?.textContent.trim() === "+"`) {
		t.Error("+ is not in the strip of pills")
	}
	if Eval[bool](p, termView+`.querySelector(".pterm-strip .term-info-btn") !== null`) {
		t.Error("ⓘ still stands alone; it moves into ⋯")
	}
	p.TapJS(termView + `.querySelector(".pterm-strip .pterm-more")`)
	p.WaitJS(`document.querySelector(".row-menu")`)
	if got := strings.Join(menuItems(p), "|"); got != "About this terminal|Show on the laptop|Close this shell…" {
		t.Errorf("⋯ menu %q", got)
	}
	pickItem(p, "About this terminal")
	p.WaitJS(`document.querySelector(".term-about")`)
	p.TapJS(`[...document.querySelectorAll(".sheet .btn")].find((b) => b.textContent === "Close")`)
	p.WaitJS(`!document.querySelector(".term-about")`)
	// × asks the laptop to close the shell (it confirms there).
	p.TapJS(termView + `.querySelector(".term-pill.is-on .pterm-close")`)
	r := expectReq(t, c, "term.close")
	if !strings.Contains(string(r.Args), `"buf":3`) {
		t.Errorf("term.close args %s", r.Args)
	}
}

func TestPhoneTermPageChipsFollowTheOutput(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ ls", "a.go", "$ "), termItem(3, "shop-api", false, true))
	p.WaitJS(termView + `.querySelector(".pterm-ask input")`)
	if Eval[bool](p, termView+`.querySelector(".pterm-chips") !== null`) {
		t.Error("chips show for calm output; the row hides when it has none")
	}
	c.Send(t, Snap("term:3", 2, termSnap(3, 0, 80, failRows...)))
	p.WaitJS(termView + `.querySelector(".pterm-chips")`)
	got := Eval[[]string](p, `[...`+termView+`.querySelectorAll(".pterm-chips .chip")].map((e) => e.textContent.trim())`)
	if strings.Join(got, "|") != "✦ Fix it|Explain this|Run TestDiscount" {
		t.Errorf("chips %q", got)
	}
	if ph := Eval[string](p, termView+`.querySelector(".pterm-ask input").placeholder`); ph != "Ask AI to write a command" {
		t.Errorf("field placeholder %q", ph)
	}
	// Run <test> types the command into the shell, no AI.
	p.TapJS(`[...` + termView + `.querySelectorAll(".pterm-chips .chip")].find((e) => e.textContent.includes("Run TestDiscount"))`)
	if got := expectSend(t, c); got != "go test -run '^TestDiscount$' ./...\r" {
		t.Errorf("Run sent %q", got)
	}
	// Fix it asks the AI with the shell's last lines as a reference.
	p.TapJS(`[...` + termView + `.querySelectorAll(".pterm-chips .chip")].find((e) => e.textContent.includes("Fix it"))`)
	a := chatSend(t, c)
	if !strings.Contains(a.Text, "Fix") || len(a.Attach) != 1 || a.Attach[0].Kind != "term" || a.Attach[0].ID != "3" || a.Attach[0].From != 1 || a.Attach[0].To != 5 {
		t.Errorf("Fix it sent %+v", a)
	}
	// The field: what the user wants, the AI types the command in.
	p.TapJS(termView + `.querySelector(".pterm-ask input")`)
	p.Key("list big files\r")
	a = chatSend(t, c)
	if !strings.HasSuffix(a.Text, "list big files") || !strings.Contains(a.Text, "without running it") || len(a.Attach) != 1 || a.Attach[0].Kind != "term" {
		t.Errorf("the field sent %+v", a)
	}
	p.WaitJS(termView + `.querySelector(".pterm-ask input").value === ""`)
}

func TestPhoneTermPageLongPressLines(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 80, failRows...), termItem(3, "shop-api", false, true))
	c.Send(t, Snap("chat", 1, chatWith(false, userMsg(0, "hi"))))
	menuOn(p, 1)
	if got := Eval[string](p, `document.querySelector(".row-menu-title").textContent`); got != "1 line · terminal shop-api" {
		t.Errorf("menu title %q", got)
	}
	if got := strings.Join(menuItems(p), "|"); !strings.HasPrefix(got, "✦ Ask AI about this|Add to chat ↩|Copy 1 line|Copy all rows") {
		t.Errorf("line menu %q", got)
	}
	pickItem(p, "Add to chat ↩")
	p.GoTab("chat")
	p.WaitJS(`[...document.querySelectorAll(".ref-chip-label")].some((e) => e.textContent.includes("1 line · terminal shop-api"))`)
	// A prompt line can run again.
	p.GoTab("term")
	subSkipping(t, c, "term:3")
	c.Send(t, Snap("term:3", 2, termSnap(3, 0, 80, failRows...)))
	p.WaitJS(termView + `.querySelector(".pterm-x .xterm-rows")?.textContent.includes("FAIL")`)
	menuOn(p, 0)
	if got := strings.Join(menuItems(p), "|"); !strings.HasPrefix(got, "✦ Ask AI about this|Add to chat ↩|Copy 1 line|Run again|Copy all rows") {
		t.Errorf("prompt line menu %q", got)
	}
	pickItem(p, "Run again")
	if got := expectSend(t, c); got != "go test ./...\r" {
		t.Errorf("Run again sent %q", got)
	}
	// Ask AI about this: the chat opens with the lines as a chip and a question.
	menuOn(p, 1)
	pickItem(p, "✦ Ask AI about this")
	p.WaitJS(`location.hash.startsWith("#/chat")`)
	p.WaitJS(`document.querySelector('[data-view="chat"] textarea')?.value === "Explain these terminal lines."`)
}
