package app

import (
	"fmt"
	"strings"
	"unicode/utf16"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/grid"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Terminal colours (WEB-40, web-ui-design §2.5), best effort: nvim keeps
// a terminal's colours in its screen, not in the buffer, so they are read
// off the laptop's own screen (the grid termocode draws), for the rows
// of the terminal's window there. A row is coloured only when the
// screen's text is the buffer row's text, so a scrolled or wrapped window
// never colours the wrong row. Never the shell's bytes: no escape
// sequence reaches the page.

func init() {
	webOps[webproto.OpTermColors] = webOpTermColors
}

// webLuaTermWin says where terminal b is on the screen: the window's
// first screen row and column of text, its size, its top buffer row and
// those rows' text. shown is false when no window shows it.
const webLuaTermWin = webLuaPrelude + `
local b = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or vim.bo[b].buftype ~= 'terminal' then return { err = 'gone' } end
local out = { tick = api.nvim_buf_get_changedtick(b) }
local w = vim.fn.bufwinid(b)
if w == -1 then return out end
local info = vim.fn.getwininfo(w)[1]
if not info then return out end
out.shown = true
out.row = info.winrow - 1 + (info.winbar or 0)
out.col = info.wincol - 1 + info.textoff
out.width = info.width - info.textoff
out.height = info.height
out.top = info.topline - 1
out.lines = api.nvim_buf_get_lines(b, out.top, out.top + out.height, false)
return out`

// webLuaTermWinRes is what webLuaTermWin returns.
type webLuaTermWinRes struct {
	Err    string   `msgpack:"err"`
	Tick   int      `msgpack:"tick"`
	Shown  bool     `msgpack:"shown"`
	Row    int      `msgpack:"row"`
	Col    int      `msgpack:"col"`
	Width  int      `msgpack:"width"`
	Height int      `msgpack:"height"`
	Top    int      `msgpack:"top"`
	Lines  []string `msgpack:"lines"`
}

func webOpTermColors(m *Model, call *webCall) tea.Cmd {
	n := call.args.(*webproto.BufArgs).Buf
	if _, ok := m.webTermTab(n); !ok {
		webFail(call, webErrNoTerm())
		return nil
	}
	if m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil
	}
	var win webLuaTermWinRes
	if err := m.nvim.ExecLuaResult(webLuaTermWin, &win, n); err != nil || win.Err != "" {
		call.fail(webproto.CodeGone, "That terminal is closed.")
		return nil
	}
	call.reply(webTermColors(win, m.editor.RowRuns))
	return nil
}

// webTermColors builds the answer from the window and a reader of the
// screen's rows.
func webTermColors(win webLuaTermWinRes, rowRuns func(r, c0, c1 int) (string, []grid.Run)) webproto.TermColorsRes {
	out := webproto.TermColorsRes{Tick: win.Tick, Shown: win.Shown, Styles: []webproto.TermStyle{}, Spans: [][]int{}}
	if !win.Shown || win.Width <= 0 {
		return out
	}
	index := map[webproto.TermStyle]int{}
	for k, line := range win.Lines {
		if k >= win.Height {
			break
		}
		text, runs := rowRuns(win.Row+k, win.Col, win.Col+win.Width)
		line = strings.TrimRight(line, " ")
		if strings.TrimRight(text, " ") != line {
			continue // the screen shows something else on this row now
		}
		end := len(utf16.Encode([]rune(line)))
		for _, r := range runs {
			to := min(r.To, end)
			if r.From >= to {
				continue
			}
			st := webTermStyle(r)
			i, ok := index[st]
			if !ok {
				if len(out.Styles) >= webproto.MaxTermStyles {
					continue
				}
				i = len(out.Styles)
				index[st] = i
				out.Styles = append(out.Styles, st)
			}
			if len(out.Spans) >= webproto.MaxTermSpans {
				return out
			}
			out.Spans = append(out.Spans, []int{win.Top + k, r.From, to, i})
		}
	}
	return out
}

func webTermStyle(r grid.Run) webproto.TermStyle {
	hex := func(c int) string {
		if c < 0 {
			return ""
		}
		return fmt.Sprintf("#%06x", c&0xffffff)
	}
	return webproto.TermStyle{Fg: hex(r.Fg), Bg: hex(r.Bg), B: r.Bold, I: r.Italic, U: r.Underline, S: r.Strike}
}
