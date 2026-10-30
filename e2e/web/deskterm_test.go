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

// The desktop's terminal panel: an integrated terminal you type into.

const deskPane = `document.querySelector('[data-view="term"] .dterm-pane.is-focus')`
const deskInput = `document.querySelector('[data-view="term"] .dterm-pane.is-focus .dterm-input')`

// sendTo reads a term.send, answers it and returns its shell and text.
func sendTo(t *testing.T, c *HubConn) (int, string) {
	t.Helper()
	r := expectReq(t, c, "term.send")
	var a webproto.TermSendArgs
	if err := json.Unmarshal(r.Args, &a); err != nil {
		t.Fatalf("term.send args %s", r.Args)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
	return a.Buf, a.Text
}

// typed reads term.sends to shell 3 until their texts make want.
func typed(t *testing.T, c *HubConn, want string) {
	t.Helper()
	got := ""
	for len(got) < len(want) {
		_, s := sendTo(t, c)
		got += s
	}
	if got != want {
		t.Fatalf("the shell got %q, want %q", got, want)
	}
}

// Every key goes straight to the shell: letters as typed, Enter as \r,
// Ctrl+letter as a control character (Ctrl+C is ^C with nothing
// selected), arrows and Alt+arrow as xterm sends them, Tab and Esc
// themselves. No command line, no key bar; a cursor after the prompt.
func TestDeskTermTypesEveryKey(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ ls", "a.txt b.txt", "$"), termItem(3, "zsh", false, true))
	p.WaitJS(`document.activeElement === ` + deskInput)
	if Eval[bool](p, `document.querySelector('[data-view="term"] .term-line, [data-view="term"] .keybar') !== null`) {
		t.Error("the desktop terminal has a command line or a key bar")
	}
	if got := Eval[string](p, `[...document.querySelectorAll('[data-view="term"] .trow')].findIndex((r) => r.querySelector(".dterm-cursor")) + ":" + `+deskPane+`.querySelector(".dterm-cursor").closest(".trow").textContent`); got != "2:$" {
		t.Errorf("cursor at %q, want after the last prompt", got)
	}

	p.Key("echo hi\r")
	typed(t, c, "echo hi\r")
	for _, k := range []struct {
		name string
		key  chromedp.Action[chromedp.Void]
		want string
	}{
		{"Ctrl+C", chromedp.KeyEvent("c", chromedp.KeyModifiers(chromedp.ModifierCtrl)), "\x03"},
		{"Ctrl+R", chromedp.KeyEvent("r", chromedp.KeyModifiers(chromedp.ModifierCtrl)), "\x12"},
		{"Up", chromedp.KeyEvent(kb.ArrowUp), "\x1b[A"},
		{"Alt+Left", chromedp.KeyEvent(kb.ArrowLeft, chromedp.KeyModifiers(chromedp.ModifierAlt)), "\x1b[1;3D"},
		{"Ctrl+Right", chromedp.KeyEvent(kb.ArrowRight, chromedp.KeyModifiers(chromedp.ModifierCtrl)), "\x1b[1;5C"},
		{"Tab", chromedp.KeyEvent("\t"), "\t"},
		{"Esc", chromedp.KeyEvent(kb.Escape), "\x1b"},
		{"Backspace", chromedp.KeyEvent(kb.Backspace), "\x7f"},
		{"Delete", chromedp.KeyEvent(kb.Delete), "\x1b[3~"},
		{"Home", chromedp.KeyEvent(kb.Home), "\x1b[H"},
	} {
		p.Do(k.key)
		if b, got := sendTo(t, c); b != 3 || got != k.want {
			t.Errorf("%s sent %q to %d, want %q", k.name, got, b, k.want)
		}
	}
	// Tab kept the keys in the terminal.
	if !Eval[bool](p, `document.activeElement === `+deskInput) {
		t.Error("a key took the focus out of the terminal")
	}
	// The workbench's own keys never reach the shell: Ctrl+J hides the panel.
	p.Do(chromedp.KeyEvent("j", modKey()))
	p.WaitJS(`!document.querySelector(".wb-panel")`)
	noSend(t, c, 300*time.Millisecond)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

// Rows the laptop wrapped join and wrap at the panel's width; the panel's
// size never changes the shell's (no resize is ever sent). Selected text
// copies with Ctrl+C instead of sending ^C.
func TestDeskTermWrapsToPanelAndCopies(t *testing.T) {
	p, c := openTerm(t, Desktop)
	long := strings.Repeat("x", 80)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ cat", long, "yyy", "$"), termItem(3, "zsh", false, true))
	if got := termTexts(p); strings.Join(got, "|") != "$ cat|"+long+"yyy|$" {
		t.Errorf("rows %q, want the laptop's wrapped row joined", got)
	}
	Eval[bool](p, `(document.documentElement.style.setProperty("--panel-h", "200px"), document.querySelector(".wb-panel").style.width = "400px", true)`)
	h := Eval[float64](p, `[...document.querySelectorAll('[data-view="term"] .trow')][1].getBoundingClientRect().height`)
	one := Eval[float64](p, `document.querySelector('[data-view="term"] .trow').getBoundingClientRect().height`)
	if h < 2*one {
		t.Errorf("the long row is %.0f px high (one row %.0f): it does not wrap at the panel", h, one)
	}
	if Eval[float64](p, `(() => { const s = document.querySelector('[data-view="term"] .dterm-scroll'); return s.scrollWidth - s.clientWidth; })()`) > 0 {
		t.Error("the terminal scrolls sideways")
	}
	// Select the second row and copy it: no ^C goes to the shell.
	Eval[bool](p, `(() => { const r = document.createRange(); r.selectNodeContents([...document.querySelectorAll('[data-view="term"] .trow-text')][0]); const s = getSelection(); s.removeAllRanges(); s.addRange(r); `+deskPane+`.focus(); return true; })()`)
	Eval[bool](p, `(navigator.clipboard.writeText = (s) => { window.__copied = s; return Promise.resolve(); }, true)`)
	p.Do(chromedp.KeyEvent("c", chromedp.KeyModifiers(chromedp.ModifierCtrl, chromedp.ModifierShift)))
	p.WaitJS(`window.__copied === "$ cat"`)
	noReq(t, c, 300*time.Millisecond)
}

// noSend checks that no keys go to a shell for d.
func noSend(t *testing.T, c *HubConn, d time.Duration) {
	t.Helper()
	end := time.After(d)
	for {
		select {
		case m := <-c.in:
			if r, ok := m.(*webproto.Req); ok && (r.Op == "term.send" || r.Op == "term.interrupt") {
				t.Fatalf("page sent %s; want no keys", show(m))
			}
		case <-end:
			return
		}
	}
}

// pasteInto pastes text into the focused terminal as the browser does.
func pasteInto(p *Page, text string) {
	p.T.Helper()
	Eval[bool](p, fmt.Sprintf(`(() => { const d = new DataTransfer(); d.setData("text/plain", %s); document.activeElement.dispatchEvent(new ClipboardEvent("paste", {clipboardData: d, bubbles: true, cancelable: true})); return true; })()`, jsq(text)))
}

// A one-line paste goes in at once; several lines ask first (Cancel
// first and focused) and their line breaks go as Enter.
func TestDeskTermPasteAsksForLines(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true))
	p.WaitJS(`document.activeElement === ` + deskInput)
	pasteInto(p, "git status")
	typed(t, c, "git status")
	pasteInto(p, "ls\npwd\n")
	const ask = `document.querySelector(".dterm-paste")`
	p.WaitJS(ask + `?.innerText.includes("Paste 2 lines into the shell?")`)
	p.WaitJS(`document.activeElement?.textContent === "Cancel"`) // the safe choice has the focus
	noReq(t, c, 300*time.Millisecond)
	p.Key("\r") // Enter on Cancel: nothing goes
	p.WaitJS(`!` + ask)
	noReq(t, c, 300*time.Millisecond)
	pasteInto(p, "ls\npwd\n")
	p.TapJS(ButtonByText("Paste"))
	typed(t, c, "ls\rpwd\r")
}

