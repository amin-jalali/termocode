//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// vimRig drives the desktop editor's Vim mode: keys go in like a
// keyboard's, and every buf.set_text the page sends is answered, until
// the editor shows want.
type vimRig struct {
	t    *testing.T
	p    *Page
	c    *HubConn
	tick int
}

func (v *vimRig) keys(s string, want ...string) {
	v.t.Helper()
	realKeys(v.p, s)
	v.settle(want...)
}

// settle answers the page's edits until the editor shows want.
func (v *vimRig) settle(want ...string) {
	v.t.Helper()
	w := strings.Join(want, "\n")
	deadline := time.Now().Add(5 * time.Second)
	for deskText(v.p) != w {
		if time.Now().After(deadline) {
			v.t.Fatalf("the editor shows %q, want %q", deskText(v.p), w)
		}
		select {
		case m := <-v.c.in:
			if r, ok := m.(*webproto.Req); ok && r.Op == "buf.set_text" {
				v.tick++
				okEdit(v.t, v.c, r, v.tick)
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	// The last edit's answer, if one is still coming.
	select {
	case m := <-v.c.in:
		if r, ok := m.(*webproto.Req); ok && r.Op == "buf.set_text" {
			v.tick++
			okEdit(v.t, v.c, r, v.tick)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

func (v *vimRig) mode(want string) {
	v.t.Helper()
	v.p.WaitJS(`(document.querySelector(".statusbar .sb-vim")?.textContent ?? "") === ` + jsq(want))
}

// toggleVim runs Editor: Toggle Vim Mode from the command list.
func toggleVim(t *testing.T, p *Page, c *HubConn) {
	t.Helper()
	wbKey(p, "P", true, true)
	reply(t, c, expectOp(t, c, "palette.list", `{}`), webproto.PaletteList{})
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("toggle vim")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Editor: Toggle Vim Mode"`)
	p.Key("\r")
	p.WaitJS(`!document.querySelector(".quick")`)
}

// Spec (WEBP-1): Vim mode in the desktop editor, off until Editor: Toggle
// Vim Mode, kept per browser. Normal mode takes the keys (nothing is
// typed); the basic motions (h j k l w b e 0 $ gg G, counts), operators
// (d c y with a motion, dd yy cc), x, p P, o O i a A I, u and Ctrl+R work
// through the laptop's buffer like typing; the status bar shows the mode;
// visual mode deletes what it selects; Escape is back to normal. Turned
// off, the keys type again.
func TestDeskVimBasicMotions(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "alpha beta", "gamma", "delta")
	toggleVim(t, p, c)
	v := &vimRig{t: t, p: p, c: c, tick: 3}
	v.mode("-- NORMAL --")
	deskFocus(p)
	p.Do(deskTextEnd(false))

	v.keys("x", "lpha beta", "gamma", "delta")
	v.keys("wdw", "lpha ", "gamma", "delta")
	v.keys("jdd", "lpha ", "delta")
	v.keys("u", "lpha ", "gamma", "delta")
	v.keys("Go", "lpha ", "gamma", "delta", "")
	v.mode("-- INSERT --")
	v.keys("end", "lpha ", "gamma", "delta", "end")
	realKeys(p, string(kb.Escape))
	v.mode("-- NORMAL --")
	v.keys("ggyyp", "lpha ", "lpha ", "gamma", "delta", "end")
	v.keys("jcwGAMMA", "lpha ", "lpha ", "GAMMA", "delta", "end")
	realKeys(p, string(kb.Escape))
	v.keys("0lx", "lpha ", "lpha ", "GMMA", "delta", "end")
	v.keys("$x", "lpha ", "lpha ", "GMM", "delta", "end")
	v.keys("u", "lpha ", "lpha ", "GMMA", "delta", "end")
	// Ctrl+R: redo, not the browser's reload.
	v.p.Do(chromedp.KeyEvent("r", chromedp.KeyModifiers(chromedp.ModifierCtrl)))
	v.settle("lpha ", "lpha ", "GMM", "delta", "end")
	// Visual mode: v, a motion, d.
	v.keys("j0vlld", "lpha ", "lpha ", "GMM", "ta", "end")
	v.mode("-- NORMAL --")
	v.keys("Ahi", "lpha ", "lpha ", "GMM", "tahi", "end")
	realKeys(p, string(kb.Escape))
	v.keys("3k", "lpha ", "lpha ", "GMM", "tahi", "end")
	v.keys("2dd", "GMM", "tahi", "end")

	// Off again: the keys type.
	toggleVim(t, p, c)
	p.WaitJS(`!document.querySelector(".statusbar .sb-vim")`)
	deskFocus(p)
	p.Do(deskTextEnd(false))
	v.keys("x", "xGMM", "tahi", "end")
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}
