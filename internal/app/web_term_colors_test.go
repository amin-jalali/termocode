package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/grid"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEB-40): colours come only for rows whose screen text is the
// buffer row's text; spans are in buffer rows, cut at the row's end, and
// equal looks share one style.
func TestTermColorsOnlyMatchingRows(t *testing.T) {
	screen := map[int]string{5: "$ ls   ", 6: "a.go  b.go", 7: "stale row"}
	runs := map[int][]grid.Run{
		5: {{From: 0, To: 1, Fg: 0x00ff00, Bg: -1, Bold: true}},
		6: {{From: 0, To: 4, Fg: 0x5555ff, Bg: -1}, {From: 6, To: 30, Fg: 0x5555ff, Bg: -1}},
		7: {{From: 0, To: 5, Fg: 0xff0000, Bg: -1}},
	}
	read := func(r, c0, c1 int) (string, []grid.Run) {
		if c0 != 2 || c1 != 22 {
			t.Errorf("read cols [%d,%d), want the window's [2,22)", c0, c1)
		}
		return screen[r], runs[r]
	}
	win := webLuaTermWinRes{Tick: 9, Shown: true, Row: 5, Col: 2, Width: 20, Height: 3, Top: 40,
		Lines: []string{"$ ls", "a.go  b.go", "new row"}}
	got := webTermColors(win, read)
	if got.Tick != 9 || !got.Shown || len(got.Styles) != 2 {
		t.Fatalf("answer %+v", got)
	}
	if got.Styles[0] != (webproto.TermStyle{Fg: "#00ff00", B: true}) || got.Styles[1] != (webproto.TermStyle{Fg: "#5555ff"}) {
		t.Errorf("styles %+v", got.Styles)
	}
	var b strings.Builder
	for _, s := range got.Spans {
		b.WriteString(" " + itoa(s[0]) + ":" + itoa(s[1]) + "-" + itoa(s[2]) + ":" + itoa(s[3]))
	}
	if want := " 40:0-1:0 41:0-4:1 41:6-10:1"; b.String() != want {
		t.Errorf("spans%s, want%s", b.String(), want)
	}
	if got := webTermColors(webLuaTermWinRes{Tick: 3}, read); got.Shown || len(got.Spans) != 0 {
		t.Errorf("not on show: %+v", got)
	}
}

// Spec (WEB-40): the window chunk finds a terminal on the laptop's
// screen (its size, top row and rows) and says shown false for one that
// is not on show; a closed buffer is gone.
func TestTermColorsWindow(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	buf := webTestShell(t, r, "sh")
	f := r.call(webproto.OpTermColors, &webproto.BufArgs{Buf: buf})
	var res webproto.TermColorsRes
	webResData(t, f, &res)
	if res.Shown || res.Tick <= 0 {
		t.Errorf("a hidden terminal: %+v", res)
	}
	r.lua(`local b = ... vim.cmd('botright split') vim.api.nvim_win_set_buf(0, b)`, nil, buf)
	var win webLuaTermWinRes
	r.lua(webLuaTermWin, &win, buf)
	if !win.Shown || win.Height <= 0 || win.Width <= 0 || win.Row <= 0 || len(win.Lines) == 0 {
		t.Errorf("a terminal on show: %+v", win)
	}
	if f := r.call(webproto.OpTermColors, &webproto.BufArgs{Buf: 9999}); webErrCode(f) != "not_found" {
		t.Errorf("no such terminal: %v", f)
	}
}