// Several shells: a list on the right picks one; + starts a new one;
// Split starts one beside the focused shell and keys go to the one
// clicked; Kill asks the laptop (term.close) after the safe choice.
func TestDeskTermListSplitAndKill(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ one"), termItem(3, "zsh", false, true), termItem(9, "build", true, false))
	p.WaitJS(`document.querySelectorAll('[data-view="term"] .dterm-tab').length === 2`)
	p.TapJS(`document.querySelector('[data-view="term"] .dterm-tab[data-buf="9"] .dterm-tab-open')`)
	waitSubSkipping(t, c, "term:9")
	c.Send(t, Snap("term:9", 1, termSnap(9, 0, 80, "$ nine")))
	p.WaitJS(`document.querySelectorAll('[data-view="term"] .dterm-pane').length === 1 && ` + deskPane + `?.dataset.buf === "9"`)

	p.TapJS(`document.querySelector('.wb-panel .dterm-split')`)
	r := expectReq(t, c, "term.new")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"buf": 14})})
	c.Send(t, Delta("terms", 2, termsSnap(termItem(3, "zsh", false, true), termItem(9, "build", true, false), termItem(14, "zsh", false, false))))
	waitSubSkipping(t, c, "term:14")
	c.Send(t, Snap("term:14", 1, termSnap(14, 0, 80, "$")))
	p.WaitJS(`[...document.querySelectorAll('[data-view="term"] .dterm-pane')].map((e) => e.dataset.buf).join() === "9,14" && ` + deskPane + `?.dataset.buf === "14"`)
	p.WaitJS(`document.activeElement === ` + deskInput)
	p.Key("a")
	if b, s := sendTo(t, c); b != 14 || s != "a" {
		t.Errorf("typed %q into %d, want a into the new shell 14", s, b)
	}
	// A click in the other pane sends the keys there.
	Eval[bool](p, `(() => { const e = document.querySelector('[data-view="term"] .dterm-pane[data-buf="9"] .dterm-scroll'); for (const k of ["mousedown", "mouseup"]) e.dispatchEvent(new MouseEvent(k, {bubbles: true})); return true; })()`)
	p.WaitJS(deskPane + `?.dataset.buf === "9" && document.activeElement === ` + deskInput)
	p.Key("b")
	if b, s := sendTo(t, c); b != 9 || s != "b" {
		t.Errorf("typed %q into %d, want b into shell 9", s, b)
	}

	p.TapJS(`document.querySelector('.wb-panel .dterm-kill')`)
	p.WaitJS(`document.querySelector(".ask")?.innerText.includes("Close terminal 2?")`)
	p.WaitJS(`document.activeElement?.textContent === "Cancel"`) // the safe choice has the focus
	p.Holds(`!document.querySelector(".ask .arm.is-armed")`, 200*time.Millisecond)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(`document.querySelector(".ask .arm")`)
	k := expectReq(t, c, "term.close")
	var a webproto.BufArgs
	if err := json.Unmarshal(k.Args, &a); err != nil || a.Buf != 9 {
		t.Errorf("term.close args %s, want shell 9", k.Args)
	}
}
