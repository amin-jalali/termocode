package app

import (
	"encoding/json"
	"os"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop's pickers (WEBP-9, Editor and files) read the laptop's own
// lists: its bookmarks (nvim's global marks A–Z, as the laptop's
// Bookmarks: commands keep them), its recent files and the snippets of a
// file's type. Paths leave as workspace-relative; a place outside the
// workspace is left out, a secret file's row text too.

func init() {
	webOps[webproto.OpBookmarksToggle] = webOpBookmarksToggle
	webOps[webproto.OpBookmarksList] = webOpBookmarksList
	webOps[webproto.OpBookmarksClear] = webOpBookmarksClear
	webOps[webproto.OpFilesRecent] = webOpFilesRecent
	webOps[webproto.OpSnippetsList] = webOpSnippetsList
}

// webLuaBookmarkToggle takes the mark off row line of buffer b, or sets
// the first free letter there.
const webLuaBookmarkToggle = `
local b, line = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
if line >= api.nvim_buf_line_count(b) then return { err = 'range' } end
for code = 65, 90 do
  local l = string.char(code)
  local p = api.nvim_get_mark(l, {})
  if p[1] == line + 1 and p[3] == b then
    api.nvim_del_mark(l)
    return { on = false, mark = l }
  end
end
for code = 65, 90 do
  local l = string.char(code)
  if api.nvim_get_mark(l, {})[1] == 0 then
    api.nvim_buf_set_mark(b, l, line + 1, 0, {})
    return { on = true, mark = l }
  end
end
return { err = 'full' }`

// webLuaBookmarks lists the global marks as JSON (file, row, text).
const webLuaBookmarks = `
local api = vim.api
local out = {}
for code = 65, 90 do
  local l = string.char(code)
  local p = api.nvim_get_mark(l, {})
  if p[1] > 0 then
    local file, text = p[4] or '', ''
    if p[3] and p[3] > 0 and api.nvim_buf_is_valid(p[3]) then
      file = api.nvim_buf_get_name(p[3])
      if api.nvim_buf_is_loaded(p[3]) then text = api.nvim_buf_get_lines(p[3], p[1] - 1, p[1], false)[1] or '' end
    end
    if file ~= '' then out[#out + 1] = { mark = l, file = vim.fn.fnamemodify(file, ':p'), line = p[1] - 1, text = text } end
  end
end
if #out == 0 then return '[]' end
return vim.json.encode(out)`

const webLuaBookmarksClear = `
for code = 65, 90 do pcall(vim.api.nvim_del_mark, string.char(code)) end`

// webLuaSnippets lists the snippets of buffer b's file type as JSON.
const webLuaSnippets = `
local b, maxb = ...
if not vim.api.nvim_buf_is_valid(b) then return '[]' end
if type(_G.termocode_snippets_for) ~= 'function' then return '[]' end
local ok, tbl = pcall(_G.termocode_snippets_for, vim.bo[b].filetype)
if not ok or type(tbl) ~= 'table' then return '[]' end
local out = {}
for k, v in pairs(tbl) do
  if type(k) == 'string' and type(v) == 'string' and #v <= maxb then out[#out + 1] = { trigger = k, body = v } end
end
if #out == 0 then return '[]' end
return vim.json.encode(out)`

type webLuaMarkRes struct {
	Err  string `msgpack:"err"`
	On   bool   `msgpack:"on"`
	Mark string `msgpack:"mark"`
}

func webOpBookmarksToggle(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.BufLineArgs)
	b, ok := m.webEditBuf(call, a.Buf)
	if !ok {
		return nil
	}
	var res webLuaMarkRes
	if err := m.nvim.ExecLuaResult(webLuaBookmarkToggle, &res, b.buf, a.Line); err != nil {
		webFail(call, err)
		return nil
	}
	switch res.Err {
	case "":
		call.reply(webproto.BookmarkToggle{On: res.On, Mark: res.Mark})
	case "range":
		call.fail(webproto.CodeBadRequest, "That row does not exist any more.")
	case "full":
		call.fail(webproto.CodeBusy, "All 26 bookmarks are in use. Clear some first.")
	default:
		call.fail(webproto.CodeGone, "This file was closed on the laptop.")
	}
	return nil
}

func webOpBookmarksList(m *Model, call *webCall) tea.Cmd {
	if m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil
	}
	var raw string
	if err := m.nvim.ExecLuaResult(webLuaBookmarks, &raw); err != nil {
		webFail(call, err)
		return nil
	}
	var marks []struct {
		Mark, File, Text string
		Line             int
	}
	if err := json.Unmarshal([]byte(raw), &marks); err != nil {
		call.fail(webproto.CodeInternal, "The bookmarks could not be read.")
		return nil
	}
	out := webproto.BookmarkList{Items: []webproto.Bookmark{}}
	for _, k := range marks {
		p, in := m.webRel(k.File)
		if !in || p == "" || k.Line < 0 {
			continue
		}
		it := webproto.Bookmark{Mark: k.Mark, Path: p, Line: k.Line}
		if !ai.IsSecretFileName(k.File) {
			it.Text = webDiagCut(k.Text, webproto.MaxLocationText)
		}
		out.Items = append(out.Items, it)
	}
	call.reply(out)
	return nil
}

func webOpBookmarksClear(m *Model, call *webCall) tea.Cmd {
	if m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil
	}
	if err := m.nvim.ExecLua(webLuaBookmarksClear); err != nil {
		webFail(call, err)
		return nil
	}
	call.reply(nil)
	return nil
}

// webOpFilesRecent reads the recents file off Update.
func webOpFilesRecent(m *Model, call *webCall) tea.Cmd {
	rel := m.webRel
	call.later()
	return func() tea.Msg {
		entries := loadRecents()
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].OpenedAt.After(entries[j].OpenedAt) })
		out := webproto.FilesRecent{Paths: []string{}}
		seen := map[string]bool{}
		for _, e := range entries {
			if len(out.Paths) == webproto.MaxRecent {
				break
			}
			p, in := rel(e.Path)
			if !in || p == "" || seen[p] {
				continue
			}
			if fi, err := os.Stat(e.Path); err != nil || fi.IsDir() {
				continue
			}
			seen[p] = true
			out.Paths = append(out.Paths, p)
		}
		call.reply(out)
		return nil
	}
}

func webOpSnippetsList(m *Model, call *webCall) tea.Cmd {
	b, ok := m.webEditBuf(call, call.args.(*webproto.BufArgs).Buf)
	if !ok {
		return nil
	}
	var raw string
	if err := m.nvim.ExecLuaResult(webLuaSnippets, &raw, b.buf, webproto.MaxSnippetBody); err != nil {
		webFail(call, err)
		return nil
	}
	var items []webproto.Snippet
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		call.fail(webproto.CodeInternal, "The snippets could not be read.")
		return nil
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Trigger < items[j].Trigger })
	if len(items) > webproto.MaxSnippets {
		items = items[:webproto.MaxSnippets]
	}
	if items == nil {
		items = []webproto.Snippet{}
	}
	call.reply(webproto.SnippetList{Items: items})
	return nil
}
