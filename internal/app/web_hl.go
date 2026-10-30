package app

import (
	"regexp"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Syntax colours of the file view (WEB-38, web-ui-design §2.4): nvim's
// own tree-sitter captures for the rows the page shows. One fixed Lua
// chunk parses the asked range (injections too: code in Markdown) and
// walks each tree's highlights query; captures that span rows are cut
// per row, and columns go out in UTF-16 units of the row, what the
// page's strings count. The page never parses code.

func init() {
	webOps[webproto.OpBufHl] = webOpBufHl
}

// webHlMaxSpans caps the spans of one answer (a test lowers it).
var webHlMaxSpans = webproto.MaxHlSpans

// webLuaHl answers {tick, first, last, groups, spans, none, cut} for rows
// [first, last) of buffer b. Groups that never colour (spell, conceal and
// the query's private "_" captures) are left out.
const webLuaHl = webLuaPrelude + `
local b, first, last, maxspans = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
local n = api.nvim_buf_line_count(b)
if last > n then last = n end
if first > last then first = last end
local out = { tick = api.nvim_buf_get_changedtick(b), first = first, last = last }
local ok, parser = pcall(vim.treesitter.get_parser, b, nil, { error = false })
if not ok or not parser then out.none = true return out end
if first == last then return out end
pcall(function() parser:parse({ first, last }) end)
local lines = api.nvim_buf_get_lines(b, first, last, false)
local function u16(s, c)
  local k = 0
  for i = 1, math.min(c, #s) do
    local x = s:byte(i)
    if x < 0x80 or x >= 0xC0 then k = k + ((x >= 0xF0) and 2 or 1) end
  end
  return k
end
local gi, groups, spans = {}, {}, {}
local skip = { spell = true, nospell = true, conceal = true, none = true }
parser:for_each_tree(function(tree, lt)
  if out.cut then return end
  local q = vim.treesitter.query.get(lt:lang(), 'highlights')
  if not q then return end
  for id, node in q:iter_captures(tree:root(), b, first, last) do
    local name = q.captures[id]
    if name and not skip[name] and name:sub(1, 1) ~= '_' then
      local r0, c0, r1, c1 = node:range()
      for r = math.max(r0, first), math.min(r1, last - 1) do
        local line = lines[r - first + 1]
        local a = (r == r0) and c0 or 0
        local z = (r == r1) and c1 or #line
        if z > #line then z = #line end
        if z > a then
          if #spans >= maxspans then out.cut = true return end
          local g = gi[name]
          if not g then
            groups[#groups + 1] = name
            g = #groups - 1
            gi[name] = g
          end
          spans[#spans + 1] = { r, u16(line, a), u16(line, z), g }
        end
      end
    end
  end
end)
if #spans > 0 then out.spans, out.groups = spans, groups end
return out`

// webLuaHlRes is what webLuaHl returns.
type webLuaHlRes struct {
	Err    string   `msgpack:"err"`
	Tick   int      `msgpack:"tick"`
	First  int      `msgpack:"first"`
	Last   int      `msgpack:"last"`
	Groups []string `msgpack:"groups"`
	Spans  [][]int  `msgpack:"spans"`
	None   bool     `msgpack:"none"`
	Cut    bool     `msgpack:"cut"`
}

// webHlGroupName is what a capture name may look like on the wire.
var webHlGroupName = regexp.MustCompile(`^[a-z0-9_.-]{1,48}$`)

// webOpBufHl reads the colours off Update: a first parse of a big file
// takes a moment.
func webOpBufHl(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.HlArgs)
	if m.web == nil || m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil
	}
	b := m.web.filesState().bufs[a.Buf]
	if b == nil || b.term {
		call.fail(webproto.CodeNotFound, "Open the file first.")
		return nil
	}
	client, n, maxSpans := m.nvim, b.buf, webHlMaxSpans
	call.later()
	return func() tea.Msg {
		var res webLuaHlRes
		if err := client.ExecLuaResult(webLuaHl, &res, n, a.First, a.Last, maxSpans); err != nil {
			recordError("[web] buf.hl: " + err.Error())
			call.fail(webproto.CodeInternal, "The colours could not be read.")
			return nil
		}
		if res.Err != "" {
			call.fail(webproto.CodeGone, "This file was closed on the laptop.")
			return nil
		}
		call.reply(webHlClean(res))
		return nil
	}
}

// webHlClean checks what the chunk answered before it goes out: known
// group names, spans inside the range that point at a group.
func webHlClean(res webLuaHlRes) webproto.HlRes {
	out := webproto.HlRes{Tick: res.Tick, First: res.First, Last: res.Last, None: res.None, Cut: res.Cut,
		Groups: []string{}, Spans: [][]int{}}
	keep := make([]int, len(res.Groups)) // chunk index -> out index + 1
	for i, g := range res.Groups {
		if webHlGroupName.MatchString(g) {
			out.Groups = append(out.Groups, g)
			keep[i] = len(out.Groups)
		}
	}
	for _, s := range res.Spans {
		if len(s) != 4 || s[0] < res.First || s[0] >= res.Last || s[1] < 0 || s[2] <= s[1] ||
			s[3] < 0 || s[3] >= len(keep) || keep[s[3]] == 0 || len(out.Spans) >= webproto.MaxHlSpans {
			continue
		}
		out.Spans = append(out.Spans, []int{s[0], s[1], s[2], keep[s[3]] - 1})
	}
	return out
}
