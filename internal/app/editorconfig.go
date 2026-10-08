package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// editorconfigLua makes sure Neovim's built-in EditorConfig support
// (runtime/plugin/editorconfig.lua, nvim ≥ 0.9) is on. We respect an
// explicit `vim.g.editorconfig = false` from the user's init.lua; we only
// default the flag to true. If the user's config skipped runtime plugins
// (`noloadplugins`) the nvim.editorconfig augroup is missing, so we
// register the same autocmd ourselves and apply it to buffers that are
// already loaded.
const editorconfigLua = `
if vim.g.editorconfig == nil then vim.g.editorconfig = true end
local ok, ec = pcall(require, 'editorconfig')
if ok and vim.fn.exists('#nvim.editorconfig') == 0 then
  local group = vim.api.nvim_create_augroup('TermocodeEditorconfig', { clear = true })
  vim.api.nvim_create_autocmd({ 'BufNewFile', 'BufRead', 'BufFilePost' }, {
    group = group,
    callback = function(ev)
      local enable = vim.F.if_nil(vim.b[ev.buf].editorconfig, vim.g.editorconfig, true)
      if enable then pcall(ec.config, ev.buf) end
    end,
  })
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == '' and vim.api.nvim_buf_get_name(b) ~= '' then
      pcall(ec.config, b)
    end
  end
end
`

// bufferFormatLua returns the real per-buffer format options plus the
// EditorConfig state as JSON. Neovim stores the applied properties in
// b:editorconfig (a table) unless EditorConfig is disabled.
const bufferFormatLua = `
local bo = vim.bo
local ec = vim.b.editorconfig
local enabled = not (ec == false or (ec == nil and vim.g.editorconfig == false))
local props = {}
if type(ec) == 'table' then
  for k, v in pairs(ec) do props[k] = tostring(v) end
end
local file = ''
local name = vim.api.nvim_buf_get_name(0)
if name ~= '' and vim.fs and vim.fs.find then
  local found = vim.fs.find('.editorconfig', { upward = true, path = vim.fs.dirname(name) })
  file = found[1] or ''
end
return vim.json.encode({
  expandtab = bo.expandtab,
  shiftwidth = bo.shiftwidth,
  tabstop = bo.tabstop,
  fileencoding = bo.fileencoding,
  fileformat = bo.fileformat,
  ec_enabled = enabled,
  ec_file = file,
  ec_props = props,
})
`

// bufferFormat is the decoded result of bufferFormatLua.
type bufferFormat struct {
	ExpandTab    bool              `json:"expandtab"`
	ShiftWidth   int               `json:"shiftwidth"`
	TabStop      int               `json:"tabstop"`
	FileEncoding string            `json:"fileencoding"`
	FileFormat   string            `json:"fileformat"`
	ECEnabled    bool              `json:"ec_enabled"`
	ECFile       string            `json:"ec_file"`
	ECProps      map[string]string `json:"ec_props"`
}

// parseBufferFormat decodes bufferFormatLua output. Lua's json encoder
// writes an empty table as `[]`, so ec_props may arrive as an array.
func parseBufferFormat(s string) (bufferFormat, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return bufferFormat{}, false
	}
	if p, ok := raw["ec_props"]; ok && strings.HasPrefix(strings.TrimSpace(string(p)), "[") {
		delete(raw, "ec_props")
	}
	clean, _ := json.Marshal(raw)
	var bf bufferFormat
	if err := json.Unmarshal(clean, &bf); err != nil {
		return bufferFormat{}, false
	}
	return bf, true
}

// indentLine renders "Spaces: 4" / "Tabs: 8" from the buffer options.
func (bf bufferFormat) indentLine() string {
	if bf.ExpandTab {
		sw := bf.ShiftWidth
		if sw == 0 {
			sw = bf.TabStop
		}
		return fmt.Sprintf("Spaces: %d", sw)
	}
	return fmt.Sprintf("Tabs: %d", bf.TabStop)
}

// encodingLine renders "UTF-8 (unix)".
func (bf bufferFormat) encodingLine() string {
	enc := strings.ToUpper(bf.FileEncoding)
	if enc == "" {
		enc = "UTF-8"
	}
	if bf.FileFormat != "" {
		return enc + " (" + bf.FileFormat + ")"
	}
	return enc
}

// editorconfigLine renders the EditorConfig row for Buffer Info, e.g.
// "indent_size=2, indent_style=space  (from /repo/.editorconfig)".
func (bf bufferFormat) editorconfigLine() string {
	if !bf.ECEnabled {
		return "off (vim.g.editorconfig = false)"
	}
	if len(bf.ECProps) == 0 || len(bf.ECProps) == 1 && bf.ECProps["root"] != "" {
		if bf.ECFile != "" {
			return "no rules match this file  (" + bf.ECFile + ")"
		}
		return "on, no .editorconfig found"
	}
	keys := make([]string, 0, len(bf.ECProps))
	for k := range bf.ECProps {
		if k == "root" { // file-search marker, not a buffer setting
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+bf.ECProps[k])
	}
	out := strings.Join(parts, ", ")
	if bf.ECFile != "" {
		out += "  (" + bf.ECFile + ")"
	}
	return out
}
