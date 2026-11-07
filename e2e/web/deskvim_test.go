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

// toggleVim runs Editor: Toggle vi Keys from the command list (the
// laptop's list is asked for the first time only).
func toggleVim(t *testing.T, p *Page, c *HubConn, first bool) {
	t.Helper()
	runCmdKeys(t, p, c, first, "toggle vi keys", "Editor: Toggle vi Keys")
}

// Spec (WEBP-1): Vim mode in the desktop editor, off until Editor: Toggle
// vi Keys, kept per browser. Normal mode takes the keys (nothing is
// typed); the basic motions (h j k l w b e 0 $ gg G, counts), operators
// (d c y with a motion, dd yy cc), x, p P, o O i a A I, u and Ctrl+R work
// through the laptop's buffer like typing; the status bar shows the mode;
// visual mode deletes what it selects; Escape is back to normal. Turned
// off, the keys type again.
func TestDeskVimBasicMotions(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "alpha beta", "gamma", "delta")
	toggleVim(t, p, c, true)
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
	toggleVim(t, p, c, false)
	p.WaitJS(`!document.querySelector(".statusbar .sb-vim")`)
	deskFocus(p)
	p.Do(deskTextEnd(false))
	v.keys("x", "xGMM", "tahi", "end")
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// Spec (WEBP-1): the . repeat, counts on an insert, macros and
// registers. . does the last change again (an insert with its typed
// text too); 2ia<Esc> types the text twice; qa … q records the keys into
// register a (the status bar says "recording @a"), @a plays them, @@
// plays the last one again; "ayy yanks into a, "0 keeps the last yank
// after a delete, "_ deletes without touching any register.
func TestDeskVimRepeatMacrosRegisters(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "one two three", "four", "five")
	toggleVim(t, p, c, true)
	v := &vimRig{t: t, p: p, c: c, tick: 3}
	v.mode("-- NORMAL --")
	deskFocus(p)
	p.Do(deskTextEnd(false))

	v.keys("dw", "two three", "four", "five")
	v.keys(".", "three", "four", "five")
	v.keys("A!", "three!", "four", "five")
	realKeys(p, string(kb.Escape))
	v.mode("-- NORMAL --")
	v.keys("j.", "three!", "four!", "five")
	v.keys("j02ia", "three!", "four!", "afive")
	realKeys(p, string(kb.Escape))
	v.settle("three!", "four!", "aafive")
	v.mode("-- NORMAL --")

	// A macro: recorded, played, played again.
	v.keys("ggqa", "three!", "four!", "aafive")
	p.WaitJS(`(document.querySelector(".statusbar .sb-vim-rec")?.textContent ?? "") === "recording @a"`)
	v.keys("0i-", "-three!", "four!", "aafive")
	realKeys(p, string(kb.Escape))
	v.keys("jq", "-three!", "four!", "aafive")
	p.WaitJS(`!document.querySelector(".statusbar .sb-vim-rec")`)
	v.keys("@a", "-three!", "-four!", "aafive")
	v.keys("@@", "-three!", "-four!", "-aafive")

	// Registers: the yank register, a named one, the black hole.
	v.keys("ggyyjdd", "-three!", "-aafive")
	v.keys(`"0p`, "-three!", "-aafive", "-three!")
	v.keys(`"_dd`, "-three!", "-aafive")
	v.keys("p", "-three!", "-aafive", "-four!")
	v.keys(`gg"byyG"bP`, "-three!", "-aafive", "-three!", "-four!")
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// Spec (WEBP-1): search, ex commands, text objects and marks. /foo<CR>
// goes to the next foo (the status bar shows the search as it is
// typed), n to the one after (round the end); :3 goes to line 3; di(
// deletes inside the brackets; :%s/o/0/g changes every o; gUiw makes
// the word upper case; ma marks a line and d'a deletes up to it; an
// unknown command and a search with no match say so in the status bar;
// :w saves through the laptop.
func TestDeskVimSearchExMarks(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "foo bar", "baz foo", "qux (a, b) end", "last")
	toggleVim(t, p, c, true)
	v := &vimRig{t: t, p: p, c: c, tick: 3}
	v.mode("-- NORMAL --")
	deskFocus(p)
	p.Do(deskTextEnd(false))
	keysShown := `(document.querySelector(".statusbar .sb-vim-keys")?.textContent ?? "")`
	msg := `(document.querySelector(".statusbar .sb-vim-msg")?.textContent ?? "")`

	v.keys("/fo", "foo bar", "baz foo", "qux (a, b) end", "last")
	p.WaitJS(keysShown + ` === "/fo"`)
	v.keys("o\rdw", "foo bar", "baz ", "qux (a, b) end", "last")
	v.keys("nx", "oo bar", "baz ", "qux (a, b) end", "last")
	v.keys(":3\rf(di(", "oo bar", "baz ", "qux () end", "last")
	v.keys(":%s/o/0/g\r", "00 bar", "baz ", "qux () end", "last")
	v.keys("GgUiw", "00 bar", "baz ", "qux () end", "LAST")
	v.keys("3Gmaggd'a", "LAST")

	v.keys(":foo\r", "LAST")
	p.WaitJS(msg + ` === "Not an editor command: foo"`)
	v.keys("/zzz\r", "LAST")
	p.WaitJS(msg + ` === "Pattern not found: zzz"`)
	// A key clears the message.
	v.keys("l", "LAST")
	p.WaitJS(`!document.querySelector(".statusbar .sb-vim-msg")`)

	realKeys(p, ":w\r")
	r := expectReq(t, c, "buf.save")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"tick": v.tick + 1, "dirty": false})})
	p.WaitJS(`(` + toastsJS + `).includes("Saved. The laptop's on-save hooks ran.")`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// Spec (WEBP-1): Vim mode follows the user's own mappings. Turned on,
// the page asks the laptop once for its editor's mappings
// (editor.vim_maps) and the keys do what they say: <Space>d deletes the
// line, jk in insert mode is Escape (j alone, then another key, types
// both), Q plays the macro in q. :nnoremap adds one in the browser.
func TestDeskVimUserMappings(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "abc", "def", "ghi")
	toggleVim(t, p, c, true)
	for {
		r := nextReq(t, c)
		if r.Op == webproto.OpEditorVimMaps {
			reply(t, c, r, webproto.VimMapList{Items: []webproto.VimMap{
				{Mode: "i", LHS: "jk", RHS: "<Esc>", Noremap: true},
				{Mode: "n", LHS: "<Space>d", RHS: "dd", Noremap: true},
				{Mode: "n", LHS: "Q", RHS: "@q"},
			}})
			break
		}
	}
	v := &vimRig{t: t, p: p, c: c, tick: 3}
	v.mode("-- NORMAL --")
	deskFocus(p)
	p.Do(deskTextEnd(false))

	v.keys(" d", "def", "ghi")
	v.keys("ix", "xdef", "ghi")
	v.mode("-- INSERT --")
	v.keys("jk", "xdef", "ghi")
	v.mode("-- NORMAL --")
	v.keys("ajy", "xjydef", "ghi")
	realKeys(p, string(kb.Escape))
	v.mode("-- NORMAL --")

	// Q is @q: the macro recorded in q.
	v.keys("qqI-", "-xjydef", "ghi")
	realKeys(p, string(kb.Escape))
	v.keys("jq", "-xjydef", "ghi")
	v.keys("Q", "-xjydef", "-ghi")

	// A mapping made here.
	v.keys(":nnoremap ,a A!<Esc>\rgg,a", "-xjydef!", "-ghi")
	v.mode("-- NORMAL --")
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// Spec (WEBP-1): visual block mode in the desktop editor. Ctrl+V starts
// it (the status bar says -- VISUAL BLOCK --; the browser never pastes),
// the motions grow a box of columns, d deletes the box, y yanks it and p
// puts it back as a box (each line its own piece), I and A type on the
// first line and Escape puts the same text on every line of the box ($
// makes A append at each line's own end), c changes the box, r replaces
// every char in it and Ctrl+V again (or Escape) is back to normal.
func TestDeskVimVisualBlock(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "abcd", "efgh", "ijkl")
	toggleVim(t, p, c, true)
	v := &vimRig{t: t, p: p, c: c, tick: 3}
	v.mode("-- NORMAL --")
	deskFocus(p)
	p.Do(deskTextEnd(false))
	ctrlV := func() { v.p.Do(chromedp.KeyEvent("v", chromedp.KeyModifiers(chromedp.ModifierCtrl))) }

	// Ctrl+V twice: in and out again, nothing pasted.
	ctrlV()
	v.mode("-- VISUAL BLOCK --")
	ctrlV()
	v.mode("-- NORMAL --")
	v.settle("abcd", "efgh", "ijkl")

	// d deletes the box; u brings it back.
	ctrlV()
	v.mode("-- VISUAL BLOCK --")
	v.keys("jld", "cd", "gh", "ijkl")
	v.mode("-- NORMAL --")
	v.keys("u", "abcd", "efgh", "ijkl")

	// I types on every line of the box at Escape.
	v.keys("gg0")
	ctrlV()
	v.keys("jjIX", "Xabcd", "efgh", "ijkl")
	v.mode("-- INSERT --")
	realKeys(p, string(kb.Escape))
	v.settle("Xabcd", "Xefgh", "Xijkl")
	v.mode("-- NORMAL --")

	// $A appends at each line's end.
	v.keys("gg0")
	ctrlV()
	v.keys("jj$A!", "Xabcd!", "Xefgh", "Xijkl")
	realKeys(p, string(kb.Escape))
	v.settle("Xabcd!", "Xefgh!", "Xijkl!")

	// y yanks a box, p puts it after the caret on each line.
	v.keys("gg0")
	ctrlV()
	v.keys("jly", "Xabcd!", "Xefgh!", "Xijkl!")
	v.mode("-- NORMAL --")
	v.keys("$p", "Xabcd!Xa", "Xefgh!Xe", "Xijkl!")

	// c changes the box on every line.
	v.keys("gg0")
	ctrlV()
	v.keys("jjcZ", "Zabcd!Xa", "Xefgh!Xe", "Xijkl!")
	realKeys(p, string(kb.Escape))
	v.settle("Zabcd!Xa", "Zefgh!Xe", "Zijkl!")

	// r replaces every char of the box; Escape leaves block mode.
	v.keys("gg0l")
	ctrlV()
	v.keys("jlrQ", "ZQQcd!Xa", "ZQQgh!Xe", "Zijkl!")
	ctrlV()
	v.mode("-- VISUAL BLOCK --")
	realKeys(p, string(kb.Escape))
	v.mode("-- NORMAL --")
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}
