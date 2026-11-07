package app

import (
	"encoding/json"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The user's own mappings for the web editor's Vim mode (WEBP-1): the
// laptop editor's global mappings of normal, visual, operator-pending
// and insert mode, and those of one open file's buffer (per file, per
// language), kept only when the web can run them.

func init() {
	webOps[webproto.OpEditorVimMaps] = webOpEditorVimMaps
	webOps[webproto.OpEditorVimBufMaps] = webOpEditorVimBufMaps
}

// nvimVimMap is one row of nvim_get_keymap, as webVimMapsLua writes it.
type nvimVimMap struct {
	Mode    string `json:"mode"`
	LHS     string `json:"lhs"`
	RHS     string `json:"rhs"`
	Desc    string `json:"desc"`
	Expr    int    `json:"expr"`
	Script  int    `json:"script"`
	Sid     int    `json:"sid"`
	Noremap int    `json:"noremap"`
}

const webVimMapsLua = `
local out = {}
for _, mode in ipairs({ 'n', 'x', 'o', 'i' }) do
  for _, k in ipairs(vim.api.nvim_get_keymap(mode)) do
    if type(k.rhs) == 'string' then
      out[#out + 1] = { mode = mode, lhs = k.lhs, rhs = k.rhs, desc = k.desc or '', expr = k.expr or 0, script = k.script or 0, sid = k.sid or 0, noremap = k.noremap or 0 }
    end
  end
end
if #out == 0 then out = vim.NIL end
return vim.json.encode({ maps = out, timeout = vim.o.timeout and vim.o.timeoutlen or 0 })`

// webVimBufMapsLua is webVimMapsLua for one buffer (nvim_buf_get_keymap).
const webVimBufMapsLua = `
local b = ...
if not vim.api.nvim_buf_is_valid(b) or not vim.api.nvim_buf_is_loaded(b) then return '{"gone":true}' end
local out = {}
for _, mode in ipairs({ 'n', 'x', 'o', 'i' }) do
  for _, k in ipairs(vim.api.nvim_buf_get_keymap(b, mode)) do
    if type(k.rhs) == 'string' then
      out[#out + 1] = { mode = mode, lhs = k.lhs, rhs = k.rhs, desc = k.desc or '', expr = k.expr or 0, script = k.script or 0, sid = k.sid or 0, noremap = k.noremap or 0 }
    end
  end
end
if #out == 0 then out = vim.NIL end
return vim.json.encode({ maps = out })`

func webOpEditorVimBufMaps(m *Model, call *webCall) tea.Cmd {
	b, ok := m.webEditBuf(call, call.args.(*webproto.BufArgs).Buf)
	if !ok {
		return nil
	}
	s, err := m.nvim.EvalLuaStringArgs(webVimBufMapsLua, b.buf)
	var in struct {
		Gone bool         `json:"gone"`
		Maps []nvimVimMap `json:"maps"`
	}
	if err != nil || json.Unmarshal([]byte(s), &in) != nil {
		call.fail(webproto.CodeInternal, "The file's mappings could not be read.")
		return nil
	}
	if in.Gone {
		call.fail(webproto.CodeGone, "This file was closed on the laptop.")
		return nil
	}
	call.reply(webproto.VimMapList{Items: webVimMaps(in.Maps)})
	return nil
}

func webOpEditorVimMaps(m *Model, call *webCall) tea.Cmd {
	out := webproto.VimMapList{Items: []webproto.VimMap{}}
	if m.nvim == nil {
		call.reply(out)
		return nil
	}
	s, err := m.nvim.EvalLuaString(webVimMapsLua)
	var in struct {
		Maps    []nvimVimMap `json:"maps"`
		Timeout int          `json:"timeout"`
	}
	if err != nil || json.Unmarshal([]byte(s), &in) != nil {
		call.fail(webproto.CodeInternal, "The editor's mappings could not be read.")
		return nil
	}
	out.Items = webVimMaps(in.Maps)
	out.Timeout = max(0, in.Timeout)
	call.reply(out)
	return nil
}

// webVimMaps keeps the mappings the web can run, by mode and keys.
func webVimMaps(raw []nvimVimMap) []webproto.VimMap {
	out := []webproto.VimMap{}
	for _, k := range raw {
		if webVimMapOK(k) {
			out = append(out, webproto.VimMap{Mode: k.Mode, LHS: k.LHS, RHS: k.RHS, Noremap: k.Noremap == 1})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Mode != out[j].Mode {
			return out[i].Mode < out[j].Mode
		}
		return out[i].LHS < out[j].LHS
	})
	if len(out) > webproto.MaxVimMaps {
		out = out[:webproto.MaxVimMaps]
	}
	return out
}

func webVimMapOK(k nvimVimMap) bool {
	switch k.Mode {
	case "n", "x", "o", "i":
	default:
		return false
	}
	if k.LHS == "" || k.RHS == "" || len(k.LHS) > webproto.MaxVimMapLHS || len(k.RHS) > webproto.MaxVimMapRHS {
		return false
	}
	if k.Expr != 0 || k.Script != 0 || k.Sid == 0 || strings.HasSuffix(k.Desc, "-default") {
		return false // computed, script-local, or the editor's own default
	}
	lhs, rhs := strings.ToLower(k.LHS), strings.ToLower(k.RHS)
	for _, bad := range []string{"<plug>", "<sid>", "<snr>"} {
		if strings.Contains(lhs, bad) || strings.Contains(rhs, bad) {
			return false
		}
	}
	if strings.Contains(rhs, "<cmd>lua") || strings.HasPrefix(rhs, ":lua") || strings.Contains(rhs, "v:lua") {
		return false // Lua runs in the laptop's editor only
	}
	for _, bad := range []string{"<m-", "<a-", "mouse", "drag", "release", "scrollwheel"} {
		if strings.Contains(lhs, bad) {
			return false // the browser never gives the editor these keys
		}
	}
	for _, r := range k.LHS + k.RHS {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
