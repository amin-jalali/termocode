//go:build browser

package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The in-page editor (WEB-39): CodeMirror in a shadow root, loaded on
// the first Edit; every pause sends one buf.set_text with the tick the
// page knows; the laptop's changes come in live; when both sides changed
// nothing is merged: the user's text stays and the page asks.

const cmHost = `document.querySelector(".editor-host")`
const cmContent = cmHost + `?.shadowRoot?.querySelector(".cm-content")`

// edText is the editor's text, line by line.
func edText(p *Page) string {
	return Eval[string](p, `[...`+cmHost+`.shadowRoot.querySelectorAll(".cm-line")].map((l) => l.textContent).join("\n")`)
}

func openEditor(t *testing.T, p *Page) {
	t.Helper()
	p.TapJS(ButtonByText("Edit"))
	p.WaitJS(cmContent)
	if !slicesHas(p.Requests(), "/assets/editor.js") {
		t.Errorf("the editor file did not load: %v", p.Requests())
	}
}

func slicesHas(list []string, part string) bool {
	for _, s := range list {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

// typeAtStart puts the caret at the start of the text and types s.
func typeAtStart(p *Page, s string) {
	p.T.Helper()
	Eval[bool](p, `(`+cmContent+`.focus(), true)`)
	p.Do(chromedp.KeyEvent(kb.Home, chromedp.KeyModifiers(chromedp.ModifierCtrl)))
	p.Do(chromedp.KeyEvent(s))
}

func okEdit(t *testing.T, c *HubConn, r *webproto.Req, tick int) {
	t.Helper()
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"tick": tick, "dirty": true})})
}

func TestLiveEditorTwoWaySync(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openFiles(t, vp)
			openBuf(t, p, c, 2, "notes.txt", "alpha", "beta é")
			if slicesHas(p.Requests(), "/assets/editor.js") {
				t.Fatal("the editor file loaded before Edit")
			}
			openEditor(t, p)
			if got := edText(p); got != "alpha\nbeta é" {
				t.Fatalf("editor text %q", got)
			}
			// One pause, one set_text: the change, at the tick the page knows.
			typeAtStart(p, "X")
			r, a := expectSetText(t, c)
			if a.Buf != 2 || a.Tick != 3 || a.Start != (webproto.Pos{}) || a.End != (webproto.Pos{}) || strings.Join(a.Lines, "|") != "X" {
				t.Fatalf("set_text %+v", a)
			}
			okEdit(t, c, r, 4)
			// Its echo changes nothing in the editor.
			c.Send(t, Delta("buf:2", 2, map[string]any{"kind": "lines", "first": 0, "last": 1, "lines": []string{"Xalpha"}, "tick": 4}))
			p.WaitJS(`(` + filesView + `.querySelector(".view-actions .chip")?.textContent ?? "") === "In sync with the laptop"`)
			if got := edText(p); got != "Xalpha\nbeta é" {
				t.Fatalf("after the echo the editor has %q", got)
			}
			// The laptop types: it shows at once and its row flashes.
			c.Send(t, Delta("buf:2", 3, map[string]any{"kind": "lines", "first": 1, "last": 2, "lines": []string{"beta é laptop"}, "tick": 5}))
			p.WaitJS(cmHost + `.shadowRoot.querySelector(".cm-line.cm-laptop")?.textContent === "beta é laptop"`)
			// Typing after a multi-byte row counts bytes for nvim.
			Eval[bool](p, `(`+cmContent+`.focus(), true)`)
			p.Do(chromedp.KeyEvent(kb.End, chromedp.KeyModifiers(chromedp.ModifierCtrl)))
			p.Do(chromedp.KeyEvent("!"))
			r, a = expectSetText(t, c)
			if a.Tick != 5 || a.Start != (webproto.Pos{Row: 1, Col: 14}) || a.End != a.Start || strings.Join(a.Lines, "|") != "!" {
				t.Fatalf("set_text at the end %+v", a)
			}
			okEdit(t, c, r, 6)
			// Undo takes back the user's own change, never the laptop's.
			p.TapJS(`[...` + filesView + `.querySelectorAll(".view-actions .btn")].find((b) => b.textContent === "Undo")`)
			r, a = expectSetText(t, c)
			if a.Tick != 6 || a.Start != (webproto.Pos{Row: 1, Col: 14}) || a.End != (webproto.Pos{Row: 1, Col: 15}) || strings.Join(a.Lines, "|") != "" {
				t.Fatalf("undo sent %+v", a)
			}
			okEdit(t, c, r, 7)
			p.WaitJS(`(` + filesView + `.querySelector(".view-actions .chip")?.textContent ?? "") === "In sync with the laptop"`)
			// Save sends what is unsent first, then buf.save.
			p.TapJS(`[...` + filesView + `.querySelectorAll(".view-actions .btn")].find((b) => b.textContent === "Save")`)
			r = expectReq(t, c, "buf.save")
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"tick": 8, "dirty": false})})
			p.WaitJS(`(` + toastsJS + `).includes("Saved. The laptop's on-save hooks ran.")`)
			// Done goes back to the read view.
			p.TapJS(ButtonByText("Done"))
			p.WaitJS(`!` + cmHost + ` && ` + filesView + `.querySelector(".crow")`)
			if probs := p.Problems(); len(probs) > 0 {
				t.Errorf("problems (CSP, console): %v", probs)
			}
		})
	}
}

