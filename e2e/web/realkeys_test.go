//go:build browser

package web

import (
	"context"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// realKeys types s the way a keyboard does: one keydown that carries the
// text, then a keyup. A page that takes the key on keydown (a Vim normal
// mode, a chord) then types nothing, as in a real browser; chromedp's own
// KeyEvent sends the text in a separate char event, which types anyway.
func realKeys(p *Page, s string) {
	p.T.Helper()
	for _, r := range s {
		down := input.DispatchKeyEventParams{Type: input.DispatchKeyEventTypeKeyDown, Key: string(r), Text: string(r), UnmodifiedText: string(r)}
		if v, ok := kb.Keys[r]; ok {
			down.Key, down.Code, down.WindowsVirtualKeyCode, down.NativeVirtualKeyCode = v.Key, v.Code, v.Windows, v.Native
			if v.Shift {
				down.Modifiers |= int64(kb.ModifierShift)
			}
			if !v.Print {
				down.Text, down.UnmodifiedText = "", ""
			}
		}
		up := down
		up.Type, up.Text, up.UnmodifiedText = input.DispatchKeyEventTypeKeyUp, "", ""
		p.Do(chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			if _, err := cdp.Call(ctx, t, input.DispatchKeyEvent, down); err != nil {
				return err
			}
			_, err := cdp.Call(ctx, t, input.DispatchKeyEvent, up)
			return err
		}))
	}
}
