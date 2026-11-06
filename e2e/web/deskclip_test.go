//go:build browser

package web

import (
	"context"
	"runtime"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEBP-1): clipboard history of the desktop editor: what is copied
// or cut in this tab's editors is kept (in memory only, newest first);
// Edit: Paste from Clipboard History… lists it and puts the chosen text
// at the caret, as the user's own edit. Nothing reads the system
// clipboard.
func TestDeskClipboardHistory(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "alpha", "beta")
	deskFocus(p)
	p.Do(deskTextEnd(false))
	p.Do(chromedp.KeyEvent(kb.End, chromedp.KeyModifiers(chromedp.ModifierShift)))
	copyKey(p)
	p.Do(deskTextEnd(true))
	p.Do(chromedp.KeyEvent(kb.Home, chromedp.KeyModifiers(chromedp.ModifierShift)))
	copyKey(p)
	p.Do(chromedp.KeyEvent(kb.End))

	wbKey(p, "P", true, true)
	reply(t, c, expectOp(t, c, "palette.list", `{}`), webproto.PaletteList{})
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("clipboard history")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Edit: Paste from Clipboard History…"`)
	realKeys(p, "\r")
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join("|") === "beta|alpha"`)
	realKeys(p, string(kb.ArrowDown))
	realKeys(p, "\r")
	p.WaitJS(`!document.querySelector(".quick")`)
	v := &vimRig{t: t, p: p, c: c, tick: 3}
	v.settle("alpha", "betaalpha")
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// copyKey presses Mod+C. On a Mac the browser copies on Cmd+C only
// through the system's editing command, which a CDP key event does not
// carry unless it names it ("copy"); without it no copy event fires.
func copyKey(p *Page) {
	p.T.Helper()
	if runtime.GOOS != "darwin" {
		p.Do(chromedp.KeyEvent("c", modKey()))
		return
	}
	down := input.DispatchKeyEventParams{Type: input.DispatchKeyEventTypeKeyDown, Key: "c", Code: "KeyC", WindowsVirtualKeyCode: 67, NativeVirtualKeyCode: 67, Modifiers: int64(kb.ModifierMeta), Commands: []string{"copy"}}
	up := down
	up.Type, up.Commands = input.DispatchKeyEventTypeKeyUp, nil
	p.Do(chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		if _, err := cdp.Call(ctx, t, input.DispatchKeyEvent, down); err != nil {
			return err
		}
		_, err := cdp.Call(ctx, t, input.DispatchKeyEvent, up)
		return err
	}))
}