// The hub refuses a stale tick (the laptop typed in between): the user's
// text stays, nothing more is sent, the laptop's change can be read, and
// Send mine sends the user's text on the laptop's newest tick.
func TestLiveEditorConflictFromHub(t *testing.T) {
	p, c := openFiles(t, Phone)
	openBuf(t, p, c, 2, "notes.txt", "alpha", "beta")
	openEditor(t, p)
	typeAtStart(p, "Y")
	r, _ := expectSetText(t, c)
	c.Send(t, Delta("buf:2", 2, map[string]any{"kind": "lines", "first": 1, "last": 2, "lines": []string{"beta from laptop"}, "tick": 4}))
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeConflict, Msg: "changed", Data: raw(map[string]any{"tick": 4})})
	p.WaitJS(`document.querySelector(".live-conflict")?.innerText.includes("Your text stays here")`)
	if got := edText(p); got != "Yalpha\nbeta" {
		t.Fatalf("the user's text is %q after the conflict", got)
	}
	typeAtStart(p, "Z") // still typing: kept, not sent
	noReq(t, c, 400*time.Millisecond)
	p.TapJS(`document.querySelector(".live-diff-btn")`)
	p.WaitJS(`document.querySelector(".live-conflict .drow-del")?.innerText.includes("beta") && document.querySelector(".live-conflict .drow-add")?.innerText.includes("beta from laptop")`)
	p.TapJS(ButtonByText("Send mine"))
	_, a := expectSetText(t, c)
	if a.Tick != 4 || a.Start != (webproto.Pos{Row: 0, Col: 0}) || a.End != (webproto.Pos{Row: 1, Col: 16}) || strings.Join(a.Lines, "|") != "ZYalpha|beta" {
		t.Fatalf("send mine %+v", a)
	}
}

// Both sides changed while a send was on its way: the user's newer text
// stays; Use the laptop's text shows the laptop's, and Undo brings the
// user's back (and sends it).
func TestLiveEditorConflictKeepsUnsentText(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openBuf(t, p, c, 2, "notes.txt", "alpha", "beta")
	openEditor(t, p)
	typeAtStart(p, "Y")
	r, _ := expectSetText(t, c)
	typeAtStart(p, "Z") // unsent: the first send is still on its way
	c.Send(t, Delta("buf:2", 2, map[string]any{"kind": "lines", "first": 0, "last": 1, "lines": []string{"Yalpha"}, "tick": 4}))
	c.Send(t, Delta("buf:2", 3, map[string]any{"kind": "lines", "first": 1, "last": 2, "lines": []string{"BETA"}, "tick": 5}))
	okEdit(t, c, r, 4)
	p.WaitJS(`document.querySelector(".live-conflict")`)
	if got := edText(p); got != "ZYalpha\nbeta" {
		t.Fatalf("the user's text is %q", got)
	}
	noReq(t, c, 400*time.Millisecond)
	p.TapJS(ButtonByText("Use the laptop's text"))
	p.WaitJS(`!document.querySelector(".live-conflict")`)
	if got := edText(p); got != "Yalpha\nBETA" {
		t.Fatalf("after Use the laptop's text: %q", got)
	}
	noReq(t, c, 300*time.Millisecond) // the laptop's text is not sent back
	p.TapJS(ButtonByText("Undo"))
	_, a := expectSetText(t, c)
	if a.Tick != 5 || strings.Join(a.Lines, "|") != "ZYalpha|beta" {
		t.Fatalf("undo after taking the laptop's text sent %+v", a)
	}
}

// The editor file cannot load: the simple edit sheet opens instead.
func TestLiveEditorFallsBackToTheSheet(t *testing.T) {
	p, c := openFiles(t, Phone)
	openBuf(t, p, c, 2, "notes.txt", "alpha")
	block := chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		_, err := cdp.Call(ctx, tg, network.SetBlockedURLs, network.SetBlockedURLsParams{URLPatterns: []*network.BlockPattern{{URLPattern: "*://*:*/assets/editor.js", Block: true}}})
		return err
	})
	p.Do(block)
	p.TapJS(ButtonByText("Edit"))
	p.WaitJS(`(` + toastsJS + `).includes("The editor did not open. The simple edit sheet works instead.") && ` + editSheet)
}

// On a touch screen the editor's text is at least 16 px, so focusing it
// does not zoom the page; with a mouse it keeps the code size.
func TestLiveEditorTouchFontSize(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openFiles(t, vp)
			openBuf(t, p, c, 2, "notes.txt", "alpha")
			openEditor(t, p)
			px := Eval[float64](p, `parseFloat(getComputedStyle(`+cmContent+`).fontSize)`)
			if vp.Mobile && px < 16 {
				t.Errorf("editor text is %v px on a phone, want at least 16", px)
			}
			if !vp.Mobile && px >= 16 {
				t.Errorf("editor text is %v px with a mouse, want the code size", px)
			}
		})
	}
}
